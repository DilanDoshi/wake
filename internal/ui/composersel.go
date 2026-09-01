package ui

// Selecting text inside the query box.
//
// The transcript has had drag-highlight-copy since the select-copy design; the
// composer had none, because startSelection treats every row under the
// transcript as chrome and takes no anchor there. That is right for the menu, the
// preview and the working line - none of them are text you typed - but wrong for
// the box itself, where the words are yours and there was no way to copy them
// back out. This routes a press that lands on a draft row into a selection over
// the box's own text, drawn by DM/Room.View and copied on release, exactly like
// the transcript.
//
// The geometry is fixed by theme.BoxStyle and NewComposer's prompt rather than
// measured per draft: each draft row is `│ > text   │`, so the text starts
// composerTextLeft columns in and stops composerRightInset short of the far
// edge. Pinned by TestComposerTextRegion.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// composerRegion is where a pane's editable text sits on screen: the row its
// first draft row is on, how many draft rows are drawn, the box width, and the
// rendered draft rows. ok is false when the pane is too short to draw a box
// whose interior is on screen.
//
// Computed from the bottom because that has the fewest moving parts: the
// composer is the last thing a pane draws bar the DM's status bar, so its rows
// are the pane's height less that bar and less the composer's own view height,
// and its interior is the box top border down.
func (a App) composerRegion(id string, width, top, height int) (draftTop, draftRows, boxWidth int, rows []string, ok bool) {
	w := max(width, minComposerWidth)
	c, below, minH := a.drawnComposer(id, w, height)
	// Too short to draw the composer inside its allocation: the pane overflows
	// and App.View clips it from the bottom, so the bottom-up placement below
	// would point at rows the frame never drew. Take no selection there.
	if height < minH {
		return 0, 0, 0, nil, false
	}
	draftRows = c.ta.Height()
	if draftRows <= 0 {
		return 0, 0, 0, nil, false
	}
	block := strings.Split(c.View(w), "\n")
	// The box's draft rows: its top border down, for draftRows of them.
	if len(block) < 1+draftRows {
		return 0, 0, 0, nil, false
	}
	viewHeight := draftRows + c.overhead()
	draftTop = top + height - below - viewHeight + 1 // +1 past the box's top border
	if draftTop < top {
		return 0, 0, 0, nil, false
	}
	return draftTop, draftRows, w, block[1 : 1+draftRows], true
}

// drawnComposer is a pane's composer as it is drawn - sized the same way View
// sizes it, through the same menu block - how many rows the pane draws below it
// (a DM's status bar, and nothing for the room), and the pane's minimum height,
// which is what says whether the composer fits inside its allocation.
func (a App) drawnComposer(id string, width, height int) (c Composer, below, minH int) {
	menu, _ := a.menuBlock(id, width)
	if id == "" {
		room := a.roomFor().WithMenu(menu).SetSize(width, height)
		// The room bar rides inside the composer as a draw-time overlay
		// (Room.View's WithBar), so overhead() cannot see it - count it here the
		// way the DM branch below does, or a room addressing an agent anchors the
		// draft rows one row too high and a drag lands a row above the pointer.
		if room.bar != "" {
			below = 1
		}
		return room.composer, below, room.minHeight()
	}
	d := a.dmFor(id).WithMenu(menu).SetSize(width, height)
	if d.bar != "" {
		below = 1
	}
	return d.composer, below, d.minHeight()
}

// composerSelectionIn is the composer selection resolved for one pane: nothing
// unless a query-box drag was taken in it. Its counterpart selectionIn answers
// the transcript.
func (a App) composerSelectionIn(id string) marked {
	if a.sel.empty() || !a.sel.inComposer || a.sel.pane != id {
		return marked{}
	}
	return a.sel.marked()
}

// startComposerSelection takes the anchor a query-box drag runs from, or clears
// the selection when the press missed the text - the border, the hint, the
// status bar and an empty box are not text to select, and neither is the blank
// space past a short line. The press must land on a row's own characters; a drag
// off their end still extends into the blank and copies only what it trims to,
// the way it does in the transcript.
func (a App) startComposerSelection(id string, col, top, height, x, y int, r Regions) App {
	draftTop, draftRows, boxWidth, rows, ok := a.composerRegion(id, r.Cols[col], top, height)
	miss := func() App { a.sel, a.selecting = selection{}, false; return a }
	if !ok || y < draftTop || y >= draftTop+draftRows {
		return miss()
	}
	paneLeft := a.layout.PaneLeft(r, col)
	// Gate on the raw press column, not the clamped one: composerPoint pulls a
	// press on the border, the "> " prompt or the padding into text column 0, so
	// clamping first would let a drag begun on that chrome copy the row's text.
	// The press must land on the row's own characters, [composerTextLeft, +len).
	rawCol := x - paneLeft
	textLen := composerRowTextLen(rows[min(max(y-draftTop, 0), draftRows-1)], boxWidth)
	if rawCol < composerTextLeft || rawCol >= composerTextLeft+textLen {
		return miss()
	}
	a.cdrag = composerDrag{draftTop, draftRows, paneLeft, boxWidth, rows}
	p := a.composerPoint(x, y)
	// refocused is unset here: it exists only for clickedTool, which the composer
	// branch of endSelection never reaches - a query box has no folded tool.
	a.sel = selection{pane: id, anchor: p, head: p, inComposer: true}
	a.selecting = true
	return a
}

