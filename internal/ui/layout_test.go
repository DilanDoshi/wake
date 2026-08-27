package ui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// The bounded layout: what each region is worth at each terminal width, where
// the divider may be dragged to, and what a mouse column is over.
//
// Every rule here is a table test rather than a golden frame, which is the
// whole reason Layout draws nothing.

func TestBothSidebarsAndTwoPanesFitTheTerminalExactly(t *testing.T) {
	for _, w := range []int{120, 160, 200, 240} {
		l := Layout{Width: w, Height: 40, ShowGroups: true, ShowRoster: true}
		r := l.Regions(2, 1)
		total := r.Groups + r.Room() + dividerWidth + r.Cols[1] + r.Roster
		if total != w {
			t.Errorf("width %d: regions sum to %d. lipgloss joins columns side by side, so a sum that is off by one either wraps the frame or leaves a ragged edge on every draw", w, total)
		}
	}
}

// The test above is an aggregate, and an aggregate is the shape this project
// has twice shipped a layout defect behind: two regions changing in
// compensating directions leave the total exactly right and every pane the
// wrong size. So each region is also asserted on its own, against the constant
// it is derived from rather than against a number copied out of this file.
func TestEachRegionIsItsOwnWidthAndNotOnlyTheRightTotal(t *testing.T) {
	const w = 200
	r := Layout{Width: w, Height: 40, ShowGroups: true, ShowRoster: true}.Regions(2, 1)

	if r.Groups != groupsWidth {
		t.Errorf("the groups sidebar took %d columns, want %d: a sidebar that is not its own width is a sidebar drawn ragged, and the sum test above cannot see it", r.Groups, groupsWidth)
	}
	if r.Roster != rosterWidth {
		t.Errorf("the roster took %d columns, want %d", r.Roster, rosterWidth)
	}
	if space := w - groupsWidth - rosterWidth - dividerWidth; r.Room()+r.Cols[1] != space {
		t.Errorf("the two panes took %d columns between them, want the %d the sidebars and the divider left", r.Room()+r.Cols[1], space)
	}
	if diff := r.Room() - r.Cols[1]; diff < -1 || diff > 1 {
		t.Errorf("an even split gave the room %d and the DM %d: neither pane is the primary one, so the only allowed difference is the odd column", r.Room(), r.Cols[1])
	}
}

// Closing the right sidebar gives each pane half of it back, which is the whole
// reason it auto-closes. Derived from rosterWidth rather than pinned at the ten
// columns the brief measured, so widening the sidebar moves this by
// construction instead of failing here.
func TestClosingTheRightSidebarBuysHalfOfItAPane(t *testing.T) {
	open := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}.Regions(2, 1)
	shut := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: false}.Regions(2, 1)
	want := rosterWidth / 2
	if gained := shut.Room() - open.Room(); gained < want-1 || gained > want+1 {
		t.Errorf("closing the right sidebar gained %d columns a pane, want ~%d: that is the difference between tight and comfortable", gained, want)
	}
}

func TestBelowOneHundredAndTwentyColumnsTheDMTakesOverInsteadOfSplitting(t *testing.T) {
	for _, w := range []int{119, 100, 80} {
		r := Layout{Width: w, Height: 40}.Regions(2, 1)
		if r.Room() != 0 {
			t.Errorf("width %d: the room still had %d columns. Below 120 a split gives 42 columns a pane, which is poor - the DM takes the pane instead", w, r.Room())
		}
		if r.Cols[1] == 0 {
			t.Errorf("width %d: neither pane was drawn", w)
		}
	}
	if r := (Layout{Width: 120, Height: 40}).Regions(2, 1); r.Room() == 0 {
		t.Error("at exactly 120 the split is still on: the rule is *below* 120, and an off-by-one here is a pane that vanishes at the width somebody sized their terminal to")
	}
}

