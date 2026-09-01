package ui

// The mouse: the wheel, a click, a drag on the divider, and a drag across text.

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// wheelLines is how far one notch of the wheel moves a transcript.
	wheelLines = 3

	// noDrag is App.dragAt with no hand on any divider.
	noDrag = -1

	// edgeLines is how far a drag past a pane's edge scrolls it per motion, so
	// a selection can reach further than the window it started in.
	edgeLines = 1
)

// mouse handles the wheel, a press, a drag and a release.
//
// # Why motion is handled here and not forwarded
//
// bubbletea v1.3.10 offers only mouse modes 1002 and 1003, and 1002 already
// reports motion while a button is held. There is no wheel-only mode to fall
// back to, so enabling the click that starts a divider drag necessarily enables
// a message per cell crossed while dragging. Two things make that affordable
// and both are load-bearing: nothing here forwards motion into a text area,
// which would be work per mouse pixel for no behaviour; and a divider drag
// changes only the pending geometry, so the expensive half waits out the
// settle.
//
// A text drag is the third thing motion now means, and it is the one that does
// *not* wait: a highlight lagging 80ms behind the pointer would feel broken,
// and unlike a width change there is nothing expensive on the far side to
// defer. Its cost is bounded by the pane's height rather than the
// conversation's length - see transcript.view.
//
// Everything the mouse can produce is consumed here rather than falling through
// to the composer, for the same reason.
func (a App) mouse(m tea.MouseMsg) (App, tea.Cmd) {
	// The board owns the whole frame while it is up, so a click or a wheel is
	// the board's: a drag reaching the divider logic would resize panes that
	// are not drawn. See board.go.
	if a.board.Up {
		return a.boardMouse(m)
	}
	switch {
	case m.Button == tea.MouseButtonWheelUp:
		return a.scroll(wheelLines, m.X, m.Y), nil
	case m.Button == tea.MouseButtonWheelDown:
		return a.scroll(-wheelLines, m.X, m.Y), nil
	case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft:
		return a.press(m.X, m.Y), nil
	case m.Action == tea.MouseActionRelease:
		// The button may be reported as None here: X10 encoding loses which one
		// was let go of, and there is only one drag to end.
		a.dragAt, a.dragRows = noDrag, false
		return a.endSelection()
	case m.Action == tea.MouseActionMotion && a.dragAt != noDrag:
		if m.Button == tea.MouseButtonNone {
			// 1002 reports motion only while a button is held, so this is a
			// release that never arrived - under 1003, or from a terminal that
			// swallowed it. Ending the drag here is what stops every later
			// motion from moving a divider nobody is holding.
			a.dragAt, a.dragRows = noDrag, false
			return a, nil
		}
		if a.dragRows {
			// A rule moves rows, and rows are the half of a geometry change that
			// costs nothing: only a *width* change re-wraps a transcript. So it
			// applies at once rather than through the settle - see changeGeometry.
			a.layout = a.layout.DragRule(a.dragAt, m.Y, a.paneHeight())
			return a.resizePanes(), nil
		}
		return a.changeGeometry(a.pending.withSplitAt(a.layout, a.regions(), a.dragAt, m.X))
	case m.Action == tea.MouseActionMotion && a.selecting:
		if m.Button == tea.MouseButtonNone {
			// A release that never arrived, read the way a divider drag reads
			// one: end it here rather than extending a selection nobody holds.
			return a.endSelection()
		}
		return a.extendSelection(m.X, m.Y), nil
	}
	return a, nil
}

// draggingRows reports whether the hand on the mouse is on a stacked column's
// rule rather than on a divider between columns.
func (a App) draggingRows() bool { return a.dragAt != noDrag && a.dragRows }

// rowsOf is how a stacked column's height is divided, for a caller that knows
// which column it means.
func (a App) rowsOf(col int) (top, bottom int) {
	return a.layout.SplitRowsIn(col, a.paneHeight())
}

