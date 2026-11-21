package ui

// A click on a sidebar agent row opens that agent in a new column at the grid's
// right end, next to the sidebar - never in the pane that had the keys.

import (
	"fmt"
	"slices"
	"testing"
)

// fleetApp is the room with the sidebar drawn and n agents on it, s1..sN.
func fleetApp(t testing.TB, w, h, n int) App {
	t.Helper()
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("agent%02d", i+1)
	}
	fresh(t)
	a := NewRoomApp(&recorder{}, Stream{}, nil).withAgents(names...).withSize(w, h)
	a.layout.ShowRoster = true
	return a.resizePanes()
}

// rowOfAgent is the screen row of id's own roster row.
func rowOfAgent(t testing.TB, a App, id string) int {
	t.Helper()
	for y := range a.paneHeight() {
		if spot, ok := a.clickedRow(y); ok && spot.agent.ID == id && spot.dispatch == "" && spot.header == "" {
			return y
		}
	}
	t.Fatalf("no roster row is %s's", id)
	return 0
}

// clickAgent clicks id's row.
func clickAgent(t testing.TB, a App, id string) App {
	t.Helper()
	a, _ = click(a, sidebarX(a), rowOfAgent(t, a, id))
	return a
}

// tops is the conversation each column holds, the room's as "".
func tops(a App) []string {
	out := make([]string, len(a.grid.Cols))
	for i, c := range a.grid.Cols {
		out[i] = c.Top
	}
	return out
}

func TestAClickOnAnAgentOpensItInANewColumnNextToTheSidebar(t *testing.T) {
	a := clickAgent(t, fleetApp(t, 200, 40, 3), "s2")
	if want := []string{"", "s2"}; !slices.Equal(tops(a), want) {
		t.Fatalf("columns %q, want %q: the click opens a column and replaces nothing", tops(a), want)
	}
	if a.focus != "s2" {
		t.Errorf("focus %q, want the opened agent", a.focus)
	}
	if got := a.regions().Cols[0]; got == 0 {
		t.Error("the room is not drawn beside the new column at 200 columns")
	}
}

// The new column goes at the right end whichever pane holds the keys, so ten
// clicks read left to right in the order they were made.
func TestEachClickAddsAColumnAtTheRightEndWhateverHoldsTheKeys(t *testing.T) {
	a := clickAgent(t, fleetApp(t, 200, 40, 4), "s1")
	a = clickAgent(t, a, "s2")
	a = a.refocus("s1") // the middle of three columns after the next click
	a = clickAgent(t, a, "s3")
	if want := []string{"", "s1", "s2", "s3"}; !slices.Equal(tops(a), want) {
		t.Errorf("columns %q, want %q", tops(a), want)
	}
	if a.focus != "s3" {
		t.Errorf("focus %q, want the newest column", a.focus)
	}
}

// A click on an agent already on screen is a focus change: one conversation in
// two panes is two composers writing to one agent.
func TestAClickOnAnAgentAlreadyOpenFocusesItsPane(t *testing.T) {
	a := clickAgent(t, fleetApp(t, 200, 40, 3), "s1")
	a = clickAgent(t, a, "s2")
	before := tops(a)
	a = clickAgent(t, a, "s1")
	if !slices.Equal(tops(a), before) {
		t.Errorf("columns %q after clicking an open agent, want %q", tops(a), before)
	}
	if a.focus != "s1" {
		t.Errorf("focus %q, want the clicked agent's pane", a.focus)
	}
}

// Where the window carries one column, a click replaces the pane on screen as
// opening a conversation always has: from the room it takes the column beside it,
// since the room is never replaced, and the group chat is drawn no more.
func TestAClickFromTheRoomInASingleColumnLayoutTakesColumnOne(t *testing.T) {
	a := clickAgent(t, fleetApp(t, 110, 30, 3), "s1")
	if want := []string{"", "s1"}; !slices.Equal(tops(a), want) || a.focus != "s1" {
		t.Fatalf("columns %q focus %q, want s1 in the column beside the room", tops(a), a.focus)
	}
	if got := a.regions().Cols; got[0] != 0 || got[1] == 0 {
		t.Errorf("regions %v: want the agent drawn alone and the room off screen", got)
	}
	byKey := fleetApp(t, 110, 30, 3).openDMWith("s1", "agent01")
	if !slices.Equal(a.regions().Cols, byKey.regions().Cols) || !slices.Equal(tops(a), tops(byKey)) {
		t.Errorf("a click gave %v %q, opening a conversation gives %v %q", a.regions().Cols, tops(a), byKey.regions().Cols, tops(byKey))
	}
}

