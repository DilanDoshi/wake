// The fake agent, and the fake `wake daemon`.
//
// These tests drive the real thing end to end: a real daemon on a real unix
// socket, a real client speaking rpc, and real `claude` processes - except
// that `claude` is this test binary wearing a symlink. Nothing here ever
// invokes a live model; a live model is slow, nondeterministic and billed per
// CI run.
//
// The mechanism is the standard Go helper-process pattern, one turn further
// than core's: core substitutes its execCommand seam, but the daemon calls
// core.NewSession and has no seam to substitute. So the test puts a directory
// on PATH containing a symlink named `claude` pointing at the test binary, and
// TestMain below intercepts the re-entry before the testing package parses a
// single flag. What the daemon spawns is a genuine process, with a genuine
// stdout pipe, a genuine process group and a genuine exit - which is the only
// way to test a wedged agent, a stdout an orphan holds open, or a reaper.
//
// That makes this file a place in the package allowed to name Claude's JSON,
// for the same narrow reason core's session_test.go is: something has to speak
// the wire to prove the daemon never has to.
//
// It is no longer the only one. faketool_test.go holds the permission fake with
// a consequence, split off by subject when this file left the project's 200-400
// band - deliberately at that point rather than after the 800 hard max, which
// is the mistake decisions.md records the airlock making. The set is those two,
// and nothing else in internal/daemon may name the wire.

package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// The markers that turn this binary into something other than a test run.
// Each branch unsets its own before doing anything that spawns: a child that
// re-entered the same branch would fork bomb, which is the trap core's helper
// documents having walked into.
const (
	fakeClaudeEnv    = "WAKE_FAKE_CLAUDE"
	fakeDaemonEnv    = "WAKE_FAKE_DAEMON"
	fakeDaemonPIDEnv = "WAKE_FAKE_DAEMON_PID"
	fakeLingerEnv    = "WAKE_FAKE_LINGER"
	fakeLingerSayEnv = "WAKE_FAKE_LINGER_SAY"
	fakeScriptEnv    = "WAKE_FAKE_SCRIPT"
	fakeCountEnv     = "WAKE_FAKE_COUNT"
	fakeDelayEnv     = "WAKE_FAKE_DELAY"
	fakePsEnv        = "WAKE_FAKE_PS"
	fakeToolDirEnv   = "WAKE_FAKE_TOOL_DIR"
	fakeAdmitGateEnv = "WAKE_FAKE_ADMIT_GATE"
)

// How the fake ps fails to answer. Empty is the refusing one.
const (
	psRefuses = "refuse"
	psHangs   = "hang"
	psQuiet   = "quiet"
	psPartial = "partial"
)

// lingerFor is how long a grandchild holds what it inherited. Long enough
// that a test timing out means a bound is missing rather than that the
// machine was slow.
const lingerFor = 30 * time.Second

