package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// An agent folds the native /goal lifecycle onto its report, so a client that
// attached after the goal was set still learns it - Commands' own reason.
func TestAnAgentReportsItsActiveGoal(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindGoal, Goal: &core.GoalOp{Op: core.GoalSet, Condition: "ship the PR"}})
	if g := a.snapshot().Goal; g == nil || g.Condition != "ship the PR" || !g.Active {
		t.Fatalf("snapshot().Goal = %+v after a set, want active with the condition", a.snapshot().Goal)
	}

	// A progress refresh keeps it active and re-carries the condition.
	a.observe(core.Event{Kind: core.KindGoal, Goal: &core.GoalOp{Op: core.GoalProgress, Condition: "ship the PR", Reason: "tests still red"}})
	if g := a.snapshot().Goal; g == nil || !g.Active {
		t.Errorf("a progress refresh cleared the goal: %+v", a.snapshot().Goal)
	}

	// A clear ends it, and the report omits the payload.
	a.observe(core.Event{Kind: core.KindGoal, Goal: &core.GoalOp{Op: core.GoalCleared, Condition: "ship the PR"}})
	if g := a.snapshot().Goal; g != nil {
		t.Errorf("snapshot().Goal = %+v after a clear, want nil", g)
	}
}

// An ordinary frame leaves the goal alone: only a KindGoal event moves it, so a
// result or tool frame between turns does not blank an active goal.
func TestAReportKeepsTheGoalAcrossOrdinaryFrames(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindGoal, Goal: &core.GoalOp{Op: core.GoalSet, Condition: "keep green"}})
	a.observe(core.Event{Kind: core.KindTurnEnd})
	a.observe(core.Event{Kind: core.KindToolResult, Text: "some output"})

	if g := a.snapshot().Goal; g == nil || g.Condition != "keep green" {
		t.Errorf("snapshot().Goal = %+v after ordinary frames, want the goal kept", a.snapshot().Goal)
	}
}

// foldGoal is a pure fold: set/progress carry the condition, clear/none end it
// (""), and an unhandled op leaves it as it was (there is no achieved op -
// achieve is silent on the wire).
func TestFoldGoal(t *testing.T) {
	if got := foldGoal("", core.GoalOp{Op: core.GoalSet, Condition: "x"}); got != "x" {
		t.Errorf("set: got %q, want x", got)
	}
	if got := foldGoal("x", core.GoalOp{Op: core.GoalProgress, Condition: "x2"}); got != "x2" {
		t.Errorf("progress: got %q, want x2 (refreshed)", got)
	}
	if got := foldGoal("x", core.GoalOp{Op: core.GoalNone}); got != "" {
		t.Errorf("none: got %q, want empty", got)
	}
	if got := foldGoal("x", core.GoalOp{Op: core.GoalOpKind("weird")}); got != "x" {
		t.Errorf("unhandled op: got %q, want the condition kept", got)
	}
}