// A click from a conversation replaces it - one agent on screen, the next in its
// place - and what was replaced keeps its transcript.
func TestAClickFromAConversationInASingleColumnLayoutReplacesIt(t *testing.T) {
	a := clickAgent(t, fleetApp(t, 110, 30, 3), "s1")
	a = clickAgent(t, a, "s2")
	if want := []string{"", "s2"}; !slices.Equal(tops(a), want) || a.focus != "s2" {
		t.Fatalf("columns %q focus %q after a second click, want s2 in s1's place", tops(a), a.focus)
	}
	if _, kept := a.dms["s1"]; !kept {
		t.Error("the replaced conversation lost its transcript")
	}
}

// A click from the room, with a conversation already in column 1, takes that
// column rather than adding one.
func TestAClickFromTheRoomWithAConversationOpenTakesItsColumn(t *testing.T) {
	a := clickAgent(t, fleetApp(t, 110, 30, 3), "s1")
	a = clickAgent(t, a.showRoom(), "s2")
	if want := []string{"", "s2"}; !slices.Equal(tops(a), want) {
		t.Errorf("columns %q, want s2 in column 1", tops(a))
	}
}

// Nothing piles up off screen: ten clicks leave one conversation column beside the
// room, not ten.
func TestTenClicksInASingleColumnLayoutLeaveOneConversationColumn(t *testing.T) {
	a := fleetApp(t, 110, 30, 12)
	for i := 1; i <= 10; i++ {
		a = clickAgent(t, a, fmt.Sprintf("s%d", i))
	}
	if want := []string{"", "s10"}; !slices.Equal(tops(a), want) || a.grid.Cols[1].Bottom != "" {
		t.Fatalf("columns %+v after ten clicks, want the room and s10 alone", a.grid.Cols)
	}
}

// The line is the layout's own: a window one column short of the takeover width
// replaces, one at it adds a column. Width is the frame's, the window less its margins.
func TestTheSingleColumnLineIsTheLayoutsCapacity(t *testing.T) {
	for _, tc := range []struct {
		window  int
		columns int
	}{{dmTakeoverColumns - 1, 2}, {dmTakeoverColumns, 3}} {
		a := fleetApp(t, tc.window-2*SideMargin, 30, 4)
		a = clickAgent(t, clickAgent(t, a, "s1"), "s2")
		if len(a.grid.Cols) != tc.columns {
			t.Errorf("a %d-column window holds %d columns after two clicks, want %d", tc.window, len(a.grid.Cols), tc.columns)
		}
	}
}

// Ten clicks leave ten columns and no cap: the window slides, the room stays in
// the grid. PR-body fact, pinned so a cap added later is a decision, not a drift.
func TestTenClicksLeaveTenColumnsAndNothingCapsThem(t *testing.T) {
	a := fleetApp(t, 200, 40, 12)
	for i := 1; i <= 10; i++ {
		a = clickAgent(t, a, fmt.Sprintf("s%d", i))
	}
	if got := len(a.grid.Cols); got != 11 {
		t.Fatalf("%d columns after ten clicks, want 11 (the room and ten)", got)
	}
	if a.grid.Cols[0].Top != "" {
		t.Error("the room is no longer column 0")
	}
	drawn := a.regions().Drawn()
	if drawn >= len(a.grid.Cols) || !slices.Contains(a.drawnIDs(), "s10") {
		t.Errorf("drew %d of %d columns with s10 among them %v", drawn, len(a.grid.Cols), a.drawnIDs())
	}
}

// A window that shrank to one column still holds every conversation in the grid:
// a click replaces the pane the window shows (the one holding the keys), keeps the
// others where they are, and the agent is drawn - from the room it takes column 1.
func TestAClickAfterTheWindowShrankReplacesTheShownPane(t *testing.T) {
	a := fleetApp(t, 200, 40, 6)
	for _, id := range []string{"s1", "s2", "s3"} {
		a = clickAgent(t, a, id)
	}
	a = settle(a.withSize(100, 40))
	if a.layout.Capacity() != 1 || len(a.grid.Cols) != 4 {
		t.Fatalf("capacity %d with %d columns: the test proves nothing", a.layout.Capacity(), len(a.grid.Cols))
	}
	a = clickAgent(t, a, "s5")
	if want := []string{"", "s1", "s2", "s5"}; !slices.Equal(tops(a), want) || !slices.Contains(a.drawnIDs(), "s5") {
		t.Errorf("columns %q drawn %v, want s5 in the focused column's place and drawn", tops(a), a.drawnIDs())
	}
	a = clickAgent(t, a.showRoom(), "s6")
	if want := []string{"", "s6", "s2", "s5"}; !slices.Equal(tops(a), want) || a.focus != "s6" || !slices.Contains(a.drawnIDs(), "s6") {
		t.Errorf("columns %q focus %q drawn %v, want s6 in column 1 and drawn", tops(a), a.focus, a.drawnIDs())
	}
}
