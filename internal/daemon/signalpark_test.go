package daemon

// A signal's quit interrupts a busy agent first, as Esc does, so its turn ends
// at once and it parks inside the grace instead of being killed at the grace's
// end and lost (owner's 2026-10-22 ruling). ⌃Q still lets a turn run.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// busyAgent spawns the busy fake and starts a turn it will hold open - after its
// start-up turn has ended, or that turn's end lands behind the prompt and closes
// the turn the prompt started.
func busyAgent(t *testing.T, c *testClient, id, name string) {
	t.Helper()
	c.spawn(id, name)
	c.awaitEvent(id, "done")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: fakeBusyPrompt})
	c.awaitEvent(id, "working")
	c.awaitState(id, rpc.StateWorking)
}

func interruptLog(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "interrupts")
	t.Setenv(fakeInterruptLogEnv, path)
	return path
}

func interruptsIn(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "interrupt\n")
}

func TestASignalInterruptsABusyAgentSoItParksRatherThanBeingKilled(t *testing.T) {
	log := interruptLog(t)
	fakeClaudeOnPath(t, "busy")
	grace := 4 * time.Second
	shortQuitGrace(t, grace)
	d := startDaemon(t)
	c := attach(t, d.socket)
	busyAgent(t, c, idAlpha, "alex")
	c.close()

	start := time.Now()
	d.stop(t) // the cancelled context a SIGTERM delivers

	if n := interruptsIn(log); n != 1 {
		t.Errorf("the busy agent was sent %d interrupts on the way out, want 1", n)
	}
	if took := time.Since(start); took >= grace {
		t.Errorf("the signalled shutdown took %v, the whole grace: the turn ran on until the kill", took)
	}
	recs := loadParkBook(parkBookPath(d.socket))
	if len(recs) != 1 || recs[0].Name != "alex" {
		t.Fatalf("the park book holds %+v after a signal, want alex: a busy agent was killed at the end of the grace and lost", recs)
	}
}

// ⌃Q is the operator's own quit, and its rule is unchanged: a turn in flight is
// let run for the grace, and a session it then has to kill is not parked.
func TestAQuitParkStillLetsABusyTurnRunRatherThanInterruptingIt(t *testing.T) {
	log := interruptLog(t)
	fakeClaudeOnPath(t, "busy")
	shortQuitGrace(t, 2*time.Second)
	d := startDaemon(t)
	c := attach(t, d.socket)
	busyAgent(t, c, idAlpha, "alex")

	c.send(rpc.Frame{Kind: rpc.FrameParkAll})
	d.waitForExit(t)

	if n := interruptsIn(log); n != 0 {
		t.Errorf("⌃Q sent %d interrupts: only a signal cancels a turn in flight", n)
	}
	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 0 {
		t.Errorf("the park book holds %+v: ⌃Q parked a session it had to kill", recs)
	}
}

// An interrupt between turns is a no-op claude answers (interrupt-idle.jsonl),
// so an idle agent is sent one like any other and parks as it always has.
func TestAnIdleAgentIsInterruptedHarmlesslyAndParks(t *testing.T) {
	log := interruptLog(t)
	fakeClaudeOnPath(t, "busy")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "done")
	c.close()

	d.stop(t)

	if n := interruptsIn(log); n != 1 {
		t.Errorf("the idle agent was sent %d interrupts on the way out, want 1", n)
	}
	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 1 {
		t.Errorf("the park book holds %+v, want the idle agent parked", recs)
	}
}

// A message typed ahead of a busy turn sits in claude's queue and runs as the
// next turn once the interrupt ends this one - after stdin has closed, for as
// long as it takes. The shutdown's interrupt cancels it, so the session parks.
func TestASignalCancelsAMessageQueuedBehindABusyTurn(t *testing.T) {
	fakeClaudeOnPath(t, "busy")
	grace := 4 * time.Second
	shortQuitGrace(t, grace)
	d := startDaemon(t)
	c := attach(t, d.socket)
	busyAgent(t, c, idAlpha, "alex")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "and then the docs"})
	c.awaitEvent(idAlpha, "queued")
	c.close()

	start := time.Now()
	d.stop(t)

	if took := time.Since(start); took >= grace {
		t.Errorf("the signalled shutdown took %v, the whole grace: the queued message ran as a turn of its own", took)
	}
	if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 1 {
		t.Fatalf("the park book holds %+v, want alex: the message queued behind its turn kept it running until the kill", recs)
	}
}
