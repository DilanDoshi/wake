package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

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

// shelvesOf is rows of one tile each, so a row is an agent and the arithmetic
// reads without a column in it.
func shelvesOf(sections ...Section) []tileRow { return tileShelves(sections, 1) }

// A selection below the window brings it down by the least that shows its whole
// row: here 92 lines of four rows in a 46-line frame, the last row selected.
func TestTileLayoutScrollsToASelectionBelowTheWindow(t *testing.T) {
	rows := shelvesOf(Section{Agents: []Agent{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}})
	l := newTileLayout(rows, 1, 10, 46, 0, "d")
	if l.cellH != 23 || l.total != 92 {
		t.Fatalf("cellH=%d total=%d, want 23 and 92: two whole rows to the frame", l.cellH, l.total)
	}
	if l.off != 46 {
		t.Errorf("off = %d, want 46: the last two rows", l.off)
	}
	if got := idsOf(l.visibleAgents()); len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Errorf("visible = %v, want [c d]", got)
	}
	// Selected near the top with the same stored scroll: the window stays where it
	// was put, because the selection is in it.
	if l := newTileLayout(rows, 1, 10, 46, 0, "b"); l.off != 0 {
		t.Errorf("off = %d with the selection in view, want the stored 0", l.off)
	}
}

// A free scroll the wheel left half a row down keeps the clipped row, so the
// frame can end mid-tile; it is the bands the window touches that are drawn.
func TestTileLayoutDrawsTheRowsAFreeScrollTouches(t *testing.T) {
	rows := shelvesOf(Section{Agents: []Agent{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}})
	l := newTileLayout(rows, 1, 10, 46, 10, "a")
	if got := idsOf(l.visibleAgents()); len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("visible = %v at scroll 10, want [a b c]: a is clipped at the top, c at the bottom", got)
	}
	if l.off != 10 {
		t.Errorf("off = %d, want 10: a is still mostly on screen, so the window is left alone", l.off)
	}
}

// The scroll stays inside the list however far it was asked to go.
func TestTileLayoutClampsTheScroll(t *testing.T) {
	rows := shelvesOf(Section{Agents: []Agent{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}})
	for _, tc := range []struct {
		scroll, want int
		selected     string
	}{{-9, 0, "a"}, {0, 0, "a"}, {50, 46, "d"}, {500, 46, "d"}} {
		if got := newTileLayout(rows, 1, 10, 46, tc.scroll, tc.selected).off; got != tc.want {
			t.Errorf("scroll %d -> off %d, want %d", tc.scroll, got, tc.want)
		}
	}
}

// The first tile of a team brings the team's own header up with it, never the next
// team's - the bug two reviewers found when the window grew down before up.
func TestTileLayoutBringsTheSelectionsOwnTeamHeaderIntoView(t *testing.T) {
	rows := shelvesOf(
		Section{Agents: []Agent{{ID: "x"}}},
		Section{Team: "A", Agents: []Agent{{ID: "a1"}, {ID: "a2"}}},
		Section{Team: "B", Agents: []Agent{{ID: "b1"}}},
	)
	// Scrolled to the bottom with a1 selected: a1 is off the top, so the window
	// comes back for it and A's header is the first line.
	l := newTileLayout(rows, 1, 10, 46, 1000, "a1")
	if from, _ := l.shown(); !l.rows[from].isHeader() || l.rows[from].header != "A" {
		t.Fatalf("the window starts on %+v, want team A's header above the selection", l.rows[from])
	}
}

// A fresh team-only tiled board - no selection, and every agent on a team, so the
// first row is a header - still shows a tile in a frame too short for the header
// band and a full cell, never an empty window.
func TestTileLayoutTeamOnlyFreshBoardShowsATile(t *testing.T) {
	rows := shelvesOf(Section{Team: "backend", Agents: []Agent{{ID: "w1"}}})
	l := newTileLayout(rows, 1, 10, 8, 0, "")
	if got := idsOf(l.visibleAgents()); len(got) != 1 || got[0] != "w1" {
		t.Fatalf("visible = %v, want [w1]: a team-only board with no selection drew no agent at a frame of 8", got)
	}
	// The header band is spent from the frame too, so the tile is not clipped by it.
	if got := l.bands[l.cur].overlap(l.off, l.view); got != l.cellH {
		t.Errorf("the tile shows %d of its %d lines under the header band", got, l.cellH)
	}
}

// A frame shorter than the floor gets one tile as tall as itself, and the
// selection's row still shows - never an empty window.
func TestTileLayoutAShortFrameStillShowsTheSelection(t *testing.T) {
	rows := shelvesOf(Section{Agents: []Agent{{ID: "a"}, {ID: "b"}}})
	l := newTileLayout(rows, 1, 10, 6, 0, "b")
	if l.cellH != 6 {
		t.Fatalf("cellH = %d, want the frame's 6", l.cellH)
	}
	if got := idsOf(l.visibleAgents()); len(got) != 1 || got[0] != "b" {
		t.Errorf("visible = %v, want [b]", got)
	}
}

