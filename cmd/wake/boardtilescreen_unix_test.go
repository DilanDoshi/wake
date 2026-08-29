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
