package ui

// The tiled board: the fleet as a grid of view-only tiles rather than a column
// of rows. A second presentation of the same overview, toggled by ⇥ - not
// panes you operate. See board.go's header for the narrowed §2c ruling and the
// four guardrails that keep this a status wall.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// minTileWidth is the narrowest a tile can be and still hold a name in the
	// border edge and a state word under it. Below one tile's worth, the grid
	// draws a single column. Tuned against the pty harness (spec §9).
	minTileWidth = 26

	// minTileHeight is the shortest a tile can be and still frame a name, a
	// state word and a row of live tail. It floors how many tile rows fit in a
	// frame; the cells then stretch to fill whatever height is left, so a big
	// window grows the cells rather than the count.
	minTileHeight = 7

	// tileGap is the blank column between neighbouring tiles.
	tileGap = 1

	// tileFrameRows is the rows a tile spends on everything but the transcript:
	// the two border edges, the state word, the subagent count and the status
	// bar. The transcript window fills whatever the cell has left (cellH -
	// tileFrameRows), which is what tileInner computes - the board's revised
	// guardrail 2: a bounded transcript window, no scrollback.
	tileFrameRows = 5

	// minTileTailRows floors the transcript window so a tile too short to have a
	// budget of its own still keeps one row rather than none.
	minTileTailRows = 1
)

// tileGrid is the tiled board's geometry for one frame: how many columns and
// rows of tiles, and the width and height of each cell. One value, computed
// once by tileGridFor, so the draw, the mouse and the cursor all measure the
// same grid - the board's own "draw and mouse measure one number" invariant.
type tileGrid struct {
	cols, rows   int
	cellW, cellH int
}

// tileGridFor chooses a near-square grid that fills the frame. A few agents get
// big cells stretched across both axes; once there are more agents than fit at
// the minimum cell size, the grid caps at that maximum and the overflow pages
// through the cursor window. cellW keeps the fill-width split; cellH is the new
// half - the rows stretch to fill the height the way the columns already fill
// the width, so the wall auto-resizes with the window.
func tileGridFor(width, availH, n int) tileGrid {
	maxCols := max((width+tileGap)/(minTileWidth+tileGap), 1)
	maxRows := max(availH/minTileHeight, 1)
	cols, rows := 1, 1
	switch {
	case n <= 0:
		// A degenerate but safe single cell rather than a divide-by-zero.
	case n <= maxCols*maxRows:
		cols = clamp(ceilSqrt(n), 1, maxCols)
		rows = ceilDiv(n, cols)
		if rows > maxRows { // a short frame forces a flatter grid
			rows = maxRows
			cols = min(ceilDiv(n, rows), maxCols)
		}
	default: // more agents than fit at min size: cap and page the rest
		cols, rows = maxCols, maxRows
	}
	return tileGrid{cols: cols, rows: rows, cellW: tileCellWidth(width, cols), cellH: max(availH/rows, 1)}
}

// ceilSqrt is the smallest c with c*c >= n, the near-square column count for n
// tiles. Zero for a non-positive n.
func ceilSqrt(n int) int {
	c := 0
	for c*c < n {
		c++
	}
	return c
}

// ceilDiv is a/b rounded up, for a >= 0 and b >= 1.
func ceilDiv(a, b int) int { return (a + b - 1) / b }

// boardTileGrid is the frame's tile geometry, computed from the width and the
// available height the tiled board draws at - the pane less the title and the
// key line. The draw, the mouse and the cursor all read this one grid, so a
// click and a tile cannot disagree: the board's "measure one number" invariant,
// in two dimensions.
func (a App) boardTileGrid(n int) tileGrid {
	availH := max(a.paneHeight()-boardChromeRows-1, 1)
	return tileGridFor(a.layout.Width, availH, n)
}

// tileCellWidth is each tile's width once the column count is chosen: the frame
// split evenly, less the gaps between tiles.
func tileCellWidth(width, cols int) int {
	if cols < 1 {
		return width
	}
	return (width - tileGap*(cols-1)) / cols
}

type tileDir int

const (
	tileUp tileDir = iota
	tileDown
	tileLeft
	tileRight
)

// tileNav is the cursor one step in the grid, without wrapping: up and down
// move a whole row (± cols), left and right move one, and a step off an edge or
// past the last agent stays put.
func tileNav(cursor, cols, total int, dir tileDir) int {
	switch dir {
	case tileUp:
		if cursor-cols >= 0 {
			return cursor - cols
		}
	case tileDown:
		if cursor+cols < total {
			return cursor + cols
		}
	case tileLeft:
		if cursor%cols != 0 {
			return cursor - 1
		}
	case tileRight:
		if cursor%cols != cols-1 && cursor+1 < total {
			return cursor + 1
		}
	}
	return cursor
}

