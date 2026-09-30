package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// A divider drag draws the divider where the hand is, the way a rule drag always
// has. The panes stay wrapped for the width they had - each is cut or padded to
// the width the drag gives it - and re-wrap once, when the hand lets go.

// dividerColumnsIn is every terminal column holding the divider glyph on each of
// the frame's first rows - what a full-height divider looks like from outside.
func dividerColumnsIn(frame string, rows int) []int {
	lines := strings.Split(stripANSI(frame), "\n")
	var cols []int
	for col := 0; ; col++ {
		all, beyond := true, true
		for _, line := range lines[:min(rows, len(lines))] {
			cell := cellAt(line, col)
			beyond = beyond && cell == ""
			all = all && cell == dividerGlyph
		}
		if beyond {
			return cols
		}
		if all {
			cols = append(cols, col)
		}
	}
}

// cellAt is the character drawn in one terminal column of a plain row, "" past
// its end.
func cellAt(line string, col int) string {
	at := 0
	for _, r := range line {
		if at == col {
			return string(r)
		}
		if at += ansi.StringWidth(string(r)); at > col {
			return ""
		}
	}
	return ""
}

// release lets go of the mouse at a column.
func release(a App, x int) App {
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft, X: x})
	return a
}

// Mutation check: drawing each pane at its wrap width without the per-row fit
// fails this at "mid-drag the divider is drawn at [99]".
func TestADividerDragIsDrawnAtThePointer(t *testing.T) {
	for _, by := range []int{-30, 30} {
		t.Run(fmt.Sprintf("by %d", by), func(t *testing.T) {
			a := splitApp(t, 200, 40, 20)
			from := dividerColumnOf(a)
			a = grab(t, a, from)
			to := from + by
			var frame string
			room, dm := countPaneRenders(t, func() {
				for x := from; x != to; {
					if by < 0 {
						x--
					} else {
						x++
					}
					a = dragTo(a, x)
				}
				frame = a.View()
			})
			if room+dm != 0 {
				t.Errorf("mid-drag the panes re-wrapped (room %d, DM %d), want none until the hand lets go", room, dm)
			}
			if got := dividerColumnsIn(frame, a.paneHeight()); !slices.Equal(got, []int{to}) {
				t.Errorf("mid-drag the divider is drawn at %v, want %d where the hand is:\n%s", got, to, stripANSI(frame))
			}
			if w, h := widest(frame), lipgloss.Height(frame); w != 200 || h != 40 {
				t.Errorf("mid-drag the frame is %dx%d, want the terminal's 200x40", w, h)
			}
		})
	}
}

// Letting go commits the drag at once: the settle exists to coalesce motions,
// and none follow a release. The timer it scheduled then finds nothing to do.
func TestReleasingTheDividerReWrapsOnceAndTheSettleNothing(t *testing.T) {
	a := splitApp(t, 200, 40, 20)
	from := dividerColumnOf(a)
	a = grab(t, a, from)
	to := from - 30
	for x := from - 1; x >= to; x-- {
		a = dragTo(a, x)
	}
	pending := a.geoGen
	room, dm := countPaneRenders(t, func() { a = release(a, to) })
	if room != 1 || dm != 1 {
		t.Errorf("letting go re-wrapped the room %d and the DM %d times, want once each", room, dm)
	}
	if a.room.width != to {
		t.Errorf("after letting go the room is %d wide, want the %d the hand left it at", a.room.width, to)
	}
	room, dm = countPaneRenders(t, func() { a = a.settled(pending) })
	if room+dm != 0 {
		t.Errorf("the drag's settle landed after the release and re-wrapped (room %d, DM %d), want nothing", room, dm)
	}
	if got := dividerColumnsIn(a.View(), a.paneHeight()); !slices.Equal(got, []int{to}) {
		t.Errorf("after letting go the divider is at %v, want %d", got, to)
	}
}

// A window drag in flight when the hand lets go keeps its settle: the terminal
// width is still moving, so committing it on the release would re-wrap for a
// size the window is passing through, and again when it stops.
func TestLettingGoMidWindowDragLeavesItToTheSettle(t *testing.T) {
	a := splitApp(t, 200, 40, 20)
	from := dividerColumnOf(a)
	a = grab(t, a, from)
	a = dragTo(a, from-30)
	dragged := a.pending.weights
	a, _ = a.resized(190, 40) // the window moves while the hand is still down
	room, dm := countPaneRenders(t, func() { a = release(a, from-30) })
	if room+dm != 0 {
		t.Errorf("letting go mid-window-drag re-wrapped (room %d, DM %d), want nothing until the window settles", room, dm)
	}
	room, dm = countPaneRenders(t, func() { a = settle(a) })
	if room != 1 || dm != 1 {
		t.Errorf("the shared settle re-wrapped the room %d and the DM %d times, want once each", room, dm)
	}
	if a.layout.Width != 190 || !slices.Equal(a.layout.Weights, dragged) {
		t.Errorf("the settle applied width %d and weights %v, want 190 and the drag's %v", a.layout.Width, a.layout.Weights, dragged)
	}
}

// The wheel is the one other thing the mouse can do while the button is held,
// and it scrolls the pane drawn under the pointer, not the one the layout the
// panes are still wrapped for would put there.
func TestTheWheelMidDragScrollsThePaneDrawnUnderIt(t *testing.T) {
	a := splitApp(t, 200, 40, 20)
	for i := range 80 {
		a = said(a, "s1", fmt.Sprintf("line %d of the conversation", i))
	}
	a = a.applyGeometry()
	from := dividerColumnOf(a)
	a = grab(t, a, from)
	to := from - 40
	for x := from - 1; x >= to; x-- {
		a = dragTo(a, x)
	}
	roomWas, dmWas := a.room.tr.scroll, a.dms["s1"].tr.scroll
	// Right of where the divider is drawn, and left of where it was: the DM on
	// screen, the room in the layout still waiting on the release.
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: to + 5, Y: 5})
	if a.dms["s1"].tr.scroll == dmWas {
		t.Errorf("the wheel over the conversation drawn right of the divider did not scroll it")
	}
	if a.room.tr.scroll != roomWas {
		t.Errorf("the wheel scrolled the room, which is drawn left of the pointer")
	}
}

// Taking hold of the divider is a width change for both panes, and a width
// change clears the highlight: a re-wrap renumbers the lines it is anchored to,
// and the divider's settle used to re-wrap under one left standing.
func TestTakingHoldOfTheDividerClearsTheSelection(t *testing.T) {
	a := splitApp(t, 200, 40, 20)
	first := a.room.tr.first()
	a.sel = selection{anchor: point{line: first}, head: point{line: first + 1, col: 5}}
	if a.sel.empty() {
		t.Fatal("precondition: the selection is empty before the drag")
	}
	if a = grab(t, a, dividerColumnOf(a)); !a.sel.empty() {
		t.Errorf("the selection survived a hand on the divider: the drag's re-wrap would leave it over other text")
	}
}
