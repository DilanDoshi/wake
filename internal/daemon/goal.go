package daemon

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// foldGoal applies one decoded /goal op to a session's goal condition. Set and
// each per-turn progress refresh carry the condition; a clear or a "no goal"
// status ends it (""), and any other op leaves the condition as it was. A goal is
// its condition, so "" is the whole of "no goal".
//
// There is deliberately no achieved case: a met goal is simply not followed by
// another Stop-hook feedback frame, so the wire carries no signal here. An
// achieved goal therefore reads active until an explicit clear - the spec's §6
// limitation, and why this is a pure fold with no timer of its own.
func foldGoal(condition string, op core.GoalOp) string {
	switch op.Op {
	case core.GoalSet, core.GoalProgress:
		return op.Condition
	case core.GoalCleared, core.GoalNone:
		return ""
	}
	return condition
}

// goalStatus builds the report's goal payload, or nil for a session with none so
// the key omits whole. See rpc.GoalStatus.
func goalStatus(condition string) *rpc.GoalStatus {
	if condition == "" {
		return nil
	}
	return &rpc.GoalStatus{Condition: condition, Active: true}
}
