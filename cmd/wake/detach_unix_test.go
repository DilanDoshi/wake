//go:build unix

// The property the whole architecture exists to provide, asserted against
// processes rather than against the daemon's own opinion of them.
//
// Everything else in this package starts the daemon with daemon.Serve in this
// process, which is right for the ordering those tests assert. It also means
// none of them exercise daemon.fork - the detached child with its own session,
// no controlling terminal and /dev/null stdio - which is the only path a real
// user takes. These do, through daemon.EnsureRunning, with TestMain standing in
// as the daemon binary.

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// fakeClaude stands in for the agent. It emits one stream-json line so the
// spawn is observable end to end, then blocks on stdin.
//
// Blocking on a shell builtin rather than sleeping is deliberate three times
// over: the script stays a single process, so killing it closes stdout and the
// session ends promptly instead of parking core's pump behind a grandchild that
// still holds the pipe; its argv keeps the whole command line, including the
// session UUID that verifyAgent matches on, which an exec would have replaced;
// and `wake stop` closes stdin, so the gentle ending path is the one under test
// rather than a kill.
const fakeClaude = `#!/bin/sh
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"fake agent ready"}]},"session_id":"fake"}'
read _ignored
`

// withFakeClaude puts a fake agent first on PATH, for this process and for
// anything it forks. core resolves the binary by name, so this is the whole of
// the substitution - and CLAUDE.md's rule against testing on a live model
// makes it mandatory rather than convenient.
func withFakeClaude(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "claude")
	if err := os.WriteFile(path, []byte(fakeClaude), 0o755); err != nil {
		t.Fatalf("write fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// agentPID finds the process this session is running as, the way the reaper
// does: a process-group leader whose argv carries the session UUID.
//
// It is a UUID and not a command name on purpose. `claude` resolves through a
// wrapper on some machines that rewrites the argv, so a name match reports "not
// running" for an agent that is running perfectly well - which is exactly the
// mistake that produced two withdrawn Criticals against this task. The UUID
// exists nowhere else on the machine.
func agentPID(t *testing.T, sessionID string) (int, bool) {
	t.Helper()

	out, err := exec.Command("ps", "-axww", "-o", "pid=,pgid=,command=").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, sessionID) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pid, perr := strconv.Atoi(fields[0])
		pgid, gerr := strconv.Atoi(fields[1])
		if perr != nil || gerr != nil {
			continue
		}
		if pid != pgid {
			// Not the group leader. The daemon records the group, and a
			// descendant carrying the same argv is not the thing being asked
			// about.
			continue
		}
		return pid, true
	}
	return 0, false
}

