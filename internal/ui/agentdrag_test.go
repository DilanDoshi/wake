package ui

// Dragging an agent out of the sidebar and dropping it on the panes.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
)

// dragFleet is a 200x40 window with the sidebar, four agents, and alex (s1) open
// beside the room, so a drop has two columns to land on.
func dragFleet(t testing.TB) App {
	t.Helper()
	// Sized after the open, so the history ask it queued has been drained.
	return fleetApp(t, 200, 40, 4).openRight("s1", "agent01").withSize(200, 40)
}

// grab presses an agent's roster row.
func grabAgent(t testing.TB, a App, id string) App {
	t.Helper()
	a, _ = a.mouse(pressAt(sidebarX(a), rowOfAgent(t, a, id)))
	return a
}

// motionTo is one cell of motion with the left button down.
func motionTo(a App, x, y int) App {
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y})
	return a
}

// letGoAt releases the button on a cell.
func letGoAt(a App, x, y int) App {
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: y})
	return a
}

// pullAgent grabs an agent and moves it to a cell: past the slop first, then
// there, as a hand does.
func pullAgent(t testing.TB, a App, id string, x, y int) App {
	t.Helper()
	a = grabAgent(t, a, id)
	a = motionTo(a, sidebarX(a)-dragSlop-1, a.dragged.y0)
	return motionTo(a, x, y)
}

// bottomHalf and topHalf are rows inside a full-height pane.
func bottomHalf(a App) int { return a.paneHeight() - 2 }
func topHalf() int         { return 2 }

func TestAPressOnAnAgentRowArmsADragAndSelectsNothing(t *testing.T) {
	a := dragFleet(t)
	a.sel, a.selecting = selection{onScreen: true, anchor: point{line: 3, col: 3}, head: point{line: 3, col: 9}}, true
	a = grabAgent(t, a, "s2")
	if a.dragged.id != "s2" || a.dragged.live {
		t.Fatalf("press armed %+v, want s2 armed and not yet live", a.dragged)
	}
	if a.selecting || !a.sel.empty() {
		t.Errorf("a press on an agent row left a selection %+v: that drag moves the agent now", a.sel)
	}
}

// Headers and blank cells keep drag-to-copy; only an agent row is a drag source.
func TestAPressOnAHeaderStillTakesTheScreenSelection(t *testing.T) {
	a := dragFleet(t)
	a, _ = a.mouse(pressAt(sidebarX(a), searchBarRows)) // the `all` row, under the search bar
	if a.dragged.id != "" || !a.selecting || !a.sel.onScreen {
		t.Errorf("a press on the all header: drag %+v, selecting %v, sel %+v", a.dragged, a.selecting, a.sel)
	}
}

func TestAMoveShorterThanTheSlopIsStillAClick(t *testing.T) {
	a := grabAgent(t, dragFleet(t), "s2")
	a = motionTo(a, a.dragged.x0-dragSlop+1, a.dragged.y0)
	if a.dragged.live {
		t.Fatalf("a %d-cell move went live", dragSlop-1)
	}
	a = letGoAt(a, a.dragged.x0-dragSlop+1, a.dragged.y0)
	if want := []string{"", "s1", "s2"}; !slices.Equal(tops(a), want) || a.focus != "s2" {
		t.Errorf("columns %q focus %q, want the click's new column", tops(a), a.focus)
	}
}

