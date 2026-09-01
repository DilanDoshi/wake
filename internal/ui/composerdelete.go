package ui

// Deleting the text a query-box selection covers.
//
// The selection is in display coordinates - wrapped rows and cells - while the
// draft is stored as raw runes, so the two ends are mapped back through the text
// area's own wrap (LineInfo), never a second copy of it. ⌫ and the delete key
// reach this before App.cleared drops the selection - see App.deleteSelectedDraft.
//
// It is scoped to a draft that fits the box (no scroll): a scrolled draft's
// visible rows are a window into the value, not its first rows, so a
// visible-relative row index no longer maps to an absolute display row. That is
// rare for a chat composer, and the fall-through is safe - the key does its
// ordinary job rather than deleting the wrong run.

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// deleteSelectedDraft turns ⌫ or delete into a deletion of the highlighted query
// text when a live query-box selection is up, reporting whether it took the key.
// A transcript selection, an empty selection, a scrolled draft or any other key
// is left to the ordinary path - App.cleared then the composer.
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
	next, deleted := c.deleteSelected(a.sel.marked(), a.cdrag.rows, a.cdrag.boxWidth)
	if !deleted {
		// The selection could not be mapped (a scrolled draft): clear the
		// highlight and take the key rather than let it fall through to a normal
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

// deleteSelected removes the runes a query-box selection covers and leaves the
// cursor where they were, or returns the composer untouched (ok=false) when the
// draft is scrolled and the mapping cannot be trusted. rows are the draft's
// rendered display rows captured when the drag began; boxWidth is their width.
func (c Composer) deleteSelected(m marked, rows []string, boxWidth int) (Composer, bool) {
	value := c.ta.Value()
	if value == "" || len(rows) == 0 {
		return c, false
	}
	// A draft taller than the visible rows is scrolled: a visible-relative row is
	// no longer an absolute display row, so the mapping would be wrong. The walk
	// declines it rather than paying to enumerate a large paste's every row.
	starts, ok := composerRowStarts(c.ta.Prompt, c.taWidth, len(rows), value)
	if !ok {
		return c, false
	}
	lines := strings.Split(value, "\n")

	start := rawOffset(starts, lines, m.from.line, runesInto(rows, m.from.line, m.from.col, boxWidth))
	end := rawOffset(starts, lines, m.to.line, runesInto(rows, m.to.line, m.to.col, boxWidth))

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

// composerRowStarts walks the text area's own wrap - through a throwaway copy and
// its LineInfo - and reports, for each display row in order, the logical line it
// belongs to and the rune index within that line where it begins. Using the text
// area itself is what keeps a second copy of bubbles' wrap rules out of this file.
//
// ok is false when the draft is taller than maxRows (scrolled): the walk stops
// there rather than enumerating every row of a large paste. **Both walks are
// bounded to maxRows steps, not to the draft length** - bubbles rehashes the
// whole logical line on every LineInfo/CursorUp call, so an unbounded walk is
// quadratic in a big single-line paste (measured seconds at ~20k runes). maxRows
// is the visible row count, so a fitting draft is fully enumerated and a scrolled
// one is declined after a bounded probe, the way draftRows stays bounded too.
func composerRowStarts(prompt string, taWidth, maxRows int, value string) ([]rowStart, bool) {
	probe := textarea.New()
	probe.Prompt = prompt
	probe.ShowLineNumbers = false
	probe.SetWidth(taWidth)
	probe.SetHeight(maxComposerRows)
	probe.SetValue(value)

	// SetValue leaves the cursor on the last display row; climb to the first.
	// Past maxRows rows above it, the draft is scrolled and we decline.
	for up := 0; up <= maxRows; up++ {
		if probe.Line() == 0 && probe.LineInfo().RowOffset == 0 {
			break
		}
		probe.CursorUp()
	}
	if probe.Line() != 0 || probe.LineInfo().RowOffset != 0 {
		return nil, false
	}
	probe.CursorStart()

	var out []rowStart
	for {
		li := probe.LineInfo()
		out = append(out, rowStart{line: probe.Line(), col: li.StartColumn})
		if len(out) > maxRows {
			return nil, false
		}
		beforeLine, beforeRow := probe.Line(), li.RowOffset
		probe.CursorDown()
		if probe.Line() == beforeLine && probe.LineInfo().RowOffset == beforeRow {
			break
		}
	}
	return out, true
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
// leaves the cursor where the removed run began rather than at the end.
func (c Composer) placeCursor(value string, offset int) Composer {
	r := []rune(value)
	offset = min(max(offset, 0), len(r))
	before := string(r[:offset])
	targetLine := strings.Count(before, "\n")
	col := len([]rune(before))
	if nl := strings.LastIndex(before, "\n"); nl >= 0 {
		col = len([]rune(before[nl+1:]))
	}
	// Climb to the first display row, then descend to the target logical line.
	// Both are bounded to the box's rows: this runs only after a successful
	// (non-scrolled) deletion, so the draft fits, and LineInfo is O(line) per
	// call - an unbounded climb would be quadratic on a big draft.
	for up := 0; up <= maxComposerRows; up++ {
		if c.ta.Line() == 0 && c.ta.LineInfo().RowOffset == 0 {
			break
		}
		c.ta.CursorUp()
	}
	c.ta.CursorStart()
	for guard := 0; c.ta.Line() < targetLine && guard <= maxComposerRows; guard++ {
		c.ta.CursorDown()
	}
	c.ta.SetCursor(col)
	return c
}
