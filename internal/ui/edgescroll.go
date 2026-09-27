package ui

// A drag held at a pane's edge keeps scrolling, so a selection can run on past
// the window without the hand having to move.
//
// Motion alone could not do it: a pointer resting at the edge sends nothing,
// and a pane that starts on the window's first row has no row above it for a
// pointer to reach. So the transcript's own first row is the top edge, and a
// one-shot tick repeats the scroll while the button stays down there - tmux's
// drag timer, at tmux's rate. It is armed only by a transcript drag at an edge
// and re-armed only while the pane still moved, so an idle Wake schedules
// nothing (beat.go's bound), even under a release that never arrives.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// edgeScrollEvery is how often a drag held at an edge scrolls another line.
const edgeScrollEvery = 50 * time.Millisecond

// edgeScrollTimer is the seam the tick is scheduled through, for beat.go's
// reason: a test delivers the tick itself and counts what was armed.
var edgeScrollTimer = tea.Tick

// edgeScrollMsg is one tick of a drag held at an edge.
type edgeScrollMsg struct{}

// edgeScroll is where a transcript drag's pointer last was, and whether a tick
// is already in flight.
type edgeScroll struct {
	x, y  int
	armed bool
}

// edgePull is how far the pointer at y, over the line under, pulls the dragged
// pane: back at or above its first row, forward past its last, else nothing. A
// drag still on the line it was pressed on is selecting within it, so the top
// row does not pull that.
func (a App) edgePull(y int, under point) int {
	switch {
	case y < a.selTop, y == a.selTop && under.line != a.sel.anchor.line:
		return edgeLines
	case y >= a.selTop+a.selRows:
		return -edgeLines
	}
	return 0
}

// holdAtEdge arms the next tick after a pull that moved the pane. One is in
// flight at most; it serves whichever drag is live when it lands.
func (a App) holdAtEdge(moved bool) (App, tea.Cmd) {
	if !moved || a.edge.armed {
		return a, nil
	}
	a.edge.armed = true
	return a, edgeScrollTimer(edgeScrollEvery, func(time.Time) tea.Msg { return edgeScrollMsg{} })
}

// edgeTicked is a tick landing: the drag moves again from where its pointer
// rests. A frame-wide or query-box drag has nothing to scroll.
func (a App) edgeTicked() (App, tea.Cmd) {
	a.edge.armed = false
	if !a.selecting || a.sel.onScreen || a.sel.inComposer {
		return a, nil
	}
	return a.extendSelection(a.edge.x, a.edge.y)
}
