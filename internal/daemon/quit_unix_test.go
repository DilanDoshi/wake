//go:build unix

// /quit interrupts, stops, sweeps, and ends by force only what ignored both once
// the grace has run (owner's 2026-11-22 ruling). These start fake agents with a
// shell in a group of their own and ask the OS what is left.

package daemon

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A stop interrupts before it closes stdin, which is what frees a claude a
// background shell would hold open: it ends with its shell, inside the grace.
func TestAQuitInterruptsFirstSoAShellDoesNotHoldTheAgentOpen(t *testing.T) {
	log := interruptLog(t)
	logs := captureLog(t)
	grace := 4 * time.Second
	shortQuitGrace(t, grace)
	_, c, left := holdFleet(t)

	start := time.Now()
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if n := interruptsIn(log); n != 1 {
		t.Errorf("the agent was sent %d interrupts before its stdin closed, want 1", n)
	}
	if took := time.Since(start); took >= grace {
		t.Errorf("the quit took %v, the whole grace: the agent was only ended by the kill", took)
	}
	left.wantGone(t, "/quit")
	if got := logs.String(); strings.Contains(got, "did not end within") {
		t.Errorf("an agent the interrupt frees was killed at the end of the grace:\n%s", got)
	}
}

// An agent that ignored the interrupt and the stop is ended by force once the
// grace has run, and the group it started while the grace ran - with no task
// event, so nothing noted it - is swept with it: the world is noted again first.
func TestAQuitEndsAnAgentThatIgnoredBothAndSweepsAGroupItStartedMeanwhile(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	t.Setenv(fakePidfileEnv, pidfile)
	t.Setenv(fakeHoldEnv, "1")
	t.Setenv(fakeLateEnv, "1")
	t.Setenv(fakeSlowEnv, "400ms")
	t.Setenv(fakeIgnoreInterruptEnv, "1")
	fakeClaudeOnPath(t, "shellgroup")
	shortQuitGrace(t, time.Second)
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "work"})
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha}) // its listing is before the group exists
	c.pollState(idAlpha, rpc.StateEnded)

	left := leftovers{shell: waitForPid(t, pidfile), shellChild: waitForPid(t, pidfile+".gc"), own: waitForPid(t, pidfile+".own")}
	t.Cleanup(func() {
		for _, pid := range left.all() {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
	left.wantGone(t, "a quit of an agent that ignored the interrupt and the stop") // the daemon is still running
}

// A turn that ends inside the grace ends the agent by itself: nothing is killed.
func TestAQuitDoesNotKillATurnThatEndsInsideTheGrace(t *testing.T) {
	t.Setenv(fakeDelayEnv, "700ms")
	logs := captureLog(t)
	grace := 4 * time.Second
	shortQuitGrace(t, grace)
	fakeClaudeOnPath(t, "slow")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	start := time.Now()
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "work"})
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.awaitEvent(idAlpha, `echo: {`) // the turn's last words: a kill mid-turn never lets them out
	c.pollState(idAlpha, rpc.StateEnded)

	if took := time.Since(start); took >= grace {
		t.Errorf("the quit took %v, the whole grace", took)
	}
	if got := logs.String(); strings.Contains(got, "did not end within") {
		t.Errorf("an agent whose turn ended inside the grace was killed:\n%s", got)
	}
}

// A message typed ahead of a busy turn would run as the next turn once the
// interrupt ends this one, after stdin has closed. The quit's interrupt cancels
// it, so the agent ends at once instead of working on until the kill.
func TestAQuitCancelsAMessageQueuedBehindABusyTurn(t *testing.T) {
	logs := captureLog(t)
	grace := 4 * time.Second
	shortQuitGrace(t, grace)
	fakeClaudeOnPath(t, "busy")
	d := startDaemon(t)
	c := attach(t, d.socket)
	busyAgent(t, c, idAlpha, "alex")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "and then the docs"})
	c.awaitEvent(idAlpha, "queued")

	start := time.Now()
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if took := time.Since(start); took >= grace {
		t.Errorf("the quit took %v, the whole grace: the queued message ran as a turn of its own", took)
	}
	if got := logs.String(); strings.Contains(got, "did not end within") {
		t.Errorf("the queued message kept the agent running until the kill:\n%s", got)
	}
}

