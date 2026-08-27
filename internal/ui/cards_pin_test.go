package ui

// Pinning, and the two ways an ask can stop being answerable.
//
// The hazard this file is written against is not that a card lingers. It is
// that closing stdin on an unanswered ask ends the turn as a permission-rule
// denial on a *success* turn, byte-for-byte what a deliberate deny produces -
// so a room that abandoned a question cannot tell afterwards that it did. The
// state is undetectable, so it has to be unreachable: an ask stays up until
// something settles it, and every report is reconciled in both directions.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// ask is a permission request as the fold hands one to the room.
func ask(requestID, tool, arg string) core.Event {
	return core.Event{
		Kind:      core.KindPermissionRequest,
		RequestID: requestID,
		Tool:      &core.ToolCall{Name: tool, Display: arg},
	}
}

// withdrawn is the frame that retires an ask nobody answered.
func withdrawn(requestID string) core.Event {
	return core.Event{Kind: core.KindRequestWithdrawn, RequestID: requestID}
}

func TestACardStaysPinnedUntilItIsSettled(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "Write", "note.txt"))
	cs = cs.Add("s2", ask("r2", "Bash", "rm -rf build/"))
	if cs.Len() != 2 {
		t.Fatalf("Len = %d, want 2: the daemon holds a single pending id per agent and a second ask overwrites the first there, which is exactly why the room keeps its own list", cs.Len())
	}
	if top, _ := cs.Top(); top.RequestID != "r1" {
		t.Errorf("Top = %q, want r1: oldest first, because the agent that has been stopped longest is the one costing the most", top.RequestID)
	}
	if cs = cs.Settle("s1", "r1"); cs.Len() != 1 {
		t.Errorf("settling r1 left %d cards", cs.Len())
	}
	if top, _ := cs.Top(); top.RequestID != "r2" {
		t.Errorf("after settling r1 the top is %q, want r2", top.RequestID)
	}
}

func TestAWithdrawnAskIsTakenDownRatherThanLeftOfferingAnAnswerThatGoesNowhere(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "Write", "note.txt")).Add("s1", withdrawn("r1"))
	if cs.Len() != 0 {
		t.Error("an ask that was withdrawn is still on screen. A well-formed allow for a withdrawn request produces no frame, no error and no tool run - the operator would answer into the void and nothing would tell them so")
	}
}

// A withdrawal names one ask, and only that one is retired. The frame carries
// no session id and the corpus has exactly one cause behind it, so reading it
// as anything wider - "this agent's asks are gone", "an interrupt happened" -
// is a claim three recordings do not support.
func TestAWithdrawalRetiresOnlyTheAskItNames(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "Write", "note.txt")).
		Add("s1", ask("r2", "Bash", "ls")).
		Add("s1", withdrawn("r1"))
	if cs.Len() != 1 {
		t.Fatalf("Len = %d, want 1", cs.Len())
	}
	if top, _ := cs.Top(); top.RequestID != "r2" {
		t.Errorf("the withdrawal of r1 took down %q as well", top.RequestID)
	}
}

func TestReconcileTakesDownAnAskTheFleetNoLongerReportsAndAddsOneItDoes(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "Write", "note.txt"))
	cs = cs.Reconcile(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateIdle},                                              // r1 is gone
		{ID: "s2", State: rpc.StateBlocked, RequestIDs: []string{"r9"}, Tool: "Bash"}, // arrived while detached
	}})
	if _, ok := cs.byRequest("s1", "r1"); ok {
		t.Error("a card outlived the ask it names. A report carries an empty request id in both directions - a client must read it on every report rather than remembering the first one it saw")
	}
	got, ok := cs.byRequest("s2", "r9")
	if !ok {
		t.Fatal("an ask that arrived while this client was detached is not offered. The fleet report carries a request id for exactly this case, and a client that only ever learns asks from events leaves that agent stopped forever")
	}
	if got.Shape() != ShapePermission {
		t.Errorf("an ask known only from a report drew as shape %d: the report carries no payload, so a yes/no on a named tool is the honest extent of what is known", got.Shape())
	}
}

