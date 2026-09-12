package ui

import (
	"strings"
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

// The ↻ shows on every operator surface, and its head-line marker survives while
// the agent is working (the tool call takes the activity line, not the marker).
func TestLoopRendersAcrossSurfaces(t *testing.T) {
	fixed := Agent{ID: "s1", Name: "iris", State: rpc.StateIdle}.withLoop(core.LoopOp{Kind: core.LoopFixed, Cron: "*/5 * * * *"})

	working := fixed
	working.State, working.Tool = rpc.StateWorking, "Bash"
	if !strings.Contains(headLine(working, 40), loopGlyph) {
		t.Errorf("roster head line lost the ↻ marker while working: %q", headLine(working, 40))
	}
	if d := idleDetail(fixed); !strings.Contains(d, "↻ every 5m") {
		t.Errorf("idle detail = %q, want the cadence", d)
	}
	if bar := statusBar(fixed, "default", 100, 1); !strings.Contains(bar, loopGlyph) {
		t.Errorf("status bar missing the loop: %q", bar)
	}
	if d := boardDetail(fixed); !strings.Contains(d, loopGlyph) {
		t.Errorf("board detail missing the loop: %q", d)
	}

	sp := Agent{ID: "s2", State: rpc.StateIdle}.withLoop(core.LoopOp{Kind: core.LoopSelfPaced})
	if d := loopLine(sp.Loop()); d != "↻ self-paced" {
		t.Errorf("self-paced line = %q, want ↻ self-paced", d)
	}
	if plain := (Agent{ID: "s3", State: rpc.StateIdle}); idleDetail(plain) != "" {
		t.Errorf("a plain agent has an idle detail: %q", idleDetail(plain))
	}
}

func TestLoopCadenceHumanises(t *testing.T) {
	cases := map[string]string{
		"*/5 * * * *":  "every 5m",
		"0 * * * *":    "every hour",
		"0 */2 * * *":  "every 2h",
		"30 14 15 3 *": "", // a specific time, not a simple cadence
		"nonsense":     "",
	}
	for cron, want := range cases {
		if got := loopCadence(cron); got != want {
			t.Errorf("loopCadence(%q) = %q, want %q", cron, got, want)
		}
	}
}