// tileWindowStart is the index of the first tile drawn, so the cursor's row is
// on screen. It pages by whole rows of `cols`, the cursor riding the bottom
// edge once it is past the first window - boardWindowStart in two dimensions.
func tileWindowStart(cursor, total, cols, visibleRows int) int {
	row := cursor / cols
	totalRows := (total + cols - 1) / cols
	startRow := clamp(row-visibleRows+1, 0, max(totalRows-visibleRows, 0))
	return startRow * cols
}

// tileView is the whole frame as a grid of tiles: the same title the row view
// draws, tiles between them in attention order, and its own key line -
// boardKeyLineTiles rather than the row view's, since ←→ really move the
// cursor here.
func (a App) tileView(agents []Agent, width int) string {
	g := a.boardTileGrid(len(agents))
	cursor := a.boardCursor(agents)
	start := tileWindowStart(cursor, len(agents), g.cols, g.rows)

	head := mutedLine(fmt.Sprintf("%s — %d agents", boardTitle, len(agents)), width)
	body := make([]string, 0, g.rows)
	for r := 0; r < g.rows; r++ {
		cells := make([]string, 0, g.cols)
		for c := 0; c < g.cols; c++ {
			i := start + r*g.cols + c
			if i >= len(agents) {
				cells = append(cells, strings.Repeat(" ", g.cellW))
				continue
			}
			cells = append(cells, a.tile(agents[i], g.cellW, g.cellH, i == cursor))
		}
		body = append(body, joinTilesRow(cells))
	}
	key := mutedLine(boardKeyLineTiles, width)
	return head + "\n" + strings.Join(body, "\n") + "\n" + key
}

// joinTilesRow lays one row of tiles side by side with the gap between them.
func joinTilesRow(cells []string) string {
	gap := strings.Repeat(" ", tileGap)
	return lipgloss.JoinHorizontal(lipgloss.Top, interleave(cells, gap)...)
}

// tile is one agent as a full rounded box of a fixed cell size: the name in the
// top edge, its state, its live tail and its subagent count in the body, and a
// per-agent status bar on the last body row - with walls down both sides. The
// body is built to exactly height-2 rows - the border owns the other two - so
// the box is exactly `height` tall and the grid stays regular; titledBox itself
// never constrains height. This holds for height >= 3 (the box's own minimum:
// two borders and a row); a shorter cell only occurs on a terminal too small to
// use the board, where View's firstRows clips the frame so an oversize tile
// cannot scroll the alt screen.
//
// The border wears the attention colour - the accent when cursored, warn when
// blocked, else the receding grey - so "needs you" is legible in a wall of
// thirty; the name wears the agent's own identity hue. The body text keeps the
// ordinary Text foreground, which is what fills between the walls.
func (a App) tile(ag Agent, width, height int, cursored bool) string {
	border := Border
	switch {
	case cursored:
		border = Accent
	case ag.State == rpc.StateBlocked:
		border = Warn
	}
	boxStyle := lipgloss.NewStyle().Foreground(Text).
		Border(lipgloss.RoundedBorder()).BorderForeground(border)
	// The name is drawn in the agent's identity hue; a session without one
	// falls back to the border colour so cursored and blocked tiles still read.
	// A borderless style, because titledEdge renders the label alone - a
	// bordered one would frame the name in a box of its own.
	name, ok := identityStyleFor(ag)
	if !ok {
		name = lipgloss.NewStyle().Foreground(border)
	}
	head := rowGlyph(ag) + " " + ag.Name
	body := a.tileBody(ag, width, max(height-2, 1))
	return titledBox(body, width, boxStyle, oneLine(head), "", name, name)
}

// tileBody is the tile's inner rows: the state word on top, the live tail (or a
// by-state detail line) filling the middle, and the subagent count and status
// bar pinned to the bottom. Every line is agent-authored or derived from
// agent-authored text, and every one goes through oneLine before it joins
// `lines` - the live tail by way of tailLines, since a raw CR or escape in a
// streamed token could otherwise redraw or forge the tile beside it (tiles sit
// side by side via lipgloss.JoinHorizontal).
//
// `lines` holds one PHYSICAL row per element - tailLines flattens the tail's
// own "\n"s into separate elements, and each framing line is truncated to
// `inner` - so padRows sizes by row count and titledBox's Width(edge) never
// word-wraps a line into a second physical row that would overshoot the cell.
//
// The middle fills every row between the state line and the bottom framing, so
// a tall cell fills with output rather than stopping at a fixed cap. The bottom
// framing is dropped from the bottom up when the body is too short: the status
// bar first, since it is the least urgent thing on the tile (statusbar.go),
// then the subagent count - so the smallest bodies keep the state and the count
// the eye triages by.
func (a App) tileBody(ag Agent, width, rows int) string {
	inner := max(width-boxFrameWidth, 1)
	lines := []string{HintStyle.Render(ansi.Truncate(labelOf(ag.State), inner, ellipsis))}

	bottom := make([]string, 0, 2)
	if rows >= 2 {
		bottom = append(bottom, tileSubagents(len(a.fleet.RunningTasks(ag.ID)), inner))
	}
	if rows >= 3 {
		// One row: a tile has a fixed height, so the bar never wraps here.
		bottom = append(bottom, statusBar(ag, a.modeOf(ag.ID), inner, tileBarRows))
	}

	lines = append(lines, a.tileMiddle(ag, inner, max(rows-1-len(bottom), 0))...)
	lines = append(lines, bottom...)
	return strings.Join(padRows(lines, rows), "\n")
}

