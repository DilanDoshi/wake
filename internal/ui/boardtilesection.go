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

// rowHeight is a header band (teamHeaderRows: a blank then the divider) or a
// tile row (cellH), the one number the draw, the hit and the window all count a
// band by.
func (l tileLayout) rowHeight(i int) int {
	if l.rows[i].isHeader() {
		return teamHeaderRows
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
// stretch-or-cap rule, counting the header band lines the shelves add.
func tileCellHeight(rows []tileRow, availH int) int {
	headerLines, tileRows := 0, 0
	for _, r := range rows {
		if r.isHeader() {
			headerLines += teamHeaderRows
		} else {
			tileRows++
		}
	}
	if tileRows == 0 {
		return minTileHeight
	}
	if headerLines+tileRows*minTileHeight <= availH {
		return max((availH-headerLines)/tileRows, minTileHeight)
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
		// No selection (a fresh board) or the cursor's agent left: seed at the first
		// tile row, never a header - tileNavSection's own recovery. Row 0 can be a
		// header (a team-only fleet), and seeding there collapsed the window to [0,0)
		// via the dangling-header cleanup at a frame too short for the header + a cell.
		cursorRow = adjacentTileRow(l.rows, -1, +1)
		if cursorRow < 0 {
			return 0, 0 // no tiles at all (a header-only wall)
		}
	}
	// The cursor's tile row is always in the window - even alone taller than availH
	// (a frame too short for one cell, which View then clips). Grow up first so the
	// cursor's own section header pages into view with it rather than the next
	// team's - the row view's boardRowFrom, the cursor riding the bottom edge -
	// then fill down.
	used := l.rowHeight(cursorRow)
	from, to = cursorRow, cursorRow+1
	for from > 0 && used+l.rowHeight(from-1) <= availH {
		used += l.rowHeight(from - 1)
		from--
	}
	for to < len(l.rows) && used+l.rowHeight(to) <= availH {
		used += l.rowHeight(to)
		to++
	}
	// A header must not be the last row with no member below it - the row view's
	// atomic rule (boardView breaks before a dangling header). to-1 is never the
	// cursor row, which is a tile, so this never drops the cursor.
	if l.rows[to-1].isHeader() {
		to--
	}
	return from, to
}

// ceilDiv is the number of rows of cols columns that hold n items (>= 1 col).
func ceilDiv(n, cols int) int {
	if cols < 1 {
		cols = 1
	}
	return (n + cols - 1) / cols
}

// tileRowCount is the tile rows the sections wrap into at cols - each team's
// members over cols columns, summed. Headers are counted separately (one line
// each); this counts only the cellH-tall rows.
func tileRowCount(sections []Section, cols int) int {
	rows := 0
	for _, s := range sections {
		rows += ceilDiv(len(s.Agents), cols)
	}
	return rows
}

// boardTileCols is the near-square column count widened until the sectioned tile
// rows fit the frame height - the flat grid's short-frame flatten (add a column
// to drop a row), made section-aware and capped at what the width allows. The
// header lines come off availH first; at least one tile row always fits, so a
// frame too short for a full cell still lays everyone out on one row (View clips)
// rather than paging tiles that could have shared a wider row.
func boardTileCols(sections []Section, n, width, availH int) int {
	maxCols := max((width+tileGap)/(minTileWidth+tileGap), 1)
	headerLines := 0
	for _, s := range sections {
		if s.Team != "" {
			headerLines += teamHeaderRows
		}
	}
	maxRows := max((availH-headerLines)/minTileHeight, 1)
	cols := clamp(ceilSqrt(n), 1, maxCols)
	for cols < maxCols && tileRowCount(sections, cols) > maxRows {
		cols++
	}
	return cols
}

// boardTileLayout is the frame's section-aware tile geometry: cols from the width
// and the frame height (near-square, widened to fit like the flat grid), cellW to
// share, the shelves from the sectioned fleet, cellH to fill, and the window
// around the cursor.
func (a App) boardTileLayout(agents []Agent) tileLayout {
	availH := max(a.paneHeight()-boardChromeRows-1, 1)
	sections := a.fleet.sections(agents)
	cols := boardTileCols(sections, len(agents), a.layout.Width, availH)
	l := tileLayout{
		cols:   cols,
		cellW:  tileCellWidth(a.layout.Width, cols),
		availH: availH,
		rows:   tileShelves(sections, cols),
	}
	l.cellH = tileCellHeight(l.rows, availH)
	cursorRow, _ := tileFind(l.rows, a.board.Selected)
	l.from, l.to = tileWindow(l, cursorRow, availH)
	return l
}

// tileHit is boardHit's tiled branch: the agent index a click at (x, y) lands on,
// or -1 for the title row, a header band, or past the last column. It walks the
// windowed shelves counting each band's height - header (one line) or tile row
// (cellH) - the one number the draw counts too, so a click and a tile cannot
// disagree across a section break.
func (l tileLayout) tileHit(x, y int, agents []Agent) int {
	line := y - boardChromeRows
	if line < 0 {
		return -1 // the title row
	}
	off := 0
	for ri := l.from; ri < l.to; ri++ {
		h := l.rowHeight(ri)
		if line < off+h {
			if l.rows[ri].isHeader() {
				return -1 // a header belongs to no agent
			}
			col := x / (l.cellW + tileGap)
			if col < 0 || col >= len(l.rows[ri].tiles) {
				return -1
			}
			return indexOf(agents, l.rows[ri].tiles[col].ID)
		}
		off += h
	}
	return -1
}
