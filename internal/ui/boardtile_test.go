package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// tileGridFor chooses a near-square grid that fills the whole frame: a few
// agents get big cells stretched across both axes, many agents pack down to
// the minimum-size cells that fit and page the rest. One function, so the
// draw, the mouse and the cursor all measure the same grid.
func TestTileGridFillsTheWindow(t *testing.T) {
	cases := []struct {
		name                string
		width, availH, n    int
		cols, rows          int
		cellW, cellH        int
	}{
		// One agent fills the whole board.
		{"one agent, whole frame", 120, 24, 1, 1, 1, 120, 24},
		// Four agents on a roomy frame: a 2x2 of big cells, each half the
		// width and half the height.
		{"four agents, 2x2", 120, 24, 4, 2, 2, 59, 12},
		// Nine agents: a 3x3, still one screen, cells shrink but stay big.
		{"nine agents, 3x3", 120, 24, 9, 3, 3, 39, 8},
		// More agents than fit at the minimum cell size: the grid caps at the
		// max that fits (4x3=12 here) and the rest page.
		{"twenty agents, capped grid", 120, 24, 20, 4, 3, 29, 8},
		// A short wide frame forces a single flat row even for four agents -
		// only one row of tiles fits vertically.
		{"short frame forces one row", 200, 6, 4, 4, 1, 49, 6},
		// A taller frame stretches the cells: same agents, double the height,
		// double the cell height.
		{"taller frame, taller cells", 120, 48, 4, 2, 2, 59, 24},
		// No agents: a safe single cell rather than a divide-by-zero.
		{"no agents", 120, 24, 0, 1, 1, 120, 24},
	}
	for _, tc := range cases {
		g := tileGridFor(tc.width, tc.availH, tc.n)
		if g.cols != tc.cols || g.rows != tc.rows || g.cellW != tc.cellW || g.cellH != tc.cellH {
			t.Errorf("%s: tileGridFor(%d,%d,%d) = {cols:%d rows:%d cellW:%d cellH:%d}, want {cols:%d rows:%d cellW:%d cellH:%d}",
				tc.name, tc.width, tc.availH, tc.n,
				g.cols, g.rows, g.cellW, g.cellH, tc.cols, tc.rows, tc.cellW, tc.cellH)
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

// The tiled board draws every agent boardApp seats - alex, sydney and robin,
// per the fixture's real names (not the brief's placeholders) - each in its
// own rounded box.
func TestTiledBoardDrawsARoundedTilePerAgent(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	out := a.View()
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╰") {
		t.Fatalf("the tiled board drew no rounded borders.\n%s", out)
	}
	for _, name := range []string{"alex", "sydney", "robin"} {
		if !strings.Contains(out, name) {
			t.Fatalf("no tile for @%s.\n%s", name, out)
		}
	}
}

// A working agent's tile shows the live tail rather than its last line. alex
// is the working one in boardApp's fixture - OnRoster()[0] is sydney, sorted
// first because attention order puts blocked ahead of working, so the agent
// under test is looked up by name rather than assumed to be the first row.
func TestTileShowsTheLiveTailOfAWorkingAgent(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	working, ok := a.fleet.ByName("alex")
	if !ok || working.State != rpc.StateWorking {
		t.Fatal("precondition: boardApp does not seat alex as the working agent")
	}
	a = a.foldTail(working.ID, partialEv("wiring the auth guard"))
	if !strings.Contains(a.View(), "wiring the auth guard") {
		t.Fatalf("the working tile did not show its live tail.\n%s", a.View())
	}
}

// boardApp gives alex one running subagent before the board opens (see
// boardApp); the tile states that count rather than a zero the fixture
// cannot back up.
func TestTileShowsSubagentCount(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	if !strings.Contains(a.View(), "⤷ 1 subagent") {
		t.Fatalf("the tile did not state alex's subagent count.\n%s", a.View())
	}
}

// A working tile's live tail can wrap to more rows than its cell has. The tile
// must still draw exactly its cell height - titledBox never constrains height,
// so an overshoot here grows the whole tile row and shifts every row below it,
// breaking the grid's fixed-height invariant boardHit's click math depends on.
// Measured on a.tile directly, since tiles sit side by side in a row
// (lipgloss.JoinHorizontal) and a single tile's row count is not separable from
// the joined row once assembled into the whole board.
func TestATileNeverOvershootsItsCellHeightWhenTheTailWraps(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	working, ok := a.fleet.ByName("alex")
	if !ok || working.State != rpc.StateWorking {
		t.Fatal("precondition: boardApp does not seat alex as the working agent")
	}
	g := a.boardTileGrid(len(a.fleet.OnRoster()))
	inner := max(g.cellW-boxFrameWidth, 1)

	// A tail long enough to wrap to many rows and fill the cell's tail budget.
	a = a.foldTail(working.ID, partialEv(strings.Repeat("wiring the auth guard ", 80)))
	if got := strings.Count(a.tails[working.ID].sized(inner).view, "\n") + 1; got < 2 {
		t.Fatalf("precondition: tail wrapped to %d row(s), want multiple rows to exercise the fill/truncate path", got)
	}
	_ = a.View() // exercises the tiled render path end to end

	ag, _ := a.fleet.Agent(working.ID)
	out := a.tile(ag, g.cellW, g.cellH, false)
	if got := strings.Count(out, "\n") + 1; got != g.cellH {
		t.Fatalf("the tile drew %d rows with an overflowing tail, want exactly the cell height %d:\n%s", got, g.cellH, out)
	}
}

// The tiled wall relaxes the DM preview's three-row cap (maxPreviewRows): a big
// cell retains up to maxTileTailRows of live tail so it fills with output
// rather than stopping at three rows - the board's narrowed guardrail 2,
// bounded and with no scrollback. The DM preview and the inbox fold keep the
// three-row cap; only the tile tail is raised.
func TestABigTileFillsWithMoreTailThanTheDMPreviewCap(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	working, ok := a.fleet.ByName("alex")
	if !ok || working.State != rpc.StateWorking {
		t.Fatal("precondition: boardApp does not seat alex as the working agent")
	}
	g := a.boardTileGrid(len(a.fleet.OnRoster()))
	inner := max(g.cellW-boxFrameWidth, 1)

	// Many wrapped rows of output, well past the three-row DM cap.
	a = a.foldTail(working.ID, partialEv(strings.Repeat("streamed line of output ", 80)))
	rows := strings.Count(a.tails[working.ID].sized(inner).view, "\n") + 1
	if rows <= maxPreviewRows {
		t.Fatalf("the tile tail retained %d rows, want more than the DM cap of %d", rows, maxPreviewRows)
	}
	if rows > maxTileTailRows {
		t.Fatalf("the tile tail retained %d rows, past its own cap of %d", rows, maxTileTailRows)
	}
}

// At a narrow single-column width, a tile's inner width falls below
// minBlockWidth (20) - the floor partial.wrapped() itself wraps at
// (max(p.width, minBlockWidth)) - so a tail line can come back wider than the
// tile it must fit in. titledBox's Width(edge) then word-wraps that
// over-width line, growing the tile past its cell height and misaligning
// boardHit's click math. tailLines must truncate each physical line to the
// tile's own inner width regardless of what floor the tail wrapped at.
func TestATileNeverOvershootsAtNarrowWidthBelowTheWrapFloor(t *testing.T) {
	a := boardApp(t)
	a = a.withSize(20, 30).applyGeometry() // single column: cellW=20, inner=18 < minBlockWidth(20)
	a.board.Tiled = true
	working, ok := a.fleet.ByName("alex")
	if !ok || working.State != rpc.StateWorking {
		t.Fatal("precondition: boardApp does not seat alex as the working agent")
	}
	g := a.boardTileGrid(len(a.fleet.OnRoster()))
	inner := max(g.cellW-boxFrameWidth, 1)
	if inner >= minBlockWidth {
		t.Fatalf("precondition: inner=%d is not below minBlockWidth(%d)", inner, minBlockWidth)
	}

	// An unbroken run of characters, so the tail wraps at the wrap floor
	// (minBlockWidth=20) with no word boundary to fall short of it - every
	// physical line comes back exactly 20 columns wide, wider than inner(18).
	a = a.foldTail(working.ID, partialEv(strings.Repeat("x", 400)))
	ag, _ := a.fleet.Agent(working.ID)
	out := a.tile(ag, g.cellW, g.cellH, false)
	if got := strings.Count(out, "\n") + 1; got != g.cellH {
		t.Fatalf("the tile drew %d rows at a narrow width, want exactly the cell height %d:\n%s", got, g.cellH, out)
	}
}

// The subagent-count line ("⤷ 1 subagent") is appended to the tile body
// without truncation, unlike the tail and boardDetail lines beside it.
// titledBox's Width(edge) word-wraps it into a second physical row the
// moment inner drops below that line's own display width - a row padRows
// (element-count based) cannot claw back, overshooting the cell height. The
// narrow-width test (width 20, inner 18) never reaches that floor; this one
// picks a width that does, at 13 (inner 11).
func TestATileNeverOvershootsWhenTheSubagentLineWraps(t *testing.T) {
	a := boardApp(t)
	a = a.withSize(13, 30).applyGeometry() // single column: cellW=13, inner=11
	a.board.Tiled = true
	working, ok := a.fleet.ByName("alex")
	if !ok || working.State != rpc.StateWorking {
		t.Fatal("precondition: boardApp does not seat alex as the working agent")
	}
	g := a.boardTileGrid(len(a.fleet.OnRoster()))
	inner := max(g.cellW-boxFrameWidth, 1)

	ag, _ := a.fleet.Agent(working.ID)
	subs := len(a.fleet.RunningTasks(ag.ID))
	if subs != 1 {
		t.Fatalf("precondition: alex must have exactly one running subagent, got %d", subs)
	}
	line := fmt.Sprintf("⤷ %d subagent", subs)
	if ansi.StringWidth(line) <= inner {
		t.Fatalf("precondition: %q (width %d) must be wider than inner=%d to reach the wrap point", line, ansi.StringWidth(line), inner)
	}

	out := a.tile(ag, g.cellW, g.cellH, false)
	if got := strings.Count(out, "\n") + 1; got != g.cellH {
		t.Fatalf("the tile drew %d rows at inner=%d with the subagent line present, want exactly the cell height %d:\n%s", got, inner, g.cellH, out)
	}
}

// At a small cell the tile body has room for the state line, one middle line
// and the subagent count - no more. The subagent count, appended last, must
// survive: it used to be the row padRows dropped whenever a detail line was
// present, because the detail line ignored the row budget the tail respected.
func TestATinyTileKeepsTheSubagentCount(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	// A working agent with a last line has a non-empty detail - the case that
	// used to push the subagent count out of a two-row body.
	ag := Agent{ID: "solo", Name: "solo", State: rpc.StateWorking, LastLine: "compiling the parser"}
	if boardDetail(ag) == "" {
		t.Fatal("precondition: this agent has no detail line to crowd the body")
	}

	out := a.tileBody(ag, 40, 2) // a two-row body: state, then one framing line
	if !strings.Contains(out, "subagent") {
		t.Fatalf("a two-row tile body dropped the subagent count:\n%s", out)
	}
	if got := strings.Count(out, "\n") + 1; got != 2 {
		t.Fatalf("the tile body drew %d rows, want exactly 2:\n%s", got, out)
	}
}

// A control byte in the live tail must not reach the drawn tile: tiles sit
// side by side (lipgloss.JoinHorizontal), so a raw CR or an escape sequence
// in one agent's streamed text could redraw or forge a neighbouring tile -
// boardRow's oneLine rule, one surface over. The check is for the specific
// injected bytes rather than "\x1b" broadly, since lipgloss's own styling
// legitimately emits unrelated SGR escapes for every tile's border and text.
func TestTileTailControlBytesCannotForgeANeighbouringTile(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	working, ok := a.fleet.ByName("alex")
	if !ok || working.State != rpc.StateWorking {
		t.Fatal("precondition: boardApp does not seat alex as the working agent")
	}
	a = a.foldTail(working.ID, partialEv("done\rFORGED\x1b[2Jred"))
	out := a.View()
	if strings.Contains(out, "\r") {
		t.Errorf("a raw carriage return from the live tail reached the tiled board:\n%q", out)
	}
	if strings.Contains(out, "\x1b[2J") {
		t.Errorf("a raw clear-screen escape from the live tail reached the tiled board:\n%q", out)
	}
}

// boardHit in tiles reads the same geometry the draw used: cols, rows, the
// cell size and the window start, so a click and a tile cannot disagree.
func TestBoardHitInTiledModeReadsTheDrawnGeometry(t *testing.T) {
	a := boardApp(t) // 3 agents, width 120, height 30 (boardApp's own fixture)
	a.board.Tiled = true
	agents := a.fleet.OnRoster()

	g := a.boardTileGrid(len(agents))
	if g.cols < 2 {
		t.Fatalf("precondition: this fixture must lay out at least two columns, got %d", g.cols)
	}

	// A click inside the second tile (row 0, col 1) lands on agents[1].
	x := g.cellW + tileGap + 1 // one column in, one cell past the gap
	y := boardChromeRows + 1   // inside the first tile row's body
	if got := a.boardHit(x, y, agents); got != 1 {
		t.Errorf("boardHit(%d,%d) = %d, want 1 (row 0, col 1)", x, y, got)
	}

	// A click on the key line, past every tile row, opens nothing.
	pastRow := boardChromeRows + g.rows*g.cellH
	if got := a.boardHit(2, pastRow, agents); got != -1 {
		t.Errorf("a click past the last tile row resolved to %d, want -1", got)
	}

	// A click past the last column opens nothing.
	pastCol := g.cols * (g.cellW + tileGap)
	if got := a.boardHit(pastCol, boardChromeRows+1, agents); got != -1 {
		t.Errorf("a click past the last column resolved to %d, want -1", got)
	}

	// A click above the title row opens nothing.
	if got := a.boardHit(2, 0, agents); got != -1 {
		t.Errorf("a click on the title row resolved to %d, want -1", got)
	}
}

// A left-press on a tile opens that agent as a new column and closes the
// board - the same placement the row view's click and ↵ now share.
func TestATileClickOpensTheAgentAsANewColumn(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	agents := a.fleet.OnRoster() // [sydney, alex, robin] in attention order

	g := a.boardTileGrid(len(agents))
	x := g.cellW + tileGap + 1 // second tile: agents[1], alex (s1)
	y := boardChromeRows + 1

	next, _ := a.boardMouse(tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y,
	})
	if next.board.Up {
		t.Fatal("a tile click left the board up over the conversation it opened")
	}
	if !next.grid.Has(agents[1].ID) {
		t.Fatalf("a click on the second tile did not open %s", agents[1].Name)
	}
	if next.grid.Cols[0].Top != "" {
		t.Fatal("the room is no longer Cols[0]; the tile click replaced it")
	}
}

// benchTiledApp is a fleet of n working agents with the tiled board up, built
// the way streamingApp (partial_bench_test.go) builds the streaming fleet -
// the same fixture, aimed at the tiled draw instead of the grid.
func benchTiledApp(b *testing.B, n int) App {
	b.Helper()
	fresh(b)
	seed := &rpc.Status{Running: true}
	for i := range n {
		seed.Sessions = append(seed.Sessions, rpc.SessionStatus{
			ID: benchAgentID(i), Name: benchAgentName(i), Dir: "/Users/someone/code/api-v2",
			State: rpc.StateWorking,
		})
	}
	a := NewRoomApp(nil, Stream{}, seed)
	a.board = Board{Up: true, Tiled: true}
	return a.withSize(idleTerminalWidth, idleTerminalHeight)
}

// BenchmarkTiledBoardFleetSecond prices a fleet-second of thirty agents
// streaming with the wall up: the fold plus a draw. It is the spec's cost
// gate - read the allocs/op, which must not scale with the token count the
// way a per-token fleet copy would (partial.go's withDM trap).
func BenchmarkTiledBoardFleetSecond(b *testing.B) {
	a := benchTiledApp(b, 30)
	const tokensPerSecond = 1300
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for t := 0; t < tokensPerSecond; t++ {
			id := a.fleet.OnRoster()[t%30].ID
			a = a.foldTail(id, partialEv("tok "))
		}
		_ = a.View()
	}
}
