package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// tiledBoardApp is an App with the tiled board up and one working agent on the
// roster, sized to a real frame so the grid is non-degenerate.
func tiledBoardApp() App {
	a := App{board: Board{Up: true, Tiled: true}, fleet: NewFleet(),
		dms: map[string]*DM{}, askedHistory: map[string]int{}}
	a.layout = Layout{Width: 100, Height: 40}
	a.fleet = a.fleet.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "luca", State: rpc.StateWorking, Cwd: "/tmp/x"},
	}})
	return a
}

func TestEnsureBuildsABoardDMForAVisibleTile(t *testing.T) {
	a := tiledBoardApp().ensureBoardDMs()
	if _, ok := a.boardDMs["s1"]; !ok {
		t.Fatal("ensureBoardDMs built no board DM for the visible agent")
	}
	if _, asked := a.boardHistoryAsked["s1"]; !asked {
		t.Error("ensureBoardDMs did not queue a history ask for the new board DM")
	}
}

func TestFoldBoardAppendsToAVisibleTilesDM(t *testing.T) {
	a := tiledBoardApp().ensureBoardDMs()
	a = a.foldBoard("s1", core.Event{Kind: core.KindAssistantText, Text: "streamed prose"})
	_, body := a.boardDMs["s1"].transcriptWindow(40, 6)
	if !strings.Contains(body, "streamed prose") {
		t.Errorf("foldBoard did not append the block to the board DM:\n%s", body)
	}
}

func TestFoldBoardIgnoresAPagedOffAgent(t *testing.T) {
	a := tiledBoardApp() // no ensure: s1 has no board DM
	a = a.foldBoard("s1", core.Event{Kind: core.KindAssistantText, Text: "prose"})
	if _, ok := a.boardDMs["s1"]; ok {
		t.Error("foldBoard built a board DM for an agent with no visible tile")
	}
}

func TestFoldBoardIsANoOpWhenTheBoardIsDownOrInRows(t *testing.T) {
	for _, tc := range []struct {
		name      string
		up, tiled bool
	}{
		{"down", false, false},
		{"rows", true, false},
	} {
		a := tiledBoardApp()
		a.board = Board{Up: tc.up, Tiled: tc.tiled}
		a = a.ensureBoardDMs().foldBoard("s1", core.Event{Kind: core.KindAssistantText, Text: "x"})
		if len(a.boardDMs) != 0 {
			t.Errorf("%s: board DMs accumulated off the tiled board", tc.name)
		}
	}
}

func TestClosingTheBoardDropsTheBoardDMs(t *testing.T) {
	a := tiledBoardApp().ensureBoardDMs()
	a = a.closeBoard()
	if len(a.boardDMs) != 0 || len(a.boardHistoryAsked) != 0 {
		t.Errorf("close left %d board DMs and %d asks, want 0/0", len(a.boardDMs), len(a.boardHistoryAsked))
	}
}

// Only the tiles on screen render a transcript - the cost bound that keeps the
// board "cheap to leave open" at fleet scale. A fleet wider than one screen of
// tiles pages the rest, and a paged-off agent gets no board DM, so neither its
// seed nor its live blocks run through renderTranscript (the whole-transcript
// glamour pass). The seam is the same var TestTranscriptWindowReWrapsOnlyOnWidthChange
// swaps.
func TestOnlyVisibleTilesRenderATranscript(t *testing.T) {
	rendered := map[string]int{}
	restore := renderTranscript
	renderTranscript = func(d DM) []block { rendered[d.SessionID]++; return restore(d) }
	defer func() { renderTranscript = restore }()

	a := App{board: Board{Up: true, Tiled: true}, fleet: NewFleet(), dms: map[string]*DM{}}
	a.layout = Layout{Width: 30, Height: 18} // a narrow, short frame: one column, few rows
	var sessions []rpc.SessionStatus
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		sessions = append(sessions, rpc.SessionStatus{ID: id, Name: id, State: rpc.StateWorking, Cwd: "/tmp"})
	}
	a.fleet = a.fleet.WithStatus(&rpc.Status{Sessions: sessions})

	visible := a.visibleBoardAgents()
	if len(visible) == 0 || len(visible) >= len(sessions) {
		t.Fatalf("precondition: want a paged grid (0 < visible < %d), got %d visible", len(sessions), len(visible))
	}
	a = a.ensureBoardDMs()
	for _, s := range sessions {
		a = a.foldBoard(s.ID, assistantBlock("x"))
	}
	if len(rendered) != len(visible) {
		t.Errorf("rendered %d transcripts, want only the %d visible tiles: %v", len(rendered), len(visible), rendered)
	}
}

// A disk-history reply for a board tile with no pane DM folds into the board DM,
// through the same routing historyArrived took (the reply is not a pane's).
func TestBoardHistoryReplyFoldsIntoTheBoardDM(t *testing.T) {
	a := tiledBoardApp().ensureBoardDMs()
	a = a.historyArrived(rpc.Frame{SessionID: "s1", Events: []core.Event{
		{Kind: core.KindAssistantText, Text: "from disk"},
	}})
	_, body := a.boardDMs["s1"].transcriptWindow(40, 6)
	if !strings.Contains(body, "from disk") {
		t.Errorf("board history reply did not fold into the board DM:\n%s", body)
	}
}