// A click at an offset resolves against the lines the draw drew there, a header
// band and the title row opening nothing.
func TestTileHitWalksTheScrolledWindow(t *testing.T) {
	agents := []Agent{{ID: "x"}, {ID: "a"}, {ID: "b"}}
	rows := shelvesOf(Section{Agents: agents[:1]}, Section{Team: "A", Agents: agents[1:]})
	l := newTileLayout(rows, 1, 10, 46, 0, "x")
	// 46 lines: x (22), A's header (2), a (22); b is below the window.
	for _, tc := range []struct {
		y, want int
	}{
		{0, -1},                     // the title row
		{boardChromeRows, 0},        // x's top edge
		{boardChromeRows + 21, 0},   // x's last line
		{boardChromeRows + 22, -1},  // A's blank
		{boardChromeRows + 23, -1},  // A's divider
		{boardChromeRows + 24, 1},   // a's top edge
		{boardChromeRows + 45, 1},   // a's last visible line
		{boardChromeRows + 46, -1},  // the key line, below the window
		{boardChromeRows + 100, -1}, // far below
	} {
		if got := l.tileHit(2, tc.y, agents); got != tc.want {
			t.Errorf("hit at y=%d = %d, want %d", tc.y, got, tc.want)
		}
	}
	// Scrolled to the bottom the same pointer rows land on later agents.
	s := newTileLayout(rows, 1, 10, 46, 1000, "b")
	if got := s.tileHit(2, boardChromeRows, agents); got != 1 && got != -1 {
		t.Errorf("hit at the top of the scrolled window = %d, want a or A's header band", got)
	}
	if got := s.tileHit(2, boardChromeRows+45, agents); got != 2 {
		t.Errorf("hit at the bottom of the scrolled window = %d, want b", got)
	}
}

// Three across at most, fewer for a fleet that small or a terminal that narrow:
// the table the layout's breakpoints are held to.
func TestBoardTileColsTable(t *testing.T) {
	for _, tc := range []struct{ n, width, want int }{
		{0, 200, 1}, {1, 200, 1}, {2, 200, 2}, {3, 200, 3}, {9, 200, 3}, {64, 400, 3},
		{9, 80, 3}, {9, 79, 2}, {9, 53, 2}, {9, 52, 1}, {9, 10, 1},
	} {
		if got := boardTileCols(tc.n, tc.width); got != tc.want {
			t.Errorf("boardTileCols(%d, %d) = %d, want %d", tc.n, tc.width, got, tc.want)
		}
	}
}

// The cell height: fills the frame when the fleet fits at the floor, keeps the
// floor in whole rows when it does not, and never exceeds the frame.
func TestTileCellHeightTable(t *testing.T) {
	flat := func(rows int) []tileRow {
		out := make([]tileRow, rows)
		for i := range out {
			out[i] = tileRow{tiles: []Agent{{ID: "x"}}}
		}
		return out
	}
	withHeader := append([]tileRow{{tiles: []Agent{{ID: "x"}}}, {header: "A"}}, flat(2)...)
	for _, tc := range []struct {
		name         string
		rows         []tileRow
		availH, want int
	}{
		{"one row fills a tall frame", flat(1), 86, 86},
		{"two rows share the frame", flat(2), 46, 23},
		{"a fleet that fits stretches to fill", flat(3), 86, 28},
		{"a bigger fleet keeps whole rows", flat(10), 46, 23},
		{"a frame that holds three floors", flat(10), 62, 20},
		{"a frame between floors stretches the whole rows", flat(10), 59, 29},
		{"a frame shorter than the floor is one tile", flat(10), 12, 12},
		{"a header band is reserved when the fleet overflows", withHeader, 46, 22},
		{"no rows is the floor", nil, 46, 20},
		{"a header at the top shares a short frame", append([]tileRow{{header: "A"}}, flat(1)...), 18, 16},
	} {
		if got := tileCellHeight(tc.rows, tc.availH); got != tc.want {
			t.Errorf("%s: tileCellHeight(availH=%d) = %d, want %d", tc.name, tc.availH, got, tc.want)
		}
	}
}

// The tiled board draws the header band's blank above each `──── team ────`
// divider too, so a team's tiles read as their own chunk (owner's 2026-09-21
// request), the same gap the rows view draws.
func TestTiledBoardDrawsABlankAboveEachTeamHeader(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40)
	a = a.applyStatus(&rpc.Status{
		Teams: []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		},
	})
	a.board = Board{Up: true, Tiled: true}
	agents := a.boardAgents()

	lines := strings.Split(a.tileView(agents, 120), "\n")
	found := false
	for i, ln := range lines {
		if strings.Contains(ln, "backend") {
			found = true
			if i == 0 || strings.TrimSpace(lines[i-1]) != "" {
				t.Errorf("no blank line above the tiled `backend` header (line %d):\n%s", i, strings.Join(lines, "\n"))
			}
		}
	}
	if !found {
		t.Fatalf("the tiled board drew no `backend` team header:\n%s", strings.Join(lines, "\n"))
	}
}
