package ui

// The query-box caret: placing it from a click, and whether ↑↓ would move it.
//
// A left click on a typed character moves the draft cursor onto it - the one
// thing the box could not do without the arrow keys. The caret is the text
// area's own, so it renders exactly as ←→ leave it: the same block on the same
// character, blink and all.
//
// It reuses composerdelete.go's display<->rune mapping - drawnRowStarts, rawOffset
// and runesInto - and its placeCursor, so the box's wrap geometry has one copy,
// not two. In a draft taller than the box, reposition then scrolls the clicked
// row to the box's bottom, the way ↑↓ scroll it - unless the wheel scrolled the
// view, which a click leaves where it is (composerscroll.go).

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
)

// caretAtPoint moves the caret onto the clicked draft position - the drawn row
// and column captured when the click landed, read against the rows and their
// starts captured at the press, the way a drag captures them.
func (c Composer) caretAtPoint(p point, d drawnDraft, boxWidth int) Composer {
	value := c.ta.Value()
	lines := strings.Split(value, "\n")
	off := rawOffset(d.starts, lines, p.line, runesInto(d.rows, p.line, p.col, boxWidth))
	return c.placeCursor(value, off).reposition()
}

// CanCursorUp reports whether ↑ would actually move the text cursor within the
// draft, which is when App.key gives the arrow to the composer rather than the
// roster. False for an empty or single-line draft, and false at the top row of
// a multi-line or soft-wrapped one.
//
// It runs the move on a copy and compares the position rather than trusting
// LineInfo's row count: bubbles' wrap adds a synthetic trailing row when a
// line's width is exactly the wrap width (its `>=`), which Height counts but the
// cursor cannot land on - so a count alone reports a move CursorUp does not make,
// which swallows the arrow (moving neither the cursor nor the roster). Because
// this is exactly the move App.key delegates to, the two can never disagree.
func (c Composer) CanCursorUp() bool {
	return c.cursorMoves((*textarea.Model).CursorUp)
}

// CanCursorDown is CanCursorUp's mirror for ↓.
func (c Composer) CanCursorDown() bool {
	return c.cursorMoves((*textarea.Model).CursorDown)
}

// cursorMoves applies a vertical move to a copy of the text area and reports
// whether the cursor changed rows. The copy shares the read-only draft buffer
// and the idempotent wrap cache by pointer but owns its own row and column, so
// the probe never disturbs the live draft.
func (c Composer) cursorMoves(move func(*textarea.Model)) bool {
	probe := c.ta
	before := cursorRow(probe)
	move(&probe)
	return cursorRow(probe) != before
}

// cursorRow is the cursor's vertical position: its logical line and the wrapped
// row within that line. Deliberately not the column - at an edge bubbles'
// CursorUp/CursorDown shuffles the column *within* a multi-rune grapheme (a
// combining accent, a wide ZWJ sequence) without changing the row, and reading
// that as a move would nudge the cursor into the middle of the glyph rather than
// moving the roster. Vertical movement is exactly a change of row.
func cursorRow(ta textarea.Model) [2]int {
	info := ta.LineInfo()
	return [2]int{ta.Line(), info.RowOffset}
}
