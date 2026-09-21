package ui

import "testing"

// tileShelves wraps each team's tiles under its header and the top block's with
// none, so the next team starts a fresh band rather than flowing into this one's
// row - the thing the uniform grid could not do.
func TestTileShelvesWrapsEachTeamUnderItsHeader(t *testing.T) {
	rows := tileShelves([]Section{
		{Agents: []Agent{{ID: "a"}, {ID: "b"}}},
		{Team: "backend", Agents: []Agent{{ID: "c"}, {ID: "d"}, {ID: "e"}}},
	}, 2) // cols=2
	// Expect: [a,b] · header backend · [c,d] · [e]
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want 4: %+v", len(rows), rows)
	}
	if rows[0].isHeader() || len(rows[0].tiles) != 2 {
		t.Errorf("row 0 should be the header-less top block a,b, got %+v", rows[0])
	}
	if !rows[1].isHeader() || rows[1].header != "backend" {
		t.Errorf("row 1 should be the backend header, got %+v", rows[1])
	}
	if rows[2].isHeader() || len(rows[2].tiles) != 2 || rows[2].tiles[0].ID != "c" {
		t.Errorf("row 2 should be c,d, got %+v", rows[2])
	}
	if len(rows[3].tiles) != 1 || rows[3].tiles[0].ID != "e" {
		t.Errorf("row 3 should wrap e alone rather than flowing it beside the top block, got %+v", rows[3])
	}
}

// No team is one header-less top block, so the shelves are the flat wall this
// build always drew.
func TestTileShelvesWithNoTeamHasNoHeader(t *testing.T) {
	rows := tileShelves([]Section{{Agents: []Agent{{ID: "a"}, {ID: "b"}, {ID: "c"}}}}, 2)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2 (a,b then c): %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.isHeader() {
			t.Error("a header was drawn for a teamless fleet")
		}
	}
}

// The cursor moves within a shelf (left/right) and across shelves (up/down),
// skipping header rows and clamping to a shorter row's own tiles.
func TestTileNavSectionMovesWithinAndAcrossShelves(t *testing.T) {
	rows := tileShelves([]Section{
		{Agents: []Agent{{ID: "a"}, {ID: "b"}}},
		{Team: "backend", Agents: []Agent{{ID: "c"}, {ID: "d"}, {ID: "e"}}},
	}, 2) // row0 [a,b] · header · row2 [c,d] · row3 [e]
	for _, tc := range []struct {
		from string
		dir  tileDir
		want string
	}{
		{"a", tileRight, "b"},
		{"b", tileRight, "b"}, // row end, stays
		{"b", tileLeft, "a"},
		{"a", tileDown, "c"}, // skips the header to the next tile row
		{"c", tileUp, "a"},   // skips the header back up
		{"d", tileDown, "e"}, // col 1 onto a one-tile row clamps to e
		{"e", tileDown, "e"}, // last row, stays
		{"a", tileUp, "a"},   // top, stays
	} {
		if got := tileNavSection(rows, tc.from, tc.dir); got != tc.want {
			t.Errorf("nav(%q, %v) = %q, want %q", tc.from, tc.dir, got, tc.want)
		}
	}
}

// A fresh board open has Selected == "" and a departed agent leaves the cursor
// naming nothing on the wall; an arrow must still land on a tile rather than
// doing nothing - the flat grid seeded from boardCursor (empty/gone → top), and
// the section walk has to keep that or the tiled board's arrows are dead until a
// click.
func TestTileNavSectionSeedsFromAnEmptyCursor(t *testing.T) {
	rows := tileShelves([]Section{
		{Agents: []Agent{{ID: "a"}, {ID: "b"}}},
		{Team: "backend", Agents: []Agent{{ID: "c"}}},
	}, 2) // row0 [a,b] · header · row2 [c]
	for _, tc := range []struct {
		from string
		dir  tileDir
		want string
	}{
		{"", tileRight, "b"},     // seed at a, then move right
		{"", tileDown, "c"},      // seed at a, skip the header, land on c
		{"", tileUp, "a"},        // seed at a, top row, stays
		{"", tileLeft, "a"},      // seed at a, left edge, stays
		{"gone", tileRight, "b"}, // a departed agent recovers the same way
	} {
		if got := tileNavSection(rows, tc.from, tc.dir); got != tc.want {
			t.Errorf("nav(%q, %v) = %q, want %q (seed at the first tile)", tc.from, tc.dir, got, tc.want)
		}
	}
	// No tiles at all: nothing to seed, so the cursor is returned unchanged.
	if got := tileNavSection([]tileRow{{header: "backend"}}, "", tileDown); got != "" {
		t.Errorf("nav on a header-only wall = %q, want \"\" (no tile to land on)", got)
	}
}

