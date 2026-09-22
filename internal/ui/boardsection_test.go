package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The board's rows overview sections the same way the roster does: a team header
// before each section, drawn in the daemon's order, and a click on a header row
// opens nothing while the row below it opens the member - the board's own
// boardChromeRows/one-height rule, made header-aware. The header band is a blank
// separator then the divider, so each team reads as its own chunk (owner's
// 2026-09-21 request): the click math counts both those rows as the header's.
func TestBoardRowsDrawTeamHeadersAndAHeaderClickOpensNothing(t *testing.T) {
	a := newRoomApp(t).withSize(120, 30)
	a = a.applyStatus(&rpc.Status{
		Teams: []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
			{ID: "s3", Name: "john", Team: "backend", State: rpc.StateIdle},
		},
	})
	agents := a.boardAgents()

	out := a.boardView(agents, 120)
	if !strings.Contains(out, "backend") {
		t.Errorf("the board drew no `backend` team header:\n%s", out)
	}
	// The line directly above the divider is blank - the gap that makes the team
	// its own chunk.
	lines := strings.Split(out, "\n")
	for i, ln := range lines {
		if strings.Contains(ln, "backend") {
			if i == 0 || strings.TrimSpace(lines[i-1]) != "" {
				t.Errorf("no blank line above the `backend` header (line %d):\n%s", i, out)
			}
			break
		}
	}

	// y=0 title, y=1 alex (top block), y=2 the blank gap, y=3 the `backend`
	// header, y=4 thea.
	if _, _, ok := a.boardHit(0, 2, agents); ok {
		t.Error("a click on the header band's blank gap opened a row; it belongs to no agent")
	}
	if _, _, ok := a.boardHit(0, 3, agents); ok {
		t.Error("a click on the board's team header row opened a row; a header belongs to no agent")
	}
	if i, _, ok := a.boardHit(0, 1, agents); !ok || agents[i].ID != "s1" {
		t.Errorf("board y=1 = (%d, %v), want alex (s1) in the top block", i, ok)
	}
	if i, _, ok := a.boardHit(0, 4, agents); !ok || agents[i].ID != "s2" {
		t.Errorf("board y=4 = (%d, %v), want thea (s2) under the backend header", i, ok)
	}
}

// The header band is teamHeaderRows tall: a blank separator first, the divider
// last, so the gap sits above the team name and the divider stays tight to the
// section's first agent.
func TestTeamHeaderBandIsABlankAboveTheDivider(t *testing.T) {
	band := teamHeaderBand("backend", 40)
	if len(band) != teamHeaderRows {
		t.Fatalf("teamHeaderBand drew %d rows, want teamHeaderRows=%d", len(band), teamHeaderRows)
	}
	if strings.TrimSpace(band[0]) != "" {
		t.Errorf("the header band's first row is not the blank gap: %q", band[0])
	}
	if !strings.Contains(band[len(band)-1], "backend") {
		t.Errorf("the header band's last row is not the divider: %q", band[len(band)-1])
	}
}
