package ui

// Type-ahead: a message typed while its agent is busy waits rather than going to
// the wire mid-turn, and is delivered when the agent is free. The trigger is the
// message lifecycle (a stamped uuid Wake tracks), with the State working→idle edge
// as a gap backstop. See queue.go.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// "completed"/"cancelled" are the CLI's command_lifecycle wire states, the shapes
// core.messageStateEvent decodes into Event.Text. A test constructs the decoded
// event directly, the way oneAgent constructs a status.
func lifecycleFrame(id, msgID, state string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: &core.Event{
		Kind: core.KindMessageState, SessionID: id, MessageID: msgID, Text: state,
	}}
}

// idleDM is a DM on s1, idle and ready to take a message immediately.
func idleDM(t *testing.T) App {
	t.Helper()
	return dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
}

// sentThenQueued sends one message to an idle s1 (dispatched, in flight) and
// queues a second behind it. Returns the app and the first message's uuid, read
// back off App.inflight so a lifecycle frame can name it.
func sentThenQueued(t *testing.T, first, second string) (App, string) {
	t.Helper()
	a := idleDM(t)
	m, _ := typeAndSubmit(a, first)
	a = m.(App)
	firstID := a.inflight["s1"]
	if firstID == "" {
		t.Fatalf("an immediate send did not mark s1 in flight")
	}
	m2, cmd := typeAndSubmit(a, second)
	a = m2.(App)
	if cmd != nil {
		t.Errorf("a fast follow-up produced a command; it should have queued, not gone to the wire")
	}
	if len(a.queued["s1"]) != 1 || a.queued["s1"][0].echo != second {
		t.Fatalf("the follow-up did not queue behind the in-flight message: %v", a.queued["s1"])
	}
	return a, firstID
}

// The bug, stated: a message typed at a working agent must not reach the wire.
func TestSubmitWhileWorkingQueuesRatherThanSends(t *testing.T) {
	a := idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))

	m, cmd := typeAndSubmit(a, "run the tests")
	a = m.(App)
	if cmd != nil {
		t.Errorf("a message to a working agent produced a command; nothing should reach the wire mid-turn")
	}
	if q := a.queued["s1"]; len(q) != 1 || q[0].echo != "run the tests" {
		t.Errorf("the message was not queued for s1: %v", q)
	}
}

// An idle agent takes its message immediately, and it is stamped so the CLI's
// lifecycle names it - which is what marks the agent in flight.
func TestSubmitWhileIdleSendsImmediatelyAndStamps(t *testing.T) {
	a := idleDM(t)

	m, cmd := typeAndSubmit(a, "hi")
	a = m.(App)
	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameSend || f.SessionID != "s1" || f.Text != "hi" {
		t.Errorf("an idle agent's message was not sent immediately: %+v", f)
	}
	if f.MessageID == "" {
		t.Errorf("the sent message was not stamped with a uuid, so its lifecycle cannot be tracked")
	}
	if a.inflight["s1"] != f.MessageID {
		t.Errorf("the dispatched message did not mark the agent in flight: inflight=%q, frame=%q", a.inflight["s1"], f.MessageID)
	}
	if len(a.queued["s1"]) != 0 {
		t.Errorf("an idle agent's message was queued instead of sent")
	}
}

// The CRITICAL fix: a follow-up typed before the daemon reports the first turn
// "working" (its init lands seconds later) still queues rather than racing onto a
// busy stdin - because inflight is set the instant the first is dispatched.
func TestFastFollowUpToIdleAgentQueuesNotSent(t *testing.T) {
	// sentThenQueued asserts the follow-up produced no command and queued. The
	// point of naming it here is the scenario: two submits with no status report
	// in between, which is what the daemon's lagging "working" makes ordinary.
	a, _ := sentThenQueued(t, "first", "second")
	if a.inflight["s1"] == "" {
		t.Errorf("the first message is no longer tracked in flight")
	}
}

// The primary trigger: the in-flight message's completed lifecycle frees the
// agent, and the next queued message goes out.
func TestLifecycleCompletedFlushesTheNextQueued(t *testing.T) {
	a, firstID := sentThenQueued(t, "first", "second")

	a = a.observeMessageState("s1", core.Event{Kind: core.KindMessageState, SessionID: "s1", MessageID: firstID, Text: "completed"})
	if a.inflight["s1"] != "" {
		t.Fatalf("a completed lifecycle did not clear the in-flight mark")
	}
	a, cmd := a.flushQueued(a.fleet)
	f := sentFrame(t, a, cmd)
	if f.Text != "second" {
		t.Errorf("the flush sent %q, want the queued message once the first completed", f.Text)
	}
	if len(a.queued["s1"]) != 0 {
		t.Errorf("the queue still holds %d after flushing", len(a.queued["s1"]))
	}
}

// esc rides the same path: interrupting the running message cancels it, and the
// cancelled lifecycle lets the next queued message through.
func TestInterruptCancelledFlushesTheNextQueued(t *testing.T) {
	a, firstID := sentThenQueued(t, "first", "second")

	a = a.observeMessageState("s1", core.Event{Kind: core.KindMessageState, SessionID: "s1", MessageID: firstID, Text: "cancelled"})
	a, cmd := a.flushQueued(a.fleet)
	f := sentFrame(t, a, cmd)
	if f.Text != "second" {
		t.Errorf("a cancelled in-flight message did not let the next through: sent %q", f.Text)
	}
}