func TestMain(m *testing.M) {
	// Before the switch, so it reaches every daemon this binary runs: the
	// in-process ones startDaemon serves, and the forked `wake daemon` below,
	// which EnsureRunning execs as a separate process with its own copy of
	// this variable. testQuitGrace carries the argument for why the
	// production value cannot be one of them.
	quitGrace = testQuitGrace

	// The order of these is load-bearing, and each one cost a debugging round
	// somewhere in this project.
	//
	// ps first, and on argv[0] rather than on a marker. Every other branch
	// here is selected by an environment variable, and a ps spawned by a
	// daemon that is itself pretending to be claude would inherit both - so
	// whichever marker were checked first would win and the other branch
	// would never run. The name it was invoked under is the one thing that
	// cannot be inherited.
	//
	// Linger next: a grandchild inherits the claude marker from the agent
	// that spawned it, and appending a replacement does not override it -
	// Getenv returns the first match.
	//
	// Daemon before claude, because a forked daemon must inherit the claude
	// marker: its own children are the fake agents. Checked the other way
	// round, the daemon started life as an agent, read EOF from the
	// /dev/null a background process gets for stdin, and exited without a
	// word - which surfaced as "daemon did not start listening" with no
	// explanation anywhere.
	switch {
	case filepath.Base(os.Args[0]) == "ps":
		os.Exit(runFakePs())
	case os.Getenv(fakeLingerEnv) == "1":
		os.Exit(runLinger())
	case os.Getenv(fakeDaemonEnv) == "1":
		os.Exit(runFakeDaemon())
	case core.AgentLauncherRequested():
		// A supervisor re-exec of this binary: become the supervisor, which then
		// execs the fake claude on PATH. Before the claude branch because a
		// supervisor inherits the fake-claude marker from the daemon that started
		// it - checked the other way round, the supervisor would run as the agent.
		if err := core.RunAgentLauncher(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	case os.Getenv(fakeClaudeEnv) == "1":
		os.Exit(runFakeClaude())
	}
	// Agents run on the direct path unless a test opts into the supervisor (see
	// useRealSupervisor), so the reclaim, reaper and process-table suites are
	// unchanged by activation. Set as an inherited env so a forked daemon gets it
	// too. A supervisor re-exec never reaches this line - it os.Exits above.
	if err := os.Setenv(DirectAgentLauncherEnv, "1"); err != nil {
		fmt.Fprintln(os.Stderr, "set direct launcher:", err)
		os.Exit(1)
	}
	leaseR, leaseW, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "test parent lease:", err)
		os.Exit(1)
	}
	if err := os.Setenv(testParentLeaseSourceEnv, strconv.Itoa(int(leaseR.Fd()))); err != nil {
		fmt.Fprintln(os.Stderr, "test parent lease:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = leaseW.Close()
	_ = leaseR.Close()
	os.Exit(code)
}

// runFakePs is a ps(1) that cannot answer, in one of the three ways a real one
// cannot. None of them is the same as a pid that is not there, and reading any
// of them as "gone" ends in a SIGKILL to a living agent's process group.
//
//   - refuse: runs and rejects the flags, which is what a container's busybox
//     does. Exits non-zero for every pid - exactly as the real thing does for
//     a missing pid - and says so on stderr, which is the only thing telling
//     the two apart.
//   - hang: outlives probeTimeout, so exec.CommandContext SIGKILLs it. That
//     one exits with *nothing* on either stream, which is why the exit status
//     itself has to be inspected. See inspect in reap_unix.go.
//   - quiet: exits 0 and says nothing. No stock ps does this, which is exactly
//     why it must not be read as an answer.
func runFakePs() int {
	switch os.Getenv(fakePsEnv) {
	case psHangs:
		time.Sleep(lingerFor)
		return 0
	case psQuiet:
		return 0
	case psPartial:
		// Exits 0 with a listing that is well formed and incomplete. No stock
		// ps does this either, and it is the one failure the whole-machine
		// probe cannot notice any other way: absence from the listing is what
		// "gone" means to goneNow, so a ps that answers partially would declare
		// live agents dead - and every row here parses, so the empty-listing
		// refusal never fires. What catches it is that the caller is not in its
		// own machine's process table. See processTable.
		fmt.Println("    1 S /sbin/launchd")
		fmt.Println("    2 S /usr/libexec/logd")
		return 0
	}
	fmt.Fprintln(os.Stderr, "ps: unrecognized option: ww")
	fmt.Fprintln(os.Stderr, "BusyBox v1.36.1 multi-call binary.")
	return 1
}

// runLinger is the grandchild: it holds whatever it inherited for far longer
// than any bound Wake sets, and writes nothing unless it is asked to.
//
// Being asked to is what makes the wedge testable rather than racy. The
// agent that spawned it exits the instant Start returns, so a test that sent
// a message straight after the agent's last frame could still be writing to a
// process that had not quite gone - which is a test that passes for the wrong
// reason on a fast machine and fails on a slow one. This waits, then speaks
// on the stdout it inherited: a frame from here proves the agent is gone.
func runLinger() int {
	if sid := os.Getenv(fakeLingerSayEnv); sid != "" {
		waitForParentToExit()
		emitText(sid, "held")
	}
	time.Sleep(lingerFor)
	return 0
}

// parentAliveFD is the descriptor the agent hands its grandchild: the read
// end of a pipe whose only write end the agent itself holds. os/exec puts
// the first ExtraFiles entry at fd 3.
const parentAliveFD = 3

// waitForParentToExit blocks until the process that spawned this one is
// really gone, and it is deliberately an observation rather than a delay.
//
// Two earlier drafts of this were wrong in opposite directions, and both are
// worth knowing about:
//
//   - A 250ms sleep passed 3/3 without the race detector and failed 3/3 with
//     it. Under -race the agent's own exit is slow enough that the write the
//     test is about still landed in the pipe buffer of a process that had not
//     finished exiting - so the test asserted a wedge it had not created.
//   - Waiting for getppid to change never fires at all. The agent becomes a
//     *zombie*: reparenting happens when a process is reaped, and nothing
//     reaps this one - core's pump is parked in Scan, which is the whole
//     scenario. It stays this process's parent forever.
//
// The pipe has neither problem. Its only write end is held by the agent, so
// the read here returns EOF the moment that process's descriptors are
// released, which is exactly when its stdin stops being writable.
func waitForParentToExit() {
	alive := os.NewFile(parentAliveFD, "parent-alive")
	if alive == nil {
		return
	}
	defer func() { _ = alive.Close() }()
	_, _ = io.Copy(io.Discard, alive)
}

// runFakeClaude impersonates an agent: stream-json on stdout, newline JSON on
// stdin, alive until stdin closes.
func runFakeClaude() int {
	_ = os.Unsetenv(fakeClaudeEnv)
	sid := argValue(os.Args, "--session-id")

	switch os.Getenv(fakeScriptEnv) {
	case "ask":
		return fakeAsk(sid)
	case "twoasks":
		return fakeTwoAsks(sid)
	case "mute":
		return fakeMute()
	case memoryScript:
		// The only fake whose identity cannot come from --session-id: a wake
		// is started with a bare `--resume <id>` and no --session-id at all
		// (identityArgs), so reading only the first would hand this fake an
		// empty key for exactly the process the test is about.
		if resume := argValue(os.Args, "--resume"); resume != "" {
			return fakeMemory(resume, true)
		}
		return fakeMemory(sid, false)
	case "hold":
		return fakeHold(sid)
	case "flood":
		return fakeFlood(sid)
	case "slow":
		return fakeSlowTurns(sid)
	case "noisyexit":
		return fakeNoisyExit(sid)
	case "delayedexit":
		return fakeDelayedExit(sid)
	case "fdcheck":
		return fakeFDCheck(sid)
	case "leaseenv":
		return fakeLeaseEnvironment(sid)
	case "closedstdin":
		return fakeClosedStdin(sid)
	case "deaf":
		return fakeDeaf(sid)
	case "leak":
		return fakeLeak(sid)
	case "cwd":
		return fakeCwd(sid)
	case "supervised":
		return fakeSupervised(sid)
	case "interruptible":
		return fakeInterruptible(sid)
	case "mode":
		return fakeMode(sid)
	case "probe":
		return fakeModelProbe(sid)
	case "tool":
		return fakeTool(sid)
	case "name":
		return fakeName(sid)
	case "argv":
		return fakeArgv(sid)
	case "question":
		// Both of these replay recorded bytes rather than hand-written
		// frames, and take no session id for that reason: every line they
		// emit already carries the one the recording had, and the daemon
		// attributes an event to the session it arrived on. See
		// question_test.go.
		return fakeQuestion()
	case "plan":
		return fakePlan()
	default:
		return fakeTurns(sid)
	}
}

func fakeClosedStdin(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	_ = os.Stdin.Close()
	emitText(sid, "stdin closed; still working")
	time.Sleep(500 * time.Millisecond)
	return 0
}

func fakeLeaseEnvironment(sid string) int {
	var inherited []string
	for _, name := range []string{testParentLeaseSourceEnv, testParentLeaseDaemonEnv} {
		if value, ok := os.LookupEnv(name); ok {
			inherited = append(inherited, name+"="+value)
		}
	}
	state := "clean"
	if len(inherited) != 0 {
		state = strings.Join(inherited, ", ")
	}
	emitText(sid, "test lease environment "+state)
	emitResult(sid)
	for range stdinLines() {
	}
	return 0
}

func fakeFDCheck(sid string) int {
	fd, _ := strconv.Atoi(os.Getenv("WAKE_FAKE_FORBIDDEN_FD"))
	file := os.NewFile(uintptr(fd), "test lease")
	if _, err := file.Stat(); err == nil {
		emitText(sid, "test lease descriptor inherited")
	} else {
		emitText(sid, "test lease descriptor closed")
	}
	_ = file.Close()
	emitResult(sid)
	for range stdinLines() {
	}
	return 0
}

// fakeDelayedExit leaves enough time for its client to disconnect, then ends
// on its own so retirement is the only event that can stop the daemon.
func fakeDelayedExit(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	time.Sleep(500 * time.Millisecond)
	return 0
}

// fakeNoisyExit behaves normally and then ends badly: it takes its turns, sees
// stdin close, complains on stderr and exits non-zero.
//
// It exists because every other fake here exits 0, so no test in this package
// has ever driven an ending whose `sess.Err()` is non-nil through a path that
// has to *ignore* it. Park is that path - `core.waitDelay` turns a clean exit 0
// into an error whenever something the agent spawned held stderr past the
// bound, which is the routine case for an agent running a stdio MCP server, and
// none of it says anything about the transcript `--resume` reads.
//
// Exit 3 with something on stderr rather than exit 1 with nothing, and that is
// not arbitrary: `core.interruptedExit` suppresses exactly the second shape, so
// a fake using it would produce a *nil* error and the test over it would assert
// nothing.
func fakeNoisyExit(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	for line := range stdinLines() {
		emitText(sid, "echo: "+line)
		emitResult(sid)
	}
	fmt.Fprintln(os.Stderr, "the mcp server this agent started is still holding the pipe")
	return 3
}

// fakeSlowTurns takes its time over a turn, which is what makes stop and kill
// distinguishable at all: stop has something to wait for, and kill has
// something to interrupt.
func fakeSlowTurns(sid string) int {
	delay, err := time.ParseDuration(os.Getenv(fakeDelayEnv))
	if err != nil {
		delay = 100 * time.Millisecond
	}
	emitText(sid, "ready")
	emitResult(sid)
	for line := range stdinLines() {
		time.Sleep(delay)
		emitText(sid, "echo: "+line)
		emitResult(sid)
	}
	return 0
}

// runFakeDaemon impersonates `wake daemon`, the background process
// EnsureRunning forks. It is the same call cmd/wake will make.
func runFakeDaemon() int {
	// Or every agent this daemon spawns becomes another daemon.
	_ = os.Unsetenv(fakeDaemonEnv)
	if path := os.Getenv(fakeDaemonPIDEnv); path != "" {
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "fake daemon:", err)
			return 1
		}
	}
	socket, err := SocketPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake daemon:", err)
		return 1
	}
	if gate := os.Getenv(fakeAdmitGateEnv); gate != "" {
		return runAdmissionGatedDaemon(socket, gate)
	}
	if err := Serve(context.Background(), socket); err != nil {
		fmt.Fprintln(os.Stderr, "fake daemon:", err)
		return 1
	}
	return 0
}

