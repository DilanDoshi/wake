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

// tileView is the whole frame as a grid of tiles: the same title the row view
// draws, tiles between them in attention order, and its own key line -
// boardKeyLineTiles rather than the row view's, since ←→ really move the
// cursor here.
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
	key := mutedLine(boardKeyLineTiles, width)
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
// tileBodyRows on top of them - overshooting tileHeight() and growing the
// whole tile row, since titledBox never constrains height. The state line and
// the subagent-count line are truncated to `inner` for the same reason: each
// is one `lines` element, and titledBox's Width(edge) word-wraps either one
// into a second physical row the moment it is wider than the tile's edge.
func (a App) tileBody(ag Agent, width int) string {
	inner := max(width-boxFrameWidth, 1)
	lines := []string{HintStyle.Render(ansi.Truncate(labelOf(ag.State), inner, ellipsis))}

	if ag.State == rpc.StateWorking {
		if tail := a.tails[ag.ID].sized(inner); tail.text != "" {
			lines = append(lines, tailLines(tail.view, inner)...)
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
	return strings.Join(padRows(lines, tileBodyRows), "\n")
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
// extra physical rows and grows the tile past tileHeight().
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
