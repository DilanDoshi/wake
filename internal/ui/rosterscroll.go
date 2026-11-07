package ui

// The sidebar's window over a roster taller than its column (the owner's
// 2026-11-06 ask, wake-orch's rulings). It starts at an item - an agent with the
// gap and team header it leads with - so a header never parts from its first
// member, and View, At and Hit walk whole items as they always have.
//
// Two things move it. The wheel over the sidebar scrolls it an item a notch and
// holds it there: the selection does not move and nothing pulls the window back
// to it. Otherwise it stays where it was drawn and moves the least that shows the
// cursor, for a ⇧↑↓, a click, ⌃X or a re-rank alike.
//
// The stored top is only where the window starts. Whether the cursor is inside
// it is worked out at every draw, so a re-rank never leaves the cursor off screen
// - the case that once argued for no stored offset at all - unless the wheel
// holds the window, and then a selection it does not draw takes no key
// (rosterPickHidden, the one place that rule lives, decided in Update's seam on
// a change, never per frame). Heights come from rowsFor, never a render: choosing
// a window is arithmetic.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// rosterView is where the sidebar's window stands.
type rosterView struct {
	top   int  // the first item drawn once the roster overflows
	held  bool // the wheel put it there: nothing pulls it back to the selection
	shown bool // while held, whether the selection is drawn; Update's seam keeps it
}

// heights is each roster item's rows, the one count a window is chosen over.
type heights []int

func itemHeights(rows []rosterItem, subs subsOf) heights {
	h := make(heights, len(rows))
	for i, row := range rows {
		h[i] = rowsForRow(row, subs)
	}
	return h
}

func (h heights) total() int {
	n := 0
	for _, rows := range h {
		n += rows
	}
	return n
}

// grow is the items from top that fit in room.
func (h heights) grow(top, room int) span {
	used, to := 0, top
	for to < len(h) && used+h[to] <= room {
		used += h[to]
		to++
	}
	return span{from: top, to: to}
}

// lastTop is the lowest top whose window reaches the last item, so a window is
// never left drawing blank rows under the fleet's end.
func (h heights) lastTop(room int) int {
	used, top := 0, len(h)
	for top > 0 && used+h[top-1] <= room {
		used += h[top-1]
		top--
	}
	return max(min(top, len(h)-1), 0)
}

// reveal is the smallest move of top that draws item c whole: c first when it is
// above the window, last when below. An item taller than the room goes first and
// draws as the count alone, which never understates the fleet.
func (h heights) reveal(top, c, room int) int {
	if c < top {
		return c
	}
	if c < h.grow(top, room).to {
		return top
	}
	used, from := h[c], c
	for from > 0 && used+h[from-1] <= room {
		used += h[from-1]
		from--
	}
	return from
}

// window is the items a column `height` rows tall draws: all of them when they
// fit, else from the view's top kept inside the list and, unless the wheel holds
// it, moved the least that shows the cursor. The count takes a row whenever
// anything is left out.
func (r Roster) window(rows []rosterItem, h heights, height int) span {
	if h.total() <= height {
		return span{from: 0, to: len(rows)}
	}
	room := height - 1
	top := clamp(r.view.top, 0, h.lastTop(room))
	if c := r.cursorRow(rows); c >= 0 && !r.view.held {
		top = h.reveal(top, c, room)
	}
	return h.grow(top, room)
}

// countLine is what the column says about the agents its window leaves out, and
// which way they are: "+5 above", "+3 below", "+5 above · +2 below". A column too
// narrow to say both says "+7 more".
func countLine(above, below, width int) string {
	var parts []string
	if above > 0 {
		parts = append(parts, "+"+strconv.Itoa(above)+" above")
	}
	if below > 0 {
		parts = append(parts, "+"+strconv.Itoa(below)+" below")
	}
	if line := "  " + strings.Join(parts, " · "); ansi.StringWidth(line) <= width {
		return line
	}
	return moreLine(above + below)
}

// rosterDrawn is the roster as the sidebar draws it and a click reads it.
func (a App) rosterDrawn() Roster {
	r := a.roster
	r.view = a.rosterView
	return r
}

// rosterRows is the rows the roster's agents have, under the pinned all.
func (a App) rosterRows() int { return a.rosterHeight() - allRows }

// rosterWindow is the sidebar's items as drawn, their heights, and the window
// over them - each worked out once.
func (a App) rosterWindow() ([]rosterItem, heights, span) {
	rows := sectionRows(a.fleet.sectioned(a.fleet.OnRoster()))
	h := itemHeights(rows, a.fleet.RunningTasks)
	return rows, h, a.rosterDrawn().window(rows, h, a.rosterRows())
}

// wheelRoster is a notch over the sidebar: the window an item on (down) or back,
// and held. A roster that fits takes no wheel - the seam lets go of the hold, and
// nothing else moves, since the pointer says what scrolls.
func (a App) wheelRoster(lines int) App {
	_, h, w := a.rosterWindow()
	step := 1
	if lines > 0 { // a wheel up reads back toward the top
		step = -1
	}
	// shown is kept: a notch the end stops moves nothing, and the seam decides
	// again only on a change.
	a.rosterView.top, a.rosterView.held = clamp(w.from+step, 0, h.lastTop(a.rosterRows()-1)), true
	return a
}

// rosterSettled is Update's seam for the window, against the model before the
// message. A selection that changed lets go of the wheel's hold from where the
// window was drawn. While the wheel holds it, whether the selection is drawn is
// decided only when the fleet, the window or its rows changed.
func (a App) rosterSettled(was App) App {
	if a.roster != was.roster {
		_, _, w := was.rosterWindow()
		a.rosterView = rosterView{top: w.from}
		return a
	}
	v, old := a.rosterView, was.rosterView
	if !v.held || old.held && v.top == old.top && a.fleet.same(was.fleet) && a.rosterRows() == was.rosterRows() {
		return a
	}
	rows, h, w := a.rosterWindow()
	if h.total() <= a.rosterRows() {
		a.rosterView = rosterView{} // it all fits: nothing to hold
		return a
	}
	c := a.roster.cursorRow(rows)
	a.rosterView.shown = a.roster.Header == core.BroadcastName || c >= w.from && c < w.to
	return a
}

// rosterPickHidden is the one ruling on a selection the wheel scrolled out of
// view (wake-orch, 2026-11-06): it takes no key until it is drawn again.
// pickedAgent, pickedHeader and dmTarget read it.
func (a App) rosterPickHidden() bool { return a.rosterView.held && !a.rosterView.shown }
