//go:build unix

// No process outlives the fleet that started it (BUG-73, BUG-25).
//
// Claude starts a background shell in a process group of its own, so retire's
// sweep of the agent's group never reached it, and a park sweeps nothing at all
// - after which the daemon exits holding no handle to anything. These start a
// fake agent with a shell in a group of its own and ask the OS, after each way
// a fleet can end, whether any of it is still running.

package daemon

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// leftovers is what one fake agent left running: the shell in a group of its
// own, the shell's child in that group, and a child in the agent's own group.
type leftovers struct{ shell, shellChild, own int }

func (l leftovers) all() []int { return []int{l.shell, l.shellChild, l.own} }

// readLeftovers reads the three pids the fake recorded, waiting for each.
func readLeftovers(t *testing.T, pidfile string) leftovers {
	t.Helper()
	l := leftovers{
		shell:      waitForPid(t, pidfile),
		shellChild: waitForPid(t, pidfile+".gc"),
		own:        waitForPid(t, pidfile+".own"),
	}
	t.Cleanup(func() {
		for _, pid := range l.all() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	for _, pid := range l.all() {
		if !processAlive(pid) {
			t.Fatalf("pid %d the agent left behind was not running before the ending, so this test proves nothing", pid)
		}
	}
	if pgid, err := syscall.Getpgid(l.shell); err != nil || pgid != l.shell {
		t.Fatalf("the shell is in group %d (%v), not one of its own, so this test is not about a foreign group", pgid, err)
	}
	return l
}

func (l leftovers) wantGone(t *testing.T, what string) {
	t.Helper()
	for _, pid := range l.all() {
		deadline := time.Now().Add(testTimeout)
		for processAlive(pid) {
			if time.Now().After(deadline) {
				t.Fatalf("after %s, pid %d of what the agent started is still running", what, pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func (l leftovers) wantRunning(t *testing.T, what string) {
	t.Helper()
	for _, pid := range l.all() {
		if !processAlive(pid) {
			t.Errorf("%s: pid %d was ended, but this ending keeps what a woken session needs", what, pid)
		}
	}
}

// shellFleet starts a daemon with one shell-group agent and returns its pids.
func shellFleet(t *testing.T) (*testDaemon, *testClient, leftovers, string) {
	t.Helper()
	pidfile := filepath.Join(t.TempDir(), "pids")
	t.Setenv(fakePidfileEnv, pidfile)
	fakeClaudeOnPath(t, "shellgroup")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")
	return d, c, readLeftovers(t, pidfile), pidfile
}

// /quit is FrameStop: the agent exits on EOF and retire runs.
func TestAStopSweepsTheShellTheAgentStartedInAGroupOfItsOwn(t *testing.T) {
	_, c, left, _ := shellFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	left.wantGone(t, "stop")
}

func TestWakeStopLeavesNothingOfTheFleetRunning(t *testing.T) {
	d, c, left, _ := shellFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)

	left.wantGone(t, "wake stop")
}

// ⌃Q⌃Q parks the fleet: the processes it started are the daemon's last chance
// to end them, since the park book holds no pid.
func TestParkingTheFleetAndExitingLeavesNothingRunning(t *testing.T) {
	d, c, left, _ := shellFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	d.waitForExit(t)

	left.wantGone(t, "⌃Q⌃Q")
}

func TestASignalExitLeavesNothingRunning(t *testing.T) {
	d, _, left, _ := shellFleet(t)

	d.stop(t)

	left.wantGone(t, "a signal")
}

// A ⌃C park in a daemon that lives on keeps the world the session will wake
// into, and ending the fleet afterwards removes it.
func TestAParkInALiveDaemonKeepsItsProcessesUntilTheFleetEnds(t *testing.T) {
	d, c, left, _ := shellFleet(t)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	left.wantRunning(t, "a ⌃C park")

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)

	left.wantGone(t, "wake stop after the park")
}

// What an earlier generation left is the woken session's too: ending the woken
// one ends it, and it is not forgotten when the replacing row takes over.
func TestAWokenSessionsStopAlsoEndsWhatItsEarlierLifeLeft(t *testing.T) {
	_, c, before, pidfile := shellFleet(t)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	for _, suffix := range []string{"", ".gc", ".own"} {
		_ = os.Remove(pidfile + suffix)
	}
	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: idAlpha})
	after := readLeftovers(t, pidfile)
	before.wantRunning(t, "a wake")

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	before.wantGone(t, "the woken session's stop, for its first life")
	after.wantGone(t, "the woken session's stop")
}

// A listing that cannot be read at the ending is a leak, never a kill: with the
// fleet already noted, the shell is known and still is not signalled, since
// nothing proves it is the fleet's now.
func TestAListingThatFailsAtTheEndingSignalsNothingItHadNoted(t *testing.T) {
	t.Setenv(fakeTaskEnv, "1")
	logs := captureLog(t)
	d, c, left, _ := shellFleet(t)
	logs.waitFor(t, "process group") // noted at the shell's start, while ps still answers
	brokenPsOnPath(t, psRefuses)

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)

	if !processAlive(left.shell) || !processAlive(left.shellChild) {
		t.Error("the shell was signalled with no listing to prove it was the fleet's")
	}
}

// A background shell that exits leaves its child orphaned under a group nobody
// can find from the agent. It is swept only because the shell's start was
// noted while the shell was alive.
func TestAShellThatExitedBeforeTheEndingIsStillSwept(t *testing.T) {
	release := filepath.Join(t.TempDir(), "release")
	t.Setenv(fakeReleaseEnv, release)
	t.Setenv(fakeTaskEnv, "1")
	logs := captureLog(t)
	_, c, left, _ := shellFleet(t)
	logs.waitFor(t, "process group")

	if err := os.WriteFile(release, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitForProcessGone(t, left.shell)
	if !processAlive(left.shellChild) {
		t.Fatal("the shell's child died with the shell, so there is no escapee to find")
	}

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	waitForProcessGone(t, left.shellChild)
	waitForProcessGone(t, left.own)
}

// logBuffer is the daemon's log as a test reads it.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLog(t *testing.T) *logBuffer {
	t.Helper()
	b := &logBuffer{}
	log.SetOutput(b)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return b
}

func (b *logBuffer) waitFor(t *testing.T, sub string) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !strings.Contains(b.String(), sub) {
		if time.Now().After(deadline) {
			t.Fatalf("the daemon never logged %q; it said:\n%s", sub, b.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A listing that does not hold this daemon is not a listing of this machine,
// and a half of one must not be read as "nothing is running".
func TestAListingThatIsNotThisMachinesIsRefused(t *testing.T) {
	for _, how := range []string{psPartial, psQuiet, psRefuses, psElsewhere} {
		brokenPsOnPath(t, how)
		if rows, err := listProcesses(); err == nil {
			t.Errorf("a ps that %s gave %d rows and no error", how, len(rows))
		}
	}
}

// The case recycling makes: the number noted for a group now leads an
// unrelated one. Nothing noted in it is that process, so nothing is signalled -
// and the same group is ended once a noted member really is running in it.
func TestAGroupWhoseNumberNowLeadsSomethingElseIsNotSignalled(t *testing.T) {
	other := lingerInOwnGroup(t)
	rows, err := listProcesses()
	if err != nil {
		t.Fatal(err)
	}
	var start string
	for _, r := range rows {
		if r.pid == other {
			start = r.start
		}
	}
	if start == "" {
		t.Fatalf("pid %d is not in the listing", other)
	}
	s := &server{}

	s.sweepGroups([]procGroup{{pgid: other, members: []procRef{{other, "Mon Jan  1 00:00:00 2001"}}}})
	time.Sleep(100 * time.Millisecond)
	if !processAlive(other) {
		t.Fatal("a group was signalled on the strength of a process that was not the one noted in it")
	}

	s.sweepGroups([]procGroup{{pgid: other, members: []procRef{{other, start}}}})
	waitForProcessGone(t, other)
}

// lingerInOwnGroup starts a process that leads a group of its own and reaps it.
func lingerInOwnGroup(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), fakeLingerEnv+"=1")
	leaveProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = syscall.Kill(cmd.Process.Pid, syscall.SIGKILL) })
	return cmd.Process.Pid
}

// holdFleet is shellFleet with an agent that behaves as claude does: it reports
// the shell's start, and once stdin closes it stays until the shell has ended.
func holdFleet(t *testing.T) (*testDaemon, *testClient, leftovers) {
	t.Helper()
	t.Setenv(fakeTaskEnv, "1")
	t.Setenv(fakeHoldEnv, "1")
	d, c, left, _ := shellFleet(t)
	c.await("the shell's start", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil && f.Event.Task != nil
	})
	return d, c, left
}

