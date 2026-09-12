package ui

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// GoalState is the native /goal an agent has active, folded onto Agent from the
// live KindGoal event and from the fleet report. A value type so Agent stays
// comparable (Observe's now == was): Active distinguishes "no goal" (the zero
// value) from one, and Condition is what the ◆ marker draws. See core.GoalOp and
// internal/daemon/goal.go.
type GoalState struct {
	Condition string
	Active    bool
}

// goalGlyph marks a session with an active /goal across every surface - the
// roster row, the DM and room bars, the board. Wake's own diamond, not Claude's
// ◎; one column wide.
const goalGlyph = "◆"

// withGoal folds one KindGoal op onto the agent for the watching client: a set
// and each per-turn progress refresh carry the condition, a clear or a "no goal"
// status ends it. Achieve is silent on the wire, so an achieved goal persists
// until an explicit clear (the design's §6).
func (a Agent) withGoal(op core.GoalOp) Agent {
	switch op.Op {
	case core.GoalSet, core.GoalProgress:
		a.goal = GoalState{Condition: op.Condition, Active: true}
	case core.GoalCleared, core.GoalNone:
		a.goal = GoalState{}
	}
	return a
}

// Goal is the native /goal this agent has active, for the render surfaces. See
// GoalState and goal.go's fold.
func (a Agent) Goal() GoalState { return a.goal }

// goalFromReport folds the report's goal - the late-attach route and the
// gap-robust second observable. It clears when the report carries none, unlike
// withCommands: the daemon holds the goal authoritatively and its snapshot is
// never spuriously empty (only a KindGoal event moves it), so the report both
// sets and clears the way Color does.
func goalFromReport(g *rpc.GoalStatus) GoalState {
	if g == nil {
		return GoalState{}
	}
	return GoalState{Condition: g.Condition, Active: g.Active}
}