func TestDropZones(t *testing.T) {
	a := dragFleet(t).openBelow("s2", "agent02") // s1 over s2, in the second column
	r := a.regions()
	room, split := midOf(r, 0), midOf(r, 1)
	upper, lower := a.layout.SplitRowsIn(1, a.paneHeight())
	cases := []struct {
		name string
		x, y int
		want dropZone
	}{
		{"over the sidebar", sidebarX(a), 5, dropZone{kind: zoneNone}},
		{"the room's top half", room, topHalf(), dropZone{kind: zoneColumn}},
		{"the room's bottom half", room, bottomHalf(a), dropZone{kind: zoneBelow, pane: ""}},
		{"the row above the room's middle", room, a.paneHeight()/2 - 1, dropZone{kind: zoneColumn}},
		{"the room's middle row is its bottom half", room, a.paneHeight() / 2, dropZone{kind: zoneBelow, pane: ""}},
		{"the row above the upper pane's middle", split, upper/2 - 1, dropZone{kind: zoneColumn}},
		{"the upper pane's middle row is its bottom half", split, upper / 2, dropZone{kind: zoneBelow, pane: "s1"}},
		{"a divider", dividerX(r, 0), 5, dropZone{kind: zoneColumn}},
		{"the upper pane's top half", split, 2, dropZone{kind: zoneColumn}},
		{"the upper pane's bottom half takes the lower slot", split, upper - 2, dropZone{kind: zoneBelow, pane: "s1"}},
		{"the rule between them", split, upper, dropZone{kind: zoneColumn}},
		{"the lower pane's bottom half has nothing below it", split, upper + dividerHeight + lower - 2, dropZone{kind: zoneColumn}},
		{"the awareness strip", room, a.paneHeight(), dropZone{kind: zoneColumn}},
	}
	for _, c := range cases {
		if got := a.dropZoneFor("s3", c.x, c.y); got != c.want {
			t.Errorf("%s (%d,%d): zone %+v, want %+v", c.name, c.x, c.y, got, c.want)
		}
	}
	// An agent already on screen is focused wherever it is dropped but the sidebar.
	if got := a.dropZoneFor("s2", room, topHalf()); got != (dropZone{kind: zoneFocus, pane: "s2"}) {
		t.Errorf("an open agent's drop: %+v, want its pane focused", got)
	}
	if got := a.dropZoneFor("s2", sidebarX(a), 5); got.kind != zoneNone {
		t.Errorf("an open agent over the sidebar: %+v, want a cancel", got)
	}
}

// A column too short to draw two panes has no bottom half to drop on.
func TestAShortColumnHasNoBelowZone(t *testing.T) {
	a := fleetApp(t, 200, 12, 3)
	if _, bottom := a.layout.SplitRowsIn(0, a.paneHeight()); bottom != 0 {
		t.Fatalf("a %d-row pane splits (bottom %d): the test proves nothing", a.paneHeight(), bottom)
	}
	if got := a.dropZoneFor("s2", midOf(a.regions(), 0), a.paneHeight()-1); got.kind != zoneColumn {
		t.Errorf("zone %+v in a pane too short to split, want a new column", got)
	}
}

func TestDroppingOnAPanesBottomHalfOpensBelowIt(t *testing.T) {
	a := dragFleet(t)
	r := a.regions()
	a = pullAgent(t, a, "s3", midOf(r, 0), bottomHalf(a))
	a = letGoAt(a, midOf(r, 0), bottomHalf(a))
	if a.grid.Cols[0].Bottom != "s3" || len(a.grid.Cols) != 2 {
		t.Fatalf("grid %+v, want s3 under the room and no new column", a.grid.Cols)
	}
	if a.focus != "s3" || a.dragged.id != "" {
		t.Errorf("focus %q, drag %+v after the drop", a.focus, a.dragged)
	}
}

// One split per column: dropping below the upper pane of a split column takes its
// lower slot, and what was there is off screen, not lost.
func TestDroppingBelowASplitColumnTakesItsLowerSlot(t *testing.T) {
	a := dragFleet(t).openBelow("s2", "agent02")
	r := a.regions()
	upper, _ := a.layout.SplitRowsIn(1, a.paneHeight())
	a = pullAgent(t, a, "s3", midOf(r, 1), upper-2)
	a = letGoAt(a, midOf(r, 1), upper-2)
	if c := a.grid.Cols[1]; c.Top != "s1" || c.Bottom != "s3" {
		t.Fatalf("column %+v, want s1 over s3", c)
	}
	if _, kept := a.dms["s2"]; !kept {
		t.Error("the displaced conversation lost its transcript")
	}
}

func TestDroppingAnywhereElseOpensANewColumnAtTheRightEnd(t *testing.T) {
	a := dragFleet(t)
	r := a.regions()
	a = pullAgent(t, a, "s3", midOf(r, 0), topHalf())
	a = letGoAt(a, midOf(r, 0), topHalf())
	if want := []string{"", "s1", "s3"}; !slices.Equal(tops(a), want) || a.focus != "s3" {
		t.Errorf("columns %q focus %q, want s3 in a new column", tops(a), a.focus)
	}
}

func TestLettingGoBackOnTheSidebarCancels(t *testing.T) {
	a := dragFleet(t)
	was, focus := tops(a), a.focus
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	a = motionTo(a, sidebarX(a), 8)
	a = letGoAt(a, sidebarX(a), 8)
	if !slices.Equal(tops(a), was) || a.focus != focus || a.dragged.id != "" {
		t.Errorf("columns %q focus %q drag %+v after dropping back on the sidebar, want nothing changed", tops(a), a.focus, a.dragged)
	}
}

