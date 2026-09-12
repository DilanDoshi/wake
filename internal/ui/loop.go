package ui

import (
	"strconv"
	"strings"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// LoopState is the native /loop an agent has active, folded onto Agent from the
// live scheduler tool_use (ev.Tool.Loop) and from the fleet report. A value type
// so Agent stays comparable (Observe's now == was): Active distinguishes "no
// loop" (the zero value) from one, SelfPaced tells the two modes apart, and Cron
// is the fixed cadence the ↻ detail draws. See core.LoopOp and daemon/loop.go.
//
// Iter, Quiet and NextFire are a self-paced run's figures and stay zero for a
// fixed loop: Iter is how many iterations have completed, Quiet the current run
// of noop ticks, and NextFire when the next wakeup is due. NextFire is a
// time.Time - comparable, so Agent stays comparable - captured like doneAt.
type LoopState struct {
	Active    bool
	SelfPaced bool
	Cron      string
	Iter      int
	Quiet     int
	NextFire  time.Time
}

// loopGlyph marks a session with an active /loop across every surface - the
// roster row, the DM and room bars, the board. Wake's own, one column wide,
// beside the ◆ a goal draws.
const loopGlyph = "↻"

// withLoop folds one scheduler op onto the agent for the watching client: a
// recurring CronCreate or a ScheduleWakeup activates it, a CronDelete ends it.
// A loop reads active until a delete - an expired or completed one is silent on
// the wire, the loop half of the goal's own limitation (spec §6). A self-paced
// tick accumulates the run onto the existing state (the daemon's foldLoop does
// the same for the report); the next-fire is stamped off clock() the way doneAt is.
func (a Agent) withLoop(op core.LoopOp) Agent {
	switch {
	case op.Stop:
		a.loop = LoopState{}
	case op.Kind == core.LoopFixed:
		a.loop = LoopState{Active: true, Cron: op.Cron}
	case op.Kind == core.LoopSelfPaced:
		a.loop.Active, a.loop.SelfPaced, a.loop.Cron = true, true, ""
		a.loop.Iter++
		if op.Noop {
			a.loop.Quiet++
		} else {
			a.loop.Quiet = 0
		}
		// A tick with no usable delay clears the next fire rather than leaving the
		// last iteration's stale one - see daemon/loop.go's foldLoop.
		if op.DelaySeconds > 0 {
			a.loop.NextFire = clock().Add(time.Duration(op.DelaySeconds) * time.Second)
		} else {
			a.loop.NextFire = time.Time{}
		}
	}
	return a
}

// Loop is the native /loop this agent has active, for the render surfaces. See
// LoopState and fleet.go's fold.
func (a Agent) Loop() LoopState { return a.loop }

// idleDetail is the muted line under an idle agent's name in the roster: its
// goal, its loop, or both joined - and "" when it has neither. The tool call
// takes this row while the agent is working (roster.go's rows).
func idleDetail(a Agent) string {
	var parts []string
	if a.goal.Active {
		parts = append(parts, goalGlyph+" "+oneLine(a.goal.Condition))
	}
	if d := loopLine(a.loop); d != "" {
		parts = append(parts, d)
	}
	return strings.Join(parts, " · ")
}

// loopLine is the ↻ text a loop draws - the cadence a fixed loop runs on,
// "self-paced" when Claude picks each delay, and a bare "looping" for a cron this
// build does not humanise. "" for no loop. Shared by the roster's activity line
// and the status bar's segment. The cron is already contained in the airlock, so
// only a derived cadence is drawn, never the raw expression.
func loopLine(l LoopState) string {
	if !l.Active {
		return ""
	}
	switch {
	case l.SelfPaced:
		s := loopGlyph + " self-paced"
		if l.Iter > 0 {
			s = loopGlyph + " iter " + strconv.Itoa(l.Iter)
		}
		if l.Quiet > 0 {
			s += " · quiet ×" + strconv.Itoa(l.Quiet)
		}
		return s
	case loopCadence(l.Cron) != "":
		return loopGlyph + " " + loopCadence(l.Cron)
	default:
		return loopGlyph + " looping"
	}
}

// loopCadence turns a cron expression into a short human cadence for the badge -
// "*/5 * * * *" reads "every 5m" - and "" for anything it does not recognise,
// where the badge falls back to a bare "looping". The five fields are Claude's
// (recorded in loop-fixed.jsonl); only the common every-N shapes are named.
func loopCadence(cron string) string {
	f := strings.Fields(cron)
	if len(f) != 5 {
		return ""
	}
	minute, hour := f[0], f[1]
	switch {
	case strings.HasPrefix(minute, "*/") && hour == "*":
		if n := strings.TrimPrefix(minute, "*/"); n != "" {
			return "every " + n + "m"
		}
	case minute == "0" && hour == "*":
		return "every hour"
	case minute == "0" && strings.HasPrefix(hour, "*/"):
		if n := strings.TrimPrefix(hour, "*/"); n != "" {
			return "every " + n + "h"
		}
	}
	return ""
}

// loopFromReport folds the report's loop - the late-attach route and the
// gap-robust second observable. It both sets and clears, Goal's reason: the
// daemon holds the loop authoritatively and its snapshot is never spuriously
// empty (only a scheduler op moves it).
func loopFromReport(l *rpc.LoopStatus) LoopState {
	if l == nil {
		return LoopState{}
	}
	s := LoopState{Active: l.Active, SelfPaced: l.SelfPaced, Cron: l.Cron, Iter: l.Iter, Quiet: l.Quiet}
	if l.NextFire != 0 {
		s.NextFire = time.Unix(l.NextFire, 0)
	}
	return s
}
