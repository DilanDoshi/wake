package ui

// One pending geometry and one settle, shared by the two things that change a
// pane's width: the terminal reporting a new size, and a hand on the divider.

import (
	"slices"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// resizeQuiet is how long the geometry has to stop changing before the panes
// are re-wrapped for it.
//
// A resize is a drag, not a step. Re-wrapping costs the whole transcript
// through glamour - 248ms at 3,000 events, measured - and a drag delivers a new
// width as fast as this loop will take one, so re-wrapping on each one spends
// the length of the drag doing work that every later width throws away. Waiting
// for the drag to stop makes it one re-wrap.
//
// Short enough that letting go of the mouse and seeing the text re-flow feels
// like the same action; long enough that no drag a hand can produce gets
// through it. The frame is still drawn at every intermediate width - see View -
// so nothing about the wait is invisible.
const resizeQuiet = 80 * time.Millisecond

// geometry is the pane sizes the terminal has most recently reported or the
// operator has most recently dragged to. It is *pending* until it settles.
//
// One value for two producers, and that is the design rather than tidiness.
// Re-wrapping is the expensive thing this model does, and both a window drag
// and a divider drag deliver a new width as fast as this loop will take one -
// so both need the same 80ms of quiet, and two debounces would let an older
// timer land after a newer change and re-wrap for a geometry that no longer
// exists. TestAWindowDragAndADividerDragShareOneSettleRatherThanRacing is where
// that is held.
type geometry struct {
	width   int
	height  int
	weights []float64
}

// App.pending is the newest geometry; App.layout is the one the transcripts are
// wrapped for. They differ only during a drag, and until then the frame is
// drawn at the old wrap and clipped to the new terminal.

// geometrySettledMsg says the geometry has stopped changing. gen is the change
// it belongs to, so a later one is not undone by an older timer landing after
// it.
type geometrySettledMsg struct{ gen uint64 }

// settleTimer is how a settle is scheduled: the message arrives after the
// quiet, and not before. It is the seam changeGeometry is reached through, in
// exactly the way renderTranscript is the seam a re-wrap is reached through,
// and for the same reason - the property worth guarding is a cost, and a cost
// is only visible to a test that can run the real loop.
//
// A var because the *delay* is the whole guarantee, and the alternative ways to
// check it are both bad. Waiting out 80ms of wall clock per drag column is the
// load-sensitive test internal/daemon has spent weeks on. Delivering the
// message by hand, which is what every test here did first, never runs the
// command at all - so replacing this line with an immediate `func() tea.Msg`
// left the entire suite green while a 40-column drag went from 93ms to 13.4
// seconds. Swapping the seam lets a test drive Update the way Bubble Tea drives
// it, hold the scheduled messages until the drag is over, and count what the
// drag actually cost.
//
// Reach it through this, never by calling tea.Tick directly: a direct call is
// invisible to the seam, and TestADragCostsOneReWrapWhenTheLoopRunsEveryCommand
// would keep passing while no longer discriminating.
var settleTimer = tea.Tick

// resized takes a new terminal size, keeping the divider where the operator put
// it: Split is a fraction rather than a column precisely so a resize preserves
// the proportion instead of pinning one pane and giving every new column to the
// other.
func (a App) resized(w, h int) (App, tea.Cmd) {
	g := a.pending
	g.width, g.height = w, h
	return a.changeGeometry(g)
}

// changeGeometry takes a new geometry and schedules the re-wrap.
//
// Height lands immediately; width and the split do not. That asymmetry is the
// cost model rather than a heuristic - only a width change re-wraps a
// transcript, and a height change moves a window over lines that are already
// rendered. The first geometry is applied whole, because there is no drag to
// wait out and a first frame drawn at a guessed width is a frame nobody asked
// for.
func (a App) changeGeometry(g geometry) (App, tea.Cmd) {
	a.pending = g
	a.geoGen++

	if a.layout.Width == 0 || g.width == a.layout.Width && slices.Equal(g.weights, a.layout.Weights) {
		return a.applyGeometry(), nil
	}
	a = a.applyHeightOnly()

	gen := a.geoGen
	return a, settleTimer(resizeQuiet, func(time.Time) tea.Msg { return geometrySettledMsg{gen: gen} })
}

// settled applies the pending geometry if it is still the newest one.
//
// A timer belonging to a geometry the terminal has already left must not
// re-wrap for it: without the generation check the first tick of a drag lands
// mid-drag and re-wraps for a width nobody is looking at, which is the cost the
// debounce exists to avoid, paid anyway.
func (a App) settled(gen uint64) App {
	if gen != a.geoGen {
		return a
	}
	return a.applyGeometry()
}

// applyGeometry commits the pending geometry: the layout takes the new width
// and split, and the panes are re-laid for it. This is the expensive one and it
// happens once per settle.
func (a App) applyGeometry() App {
	// A re-wrap renumbers transcript.lines, so a selection's indices stop
	// pointing at the text they were taken on - and a highlight sliding onto
	// different words is worse than one that goes away. Only a *width* change
	// re-wraps, which is the same reason it is the only one that returns a
	// reader to the newest line. A screen selection is the exception: it is
	// anchored to absolute screen rows, so a *height* change re-lays the frame
	// under it too and it must go as well.
	if a.layout.Width != a.pending.width || (a.sel.onScreen && a.layout.Height != a.pending.height) {
		a.sel, a.selecting = selection{}, false
	}
	a.layout.Width, a.layout.Height, a.layout.Weights = a.pending.width, a.pending.height, a.pending.weights
	return a.resizePanes()
}

// applyHeightOnly takes the half that costs nothing. SetSize re-renders only
// when the width it is given differs from the one a transcript is wrapped for,
// so re-laying with the layout's *old* width and the new height moves a window
// over lines that already exist - which is what makes the frame keep up during
// a drag without paying for one.
func (a App) applyHeightOnly() App {
	a.layout.Height = a.pending.height
	return a.resizePanes()
}

// resizePanes lays every pane the layout is drawing out for it. The one
// expensive call in this file, and the only one.
//
// A pane the layout is not drawing keeps the wrap it had. Sizing it anyway
// would re-wrap the room to nothing every time the terminal crossed the
// 120-column takeover, and again on the way back - two whole re-wraps for a
// pane nobody could see in between.
// A column too narrow to draw cannot take the focus away, because the drawn
// window follows it - Layout.window is what makes that true. A column too
// *short* to stack still can, and that is corrected here: this is the one place
// that knows a new layout has been applied, and the terminal can lose the rows
// while a lower pane has the keys, from a drag or from a window manager, with
// no key pressed at all.
func (a App) resizePanes() App {
	r := a.regions()
	h := a.paneHeight()
	for i, w := range r.Cols {
		if w == 0 {
			continue
		}
		c := a.grid.Cols[i]
		if c.Bottom == "" {
			a = a.sizePane(c.Top, w, h)
			continue
		}
		top, bottom := a.layout.SplitRowsIn(i, h)
		if bottom == 0 {
			// Not enough rows for two: the lower conversation is not drawn, and
			// a pane nobody can see may not hold the keys.
			if a.focus == c.Bottom {
				a = a.withFocus(c.Top)
			}
			a = a.sizePane(c.Top, w, top)
			continue
		}
		a = a.sizePane(c.Top, w, top).sizePane(c.Bottom, w, bottom)
	}
	return a
}

// sizePane re-lays one conversation. "" is the room, the same convention the
// grid and the focus use.
func (a App) sizePane(id string, w, h int) App {
	if id == "" {
		a.room = a.room.SetSize(w, h)
		// The bar re-renders at the new width, the way DM.SetSize does its own -
		// but the room's agent lives in the fleet, not on the pane, so it is
		// resolved here rather than inside SetSize.
		return a.withRoomBar()
	}
	return a.withDM(id, a.dms[id].SetSize(w, h))
}

// withSplitAt is this geometry with the divider moved to a terminal column,
// clamped by the layout so neither pane becomes unreadable.
//
// The clamp reads the layout the panes are currently *drawn* at rather than the
// pending one, which is what keeps a drag from accumulating: where the divider
// may go depends on the two panes' combined width and on where the left sidebar
// ends, and a mid-drag split changes neither.
func (g geometry) withSplitAt(l Layout, r Regions, at, x int) geometry {
	g.weights = l.DragDivider(r, at, x).Weights
	return g
}

// paneHeight is what is left for a pane once the notice row is taken. Every
// column of the frame is exactly this tall, which is what keeps the frame the
// height the terminal reported.
func (a App) paneHeight() int { return a.layout.Height - noticeHeight - stripHeight }

// pageLines is how far a page key scrolls.
func (a App) pageLines() int { return max(1, a.paneHeight()/pageFraction) }
