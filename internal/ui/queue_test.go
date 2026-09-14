package ui

// Type-ahead: a message typed while an agent is working waits rather than going
// to the wire mid-turn, and is delivered on the turn's working→idle edge. The
// bug this fixes is "submit while the agent is thinking and it never sees the
// message": Wake wrote un-stamped lines to a busy stdin, where the CLI's
// handling is unrecorded. See queue.go.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// workingWithQueued is a DM on s1, put into a turn, with one message typed into
// it - so it is queued rather than sent. Returns the app and the fleet as it was
// while working, which is a flush's prev.
func workingWithQueued(t *testing.T, text string) (App, Fleet) {
	t.Helper()
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	m, _ := typeAndSubmit(a, text)
	a = m.(App)
	if len(a.queued["s1"]) != 1 {
		t.Fatalf("submit while working did not queue the message: %v", a.queued["s1"])
	}
	return a, a.fleet
}

// The bug, stated: a message typed at a working agent must not reach the wire.
func TestSubmitWhileWorkingQueuesRatherThanSends(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateWorking))

	m, cmd := typeAndSubmit(a, "run the tests")
	a = m.(App)
	if cmd != nil {
		t.Errorf("a message to a working agent produced a command; nothing should reach the wire mid-turn")
	}
	q := a.queued["s1"]
	if len(q) != 1 || q[0].echo != "run the tests" || q[0].wire != "run the tests" {
		t.Errorf("the message was not queued for s1: %v", q)
	}
}

// The other half: an idle agent still takes its message immediately, so the
// common case is unchanged.
func TestSubmitWhileIdleSendsImmediately(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)

	m, cmd := typeAndSubmit(a, "hi")
	a = m.(App)
	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameSend || f.SessionID != "s1" || f.Text != "hi" {
		t.Errorf("an idle agent's message was not sent immediately: %+v", f)
	}
	if len(a.queued["s1"]) != 0 {
		t.Errorf("an idle agent's message was queued instead of sent")
	}
}

// The turn ends, and the held message goes out - as its own turn, on the wire
// the first message would have taken.
func TestQueuedMessageFlushesOnWorkingToIdleEdge(t *testing.T) {
	a, prev := workingWithQueued(t, "run the tests")

	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateIdle))
	a, cmd := a.flushQueued(prev)

	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameSend || f.SessionID != "s1" || f.Text != "run the tests" {
		t.Errorf("the flush wrote %+v, want the queued text sent to s1", f)
	}
	if len(a.queued["s1"]) != 0 {
		t.Errorf("the queue still holds %d messages after flushing one", len(a.queued["s1"]))
	}
}

// One per edge: two queued messages do not both go out when the turn ends. The
// first goes, the second waits for that one's own turn to finish - which is what
// keeps a burst from coalescing into one prompt.
func TestQueuedMessagesFlushOneAtATimeInOrder(t *testing.T) {
	a, prev := workingWithQueued(t, "first")
	m, _ := typeAndSubmit(a, "second") // still working → also queued, behind the first
	a = m.(App)
	if len(a.queued["s1"]) != 2 {
		t.Fatalf("the second message did not queue behind the first: %v", a.queued["s1"])
	}

	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateIdle))
	a, cmd := a.flushQueued(prev)

	f := sentFrame(t, a, cmd)
	if f.Text != "first" {
		t.Errorf("the flush sent %q, want the oldest queued message first", f.Text)
	}
	if got := a.queued["s1"]; len(got) != 1 || got[0].echo != "second" {
		t.Errorf("the second message did not stay queued in order: %v", got)
	}
}

// The edge is a real transition, not merely "idle with a queue": an agent that
// never left idle has nothing to flush. Guards against a message going out the
// instant it is queued behind a still-in-flight one.
func TestNoFlushForAnAgentThatWasAlreadyIdle(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.enqueue("s1", queuedMsg{wire: "x", echo: "x"})

	prev := a.fleet // s1 is idle here, and stays idle
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateIdle))
	a, cmd := a.flushQueued(prev)

	if cmd != nil {
		t.Errorf("the queue flushed with no working→idle edge")
	}
	if len(a.queued["s1"]) != 1 {
		t.Errorf("the queue was drained without an edge")
	}
}