// press routes a click by which region it landed in: it takes hold of the
// divider, or moves the focus to the pane it landed in and takes an anchor
// there for a drag that may follow.
//
// It is no longer the only path to a focus change - ⇥ is the keyboard's, and a
// two-pane window whose room could never take a keystroke was one where @all
// was unreachable for as long as a DM was open. Both go through App.withFocus,
// so a click and a key leave the same state behind.
//
// A click on the activity sidebar opens that agent's conversation, which is
// what noPaneAdvice has told operators to do since Phase 2 while nothing here
// took the y coordinate. The workspaces sidebar still answers nothing: a
// workspace is not a conversation and there is no verb for one yet.
func (a App) press(x, y int) App {
	r := a.regions()
	switch region, at := a.layout.Hit(r, x); region {
	case RegionDivider:
		a.dragAt, a.dragRows = at, false
	case RegionPane:
		id, top, height, ok := a.paneAt(at, y)
		if !ok {
			// Between the two halves of a stacked column: the rule, which is a
			// thing to take hold of rather than a pane to focus. It belongs to
			// neither conversation, which is what makes it draggable.
			a.dragAt, a.dragRows = at, true
			return a
		}
		// The anchor is taken before the keys move, because the frame that was
		// clicked is the one drawn *before* the move: refocus re-sizes the panes,
		// and a picker belongs to whichever pane holds the keys - so measuring
		// after it counts another pane's menu against this one.
		//
		// refocused is whether this press moves the keys to a pane that did not
		// hold them: a click that changes focus only focuses, and does not also
		// open the run under the pointer. Read here, before the move, for the
		// same reason the anchor is. See clickedTool.
		refocused := a.focus != id
		a = a.startSelection(id, at, top, height, x, y, r, refocused)
		a.fleet = a.fleet.Focus(id)
		a = a.refocus(id)
	case RegionRoster:
		if agent, dispatch, ok := a.clickedAgent(y); ok {
			a = a.openDMWith(agent.ID, agent.Name)
			// Both fields together, always. show() writes Selected on every
			// open and never touches SelectedTask, so a click that resolved to
			// no dispatch used to leave the *previous* agent's dispatch
			// attached to this one - a pair Move and walkable cannot produce,
			// which then draws the cursor on a row nobody clicked and asks this
			// conversation to show a transcript belonging to another agent.
			a.roster.Selected, a.roster.SelectedTask = agent.ID, dispatch
			a = a.viewingPicked(agent.ID)
		}
	case RegionNone, RegionGroups:
	}
	return a
}

// paneAt is the conversation in a column at a terminal row, where its own rows
// start, and how many it was given.
//
// The rule between two stacked panes belongs to neither, so a click on it moves
// nothing - the same answer a click between two columns gets, read the other
// way. The top and the height are what a text drag needs: a screen row is only
// a transcript line once you know where the pane starts.
func (a App) paneAt(col, y int) (id string, top, height int, ok bool) {
	if col < 0 || col >= len(a.grid.Cols) {
		return "", 0, 0, false
	}
	h := a.paneHeight()
	c := a.grid.Cols[col]
	if c.Bottom == "" {
		return c.Top, 0, h, true
	}
	upper, lower := a.layout.SplitRowsIn(col, h)
	switch {
	case lower == 0 || y < upper:
		return c.Top, 0, upper, true
	case y < upper+dividerHeight:
		return "", 0, 0, false
	default:
		return c.Bottom, upper + dividerHeight, lower, true
	}
}

// scroll moves the transcript under the pointer by lines, negative for
// forwards.
//
// Under the pointer rather than focused, which only became a distinction worth
// making with a grid: with one conversation the two were the same pane, and
// with four on screen scrolling the focused one while reading another is the
// wrong pane every time. It moves no keys - reading is not focusing, and a wheel
// that stole the focus would send the next keystroke somewhere nobody chose.
//
// A pointer over a sidebar, a divider or a rule scrolls the focused pane, which
// is the honest fallback: there is no transcript under it to move.
func (a App) scroll(lines, x, y int) App {
	if region, at := a.layout.Hit(a.regions(), x); region == RegionPane {
		if id, _, _, ok := a.paneAt(at, y); ok {
			return a.scrollPane(id, lines)
		}
	}
	return a.scrollFocused(lines)
}

