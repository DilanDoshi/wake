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
		{"a", tileDown, "c"},  // skips the header to the next tile row
		{"c", tileUp, "a"},    // skips the header back up
		{"d", tileDown, "e"},  // col 1 onto a one-tile row clamps to e
		{"e", tileDown, "e"},  // last row, stays
		{"a", tileUp, "a"},    // top, stays
	} {
		if got := tileNavSection(rows, tc.from, tc.dir); got != tc.want {
			t.Errorf("nav(%q, %v) = %q, want %q", tc.from, tc.dir, got, tc.want)
		}
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
