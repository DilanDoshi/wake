package ui

// Deleting the text a query-box selection covers.
//
// The selection is in display coordinates - wrapped rows and cells - while the
// draft is stored as raw runes, so the two ends are mapped back through the text
// area's own wrap (LineInfo), never a second copy of it. ⌫ and the delete key
// reach this before App.cleared drops the selection - see App.deleteSelectedDraft.
//
// A draft taller than the box scrolls inside it, so the drawn rows are a window
// into the value rather than its first rows. bubbles keeps the window's offset to
// itself; drawnRowStarts finds the window from the caret instead, or from where
// the wheel left it.

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// deleteSelectedDraft turns ⌫ or delete into a deletion of the highlighted query
// text when a live query-box selection is up, reporting whether it took the key.
// A transcript selection, an empty selection or any other key is left to the
// ordinary path - App.cleared then the composer.
func (a App) deleteSelectedDraft(m tea.KeyMsg) (App, tea.Cmd, bool) {
	if m.Type != tea.KeyBackspace && m.Type != tea.KeyDelete {
		return a, nil, false
	}
	if a.sel.empty() || !a.sel.inComposer {
		return a, nil, false
	}
	c, ok := a.composerFor(a.sel.pane)
	if !ok {
		return a, nil, false
	}
	next, deleted := c.deleteSelected(a.sel.marked(), a.cdrag.drawnDraft, a.cdrag.boxWidth)
	if !deleted {
		// No typed rune lies between the highlight's ends: clear the highlight
		// and take the key rather than let it fall through to a normal
		// backspace, which would delete an unrelated character at the cursor while
		// the highlight vanished. The draft is untouched; a second press deletes
		// normally.
		return a.cleared(), nil, true
	}
	a = a.withComposerFor(a.sel.pane, next).cleared()
	moved, scan := a.retarget().recompleted().scanning()
	return moved, scan, true
}

// rowStart is where one display row of the draft begins: the logical line it is
// part of, and the rune index within that line where the row's first character
// sits.
type rowStart struct{ line, col int }

// drawnDraft is the box's draft rows as a press captured them, and where each
// one begins in the draft. Both come off the composer as drawn: a menu or card
// can draw the box at a different height than the pane stores.
type drawnDraft struct {
	rows   []string
	starts []rowStart
}

// deleteSelected removes the runes a query-box selection covers and leaves the
// cursor where they were, or returns the composer untouched (ok=false) when no
// typed rune lies between the selection's ends.
func (c Composer) deleteSelected(m marked, d drawnDraft, boxWidth int) (Composer, bool) {
	value := c.ta.Value()
	lines := strings.Split(value, "\n")

	start := rawOffset(d.starts, lines, m.from.line, runesInto(d.rows, m.from.line, m.from.col, boxWidth))
	end := rawOffset(d.starts, lines, m.to.line, runesInto(d.rows, m.to.line, m.to.col, boxWidth))

	r := []rune(value)
	start = min(max(start, 0), len(r))
	end = min(max(end, start), len(r))
	if start == end {
		return c, false
	}
	newVal := string(r[:start]) + string(r[end:])
	c.ta.SetValue(newVal)
	c = c.placeCursor(newVal, start)
	return c.fit().reposition(), true
}

// drawnRowStarts is where each of the n draft rows the box draws begins, walked
// through the text area's own wrap (LineInfo) on a copy, so bubbles' wrap rules
// have no second copy here.
func (c Composer) drawnRowStarts(n int) []rowStart {
	return c.rowsFrom(c.firstDrawn(), n)
}

// followedTop is the first row reposition draws: it leaves the caret on the
// bottom row of a draft taller than the box and the view at the top of one that
// fits, so it is the caret's row less up to bound-1 rows. **Every walk here is
// bounded by the box, not the draft** - LineInfo rehashes the whole logical line,
// so a walk over every row of a big single-line paste would be quadratic in it.
func (c Composer) followedTop() rowStart {
	probe := c.ta
	for range c.bound() - 1 {
		if !rowUp(&probe) {
			break
		}
	}
	return startOf(probe)
}

// rowsFrom is where each of up to n draft rows begins, from the row at top down.
func (c Composer) rowsFrom(top rowStart, n int) []rowStart {
	probe := c.ta
	caretTo(&probe, top)
	out := []rowStart{startOf(probe)}
	for len(out) < n && rowDown(&probe) {
		out = append(out, startOf(probe))
	}
	return out
}

