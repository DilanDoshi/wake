package ui

// What the grid actually draws, read off the rendered frame.
//
// The in-process half of the screen tests. `cmd/wake/gridscreen_unix_test.go`
// is the other one and is the stronger of the two: it drives the real binary
// through a pty. These trade that reach for precision - they can index the
// frame against Layout.Regions, so they say *which cell* a divider is in rather
// than that one exists, and they run in milliseconds over sizes a pty test
// would spend a second each on.
//
// Everything else about the grid is asserted against Regions, which is a pure
// function agreeing with itself; these read the characters instead, so a frame
// that measures right and *looks* wrong fails here.
//
// Derived rather than golden: a committed frame would have to be regenerated
// on every wording change and would stop being read. What is pinned is the
// relation between what Regions says and what is on screen.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// screen is the rendered frame as rows of runes, with the notice row dropped -
// it is drawn under the columns and belongs to no pane.
func screen(a App) [][]rune {
	rows := strings.Split(stripANSI(a.View()), "\n")
	// The pane rows only. The awareness strip and the notice row sit below them,
	// span the whole frame and hold no dividers - so they are dropped by the same
	// constants paneHeight reserves them with, not by a literal.
	out := make([][]rune, 0, len(rows))
	for _, r := range rows[:max(len(rows)-stripHeight-noticeHeight, 0)] {
		out = append(out, []rune(r))
	}
	return out
}

// colRange is the terminal columns a grid column occupies, [start, end).
func colRange(r Regions, col int) (int, int) {
	start := r.Groups
	for i := range col {
		if r.Cols[i] > 0 {
			start += r.Cols[i] + dividerWidth
		}
	}
	return start, start + r.Cols[col]
}

// textIn is everything drawn inside a rectangle of the frame.
func textIn(rows [][]rune, x0, x1, y0, y1 int) string {
	var b strings.Builder
	for y := y0; y < y1 && y < len(rows); y++ {
		if x0 < len(rows[y]) {
			b.WriteString(string(rows[y][x0:min(x1, len(rows[y]))]))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// Every divider Regions accounts for is a column of rule glyphs on screen, top
// to bottom. A divider that is a column of arithmetic and not of characters is
// one the eye cannot find and the mouse lands beside.
func TestEveryDividerIsDrawnWhereRegionsPutsIt(t *testing.T) {
	a := threeColumns(t)
	r, rows := a.regions(), screen(a)

	for col := range len(r.Cols) - 1 {
		_, end := colRange(r, col)
		for y := range rows {
			if end >= len(rows[y]) {
				t.Fatalf("row %d is %d wide, so column %d's divider at x=%d is off the frame", y, len(rows[y]), col, end)
			}
			if got := rows[y][end]; got != []rune(dividerGlyph)[0] {
				t.Fatalf("column %d's divider should be at x=%d; row %d has %q there:\n%s", col, end, y, string(got), string(rows[y]))
			}
		}
	}
}

// The left workspaces sidebar is hidden for now, so the room owns the left
// edge with no sidebar and no divider before it - even at a width wide enough
// to have drawn it. The implementation is kept (groups.go); only the render and
// the ⌃G key are gone, restored with the multi-groupchat version.
func TestTheWorkspacesSidebarIsHidden(t *testing.T) {
	a := newRoomApp(t).withSize(220, 40).withAgents("sydney", "john").applyGeometry()

	r := a.regions()
	if r.Groups != 0 {
		t.Fatalf("the workspaces sidebar took %d columns at 220 wide; it is hidden for now and the room "+
			"owns the left edge (groups.go)", r.Groups)
	}
	// Gone from the frame, not merely zero in the arithmetic: the room is the
	// leftmost region, so "group chat" is drawn inside column 0's own columns
	// with no sidebar and no divider before it.
	_, end := colRange(r, 0)
	if got := textIn(screen(a), 0, end, 0, len(screen(a))); !strings.Contains(got, "group chat") {
		t.Fatalf("the room is not drawn at the left edge, so something is still left of it:\n%s", got)
	}
}

// Each column draws its own conversation and nobody else's. The whole feature
// is "put *that* agent over there", so a frame where the names are in the wrong
// columns is the feature not working however well the widths measure.
func TestEachColumnDrawsItsOwnConversation(t *testing.T) {
	a := threeColumns(t)
	r, rows := a.regions(), screen(a)

	for _, tc := range []struct {
		col      int
		wants    []string
		wantsNot []string
	}{
		{0, []string{"group chat"}, []string{"@sydney", "@john", "@marcus"}},
		{1, []string{"@sydney", "@john"}, []string{"@marcus", "group chat"}},
		{2, []string{"@marcus"}, []string{"@sydney", "@john", "group chat"}},
	} {
		x0, x1 := colRange(r, tc.col)
		got := textIn(rows, x0, x1, 0, len(rows))
		for _, want := range tc.wants {
			if !strings.Contains(got, want) {
				t.Errorf("column %d (x %d-%d) does not draw %q:\n%s", tc.col, x0, x1, want, got)
			}
		}
		for _, not := range tc.wantsNot {
			if strings.Contains(got, not) {
				t.Errorf("column %d (x %d-%d) draws %q, which belongs to another column:\n%s", tc.col, x0, x1, not, got)
			}
		}
	}
}

// The rule between two stacked panes is on the row SplitRows names, and spans
// that column and nothing either side of it - a rule that ran the width of the
// frame would cut the columns beside it in half.
func TestTheStackedRuleIsDrawnOnTheRowThatDividesTheColumn(t *testing.T) {
	a := threeColumns(t)
	r, rows := a.regions(), screen(a)
	top, bottom := a.rowsOf(1)
	if bottom == 0 {
		t.Fatal("column 1 is not stacked at this height, so this test proves nothing")
	}

	x0, x1 := colRange(r, 1)
	rule := []rune(dividerRow)[0]

	for x := x0; x < x1; x++ {
		if got := rows[top][x]; got != rule {
			t.Fatalf("the rule should span x %d-%d on row %d; x=%d has %q:\n%s", x0, x1, top, x, string(got), string(rows[top]))
		}
	}
	// And only there: the columns either side keep their own content on that row.
	for _, col := range []int{0, 2} {
		s, e := colRange(r, col)
		if strings.Count(string(rows[top][s:e]), dividerRow) == e-s {
			t.Errorf("the rule also ran the whole of column %d on row %d, which cuts a conversation nobody split", col, top)
		}
	}
}

// Both halves of a stacked column draw a composer, which is what says a pane is
// somewhere you can type rather than a transcript with a title.
func TestBothHalvesOfAStackedColumnDrawAComposer(t *testing.T) {
	a := threeColumns(t)
	r, rows := a.regions(), screen(a)
	top, bottom := a.rowsOf(1)
	if bottom == 0 {
		t.Fatal("column 1 is not stacked at this height")
	}

	x0, x1 := colRange(r, 1)
	upper := textIn(rows, x0, x1, 0, top)
	lower := textIn(rows, x0, x1, top+dividerHeight, len(rows))

	if !strings.Contains(upper, "@sydney") || !strings.Contains(upper, ">") {
		t.Errorf("the upper pane does not draw @sydney's composer:\n%s", upper)
	}
	if !strings.Contains(lower, "@john") || !strings.Contains(lower, ">") {
		t.Errorf("the lower pane does not draw @john's composer:\n%s", lower)
	}
}

// A drag moves the divider *on screen*, to the cell the pointer is on. This is
// the assertion the pure-function tests cannot make: they agree with Regions,
// and Regions is what would be wrong.
func TestADraggedDividerMovesOnScreenToWhereThePointerIs(t *testing.T) {
	a := threeColumns(t)
	before := a.regions()
	_, was := colRange(before, 0)

	target := was - 20
	a = clickAt(a, was, 0)
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: target})
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease})
	a = settle(a)

	rows := screen(a)
	_, now := colRange(a.regions(), 0)
	if now != target {
		t.Fatalf("the divider was dragged to x=%d and Regions puts it at %d", target, now)
	}
	for y := range rows {
		if got := rows[y][target]; got != []rune(dividerGlyph)[0] {
			t.Fatalf("row %d has %q at x=%d, where the divider was dragged to:\n%s", y, string(got), target, string(rows[y]))
		}
		if got := rows[y][was]; got == []rune(dividerGlyph)[0] && was < len(rows[y]) {
			// The old column may legitimately hold a composer's own border, so
			// this is only a failure if *every* row still has a rule there.
			continue
		}
		return
	}
	t.Errorf("every row still draws a rule at x=%d, so the divider was drawn in both places", was)
}

