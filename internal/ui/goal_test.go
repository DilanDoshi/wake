package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func goalEvent(op core.GoalOpKind, cond string) core.Event {
	return core.Event{Kind: core.KindGoal, Goal: &core.GoalOp{Op: op, Condition: cond}}
}

// A live KindGoal event folds onto Agent.Goal: a set activates with the
// condition, a progress refresh keeps it active, and a clear ends it.
func TestGoalFoldsFromTheLiveEvent(t *testing.T) {
	f, _ := NewFleet().Observe(goalEvent(core.GoalSet, "ship the PR"), "s1")
	if a, _ := f.Agent("s1"); !a.Goal().Active || a.Goal().Condition != "ship the PR" {
		t.Fatalf("after set, Goal = %+v, want active with the condition", func() GoalState { a, _ := f.Agent("s1"); return a.Goal() }())
	}

	f, _ = f.Observe(goalEvent(core.GoalProgress, "ship the PR"), "s1")
	if a, _ := f.Agent("s1"); !a.Goal().Active {
		t.Errorf("a progress refresh cleared the goal: %+v", a.Goal())
	}

	f, _ = f.Observe(goalEvent(core.GoalCleared, "ship the PR"), "s1")
	if a, _ := f.Agent("s1"); a.Goal().Active {
		t.Errorf("after clear, Goal still active: %+v", a.Goal())
	}
}

// A KindGoal event draws no room line - it is machinery, not speech.
func TestGoalDrawsNoRoomLine(t *testing.T) {
	if _, out := NewFleet().Observe(goalEvent(core.GoalSet, "x"), "s1"); len(out) != 0 {
		t.Errorf("a goal event produced %d room lines, want 0", len(out))
	}
}

// The report is the late-attach route and both sets and clears the goal, because
// the daemon holds it authoritatively and never reports it spuriously empty.
func TestGoalFoldsFromTheReport(t *testing.T) {
	set := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateWorking, Goal: &rpc.GoalStatus{Condition: "keep green", Active: true}},
	}})
	if a, _ := set.Agent("s1"); !a.Goal().Active || a.Goal().Condition != "keep green" {
		t.Fatalf("report did not set the goal: %+v", a.Goal())
	}

	cleared := set.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateIdle}, // no goal
	}})
	if a, _ := cleared.Agent("s1"); a.Goal().Active {
		t.Errorf("a report with no goal did not clear it: %+v", a.Goal())
	}
}

// Re-observing the same goal changes nothing, so Agent stays comparable and the
// fleet is not copied - the now == was optimisation Observe rests on.
func TestReobservingTheSameGoalIsANoOp(t *testing.T) {
	f, _ := NewFleet().Observe(goalEvent(core.GoalSet, "x"), "s1")
	before := rosterIdentity(f)
	same, _ := f.Observe(goalEvent(core.GoalSet, "x"), "s1")
	if rosterIdentity(same) != before {
		t.Error("re-observing an identical goal copied the whole fleet")
	}
}
