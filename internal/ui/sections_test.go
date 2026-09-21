package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func idsOf(agents []Agent) []string {
	out := make([]string, 0, len(agents))
	for _, a := range agents {
		out = append(out, a.ID)
	}
	return out
}

// The header-less top block first (manager + un-tagged), then each team in the
// daemon's order, every agent covered exactly once.
func TestSectionsIsTheTopBlockThenTeamsInOrder(t *testing.T) {
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams: []string{"backend", "frontend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "thea", Team: "backend", State: rpc.StateIdle},
			{ID: "s2", Name: "delta", Team: "frontend", State: rpc.StateIdle},
			{ID: "s3", Name: "manager", State: rpc.StateIdle},
			{ID: "s4", Name: "sydney", State: rpc.StateIdle},
		},
	})
	secs := f.sections(f.Agents())
	if len(secs) != 3 {
		t.Fatalf("got %d sections, want top block + backend + frontend: %+v", len(secs), secs)
	}
	if secs[0].Team != "" {
		t.Errorf("first section is %q, want the header-less top block", secs[0].Team)
	}
	top := idsOf(secs[0].Agents)
	if len(top) != 2 || (top[0] != "s3" && top[0] != "s4") {
		t.Errorf("top block = %v, want the manager and the un-tagged agent", top)
	}
	if secs[1].Team != "backend" || len(secs[1].Agents) != 1 || secs[1].Agents[0].ID != "s1" {
		t.Errorf("section 1 = %+v, want backend with thea", secs[1])
	}
	if secs[2].Team != "frontend" || len(secs[2].Agents) != 1 || secs[2].Agents[0].ID != "s2" {
		t.Errorf("section 2 = %+v, want frontend with delta", secs[2])
	}
}

// No teams is one header-less block holding every agent - the flat roster this
// build has always drawn, so nothing renders differently until a /team is typed.
func TestSectionsWithNoTeamsIsOneFlatBlock(t *testing.T) {
	f := (Fleet{}).WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "s2", Name: "sydney", State: rpc.StateIdle},
	}})
	secs := f.sections(f.Agents())
	if len(secs) != 1 || secs[0].Team != "" || len(secs[0].Agents) != 2 {
		t.Errorf("no teams should be one flat header-less block, got %+v", secs)
	}
}

// Every agent the caller passed is covered: an agent whose team is not in the
// daemon's order (an ended row lingering after its team emptied) draws in the top
// block rather than vanishing - the class of bug that lands a click one row off.
func TestSectionsCoversAnAgentWhoseTeamIsNotOrdered(t *testing.T) {
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams: []string{"backend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "thea", Team: "backend", State: rpc.StateIdle},
			{ID: "s2", Name: "ghost", Team: "frontend", State: rpc.StateEnded},
		},
	})
	secs := f.sections(f.Agents())
	seen := map[string]bool{}
	for _, s := range secs {
		for _, a := range s.Agents {
			seen[a.ID] = true
		}
	}
	if !seen["s2"] {
		t.Error("an agent whose team is not in the daemon's order was dropped from every section")
	}
}
