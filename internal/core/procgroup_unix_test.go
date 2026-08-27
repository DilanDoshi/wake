//go:build unix

// What happens to the processes an agent spawned when the agent ends. These
// tests are Unix-only because the observation is: they ask the OS whether a
// specific pid is still alive, which is what "did Wake actually reap it" means
// and the only thing that would have caught the orphan accumulating.

package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Cancelling the context is Wake's only hard kill, and it has to reach the
// whole tree. Killing the direct child leaves whatever it spawned running with
// nobody left holding a handle to it - Wake's goroutines are recovered, the OS
// processes are not, and at 15-30 sessions those accumulate until the machine
// is full of claude processes nobody can name.
func TestCancellingKillsWhatTheAgentSpawnedNotJustTheAgent(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "orphan.pid")
	t.Setenv("WAKE_HELPER_SCRIPT", "idle")
	t.Setenv("WAKE_HELPER_PIDFILE", pidPath)
	withFakeExec(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := waitForKind(t, s, KindAssistantText); got != "pong 0" {
		t.Fatalf("assistant text = %q, want pong 0", got)
	}

	orphan := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(orphan, syscall.SIGKILL) })
	if !processAlive(orphan) {
		t.Fatal("the process the agent spawned was not running before cancel, so this test proves nothing")
	}

	cancel()

	select {
	case <-drainedAsync(s):
	case <-time.After(waitDelay + 10*time.Second):
		t.Fatal("events channel never closed after cancel")
	}
	waitForProcessGone(t, orphan)
}

// cmd.Cancel is not belt-and-braces: there is a whole steady state where it is
// the only thing cancellation reaches.
//
// Once the scan ends at EOF, pump's kill branch is skipped because scanErr is
// nil, and closeOnCancel has retired on scanDone. If the process is still
// alive - it closed stdout but has not exited - finish is parked in cmd.Wait
// and every other mechanism in this file has already stood down. Without the
// cmd.Cancel override, os/exec's default reaps the agent alone and orphans its
// whole tree.
//
// Reaching that state is deterministic, not a race: the helper closes its own
// stdout and then sleeps, so EOF is guaranteed to precede the cancel.
func TestCancellingReapsTheTreeOfAnAgentThatWentQuietWithoutExiting(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "orphan.pid")
	t.Setenv("WAKE_HELPER_SCRIPT", "eof-then-idle")
	t.Setenv("WAKE_HELPER_PIDFILE", pidPath)
	withFakeExec(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := waitForKind(t, s, KindAssistantText); got != "pong 0" {
		t.Fatalf("assistant text = %q, want pong 0", got)
	}
	orphan := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(orphan, syscall.SIGKILL) })

	// The stream is over but the session is not: finish is inside cmd.Wait on
	// a process that is still running. If the channel closes here the process
	// exited, and the test is not in the state it is about.
	done := drainedAsync(s)
	select {
	case <-done:
		t.Fatal("the session ended on its own - the agent did not stay alive past EOF, so this test proves nothing")
	case <-time.After(500 * time.Millisecond):
	}

	cancel()

	select {
	case <-done:
	case <-time.After(waitDelay + 10*time.Second):
		t.Fatal("events channel never closed after cancel")
	}
	waitForProcessGone(t, orphan)
}

// The pump's own kill path, which no cancellation reaches: a line past
// maxLineBytes ends the scan with the process alive and still writing into a
// pipe nobody is draining. Wake kills it there, or the Wait in finish never
// returns and the session holds a live-cap slot for good.
//
// That kill has to be the group's. The agent's children inherited this stdout,
// so killing the agent alone leaves the pipe open and the Wait it was meant to
// unblock still blocked - the same shape as the orphan cases, arrived at
// without anyone cancelling anything.
//
// The oversized line is generated at runtime rather than committed: 16MB is
// not a fixture worth keeping, and this branch was defensive and unguarded
// until it acquired a second job.
func TestAnOversizedLineEndsTheSessionAndReapsWhatItSpawned(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "orphan.pid")
	t.Setenv("WAKE_HELPER_SCRIPT", "bigline")
	t.Setenv("WAKE_HELPER_BIGLINE", strconv.Itoa(maxLineBytes+1))
	t.Setenv("WAKE_HELPER_PIDFILE", pidPath)
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	// Written by the agent right after the fork, so it is on disk before the
	// first byte of the line - reading it cannot race the kill.
	orphan := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(orphan, syscall.SIGKILL) })

	select {
	case <-drainedAsync(s):
	case <-time.After(waitDelay + 10*time.Second):
		t.Fatal("events channel never closed: the scan gave up and nothing ended the process it stopped reading")
	}
	err := s.Err()
	if err == nil {
		t.Fatal("Err = nil after the scan gave up on an oversized line")
	}
	if !strings.Contains(err.Error(), "stopped reading stdout") {
		t.Errorf("Err = %v, want the reason the stream ended", err)
	}
	// The scan reason outranks the rest but must not replace it. This branch
	// is every cancel and every oversized line - the wedged endings, where
	// what the agent last said on stderr is the best clue there is.
	if !strings.Contains(err.Error(), mcpStartupComplaint) {
		t.Errorf("Err = %v, want the stderr the agent wrote before the stream died", err)
	}
	// Deliberately not asserted here: whether the error also carries an exit
	// status. Which one arrives is a genuine race - the agent can finish
	// writing and exit 0 before the kill lands, in which case there is no
	// status to report and scanStopError correctly omits the clause. That
	// shape is pinned in TestScanStopErrorKeepsWhatTheProcessSaidAsWell, where
	// it can be pinned without a stopwatch.
	waitForProcessGone(t, orphan)
}