func TestDroppingAnOpenAgentFocusesItsPaneAndMovesNothing(t *testing.T) {
	a := dragFleet(t).refocus("")
	was := tops(a)
	a = pullAgent(t, a, "s1", midOf(a.regions(), 0), bottomHalf(a))
	a = letGoAt(a, midOf(a.regions(), 0), bottomHalf(a))
	if !slices.Equal(tops(a), was) || a.grid.Cols[0].Bottom != "" || a.focus != "s1" {
		t.Errorf("columns %+v focus %q: want s1's pane focused and the grid as it was", a.grid.Cols, a.focus)
	}
}

// Esc cancels the drag and is consumed: it must not also interrupt the agent in
// the focused pane.
func TestEscCancelsADragAndInterruptsNothing(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	was := tops(a)
	got, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if got.dragged.id != "" {
		t.Error("esc left the drag armed")
	}
	if cmd != nil {
		t.Errorf("esc wrote %+v: it stopped a turn as well as the drag", sentFrames(t, got, cmd))
	}
	if !slices.Equal(tops(got), was) || got.focus != "s1" {
		t.Errorf("columns %q focus %q after esc", tops(got), got.focus)
	}
	// The release that follows opens nothing.
	got = letGoAt(got, midOf(got.regions(), 0), topHalf())
	if !slices.Equal(tops(got), was) {
		t.Errorf("a release after esc opened %q", tops(got))
	}
}

// Any other key ends the drag too and does its own job; a drop must never land
// after somebody typed.
func TestAnotherKeyEndsTheDragAndStillActs(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	got, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if got.dragged.id != "" {
		t.Error("a typed key left the drag armed")
	}
	if got.composer().Value() != "x" {
		t.Errorf("the key was swallowed: draft %q", got.composer().Value())
	}
}

// 1002 reports motion only while a button is held, so motion with no button is a
// release that never arrived.
func TestMotionWithNoButtonEndsTheDrag(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonNone, X: 5, Y: 5})
	if a.dragged.id != "" {
		t.Error("a buttonless motion left the drag armed")
	}
}

func TestAResizeEndsTheDrag(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	m, _ := a.Update(tea.WindowSizeMsg{Width: 180, Height: 40})
	if m.(App).dragged.id != "" {
		t.Error("a resize left the drag armed over a layout it was measured against")
	}
}

// Press-time target: a report arriving mid-drag still drops the agent that was
// grabbed, as a click opens the press-time agent.
func TestADropOpensTheAgentThatWasGrabbed(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	a = a.withAgents("a", "b", "c", "d") // a new report names the same ids differently
	a = letGoAt(a, midOf(a.regions(), 0), topHalf())
	if a.focus != "s3" {
		t.Errorf("focus %q, want the grabbed agent s3", a.focus)
	}
}

// An agent the fleet no longer holds opens nothing.
func TestADropOfAnAgentThatLeftOpensNothing(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	a.fleet = a.fleet.drop(map[string]struct{}{"s3": {}})
	a = letGoAt(a, midOf(a.regions(), 0), topHalf())
	if a.grid.Has("s3") {
		t.Error("a vanished agent was opened")
	}
}

// shiftPress is the left button going down with Shift held, the report a terminal
// that delivers Shift-clicks sends.
func shiftPress(x, y int) tea.MouseMsg {
	m := pressAt(x, y)
	m.Shift = true
	return m
}

func shiftClickAgent(t testing.TB, a App, id string) App {
	t.Helper()
	x, y := sidebarX(a), rowOfAgent(t, a, id)
	a, _ = a.mouse(shiftPress(x, y))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: y})
	return a
}

// A Shift-click is ⌃B from the pointer: below the pane that holds the keys.
func TestAShiftClickOpensBelowTheFocusedPane(t *testing.T) {
	a := dragFleet(t) // s1 holds the keys, in the second column
	a = shiftClickAgent(t, a, "s3")
	if c := a.grid.Cols[1]; c.Top != "s1" || c.Bottom != "s3" || len(a.grid.Cols) != 2 {
		t.Fatalf("columns %+v, want s3 under s1", a.grid.Cols)
	}
	if a.focus != "s3" {
		t.Errorf("focus %q, want the opened agent", a.focus)
	}
}