// A turn in flight is interrupted as Esc interrupts it, and ends at once: it is
// not left to run, and it is not killed.
func TestAQuitInterruptsAMidTurnAgentRatherThanKillingIt(t *testing.T) {
	log := interruptLog(t)
	logs := captureLog(t)
	grace := 4 * time.Second
	shortQuitGrace(t, grace)
	fakeClaudeOnPath(t, "busy")
	d := startDaemon(t)
	c := attach(t, d.socket)
	busyAgent(t, c, idAlpha, "alex")

	start := time.Now()
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if n := interruptsIn(log); n != 1 {
		t.Errorf("the busy agent was sent %d interrupts, want 1", n)
	}
	if took := time.Since(start); took >= grace {
		t.Errorf("the quit took %v, the whole grace: the turn ran on until the kill", took)
	}
	if got := logs.String(); strings.Contains(got, "did not end within") {
		t.Errorf("a mid-turn agent was killed rather than interrupted:\n%s", got)
	}
}

// An ask outstanding is withdrawn by the interrupt, as Esc withdraws it, where
// closing stdin alone would deny it. /quit does not refuse a blocked agent.
func TestAQuitWithdrawsABlockedAgentsAskRatherThanDenyingIt(t *testing.T) {
	log := interruptLog(t)
	logs := captureLog(t)
	shortQuitGrace(t, 4*time.Second)
	fakeClaudeOnPath(t, "ask")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.pollState(idAlpha, rpc.StateBlocked)

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if n := interruptsIn(log); n != 1 {
		t.Errorf("the blocked agent was sent %d interrupts before its stdin closed, want 1: the ask died as a denial", n)
	}
	if got := logs.String(); strings.Contains(got, "did not end within") {
		t.Errorf("a blocked agent was killed rather than interrupted:\n%s", got)
	}
}

// A second /quit of an agent already on its way out starts nothing more.
func TestASecondQuitOfTheSameAgentStartsNothingMore(t *testing.T) {
	log := interruptLog(t)
	logs := captureLog(t)
	shortQuitGrace(t, 4*time.Second)
	fakeClaudeOnPath(t, "busy")
	d := startDaemon(t)
	c := attach(t, d.socket)
	busyAgent(t, c, idAlpha, "alex")

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if n := strings.Count(logs.String(), "is being quit"); n != 1 {
		t.Errorf("two /quits started %d quits of the agent, want 1:\n%s", n, logs.String())
	}
	if n := interruptsIn(log); n != 1 {
		t.Errorf("the agent was sent %d interrupts, want 1", n)
	}
}

// An agent that reads nothing, with a queue full of what it was sent, is the one
// the kill exists for: the stop is not refused for the full queue, and the grace
// is armed whatever became of the interrupt.
func TestAQuitOfAnAgentWithAFullQueueIsEndedAfterTheGrace(t *testing.T) {
	logs := captureLog(t)
	shortQuitGrace(t, 2*time.Second)
	fakeClaudeOnPath(t, "deaf")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")
	text := strings.Repeat("x", 8<<10)
	for i := 0; i < agentQueue+32; i++ {
		c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: text})
	}
	c.await("the agent's queue refusing a message", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && strings.Contains(f.Text, "not reading its input")
	})

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if got := logs.String(); !strings.Contains(got, "did not end within") {
		t.Errorf("the wedged agent ended without the kill the grace is for:\n%s", got)
	}
}