func TestSidebarsCollapseBeforeThePanesDo(t *testing.T) {
	// §8: at 120-160 the groups sidebar collapses; below 100 both auto-hide.
	if r := (Layout{Width: 150, Height: 40, ShowGroups: true, ShowRoster: true}).Regions(1, 0); r.Groups != 0 {
		t.Errorf("at 150 columns the groups sidebar took %d: it collapses between 120 and 160", r.Groups)
	}
	if r := (Layout{Width: 90, Height: 40, ShowGroups: true, ShowRoster: true}).Regions(1, 0); r.Groups != 0 || r.Roster != 0 {
		t.Errorf("at 90 columns the sidebars took %d and %d, want both hidden", r.Groups, r.Roster)
	}
	// And the conversation keeps what they gave up, rather than the columns
	// going nowhere: a sidebar that collapses without widening a pane is a
	// sidebar that collapsed for nothing.
	wide := Layout{Width: 160, Height: 40, ShowGroups: true, ShowRoster: true}.Regions(1, 0)
	narrow := Layout{Width: 159, Height: 40, ShowGroups: true, ShowRoster: true}.Regions(1, 0)
	if narrow.Room() <= wide.Room()-groupsWidth {
		t.Errorf("the groups sidebar collapsed at 159 and the room went from %d to %d: the %d columns it gave up did not reach the conversation", wide.Room(), narrow.Room(), groupsWidth)
	}
}

func TestADividerCannotBeDraggedIntoAPaneTooNarrowToRead(t *testing.T) {
	l := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}
	base := l.Regions(2, 1)
	for _, x := range []int{0, 1, 17, 199, 500, -20} {
		r := l.DragDivider(base, 0, x).Regions(2, 1)
		if r.Room() < minPaneWidth || r.Cols[1] < minPaneWidth {
			t.Errorf("dragging to x=%d left panes of %d and %d, minimum %d: a pane you cannot read is not a pane, and a drag that produces one is a drag somebody cannot undo without a mouse", x, r.Room(), r.Cols[1], minPaneWidth)
		}
	}
}

// ...and it stops at the limit rather than springing back off it.
//
// The clamp in DragDivider looks redundant next to the one in panes, and this
// is what it is actually for: a Split outside [0,1] reads as "never dragged"
// and falls back to even, so storing an unclamped fraction sends the divider to
// the *middle* the moment somebody pushes past the floor - a jump in the
// opposite direction from the hand that caused it.
//
// Mutation check: dropping DragDivider's clamp leaves every pane above the
// floor and fails this at "dragging to x=0 left the divider exactly where an
// untouched layout puts it".
func TestADragPastTheLimitStopsAtItRatherThanSpringingBack(t *testing.T) {
	l := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}
	even := l.Regions(2, 1)
	for _, x := range []int{0, 1, 199, 500, -20} {
		if r := l.DragDivider(even, 0, x).Regions(2, 1); r.Room() == even.Room() {
			t.Errorf("dragging to x=%d left the divider exactly where an untouched layout puts it (room %d)", x, r.Room())
		}
	}
}

// The clamp above must not be the only thing the drag does. A DragDivider that
// ignored x entirely and returned the layout unchanged would satisfy every
// bound in it, so this is the half that says the divider moves at all - and
// lands where it was put, which is what a hand on a mouse is watching.
func TestTheDividerLandsWhereItWasDragged(t *testing.T) {
	l := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}
	base := l.Regions(2, 1)
	for _, x := range []int{60, 90, 140} {
		r := l.DragDivider(base, 0, x).Regions(2, 1)
		if got := base.Groups + r.Room(); got != x {
			t.Errorf("dragging to column %d put the divider at %d (room %d): the divider is the column the pointer is on, or a drag drifts away from the cursor", x, got, r.Room())
		}
	}
}