// Claude does not exit on EOF while a background shell runs, so a stop never
// ended such an agent: the row sat idle and the shell outlived /quit (BUG-73).
func TestAStopEndsAnAgentItsShellWouldHoldOpen(t *testing.T) {
	_, c, left := holdFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	left.wantGone(t, "/quit")
}

// A hand park of the same agent completes, and still keeps the shell it will
// wake into; ending the fleet afterwards removes it.
func TestAParkEndsAnAgentItsShellWouldHoldOpenAndKeepsTheShell(t *testing.T) {
	d, c, left := holdFleet(t)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	// What the agent was held open by stays for the wake. Ending claude takes its
	// own group with it, as every kill of it does.
	for _, pid := range []int{left.shell, left.shellChild} {
		if !processAlive(pid) {
			t.Errorf("a ⌃C park ended pid %d, the shell the woken session needs", pid)
		}
	}

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)
	left.wantGone(t, "wake stop after the park")
}

// The same when the stop lands mid-turn: the turn finishes, and it is its end
// that finds the agent held open.
func TestAStopMidTurnEndsAnAgentItsShellWouldHoldOpenOnceTheTurnIsDone(t *testing.T) {
	t.Setenv(fakeSlowEnv, "600ms")
	_, c, left := holdFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "work"})
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.awaitEvent(idAlpha, `echo: {`) // the turn the stop landed in, which the fake now only answers to a message
	c.pollState(idAlpha, rpc.StateEnded)

	left.wantGone(t, "a stop mid-turn")
}