// awaitAgent waits for the session's process to appear.
func awaitAgent(t *testing.T, sessionID string) int {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if pid, ok := agentPID(t, sessionID); ok {
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no process-group leader carrying session %s ever appeared", sessionID)
	return 0
}

// alive reports whether a pid exists. Signal 0 checks for the process without
// disturbing it.
func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func awaitGone(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !alive(pid)
}

// forkedFleet is a daemon started the way a user starts one, with one session
// on it.
type forkedFleet struct {
	socket    string
	sessionID string
	pid       int

	conn   net.Conn
	stream ui.Stream
	once   sync.Once
}

// detach drops the client connection, which is the whole of a detach as far as
// the daemon is concerned - there is no frame for it. Draining afterwards is
// not optional: rpc.ReadFrames has no cancellation.
func (f *forkedFleet) detach() {
	f.once.Do(func() {
		_ = f.conn.Close()
		for range f.stream.Frames {
		}
		<-f.stream.Errs
	})
}

// startForkedFleet runs the whole client path: EnsureRunning forks a detached
// daemon, connect completes the handshake, and a spawn frame starts an agent.
func startForkedFleet(t *testing.T) *forkedFleet {
	t.Helper()

	socket := tempSocket(t)
	t.Setenv(daemon.SocketEnv, socket)
	withFakeClaude(t)

	f := &forkedFleet{socket: socket}

	// Registered before a daemon can exist, and that ordering is the point.
	// Every call below can t.Fatal, and the daemon this starts is setsid'd -
	// so a failure with the cleanup registered later leaves a detached daemon
	// running and then tempSocket's RemoveAll deletes its socket, roster and
	// lock out from under it, leaving it unreachable by any wake command for
	// the rest of the machine's uptime. These are also the tests most likely
	// to fail on a loaded CI box.
	//
	// It runs before tempSocket's own cleanup because t.Cleanup is LIFO, which
	// is the order this needs: stop the daemon, then delete its directory.
	t.Cleanup(func() {
		_ = stopFleet(socket, io.Discard)
		if f.pid > 0 && alive(f.pid) {
			_ = syscall.Kill(-f.pid, syscall.SIGKILL)
		}
	})

	conn, stream, err := connect(socket, io.Discard)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	f.conn, f.stream = conn, stream
	t.Cleanup(f.detach)

	sessionID := uuid.NewString()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := rpc.WriteFrameTo(conn, rpc.Frame{
		Kind: rpc.FrameSpawn, SessionID: sessionID, Text: "alex", Dir: dir,
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	awaitOwnEvent(t, stream, sessionID)

	f.sessionID = sessionID
	f.pid = awaitAgent(t, sessionID)
	return f
}

// awaitOwnEvent waits for an event from this session, which is the spawn
// proving itself all the way from the fake agent's stdout to this client.
func awaitOwnEvent(t *testing.T, stream ui.Stream, sessionID string) {
	t.Helper()

	deadline := time.After(testTimeout)
	for {
		select {
		case f, ok := <-stream.Frames:
			if !ok {
				t.Fatal("the daemon hung up before the session produced anything")
			}
			if f.Kind == rpc.FrameError {
				t.Fatalf("the daemon refused the spawn: %s", f.Text)
			}
			if f.Kind == rpc.FrameEvent && f.SessionID == sessionID {
				return
			}
		case err := <-stream.Errs:
			t.Fatalf("reading from the daemon: %v", err)
		case <-deadline:
			t.Fatalf("session %s produced no event within %v", sessionID, testTimeout)
		}
	}
}

// The whole product in one test. A TUI that exits must not take its agent with
// it, and `wake stop` must be the thing that does.
func TestAnAgentOutlivesItsClientAndDiesWithWakeStop(t *testing.T) {
	f := startForkedFleet(t)

	// (ii) The client goes away, which is all a detach is on the wire.
	f.detach()

	// Bounded settle, so this is "still alive afterwards" rather than "alive
	// at the same instant".
	time.Sleep(detachSettle)
	if !alive(f.pid) {
		t.Fatalf("agent %d died with its client; the daemon exists to stop exactly this", f.pid)
	}

	// (iii) And it is not immortal.
	if err := stopFleet(f.socket, io.Discard); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}
	if !awaitGone(f.pid, testTimeout) {
		t.Errorf("agent %d survived wake stop", f.pid)
	}
}

// detachSettle is how long a detached agent is given to die before it is
// declared to have survived. Long enough that a session torn down by the
// client's disconnect has finished dying.
const detachSettle = 750 * time.Millisecond

// The negative control, and without it the test above is a decoration: "the
// pid is alive" also passes against a daemon that never looks at its agents at
// all. Kill the agent out from under the daemon and it must notice.
func TestTheDaemonNoticesAnAgentKilledOutFromUnderIt(t *testing.T) {
	f := startForkedFleet(t)

	// The positive half first. Waiting for a count to reach zero is satisfied
	// instantly by a count that was never anything else - the same shape as a
	// detector whose negative answer has never been seen turn positive, which
	// is what produced two withdrawn Criticals against this task.
	before, err := daemon.Status(f.socket)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := runningCount(before); got != 1 {
		t.Fatalf("the daemon reports %d running sessions before the kill, want 1; this test would pass without observing anything", got)
	}

	if err := syscall.Kill(f.pid, syscall.SIGKILL); err != nil {
		t.Fatalf("kill agent %d: %v", f.pid, err)
	}

	if err := awaitNoRunningSessions(f.socket, testTimeout); err != nil {
		t.Errorf("after killing session %s: %v", f.sessionID, err)
	}
}

// awaitNoRunningSessions waits for the daemon to report nothing running, and
// insists it is still answering the whole time.
//
// The liveness check is not decoration. daemon.Status falls back to the on-disk
// roster when the dial fails, and that answer has no live sessions in it either
// - so "the count reached zero" is also exactly what a daemon that *died*
// during the wait produces, and the negative control would pass while having
// observed nothing. Extracted from that test so this predicate has a test of
// its own; see TestAwaitNoRunningSessionsRejectsADaemonThatStoppedAnswering.
func awaitNoRunningSessions(socket string, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		st, err := daemon.Status(socket)
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}
		if !st.Running {
			return errors.New("the daemon stopped answering while it was being watched, so its report proves nothing")
		}
		if runningCount(st) == 0 {
			return nil
		}
		time.Sleep(statusPoll)
	}
	return fmt.Errorf("the daemon still reported a running session after %v", within)
}

