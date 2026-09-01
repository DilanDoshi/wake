package ui

// The five keys the room brought, and what each of them does.
//
// The legend guard proves they are *taken* - a bijection between legendEntries
// and the cases in App.key's switch - and a case that returns the receiver
// satisfies it while doing nothing at all. That is the same lie the legend rule
// exists for, arriving one level in, so each of these has to be asserted on
// what changed rather than on whether the key was handled.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// blockedFleet is three agents, one of them stopped on an ask.
func blockedFleet() rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Dir: "/repos/wake", State: rpc.StateIdle},
		{ID: "s2", Name: "john", Dir: "/repos/api", State: rpc.StateWorking},
		{ID: "s3", Name: "marco", Dir: "/repos/api", State: rpc.StateBlocked, RequestIDs: []string{"r9"}, Tool: "Bash"},
	}}}
}

// ⌃D is drawn on every collapsed reply in the room - "this one is long, go and
// read it in a DM" - and named nothing for the whole of Phase 1.
func TestCtrlDOpensAConversationWithTheAgentTheCursorIsOn(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(blockedFleet())
	a.roster.Selected = "s2"

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if a.focus != "s2" || a.focus == "" {
		t.Fatalf("⌃D left open=%q focus=%v, want a focused DM with john", a.focus, a.focus)
	}
	if !strings.Contains(shown(a), agentPrefix+"john") {
		t.Errorf("the DM pane does not name the agent it was opened for:\n%s", shown(a))
	}
	// And typing now reaches that agent rather than the room, which is the
	// whole of what focus means.
	a = a.withDraft("run the tests")
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if f := sentFrame(t, a, cmd); f.SessionID != "s2" || f.Text != "run the tests" {
		t.Errorf("a message typed after ⌃D went to %+v, want john with no routing", f)
	}
}

// With nobody to open one with, ⌃D says so. A key that is advertised, taken,
// and silently does nothing is the failure the legend rule exists for wearing a
// different hat.
func TestCtrlDOnAnEmptyFleetSaysSoRatherThanDoingNothing(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})

	if a.focus != "" {
		t.Fatalf("⌃D opened a conversation with %q on a fleet with nobody in it", a.focus)
	}
	if _, said := notice.Latest(); !said {
		t.Error("⌃D did nothing and said nothing")
	}
}

// ⌃W puts the room back across the pane and keeps what the conversation held -
// which is the whole reason dms is a map rather than one DM.
func TestCtrlWClosesTheConversationAndKeepsIt(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(blockedFleet())
	a.roster.Selected = "s2"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "the retry header is fixed",
	}})

	closed, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlW})
	if closed.focus != "" {
		t.Fatalf("⌃W left the keys on %q, want the room", closed.focus)
	}
	// The column is gone rather than drawn at zero: a closed conversation is out
	// of the grid, and a grid entry drawn at no width is the phantom pane the
	// old ShowDM bool made possible.
	if r := closed.regions(); len(r.Cols) != 1 {
		t.Errorf("the layout still holds %d columns after the only conversation closed, want 1", len(r.Cols))
	}

	reopened, _ := pressKey(closed, tea.KeyMsg{Type: tea.KeyCtrlD})
	if !strings.Contains(shown(reopened), "retry header") {
		t.Errorf("reopening the conversation lost what it held, which is the only reason dms is a map:\n%s", shown(reopened))
	}
}

