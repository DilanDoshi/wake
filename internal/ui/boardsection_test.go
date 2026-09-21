package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The board's rows overview sections the same way the roster does: a team header
// before each section, drawn in the daemon's order, and a click on a header row
// opens nothing while the row below it opens the member - the board's own
// boardChromeRows/one-height rule, made header-aware.
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

	if out := a.boardView(agents, 120); !strings.Contains(out, "backend") {
		t.Errorf("the board drew no `backend` team header:\n%s", out)
	}

	// y=0 title, y=1 alex (top block), y=2 the `backend` header, y=3 thea.
	if _, _, ok := a.boardHit(0, 2, agents); ok {
		t.Error("a click on the board's team header row opened a row; a header belongs to no agent")
	}
	if i, _, ok := a.boardHit(0, 1, agents); !ok || agents[i].ID != "s1" {
		t.Errorf("board y=1 = (%d, %v), want alex (s1) in the top block", i, ok)
	}
	if i, _, ok := a.boardHit(0, 3, agents); !ok || agents[i].ID != "s2" {
		t.Errorf("board y=3 = (%d, %v), want thea (s2) under the backend header", i, ok)
	}
}
