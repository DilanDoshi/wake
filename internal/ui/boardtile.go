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

	// maxTileTailRows bounds the live tail a tile retains. It relaxes the DM
	// preview's maxPreviewRows cap (partial.go) so a big cell fills with output
	// rather than stopping at three rows - the board's narrowed guardrail 2:
	// bounded to the tile body, still no scrollback. A render draws only the
	// rows its own cell has; this is the ceiling on what is kept per token.
	maxTileTailRows = 10
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

// tile is one agent as a rounded box of a fixed cell size: the name in the top
// edge, its state and what it is doing (or saying) in the body, and its
// subagent count. The body is built to exactly height-2 rows - the border owns
// the other two - so the box is exactly `height` tall and the grid stays
// regular; titledBox itself never constrains height.
func (a App) tile(ag Agent, width, height int, cursored bool) string {
	boxStyle := TextStyle
	switch {
	case cursored:
		boxStyle = AccentStyle
	case ag.State == rpc.StateBlocked:
		boxStyle = warnStyle
	}
	head := rowGlyph(ag) + " " + ag.Name
	body := a.tileBody(ag, width, max(height-2, 1))
	return titledBox(body, width, boxStyle, oneLine(head), "", boxStyle, boxStyle)
}

// tileBody is the tile's inner rows: the state word, then what the agent is on
// (a live tail while it works, the ask while it is blocked, its last line when
// idle), then the subagent count - boardDetail's by-state logic, one field
// richer for the live tail. Every line is agent-authored or derived from
// agent-authored text, and every line goes through oneLine before it joins
// `lines` - the live tail included, by way of tailLines, since a raw CR or
// escape in a streamed token could otherwise redraw or forge the tile beside
// it (tiles sit side by side via lipgloss.JoinHorizontal).
//
// `lines` holds one PHYSICAL row per element - tailLines flattens the tail's
// own "\n"s into separate elements before this returns - so padRows pads and
// truncates by row count, not by how many pieces the body was assembled from.
// Without that, a tail wrapped to multiple lines was one `lines` element
// holding several rows, and padRows padded the *element* count back up to
// `rows` on top of them - overshooting the cell height and growing the whole
// tile row, since titledBox never constrains height. The state line and the
// subagent-count line are truncated to `inner` for the same reason: each is
// one `lines` element, and titledBox's Width(edge) word-wraps either one into a
// second physical row the moment it is wider than the tile's edge.
//
// The tail fills the rows between the state line and the subagent count. It is
// cut to that budget here - the newest rows kept - so the subagent line, which
// is appended last, is never the row padRows drops when a big tail overruns.
func (a App) tileBody(ag Agent, width, rows int) string {
	inner := max(width-boxFrameWidth, 1)
	lines := []string{HintStyle.Render(ansi.Truncate(labelOf(ag.State), inner, ellipsis))}

	tailRows := max(rows-2, 0) // the rows left once the state and subagent lines are placed
	if ag.State == rpc.StateWorking {
		if tail := a.tails[ag.ID].sized(inner); tail.text != "" {
			tl := tailLines(tail.view, inner)
			if len(tl) > tailRows {
				tl = tl[len(tl)-tailRows:]
			}
			lines = append(lines, tl...)
		} else if d := boardDetail(ag); d != "" {
			lines = append(lines, ansi.Truncate(oneLine(d), inner, ellipsis))
		}
	} else if d := boardDetail(ag); d != "" {
		lines = append(lines, ansi.Truncate(oneLine(d), inner, ellipsis))
	}

	subs := len(a.fleet.RunningTasks(ag.ID))
	word := "subagents"
	if subs == 1 {
		word = "subagent"
	}
	lines = append(lines, ansi.Truncate(fmt.Sprintf("⤷ %d %s", subs, word), inner, ellipsis))
	return strings.Join(padRows(lines, rows), "\n")
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