// esc interrupts the current turn and the held message survives it, then goes
// out on the idle the interrupt produces: "esc interrupts and lets the next
// queued message through".
func TestEscInterruptKeepsTheQueueAndItFlushesAfter(t *testing.T) {
	a, _ := workingWithQueued(t, "run it")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if len(a.queued["s1"]) != 1 {
		t.Fatalf("esc dropped the queued message: %v", a.queued["s1"])
	}

	prev := a.fleet // still working: esc sends an interrupt, the daemon reports idle later
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateIdle))
	a, cmd := a.flushQueued(prev)

	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameSend || f.Text != "run it" {
		t.Errorf("the queued message did not go through after the interrupt: %+v", f)
	}
}

// A message queued for an agent that ends before its turn finishes is dropped,
// not delivered to nothing.
func TestQueueDropsWhenTheAgentEnds(t *testing.T) {
	a, _ := workingWithQueued(t, "later")

	prev := a.fleet
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateEnded))
	a, cmd := a.flushQueued(prev)

	if cmd != nil {
		t.Errorf("an ended agent's queue was delivered rather than dropped")
	}
	if _, held := a.queued["s1"]; held {
		t.Errorf("an ended agent's queue was not dropped")
	}
}

// The flush is wired into the real frame path, not only callable in isolation:
// a status report that ends a turn drains the queue through Update.
func TestFlushIsWiredIntoTheFrameUpdate(t *testing.T) {
	a, _ := workingWithQueued(t, "go")

	m, _ := a.update(frameMsg{Frame: oneAgent("s1", "alex", rpc.StateIdle)})
	a = m.(App)

	if len(a.queued["s1"]) != 0 {
		t.Errorf("a turn-ending report did not flush the queue; flushQueued is not wired into the frameMsg case")
	}
}

// In the room a broadcast reaches idle targets now and holds for busy ones. The
// room's own line is drawn regardless (tested elsewhere); here it is the split.
func TestRoomBroadcastQueuesBusyTargetsAndSendsIdle(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true,
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "sydney", State: rpc.StateIdle},
			{ID: "s2", Name: "alex", State: rpc.StateWorking},
			{ID: "s6", Name: core.ManagerName, State: rpc.StateIdle},
		},
	}})

	m, cmd := typeAndSubmit(a, "@all ship it")
	a = m.(App)

	f := sentFrame(t, a, cmd) // only the idle target is written now
	if f.SessionID != "s1" {
		t.Errorf("the idle target was not sent to immediately: %+v", f)
	}
	q := a.queued["s2"]
	if len(q) != 1 || q[0].echo != "@all ship it" || !q[0].fromRoom {
		t.Errorf("the working target's broadcast was not queued fromRoom: %v", q)
	}
	if len(a.queued["s1"]) != 0 {
		t.Errorf("the idle target's message was queued instead of sent")
	}
}

// The waiting messages are visible above the composer so type-ahead is not
// silent: what you queued is on screen until its turn comes.
func TestAQueuedMessageShowsInThePin(t *testing.T) {
	a, _ := workingWithQueued(t, "fix the bug")

	out := shown(a)
	if !strings.Contains(out, "fix the bug") {
		t.Errorf("the queued message is not shown above the composer:\n%s", out)
	}
	if !strings.Contains(out, queuedGlyph) {
		t.Errorf("the queued pin is not marked with %q:\n%s", queuedGlyph, out)
	}
}

// A deep queue does not take the transcript's rows without limit: the pin is
// bounded and counts the rest, so the pane can never grow past the terminal.
func TestTheQueuedPinIsBounded(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	for i := 0; i < maxQueuedPinRows+3; i++ {
		m, _ := typeAndSubmit(a, "message")
		a = m.(App)
	}

	d := a.dmFor("s1")
	if d.queuedRows() != maxQueuedPinRows {
		t.Errorf("a deep queue draws %d rows, want it capped at %d", d.queuedRows(), maxQueuedPinRows)
	}
	if got := strings.Count(d.queuedPin(200), "\n") + 1; got != maxQueuedPinRows {
		t.Errorf("the pin drew %d rows, want the cap %d", got, maxQueuedPinRows)
	}
	if !strings.Contains(shown(a), "more queued") {
		t.Errorf("a queue past the cap does not count the rest:\n%s", shown(a))
	}
}
