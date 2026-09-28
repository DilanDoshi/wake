package ui

import (
	"slices"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func typesOf(t *testing.T, f Fleet, id string) []string {
	t.Helper()
	a, ok := f.Agent(id)
	if !ok {
		t.Fatalf("no agent %s", id)
	}
	return a.SubagentTypes()
}

// A report is the only route to a session's subagent types for a client that
// attached after the init, so WithStatus folds them.
func TestAReportFoldsTheSubagentTypes(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Agents: []string{"Explore", "Plan"}}}})
	if got := typesOf(t, f, "s1"); !slices.Equal(got, []string{"Explore", "Plan"}) {
		t.Errorf("SubagentTypes = %v after a report naming two, want both", got)
	}
}

// The live init frame folds them too, for a client that watched it arrive.
func TestAnInitFrameFoldsTheSubagentTypes(t *testing.T) {
	f, _ := NewFleet().Observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Agents: []string{"Explore"}}}, "s1")
	if got := typesOf(t, f, "s1"); !slices.Equal(got, []string{"Explore"}) {
		t.Errorf("SubagentTypes = %v after an init naming one, want it", got)
	}
}

// A later report replaces the set wholesale; one naming none keeps it.
func TestALaterReportReplacesTheSubagentTypes(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Agents: []string{"Explore"}}}})
	f = f.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Agents: []string{"Plan"}}}})
	if got := typesOf(t, f, "s1"); !slices.Equal(got, []string{"Plan"}) {
		t.Errorf("SubagentTypes = %v after a second report, want [Plan]", got)
	}
	f = f.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1"}}})
	if got := typesOf(t, f, "s1"); !slices.Equal(got, []string{"Plan"}) {
		t.Errorf("SubagentTypes = %v after a report naming none, want [Plan] kept", got)
	}
}

// The reader hands out a copy, so a caller cannot rewrite what every copy of
// the agent shares behind its pointer; the fold copies its input likewise.
func TestSubagentTypesAreCopiedOnTheWayInAndOut(t *testing.T) {
	in := []string{"Explore", "Plan"}
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", Agents: in}}})
	in[0] = "mutated-in"
	a, _ := f.Agent("s1")
	a.SubagentTypes()[1] = "mutated-out"
	if got := a.SubagentTypes(); !slices.Equal(got, []string{"Explore", "Plan"}) {
		t.Errorf("SubagentTypes = %v after a caller mutated its input and the reader's result", got)
	}
}

// Re-advertising the same types each turn must not make the agent unequal to
// itself, or Observe copies the fleet once per init.
func TestReadvertisedSubagentTypesLeaveTheAgentComparableAndEqual(t *testing.T) {
	a := Agent{}.withAgents([]string{"Explore"})
	b := a.withAgents([]string{"Explore"})
	if a != b {
		t.Error("an unchanged advertisement made the Agent unequal to itself")
	}
	if a == a.withAgents([]string{"Plan"}) {
		t.Error("a changed advertisement left the Agent equal")
	}
}
