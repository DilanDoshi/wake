package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A history reply for an open pane conversation still folds into it. This is the
// characterisation guard for the routing change: historyArrived now hands a
// reply for a non-pane conversation to the board, and this proves the pane path
// it forks from is untouched.
func TestHistoryArrivedStillFoldsIntoAnOpenPaneDM(t *testing.T) {
	a := App{dms: map[string]*DM{}, askedHistory: map[string]int{}}
	d := NewDM("s1", "luca").SetSize(80, 20)
	a.dms["s1"] = &d
	a = a.askHistory("s1")
	a = a.historyArrived(rpc.Frame{SessionID: "s1", Events: []core.Event{
		{Kind: core.KindAssistantText, Text: "restored line"},
	}})
	if got := a.dms["s1"].tr.view(marked{}); !strings.Contains(got, "restored line") {
		t.Errorf("pane history fold regressed; transcript:\n%s", got)
	}
}