// startOf is the row the text area's caret is on.
func startOf(ta textarea.Model) rowStart {
	return rowStart{line: ta.Line(), col: ta.LineInfo().StartColumn}
}

// rowUp moves the caret onto the display row above and reports whether there
// was one. Within a logical line it steps by column, mirroring rowDown; across
// lines CursorUp lands on the last row of the line above.
func rowUp(ta *textarea.Model) bool {
	li := ta.LineInfo()
	switch {
	case li.RowOffset > 0:
		ta.SetCursor(li.StartColumn - 1)
	case ta.Line() > 0:
		ta.CursorUp()
	default:
		return false
	}
	return true
}

// rowDown moves the caret onto the display row below and reports whether there
// was one. Within a logical line it steps to the next row's first rune rather
// than calling CursorDown, which clamps short of the blank row bubbles wraps after
// a line that exactly fills the width - a row the box draws. CursorStart after
// crossing a line keeps a wide-rune column from carrying onto its second row.
func rowDown(ta *textarea.Model) bool {
	li := ta.LineInfo()
	switch {
	case li.RowOffset < li.Height-1:
		ta.SetCursor(li.StartColumn + li.Width)
	case ta.Line() < ta.LineCount()-1:
		ta.CursorDown()
		ta.CursorStart()
	default:
		return false
	}
	return true
}

// rawOffset is the rune offset into the value of a point runeCol runes into
// display row dispRow, counting one newline per logical line before it.
func rawOffset(starts []rowStart, lines []string, dispRow, runeCol int) int {
	if dispRow < 0 || len(starts) == 0 {
		return 0
	}
	if dispRow >= len(starts) {
		return valueRuneLen(lines)
	}
	rs := starts[dispRow]
	off := 0
	for k := 0; k < rs.line && k < len(lines); k++ {
		off += len([]rune(lines[k])) + 1 // + the newline that ended line k
	}
	return off + rs.col + runeCol
}

// runesInto is how many runes of a display row's typed text sit left of display
// cell col, clamped to the row's real text so a drag past the end stops at it.
func runesInto(rows []string, dispRow, col, boxWidth int) int {
	if dispRow < 0 || dispRow >= len(rows) {
		return 0
	}
	seg := ansi.Strip(ansi.Cut(rows[dispRow], composerTextLeft, composerTextLeft+col))
	return min(len([]rune(seg)), rowRuneLen(rows, dispRow, boxWidth))
}

// rowRuneLen is the number of typed runes on a display row - its text with the
// prompt, border and trailing pad stripped.
func rowRuneLen(rows []string, dispRow, boxWidth int) int {
	if dispRow < 0 || dispRow >= len(rows) {
		return 0
	}
	seg := ansi.Cut(rows[dispRow], composerTextLeft, boxWidth-composerRightInset)
	return len([]rune(strings.TrimRight(ansi.Strip(seg), " ")))
}

// valueRuneLen is the total rune length of the value the lines came from,
// newlines included.
func valueRuneLen(lines []string) int {
	n := 0
	for i, l := range lines {
		if i > 0 {
			n++ // the newline before this line
		}
		n += len([]rune(l))
	}
	return n
}

// placeCursor moves the draft cursor to a rune offset in value, so a deletion
// leaves the cursor where the removed run began and a click where it landed.
func (c Composer) placeCursor(value string, offset int) Composer {
	r := []rune(value)
	offset = min(max(offset, 0), len(r))
	before := string(r[:offset])
	targetLine := strings.Count(before, "\n")
	col := len([]rune(before))
	if nl := strings.LastIndex(before, "\n"); nl >= 0 {
		col = len([]rune(before[nl+1:]))
	}
	caretTo(&c.ta, rowStart{line: targetLine, col: col})
	return c
}

// caretTo moves ta's caret to rune col of logical line line. It hops whole
// logical lines, never wrapped rows: from a line's start CursorUp reaches the
// line above and from its end CursorDown the line below, so a long wrapped line
// costs one LineInfo, not one per row.
func caretTo(ta *textarea.Model, at rowStart) {
	for range ta.Line() - at.line {
		ta.CursorStart()
		ta.CursorUp()
	}
	for range at.line - ta.Line() {
		ta.CursorEnd()
		ta.CursorDown()
	}
	ta.SetCursor(at.col)
}