func runAdmissionGatedDaemon(socket, gatePath string) int {
	gate, err := net.Dial("unix", gatePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake gated daemon:", err)
		return 1
	}
	defer func() { _ = gate.Close() }()
	s := newServer(socket)
	s.admitMu.Lock()
	go func() {
		_, _ = io.Copy(io.Discard, gate)
		s.admitMu.Unlock()
	}()
	ln, err := listen(socket)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake gated daemon:", err)
		return 1
	}
	if _, err := gate.Write([]byte{'L'}); err != nil {
		fmt.Fprintln(os.Stderr, "fake gated daemon:", err)
		return 1
	}
	go func() {
		for {
			s.parked.mu.Lock()
			reserved := len(s.parked.reserved) != 0
			s.parked.mu.Unlock()
			if reserved {
				_, _ = gate.Write([]byte{'R'})
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	runErr := s.run(context.Background(), ln)
	shutdownErr := s.shutdown()
	closeErr := ln.Close()
	if err := errors.Join(runErr, shutdownErr, closeErr); err != nil {
		fmt.Fprintln(os.Stderr, "fake gated daemon:", err)
		return 1
	}
	return 0
}

// fakeTurns is the ordinary agent: one opening turn, then a turn per line on
// stdin, ending when stdin closes. That is how a real process behaves - it
// idles between turns and exits on EOF, which is what makes Stop work.
func fakeTurns(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	for line := range stdinLines() {
		emitText(sid, "echo: "+line)
		emitResult(sid)
	}
	return 0
}

// fakeLeak spawns a child that stays in the agent's own process group - the
// `npm run dev &` case - and then behaves like fakeTurns, so the agent still
// exits cleanly on EOF. It is the daemon analog of core's
// startOrphan(func(*exec.Cmd){}): fakeHold's grandchild leaves the group and
// holds stdout on purpose, and this one must do neither. Left in the group, it
// is reached only by retire's group sweep; holding none of the agent's pipes,
// it does not wedge the pump, so the clean-exit path runs at all. The parent
// records the pid so a test never races the child writing its own.
func fakeLeak(sid string) int {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), fakeLingerEnv+"=1")
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "leak:", err)
		return 1
	}
	if pidfile := os.Getenv("WAKE_FAKE_PIDFILE"); pidfile != "" {
		_ = os.WriteFile(pidfile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	}
	return fakeTurns(sid)
}

