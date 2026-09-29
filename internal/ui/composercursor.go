package ui

// Placing the query-box caret from a click.
//
// A left click on a typed character moves the draft cursor onto it - the one
// thing the box could not do without the arrow keys. The caret is the text
// area's own, so it renders exactly as ←→ leave it: the same block on the same
// character, blink and all.
//
// It reuses composerdelete.go's display<->rune mapping - drawnRowStarts, rawOffset
// and runesInto - and its placeCursor, so the box's wrap geometry has one copy,
// not two. In a draft taller than the box, reposition then scrolls the clicked
// row to the box's bottom, the way ↑↓ scroll it.

import "strings"

// caretAtPoint moves the caret onto the clicked draft position - the drawn row
// and column captured when the click landed, read against the rows and their
// starts captured at the press, the way a drag captures them.
func (c Composer) caretAtPoint(p point, d drawnDraft, boxWidth int) Composer {
	value := c.ta.Value()
	lines := strings.Split(value, "\n")
	off := rawOffset(d.starts, lines, p.line, runesInto(d.rows, p.line, p.col, boxWidth))
	return c.placeCursor(value, off).reposition()
}