// scrollPane moves one conversation's scrollback, the room's for "".
func (a App) scrollPane(id string, lines int) App {
	if id == "" {
		a.room = a.room.ScrollUp(lines)
		return a
	}
	return a.withDM(id, a.dms[id].ScrollUp(lines))
}

// columnOf is the column a conversation is drawn in, and 0 - the room's - for
// one that is not on screen.
func (a App) columnOf(id string) int {
	return max(slices.IndexFunc(a.grid.Cols, func(c Column) bool {
		return c.Top == id || c.Bottom == id
	}), 0)
}

// transcriptIn is one conversation's scrollback, the room's for "".
func (a App) transcriptIn(id string) transcript {
	if id == "" {
		return a.room.tr
	}
	return a.dms[id].tr
}

// selectionIn is the selection resolved for one pane: nothing unless the drag
// was taken in that pane.
//
// This is the whole of what a pane knows about selection, which is what keeps
// transcript a pure value that has never heard of a pane or an id.
func (a App) selectionIn(id string) marked {
	if a.sel.empty() || a.sel.inComposer || a.sel.pane != id {
		return marked{}
	}
	return a.sel.marked()
}

// startSelection takes the anchor a drag will run from.
//
// The transcript is the pane's first row: the card, the picker and the
// completion menu are all pinned above the *composer* now (see menuBlock), not
// above the transcript, so there is nothing to offset the anchor by. It used to
// measure paneChrome here, and every drag in a pane moved by a card's height the
// moment an agent asked something - while somebody was reading that very pane.
//
// It is the transcript's *rows* that are text, though, and a pane is a whole
// column. pointIn clamps a row under the transcript - the working line, the
// menu, the composer - into a real line, so a drag across the query bar once
// highlighted and copied an answer nobody dragged over, the further back the
// reader had scrolled the further from the pointer it landed. So a press below
// the transcript no longer clamps into it: the query box's own draft rows take
// a composer selection (see startComposerSelection), and the chrome around them
// - the menu, the preview, the working line, the borders - still takes nothing.
//
// The count is App.transcriptRows rather than the transcript's own height,
// which a card or a completion menu shortens without resizing.
func (a App) startSelection(id string, col, top, height, x, y int, r Regions, refocused bool) App {
	rows := a.transcriptRows(id, r.Cols[col], height)
	if y >= top+rows {
		// Below the transcript is chrome and the query box. The box's own draft
		// rows are text you typed and select like the transcript; the menu, the
		// preview, the working line and the borders around them are not.
		return a.startComposerSelection(id, col, top, height, x, y, r)
	}
	a.selTop, a.selRows = top, rows
	p := a.pointIn(id, x-a.layout.PaneLeft(r, col), y)
	a.sel, a.selecting = selection{
		pane: id, anchor: p, head: p, refocused: refocused,
		bannerHit: y == top+rows-1 && a.bannerShowing(id, rows),
	}, true
	return a
}

// bannerShowing reports whether a DM's follow banner draws on its own last
// transcript row at rows - the height transcriptRows just measured for this
// press, rather than the DM's own possibly-stale stored one. See
// followbanner.go.
func (a App) bannerShowing(id string, rows int) bool {
	dm, ok := a.dms[id]
	if !ok {
		return false
	}
	tr := dm.tr
	tr.height = rows
	return tr.followLine() >= 0
}

// extendSelection moves the end the pointer is on, scrolling the pane when the
// drag has left it so a selection can reach further than one window.
//
// The edge is selRows, the window the drag was *taken* in - selTop's own rule -
// rather than a fresh measurement: a motion message arrives per cell crossed,
// and re-rendering a pane's chrome on each one is the work per mouse pixel this
// file is written to avoid. Chrome that moves mid-drag (a preview growing a
// row) leaves the edge a row or two out for the rest of that drag, which is
// where the stored height it used to read was already.
func (a App) extendSelection(x, y int) App {
	if a.sel.inComposer {
		return a.extendComposerSelection(x, y)
	}
	r := a.regions()
	col := a.columnOf(a.sel.pane)
	if col >= len(r.Cols) || r.Cols[col] <= 0 {
		return a
	}
	switch {
	case y < a.selTop:
		a = a.scrollPane(a.sel.pane, edgeLines)
	case y >= a.selTop+a.selRows:
		a = a.scrollPane(a.sel.pane, -edgeLines)
	}
	a.sel.head = a.pointIn(a.sel.pane, x-a.layout.PaneLeft(r, col), y)
	a.sel = a.sel.clampedTo(r.Cols[col])
	return a
}