// fakeInterruptible answers an interrupt the way the recordings do, and keeps
// working afterwards.
//
// Both halves are the point. The receipt echoes the request_id back, which is
// the only correlator either end has - a permission answer at least names a
// tool, a receipt names nothing at all. And the process **stays alive**: an
// interrupt aborts a turn, not a session, so anything that let the daemon
// treat it as an ending would show here as a session that vanished.
func fakeInterruptible(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	for line := range stdinLines() {
		if !strings.Contains(line, `"subtype":"interrupt"`) {
			emitText(sid, "echo: "+line)
			emitResult(sid)
			continue
		}
		fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"still_queued":[]}}}`+"\n",
			controlRequestID(line))
		emitText(sid, "interrupted")
		emitResult(sid)
	}
	return 0
}

// controlRequestID reads the correlator off the envelope, where a
// control_request carries it - a control_response nests its own one level
// further, which is the trap on the other side of this exchange. Shared by both
// control_requests Wake sends.
func controlRequestID(line string) string {
	var f struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		return ""
	}
	return f.RequestID
}

// fakeMode answers a set_permission_mode request the way 2.1.228 does, and the
// half that matters is the normalization: `manual` is accepted and comes back
// `default` (permission-mode-findings.md §6). Without that in the fake, nothing
// could tell a label that follows the receipt from one that follows the
// keystroke, because the two would never disagree.
func fakeMode(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	for line := range stdinLines() {
		if !strings.Contains(line, `"subtype":"set_permission_mode"`) {
			emitText(sid, "echo: "+line)
			emitResult(sid)
			continue
		}
		fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"mode":%q}}}`+"\n",
			controlRequestID(line), normalizeFakeMode(modeAsked(line)))
		emitResult(sid)
	}
	return 0
}

