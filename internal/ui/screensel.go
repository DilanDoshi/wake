package ui

// Selecting text anywhere on the frame.
//
// The transcript and the query box each carry their own selection, anchored to
// what they draw (transcript.lines indices, draft-row indices) so a highlight
// follows its text as the pane scrolls and events arrive. Everything else Wake
// draws - the roster, the sidebars, the status bar, the awareness strip, a
// card, a menu, a preview, the composer's borders - is chrome that neither
// scrolls nor renumbers, so it needs none of that: a drag over it anchors to an
// absolute (row, column) on screen, and the highlight is drawn once over the
// whole assembled frame rather than by any one pane.
//
// It reuses selection/marked/covers/selectedText unchanged; only the anchor's
// meaning differs, which is what onScreen records. A press routes here when it
// is neither a transcript row nor a query-box draft row nor a divider - the
// three surfaces with a gesture of their own - so this is the fallback that
// makes "everything is selectable" true without a geometry mapper per surface.
//
// It reads the frame live rather than snapshotting it: chrome is redrawn on
// every fleet report, so like a terminal's own selection it follows the cells
// and copies whatever stands under them at release. The one thing resolved at
// *press* is a roster click's target (rosterHit) - the roster reorders by
// attention, so a click must open the row that was pressed, not whoever slid
// onto it before the button came up.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// startScreenSelection takes the anchor a frame-wide drag runs from. Nothing
// follows it, so there is no pane, no scroll and no draft row to record - just
// the cell the press landed on.
func (a App) startScreenSelection(x, y int) App {
	p := a.screenPoint(x, y)
	a.sel, a.selecting = selection{onScreen: true, anchor: p, head: p}, true
	return a
}

// extendScreenSelection moves the end the pointer is on, clamped to the frame.
// Unlike the transcript it does not scroll a pane off its edge - the frame is
// the whole screen, there is nowhere further to reach.
func (a App) extendScreenSelection(x, y int) App {
	a.sel.head = a.screenPoint(x, y)
	return a
}

// screenPoint maps a terminal cell to a place in the frame: an absolute row and
// column, clamped to what is drawn.
func (a App) screenPoint(x, y int) point {
	return point{
		line: clamp(y, 0, max(a.layout.Height-1, 0)),
		col:  clamp(x, 0, max(a.layout.Width-1, 0)),
	}
}

// screenSelectedText is what the current frame-wide drag covers, off the frame
// as it is drawn - styling stripped and the pad trimmed, selectedText's own
// rule. Read live at release rather than snapshotted: chrome does not change
// under a drag the way a transcript appends, but reading it off the frame is
// what keeps this in step with what the highlight drew.
func (a App) screenSelectedText() string {
	return selectedText(strings.Split(a.assembleFrame(), "\n"), 0, a.sel.marked())
}

// rosterHit is the roster row a press landed on, resolved at press time (see
// mouse.go's press) and opened by screenClick on an empty release. Resolved at
// press because the roster reorders by attention on every fleet report, so
// resolving on release would open whatever agent slid onto that row in the
// window between the two.
type rosterHit struct {
	id, name, dispatch string
	ok                 bool
}

// screenClick does what a click - a press and release on one cell - means for
// the chrome it landed on. Only the roster has one: it opens the row's
// conversation, the same as a click there has always done. Every other surface
// has no click action, so a click on it does nothing and leaves no highlight,
// exactly as a click in the transcript that hits no folded tool does.
func (a App) screenClick() App {
	h := a.rosterHit
	if !h.ok {
		return a
	}
	// It may have ended between press and release; opening a name the fleet no
	// longer has would focus nothing.
	if _, ok := a.fleet.Agent(h.id); !ok {
		return a
	}
	a = a.openDMWith(h.id, h.name)
	// Both fields together, always - the pair openDMWith's own comment argues:
	// a click that resolved to no dispatch must not leave the previous agent's
	// attached to this one.
	a.roster.Selected, a.roster.SelectedTask = h.id, h.dispatch
	return a.viewingPicked(h.id)
}

// overlayScreenSelection draws the highlight of a frame-wide selection over the
// assembled frame. A no-op for every other kind, and for none at all, so it
// costs nothing on a frame that is not being screen-selected - the one place a
// price would multiply by thirty is per token, not per selection.
func (a App) overlayScreenSelection(frame string) string {
	if !a.sel.onScreen || a.sel.empty() {
		return frame
	}
	m := a.sel.marked()
	lines := strings.Split(frame, "\n")
	for i := range lines {
		c0, c1, ok := m.covers(i)
		if !ok {
			continue
		}
		if c1 == lineEnd {
			c1 = ansi.StringWidth(lines[i])
		}
		lines[i] = highlighted(lines[i], c0, c1)
	}
	return strings.Join(lines, "\n")
}