func TestHitTestingNamesTheRegionUnderTheCursor(t *testing.T) {
	l := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}
	r := l.Regions(2, 1)
	cases := []struct {
		x    int
		want Region
		col  int
	}{
		{0, RegionGroups, 0},
		{r.Groups + 1, RegionPane, 0},
		{r.Groups + r.Room(), RegionDivider, 0},
		{r.Groups + r.Room() + dividerWidth + 1, RegionPane, 1},
		{199, RegionRoster, 0},
	}
	for _, c := range cases {
		if got, col := l.Hit(r, c.x); got != c.want || col != c.col {
			t.Errorf("Hit(%d) = %v/%d, want %v/%d", c.x, got, col, c.want, c.col)
		}
	}
}

// Every region names itself, and no two name the same thing.
//
// The names are only ever read by a human staring at a failure - "Hit(98) =
// room, want divider" - which is exactly why the guard is derived rather than
// listed: a region added without a String case falls to the default and prints
// "none", so the next failure message about it would be a lie, and a
// hand-written list of the regions that exist today could not see that. The
// constants come out of layout.go's own const block.
func TestEveryRegionNamesItselfAndNoTwoShareAName(t *testing.T) {
	names := regionConstsIn(t, "layout.go")
	if len(names) < 5 {
		t.Fatalf("found %d Region constants in layout.go, want at least the 5 that exist: the scan is broken and this test is asserting nothing", len(names))
	}

	seen := map[string]string{}
	for i, constName := range names {
		got := Region(i).String()
		if got == "" {
			t.Errorf("%s has no name", constName)
			continue
		}
		if other, dup := seen[got]; dup {
			t.Errorf("%s and %s both print %q: a Region with no String case falls to the default, so a failure message about it names the wrong region", other, constName, got)
		}
		seen[got] = constName
	}
}

// regionConstsIn returns the Region constants declared in a file, in
// declaration order - which is iota's order, so the index is the value.
func regionConstsIn(t *testing.T, file string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	var out []string
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range vs.Names {
				if strings.HasPrefix(name.Name, "Region") {
					out = append(out, name.Name)
				}
			}
		}
	}
	return out
}

// A column outside the terminal belongs to nothing. Nothing produces one
// today, and the reason to say so is that Hit's first case is a bare `x <
// edge`: a negative column would otherwise land in the leftmost region, which
// is a click somebody never made.
func TestAColumnOutsideTheTerminalIsInNoRegion(t *testing.T) {
	l := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}
	r := l.Regions(2, 1)
	for _, x := range []int{-1, -40, 200, 4000} {
		if got, _ := l.Hit(r, x); got != RegionNone {
			t.Errorf("Hit(%d) = %v, want RegionNone", x, got)
		}
	}
}

// With one pane there is no divider column, and a hit test that reported one
// would arm a drag that divides nothing.
func TestThereIsNoDividerWithOnlyOnePane(t *testing.T) {
	l := Layout{Width: 200, Height: 40, ShowGroups: true, ShowRoster: true}
	r := l.Regions(1, 0)
	for x := range l.Width {
		if got, col := l.Hit(r, x); got == RegionDivider || col > 0 {
			t.Fatalf("Hit(%d) = %v/%d with no second pane open (room %d)", x, got, col, r.Room())
		}
	}
}