// modeAsked reads the mode out of a set_permission_mode request, one level down
// beside its subtype.
func modeAsked(line string) string {
	var f struct {
		Request struct {
			Mode string `json:"mode"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(line), &f); err != nil {
		return ""
	}
	return f.Request.Mode
}

// normalizeFakeMode is the one recorded rewrite the CLI performs.
func normalizeFakeMode(mode string) string {
	if mode == "manual" {
		return core.PermissionModeDefault
	}
	return mode
}

// fakeCwd says where it is running, which is the only way to find out what
// directory the daemon actually spawned it in.
// fakeSupervised reports whether it is its own process-group leader or a child
// of one. On the direct path the target is the leader Wake put a Setpgid on
// (pid == pgid); under the supervisor it is a non-leader child in the
// supervisor's group (pid != pgid). That difference is the only in-band proof
// that activation actually put a supervisor between the daemon and claude.
func fakeSupervised(sid string) int {
	role := "leader"
	if os.Getpid() != syscall.Getpgrp() {
		role = "child"
	}
	emitText(sid, "supervised: "+role)
	emitResult(sid)
	for range stdinLines() {
	}
	return 0
}

func fakeCwd(sid string) int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cwd:", err)
		return 1
	}
	emitText(sid, "cwd: "+wd)
	emitResult(sid)
	for range stdinLines() {
	}
	return 0
}

// fakeName says what --name it was given, which is the only way to find out
// whether the name the daemon assigned reached the agent at all.
//
// It matters because the name is not decoration: spec §7 routes on `@name`, and
// a name the daemon holds and the process has never heard of is a name that
// cannot be routed to. core.Config.Name is what puts it on the command line.
func fakeName(sid string) int {
	emitText(sid, "name: "+argValue(os.Args, "--name"))
	emitResult(sid)
	for range stdinLines() {
	}
	return 0
}

// fakeArgv reports the whole command line it was started with, so a test can
// assert on flags no other fake exposes.
//
// The whole line rather than one flag's value: the fork's three identity flags
// are only correct *together and in order*, and argValue would happily report
// a --session-id sitting beside a --resume that is going to be refused at
// startup with nothing on stdout.
func fakeArgv(sid string) int {
	emitText(sid, "argv: "+strings.Join(os.Args, " "))
	emitResult(sid)
	for range stdinLines() {
	}
	return 0
}

// fakeDeaf never reads its stdin, so it outlives the daemon that spawned it.
//
// That is what makes it the reaper's case. A daemon dying closes the stdin it
// held, and an agent waiting on stdin would take the EOF and exit on its own -
// but an agent *mid-turn* is not waiting on stdin, and neither is anything it
// spawned. Those are the trees a SIGKILLed daemon leaves behind, and there is
// no handle to any of them.
func fakeDeaf(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	time.Sleep(lingerFor)
	return 0
}

// fakeAsk blocks on a permission request the way --permission-prompt-tool
// stdio does: the ask goes out and nothing else happens until an answer
// arrives on stdin.
func fakeAsk(sid string) int {
	fmt.Printf(`{"type":"control_request","request_id":%q,"request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"note.txt","content":"ok"},"tool_use_id":"toolu_1"}}`+"\n", askRequestID)
	aborted := false
	for line := range stdinLines() {
		switch {
		case strings.Contains(line, `"subtype":"interrupt"`):
			fakeWithdrawTheAsk(line)
			aborted = true
			continue
		case aborted:
			// The aborted turn's end, held back until now. See
			// fakeWithdrawTheAsk for why the gap is deliberate.
			aborted = false
			emitText(sid, "interrupted")
			emitResult(sid)
			continue
		}
		if !strings.Contains(line, `"type":"control_response"`) {
			continue
		}
		behavior := "deny"
		if strings.Contains(line, `"behavior":"allow"`) {
			behavior = "allow"
		}
		emitText(sid, "permission "+behavior)
		emitResult(sid)
	}
	return 0
}

// fakeWithdrawTheAsk answers an interrupt that landed on an outstanding ask,
// in the order the recording puts these frames on the wire:
// interrupt-pending-basic.jsonl has the withdrawal at :14, the interrupt's own
// receipt at :15, and the aborted turn's result at :18.
//
// The withdrawal names the *ask*; the receipt names the *interrupt*. The two
// ids never meet on one frame, and the receipt says nothing at all about the
// ask it destroyed (findings §3), which is why the withdrawal is the only
// frame a client can retire an ask on.
//
// **The turn end is deliberately not emitted here**, and that is the whole
// reason this exists as its own step. The recording puts the result four
// frames and single-digit milliseconds after the withdrawal, and the daemon
// clears an outstanding ask on a turn end too - so a fake that emitted both at
// once would leave a test asserting the ask was retired passing whether or not
// anything acts on the withdrawal at all. Wake's caller releases the turn end
// by writing the next line to stdin, which holds the window open long enough
// for a test to look inside it. The gap is widened; the order is not changed.
func fakeWithdrawTheAsk(interrupt string) {
	fmt.Printf(`{"type":"control_cancel_request","request_id":%q}`+"\n", askRequestID)
	fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"still_queued":[]}}}`+"\n",
		controlRequestID(interrupt))
}

// askRequestID is the correlator the fake ask carries. A permission request
// has no session_id on Claude's wire, so this is the only thing tying an
// answer to it.
//
// Every fake here uses the same one, including two sessions asking at the same
// time, and that is deliberate rather than lazy: the CLI numbers requests per
// process, so `req-1` really is what two agents' first asks are both called.
// An answer is routed by the session Wake stamped on the ask - see
// core.Session.attribute - and a daemon that correlated on this string instead
// would run the wrong agent's tool. TestAnAllowRunsTheToolOfOnlyTheSessionItNames
// is where that is held.
const askRequestID = "req-1"

// askRequestID2 is the second correlator fakeTwoAsks carries, for the case a
// session is blocked on two asks at once. The CLI numbers requests per process,
// so a real second ask in one turn really is `req-2`.
const askRequestID2 = "req-2"

// fakeTwoAsks blocks on two permission requests at once - the concurrent-ask
// shape a parent and its subagent, or two parallel tool calls, produce - and
// ends the turn only once both have been answered on stdin. It is the
// whole-stack half of concurrent_ask_test.go: that one proves the daemon tracks
// both, this drives both through the socket so a report naming only one - the
// bug - would leave the second answer with nothing to correlate against.
func fakeTwoAsks(sid string) int {
	for _, rid := range []string{askRequestID, askRequestID2} {
		fmt.Printf(`{"type":"control_request","request_id":%q,"request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"note.txt","content":"ok"},"tool_use_id":"toolu_1"}}`+"\n", rid)
	}
	answered := 0
	for line := range stdinLines() {
		if !strings.Contains(line, `"type":"control_response"`) {
			continue
		}
		if answered++; answered < 2 {
			continue
		}
		emitText(sid, "both answered")
		emitResult(sid)
	}
	return 0
}

