//go:build unix

// Team sections on a real screen: /team groups the roster and the board under
// `──── name ────` dividers, drawn by the real binary through a pty. internal/ui
// tests the window arithmetic in process; this is the half a person sees, and
// HANDOFF's lesson is that in-process greens have hidden rendered-frame bugs.

package main

import (
	"strings"
	"testing"
)

// teamedFleet spawns three named agents beside the one bare `wake` opens, groups
// w1+w2 under backend and w3 under frontend, and leaves the room focused with the
// roster open. The first (pool-named) agent stays in the header-less top block.
func teamedFleet(t *testing.T, s *screen) {
	t.Helper()
	s.await("ready")
	s.send("\x17") // ⌃W: close the opened pane, room focused
	s.await("group chat")

	for _, n := range []string{"w1", "w2", "w3"} {
		s.send("/new " + n + "\r")
		s.await("@" + n + " started")
		s.send("\x1b") // clear the drafted @n mention before the next keystrokes
		s.settle()
	}

	// @who /team <name> is the room mention bridge. backend is created first, so
	// it is the first team section; frontend second.
	for _, a := range []struct{ name, team string }{
		{"w1", "backend"}, {"w2", "backend"}, {"w3", "frontend"},
	} {
		s.send("@" + a.name + " /team " + a.team + "\r")
		s.settle()
	}
	s.await("backend")
	s.await("frontend")
}

func TestTeamSectionsRenderInTheRosterAndBoard(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	teamedFleet(t, s)

	roster := s.text()
	for _, want := range []string{"backend", "frontend", "w1", "w2", "w3"} {
		if !strings.Contains(roster, want) {
			t.Fatalf("the roster is missing %q after teams were assigned.\n%s", want, s.dump())
		}
	}
	// Section order is creation order: backend (created first) above frontend.
	if strings.Index(roster, "backend") > strings.Index(roster, "frontend") {
		t.Errorf("frontend's section is drawn before backend's, but backend was created first.\n%s", roster)
	}
	t.Logf("SCREENSHOT roster team sections:\n%s", s.dump())

	// The board sections the same way.
	s.send("/board\r")
	s.await("BOARD")
	s.settle()
	board := s.text()
	for _, want := range []string{"backend", "frontend", "w1", "w3"} {
		if !strings.Contains(board, want) {
			t.Fatalf("the board is missing %q after teams were assigned.\n%s", want, s.dump())
		}
	}
	t.Logf("SCREENSHOT board team sections:\n%s", s.dump())
}

// The tiled board (⇥ toggles it while /board is up) wraps each team's tiles
// under its own `──── name ────` shelf rather than flowing them into a uniform
// grid. This is the half a person sees; internal/ui tests the shelf arithmetic
// in process. The rounded tile boxes (╭) prove the tile renderer drew, not the
// row list.
func TestTeamSectionsRenderInTheTiledBoard(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	teamedFleet(t, s)

	s.send("/board\r")
	s.await("BOARD")
	s.settle()
	s.send("\t") // ⇥: toggle the rows overview into the tiled wall
	s.settle()

	board := s.text()
	if !strings.Contains(board, "╭") {
		t.Fatalf("the tiled wall drew no rounded tile box (╭); still in the row view?\n%s", s.dump())
	}
	for _, want := range []string{"backend", "frontend", "w1", "w3"} {
		if !strings.Contains(board, want) {
			t.Fatalf("the tiled board is missing %q after teams were assigned.\n%s", want, s.dump())
		}
	}
	// Section order is creation order here too: backend's shelf above frontend's.
	if strings.Index(board, "backend") > strings.Index(board, "frontend") {
		t.Errorf("frontend's shelf is drawn before backend's, but backend was created first.\n%s", board)
	}
	t.Logf("SCREENSHOT tiled board team shelves:\n%s", s.dump())
}