// Every width, not the four the sum test picks. The two properties that must
// hold at all of them: the regions tile the terminal exactly, and wherever both
// panes are drawn neither is below the floor.
//
// Mutation check: lowering dmTakeoverColumns to 48 fails this at "width 48:
// panes of 23 and 24, floor 24" - which is the defect the test above the floor
// branch describes, arriving from the one direction that can still produce it.
func TestTheRegionsTileEveryTerminalWidthAndNoPaneIsUnreadable(t *testing.T) {
	for w := range 400 {
		for _, cols := range []int{1, 2, 3, 5} {
			for focused := range cols {
				l := Layout{Width: w, Height: 40, ShowGroups: true, ShowRoster: true}
				r := l.Regions(cols, focused)

				if got := spanned(r); got != w {
					t.Fatalf("width %d (%d cols, focus %d): the regions span %d columns - %+v", w, cols, focused, got, r)
				}
				if r.Groups < 0 || r.Roster < 0 {
					t.Fatalf("width %d: a sidebar has a negative width - %+v", w, r)
				}
				for i, c := range r.Cols {
					if c < 0 {
						t.Fatalf("width %d: column %d has a negative width - %+v", w, i, r)
					}
					if c > 0 && c < minPaneWidth && r.Drawn() > 1 {
						t.Fatalf("width %d: column %d is %d wide beside another, floor %d", w, i, c, minPaneWidth)
					}
				}
				// The pane holding the keys is always one somebody can see. A
				// focus on a column the width dropped is the split-brain
				// composer App.withFocus exists to prevent.
				if r.Drawn() > 0 && r.Cols[focused] == 0 {
					t.Fatalf("width %d (%d cols): the focused column %d was not drawn - %+v", w, cols, focused, r)
				}
				// A sidebar never takes so much that the conversation is
				// unreadable. It is the breakpoints that hold this rather than a
				// clause in Regions - see its comment - so this is where lowering
				// one of them is caught.
				if total := totalCols(r); (r.Groups > 0 || r.Roster > 0) && total < minPaneWidth {
					t.Fatalf("width %d: the sidebars took %d and %d and left the conversation %d, floor %d", w, r.Groups, r.Roster, total, minPaneWidth)
				}
			}
		}
	}
}

// totalCols is what every drawn column holds between them.
func totalCols(r Regions) int {
	var n int
	for _, w := range r.Cols {
		n += w
	}
	return n
}

// spanned is the columns a layout occupies. The divider is counted only where
// there is something on both sides of it, which is the same condition Hit uses
// to report one.
func spanned(r Regions) int {
	total := r.Groups + r.Roster + totalCols(r)
	if n := r.Drawn(); n > 1 {
		total += (n - 1) * dividerWidth
	}
	return total
}

// panes carries no floor of its own, and this is the relationship that makes
// that safe rather than lucky: at the narrowest terminal that still splits,
// what the sidebars and the divider leave is more than two readable panes.
//
// Derived from the constants rather than asserted at a few widths, so lowering
// the takeover threshold or widening a sidebar fails here - naming the fix -
// instead of producing a negative pane width in a terminal nobody owns yet.
func TestTheTakeoverThresholdIsWhatKeepsBothPanesReadable(t *testing.T) {
	// Below groupsCollapseColumns only the right sidebar is drawn, so the
	// narrowest split is the takeover width less the roster.
	if left := dmTakeoverColumns - rosterWidth - dividerWidth; left < 2*minPaneWidth {
		t.Errorf("at %d columns a split leaves %d for two panes with a floor of %d: either raise dmTakeoverColumns or give panes a floor of its own", dmTakeoverColumns, left, minPaneWidth)
	}
	// At and above it, both sidebars are.
	if left := groupsCollapseColumns - rosterWidth - groupsWidth - dividerWidth; left < 2*minPaneWidth {
		t.Errorf("at %d columns both sidebars leave %d for two panes with a floor of %d", groupsCollapseColumns, left, minPaneWidth)
	}
	// And with one pane, where the sidebars' own breakpoints are the only
	// thing standing between a sidebar and an unreadable conversation.
	if left := sidebarsHideColumns - rosterWidth; left < minPaneWidth {
		t.Errorf("at the sidebar cutoff of %d the roster leaves %d columns, floor %d: raise sidebarsHideColumns", sidebarsHideColumns, left, minPaneWidth)
	}
	if left := groupsCollapseColumns - rosterWidth - groupsWidth; left < minPaneWidth {
		t.Errorf("at %d both sidebars leave %d columns, floor %d", groupsCollapseColumns, left, minPaneWidth)
	}
}