// fakeMute reads its input and says nothing at all. It is a session that owes
// a turn end and never produces one, which is the state the daemon has to
// call silent rather than idle.
func fakeMute() int {
	for range stdinLines() {
	}
	return 0
}

// fakeHold is the wedged agent, and the reason these tests spawn real
// processes. It hands its stdout to a grandchild in another process group and
// exits: the process is gone, but the pipe Wake is reading is not, so core's
// pump stays parked in Scan with Err() nil and Events() open. From outside it
// is indistinguishable from an agent thinking hard.
func fakeHold(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	_ = os.Stdout.Sync()

	// The write end is never closed and never written to: this process
	// holding it is the whole signal, and its exit is what delivers the EOF
	// that tells the grandchild the wedge is real.
	//
	// Deliberately never closed here, not even on the way out: a defer would
	// close it while this process is still very much alive, which is exactly
	// the wrong moment and cost a debugging round. Only process exit may
	// deliver this EOF, because process exit is what the reader is waiting to
	// learn about.
	aliveR, aliveW, err := os.Pipe()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hold:", err)
		return 1
	}
	_ = aliveW // held open until this process exits; see above

	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), fakeLingerEnv+"=1", fakeLingerSayEnv+"="+sid)
	cmd.Stdout = os.Stdout
	cmd.ExtraFiles = []*os.File{aliveR}
	leaveProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "hold:", err)
		return 1
	}
	_ = aliveR.Close()
	if pidfile := os.Getenv("WAKE_FAKE_PIDFILE"); pidfile != "" {
		_ = os.WriteFile(pidfile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	}
	return 0
}

