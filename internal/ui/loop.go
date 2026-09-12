package ui

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// LoopState is the native /loop an agent has active, folded onto Agent from the
// live scheduler tool_use (ev.Tool.Loop) and from the fleet report. A value type
// so Agent stays comparable (Observe's now == was): Active distinguishes "no
// loop" (the zero value) from one, SelfPaced tells the two modes apart, and Cron
// is the fixed cadence the ↻ detail draws. See core.LoopOp and daemon/loop.go.
type LoopState struct {
	Active    bool
	SelfPaced bool
	Cron      string
}

// loopGlyph marks a session with an active /loop across every surface - the
// roster row, the DM and room bars, the board. Wake's own, one column wide,
// beside the ◆ a goal draws.
const loopGlyph = "↻"

// withLoop folds one scheduler op onto the agent for the watching client: a
// recurring CronCreate or a ScheduleWakeup activates it, a CronDelete ends it.
// A loop reads active until a delete - an expired or completed one is silent on
// the wire, the loop half of the goal's own limitation (spec §6).
func (a Agent) withLoop(op core.LoopOp) Agent {
	switch {
	case op.Stop:
		a.loop = LoopState{}
	case op.Kind == core.LoopFixed:
		a.loop = LoopState{Active: true, Cron: op.Cron}
	case op.Kind == core.LoopSelfPaced:
		a.loop = LoopState{Active: true, SelfPaced: true}
	}
	return a
}

// Loop is the native /loop this agent has active, for the render surfaces. See
// LoopState and fleet.go's fold.
func (a Agent) Loop() LoopState { return a.loop }

// loopFromReport folds the report's loop - the late-attach route and the
// gap-robust second observable. It both sets and clears, Goal's reason: the
// daemon holds the loop authoritatively and its snapshot is never spuriously
// empty (only a scheduler op moves it).
func loopFromReport(l *rpc.LoopStatus) LoopState {
	if l == nil {
		return LoopState{}
	}
	return LoopState{Active: l.Active, SelfPaced: l.SelfPaced, Cron: l.Cron}
}
