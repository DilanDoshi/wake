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
		// No selection yet (a fresh board open, Selected == "") or the cursor's
		// agent has left the fleet: seed at the first tile, boardCursor's own
		// "empty or gone → top" recovery, so an arrow lands on a tile rather than
		// doing nothing (the flat grid seeded from boardCursor for this reason).
		first := adjacentTileRow(rows, -1, +1)
		if first < 0 {
			return cursorID // no tiles at all
		}
		tr, c = first, 0
	}
	switch dir {
	case tileLeft:
		if c > 0 {
			c--
		}
	case tileRight:
		if c+1 < len(rows[tr].tiles) {
			c++
		}
	case tileUp:
		if i := adjacentTileRow(rows, tr, -1); i >= 0 {
			tr, c = i, min(c, len(rows[i].tiles)-1)
		}
	case tileDown:
		if i := adjacentTileRow(rows, tr, +1); i >= 0 {
			tr, c = i, min(c, len(rows[i].tiles)-1)
		}
	}
	return rows[tr].tiles[c].ID
}

// tileLayout is the tiled board's section-aware geometry for one frame: the
// column width every tile shares, the height a tile row shares, and the window of
// lines (viewport) the shelves scroll under. Its bands are the tile rows; a
// header band is no stop of its own and rides above the row it heads.
type tileLayout struct {
	viewport
	cols, cellW, cellH int
	rows               []tileRow
	tops               []int // the first line of each row
	bandRow            []int // each band's row, in the order the bands run
}

// rowHeight is a header band (teamHeaderRows: a blank then the divider) or a
// tile row (cellH), the one number the draw, the hit and the window all count a
// band by.
func (l tileLayout) rowHeight(i int) int {
	if l.rows[i].isHeader() {
		return teamHeaderRows
	}
	return l.cellH
}

// shown is the rows the window touches, as a half-open run.
func (l tileLayout) shown() (from, to int) {
	for i := range l.rows {
		if l.tops[i]+l.rowHeight(i) > l.off && l.tops[i] < l.off+l.view {
			if to == 0 { // the first row the window touches
				from = i
			}
			to = i + 1
		}
	}
	return from, to
}

// visibleAgents is the tiles any line of which the window shows, so the
// transcript seeding asks history for exactly what is drawn.
func (l tileLayout) visibleAgents() []Agent {
	var out []Agent
	from, to := l.shown()
	for _, row := range l.rows[from:to] {
		out = append(out, row.tiles...)
	}
	return out
}

// selectBand puts the selection on a tile row, in the column it is already in
// (a shorter row keeps it in bounds).
func (l tileLayout) selectBand(a App, i int) App {
	_, c := tileFind(l.rows, a.board.Selected)
	tiles := l.rows[l.bandRow[i]].tiles
	a.board.Selected = tiles[min(max(c, 0), len(tiles)-1)].ID
	a.board.SelectedTask = ""
	return a
}

// endpoint is the grid's first or last tile - the last of the last row, not the
// column the selection is in.
func (l tileLayout) endpoint(last bool) (agent, task string) {
	tiles := l.rows[l.bandRow[0]].tiles
	if last {
		tiles = l.rows[l.bandRow[len(l.bandRow)-1]].tiles
		return tiles[len(tiles)-1].ID, ""
	}
	return tiles[0].ID, ""
}

// tileCellHeight is the height every tile row shares. Everything that fits at the
// floor stretches to fill the frame; a bigger fleet keeps the floor and scrolls,
// stretched to a whole number of rows so no frame ends on a blank gap. A frame
// shorter than the floor gets one tile as tall as itself, never taller: a tile
// the frame clips loses its status bar. Below minDrawnTileRows the tile is that
// tall anyway, which the frame then clips.
func tileCellHeight(rows []tileRow, availH int) int {
	headerLines, tileRows := 0, 0
	for _, r := range rows {
		if r.isHeader() {
			headerLines += teamHeaderRows
		} else {
			tileRows++
		}
	}
	// A frame holds one header band along with its rows, however many the fleet
	// has, so the floor is what is left of the frame beside it.
	reserve := min(headerLines, teamHeaderRows)
	floor := max(min(minTileHeight, availH-reserve), minDrawnTileRows)
	if tileRows == 0 {
		return floor
	}
	if headerLines+tileRows*floor <= availH {
		return (availH - headerLines) / tileRows
	}
	fit := max((availH-reserve)/floor, 1)
	return max((availH-reserve)/fit, floor)
}

// boardTileCols is how many tiles share a row: three across at most, fewer for a
// fleet that small or a terminal that narrow.
func boardTileCols(n, width int) int {
	fits := max((width+tileGap)/(minTileWidth+tileGap), 1)
	return clamp(n, 1, min(maxTileCols, fits))
}

// newTileLayout lays the shelves out `availH` lines tall, scrolled to `scroll`,
// the selection `selected`.
func newTileLayout(rows []tileRow, cols, cellW, availH, scroll int, selected string) tileLayout {
	l := tileLayout{cols: cols, cellW: cellW, rows: rows, cellH: tileCellHeight(rows, availH)}
	l.view = availH
	l.tops = make([]int, len(rows))
	line, selRow := 0, -1
	for i, r := range rows {
		l.tops[i] = line
		if r.isHeader() {
			line += teamHeaderRows
			continue
		}
		lead := 0
		if i > 0 && rows[i-1].isHeader() {
			lead = teamHeaderRows
		}
		if tileHas(r, selected) {
			selRow = len(l.bands)
		}
		l.bands = append(l.bands, band{top: line, h: l.cellH, lead: lead})
		l.bandRow = append(l.bandRow, i)
		line += l.cellH
	}
	l.total = line
	// No selection yet, or its agent has left: the first tile, boardCursor's own
	// "empty or gone -> top".
	l.cur = max(selRow, 0)
	if len(l.bands) == 0 {
		l.cur = -1
	}
	l.viewport = l.settled(scroll)
	return l
}

func tileHas(row tileRow, id string) bool {
	for _, ag := range row.tiles {
		if ag.ID == id {
			return true
		}
	}
	return false
}

// boardTileLayout is the frame's section-aware tile geometry: three columns at
// most, the shelves from the sectioned fleet, cellH to fill, and the window the
// board is scrolled to.
func (a App) boardTileLayout(agents []Agent) tileLayout {
	availH := max(a.paneHeight()-boardChromeRows-1, 1)
	cols := boardTileCols(len(agents), a.layout.Width)
	return newTileLayout(tileShelves(a.fleet.sections(agents), cols), cols,
		tileCellWidth(a.layout.Width, cols), availH, a.board.Scroll, a.board.Selected)
}

// tileHit is boardHit's tiled branch: the agent index a click at (x, y) lands on,
// or -1 for the title row, a header band, or past the last column. It reads the
// window the draw used, so a click and a tile cannot disagree across a section
// break or a scroll.
func (l tileLayout) tileHit(x, y int, agents []Agent) int {
	line := y - boardChromeRows
	if line < 0 || line >= l.view {
		return -1 // the title row, or below the window (the key line, the strip)
	}
	from, to := l.shown()
	for ri := from; ri < to; ri++ {
		if l.off+line >= l.tops[ri]+l.rowHeight(ri) {
			continue
		}
		if l.rows[ri].isHeader() {
			return -1 // a header belongs to no agent
		}
		col := x / (l.cellW + tileGap)
		if col < 0 || col >= len(l.rows[ri].tiles) {
			return -1
		}
		return indexOf(agents, l.rows[ri].tiles[col].ID)
	}
	return -1
}