// tileMiddle is the rows between the state line and the bottom framing: the
// agent's live transcript tail at DM fidelity, sized to the cell in
// ensureBoardDMs and drawn here, following the newest line. Every state draws it
// - idle shows the last exchange, working shows text streaming in, blocked shows
// the transcript with the ask below it. Empty rows while a freshly-seeded tile's
// disk history is still in flight; the framing (state, subagents, bar) draws
// immediately regardless. It returns exactly `rows` rows so the box stays its
// cell height and boardHit's click math holds.
//
// The transcript lines keep their SGR colour (the DM pane draws the same lines,
// so they are safe) and are only bounded in width by transcriptWindow. The
// streaming preview rides at the bottom, hardened through tailLines - oneLine
// strips the raw control bytes a token could carry to redraw or forge a
// neighbouring tile, which the coloured transcript above must not be run through
// (oneLine maps ESC to a space and would strip its colour).
func (a App) tileMiddle(ag Agent, inner, rows int) []string {
	if rows <= 0 {
		return nil
	}
	d, ok := a.boardDMs[ag.ID]
	if !ok {
		return padRows(nil, rows) // seeded on the next Update; the framing draws now
	}
	_, view := d.transcriptWindow(inner, rows)
	lines := strings.Split(view, "\n")
	// Clamp each transcript line to inner. transcriptWindow floors its render
	// width to minBlockWidth, so at a tile narrower than that its lines come back
	// wider than inner - and titledBox's Width(edge) then word-wraps the overrun
	// into an extra physical row, growing the tile past its cell height. ansi.Truncate
	// is SGR-aware, so this keeps the transcript's colour where oneLine would strip it.
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, inner, "")
	}
	if pv := d.partial.sized(inner).view; pv != "" {
		lines = append(dropTrailingBlank(lines), tailLines(pv, inner)...)
	}
	if len(lines) > rows {
		lines = lines[len(lines)-rows:] // the newest rows that fit
	}
	return padRows(lines, rows)
}

// dropTrailingBlank removes the blank padding transcript.view adds below a short
// transcript, so the streaming preview appended after it continues the
// conversation rather than sitting past a gap.
func dropTrailingBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// tileSubagents is the "⤷ N subagents" line, dim and truncated to the tile's
// inner width - titledBox's Width(edge) word-wraps an over-wide line into a
// second physical row that would overshoot the cell.
func tileSubagents(count, inner int) string {
	word := "subagents"
	if count == 1 {
		word = "subagent"
	}
	return HintStyle.Render(ansi.Truncate(fmt.Sprintf("⤷ %d %s", count, word), inner, ellipsis))
}

// tailLines splits a live tail's wrapped view into its physical rows and
// hardens each one. oneLine strips "\n" as a control byte along with every
// other one, so the split has to happen first - running oneLine on the whole
// view before splitting would collapse a multi-row wrap into a single row.
//
// The tail wraps at max(inner, minBlockWidth) (partial.wrapped), so at a
// tile narrower than minBlockWidth its lines come back wider than inner - the
// same boardDetail lines beside them are already truncated to. Each line is
// truncated here too, or titledBox's Width(edge) word-wraps the overrun into
// extra physical rows and grows the tile past its cell height.
func tailLines(view string, inner int) []string {
	rows := strings.Split(view, "\n")
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ansi.Truncate(oneLine(r), inner, ellipsis)
	}
	return out
}

// interleave puts `sep` between every pair of items and returns the flat slice.
func interleave(items []string, sep string) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, s := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, s)
	}
	return out
}

// padRows pads a slice of rows up to n with blank rows, so every tile is the
// same height and the grid stays regular.
func padRows(rows []string, n int) []string {
	for len(rows) < n {
		rows = append(rows, "")
	}
	return rows[:n]
}
