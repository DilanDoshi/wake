package ui

// The sidebar's window over a roster taller than its column, starting at an item
// (an agent with the gap and header it leads with). The wheel holds it where it
// left it; otherwise it moves the least that shows the cursor, worked out at every
// draw, so a re-rank cannot strand the cursor unless the wheel holds the window -
// and then a selection it does not draw takes no key. decisions.md, 2026-11-06.

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// pickOutOfView is what a key refused for a selection out of view says.
const pickOutOfView = "the agent selected in the sidebar is scrolled out of view. ⇧↑↓ or the wheel brings it back"

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

// window is the items a column `height` rows tall draws: all of them when they
// fit, else from the view's top kept inside the list and, unless the wheel holds
// it, moved the least that draws the cursor's item whole - first when it was
// above, last when below. An item taller than the room goes first and draws as
// the count alone, which never understates the fleet. The count takes a row
// whenever anything is left out.
func (r Roster) window(rows []rosterItem, h heights, height int) span {
	if h.total() <= height {
		return span{from: 0, to: len(rows)}
	}
	room := height - 1
	top := clamp(r.view.top, 0, h.lastTop(room))
	c := r.cursorRow(rows)
	switch {
	case c < 0 || r.view.held || c >= top && c < h.grow(top, room).to:
	case c < top:
		top = c
	default:
		used := h[c]
		for top = c; top > 0 && used+h[top-1] <= room; top-- {
			used += h[top-1]
		}
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
// window was drawn, and stores where it is drawn now. While the wheel holds it,
// whether the selection is drawn is decided only when the fleet, the window or
// its rows changed.
func (a App) rosterSettled(was App) App {
	if a.roster != was.roster {
		_, _, drawn := was.rosterWindow()
		a.rosterView = rosterView{top: drawn.from}
		_, _, w := a.rosterWindow() // the new cursor shown the least from there, kept
		a.rosterView.top = w.from
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
// pickedAgent, pickedHeader and dmTarget read it. The conversation holding the
// keys is on screen as its pane, whatever the sidebar shows.
func (a App) rosterPickHidden() bool {
	return a.rosterView.held && !a.rosterView.shown && (a.focus == "" || a.roster.Selected != a.focus)
}

// unseenOr is a key's no-target notice: why, when the selection is out of view.
func (a App) unseenOr(advice string) string {
	if a.rosterPickHidden() {
		return pickOutOfView
	}
	return advice
}
