package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

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
	if !strings.Contains(a.View(), "⤷ 1 subagents") {
		t.Fatalf("the tile did not state alex's subagent count.\n%s", a.View())
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
