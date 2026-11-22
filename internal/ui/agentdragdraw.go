package ui

// How a live agent drag is drawn: the agent's name as a small coloured tag at
// the pointer, and the drop zone outlined. Both are laid over the assembled
// frame, as the screen selection's highlight is, so nothing re-lays out until
// the button is released - a drag is hundreds of motions and the layout is
// settled once, on the drop. The outline's cells are built when the zone changes
// (dragMoved), not per motion; only the splice into the frame is per motion, and
// a frame with no live drag is returned as it came.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// tagMaxWidth bounds the name the tag shows, so a long name stays a small tag.
const tagMaxWidth = 24

// seg is text laid over a row from a column.
type seg struct {
	col  int
	text string
}

// edit is the segments one frame row takes, in column order.
type edit struct {
	row  int
	segs []seg
}

// overlayAgentDrag lays the outline and the tag over a frame while a drag is
// live, and costs nothing otherwise.
func (a App) overlayAgentDrag(frame string) string {
	d := a.dragged
	if !d.live {
		return frame
	}
	lines := strings.Split(frame, "\n")
	for _, e := range d.outline {
		if e.row >= 0 && e.row < len(lines) {
			lines[e.row] = spliced(lines[e.row], e.segs)
		}
	}
	if row, s, ok := a.tagAt(d); ok && row < len(lines) {
		lines[row] = spliced(lines[row], []seg{s})
	}
	return strings.Join(lines, "\n")
}

// tagAt is the tag drawn at the pointer, one cell right of it and kept inside the
// frame.
func (a App) tagAt(d agentDrag) (row int, s seg, ok bool) {
	agent, _ := a.fleet.Agent(d.id)
	hue, coloured := identityStyleFor(agent)
	if !coloured {
		hue = AccentStyle
	}
	text := ansi.Truncate(oneLine(d.name), tagMaxWidth, ellipsis)
	tag := lipgloss.NewStyle().Background(hue.GetForeground()).Foreground(TagInk).Bold(true).Render(" " + text + " ")
	w := ansi.StringWidth(tag)
	if w > a.layout.Width || d.y < 0 {
		return 0, seg{}, false
	}
	return d.y, seg{col: clamp(d.x+1, 0, a.layout.Width-w), text: tag}, true
}

// spliced is a row with each segment laid over the cells from its column, the
// rest of the row as it was. Segments are in column order and do not overlap.
func spliced(line string, segs []seg) string {
	w := ansi.StringWidth(line)
	var b strings.Builder
	at := 0
	for _, s := range segs {
		if s.col < at {
			continue
		}
		if s.col > w { // a short row: pad to the segment
			line += strings.Repeat(" ", s.col-w)
			w = s.col
		}
		b.WriteString(cells(line, at, s.col))
		b.WriteString(s.text)
		at = s.col + ansi.StringWidth(s.text)
	}
	b.WriteString(cells(line, at, max(w, at)))
	return b.String()
}

// cells is exactly the cells [from, to) of a row. ansi.Cut keeps a wide rune whose
// second cell is `from`, a cell too many, and drops one whose first cell is the
// last of the range, a cell too few: a row spliced from them moves everything
// after it by one. Each half cut off a wide rune is a blank instead.
func cells(line string, from, to int) string {
	if to <= from {
		return ""
	}
	lead, trail := "", ""
	if from > 0 && ansi.StringWidth(ansi.Cut(line, from, from+1)) > 1 {
		lead, from = " ", from+1
	}
	end := to
	if ansi.StringWidth(ansi.Cut(line, to-1, to)) == 0 {
		trail, end = " ", to-1
	}
	return lead + ansi.Cut(line, from, max(end, from)) + trail
}

// outlineOf is the box a zone draws: where the agent will be once it is dropped.
// None for the sidebar, and for a zone whose pane is not on screen.
func (a App) outlineOf(z dropZone, id string) []edit {
	x, y, w, h, ok := a.zoneRect(z, id)
	if !ok || w < 2 || h < 2 {
		return nil
	}
	rule := strings.Repeat("─", w-2)
	side := AccentStyle.Render("│")
	rows := make([]edit, 0, h)
	rows = append(rows, edit{row: y, segs: []seg{{x, AccentStyle.Render("┌" + rule + "┐")}}})
	for r := y + 1; r < y+h-1; r++ {
		rows = append(rows, edit{row: r, segs: []seg{{x, side}, {x + w - 1, side}}})
	}
	return append(rows, edit{row: y + h - 1, segs: []seg{{x, AccentStyle.Render("└" + rule + "┘")}}})
}

// zoneRect is the cells the dropped agent's pane takes once the drop is done: the
// pane in the grid the drop would produce, with the keys moved to it - the window
// slides to the column that then holds them. It is the drop's own grid operation
// (newColumnPlace, the lower slot, the agent's pane where it already is), so it is
// where the agent lands by construction.
func (a App) zoneRect(z dropZone, id string) (x, y, w, h int, ok bool) {
	switch z.kind {
	case zoneColumn:
		return a.landed(a.newColumnPlace(id)(a.grid), id)
	case zoneBelow:
		return a.landed(a.grid.OpenBelow(z.pane, id), id)
	case zoneFocus:
		return a.landed(a.grid, z.pane)
	}
	return 0, 0, 0, 0, false
}

// landed is where id's pane is drawn in grid g once it holds the keys.
func (a App) landed(g Grid, id string) (int, int, int, int, bool) {
	a.grid, a.focus = g, id
	return a.paneRect(id)
}

// paneRect is the cells id's pane is drawn in under this App's layout and focus,
// and false when it is not on screen: its size is paneSize's, and it starts where
// its column does, a lower pane under the rule below its column's upper one.
func (a App) paneRect(id string) (x, y, w, h int, ok bool) {
	if !a.grid.Has(id) {
		return 0, 0, 0, 0, false
	}
	w, h, ok = a.paneSize(id)
	if !ok {
		return 0, 0, 0, 0, false
	}
	col := a.columnOf(id)
	if id != "" && a.grid.Cols[col].Bottom == id {
		y, _ = a.rowsOf(col)
		y += dividerHeight
	}
	return a.layout.PaneLeft(a.regions(), col), y, w, h, true
}
