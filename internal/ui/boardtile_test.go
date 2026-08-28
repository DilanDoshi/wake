package ui

import "testing"

func TestTileColumns(t *testing.T) {
	cases := []struct{ width, want int }{
		{0, 1},  // never zero columns
		{30, 1}, // one tile plus slack
		{60, 2},
		{120, 4},
	}
	for _, tc := range cases {
		if got := tileColumns(tc.width); got != tc.want {
			t.Errorf("tileColumns(%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

func TestTileNavDoesNotWrap(t *testing.T) {
	// A 2x2-ish grid over 5 agents (cols=2): indices 0..4.
	cases := []struct {
		name   string
		cursor int
		dir    tileDir
		want   int
	}{
		{"right within row", 0, tileRight, 1},
		{"right at row edge stays", 1, tileRight, 1},
		{"left at row start stays", 0, tileLeft, 0},
		{"down a row", 0, tileDown, 2},
		{"up a row", 2, tileUp, 0},
		{"up from top stays", 1, tileUp, 1},
		{"down past the end stays", 4, tileDown, 4},
		{"right onto nonexistent last stays", 4, tileRight, 4},
	}
	for _, tc := range cases {
		if got := tileNav(tc.cursor, 2, 5, tc.dir); got != tc.want {
			t.Errorf("%s: tileNav(%d,2,5,%v) = %d, want %d", tc.name, tc.cursor, tc.dir, got, tc.want)
		}
	}
}

func TestTileWindowStartRidesTheCursorRow(t *testing.T) {
	// cols=2, visibleRows=2 → 4 tiles on screen. 10 agents = 5 rows.
	cases := []struct{ cursor, want int }{
		{0, 0}, // first row: window at 0
		{2, 0}, // second row still fits
		{4, 2}, // third row: window slides down one row (start index 2)
		{9, 6}, // last agent: last two rows (start index 6)
	}
	for _, tc := range cases {
		if got := tileWindowStart(tc.cursor, 10, 2, 2); got != tc.want {
			t.Errorf("tileWindowStart(%d,10,2,2) = %d, want %d", tc.cursor, got, tc.want)
		}
	}
}
