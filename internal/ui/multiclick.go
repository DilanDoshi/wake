package ui

// Double- and triple-click: a second click on the same cell selects the word
// under it, a third the whole row, and any more leave that row highlighted.
//
// A run is counted at press, on one cell and one surface - the same pane, the
// same query box, the same chrome - within multiClickWindow of the press before
// it, and any drag ends it. It is the only timer in selection, and it counts clicks rather
// than telling a click from a drag, which head != anchor still does alone. The
// first click of a run does whatever a click does where it lands - opens a
// fold, places the caret, opens a roster row - and only the presses after it
// select. A run's selection is a span, so a one-character word is a selection
// rather than the click its equal ends would read as, and its release copies it.

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// multiClickWindow is how soon a press must follow the one before for the two
// to be one run - about the double-click interval a desktop defaults to.
const multiClickWindow = 500 * time.Millisecond

// clickRun is the presses so far that landed on one cell in quick succession,
// and the surface the last of them selected on.
type clickRun struct {
	x, y, n int
	at      time.Time
	on      surface
}

// surface is what a press selected on: a pane's transcript or query box, or the
// frame's chrome. A cell that belongs to another surface by the next press - a
// narrow grid slides to show the pane a click focused - starts a new run.
type surface struct {
	pane               string
	inComposer, screen bool
}

// surfaceOf is the surface the selection a press just took lies on.
func (s selection) surfaceOf() surface {
	return surface{pane: s.pane, inComposer: s.inComposer, screen: s.onScreen}
}

// next folds a press into the run: the same cell within the window continues
// it, anything else starts a new one.
func (c clickRun) next(x, y int, now time.Time) clickRun {
	if c.n > 0 && c.x == x && c.y == y && now.Sub(c.at) <= multiClickWindow {
		return clickRun{x: x, y: y, n: c.n + 1, at: now, on: c.on}
	}
	return clickRun{x: x, y: y, n: 1, at: now}
}

// pressed is a left press, counted into the run before it is routed: a fourth
// click or later does nothing, and a second or third on the surface the run
// began on widens the anchor the press took into the word or the row under it.
func (a App) pressed(x, y int) App {
	run := a.clicks.next(x, y, clock())
	if run.n > 3 {
		a.clicks = run
		return a
	}
	a = a.press(x, y)
	if run.n > 1 && a.sel.surfaceOf() != run.on {
		run.n = 1
	}
	run.on = a.sel.surfaceOf()
	a.clicks = run
	if run.n > 1 && a.selecting {
		a = a.widen(run.n == 3)
	}
	return a
}

// widen turns the anchor a run's press just took into the word under it, or
// the row; a press with no text under it takes nothing.
func (a App) widen(row bool) App {
	text, lo, hi := a.selectedRow()
	c0, c1, ok := wordAt(text, a.sel.anchor.col, lo, hi)
	if row {
		c0, c1, ok = rowSpan(text, lo, hi)
	}
	if !ok {
		return a.cleared()
	}
	a.sel.anchor.col, a.sel.head.col, a.sel.head.line = c0, c1-1, a.sel.anchor.line
	a.sel.span = true
	return a
}

// selectedRow is the text of the row the selection is anchored on, styling
// stripped, in the columns its points use, and the columns of it the click's
// surface owns: a pane's line, the typed part of a draft row, or the region of
// the frame the press landed in - so a word never runs across a divider.
func (a App) selectedRow() (text string, lo, hi int) {
	switch {
	case a.sel.onScreen:
		lo, hi = a.screenBounds(a.sel.anchor.col, a.sel.anchor.line)
		rows := strings.Split(a.assembleFrame(), "\n")
		return ansi.Strip(rows[min(a.sel.anchor.line, len(rows)-1)]), lo, hi
	case a.sel.inComposer:
		row := a.cdrag.rows[a.sel.anchor.line]
		text = ansi.Strip(ansi.Cut(row, composerTextLeft, a.cdrag.boxWidth-composerRightInset))
		return text, 0, ansi.StringWidth(text)
	}
	at := a.sel.anchor
	lines, _ := a.transcriptIn(a.sel.pane).selectionLines(marked{from: at, to: at})
	if len(lines) == 0 {
		return "", 0, 0
	}
	text = ansi.Strip(lines[0])
	return text, 0, ansi.StringWidth(text)
}

// screenBounds is the columns of the frame row y the surface at column x owns:
// its pane, the roster or the workspaces sidebar, or the whole width below the
// panes, where the awareness strip and the notice row run edge to edge.
func (a App) screenBounds(x, y int) (lo, hi int) {
	if y >= a.paneHeight() {
		return 0, a.layout.Width
	}
	r := a.regions()
	switch region, at := a.layout.Hit(r, x); region {
	case RegionRoster:
		return a.layout.Width - r.Roster, a.layout.Width
	case RegionGroups:
		return 0, r.Groups
	case RegionPane:
		left := a.layout.PaneLeft(r, at)
		return left, left + r.Cols[at]
	}
	return 0, a.layout.Width
}

// cell is one character of a row: the columns it covers, and whether it
// separates words.
type cell struct {
	c0, c1 int
	gap    bool
}

// cellsOf lays a stripped row out in columns, one cell per grapheme cluster at
// the width ansi draws it - the unit highlighted cuts by, so an emoji sequence
// is one character. A space, or a box-drawing rune - a border or a divider,
// which is chrome and never part of what is written - is a gap.
func cellsOf(text string) []cell {
	var cells []cell
	for col := 0; text != ""; {
		g, w := ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
		if g == "" {
			break
		}
		text = text[len(g):]
		if w == 0 {
			continue
		}
		r, _ := utf8.DecodeRuneInString(g)
		cells = append(cells, cell{c0: col, c1: col + w, gap: unicode.IsSpace(r) || (r >= '─' && r <= '╿')})
		col += w
	}
	return cells
}

// wordAt is the columns [c0, c1) of the run of characters that are not gaps
// covering column col of text, kept inside [lo, hi). ok is false on a gap.
func wordAt(text string, col, lo, hi int) (c0, c1 int, ok bool) {
	cells := cellsOf(text)
	at := -1
	for i, c := range cells {
		if col >= c.c0 && col < c.c1 && c.c0 >= lo && c.c1 <= hi {
			at = i
		}
	}
	if at < 0 || cells[at].gap {
		return 0, 0, false
	}
	first, last := at, at
	for first > 0 && !cells[first-1].gap && cells[first-1].c0 >= lo {
		first--
	}
	for last < len(cells)-1 && !cells[last+1].gap && cells[last+1].c1 <= hi {
		last++
	}
	return cells[first].c0, cells[last].c1, true
}

// rowSpan is the columns [c0, c1) of text inside [lo, hi) from its first
// character that is not a gap to its last. ok is false on a row with none.
func rowSpan(text string, lo, hi int) (c0, c1 int, ok bool) {
	for _, c := range cellsOf(text) {
		if c.gap || c.c0 < lo || c.c1 > hi {
			continue
		}
		if !ok {
			c0, ok = c.c0, true
		}
		c1 = c.c1
	}
	return c0, c1, ok
}
