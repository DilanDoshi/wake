package ui

// The tiled board sectioned into per-team shelves: a header before each team's
// tiles, the tiles wrapping *within* the team so the next team starts a fresh
// band. The uniform grid (i = start + r*cols + c over one flat index) could not
// express that - a team of two beside cols=3 would flow the next team's tile
// into its row - so the tiled board grows a row model of its own, the way the
// roster did, with headers that belong to no agent.
//
// One layout value (boardTileLayout) is computed once and read by the draw, the
// mouse and the cursor, so a click and a tile cannot disagree - the board's
// "measure one number" invariant, now over bands of varying height. With no team
// there is one header-less top block and the shelves are the flat wall this build
// always drew.

// tileRow is one drawn band: a team header, or a row of up to cols tiles.
type tileRow struct {
	header string  // team header, "" for a tile row
	tiles  []Agent // the tiles in this row, nil for a header
}

func (row tileRow) isHeader() bool { return row.header != "" }

// tileShelves lays the sections into bands: a header then the wrapped tiles for
// each team, and the top block's tiles with no header. cols is the tiles-per-row.
func tileShelves(sections []Section, cols int) []tileRow {
	if cols < 1 {
		cols = 1
	}
	var rows []tileRow
	for _, s := range sections {
		if s.Team != "" {
			rows = append(rows, tileRow{header: s.Team})
		}
		for i := 0; i < len(s.Agents); i += cols {
			rows = append(rows, tileRow{tiles: s.Agents[i:min(i+cols, len(s.Agents))]})
		}
	}
	return rows
}

// tileFind is the (row, column) of the cursor's tile, or (-1, -1).
func tileFind(rows []tileRow, id string) (int, int) {
	for i, row := range rows {
		for j, ag := range row.tiles {
			if ag.ID == id {
				return i, j
			}
		}
	}
	return -1, -1
}

// adjacentTileRow is the nearest tile row above (step -1) or below (step +1) a
// given row, skipping header rows, or -1 at the edge.
func adjacentTileRow(rows []tileRow, from, step int) int {
	for i := from + step; i >= 0 && i < len(rows); i += step {
		if len(rows[i].tiles) > 0 {
			return i
		}
	}
	return -1
}

// tileNavSection moves the cursor one step over the shelves, no wrapping:
// left/right within a row, up/down to the nearest tile in the adjacent tile row
// (headers skipped), clamped to that row's own tiles so a short row keeps it in
// bounds. Returns the id to select, unchanged at an edge.
func tileNavSection(rows []tileRow, cursorID string, dir tileDir) string {
	tr, c := tileFind(rows, cursorID)
	if tr < 0 {
		return cursorID
	}
	switch dir {
	case tileLeft:
		if c > 0 {
			return rows[tr].tiles[c-1].ID
		}
	case tileRight:
		if c+1 < len(rows[tr].tiles) {
			return rows[tr].tiles[c+1].ID
		}
	case tileUp:
		if i := adjacentTileRow(rows, tr, -1); i >= 0 {
			return rows[i].tiles[min(c, len(rows[i].tiles)-1)].ID
		}
	case tileDown:
		if i := adjacentTileRow(rows, tr, +1); i >= 0 {
			return rows[i].tiles[min(c, len(rows[i].tiles)-1)].ID
		}
	}
	return cursorID
}

// tileLayout is the tiled board's section-aware geometry for one frame: the
// column width every tile shares, the height a tile row shares, and the window
// of shelves [from, to) that fit and hold the cursor.
type tileLayout struct {
	cols     int
	cellW    int
	cellH    int
	availH   int
	rows     []tileRow
	from, to int
}

// rowHeight is a header (one line) or a tile row (cellH), the one number the
// draw, the hit and the window all count a band by.
func (l tileLayout) rowHeight(i int) int {
	if l.rows[i].isHeader() {
		return 1
	}
	return l.cellH
}

// visibleTiles is the agents drawn this frame - the tiles in the windowed rows -
// so the transcript seeding asks history for exactly what is on screen.
func (l tileLayout) visibleTiles() []Agent {
	var out []Agent
	for _, row := range l.rows[l.from:l.to] {
		out = append(out, row.tiles...)
	}
	return out
}

// tileCellHeight fills the frame when the shelves fit at their minimum, else
// falls to the minimum and lets the window page the overflow - the flat grid's
// stretch-or-cap rule, counting the header rows the shelves add.
func tileCellHeight(rows []tileRow, availH int) int {
	headers, tileRows := 0, 0
	for _, r := range rows {
		if r.isHeader() {
			headers++
		} else {
			tileRows++
		}
	}
	if tileRows == 0 {
		return minTileHeight
	}
	if headers+tileRows*minTileHeight <= availH {
		return max((availH-headers)/tileRows, minTileHeight)
	}
	return minTileHeight
}

// tileWindow is the run of shelves that fits availH and holds the cursor's row,
// grown around it the way the roster's window is, so the cursor's tile and the
// header above its section come into view together.
func tileWindow(l tileLayout, cursorRow, availH int) (from, to int) {
	if len(l.rows) == 0 {
		return 0, 0
	}
	total := 0
	for i := range l.rows {
		total += l.rowHeight(i)
	}
	if total <= availH {
		return 0, len(l.rows)
	}
	if cursorRow < 0 {
		cursorRow = 0
	}
	used := l.rowHeight(cursorRow)
	from, to = cursorRow, cursorRow+1
	for to < len(l.rows) && used+l.rowHeight(to) <= availH {
		used += l.rowHeight(to)
		to++
	}
	for from > 0 && used+l.rowHeight(from-1) <= availH {
		used += l.rowHeight(from - 1)
		from--
	}
	return from, to
}

// boardTileLayout is the frame's section-aware tile geometry: cols and cellW from
// the width (near-square, the flat grid's own choice), the shelves from the
// sectioned fleet, cellH to fill, and the window around the cursor.
func (a App) boardTileLayout(agents []Agent) tileLayout {
	availH := max(a.paneHeight()-boardChromeRows-1, 1)
	maxCols := max((a.layout.Width+tileGap)/(minTileWidth+tileGap), 1)
	cols := clamp(ceilSqrt(len(agents)), 1, maxCols)
	l := tileLayout{
		cols:   cols,
		cellW:  tileCellWidth(a.layout.Width, cols),
		availH: availH,
		rows:   tileShelves(a.fleet.sections(agents), cols),
	}
	l.cellH = tileCellHeight(l.rows, availH)
	cursorRow, _ := tileFind(l.rows, a.board.Selected)
	l.from, l.to = tileWindow(l, cursorRow, availH)
	return l
}