const statusPoll = 50 * time.Millisecond

// The false-pass the guard above exists to close: a socket with no daemon
// behind it reports zero running sessions, which is the same answer as a
// daemon that noticed its agent die.
func TestAwaitNoRunningSessionsRejectsADaemonThatStoppedAnswering(t *testing.T) {
	socket := tempSocket(t)

	// Cheap rather than the full timeout: there is no daemon, so the first
	// look already has the answer.
	err := awaitNoRunningSessions(socket, statusPoll)
	if err == nil {
		t.Fatal("a socket with no daemon behind it was accepted as a daemon reporting nothing running")
	}
	if !strings.Contains(err.Error(), "stopped answering") {
		t.Errorf("awaitNoRunningSessions failed with %q, want it to name the missing daemon", err)
	}
}

// --- an abnormally dead daemon --------------------------------------------

// startBlockedProcess runs a process-group leader that carries an id in its
// argv and does nothing until its stdin closes. It is what a roster entry has
// to point at for daemon.Status to call the session alive: verifyAgent wants a
// group leader whose command line contains the session UUID.
func startBlockedProcess(t *testing.T, id string) int {
	t.Helper()

	cmd := exec.Command("/bin/sh", "-c", "read _ignored", id)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start blocked process: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// writeRoster leaves behind what a daemon that died without cleaning up would
// have left: the session ids on disk, which is the whole reason the file
// exists.
func writeRoster(t *testing.T, socket, sessionID string, pid int) {
	t.Helper()

	body := fmt.Sprintf(`[{"id":%q,"name":"alex","pid":%d,"started":%q}]`,
		sessionID, pid, time.Now().Format(time.RFC3339Nano))
	path := filepath.Join(filepath.Dir(socket), "sessions.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write roster: %v", err)
	}
}

// The EOF `wake stop` waits for has two producers, and only one of them is
// evidence. This is the other: a daemon that unlinked its socket and closed its
// clients on the way out of a panic, without stopping anything - Serve calls
// shutdown() in its return expression rather than a defer, so a panic skips it
// while the deferred ln.Close still runs.
//
// Saying "the fleet is down" here is the one thing this command must never do.
func TestStopDoesNotClaimTheFleetIsDownWhenTheDaemonDiedWithoutStoppingAnything(t *testing.T) {
	sessionID := uuid.NewString()
	pid := startBlockedProcess(t, sessionID)

	d := startFakeDaemon(t, quitDelay, rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: sessionID, Name: "alex", State: rpc.StateWorking}},
	})
	writeRoster(t, d.socket, sessionID, pid)

	var out bytes.Buffer
	err := stopFleet(d.socket, &out)

	if !alive(pid) {
		t.Fatal("the survivor died on its own, so this test observed nothing")
	}
	if err == nil {
		t.Fatalf("stopFleet reported success with %d still running: %q", pid, out.String())
	}
	if strings.Contains(out.String(), "fleet is down") {
		t.Errorf("stopFleet claimed the fleet was down: %q", out.String())
	}
	if !strings.Contains(err.Error(), "still running") {
		t.Errorf("stopFleet failed with %q, want it to name what survived", err)
	}
	// This daemon unlinked its socket on the way out, which is what unwinding
	// through a deferred Close looks like. Blaming a socket that went is the
	// other way this diagnostic can be wrong.
	if strings.Contains(err.Error(), "never unlinked") {
		t.Errorf("stopFleet blamed a socket that was unlinked: %q", err)
	}
}