// The State working→idle edge is a backstop: if the completed lifecycle is lost to
// a frame gap, the daemon's own idle report still frees the agent.
func TestStateEdgeBackstopsALostLifecycle(t *testing.T) {
	a, _ := sentThenQueued(t, "first", "second")
	// The first message's turn ran (working) and ended (idle), but its lifecycle
	// never arrived - only the status reports did.
	prev := a.applyFrame(oneAgent("s1", "alex", rpc.StateWorking)).fleet
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateWorking)).applyFrame(oneAgent("s1", "alex", rpc.StateIdle))
	a, cmd := a.flushQueued(prev)

	f := sentFrame(t, a, cmd)
	if f.Text != "second" {
		t.Errorf("the State-edge backstop did not flush when the lifecycle was lost: sent %q", f.Text)
	}
	if a.inflight["s1"] != f.MessageID {
		t.Errorf("the backstop did not re-arm inflight for the flushed message")
	}
}

// No flush while a Wake message is genuinely in flight: the next waits for that
// one to end, so a burst never coalesces.
func TestNoFlushWhileAMessageIsInFlight(t *testing.T) {
	a, _ := sentThenQueued(t, "first", "second")
	// s1 is now working on the first message.
	prev := a.fleet
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	a, cmd := a.flushQueued(prev)

	if cmd != nil {
		t.Errorf("the queue flushed while the first message was still in flight")
	}
	if len(a.queued["s1"]) != 1 {
		t.Errorf("the queued message was drained before its turn came")
	}
}

// A message queued against an agent that was busy on its own turn (not a Wake
// send) flushes when that turn ends and the agent reports idle.
func TestQueuedMessageFlushesWhenAgentGoesIdle(t *testing.T) {
	a := idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	m, _ := typeAndSubmit(a, "run the tests")
	a = m.(App)
	prev := a.fleet

	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateIdle))
	a, cmd := a.flushQueued(prev)

	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameSend || f.SessionID != "s1" || f.Text != "run the tests" {
		t.Errorf("the flush wrote %+v, want the queued text sent to s1", f)
	}
}

// A message queued for an agent that ends before its turn finishes is dropped,
// not delivered to nothing.
func TestQueueDropsWhenTheAgentEnds(t *testing.T) {
	a := idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	m, _ := typeAndSubmit(a, "later")
	a = m.(App)

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

// The flush runs on the batched stream path, which is the one production frames
// take: a completed lifecycle arriving there drains the queue.
func TestFlushIsWiredIntoTheStreamPath(t *testing.T) {
	a, firstID := sentThenQueued(t, "first", "second")

	m, _ := a.Update(streamMsg{gen: a.gen, batch: batch{frames: []rpc.Frame{lifecycleFrame("s1", firstID, "completed")}}})
	a = m.(App)

	if len(a.queued["s1"]) != 0 {
		t.Errorf("a completed lifecycle on the stream path did not flush the queue; flushQueued is not wired into stream()")
	}
}

// In the room a broadcast reaches idle targets now and holds for busy ones, each
// stamped so its lifecycle is tracked; the room's own line is drawn once.
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
	if f.SessionID != "s1" || f.MessageID == "" {
		t.Errorf("the idle target was not sent a stamped message: %+v", f)
	}
	q := a.queued["s2"]
	if len(q) != 1 || q[0].echo != "@all ship it" || !q[0].fromRoom || q[0].id == "" {
		t.Errorf("the working target's broadcast was not queued fromRoom with a uuid: %v", q)
	}
	if len(a.queued["s1"]) != 0 {
		t.Errorf("the idle target's message was queued instead of sent")
	}
}

// The waiting messages are visible above the composer so type-ahead is not silent.
func TestAQueuedMessageShowsInThePin(t *testing.T) {
	a := idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	m, _ := typeAndSubmit(a, "fix the bug")
	a = m.(App)

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
	a := idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	for range maxQueuedPinRows + 3 {
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

// The pane never draws taller than its allocation with the pin stacked above a
// growing draft - the composer must reserve the pinned rows from its own growth,
// not just from the transcript's floor. The alt-screen overflow the reviews
// caught: measured on the DM's own View, before any frame-level clip.
func TestThePaneStaysInBoundsWithPinAndDraft(t *testing.T) {
	const w, h = 80, 14
	a := idleDM(t).withSize(w, h).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
	for range maxQueuedPinRows {
		m, _ := typeAndSubmit(a, "a queued follow-up")
		a = m.(App)
	}
	a = a.withDraft(strings.Repeat("line\n", 20)) // a draft that wants far more rows than fit

	got := strings.Count(a.dmFor("s1").View(w, h), "\n") + 1
	if got > h {
		t.Errorf("the pane drew %d rows into a %d-row terminal: the pin and draft overflow", got, h)
	}
}