// A session blocked on two asks at once - a question and a permission from
// parallel tool calls - keeps both cards across a reconcile. The report names
// every outstanding ask, so a reconcile keyed on one would take the other down,
// stranding a live ask with the agent stopped behind it and nothing on screen.
func TestReconcileKeepsEveryAskAConcurrentlyBlockedSessionReports(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "AskUserQuestion", "")).
		Add("s1", ask("r2", "Write", "note.txt"))
	cs = cs.Reconcile(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateBlocked, RequestIDs: []string{"r1", "r2"}, Tool: "Write"},
	}})
	if cs.Len() != 2 {
		t.Fatalf("Len = %d, want 2: a reconcile against a report naming both asks took one card down, which is exactly the stranded-ask bug - the agent is stopped on it and it is on no surface", cs.Len())
	}
	if _, ok := cs.byRequest("s1", "r1"); !ok {
		t.Error("the question card was dropped: the report named it, and it is what the operator has to answer")
	}
	if _, ok := cs.byRequest("s1", "r2"); !ok {
		t.Error("the permission card was dropped")
	}
}

// And the other half a report cannot carry twice: a client that reattached onto
// a session already blocked on two asks learns both from the report alone.
func TestReconcileLearnsEveryAskThatArrivedWhileDetached(t *testing.T) {
	cs := Cards{}.Reconcile(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateBlocked, RequestIDs: []string{"r1", "r2"}, Tool: "Bash"},
	}})
	if cs.Len() != 2 {
		t.Fatalf("Len = %d, want 2: a reattaching client that learns only one of a session's asks leaves it stopped on the other forever", cs.Len())
	}
	if _, ok := cs.byRequest("s1", "r1"); !ok {
		t.Error("the first ask was not learned from the report")
	}
	if _, ok := cs.byRequest("s1", "r2"); !ok {
		t.Error("the second ask was not learned from the report")
	}
}

// A nil report is not "the fleet is empty". Nothing has been said, so nothing
// may be taken down - a client that cleared its cards on a missing report
// would drop a live ask every time a frame arrived without one.
func TestReconcileAgainstNothingChangesNothing(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "Write", "note.txt"))
	if cs.Reconcile(nil).Len() != 1 {
		t.Error("a nil report took a live ask down")
	}
}

// A duplicate is a duplicate, not a new ask - a reattach delivers the same one
// twice, once as an event and once through a report. And it must not move: a
// queue that reorders under the operator's hand is how somebody answers the
// wrong card.
func TestAnAskDeliveredTwiceIsOneCardAndKeepsItsPlace(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "Write", "note.txt")).
		Add("s2", ask("r2", "Bash", "ls")).
		Add("s1", ask("r1", "Write", "note.txt"))
	if cs.Len() != 2 {
		t.Fatalf("Len = %d, want 2: the same ask arrived twice and was promoted twice", cs.Len())
	}
	if top, _ := cs.Top(); top.RequestID != "r1" {
		t.Errorf("Top = %q, want r1: re-delivering the oldest ask moved it behind a newer one, so the card under the operator's hand changed while they were reading it", top.RequestID)
	}
}

// Writing a choice back keeps the card where it was, for the same reason.
func TestWritingAChoiceBackKeepsTheCardWhereItWas(t *testing.T) {
	first := recordedAsks(t, choiceFixture)[0]
	cs := Cards{}.Add("s1", first).Add("s2", ask("r2", "Bash", "ls"))
	top, _ := cs.Top()
	cs = cs.With(top.Pick(0, 1))

	if cs.Len() != 2 {
		t.Fatalf("Len = %d, want 2", cs.Len())
	}
	got, _ := cs.Top()
	if got.RequestID != first.RequestID {
		t.Fatalf("Top = %q, want the ask a choice was just made on", got.RequestID)
	}
	if !got.picked(0, 1) {
		t.Error("the choice did not survive being written back: the operator's answer is gone and the card looks untouched")
	}
}

