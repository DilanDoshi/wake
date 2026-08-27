package ui

// The mouse over a grid: clicking any pane, dragging any divider, and the wheel
// over the pane the pointer is actually on.
//
// mouse_test.go covers the two-pane case this grew out of. What is here is what
// only exists once there are more than two panes and more than one divider.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// threeColumns is the room, a stacked column, and a third column beside it:
// [room][sydney/john][marcus]. Wide enough that all of them are drawn.
func threeColumns(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(220, 40).withAgents("sydney", "john", "marcus")
	a = pick(a, "s1").openRight("s1", "sydney")
	a = pick(a, "s2").openBelow("s2", "john")
	a = pick(a, "s3").openRight("s3", "marcus")
	// Enough conversation in each that there is something to scroll back
	// through: a transcript shorter than its pane has no scroll to move.
	for _, id := range []string{"s1", "s2", "s3"} {
		for i := range 80 {
			a = said(a, id, "line "+string(rune('a'+i%26)))
		}
	}
	return a.applyGeometry()
}

// clickAt presses the left button at a terminal cell.
func clickAt(a App, x, y int) App {
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y})
	return a
}

// midOf is a column's middle terminal column, which is a cell inside it and not
// on either of its edges.
func midOf(r Regions, col int) int {
	x := r.Groups
	for i := 0; i < col; i++ {
		if r.Cols[i] > 0 {
			x += r.Cols[i] + dividerWidth
		}
	}
	return x + r.Cols[col]/2
}

// dividerX is the terminal column the divider right of a column is drawn in.
func dividerX(r Regions, col int) int {
	x := r.Groups
	for i := 0; i <= col; i++ {
		if r.Cols[i] > 0 {
			x += r.Cols[i] + dividerWidth
		}
	}
	return x - dividerWidth
}

func TestAClickFocusesWhicheverColumnItLandedIn(t *testing.T) {
	a := threeColumns(t)
	r := a.regions()

	for _, tc := range []struct {
		col  int
		want string
	}{
		{0, ""},   // the room
		{1, "s1"}, // the upper half of the stack
		{2, "s3"},
	} {
		got := clickAt(a, midOf(r, tc.col), 0)
		if got.focus != tc.want {
			t.Errorf("clicking column %d at x=%d focused %q, want %q", tc.col, midOf(r, tc.col), got.focus, tc.want)
		}
	}
}

// The halves of a stacked column are two panes at one x, so only the row tells
// them apart - and a click that ignored y would make the lower one unreachable
// with a mouse.
func TestAClickTellsTheHalvesOfAStackedColumnApart(t *testing.T) {
	a := threeColumns(t)
	x := midOf(a.regions(), 1)
	top, bottom := SplitRows(a.paneHeight())
	if bottom == 0 {
		t.Fatal("the column is not stacked at this height, so this test proves nothing")
	}

	if got := clickAt(a, x, top/2); got.focus != "s1" {
		t.Errorf("a click in the upper half focused %q, want s1", got.focus)
	}
	if got := clickAt(a, x, top+dividerHeight+1); got.focus != "s2" {
		t.Errorf("a click in the lower half focused %q, want s2", got.focus)
	}
}

// Every divider, not only the first. With three columns there are two, and a
// hit test that reported the same index for both would drag the wrong pair.
func TestEveryDividerIsItsOwnAndDraggable(t *testing.T) {
	a := threeColumns(t)
	r := a.regions()

	for col := range 2 {
		x := dividerX(r, col)
		region, at := a.layout.Hit(r, x)
		if region != RegionDivider {
			t.Fatalf("column %d's divider at x=%d hit-tests as %v, not a divider", col, x, region)
		}
		if at != col {
			t.Errorf("the divider at x=%d reports index %d, want %d: a drag would move the wrong pair", x, at, col)
		}
		if got := clickAt(a, x, 0); got.dragAt != col {
			t.Errorf("pressing the divider at x=%d took hold of %d, want %d", x, got.dragAt, col)
		}
	}
}

// And dragging one moves the pair it sits between, leaving every other column
// where it was. That is the property a single shared split cannot have.
func TestDraggingADividerMovesOnlyThePairEitherSideOfIt(t *testing.T) {
	a := threeColumns(t)
	before := a.regions()

	// Take hold of the second divider and pull it left.
	a = clickAt(a, dividerX(before, 1), 0)
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: dividerX(before, 1) - 12})
	a = settle(a)
	after := a.regions()

	if after.Cols[0] != before.Cols[0] {
		t.Errorf("dragging the second divider changed the room from %d to %d: a drag is a statement about one boundary", before.Cols[0], after.Cols[0])
	}
	if after.Cols[1] >= before.Cols[1] {
		t.Errorf("pulling the second divider left did not narrow column 1: %d then %d", before.Cols[1], after.Cols[1])
	}
	if after.Cols[2] <= before.Cols[2] {
		t.Errorf("column 2 did not gain what column 1 lost: %d then %d", before.Cols[2], after.Cols[2])
	}
	if got := after.Cols[0] + after.Cols[1] + after.Cols[2]; got != before.Cols[0]+before.Cols[1]+before.Cols[2] {
		t.Errorf("the columns total %d after the drag, want the %d they started with", got, before.Cols[0]+before.Cols[1]+before.Cols[2])
	}
}

