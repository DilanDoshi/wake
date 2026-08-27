package ui

// The effort in a conversation's title, through the path that draws it.
//
// DM.standing() had a unit-testable Effort field of its own and no test that
// went through App.dmPane, so when the grid and the fidelity branch merged and
// dmPane started handing the pane a whole Agent, the field stopped being
// assigned and nothing failed. This asserts the fleet's effort reaches the
// title, which is the half that was never covered.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestThePaneTitleNamesTheEffortItsAgentRunsAt(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(120, 30)
	a.fleet = a.fleet.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle, Effort: "high"},
	}})

	if got := a.dmPane("s1", 80, 20); !strings.Contains(stripANSI(got), "high") {
		t.Errorf("the pane title does not name the effort its agent runs at:\n%s", stripANSI(got))
	}
}

// And an agent Wake chose no effort for says nothing, rather than claude's own
// default under Wake's name.
func TestThePaneTitleNamesNoEffortWhenWakeChoseNone(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(120, 30)
	a.fleet = a.fleet.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
	}})

	if got := stripANSI(a.dmPane("s1", 80, 20)); strings.Contains(got, " · ") {
		t.Errorf("the pane title says something about an agent with no effort and no standing:\n%s", got)
	}
}