// And the other death. A SIGKILLed daemon has its listening fd closed by the
// kernel and unlinks nothing, so the socket file outlives it - which is the one
// fact separating that from a panic, and the only thing awaitSocketRelease's
// answer is good for.
//
// The wire from that answer to the message had no test: every other daemon in
// this suite unlinks its socket, so `released` was true on every path the suite
// ran and hardcoding it either way left everything green. That is the shape
// this round set out to eliminate, in the round's own fix.
func TestStopNamesASocketTheDaemonNeverUnlinked(t *testing.T) {
	shortRelease(t, 200*time.Millisecond)

	sessionID := uuid.NewString()
	pid := startBlockedProcess(t, sessionID)

	d := startFakeDaemon(t, quitDelay, rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: sessionID, Name: "alex", State: rpc.StateWorking}},
	})
	d.keepSocketFile = true
	writeRoster(t, d.socket, sessionID, pid)

	var out bytes.Buffer
	err := stopFleet(d.socket, &out)

	if err == nil {
		t.Fatalf("stopFleet reported success with %d still running: %q", pid, out.String())
	}
	if !strings.Contains(err.Error(), "never unlinked its socket") {
		t.Errorf("stopFleet failed with %q, want it to name the socket that outlived the daemon", err)
	}
	if strings.Contains(out.String(), "fleet is down") {
		t.Errorf("stopFleet claimed the fleet was down: %q", out.String())
	}
	// The socket really did survive, or this observed nothing.
	if _, serr := os.Stat(d.socket); serr != nil {
		t.Errorf("the socket was removed after all, so this test proves nothing: %v", serr)
	}
}

// And the same connection ending with the roster empty is still success, or
// the fix would have turned every ordinary stop into a failure.
func TestStopStillConfirmsAFleetThatReallyIsDown(t *testing.T) {
	sessionID := uuid.NewString()
	pid := startBlockedProcess(t, sessionID)

	d := startFakeDaemon(t, quitDelay, rpc.Status{Running: true})
	// A roster naming a process that is no longer that session: the shape a
	// daemon leaves when it stopped its fleet but the file outlived it.
	writeRoster(t, d.socket, uuid.NewString(), pid)

	var out bytes.Buffer
	if err := stopFleet(d.socket, &out); err != nil {
		t.Fatalf("stopFleet: %v", err)
	}
	if !strings.Contains(out.String(), "fleet is down") {
		t.Errorf("stopFleet did not confirm a fleet that was down: %q", out.String())
	}
}

// `wake stop` over a fleet whose daemon died has done nothing about it, and it
// has positively established that: the roster it reports from is already
// filtered by which processes exist. Exiting zero there tells a script the
// fleet is down while agents are running, and `wake stop && rm -rf worktrees`
// is what that costs.
//
// The branch had no test at all - only the genuinely-empty case did, and that
// counter-case stays where it was: TestStopWithNoDaemonSaysSo asserts the same
// no-daemon socket exits zero, which is what makes this test a distinction
// rather than a blanket rule. It lives in the untagged file, so it runs on
// platforms this one does not.
func TestStopOverLiveOrphansFailsRatherThanReportingANoOp(t *testing.T) {
	socket := tempSocket(t)
	sessionID := uuid.NewString()
	pid := startBlockedProcess(t, sessionID)
	writeRoster(t, socket, sessionID, pid)

	var out bytes.Buffer
	err := stopFleet(socket, &out)

	if !alive(pid) {
		t.Fatal("the orphan died on its own, so this test observed nothing")
	}
	if err == nil {
		t.Fatalf("wake stop exited 0 with %d still running: %q", pid, out.String())
	}
	// The message was already the right message; only the return was wrong.
	if !strings.Contains(out.String(), "left 1 agent behind") {
		t.Errorf("wake stop no longer explains what it found: %q", out.String())
	}
	if !strings.Contains(err.Error(), "not down") {
		t.Errorf("wake stop failed with %q, want it to say the fleet is not down", err)
	}
}