// The window pages around the cursor's row and pulls its section header into view
// with it, counting each band by its own height (header 1, tile row cellH).
func TestTileWindowPagesAroundTheCursorRowWithItsHeader(t *testing.T) {
	l := tileLayout{cellH: 3, rows: []tileRow{
		{tiles: []Agent{{ID: "a"}}}, // 3
		{header: "t1"},              // 1
		{tiles: []Agent{{ID: "b"}}}, // 3
		{tiles: []Agent{{ID: "c"}}}, // 3
	}}
	// total 10 > availH 5: cursor in row 2 fits with its header (row 1), not row 0/3.
	from, to := tileWindow(l, 2, 5)
	if from != 1 || to != 3 {
		t.Errorf("window = [%d,%d), want [1,3): the cursor's tile row and the header above it", from, to)
	}
	// Everything fits: the whole list.
	if from, to := tileWindow(l, 0, 100); from != 0 || to != 4 {
		t.Errorf("window = [%d,%d), want the whole list [0,4) when it all fits", from, to)
	}
}

// Paging must keep the cursor tile under its OWN team header, not the next
// team's - the bug two reviewers found: growing down before up paged the cursor
// under the following team's header, misclassifying it.
func TestTileWindowKeepsTheCursorTileWithItsTeamHeader(t *testing.T) {
	l := tileLayout{cellH: 7, rows: []tileRow{
		{header: "A"},                // 0: 1
		{tiles: []Agent{{ID: "a1"}}}, // 1: 7
		{tiles: []Agent{{ID: "a2"}}}, // 2: 7 (cursor, team A's second row)
		{header: "B"},                // 3: 1
		{tiles: []Agent{{ID: "b1"}}}, // 4: 7
	}}
	// availH 15 fits A's header + both A rows (1+7+7); growing down-first would
	// have paged the cursor under team B's header instead.
	from, to := tileWindow(l, 2, 15)
	if from != 0 || to != 3 {
		t.Fatalf("window = [%d,%d), want [0,3): the cursor with team A's header, not B's", from, to)
	}
	if !l.rows[from].isHeader() || l.rows[from].header != "A" {
		t.Errorf("the window's first row is %+v, want team A's header above the cursor", l.rows[from])
	}
}

// A header must never be the last row in the window with its member paged off -
// the row view's atomic rule (rostersection.go glues header to first member).
func TestTileWindowDropsADanglingTrailingHeader(t *testing.T) {
	l := tileLayout{cellH: 7, rows: []tileRow{
		{tiles: []Agent{{ID: "x"}}}, // 0: 7 (cursor, top block)
		{header: "B"},               // 1: 1
		{tiles: []Agent{{ID: "b"}}}, // 2: 7
	}}
	// availH 8 fits the top row and B's header but not B's tile: the header would
	// dangle at the bottom edge as an empty team without the drop.
	from, to := tileWindow(l, 0, 8)
	if from != 0 || to != 1 {
		t.Fatalf("window = [%d,%d), want [0,1): team B's header must not dangle with its member paged off", from, to)
	}
}

// A frame shorter than one cell still shows the cursor's row (View clips it),
// never an empty window - the regression the down-first fill would leave when a
// single row was taller than availH.
func TestTileWindowAlwaysShowsTheCursorRowEvenTallerThanTheFrame(t *testing.T) {
	l := tileLayout{cellH: 7, rows: []tileRow{
		{tiles: []Agent{{ID: "a"}}},
		{tiles: []Agent{{ID: "b"}}},
	}}
	from, to := tileWindow(l, 0, 6)
	if from != 0 || to != 1 {
		t.Fatalf("window = [%d,%d), want [0,1): the cursor row shows even when taller than availH", from, to)
	}
}

// Columns widen to fit the frame height, the flat grid's short-frame flatten:
// nine agents in a short wide frame lay out five-wide (two rows) rather than the
// near-square three-wide (three rows) that would page a row off screen.
func TestBoardTileColsWidensToFitAShortFrame(t *testing.T) {
	top := []Section{{Agents: make([]Agent, 9)}}
	if got := boardTileCols(top, 9, 200, 15); got != 5 {
		t.Errorf("boardTileCols(short frame) = %d, want 5 (widen to fit two rows)", got)
	}
	// A tall frame keeps the near-square choice.
	if got := boardTileCols(top, 9, 200, 100); got != 3 {
		t.Errorf("boardTileCols(tall frame) = %d, want 3 (near-square)", got)
	}
	// A frame too short for even one full cell still flattens to one row so
	// everyone is on screen (View clips), rather than a 2x2 that pages.
	four := []Section{{Agents: make([]Agent, 4)}}
	if got := boardTileCols(four, 4, 200, 6); got != 4 {
		t.Errorf("boardTileCols(tiny frame) = %d, want 4 (one row, everyone on screen)", got)
	}
}