// An ask settled while the operator was choosing must not come back when the
// choice is written. It is dead - answering it reaches nothing.
func TestWritingBackACardThatWasSettledDoesNotResurrectIt(t *testing.T) {
	cs := Cards{}.Add("s1", ask("r1", "Write", "note.txt"))
	top, _ := cs.Top()
	if got := cs.Settle("s1", "r1").With(top); got.Len() != 0 {
		t.Error("a card settled while it was being answered was put back on screen by the answer")
	}
}

// Nothing here mutates what a caller is holding. Cards are passed by value all
// over the room and the transcript keeps older copies; a slice written in
// place would rewrite history.
func TestEveryCardsMethodLeavesTheOneItWasCalledOnAlone(t *testing.T) {
	base := Cards{}.Add("s1", ask("r1", "Write", "note.txt")).arm(Card{AgentID: "s1", RequestID: "r1"}, cardDenyKey)
	top, _ := base.Top()

	for name, fn := range map[string]func() Cards{
		"Add":       func() Cards { return base.Add("s2", ask("r2", "Bash", "ls")) },
		"Settle":    func() Cards { return base.Settle("s1", "r1") },
		"Reconcile": func() Cards { return base.Reconcile(&rpc.Status{}) },
		"With":      func() Cards { return base.With(top.Move(0, 1)) },
		"arm":       func() Cards { return base.arm(top, cardAllowKey) },
		"disarm":    func() Cards { return base.disarm() },
	} {
		fn()
		if base.Len() != 1 {
			t.Fatalf("%s changed the Cards it was called on: Len = %d", name, base.Len())
		}
		if got, _ := base.Top(); got.RequestID != "r1" || got.Option != 0 {
			t.Errorf("%s changed the card it was called on: %+v", name, got)
		}
		// The arm is state a keypress reads, so a method that reached through
		// to it would settle a card against an arm the caller no longer holds.
		if !base.armedFor(top, cardDenyKey) {
			t.Errorf("%s changed the armed settle on the Cards it was called on: %+v", name, base.armed)
		}
	}
}

// A card is a value too, and Pick is the method a keypress calls on a copy the
// room is still drawing.
func TestPickLeavesTheCardItWasCalledOnAlone(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	_ = card.Pick(0, 1)
	if card.chosen(0) != noChoice {
		t.Error("Pick wrote through to the card it was called on, so an older copy of the room would draw a choice nobody had made there")
	}
}

// The room's own excuse table says these two kinds are drawn here rather than
// by roomBlock. This is the other half of that obligation: an excuse naming
// cards.go is worth nothing unless cards.go does something with the kind.
//
// Derived from notDrawnByRoomBlock rather than listed, so a third kind excused
// over there has to be handled here before the suite goes green again.
func TestEveryKindTheRoomExcusesToThisFileIsHandledHere(t *testing.T) {
	if len(notDrawnByRoomBlock) == 0 {
		t.Fatal("the room excuses no kinds to cards.go: this test is asserting nothing")
	}
	held := Cards{}.Add("s1", ask("r1", "Write", "note.txt"))
	for kind, why := range notDrawnByRoomBlock {
		ev := core.Event{Kind: kind, RequestID: "r1", Tool: &core.ToolCall{Name: "Write"}}
		promoted := Cards{}.Add("s1", ev).Len() == 1
		retired := held.Add("s1", ev).Len() == 0
		if !promoted && !retired {
			t.Errorf("roomBlock draws nothing for %q because %q - and cards.go neither promotes nor retires it either, so the event reaches no surface at all", kind, why)
		}
	}
}

// An ask with no request id cannot be answered, so promoting it would put a
// card on screen whose keys reach nothing.
func TestAnAskWithNoRequestIDIsNotOffered(t *testing.T) {
	if (Cards{}).Add("s1", ask("", "Write", "note.txt")).Len() != 0 {
		t.Error("an ask carrying no request id was promoted: nothing can name it, so every key on that card is a no-op")
	}
}