// The first divider still works with a third column beyond it, which is the
// case an implementation that only ever knew about "the" divider gets wrong.
func TestDraggingTheFirstDividerLeavesTheThirdColumnAlone(t *testing.T) {
	a := threeColumns(t)
	before := a.regions()

	a = clickAt(a, dividerX(before, 0), 0)
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: dividerX(before, 0) + 10})
	a = settle(a)
	after := a.regions()

	if after.Cols[2] != before.Cols[2] {
		t.Errorf("dragging the first divider changed the third column from %d to %d", before.Cols[2], after.Cols[2])
	}
	if after.Cols[0] <= before.Cols[0] {
		t.Errorf("pushing the first divider right did not widen the room: %d then %d", before.Cols[0], after.Cols[0])
	}
}

// No drag leaves a pane too narrow to read, at any divider.
func TestNoDragAtAnyDividerLeavesAnUnreadablePane(t *testing.T) {
	a := threeColumns(t)
	r := a.regions()
	for col := range 2 {
		for _, x := range []int{0, 1, 60, 219, 900, -30} {
			held := clickAt(a, dividerX(r, col), 0)
			held, _ = held.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x})
			held = settle(held)
			for i, w := range held.regions().Cols {
				if w > 0 && w < minPaneWidth {
					t.Fatalf("dragging divider %d to x=%d left column %d at %d, floor %d", col, x, i, w, minPaneWidth)
				}
			}
		}
	}
}

// The rule between two stacked panes is draggable too, and it is the only way
// to give one of them more rows: vertical space is the scarce resource, so a
// stack fixed at half each is a stack whose lower pane cannot be read.
func TestTheRuleBetweenStackedPanesIsDraggable(t *testing.T) {
	a := threeColumns(t)
	x := midOf(a.regions(), 1)
	top, _ := a.rowsOf(1)

	region, at := a.layout.Hit(a.regions(), x)
	if region != RegionPane || at != 1 {
		t.Fatalf("x=%d is not inside the stacked column: %v/%d", x, region, at)
	}

	a = clickAt(a, x, top) // the rule's own row
	if !a.draggingRows() {
		t.Fatal("pressing the rule between two stacked panes did not take hold of it")
	}

	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: top + 6})
	a = settle(a)

	moved, _ := a.rowsOf(1)
	if moved <= top {
		t.Errorf("dragging the rule down left the upper pane at %d rows, want more than the %d it had", moved, top)
	}
}

// And the rows it divides always add up to the column, or the frame is taller
// or shorter than the terminal - which scrolls the alt screen on every draw.
func TestTheStackedRowsAlwaysSpendTheColumnExactly(t *testing.T) {
	a := threeColumns(t)
	x := midOf(a.regions(), 1)
	start, _ := a.rowsOf(1)

	for _, y := range []int{0, 1, 5, 20, 39, 200, -10} {
		held := clickAt(a, x, start)
		held, _ = held.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y})
		held = settle(held)

		top, bottom := held.rowsOf(1)
		if top+dividerHeight+bottom != held.paneHeight() {
			t.Fatalf("dragging the rule to y=%d gave rows %d and %d, which with the rule is not the %d the column has", y, top, bottom, held.paneHeight())
		}
		if top < minPaneHeight || bottom < minPaneHeight {
			t.Fatalf("dragging the rule to y=%d left panes of %d and %d rows, floor %d", y, top, bottom, minPaneHeight)
		}
	}
}

// The wheel moves the transcript under the pointer, not whichever pane happens
// to hold the keys. With one conversation those were the same thing; with four
// on screen, scrolling one while reading another is the wrong pane every time.
func TestTheWheelScrollsThePaneUnderThePointer(t *testing.T) {
	a := threeColumns(t)
	r := a.regions()
	a = a.refocus("") // the keys are in the room; the pointer will not be

	for _, tc := range []struct {
		col int
		id  string
	}{{1, "s1"}, {2, "s3"}} {
		a = a.withDM(tc.id, a.dms[tc.id].ScrollUp(-1000)) // pin it at the newest line
		before := a.dms[tc.id].tr.scroll
		if a.dms[tc.id].tr.bottom() == 0 {
			t.Fatalf("%s has nothing to scroll, so this case proves nothing", tc.id)
		}

		got, _ := a.mouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, X: midOf(r, tc.col), Y: 2})
		if got.dms[tc.id].tr.scroll == before {
			t.Errorf("the wheel over column %d did not scroll %s (offset stayed %d)", tc.col, tc.id, before)
		}
		if got.focus != "" {
			t.Errorf("the wheel over column %d moved the keys to %q: scrolling is reading, not focusing", tc.col, got.focus)
		}
	}
}

// The fence under a transcript is measured from the pane's own top row, not
// from the column's: in a stacked column the upper conversation's query bar
// sits in the middle of the column, and its rows are chrome to that pane while
// the same rows further down are conversation in the pane below.
func TestAQueryBarInsideAStackedColumnIsNotConversation(t *testing.T) {
	a := threeColumns(t)
	x := midOf(a.regions(), 1)
	top, bottom := SplitRows(a.paneHeight())
	if bottom == 0 {
		t.Fatal("the column is not stacked at this height, so this test proves nothing")
	}

	// The upper pane's own chrome: its transcript ends, its composer follows,
	// and the rule is still rows below.
	for y := a.dms["s1"].tr.height; y < top; y++ {
		got, _ := drag(a, x, x+20, y)
		if !got.sel.empty() {
			t.Errorf("a drag on row %d of the upper pane selected %+v: its transcript ends at row %d",
				y, got.sel, a.dms["s1"].tr.height-1)
		}
	}
	// And the pane below still selects from its first row, which is what says
	// the fence moved with the pane rather than being measured off the column.
	if got, _ := drag(a, x, x+20, top+dividerHeight); got.sel.empty() {
		t.Errorf("the lower pane's first row selected nothing: it is that conversation's newest line, not chrome")
	}
}
