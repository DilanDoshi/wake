package ui

// The board's scroll. Both views draw `view` lines out of a taller list, and
// Board.Scroll is the first line drawn. Two inputs move it (owner's 2026-10-12
// ask): the wheel and the page keys scroll the window freely, and an arrow key
// moves the selection and brings it into the window.
//
// One rule keeps both safe: the selection is always on screen, because ⌃C parks
// and ↵ opens whatever the highlight is on. A free scroll that leaves it behind
// carries it along (landing), and a re-rank or a resize that strands it pulls the
// window back (viewport's off). Tiles still hold no scrollback of their own
// (board.go's guardrail 2) - it is the grid of them that scrolls.

import (
	"fmt"
)

// band is one stop the selection can rest on: the lines it occupies, and the
// header band drawn above it that comes into view with it.
type band struct{ top, h, lead int }

func (b band) bottom() int { return b.top + b.h }

// overlap is how many of the band's own lines the window [off, off+view) shows.
func (b band) overlap(off, view int) int {
	return max(min(b.bottom(), off+view)-max(b.top, off), 0)
}

// mostlyIn is at least half the band on screen, or the whole window when the band
// is taller than it. Below that a tile is a sliver and not somewhere to rest.
func (b band) mostlyIn(off, view int) bool {
	o := b.overlap(off, view)
	return o > 0 && (o*2 >= b.h || o == view)
}

// reveal is the smallest move of off that shows the whole band, its header too
// when both fit, and off itself when they already show. A band taller than the
// window is aligned to its top.
func (b band) reveal(off, view int) int {
	top := b.top
	if b.lead+b.h <= view {
		top -= b.lead
	}
	switch {
	case top < off:
		return top
	case b.bottom() > off+view:
		return min(b.bottom()-view, top)
	}
	return off
}

// clampScroll keeps the window inside the list.
func clampScroll(off, total, view int) int { return clamp(off, 0, max(total-view, 0)) }

// landing is where the selection rests after a scroll left it behind: itself
// while it is still mostly on screen, else the nearest band toward the window
// that is, else whichever shows the most. -1 for no bands.
func landing(bands []band, cur, off, view int) int {
	if len(bands) == 0 {
		return -1
	}
	cur = clamp(cur, 0, len(bands)-1)
	if bands[cur].mostlyIn(off, view) {
		return cur
	}
	step := 1 // starts above the window's top: look down toward it
	if bands[cur].top > off {
		step = -1 // starts inside or below it, clipped at the bottom or out of it: look up
	}
	for i := cur + step; i >= 0 && i < len(bands); i += step {
		if bands[i].mostlyIn(off, view) {
			return i
		}
	}
	best, most := cur, -1
	for i, b := range bands {
		if o := b.overlap(off, view); o > most {
			best, most = i, o
		}
	}
	return best
}

// viewport is a window over a list of bands: where it starts, the band the
// selection is on, and the whole list's height. Both views carry one.
type viewport struct {
	total, view int
	off         int // the first line drawn, after clamping and after pulling the selection back
	cur         int // the selection's band, -1 when there are none
	bands       []band
}

// settled is the window's start for a stored scroll: kept inside the list, and
// moved only as far as it takes to bring a selection that has left it back.
func (v viewport) settled(scroll int) viewport {
	v.off = clampScroll(scroll, v.total, v.view)
	if v.cur >= 0 && v.bands[v.cur].overlap(v.off, v.view) == 0 {
		v.off = clampScroll(v.bands[v.cur].reveal(v.off, v.view), v.total, v.view)
	}
	return v
}

// scrollable is what the two views share: a viewport, the agents it touches, a
// way to put the selection on one of its bands, and the stops at either end.
type scrollable interface {
	vp() viewport
	visibleAgents() []Agent
	selectBand(a App, i int) App
	endpoint(last bool) (agent, task string) // the first or last stop, for Home and End
}

func (v viewport) vp() viewport { return v }

func (a App) boardScrollable(agents []Agent) scrollable {
	if a.board.Tiled {
		return a.boardTileLayout(agents)
	}
	return a.boardRowLayout(agents)
}

// scrollBoard is the wheel and the page keys: the window moves by delta lines, and
// the selection goes with it once it is no longer mostly on screen.
func (a App) scrollBoard(delta int) App {
	agents := a.boardAgents()
	if len(agents) == 0 {
		return a
	}
	s := a.boardScrollable(agents)
	v := s.vp()
	a.board.Scroll = clampScroll(v.off+delta, v.total, v.view)
	if to := landing(v.bands, v.cur, a.board.Scroll, v.view); to >= 0 && to != v.cur {
		a = s.selectBand(a, to)
	}
	return a
}

// jumpBoard is Home and End: the window to the top or the bottom, the selection
// on the first or last stop. The last stop rather than the last agent, so that it
// is on screen at the bottom even when the last agent's block is taller than the
// window.
func (a App) jumpBoard(end bool) App {
	agents := a.boardAgents()
	if len(agents) == 0 {
		return a
	}
	s := a.boardScrollable(agents)
	v := s.vp()
	a.board.Scroll = 0
	if end {
		a.board.Scroll = max(v.total-v.view, 0)
	}
	a.board.Selected, a.board.SelectedTask = s.endpoint(end)
	return a
}

// revealBoardSelection is an arrow key's scroll: the smallest move that shows all
// of the band the selection just landed on.
func (a App) revealBoardSelection(agents []Agent) App {
	v := a.boardScrollable(agents).vp()
	if v.cur >= 0 {
		a.board.Scroll = clampScroll(v.bands[v.cur].reveal(v.off, v.view), v.total, v.view)
	}
	return a
}

// boardTitleLine heads the board: the fleet's count, and which of the fleet the
// window shows once that is not all of it.
func boardTitleLine(agents, visible []Agent, width int) string {
	title := fmt.Sprintf("%s — %d agents", boardTitle, len(agents))
	if n := len(visible); n > 0 && n < len(agents) {
		first := indexOf(agents, visible[0].ID) + 1
		title += fmt.Sprintf(" · showing %d–%d", first, first+n-1)
	}
	return mutedLine(title, width)
}