// fakeFlood emits a burst far larger than any client queue and then idles, so
// a client that stops reading is guaranteed to fall behind.
func fakeFlood(sid string) int {
	n, err := strconv.Atoi(os.Getenv(fakeCountEnv))
	if err != nil || n <= 0 {
		n = 1
	}
	w := bufio.NewWriterSize(os.Stdout, 256*1024)
	for i := range n {
		_, _ = fmt.Fprintf(w, `{"type":"assistant","session_id":%q,"message":{"role":"assistant","content":[{"type":"text","text":"burst %d"}]}}`+"\n", sid, i)
	}
	if err := w.Flush(); err != nil {
		return 1
	}
	// The marker that matters. It is emitted only if every frame above got
	// out, and after a lull so a client that fell behind during the burst has
	// caught up by the time it arrives. An agent frozen by backpressure never
	// reaches this line.
	time.Sleep(floodSettle)
	emitText(sid, "flood done")
	for range stdinLines() {
	}
	return 0
}

// floodSettle is the pause before the marker: long enough that the daemon has
// drained what it can, short enough not to dominate the test.
const floodSettle = 300 * time.Millisecond

func emitText(sid, text string) {
	fmt.Printf(`{"type":"assistant","session_id":%q,"message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`+"\n", sid, text)
}

// emitInit emits a system/init frame - the frame real claude sends before any
// input, which initFacts decodes to a SessionFacts and firstInit probes on.
func emitInit(sid string) {
	fmt.Printf(`{"type":"system","subtype":"init","session_id":%q,"model":"claude-opus-5","cwd":"/tmp/repo","permissionMode":"default"}`+"\n", sid)
}