// endSelection puts what the drag took on the clipboard and leaves the
// highlight up. A click took nothing, so it copies nothing.
func (a App) endSelection() (App, tea.Cmd) {
	if !a.selecting {
		return a, nil
	}
	a.selecting = false
	if a.sel.inComposer {
		// A drag in the query box copies its own text; a click there took none,
		// and there is no folded tool under a composer to open the way there is
		// under a transcript.
		if a.sel.empty() {
			return a, nil
		}
		return a, copyToClipboard(a.composerSelectedText())
	}
	if a.sel.empty() {
		// A click took no text, and Claude Code spends that same gesture on
		// opening a folded tool result - "click to expand collapsed tool
		// results", in its own words. This is the only place that can tell a
		// click from a drag.
		return a.clickedTool(), nil
	}
	m := a.sel.marked()
	tr := a.transcriptIn(a.sel.pane)
	// Straight off the scrollback rather than out of the drawn window, which is
	// what lets a selection that ran past the pane's edge copy the lines it
	// scrolled to reach.
	lines, first := tr.selectionLines(m)
	return a, copyToClipboard(selectedText(lines, first, m))
}

// clickedTool opens or folds what a click landed on: in a conversation, a
// folded run's rollup line or one tool result; in the room, a response the room
// collapsed into a pointer. It leaves everything alone when the line held none
// of those.
//
// A run is tried first because its rollup and a result are never on the same
// line - a folded run draws no results, an expanded one draws no rollup - so the
// order only decides which lookup answers, never which of two things a line is.
func (a App) clickedTool() App {
	// A click that moved the keys to this pane only focuses it: opening the run
	// or result under the pointer is a second click, once the pane already holds
	// the keys. Otherwise looking at a conversation - clicking it to read it -
	// expanded whatever run sat under the cursor, and that opened state persists,
	// so the pane came back expanded every time. See selection.refocused.
	if a.sel.refocused {
		return a
	}
	// The room folds responses rather than tool blocks, so its click resolves a
	// line to a collapsed reply and toggles that one - the per-line half of the
	// ask, where ⌃E is the expand-all half.
	if a.sel.pane == "" {
		if next, hit := a.room.toggleLine(a.sel.anchor.line); hit {
			return a.withRoom(next)
		}
		return a
	}
	dm, ok := a.dms[a.sel.pane]
	if !ok {
		return a
	}
	// Tried first: the banner replaces whatever the transcript actually drew on
	// that line, so a run or a result registered against it is not what the
	// reader sees or clicked. Decided at press time (selection.bannerHit) and
	// not re-derived here, which would need the DM's own stored transcript -
	// stale whenever chrome has moved since the last resize. See
	// followbanner.go.
	if a.sel.bannerHit {
		return a.withDM(a.sel.pane, dm.JumpToLatest())
	}
	if next, hit := dm.openRun(a.sel.anchor.line); hit {
		return a.withDM(a.sel.pane, next)
	}
	if next, hit := dm.openTool(a.sel.anchor.line); hit {
		return a.withDM(a.sel.pane, next)
	}
	return a
}

// pointIn is where a screen row and a pane-local column land in a
// conversation's scrollback.
func (a App) pointIn(id string, col, y int) point {
	tr := a.transcriptIn(id)
	first := min(max(tr.scroll, tr.first()), tr.bottom())
	return point{
		line: clamp(first+y-a.selTop, tr.first(), max(tr.lines.len()-1, tr.first())),
		col:  max(col, 0),
	}
}