// A quit ends the live agent, and its transcript stays: /resume finds it and
// brings the conversation back in place, which is what the manual says.
func TestAQuitSessionCanBeResumedFromItsTranscript(t *testing.T) {
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	fakeClaudeOnPath(t, "")
	real := t.TempDir()
	writeTranscript(t, projects, slugOf(real), idAlpha, real)
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	c.send(rpc.Frame{Kind: rpc.FrameResume, SessionID: idAlpha})
	f := c.await("the daemon's answer to a resume of the quit session", func(f rpc.Frame) bool {
		if f.Kind == rpc.FrameError && f.SessionID == idAlpha {
			return true
		}
		return f.Kind == rpc.FrameStatusReply && f.Status != nil && stateOf(*f.Status, idAlpha) != rpc.StateEnded
	})
	if f.Kind == rpc.FrameError {
		t.Fatalf("a quit session cannot be resumed: %s", f.Text)
	}
}

// ⌃C on a busy agent parks it once its turn ends. A /quit typed after that wins:
// the agent ends and is not parked, since a quit is the operator saying it is
// not to come back (the turn ended inside the grace here, which is when a park in
// flight would otherwise have completed).
func TestAQuitOfAnAgentBeingParkedEndsItRatherThanParkingIt(t *testing.T) {
	t.Setenv(fakeDelayEnv, "1500ms")
	shortQuitGrace(t, 4*time.Second)
	fakeClaudeOnPath(t, "slow")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "work"})
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	time.Sleep(300 * time.Millisecond) // the park is applied behind the message and stdin closes; its turn runs on

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 0 {
		t.Errorf("the park book holds %+v: a session the operator quit after parking it came back as parked", recs)
	}
}

// And the other order: a park of an agent already being quit is refused, and the
// agent ends as the quit it is.
func TestAParkOfAnAgentBeingQuitIsRefused(t *testing.T) {
	t.Setenv(fakeDelayEnv, "700ms")
	shortQuitGrace(t, 4*time.Second)
	fakeClaudeOnPath(t, "slow")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	c.await("the park of an agent being quit refused", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameError && strings.Contains(f.Text, "being quit")
	})
	c.pollState(idAlpha, rpc.StateEnded)

	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 0 {
		t.Errorf("the park book holds %+v: an agent being quit was parked", recs)
	}
}

// ⌃Q parks every agent it takes, but not one the operator has already quit: that
// one ends as a quit and is not offered back.
func TestAParkingQuitDoesNotBookAnAgentThatIsBeingQuit(t *testing.T) {
	t.Setenv(fakeDelayEnv, "700ms")
	shortQuitGrace(t, 4*time.Second)
	fakeClaudeOnPath(t, "slow")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	d.waitForExit(t)

	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 0 {
		t.Errorf("the park book holds %+v: ⌃Q parked an agent the operator had quit", recs)
	}
}

// An interrupt frees claude from the shell that would hold it open (checked live
// on 2.1.329), so a quit leaves it to exit by itself: the held-open backstop is a
// park's, and a kill is only for an agent that ignored the interrupt and the stop.
func TestAQuitLeavesAnAgentThatHeedsTheInterruptToExitByItself(t *testing.T) {
	t.Setenv(fakeSlowEnv, "600ms")
	logs := captureLog(t)
	_, c, left := holdFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "work"})
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.awaitEvent(idAlpha, `echo: {`) // its turn ends with the shell still running
	c.pollState(idAlpha, rpc.StateEnded)

	left.wantGone(t, "a quit mid-turn with a shell running")
	if got := logs.String(); strings.Contains(got, "held open") {
		t.Errorf("an agent that heeded the interrupt was killed at its turn's end for the shell it ran:\n%s", got)
	}
}

// The same for `wake stop`: every stop leaves an agent that heeds the interrupt to
// exit by itself.
func TestWakeStopLeavesAnAgentThatHeedsTheInterruptToExitByItself(t *testing.T) {
	t.Setenv(fakeSlowEnv, "600ms")
	logs := captureLog(t)
	d, c, left := holdFleet(t)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "work"})
	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)

	left.wantGone(t, "wake stop mid-turn with a shell running")
	if got := logs.String(); strings.Contains(got, "held open") {
		t.Errorf("an agent that heeded the interrupt was killed at its turn's end for the shell it ran:\n%s", got)
	}
}
