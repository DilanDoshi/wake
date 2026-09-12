package daemon

import (
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// loopState is the native /loop a session has active, folded from the scheduler
// tool_use calls it makes: a recurring CronCreate is a fixed cadence, a
// ScheduleWakeup is self-paced, a CronDelete ends it. There is deliberately no
// "ended" op beyond a delete - an expired or completed loop leaves no frame -
// so a loop reads active until a CronDelete, the loop half of /goal's §6 silence.
//
// A self-paced run also accumulates: iter counts the iterations (each ends with a
// ScheduleWakeup), quiet is the current run of noop ticks, and nextFire is when
// the last-scheduled wakeup is due. A fixed loop has none of the three - a
// cron-fire carries no wire marker to count - so it stays cadence-only.
type loopState struct {
	active    bool
	selfPaced bool
	cron      string
	iter      int
	quiet     int
	nextFire  time.Time
}

// foldLoop applies one decoded loop op to a session's loop state at now, and
// leaves it as it was for an op that names neither mode nor a stop. A self-paced
// tick accumulates onto the run; a fixed CronCreate or a delete resets it.
func foldLoop(s loopState, op core.LoopOp, now time.Time) loopState {
	switch {
	case op.Stop:
		return loopState{}
	case op.Kind == core.LoopFixed:
		return loopState{active: true, cron: op.Cron}
	case op.Kind == core.LoopSelfPaced:
		s.active, s.selfPaced, s.cron = true, true, ""
		s.iter++
		if op.Noop {
			s.quiet++
		} else {
			s.quiet = 0
		}
		// Every tick resolves the next fire, the way it resolves the quiet streak:
		// a tick with no usable delay (a missing/malformed key, or an immediate
		// wake) clears it rather than leaving the last iteration's - which is a time
		// already in the past, and would render "next <elapsed time>".
		if op.DelaySeconds > 0 {
			s.nextFire = now.Add(time.Duration(op.DelaySeconds) * time.Second)
		} else {
			s.nextFire = time.Time{}
		}
		return s
	}
	return s
}

// loopStatus builds the report's loop payload, or nil for a session with none so
// the key omits whole. See rpc.LoopStatus.
func loopStatus(s loopState) *rpc.LoopStatus {
	if !s.active {
		return nil
	}
	st := &rpc.LoopStatus{Active: true, SelfPaced: s.selfPaced, Cron: s.cron, Iter: s.iter, Quiet: s.quiet}
	if !s.nextFire.IsZero() {
		st.NextFire = s.nextFire.Unix()
	}
	return st
}