// The frame never exceeds the terminal, at every arrangement the grid can reach.
// A frame one row too tall scrolls the alt screen on every draw and one column
// too wide wraps every row - both cost the whole window rather than one pane.
//
// Bounded rather than exact, and the difference is a finding rather than a
// weakening: the composer's target line and its legend are not right-padded, so
// the *rightmost* block's short lines stay short - lipgloss.JoinHorizontal pads
// every block but the last. Reproduced on a room with no grid at all, so it
// predates this and is cosmetically harmless (the renderer clears to end of
// line). It is in deferred.md. What must never happen is the other direction,
// which is what this holds.
func TestTheFrameNeverExceedsTheTerminalForEveryArrangement(t *testing.T) {
	for _, size := range []struct{ w, h int }{{240, 40}, {200, 24}, {160, 30}, {130, 20}, {110, 18}, {90, 14}} {
		a := threeColumns(t)
		a, _ = a.resized(size.w, size.h)
		a = settle(a)

		rows := strings.Split(stripANSI(a.View()), "\n")
		if len(rows) != size.h {
			t.Errorf("%dx%d: the frame is %d rows", size.w, size.h, len(rows))
		}
		for y, row := range rows[:len(rows)-1] {
			if got := widest(row); got > size.w {
				t.Errorf("%dx%d: row %d is %d columns, wider than the terminal - every row below it wraps:\n%q", size.w, size.h, y, got, row)
			}
		}
	}
}

// And the pane holding the keys is one that is actually on screen, checked
// against the characters rather than against Regions. A composer nobody can see
// taking keystrokes is the failure App.withFocus exists to prevent, and it is
// the one HANDOFF-NEXT §2 says a rendered frame is the only witness to.
func TestTheFocusedConversationIsDrawnAtEveryWidth(t *testing.T) {
	names := map[string]string{"": "group chat", "s1": "@sydney", "s2": "@john", "s3": "@marcus"}

	for _, w := range []int{240, 200, 160, 130, 110, 90} {
		for _, id := range []string{"", "s1", "s2", "s3"} {
			a := threeColumns(t)
			a, _ = a.resized(w, 40)
			a = settle(a).refocus(id)
			if a.focus != id {
				// The layout may move the keys off a pane it cannot draw, which
				// is correct; what it must not do is leave them there.
				id = a.focus
			}
			if got := stripANSI(a.View()); !strings.Contains(got, names[id]) {
				t.Errorf("at %d columns the keys are on %q and %q is not on screen:\n%s", w, id, names[id], got)
			}
		}
	}
}