// The other side of the same rule, and the one that must not be "fixed" into
// symmetry with it. The spec is explicit: stop closes stdin and lets the
// in-flight turn finish, because an agent killed mid-Edit leaves a half-written
// file and Wake is not entitled to do that to someone's repo. A group kill on
// this path would take the agent's children with it too.
//
// This guard passes against the code as it was before the group kill existed -
// it is not evidence for that change, it is what keeps the change from
// spreading onto the one path the spec forbids it on.
func TestStopEndsTheAgentWithoutKillingWhatItSpawned(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "orphan.pid")
	t.Setenv("WAKE_HELPER_SCRIPT", "idle")
	t.Setenv("WAKE_HELPER_PIDFILE", pidPath)
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	if got := waitForKind(t, s, KindAssistantText); got != "pong 0" {
		t.Fatalf("assistant text = %q, want pong 0", got)
	}
	orphan := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(orphan, syscall.SIGKILL) })

	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	select {
	case <-drainedAsync(s):
	case <-time.After(10 * time.Second):
		t.Fatal("events channel never closed after Stop")
	}
	if err := s.Err(); err != nil {
		t.Errorf("Err = %v, want nil - closing stdin is how a session ends cleanly", err)
	}
	if !processAlive(orphan) {
		t.Error("Stop killed a process the agent spawned: stop is the gentle ending, and a hard kill belongs only on the cancel path")
	}
}

// A child Wake never gave a group to is sitting in Wake's own, and a broadcast
// there would take down Wake, the terminal job it runs in and every other
// session at once. So killProcessGroup signals only a group it created,
// identified by the pid it recorded at spawn, and anything else is killed
// alone. The bystander is the proof: it is in that same group and has to still
// be there afterwards.
func TestKillProcessGroupWillNotSignalTheGroupWakeIsIn(t *testing.T) {
	victim := startLingererInWakesGroup(t)
	bystander := startLingererInWakesGroup(t)

	if err := killProcessGroup(victim); err != nil {
		t.Fatalf("killProcessGroup: %v", err)
	}
	if err := victim.Wait(); err == nil {
		t.Error("the child was not killed at all")
	}
	if !processAlive(bystander.Process.Pid) {
		t.Error("killProcessGroup signalled the whole group its child was in, and that group is Wake's own")
	}
}

// Setpgid on its own does not mean "leads a group". With a non-zero Pgid it
// means "join group N", and then the child's pid is not its group id at all -
// so -pid names some other group, or none. Wake never sets Pgid, but
// SysProcAttr arrives from whatever execCommand returned, and that is a
// mutable package-level seam.
//
// The victim here really is in Wake's group; only the struct claims otherwise.
// That is the point: this function trusts the struct, so the struct is what it
// has to be careful about.
func TestKillProcessGroupIgnoresAChildToldToJoinAnotherGroup(t *testing.T) {
	victim := startLingererInWakesGroup(t)
	bystander := startLingererInWakesGroup(t)
	victim.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: bystander.Process.Pid}

	if err := killProcessGroup(victim); err != nil {
		t.Fatalf("killProcessGroup: %v", err)
	}
	if err := victim.Wait(); err == nil {
		t.Error("the child was not killed: signalling -pid for a child that joined someone else's group reaches a group it does not lead, so nothing dies and the session leaks")
	}
	if !processAlive(bystander.Process.Pid) {
		t.Error("killProcessGroup signalled a group the child did not lead")
	}
}

func TestKillProcessGroupOnSomethingThatNeverStarted(t *testing.T) {
	if err := killProcessGroup(&exec.Cmd{}); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("killProcessGroup = %v, want os.ErrProcessDone - os/exec reads anything else as a cancel that failed and reports it against the session", err)
	}
}