// composerRowTextLen is the width of the typed characters on one draft row, with
// the prompt, the border and lipgloss's trailing pad removed - what decides
// whether a press landed on text or on the blank beyond it.
func composerRowTextLen(row string, boxWidth int) int {
	seg := ansi.Cut(row, composerTextLeft, boxWidth-composerRightInset)
	return ansi.StringWidth(strings.TrimRight(ansi.Strip(seg), " "))
}

// extendComposerSelection moves the end the pointer is on, clamped to the box.
// A query box does not scroll under a drag the way a transcript does - it is a
// handful of rows, all on screen - so a drag off its edge stops at the edge.
func (a App) extendComposerSelection(x, y int) App {
	a.sel.head = a.composerPoint(x, y)
	return a
}

// composerPoint maps a screen cell to a place in the draft: the draft row, and
// the column into the typed text with the prompt and border taken off.
func (a App) composerPoint(x, y int) point {
	line := min(max(y-a.cdrag.draftTop, 0), a.cdrag.draftRows-1)
	width := a.cdrag.boxWidth - composerRightInset - composerTextLeft
	col := min(max(x-a.cdrag.paneLeft-composerTextLeft, 0), max(width-1, 0))
	return point{line: line, col: col}
}

// composerSelectedText is what the current query-box drag covers, off the rows
// captured when it began.
//
// A snapshot rather than a live read - which is where the transcript reads off
// the scrollback at release - because the draft cannot change under a live
// composer drag: the only thing that edits it is a keystroke, and App.cleared
// drops the selection on every KeyMsg before the key does its job. So the rows
// captured at press are the rows on screen at release, and the copy matches the
// highlight. The transcript reads live for the opposite reason: events append to
// it while the button is held.
func (a App) composerSelectedText() string {
	return composerText(a.cdrag.rows, a.sel.marked(), composerTextLeft, a.cdrag.boxWidth-composerRightInset)
}

const (
	// composerTextLeft is the pane-local column the typed text starts at: the
	// left border, one column of BoxStyle padding, and the two-column "> " prompt
	// the text area draws on every visual row.
	composerTextLeft = 4

	// composerRightInset is what the right side spends that the text cannot use:
	// one column of padding and the border.
	composerRightInset = 2
)

// composerDrag is where a query-box drag is running: the screen row its first
// draft row is on, how many draft rows are drawn, the terminal column the pane
// starts at, the box width, and the draft rows themselves - captured once when
// the drag begins so extend and copy do not re-render the box per motion.
type composerDrag struct {
	draftTop, draftRows, paneLeft, boxWidth int
	rows                                    []string
}

// composerText is the typed characters a selection covers, ready for the
// clipboard: the border, the prompt and lipgloss's trailing pad all stripped.
// rows are the box's draft rows (the box without its top border), textLeft and
// textRight bound the text between the prompt and the far padding.
func composerText(rows []string, m marked, textLeft, textRight int) string {
	width := textRight - textLeft
	var out []string
	for i, row := range rows {
		c0, c1, ok := m.covers(i)
		if !ok {
			continue
		}
		if c1 == lineEnd {
			c1 = width
		}
		c0, c1 = min(max(c0, 0), width), min(max(c1, 0), width)
		if c1 <= c0 {
			out = append(out, "")
			continue
		}
		seg := ansi.Cut(row, textLeft+c0, textLeft+c1)
		out = append(out, strings.TrimRight(ansi.Strip(seg), " "))
	}
	return strings.Join(out, "\n")
}

// highlightComposerBlock restyles the dragged cells of a rendered composer.
// block is the whole View output; its first row is the box's top border, so
// draft row i is block row i+1. The border, prompt, target and hint rows are
// left alone - m only ever covers draft-row indices.
func highlightComposerBlock(block string, m marked, textLeft, textRight int) string {
	if m == (marked{}) {
		return block
	}
	width := textRight - textLeft
	lines := strings.Split(block, "\n")
	for i := 1; i < len(lines); i++ {
		c0, c1, ok := m.covers(i - 1)
		if !ok {
			continue
		}
		if c1 == lineEnd {
			c1 = width
		}
		c0, c1 = min(max(c0, 0), width), min(max(c1, 0), width)
		lines[i] = highlighted(lines[i], textLeft+c0, textLeft+c1)
	}
	return strings.Join(lines, "\n")
}
