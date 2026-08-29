package ui

// A selection: where a drag started, where the pointer is, and the text
// between them.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// point is a place in a transcript: an absolute index into transcript.lines,
// and a column local to the pane it was taken in.
//
// Absolute rather than a screen row because chunked never renumbers, so a
// selection needs no maintenance as events arrive - which is what lets a
// highlight ride up the screen while the pane keeps following its agent.
type point struct{ line, col int }

// selection is a live drag, or the highlight one left behind. The zero value
// selects nothing; pane is a DM id, and "" is the room.
type selection struct {
	pane         string
	anchor, head point

	// refocused marks a selection whose opening press moved the keys to this
	// pane. A click that changed focus only focuses; opening the run or result
	// under the pointer is a second click, once the pane already holds the keys.
	// Without it, clicking a pane to read it expanded whatever run sat under the
	// cursor - see clickedTool.
	refocused bool

	// inComposer marks a selection taken inside a pane's query box rather than
	// its transcript. Its point.line is a draft-row index and point.col is a
	// column into the typed text (0 is the first character after the "> "
	// prompt), so the same marked/covers/selectedText machinery reads it. See
	// composersel.go.
	inComposer bool
}

// marked is a selection with its ends ordered, half-open in both axes.
//
// Not "span": roster.go has one of those already, and it is the slice of
// agents a column has room for rather than a range of text.
type marked struct{ from, to point }

// lineEnd in a marked column means "to the end of this line", which is what
// every line but the last one takes. Only a caller knows how wide its line is.
const lineEnd = -1

// empty reports a press and release on one cell: a click, which focuses a pane
// and selects nothing.
func (s selection) empty() bool { return s.anchor == s.head }

// marked orders the two ends and makes the range half-open.
//
// The ends are stored as the cells they are, so a continued drag still knows
// which one the pointer is on. Only this knows the convention: the end column
// gains one, because the cell under the pointer is part of what an operator
// dragging across four characters expects to get.
func (s selection) marked() marked {
	from, to := s.anchor, s.head
	if to.line < from.line || (to.line == from.line && to.col < from.col) {
		from, to = to, from
	}
	to.col++
	return marked{from: from, to: to}
}

// covers is the columns of one line inside the selection, and whether it is in
// it at all. A c1 of lineEnd means the rest of the line.
func (m marked) covers(line int) (c0, c1 int, ok bool) {
	if line < m.from.line || line > m.to.line {
		return 0, 0, false
	}
	c0, c1 = 0, lineEnd
	if line == m.from.line {
		c0 = m.from.col
	}
	if line == m.to.line {
		c1 = m.to.col
	}
	return c0, c1, true
}

// selectedText is what a selection covers, ready for the clipboard: styling
// stripped, and the pad lipgloss adds to every line trimmed. lines are rendered
// transcript lines and first is the absolute index of lines[0].
//
// The trim is not cosmetic. Every line in a pane is rendered to the pane's
// width, so without it a two-word copy arrives as two words and forty spaces.
func selectedText(lines []string, first int, m marked) string {
	var out []string
	for i, l := range lines {
		c0, c1, ok := m.covers(first + i)
		if !ok {
			continue
		}
		if c1 == lineEnd {
			c1 = ansi.StringWidth(l)
		}
		out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(l, c0, c1)), " "))
	}
	return strings.Join(out, "\n")
}

// clampedTo holds both ends inside a pane. A drag into the next column
// continues at this pane's edge rather than being abandoned, which is also the
// only way to select a full-width line without landing on its last cell.
func (s selection) clampedTo(width int) selection {
	s.anchor.col = min(max(s.anchor.col, 0), width-1)
	s.head.col = min(max(s.head.col, 0), width-1)
	return s
}
