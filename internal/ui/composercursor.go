package ui

// Placing the query-box caret from a click.
//
// A left click on a typed character moves the draft cursor onto it - the one
// thing the box could not do without the arrow keys. The caret is the text
// area's own, so it renders exactly as ←→ leave it: the same block on the same
// character, blink and all.
//
// It reuses composerdelete.go's display<->rune mapping - composerRowStarts,
// rawOffset and runesInto - and its placeCursor, so the box's wrap geometry has
// one copy, not two. Like the deletion it declines a scrolled draft: a visible
// row is then a window into the value rather than an absolute display row, and
// there is no public way to read the scroll offset, so the arrow keys stay the
// way to position there rather than the caret landing on the wrong line.

import "strings"

// caretAtPoint moves the caret onto the clicked draft position - the display row
// and column captured when the click landed - and reports whether it could. rows
// are the box's rendered draft rows and boxWidth their width, both captured at
// the press the way a drag captures them. It declines an empty draft (nothing to
// place) and a scrolled one (composerRowStarts' own bound), leaving the caret
// where it was.
func (c Composer) caretAtPoint(p point, rows []string, boxWidth int) (Composer, bool) {
	value := c.ta.Value()
	if value == "" || len(rows) == 0 {
		return c, false
	}
	starts, ok := composerRowStarts(c.ta.Prompt, c.taWidth, len(rows), value)
	if !ok {
		return c, false
	}
	lines := strings.Split(value, "\n")
	off := rawOffset(starts, lines, p.line, runesInto(rows, p.line, p.col, boxWidth))
	return c.placeCursor(value, off).reposition(), true
}