// Only a shell holds claude open: a stopped agent whose turn ended and which has
// none is left to exit on its own, as before.
func TestAStopOfAnAgentWithNoShellIsLeftToExitItself(t *testing.T) {
	logs := captureLog(t)
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if got := logs.String(); strings.Contains(got, "held open") {
		t.Errorf("an agent with no shell was ended for being held open: %s", got)
	}
}

// `wake stop` of such an agent used to wait out the whole grace and then kill it.
func TestWakeStopDoesNotWaitOutTheGraceForAnAgentItsShellHoldsOpen(t *testing.T) {
	logs := captureLog(t)
	d, c, left := holdFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)

	left.wantGone(t, "wake stop")
	if got := logs.String(); strings.Contains(got, "did not end within") {
		t.Errorf("the stop waited for an agent its own shell was holding open, and then killed it:\n%s", got)
	}
}

// A shell running is an agent's ordinary state, not a reason to end it: only a
// stop makes it one.
func TestAnAgentThatWasNotStoppedIsNotEndedWhenATurnEndsWithAShellRunning(t *testing.T) {
	_, c, left := holdFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "work"})
	c.awaitEvent(idAlpha, "echo: ")
	time.Sleep(300 * time.Millisecond) // a wrongly ended agent's group is killed within this

	if row := sessionRow(c.status(), idAlpha); row.State == rpc.StateEnded {
		t.Errorf("the agent was ended at a turn's end with nobody having stopped it: %+v", row)
	}
	left.wantRunning(t, "a turn's end")
}

// Claude opens a turn of its own when a background task finishes. A hand park
// that lands in one lets it finish, as a park does any turn: the agent is not
// between turns for owing nothing.
func TestAParkInATurnTheAgentStartedItselfLetsTheTurnFinish(t *testing.T) {
	t.Setenv(fakeSelfTurnEnv, "700ms")
	_, c, left := holdFleet(t)
	c.awaitEvent(idAlpha, "tick")

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitEvent(idAlpha, "tock") // the turn's last words: a kill mid-turn never lets them out
	c.awaitState(idAlpha, rpc.StateParked)

	for _, pid := range []int{left.shell, left.shellChild} {
		if !processAlive(pid) {
			t.Errorf("a ⌃C park ended pid %d, the shell the woken session needs", pid)
		}
	}
}