// fakeModelProbe answers a bare /model with a "Current model: … (effort: X)"
// line the way 2.1.232 does, reporting whatever level the last /effort set, so
// the daemon's startup probe and its re-probe after /effort can both be seen to
// confirm the level end to end. Everything else it echoes like fakeTurns.
func fakeModelProbe(sid string) int {
	emitInit(sid)
	emitText(sid, "ready")
	emitResult(sid)
	effort, model := "max", "Opus 5 (1M context)"
	for line := range stdinLines() {
		switch {
		case strings.Contains(line, `"text":"/model"`):
			emitText(sid, "Current model: "+model+" (effort: "+effort+")")
			emitResult(sid)
		case strings.Contains(line, `"text":"/model `):
			// A runtime /model change: the next bare-/model probe reports it, the
			// way 2.1.232 renders the newly selected model back.
			model = argAskedIn(line, "/model ")
			emitText(sid, "echo: "+line)
			emitResult(sid)
		case strings.Contains(line, `"text":"/effort `):
			effort = argAskedIn(line, "/effort ")
			emitText(sid, "echo: "+line)
			emitResult(sid)
		default:
			emitText(sid, "echo: "+line)
			emitResult(sid)
		}
	}
	return 0
}

// argAskedIn pulls the argument out of a "text":"<cmd> <arg>" user line, for
// the fake to echo back what a runtime /effort or /model asked for.
func argAskedIn(line, cmd string) string {
	marker := `"text":"` + cmd
	i := strings.Index(line, marker)
	if i < 0 {
		return ""
	}
	rest := line[i+len(marker):]
	if j := strings.IndexByte(rest, '"'); j >= 0 {
		return strings.TrimSpace(rest[:j])
	}
	return ""
}

func emitResult(sid string) {
	fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"session_id":%q,"result":"done"}`+"\n", sid)
}

// stdinLines yields whole newline-delimited frames until stdin closes, which
// is the EOF that ends a real agent too.
func stdinLines() <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
		for sc.Scan() {
			out <- sc.Text()
		}
	}()
	return out
}

// argValue reads a flag's value out of the command line the daemon built.
// The session id is the one thing the fake has to agree with Wake about: it
// stamps every frame with it, so a daemon that routed by anything else would
// be caught here.
func argValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return ""
}

// fakeClaudeOnPath puts a `claude` on PATH that is this test binary, and
// selects which script it runs.
//
// The daemon calls core.NewSession directly and core resolves "claude" on
// PATH, so this is the only seam there is - and it is a better one than a
// function pointer, because what gets spawned is a real process.
func fakeClaudeOnPath(t *testing.T, script string) {
	t.Helper()

	shadowOnPath(t, "claude")
	t.Setenv(fakeClaudeEnv, "1")
	t.Setenv(fakeScriptEnv, script)
}

// brokenPsOnPath shadows ps(1) with one that cannot answer, in the named way.
//
// It shadows rather than removes on purpose: the dangerous case is not a
// missing ps - exec fails, which is already unknown - but a ps that *runs* and
// then fails to answer, which a reader of the exit status alone cannot tell
// from "there is no such process".
func brokenPsOnPath(t *testing.T, how string) {
	t.Helper()
	shadowOnPath(t, "ps")
	t.Setenv(fakePsEnv, how)
}

// shortProbeTimeout compresses how long one process lookup may take. Two
// seconds is the right production value and an impossible test.
func shortProbeTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := probeTimeout
	probeTimeout = d
	t.Cleanup(func() { probeTimeout = prev })
}

// shortFleetProbeBudget compresses the one deadline FleetOnDisk holds over its
// whole liveness sweep. statusTimeout is the right production value and an
// impossible test.
func shortFleetProbeBudget(t *testing.T, d time.Duration) {
	t.Helper()
	prev := fleetProbeBudget
	fleetProbeBudget = d
	t.Cleanup(func() { fleetProbeBudget = prev })
}

// shadowOnPath puts a symlink to this test binary at the front of PATH under
// the given name. TestMain dispatches on it.
func shadowOnPath(t *testing.T, name string) {
	t.Helper()

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locate test binary: %v", err)
	}
	dir := t.TempDir()
	if err := os.Symlink(exe, filepath.Join(dir, name)); err != nil {
		t.Fatalf("symlink fake %s: %v", name, err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
