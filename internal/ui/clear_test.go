package ui

// What a /clear does to an open conversation - see clear.go. The shared
// assert helpers (assertShows/assertHides/visible) live in dm_test.go.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A /clear reaches the agent, which mints a new session id and drops the
// conversation it had; Wake sees that as a KindSessionReset. The pane it lands
// in blanks back to a fresh conversation - the old transcript is history the
// model no longer has - rather than drawing a marker beneath it. The reopen
// path reads the post-clear transcript off disk (daemon/history.go); this is
// the live half, for a pane already on screen.
//
// The event is built the way the decoder produces one: SessionID is the id
// that died, and there is no Text, because the reset frame names no successor.
func TestSessionResetClearsTheConversation(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "earlier reply"})
	assertShows(t, d, 60, 20, "earlier reply")

	d = d.Append(core.Event{Kind: core.KindSessionReset, SessionID: "old-id"})
	assertHides(t, d, 60, 20, "earlier reply")
	assertHides(t, d, 60, 20, "session reset")

	// The blanked pane is a live conversation, not a frozen one: the next turn
	// draws into it, and the cleared turn stays gone.
	d = d.Append(core.Event{Kind: core.KindAssistantText, Text: "after clear"})
	assertShows(t, d, 60, 20, "after clear")
	assertHides(t, d, 60, 20, "earlier reply")
}

// A reset while the pane is viewing a subagent must fall back to the (now empty)
// conversation, not strand the pane on a dispatch that belonged to the gone turn.
func TestSessionResetWhileViewingSubagentReturnsToConversation(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "parent said"}).
		Append(core.Event{Kind: core.KindAssistantText, Text: "sub said",
			Subagent: &core.Subagent{Dispatch: "toolu_abc", Type: "explorer"}})
	d = d.Viewing("toolu_abc")
	if d.Viewed() != "toolu_abc" {
		t.Fatalf("precondition: expected to be viewing the subagent, got %q", d.Viewed())
	}
	d = d.Append(core.Event{Kind: core.KindSessionReset, SessionID: "old"})
	if d.Viewed() != "" {
		t.Errorf("after reset, Viewed() = %q, want \"\" (back on the conversation)", d.Viewed())
	}
	assertHides(t, d, 60, 20, "parent said")
	assertHides(t, d, 60, 20, "sub said")
}

// A reset drops the task board, and a fresh board built afterward shows only the
// new items - the board is the DM's own fold, so blanking it must reset that.
func TestSessionResetClearsTheTaskBoard(t *testing.T) {
	create := func(text, active string) core.Event {
		return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
			Checklist: &core.ChecklistOp{Text: text, ActiveForm: active},
		}}
	}
	d := NewDM("s1", "alex").SetSize(60, 24).Append(create("old task item", "doing old"))
	assertShows(t, d, 60, 24, "old task item")
	d = d.Append(core.Event{Kind: core.KindSessionReset, SessionID: "old"})
	assertHides(t, d, 60, 24, "old task item")
	d = d.Append(create("new task item", "doing new"))
	assertShows(t, d, 60, 24, "new task item")
	assertHides(t, d, 60, 24, "old task item")
}

// A pre-clear history reply still in flight when a /clear lands is dropped, not
// folded onto the blanked pane.
//
// The reset returns events.len() to the count the ask recorded at open, so the
// count guard alone would read "nothing arrived since" and Before would prepend
// the pre-clear transcript - resurrecting history the model no longer has, the
// one thing this whole feature fights. observe drops the ask on the reset and
// historyArrived checks membership, so the late reply is dropped on arrival.
func TestHistoryForAClearedConversationIsDropped(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex")
	a, _ = a.takeHistoryAsks()

	// A turn streams in after the ask, then a /clear blanks the pane - which
	// takes events.len() back to the zero the ask recorded at open.
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "said before the clear"})
	a = a.observe("s1", core.Event{Kind: core.KindSessionReset, SessionID: "s1"})

	// The pre-clear history reply, delayed off a slow disk, arrives now.
	a = a.historyArrived(rpc.Frame{
		Kind: rpc.FrameHistoryReply, SessionID: "s1",
		Events: []core.Event{
			{Kind: core.KindUserText, SessionID: "s1", Text: "older question"},
			{Kind: core.KindAssistantText, SessionID: "s1", Text: "older answer"},
		},
	})
	if got := a.dms["s1"].events.len(); got != 0 {
		t.Errorf("a cleared conversation holds %d events after a stale history reply, want 0: the pre-clear transcript must not come back", got)
	}
	assertHides(t, *a.dms["s1"], 60, 20, "older answer")
}
