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

	// Tall enough for all three shelves at the tile floor (a row is 20 lines, a
	// header 2); a shorter frame scrolls to them, which boardscrollscreen covers.
	s := startWakeInAConversation(t, 100, 72)
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

// A team is set off from whatever is above it by a blank row in the roster, on a
// real screen: the top block's last agent, a blank, then `──── backend ────`, and
// the same between backend's last row and `──── frontend ────`.
func TestTeamSectionsAreSetOffByABlankRowInTheRoster(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	teamedFleet(t, s)
	s.settle()

	// The sidebar is what follows the last "│" of a row: its divider, drawn on every
	// row, and nothing the sidebar itself draws contains one.
	sidebar := func(line string) string {
		r := []rune(line)
		for i := len(r) - 1; i >= 0; i-- {
			if r[i] == '│' {
				return strings.TrimSpace(string(r[i+1:]))
			}
		}
		return ""
	}
	lines := s.lines()
	for _, team := range []string{"backend", "frontend"} {
		at := -1
		for y, line := range lines {
			if strings.Contains(sidebar(line), "─ "+team+" ─") {
				at = y
			}
		}
		if at < 2 {
			t.Fatalf("no `%s` divider in the sidebar:\n%s", team, s.dump())
		}
		if got := sidebar(lines[at-1]); got != "" {
			t.Errorf("the row above the %s divider is %q, want a blank row:\n%s", team, got, s.dump())
		}
		if got := sidebar(lines[at-2]); got == "" || strings.Contains(got, "─") {
			t.Errorf("the row above the blank is %q, want an agent's row:\n%s", got, s.dump())
		}
	}
}
