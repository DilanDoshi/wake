//go:build unix

package main

import (
	"strings"
	"testing"
)

// The tiled board draws on a real screen: ⇥ switches from rows to tiles, the
// tiles carry rounded borders, and a click on one opens that agent's
// conversation as a new column and gives the board back - boardscreen's own
// takeover-and-return check, one surface over.
func TestTheTiledBoardDrawsAndClicksOnAScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	names := agentsOnRoster(s)
	if len(names) != 1 {
		t.Fatalf("want one agent on the roster, got %v.\n%s", names, s.dump())
	}

	s.send("/board\r")
	s.await("BOARD")
	s.send("\t") // ⇥ to tiles
	s.await("╭")
	s.settle()
	if !strings.Contains(s.text(), "╰") {
		t.Fatalf("the tiled board drew no rounded borders.\n%s", s.dump())
	}

	// A click inside the first tile - row 2 (past the title row and the tile's
	// own top border), column 2 (inside the first cell) - opens that agent and
	// closes the board. The pane legend coming back is the durable fact
	// boardscreen's esc case checks too.
	s.click(2, 2)
	s.await("interrupt")
	if strings.Contains(s.text(), "BOARD") {
		t.Fatalf("the click opened a conversation but left the board drawn.\n%s", s.dump())
	}
}

// A tile is a full four-sided box on a real screen: its body rows carry a wall
// on the left and on the right, not just a top and a bottom edge. Before this
// the tile passed a borderless style and its body rows were open on both sides
// - a lid and a floor. The single-agent tile fills the frame width, so a body
// row's right wall is the last non-blank cell and survives the row's trim.
func TestATiledBoardTileIsAFullFourSidedBox(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	s.send("/board\r")
	s.await("BOARD")
	s.send("\t") // ⇥ to tiles
	s.await("╭")
	s.settle()

	walled := false
	for _, ln := range strings.Split(s.text(), "\n") {
		if !strings.HasPrefix(ln, "│") {
			continue
		}
		if !strings.HasSuffix(ln, "│") {
			t.Fatalf("a tile body row has a left wall but no right wall: %q\n%s", ln, s.dump())
		}
		walled = true
	}
	if !walled {
		t.Fatalf("the tiled board drew no body row with side walls.\n%s", s.dump())
	}
}

// The tile carries the per-agent status bar as a body row - the same bar the DM
// pane draws (path, branch, model, context, permission mode), reused rather
// than reimplemented. The path is the bar's first segment and the one it keeps
// longest under a narrow tile, so the home-glyph path is the durable screen
// assertion; the field-by-field check is the unit test at a controlled width
// (internal/ui: TestATileDrawsThePerAgentStatusBar). The tiled board draws no
// other path, so "~/" on screen is the status bar.
func TestATiledBoardTileShowsThePerAgentStatusBar(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	s.send("/board\r")
	s.await("BOARD")
	s.send("\t") // ⇥ to tiles
	s.await("╭")
	s.settle()

	if !strings.Contains(s.text(), "~/") {
		t.Fatalf("the tiled board drew no per-agent status bar (no home-glyph path).\n%s", s.dump())
	}
}

// A tile fills the frame rather than sitting at a fixed small height: with one
// agent on a tall terminal the box's top and bottom borders are far apart, so
// the wall scales with the window instead of drawing a tiny cell in a big
// screen. This is the whole point of the fill-the-window grid.
func TestATiledBoardCellFillsTheWindowHeight(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 40)
	s.await("ready")
	s.send("/board\r")
	s.await("BOARD")
	s.send("\t") // ⇥ to tiles
	s.await("╭")
	s.settle()

	lines := strings.Split(s.text(), "\n")
	top, bottom := -1, -1
	for i, ln := range lines {
		if strings.Contains(ln, "╭") {
			top = i
		}
		if strings.Contains(ln, "╰") {
			bottom = i
		}
	}
	if top < 0 || bottom < 0 {
		t.Fatalf("the tiled board drew no complete box (top=%d bottom=%d).\n%s", top, bottom, s.dump())
	}
	// The old fixed cell was seven rows tall; a filled cell in a 40-row
	// terminal is far taller, so a generous floor separates the two.
	if span := bottom - top + 1; span <= 12 {
		t.Fatalf("the single tile spanned only %d rows in a 40-row terminal; it should fill the frame.\n%s", span, s.dump())
	}
}