// From a lower pane there is nothing below: the same refusal ⌃B gives, and no
// cursor move for a click that did nothing.
func TestAShiftClickFromALowerPaneIsRefusedAsCtrlBIs(t *testing.T) {
	a := dragFleet(t).openBelow("s2", "agent02") // s2 is the lower pane and holds the keys
	was, sel := tops(a), a.roster
	a = shiftClickAgent(t, a, "s3")
	if !slices.Equal(tops(a), was) || a.grid.Has("s3") {
		t.Fatalf("columns %+v after a refused Shift-click", a.grid.Cols)
	}
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), noRowBelow) {
		t.Errorf("notice %v, want %q", n, noRowBelow)
	}
	if a.roster != sel {
		t.Errorf("the refused click moved the sidebar cursor: %+v, was %+v", a.roster, sel)
	}
}

// Shift on an agent already on screen is a focus change, as a plain click is.
func TestAShiftClickOnAnOpenAgentFocusesIt(t *testing.T) {
	a := dragFleet(t).refocus("")
	a = shiftClickAgent(t, a, "s1")
	if a.focus != "s1" || a.grid.Cols[1].Bottom != "" {
		t.Errorf("focus %q columns %+v, want s1 focused and nothing split", a.focus, a.grid.Cols)
	}
}

// Shift held through a drag changes nothing: the drag is the gesture.
func TestAShiftDragIsAnOrdinaryDrag(t *testing.T) {
	a := dragFleet(t)
	r := a.regions()
	x, y := sidebarX(a), rowOfAgent(t, a, "s3")
	a, _ = a.mouse(shiftPress(x, y))
	a = motionTo(a, x-dragSlop-1, y)
	a = motionTo(a, midOf(r, 0), topHalf())
	a = letGoAt(a, midOf(r, 0), topHalf())
	if want := []string{"", "s1", "s3"}; !slices.Equal(tops(a), want) {
		t.Errorf("columns %q, want a new column: a Shift-drag does not open below", tops(a))
	}
}

// From a lower pane a Shift-click on an agent already open still focuses it: the
// refusal is for an open that has nowhere to go, and this opens nothing.
func TestAShiftClickOnAnOpenAgentFromALowerPaneFocusesIt(t *testing.T) {
	a := dragFleet(t).openBelow("s2", "agent02") // s2, the lower pane, holds the keys
	a = shiftClickAgent(t, a, "s1")
	if a.focus != "s1" {
		t.Errorf("focus %q, want the open agent's pane", a.focus)
	}
	if n, ok := notice.Latest(); ok && strings.Contains(n.String(), noRowBelow) {
		t.Errorf("an open agent was refused as if it were being opened below: %q", n.String())
	}
}

// A new press is a new gesture: a drag whose release was lost is over, so the
// motion after it is the press's own and the release drops nothing.
func TestANewPressEndsADragWhoseReleaseWasLost(t *testing.T) {
	a := dragFleet(t)
	r := a.regions()
	a = pullAgent(t, a, "s3", midOf(r, 0), topHalf()) // live, and the button comes up unseen
	was := tops(a)
	a, _ = a.mouse(pressAt(midOf(r, 0), topHalf()+3)) // a press in a pane
	if a.dragged.id != "" {
		t.Fatalf("a press in a pane left the drag armed: %+v", a.dragged)
	}
	a = motionTo(a, midOf(r, 0)+6, topHalf()+3)
	a = letGoAt(a, midOf(r, 0)+6, topHalf()+3)
	if !slices.Equal(tops(a), was) || a.grid.Has("s3") {
		t.Errorf("columns %q: the old drag landed after a new press", tops(a))
	}
}

// A click on an agent's own row while another agent's subagent is the sidebar's
// cursor leaves no dispatch attached to the agent it opened.
func TestAClickOnAnAgentRowDropsADispatchTheCursorWasOn(t *testing.T) {
	a := dragFleet(t)
	a.roster = Roster{Selected: "s2", SelectedTask: "toolu_1"}
	a = clickAgent(t, a, "s3")
	if a.roster.Selected != "s3" || a.roster.SelectedTask != "" {
		t.Errorf("roster %+v after clicking s3's row, want s3 with no dispatch", a.roster)
	}
}

