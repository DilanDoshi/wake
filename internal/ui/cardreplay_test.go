package ui

// A card learned about only from a fleet report, and what makes it answerable
// again once the daemon hands this client the ask it never saw live.
//
// See internal/daemon/askreplay_test.go for the end-to-end half: a second
// client attaching after the ask is already outstanding, over a real socket.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// TestAReportOnlyCardDefaultsToABarePermissionAllow pins the shape a client
// that never saw an ask live is stuck with: Cards.Reconcile has only
// rpc.SessionStatus.RequestIDs to build from - an id and a tool name, no Ask
// kind and no questions - so Card.Shape defaults to ShapePermission and Allow
// mints a bare FrameAllow. For a question that is exactly the silent loss
// docs/notes/bugs.md records: the tool runs, the model is told nobody
// answered, and the turn still ends successfully.
func TestAReportOnlyCardDefaultsToABarePermissionAllow(t *testing.T) {
	ask := recordedAsks(t, choiceFixture)[0]
	cs := Cards{}.Reconcile(&rpc.Status{Sessions: []rpc.SessionStatus{{
		ID: "s1", Tool: "AskUserQuestion", RequestIDs: []string{ask.RequestID},
	}}})

	c, ok := cs.For("s1")
	if !ok {
		t.Fatal("Reconcile built no card for the outstanding request id")
	}
	if c.Shape() != ShapePermission {
		t.Fatalf("a report-only card resolved to shape %d, want ShapePermission - Reconcile carries no Ask kind", c.Shape())
	}
	if f := c.Allow(); f.Kind != rpc.FrameAllow {
		t.Fatalf("Allow() = %q, want a bare FrameAllow; this is the pre-fix behaviour the next test proves the replay corrects", f.Kind)
	}
}

// TestReplayingTheRetainedAskRebuildsAnAnswerableCard is the fix: once the
// same core.Event a live client would have gotten reaches this one - the
// daemon's retained ask, replayed as an ordinary rpc.FrameEvent, folded
// through the very same Cards.Add a live ask always goes through - the
// report-only stand-in is replaced in place (Cards.Add keys on AgentID and
// RequestID) and an allow now carries the answer instead of losing it.
func TestReplayingTheRetainedAskRebuildsAnAnswerableCard(t *testing.T) {
	ask := recordedAsks(t, choiceFixture)[0]
	cs := Cards{}.Reconcile(&rpc.Status{Sessions: []rpc.SessionStatus{{
		ID: "s1", Tool: "AskUserQuestion", RequestIDs: []string{ask.RequestID},
	}}})

	// What server.replayPendingAsks puts on the wire and App.observe folds:
	// the retained ask, arriving late as the ordinary event a live client
	// would have seen at the time it was asked.
	cs = cs.Add("s1", ask)

	c, ok := cs.For("s1")
	if !ok {
		t.Fatal("the replayed ask produced no card")
	}
	if c.Shape() != ShapeQuestion {
		t.Fatalf("shape = %d after the replay, want ShapeQuestion", c.Shape())
	}
	for i := range c.Detail.Questions {
		c = c.Pick(i, 0)
	}

	f := c.Allow()
	if f.Kind != rpc.FrameAnswer {
		t.Fatalf("Allow() = %q after the replay, want FrameAnswer - a bare FrameAllow here is exactly the silent loss this fix closes", f.Kind)
	}
	if len(f.Answers) == 0 {
		t.Fatal("the replayed card's allow carries no answers")
	}
}
