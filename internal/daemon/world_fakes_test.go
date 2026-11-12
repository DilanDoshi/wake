package daemon

// The fake agent behind world_unix_test.go: one that, like a real claude, starts
// a shell in a process group of its own, leaving a second child in its own group
// as well, and - when told to - holds its session open for that shell.

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// fakeLeaderEnv turns this binary into the shell: a group leader with a
	// child of its own in that group.
	fakeLeaderEnv = "WAKE_FAKE_LEADER"
	// fakeReleaseEnv names a file whose appearance makes the shell exit, leaving
	// its child orphaned in the shell's group: a hook's escapee.
	fakeReleaseEnv = "WAKE_FAKE_RELEASE"
	// fakeTaskEnv makes the agent report a background shell's start, as claude
	// does for a Bash call run in the background.
	fakeTaskEnv = "WAKE_FAKE_TASK"
	// fakeHoldEnv makes the agent do what claude does: on stdin EOF it stays until
	// the shell it started has ended, since a background shell holds a headless
	// session open - unless it was interrupted first, which frees it.
	fakeHoldEnv = "WAKE_FAKE_HOLD"
	// fakeSlowEnv makes each turn take this long, so a stop can land mid-turn.
	fakeSlowEnv = "WAKE_FAKE_SLOW"
	// fakeLateEnv starts the shell during the first turn the agent is sent, after
	// the delay, instead of at launch.
	fakeLateEnv = "WAKE_FAKE_LATE"
	// fakeSelfTurnEnv makes the agent open a turn of its own after "ready", this
	// long: claude does, when a background task finishes.
	fakeSelfTurnEnv = "WAKE_FAKE_SELFTURN"
	// fakePidfileEnv names the file the pids are written beside: the shell's
	// pid in it, the shell's child in <it>.gc, the agent's own-group child in
	// <it>.own.
	fakePidfileEnv = "WAKE_FAKE_PIDFILE"
)

// runGroupLeader is the shell: it starts a child that stays in its group,
// records the child's pid, and then either lingers or - once released -
// exits, which orphans the child under its group's reserved id.
func runGroupLeader() int {
	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), fakeLingerEnv+"=1")
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "leader:", err)
		return 1
	}
	_ = os.WriteFile(os.Getenv(fakePidfileEnv)+".gc", []byte(strconv.Itoa(child.Process.Pid)), 0o600)
	if release := os.Getenv(fakeReleaseEnv); release != "" {
		for {
			if _, err := os.Stat(release); err == nil {
				return 0
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	time.Sleep(lingerFor)
	return 0
}

// launchShell starts the shell in a group of its own, records its pid, waits for
// its child's, and reports its start as claude does when asked to.
func launchShell(sid, pidfile string) *exec.Cmd {
	shell := exec.Command(os.Args[0])
	shell.Env = append(os.Environ(), fakeLeaderEnv+"=1")
	leaveProcessGroup(shell)
	if err := shell.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "shellgroup:", err)
		os.Exit(1)
	}
	_ = os.WriteFile(pidfile, []byte(strconv.Itoa(shell.Process.Pid)), 0o600)
	go func() { _ = shell.Wait() }() // reaped as claude reaps its own

	// The shell's own child is the last thing recorded, so a test that reads all
	// three has a whole world to look at.
	for deadline := time.Now().Add(testTimeout); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(pidfile + ".gc"); err == nil {
			break
		}
	}
	if os.Getenv(fakeTaskEnv) == "1" {
		fmt.Printf(`{"type":"system","subtype":"task_started","task_id":"by1","run_id":"r1","tool_use_id":"toolu_1","description":"dev server","is_backgrounded":true,"task_type":"local_bash","session_id":%q}`+"\n", sid)
	}
	return shell
}

var lineUUID = regexp.MustCompile(`"uuid":"([0-9a-fA-F-]{36})"`)

// fakeShellGroup is an agent with two kinds of leftover: a child that stays in
// the agent's own group, and a shell in a group of its own (claude's background
// shell). Like claude it ignores control requests but an interrupt, reports each
// message it takes up, and - when held - stays for its shell once stdin closes.
func fakeShellGroup(sid string) int {
	pidfile := os.Getenv(fakePidfileEnv)

	own := exec.Command(os.Args[0])
	own.Env = append(os.Environ(), fakeLingerEnv+"=1")
	if err := own.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "shellgroup:", err)
		return 1
	}
	_ = os.WriteFile(pidfile+".own", []byte(strconv.Itoa(own.Process.Pid)), 0o600)

	late := os.Getenv(fakeLateEnv) == "1"
	var shell *exec.Cmd
	if !late {
		shell = launchShell(sid, pidfile)
	}
	emitText(sid, "ready")
	emitResult(sid)
	if self, err := time.ParseDuration(os.Getenv(fakeSelfTurnEnv)); err == nil {
		emitText(sid, "tick")
		time.Sleep(self)
		emitText(sid, "tock")
		emitResult(sid)
	}

	delay, _ := time.ParseDuration(os.Getenv(fakeSlowEnv))
	interrupted := false
	for line := range stdinLines() {
		if strings.Contains(line, `"type":"control_request"`) {
			if strings.Contains(line, `"subtype":"interrupt"`) {
				interrupted = true
				fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"still_queued":[]}}}`+"\n",
					controlRequestID(line))
			}
			continue
		}
		if m := lineUUID.FindStringSubmatch(line); m != nil {
			fmt.Printf(`{"type":"command_lifecycle","command_uuid":%q,"state":"started","session_id":%q}`+"\n", m[1], sid)
		}
		time.Sleep(delay)
		if late && shell == nil {
			shell = launchShell(sid, pidfile)
		}
		emitText(sid, "echo: "+line)
		emitResult(sid)
	}
	if os.Getenv(fakeHoldEnv) == "1" && !interrupted && shell != nil {
		for syscall.Kill(shell.Process.Pid, 0) == nil {
			time.Sleep(10 * time.Millisecond)
		}
	}
	return 0
}
