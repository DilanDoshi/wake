package daemon

// Both quits that park - a signal and ⌃Q - first interrupt every agent, as Esc
// does, so a turn in flight ends at once and the session parks inside the grace
// instead of being killed at the grace's end and lost (owner's 2026-10-22 ruling
// for a signal, 2026-11-07 for ⌃Q).

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// parkingQuits are the two quits that park, each as a client meets it: a signal
// is the cancelled context a SIGTERM delivers, and ⌃Q is FrameParkAll on the
// client's own connection.
var parkingQuits = []struct {
	name string
	quit func(t *testing.T, d *testDaemon, c *testClient)
}{
	{"signal", func(t *testing.T, d *testDaemon, c *testClient) {
		c.close()
		d.stop(t)
	}},
	{"ctrl-q", func(t *testing.T, d *testDaemon, c *testClient) {
		c.send(rpc.Frame{Kind: rpc.FrameParkAll})
		d.waitForExit(t)
	}},
}

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

// offeredBack is the names the next daemon on this socket offers through
// /resume: its report's parked half, read off the park book this one left. A
// record with no transcript is never offered, and the fake writes none, so the
// caller plants one.
func offeredBack(t *testing.T, socket string) []string {
	t.Helper()
	startDaemonOn(t, socket)
	var names []string
	for _, p := range attach(t, socket).status().Parked {
		names = append(names, p.Name)
	}
	return names
}

func TestAParkingQuitInterruptsABusyAgentSoItParksRatherThanBeingKilled(t *testing.T) {
	for _, q := range parkingQuits {
		t.Run(q.name, func(t *testing.T) {
			log := interruptLog(t)
			fakeClaudeOnPath(t, "busy")
			plantTranscript(t, idAlpha)
			grace := 4 * time.Second
			shortQuitGrace(t, grace)
			d := startDaemon(t)
			c := attach(t, d.socket)
			busyAgent(t, c, idAlpha, "alex")

			start := time.Now()
			q.quit(t, d, c)

			if n := interruptsIn(log); n != 1 {
				t.Errorf("the busy agent was sent %d interrupts on the way out, want 1", n)
			}
			if took := time.Since(start); took >= grace {
				t.Errorf("the shutdown took %v, the whole grace: the turn ran on until the kill", took)
			}
			if got := offeredBack(t, d.socket); !slices.Equal(got, []string{"alex"}) {
				t.Errorf("/resume offers %v after the quit, want alex: a busy agent was killed at the end of the grace and lost", got)
			}
		})
	}
}

// An interrupt between turns is a no-op claude answers (interrupt-idle.jsonl),
// so an idle agent is sent one like any other and parks as it always has.
func TestAnIdleAgentIsInterruptedHarmlesslyAndParks(t *testing.T) {
	for _, q := range parkingQuits {
		t.Run(q.name, func(t *testing.T) {
			log := interruptLog(t)
			fakeClaudeOnPath(t, "busy")
			d := startDaemon(t)
			c := attach(t, d.socket)
			c.spawn(idAlpha, "alex")
			c.awaitEvent(idAlpha, "done")

			q.quit(t, d, c)

			if n := interruptsIn(log); n != 1 {
				t.Errorf("the idle agent was sent %d interrupts on the way out, want 1", n)
			}
			if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 1 {
				t.Errorf("the park book holds %+v, want the idle agent parked", recs)
			}
		})
	}
}

// A message typed ahead of a busy turn sits in claude's queue and runs as the
// next turn once the interrupt ends this one - after stdin has closed, for as
// long as it takes. The shutdown's interrupt cancels it, so the session parks.
func TestAParkingQuitCancelsAMessageQueuedBehindABusyTurn(t *testing.T) {
	for _, q := range parkingQuits {
		t.Run(q.name, func(t *testing.T) {
			fakeClaudeOnPath(t, "busy")
			grace := 4 * time.Second
			shortQuitGrace(t, grace)
			d := startDaemon(t)
			c := attach(t, d.socket)
			busyAgent(t, c, idAlpha, "alex")
			c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "and then the docs"})
			c.awaitEvent(idAlpha, "queued")

			start := time.Now()
			q.quit(t, d, c)

			if took := time.Since(start); took >= grace {
				t.Errorf("the shutdown took %v, the whole grace: the queued message ran as a turn of its own", took)
			}
			if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 1 {
				t.Fatalf("the park book holds %+v, want alex: the message queued behind its turn kept it running until the kill", recs)
			}
		})
	}
}

// Closing stdin under an outstanding ask is recorded as the operator's denial,
// which is why ⌃C refuses a blocked agent. The interrupt reaches it first and
// withdraws the ask the way Esc does (interrupt-pending-basic.jsonl), so the
// session parks without being told no.
func TestAParkingQuitWithdrawsAnOutstandingAskRatherThanDenyingIt(t *testing.T) {
	for _, q := range parkingQuits {
		t.Run(q.name, func(t *testing.T) {
			log := interruptLog(t)
			fakeClaudeOnPath(t, "ask")
			d := startDaemon(t)
			c := attach(t, d.socket)
			c.spawn(idAlpha, "sydney")
			c.pollState(idAlpha, rpc.StateBlocked)

			q.quit(t, d, c)

			if n := interruptsIn(log); n != 1 {
				t.Errorf("the blocked agent was sent %d interrupts before its stdin closed, want 1: the ask died as a denial", n)
			}
			if recs := loadParkBook(parkBookPath(d.socket)); len(recs) != 1 {
				t.Errorf("the park book holds %+v, want the blocked agent parked", recs)
			}
		})
	}
}
