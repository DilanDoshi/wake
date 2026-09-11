package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The reported bug: an agent's reply that appeared in the group chat did not
// appear in that agent's DM. It happens when the DM was never open while the
// room turn arrived (so the turn lives only on disk), and then the on-disk
// restore is dropped by the ask-vs-reply race - the ordinary case for an active
// agent. The room still holds the turn in memory, so a freshly opened DM seeds
// itself from the room, and that seed survives the race-drop.
func TestARoomReplyShowsInADMOpenedAfterwardDespiteTheHistoryRace(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("ruth") // s1 == ruth
	if _, open := a.dms["s1"]; open {
		t.Fatal("precondition: ruth's DM must not be open yet")
	}
	// A room-routed turn while ruth's DM has never been opened.
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "I am here and ready"})

	// Open ruth's DM. A live event races the on-disk history reply, so the reply
	// is dropped (historyArrived: events.len != held).
	a = a.openDMWith("s1", "ruth")
	a = a.observe("s1", core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "test"})
	a = a.historyArrived(rpc.Frame{Kind: rpc.FrameHistoryReply, SessionID: "s1", Events: []core.Event{
		{Kind: core.KindAssistantText, SessionID: "s1", Text: "I am here and ready", At: base},
	}})

	out := stripANSI(a.dms["s1"].View(70, 40))
	if !strings.Contains(out, "I am here and ready") {
		t.Errorf("ruth's room reply is missing from the DM opened afterward:\n%s", out)
	}
}

// The on-disk read stays authoritative: when it folds cleanly it supersedes the
// provisional room seed, so the turn is drawn exactly once - never doubled.
func TestTheDiskReadSupersedesTheRoomSeedWithoutDoubling(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("ruth")
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "unique-room-reply"})
	a = a.openDMWith("s1", "ruth")
	// No race: the disk read folds cleanly and carries the same turn.
	a = a.historyArrived(rpc.Frame{Kind: rpc.FrameHistoryReply, SessionID: "s1", Events: []core.Event{
		{Kind: core.KindAssistantText, SessionID: "s1", Text: "unique-room-reply", At: base},
	}})
	out := stripANSI(a.dms["s1"].View(70, 40))
	if n := strings.Count(out, "unique-room-reply"); n != 1 {
		t.Errorf("room turn should appear exactly once after a clean disk fold, got %d:\n%s", n, out)
	}
}

// A user turn the operator routed to the agent from the room seeds the DM too,
// headed "from the room" as the live echo would head it.
func TestTheRoomSeedIncludesTheOperatorsRoutedTurn(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("ruth").withDraft("@ruth ping")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter}) // routes "@ruth ping" from the room to ruth
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "pong-reply"})

	a = a.openDMWith("s1", "ruth")
	// A live event arrives before the reply, so a non-empty reply is dropped by
	// the genuine dm.events.len() != held race branch (not the len==0 early
	// return) - and the seeded routed turn and its reply must survive that drop.
	a = a.observe("s1", core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "later"})
	a = a.historyArrived(rpc.Frame{Kind: rpc.FrameHistoryReply, SessionID: "s1", Events: []core.Event{
		{Kind: core.KindAssistantText, SessionID: "s1", Text: "some-disk-tail", At: base},
	}})

	out := stripANSI(a.dms["s1"].View(70, 40))
	if !strings.Contains(out, "ping") || !strings.Contains(out, "pong-reply") {
		t.Errorf("the routed turn and its reply should both seed ruth's DM:\n%s", out)
	}
}

// /clear blanks the pane, and the provisional seed is the conversation as it was
// before the clear - so a session reset must drop it, not redraw the pre-clear
// room turns beneath the fresh conversation.
func TestSessionResetDropsTheRoomSeed(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("ruth")
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "pre-clear-room-reply"})
	a = a.openDMWith("s1", "ruth")
	// Race the read so the seed stays active.
	a = a.observe("s1", core.Event{Kind: core.KindUserText, SessionID: "s1", Text: "test"})
	a = a.historyArrived(rpc.Frame{Kind: rpc.FrameHistoryReply, SessionID: "s1", Events: []core.Event{
		{Kind: core.KindAssistantText, SessionID: "s1", Text: "pre-clear-room-reply", At: base},
	}})
	if !strings.Contains(stripANSI(a.dms["s1"].View(70, 40)), "pre-clear-room-reply") {
		t.Fatal("precondition: seed should be active before the clear")
	}

	a = a.observe("s1", core.Event{Kind: core.KindSessionReset, SessionID: "s1"})
	if got := stripANSI(a.dms["s1"].View(70, 40)); strings.Contains(got, "pre-clear-room-reply") {
		t.Errorf("/clear left the pre-clear room seed in the pane:\n%s", got)
	}
}

// A seed-only DM (opened after the room turn, no live event yet) still has a
// transcript the reader read, so leaving it marks a last-read boundary and the
// first live event to arrive renders under that boundary.
func TestLeavingASeedOnlyDMSetsALastReadBoundary(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("ruth")
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "seeded-room-reply"})
	a = a.openDMWith("s1", "ruth") // fresh DM: seeded, events still empty
	if a.dms["s1"].events.len() != 0 || len(a.dms["s1"].seed) == 0 {
		t.Fatalf("precondition: DM should be seed-only (events=%d seed=%d)", a.dms["s1"].events.len(), len(a.dms["s1"].seed))
	}
	a = a.withDM("s1", a.dms["s1"].Leave())
	if got := len(a.dms["s1"].marks); got == 0 {
		t.Fatalf("leaving a seed-only DM set no last-read boundary (marks=%d)", got)
	}
	// A live event now arrives; the boundary should sit above it.
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "arrived-after-leaving"})
	if m := a.dms["s1"].marks; len(m) == 0 || m[0] != 0 {
		t.Errorf("the boundary is not at index 0 (marks=%v): the first unseen event has no marker", m)
	}
}
