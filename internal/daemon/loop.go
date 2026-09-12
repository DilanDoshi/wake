package daemon

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// loopState is the native /loop a session has active, folded from the scheduler
// tool_use calls it makes: a recurring CronCreate is a fixed cadence, a
// ScheduleWakeup is self-paced, a CronDelete ends it. There is deliberately no
// "ended" op beyond a delete - an expired or completed loop leaves no frame -
// so a loop reads active until a CronDelete, the loop half of /goal's §6 silence.
type loopState struct {
	active    bool
	selfPaced bool
	cron      string
}

// foldLoop applies one decoded loop op to a session's loop state, and leaves it
// as it was for an op that names neither mode nor a stop.
func foldLoop(s loopState, op core.LoopOp) loopState {
	switch {
	case op.Stop:
		return loopState{}
	case op.Kind == core.LoopFixed:
		return loopState{active: true, cron: op.Cron}
	case op.Kind == core.LoopSelfPaced:
		return loopState{active: true, selfPaced: true}
	}
	return s
}

// loopStatus builds the report's loop payload, or nil for a session with none so
// the key omits whole. See rpc.LoopStatus.
func loopStatus(s loopState) *rpc.LoopStatus {
	if !s.active {
		return nil
	}
	return &rpc.LoopStatus{Active: true, SelfPaced: s.selfPaced, Cron: s.cron}
}