// ⌃R is the sidebar key. Asserted on the columns rather than on a flag, because
// the flag is what a case that does nothing else would still set.
//
// ⌃G, the left workspaces sidebar, is hidden for now (see groups.go), so only
// the activity sidebar has a key here.
func TestTheSidebarKeysOpenAndCloseTheirColumns(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(blockedFleet())

	for _, tc := range []struct {
		what string
		key  tea.KeyType
		of   func(Regions) int
	}{
		{"⌃R", tea.KeyCtrlR, func(r Regions) int { return r.Roster }},
	} {
		open := tc.of(a.regions())
		if open == 0 {
			t.Fatalf("%s's sidebar is already closed at 200 columns, so this proves nothing", tc.what)
		}
		closed, _ := pressKey(a, tea.KeyMsg{Type: tc.key})
		if got := tc.of(closed.regions()); got != 0 {
			t.Errorf("%s left its sidebar at %d columns", tc.what, got)
		}
		if closed.room.width <= a.room.width {
			t.Errorf("%s closed a sidebar and the room did not take the columns: %d then %d", tc.what, a.room.width, closed.room.width)
		}
		again, _ := pressKey(closed, tea.KeyMsg{Type: tc.key})
		if got := tc.of(again.regions()); got != open {
			t.Errorf("%s pressed twice left its sidebar at %d columns, want the %d it started at", tc.what, got, open)
		}
	}
}

// Spec §6's next-agent jump is ⇧⇥ - ⇥ carried it until this build gave ⇥ to
// pane focus, and ⌃⇧A is not bindable in bubbletea v1.3.10 at all. See
// docs/notes/decisions.md. That it opens the blocked agent is asserted in
// focus_test.go beside the rest of the two tab keys; this is the other half:
// with nothing blocked it moves nothing and says so, which is Roster.Next's own
// rule - a key that moves a cursor when there is nothing to move it to is a key
// that lies about the fleet.
func TestShiftTabWithNothingBlockedMovesNothing(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a.roster.Selected = "s1"

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlX})
	if a.focus != "" {
		t.Errorf("⌃X opened %q with nothing waiting on anybody", a.focus)
	}
	if _, said := notice.Latest(); !said {
		t.Error("⇧⇥ did nothing and said nothing")
	}
}

// A DM held by a discarded model must not keep growing. Bubble Tea hands models
// around by value and dms is a map, so without the copy in withDM an event
// folded into the newest App reaches every copy that ever existed - which is
// the same defect Fleet copies to avoid, one type up.
func TestFoldingIntoAConversationDoesNotReachAModelSomebodyElseIsHolding(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(blockedFleet())
	a.roster.Selected = "s2"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})

	after := a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: "a later message",
	}})

	if strings.Contains(shown(a), "a later message") {
		t.Errorf("appending to a conversation changed a model somebody else was holding:\n%s", shown(a))
	}
	if !strings.Contains(shown(after), "a later message") {
		t.Errorf("the returned model is missing the event:\n%s", shown(after))
	}
}

// The map above is keyed on *DM, so copy-on-write now has two ways to break that
// a value map could not: a write that mutated the shared map in place, and a
// reader (dmFor) that wrote a field through the shared pointer. This pins both by
// reading the map values directly rather than the rendered view. See withDM.
func TestWithDMAndDmForLeaveAnOlderAppUnchanged(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")
	a = a.openDMWith("s1", "alex")
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindAssistantText, SessionID: "s1", Text: "first",
	}})

	a0 := a // shares the dms map and its *DM pointers
	wantLen := a0.dms["s1"].events.len()
	wantAgent := a0.dms["s1"].Agent // zero: the stored DM never carries the fleet's agent

	// dmFor sets Agent for the draw; it must deref to a local, not write through.
	_ = a0.dmFor("s1")
	// A later write to the same session, folded the way an event arrives.
	a1 := a0.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "second"})
	_ = a1.dmFor("s1")

	if got := a0.dms["s1"].events.len(); got != wantLen {
		t.Errorf("the older App's conversation grew from %d to %d events: withDM aliased the map rather than replacing a pointer", wantLen, got)
	}
	if got := a0.dms["s1"].Agent; got != wantAgent {
		t.Errorf("dmFor wrote Agent %+v through the shared pointer; the older App held %+v", got, wantAgent)
	}
	if got := a1.dms["s1"].events.len(); got != wantLen+1 {
		t.Errorf("the write did not take: a1 holds %d events, want %d", got, wantLen+1)
	}
}
