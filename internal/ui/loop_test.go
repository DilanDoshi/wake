package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func loopToolEvent(op core.LoopOp) core.Event {
	return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "sched", Loop: &op}}
}

// A live scheduler tool_use folds onto Agent.Loop: a recurring CronCreate is a
// fixed loop, a ScheduleWakeup is self-paced, and a CronDelete ends it.
func TestLoopFoldsFromTheLiveEvent(t *testing.T) {
	f, _ := NewFleet().Observe(loopToolEvent(core.LoopOp{Kind: core.LoopFixed, Cron: "*/5 * * * *"}), "s1")
	if a, _ := f.Agent("s1"); !a.Loop().Active || a.Loop().SelfPaced || a.Loop().Cron != "*/5 * * * *" {
		a, _ := f.Agent("s1")
		t.Fatalf("after a fixed CronCreate, Loop = %+v, want active fixed with the cron", a.Loop())
	}

	f2, _ := NewFleet().Observe(loopToolEvent(core.LoopOp{Kind: core.LoopSelfPaced}), "s2")
	if a, _ := f2.Agent("s2"); !a.Loop().Active || !a.Loop().SelfPaced {
		t.Errorf("after a ScheduleWakeup, Loop = %+v, want active self-paced", a.Loop())
	}

	f, _ = f.Observe(loopToolEvent(core.LoopOp{Stop: true}), "s1")
	if a, _ := f.Agent("s1"); a.Loop().Active {
		t.Errorf("after a CronDelete, s1 Loop still active: %+v", a.Loop())
	}
}

// The report is the late-attach route and both sets and clears the loop.
func TestLoopFoldsFromTheReport(t *testing.T) {
	set := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateWorking, Loop: &rpc.LoopStatus{Active: true, Cron: "0 * * * *"}},
	}})
	if a, _ := set.Agent("s1"); !a.Loop().Active || a.Loop().Cron != "0 * * * *" {
		t.Fatalf("report did not set the loop: %+v", a.Loop())
	}
	cleared := set.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", State: rpc.StateIdle}}})
	if a, _ := cleared.Agent("s1"); a.Loop().Active {
		t.Errorf("a report with no loop did not clear it: %+v", a.Loop())
	}
}

// An ordinary tool_use carries no Loop op and leaves the loop untouched.
func TestOrdinaryToolDoesNotTouchTheLoop(t *testing.T) {
	f, _ := NewFleet().Observe(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "Bash"}}, "s1")
	if a, _ := f.Agent("s1"); a.Loop().Active {
		t.Errorf("a plain tool_use activated a loop: %+v", a.Loop())
	}
}
