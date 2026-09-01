package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A tile is a full four-sided box: every body row begins and ends with the
// rounded border's side wall, not just the top and bottom edges. The walls are
// what titledBox's inner render draws once the tile passes a bordered style;
// before this, TextStyle carried no border and the body rows were open on both
// sides - a lid and a floor with no walls.
func TestATileDrawsWallsOnBothSides(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	ag, _ := a.fleet.ByName("robin") // idle, so no live tail wobbles the rows
	out := a.tile(ag, 40, 10, false)
	rows := strings.Split(out, "\n")
	if len(rows) < 3 {
		t.Fatalf("a tile drew %d rows, too few to have walls:\n%s", len(rows), out)
	}
	// The body rows are everything between the top and bottom border edges.
	for i, r := range rows[1 : len(rows)-1] {
		stripped := ansi.Strip(r)
		if !strings.HasPrefix(stripped, "│") || !strings.HasSuffix(stripped, "│") {
			t.Fatalf("body row %d has no wall on both sides: %q\nfull tile:\n%s", i, stripped, out)
		}
	}
}

// The tile's last body row is the per-agent status bar - the same one the DM
// pane draws (statusbar.go), reused rather than reimplemented: path, model,
// context left and the permission mode, muted grey.
func TestATileDrawsThePerAgentStatusBar(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	ag := Agent{
		ID: "sb", Name: "sb", State: rpc.StateIdle,
		Cwd: "/repos/one", Model: "claude-opus-5",
		ContextWindow: 1_000_000, ContextTokens: 910_000,
	}
	out := ansi.Strip(a.tile(ag, 64, 12, false))
	for _, want := range []string{"/repos/one", "Opus 5", "ctx:9%", "permissions: auto"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the tile's status bar did not carry %q:\n%s", want, out)
		}
	}
	// It is the last body row: the row directly above the bottom border edge.
	rows := strings.Split(out, "\n")
	lastBody := rows[len(rows)-2]
	if !strings.Contains(lastBody, "permissions: auto") {
		t.Fatalf("the status bar is not the tile's last body row, found %q:\n%s", lastBody, out)
	}
}

// A working agent's live tail fills the whole middle of a tall tile rather than
// stopping at a fixed cap - the fill-the-window change (guardrail 2). A tall
// cell shows far more than the DM preview's three rows.
func TestATileTailFillsTheBodyOfATallCell(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	working, _ := a.fleet.ByName("alex") // the working agent
	a = a.foldTail(working.ID, partialEv(strings.Repeat("streamed line of output ", 200)))

	const w, h = 50, 20 // a tall tile: body is 18 rows, tail middle ~15
	ag, _ := a.fleet.Agent(working.ID)
	out := ansi.Strip(a.tile(ag, w, h, false))
	rows := strings.Split(out, "\n")
	tailRows := 0
	for _, r := range rows {
		if strings.Contains(r, "streamed line of output") {
			tailRows++
		}
	}
	if tailRows <= 2 {
		t.Fatalf("the live tail filled only %d rows of a %d-row tile, want it to fill the body:\n%s", tailRows, h, out)
	}
}

// The tile is exactly its cell size on both axes: every rendered row is exactly
// `width` columns and there are exactly `height` of them. A tile one row too
// tall scrolls the alt screen away every frame (the pty-harness invariant), and
// one column too wide shoves its neighbour out of the joined row.
func TestATileIsExactlyItsCellSize(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	working, _ := a.fleet.ByName("alex")
	a = a.foldTail(working.ID, partialEv(strings.Repeat("filling the cell with output ", 60)))
	ag, _ := a.fleet.Agent(working.ID)

	sizes := []struct{ w, h int }{{26, 7}, {40, 12}, {59, 24}, {30, 8}}
	for _, cursored := range []bool{false, true} {
		for _, s := range sizes {
			out := a.tile(ag, s.w, s.h, cursored)
			lines := strings.Split(out, "\n")
			if len(lines) != s.h {
				t.Fatalf("tile %dx%d cursored=%v drew %d rows, want %d:\n%s", s.w, s.h, cursored, len(lines), s.h, out)
			}
			for i, l := range lines {
				if got := ansi.StringWidth(l); got != s.w {
					t.Fatalf("tile %dx%d cursored=%v row %d is %d cols, want %d:\n%s", s.w, s.h, cursored, i, got, s.w, out)
				}
			}
		}
	}
}
