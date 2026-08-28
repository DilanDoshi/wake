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

	// tileGap is the blank column between neighbouring tiles.
	tileGap = 1

	// tileBodyRows is the fixed inner height every tile pads to, so a wall of
	// tiles is a regular grid: a state line, up to maxPreviewRows of live tail,
	// and the subagent count. Tuned against the pty harness.
	tileBodyRows = maxPreviewRows + 2
)

// tileHeight is the rows one tile occupies: its body plus the rounded border's
// top and bottom.
func tileHeight() int { return tileBodyRows + 2 }

// tileColumns is how many tiles fit across a width, at least one.
func tileColumns(width int) int {
	return max((width+tileGap)/(minTileWidth+tileGap), 1)
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

// tileView is the whole frame as a grid of tiles: the same title and key line
// the row view draws, with tiles between them in attention order.
func (a App) tileView(agents []Agent, width int) string {
	cols := tileColumns(width)
	cellW := tileCellWidth(width, cols)
	rowsVisible := max((a.paneHeight()-boardChromeRows-1)/tileHeight(), 1)
	cursor := a.boardCursor(agents)
	start := tileWindowStart(cursor, len(agents), cols, rowsVisible)

	head := mutedLine(fmt.Sprintf("%s — %d agents", boardTitle, len(agents)), width)
	body := make([]string, 0, rowsVisible)
	for r := 0; r < rowsVisible; r++ {
		cells := make([]string, 0, cols)
		for c := 0; c < cols; c++ {
			i := start + r*cols + c
			if i >= len(agents) {
				cells = append(cells, strings.Repeat(" ", cellW))
				continue
			}
			ag := agents[i]
			cells = append(cells, a.tile(ag, cellW, i == cursor))
		}
		body = append(body, joinTilesRow(cells))
	}
	key := mutedLine(boardKeyLine, width)
	return head + "\n" + strings.Join(body, "\n") + "\n" + key
}

// joinTilesRow lays one row of tiles side by side with the gap between them.
func joinTilesRow(cells []string) string {
	gap := strings.Repeat(" ", tileGap)
	return lipgloss.JoinHorizontal(lipgloss.Top, interleave(cells, gap)...)
}

// tile is one agent as a rounded box: the name in the top edge, its state and
// what it is doing (or saying) in the body, and its subagent count.
func (a App) tile(ag Agent, width int, cursored bool) string {
	boxStyle := TextStyle
	switch {
	case cursored:
		boxStyle = AccentStyle
	case ag.State == rpc.StateBlocked:
		boxStyle = warnStyle
	}
	head := rowGlyph(ag) + " " + ag.Name
	body := a.tileBody(ag, width)
	return titledBox(body, width, boxStyle, oneLine(head), "", boxStyle, boxStyle)
}

// tileBody is the tile's inner rows: the state word, then what the agent is on
// (a live tail while it works, the ask while it is blocked, its last line when
// idle), then the subagent count - boardDetail's by-state logic, one field
// richer for the live tail. The state word and boardDetail's output are
// agent-authored and go through oneLine; the live tail does not, matching
// dm.go's own preview line (d.partial.view), which draws the same partial
// unsanitized - a gap this task does not close, since fixing it means
// changing partial.go's wrap, out of scope here.
func (a App) tileBody(ag Agent, width int) string {
	inner := max(width-boxFrameWidth, 1)
	lines := []string{HintStyle.Render(labelOf(ag.State))}

	if ag.State == rpc.StateWorking {
		if tail := a.tails[ag.ID].sized(inner); tail.text != "" {
			lines = append(lines, tail.view)
		} else if d := boardDetail(ag); d != "" {
			lines = append(lines, ansi.Truncate(oneLine(d), inner, ellipsis))
		}
	} else if d := boardDetail(ag); d != "" {
		lines = append(lines, ansi.Truncate(oneLine(d), inner, ellipsis))
	}

	subs := len(a.fleet.RunningTasks(ag.ID))
	lines = append(lines, fmt.Sprintf("⤷ %d subagents", subs))
	return strings.Join(padRows(lines, tileBodyRows), "\n")
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