func TestKillGroupErrorSpeaksOsExecsVocabulary(t *testing.T) {
	if err := killGroupError(nil); err != nil {
		t.Errorf("killGroupError(nil) = %v, want nil", err)
	}
	if err := killGroupError(syscall.ESRCH); !errors.Is(err, os.ErrProcessDone) {
		t.Errorf("killGroupError(ESRCH) = %v, want os.ErrProcessDone - an empty group is the outcome asked for", err)
	}
	if err := killGroupError(syscall.EPERM); !errors.Is(err, syscall.EPERM) {
		t.Errorf("killGroupError(EPERM) = %v, want it kept - a real failure to signal must not be laundered into 'it had already finished'", err)
	}
}

// startLingererInWakesGroup spawns a process that does nothing but stay alive
// and, by not calling setProcessGroup, inherits Wake's process group - which
// is what every process Wake does not deliberately separate ends up in.
func startLingererInWakesGroup(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
	cmd.Env = append(os.Environ(), "WAKE_WANT_HELPER=1", "WAKE_HELPER_LINGER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lingerer: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// processAlive asks the OS rather than inferring from anything Wake holds:
// signal 0 checks for a process without sending one.
func processAlive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func waitForProcessGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if !processAlive(pid) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("process %d outlived the session that spawned it: cancelling reached the agent and nothing it started", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitForPid reads the pid the grandchild recorded, polling because it is
// written by a process two levels down that Wake never waits for.
func waitForPid(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(b)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agent never recorded the pid of what it spawned (%s)", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// --- what survives the session, for whoever has to reap it -----------------

// The pgid has to outlive the session that recorded it.
//
// Setpgid detached the agents from Wake's own death, which is what makes
// "detach" mean anything - but it also means a SIGKILLed daemon leaves 15-30
// process trees running with nobody holding a handle. The only way back to
// them is a group id written down at spawn, and cmd.Process.Pid is gone the
// moment finish reaps: the pid must be captured while the process exists and
// kept after it does not.
func TestSessionReportsTheGroupItCanBeReapedByEvenAfterItEnds(t *testing.T) {
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	pgid := s.Pgid()
	if pgid <= 0 {
		t.Fatalf("Pgid() = %d before the session ended: a daemon that crashes now leaves this agent unreachable", pgid)
	}
	// It must be the agent's *own* group, not the one Wake sits in.
	// Signalling the latter would take down Wake, its terminal job and
	// every other session at once.
	if got, err := syscall.Getpgid(pgid); err != nil || got != pgid {
		t.Fatalf("Getpgid(%d) = %d, %v - Pgid does not name a group the agent leads", pgid, got, err)
	}
	if wake := syscall.Getpgrp(); pgid == wake {
		t.Fatalf("Pgid() = %d, which is Wake's own process group", pgid)
	}

	drain(s)

	if after := s.Pgid(); after != pgid {
		t.Errorf("Pgid() = %d after the session ended, want %d kept - the reaper reads this from disk long after the process is gone", after, pgid)
	}
}

// The reaper's case: a process group with no *exec.Cmd behind it, because the
// process that held the handle was SIGKILLed. killProcessGroup cannot express
// that - it takes a live command - so the group id has to be enough on its
// own.
func TestKillGroupReachesAGroupNobodyHoldsAHandleTo(t *testing.T) {
	victim := startLingererInItsOwnGroup(t)
	bystander := startLingererInWakesGroup(t)

	if err := KillGroup(victim.Process.Pid); err != nil {
		t.Fatalf("KillGroup: %v", err)
	}
	if err := victim.Wait(); err == nil {
		t.Error("the group was not killed")
	}
	if !processAlive(bystander.Process.Pid) {
		t.Error("KillGroup reached a group it was not given")
	}
}

// A reaper reads process group ids off a file written by a process that is
// gone. A truncated write, a zeroed entry or a stale one, and the argument to
// this function is not what anybody intended - so the one group it must never
// signal is the caller's own, which would kill the daemon doing the reaping,
// its terminal job and every session it had just started.
func TestKillGroupRefusesTheGroupItIsCalledFrom(t *testing.T) {
	for _, pgid := range []int{0, 1, -1, syscall.Getpgrp()} {
		if err := KillGroup(pgid); err == nil {
			t.Errorf("KillGroup(%d) = nil, want a refusal - that is a signal to Wake's own group or to everything on the machine", pgid)
		}
	}
}

// startLingererInItsOwnGroup spawns a process that leads its own group and
// stays alive, which is what every agent looks like to a daemon that did not
// spawn it.
func startLingererInItsOwnGroup(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
	cmd.Env = append(os.Environ(), "WAKE_WANT_HELPER=1", "WAKE_HELPER_LINGER=1")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start lingerer: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}
