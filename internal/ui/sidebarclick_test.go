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

// Below the takeover width exactly one column is drawn and the window follows
// the keys, so the new column is drawn in the group chat's place - the same
// frame a ⌃Y from the room gives. No second fallback is added.
func TestAClickInANarrowWindowTakesTheGroupChatsPlace(t *testing.T) {
	a := clickAgent(t, fleetApp(t, 110, 30, 2), "s1")
	byKey := fleetApp(t, 110, 30, 2).openRight("s1", "agent01")
	if got, want := a.regions().Cols, byKey.regions().Cols; !slices.Equal(got, want) {
		t.Fatalf("regions %v after a click, %v after ⌃Y", got, want)
	}
	if a.regions().Cols[0] != 0 || a.regions().Cols[1] == 0 {
		t.Errorf("regions %v: want the agent drawn alone and the room off screen", a.regions().Cols)
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