// A divider drag whose release was lost does not outlive the next agent-row
// click: every release ends it, as every release did before an agent row was a
// drag source.
func TestAnAgentRowReleaseEndsAStaleDividerDrag(t *testing.T) {
	a := dragFleet(t)
	a.dragAt = 0
	a = clickAgent(t, a, "s3")
	if a.dragAt != noDrag {
		t.Errorf("dragAt %d after the click: the next text drag would move a divider", a.dragAt)
	}
}

// Alt+esc is how an Esc and the next mouse report read when the pump hands them
// over together; the report's runes are not typed into the draft.
func TestAnEscThatBroughtAMouseReportDoesNotTypeIt(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<32;61;6M")})
	if got := a.composer().Value(); got != "" || a.dragged.id != "" {
		t.Fatalf("draft %q, drag %+v: the report was typed or the drag outlived the esc", got, a.dragged)
	}
	// Once only, and only for that shape: the next key is the operator's.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<32;61;6M")})
	if got := a.composer().Value(); got != "[<32;61;6M" {
		t.Errorf("a second report-shaped key was swallowed: draft %q", got)
	}
}

// A plain esc ends the drag and arms nothing for the keys after it.
func TestAPlainEscDoesNotSwallowTheNextKey(t *testing.T) {
	a := dragFleet(t)
	a = pullAgent(t, a, "s3", midOf(a.regions(), 0), topHalf())
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<32;61;6M")})
	if got := a.composer().Value(); got != "[<32;61;6M" {
		t.Errorf("draft %q: a key after a plain esc was swallowed", got)
	}
}

// Over the sidebar's column below its rows, on the awareness strip, a release
// still cancels: the drag is back where it came from.
func TestTheSidebarCancelsAtEveryRow(t *testing.T) {
	a := dragFleet(t)
	for _, y := range []int{1, a.paneHeight() / 2, a.paneHeight() - 1, a.paneHeight(), a.paneHeight() + 1} {
		if got := a.dropZoneFor("s3", sidebarX(a), y); got.kind != zoneNone {
			t.Errorf("row %d of the sidebar's column: zone %+v, want a cancel", y, got)
		}
	}
}

// narrowFleet is a window that carries one column, with s1 open beside the room
// and holding the keys.
func narrowFleet(t *testing.T) App {
	t.Helper()
	return fleetApp(t, 110, 30, 4).openDMWith("s1", "agent01").withSize(110, 30)
}

// In a single-column layout a drop that is not below a pane replaces the pane on
// screen, as the click does: no column is added.
func TestANewColumnDropInASingleColumnLayoutReplacesTheShownPane(t *testing.T) {
	a := narrowFleet(t)
	r := a.regions()
	x := midOf(r, a.columnOf("s1"))
	a = pullAgent(t, a, "s3", x, topHalf())
	if got := a.dragged.zone; got != (dropZone{kind: zoneColumn}) {
		t.Fatalf("zone %+v over the shown pane's top half, want a new column", got)
	}
	a = letGoAt(a, x, topHalf())
	if want := []string{"", "s3"}; !slices.Equal(tops(a), want) || a.focus != "s3" {
		t.Errorf("columns %q focus %q, want s3 in s1's place", tops(a), a.focus)
	}
}

// A drop below stays what it was where the grid allows it: the drawn column
// splits, so one column holds two panes and still nothing is added off screen.
func TestABelowDropInASingleColumnLayoutStillSplitsTheShownColumn(t *testing.T) {
	a := narrowFleet(t)
	x := midOf(a.regions(), a.columnOf("s1"))
	a = pullAgent(t, a, "s3", x, bottomHalf(a))
	a = letGoAt(a, x, bottomHalf(a))
	if c := a.grid.Cols[1]; c.Top != "s1" || c.Bottom != "s3" || len(a.grid.Cols) != 2 {
		t.Errorf("columns %+v, want s3 under s1 and no new column", a.grid.Cols)
	}
}

// The outline over the shown pane is that pane's own rect, as the draw's hit-testing
// and the laid-out size see it: where the agent replacing it lands.
func TestTheNewColumnOutlineInASingleColumnLayoutIsTheShownPane(t *testing.T) {
	a := narrowFleet(t)
	x, y, w, h, ok := a.zoneRect(dropZone{kind: zoneColumn}, "s3")
	if !ok {
		t.Fatal("no outline over the shown pane")
	}
	landsAt(t, "the shown pane", a, "s1", x, y, w, h)
}