// A message typed before the stop is the stop's to finish: it is queued behind
// the turn in flight, and that turn's end is not the end of what was given.
func TestAMessageQueuedBeforeTheStopIsAnsweredEvenWithAShellRunning(t *testing.T) {
	t.Setenv(fakeSlowEnv, "600ms")
	_, c, left := holdFleet(t)

	for _, text := range []string{"first", "second"} {
		c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: text, MessageID: uuid.NewString()})
	}
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.awaitEvent(idAlpha, "second")
	c.pollState(idAlpha, rpc.StateEnded)

	left.wantGone(t, "a stop after two messages")
}

// A shell that starts after the stop's listing, in the turn the stop lets run,
// is noted before claude is ended for it, or nothing would ever reach it.
func TestAShellStartedAfterTheStopsListingIsSweptWhenItsTurnEndsTheAgent(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	t.Setenv(fakePidfileEnv, pidfile)
	t.Setenv(fakeTaskEnv, "1")
	t.Setenv(fakeHoldEnv, "1")
	t.Setenv(fakeLateEnv, "1")
	t.Setenv(fakeSlowEnv, "400ms")
	fakeClaudeOnPath(t, "shellgroup")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "work"})
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha}) // lists before the shell exists
	c.awaitEvent(idAlpha, `echo: {`)
	c.pollState(idAlpha, rpc.StateEnded)
	d.stop(t)

	left := leftovers{shell: waitForPid(t, pidfile), shellChild: waitForPid(t, pidfile+".gc"), own: waitForPid(t, pidfile+".own")}
	t.Cleanup(func() {
		for _, pid := range left.all() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	left.wantGone(t, "a stop whose turn started a shell")
}

// ⌃Q interrupts before it closes stdin, which is what lets claude go while its
// shell runs: the agent parks inside the grace, is booked, and the daemon's exit
// is what ends the shell.
func TestParkingTheFleetParksAnAgentItsShellHoldsOpenAndSweepsTheShell(t *testing.T) {
	logs := captureLog(t)
	d, c, left := holdFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	d.waitForExit(t)

	left.wantGone(t, "⌃Q⌃Q")
	if booked := loadParkBook(parkBookPath(d.socket)); len(booked) != 1 || booked[0].ID != idAlpha {
		t.Errorf("the park book holds %+v, want the one agent ⌃Q parked", booked)
	}
	if got := logs.String(); strings.Contains(got, "did not end within") {
		t.Errorf("an agent ⌃Q interrupted still ran out the grace:\n%s", got)
	}
}

func TestASignalParksAnAgentItsShellHoldsOpenAndSweepsTheShell(t *testing.T) {
	d, _, left := holdFleet(t)

	d.stop(t)

	left.wantGone(t, "a signal")
	if booked := loadParkBook(parkBookPath(d.socket)); len(booked) != 1 || booked[0].ID != idAlpha {
		t.Errorf("the park book holds %+v, want the one agent the signal parked", booked)
	}
}

// What a session's first life left is held only in the ledger once it wakes, and
// the daemon's exit still reaches it.
func TestAWokenSessionsFirstLifeIsSweptWhenTheDaemonExits(t *testing.T) {
	d, c, before, pidfile := shellFleet(t)

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)
	for _, suffix := range []string{"", ".gc", ".own"} {
		_ = os.Remove(pidfile + suffix)
	}
	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: idAlpha})
	after := readLeftovers(t, pidfile)

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	d.waitForExit(t)

	before.wantGone(t, "⌃Q⌃Q, for the first life")
	after.wantGone(t, "⌃Q⌃Q")
}

// A daemon whose last client left and whose agents are all parked exits by
// itself, and that exit sweeps too: nothing is left to keep what a park kept.
func TestAnEmptyDaemonsExitSweepsWhatItsParkedAgentsLeft(t *testing.T) {
	d, c, left, _ := shellFleet(t)
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.awaitState(idAlpha, rpc.StateParked)

	c.close()
	d.waitForExit(t)

	left.wantGone(t, "an empty daemon's exit")
}
