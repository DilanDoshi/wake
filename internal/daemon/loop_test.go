package daemon

import (
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// foldLoop tracks the mode across the scheduler ops, and loopStatus reports it
// or nil for an idle session.
func TestFoldLoopTracksTheMode(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := foldLoop(loopState{}, core.LoopOp{Kind: core.LoopFixed, Cron: "*/5 * * * *"}, now)
	if !s.active || s.selfPaced || s.cron != "*/5 * * * *" {
		t.Fatalf("fixed fold = %+v, want active fixed with the cron", s)
	}

	s = foldLoop(s, core.LoopOp{Kind: core.LoopSelfPaced}, now)
	if !s.active || !s.selfPaced || s.cron != "" {
		t.Errorf("self-paced fold = %+v, want active self-paced with no cron", s)
	}

	if s = foldLoop(s, core.LoopOp{Stop: true}, now); s.active {
		t.Errorf("after a CronDelete, still active: %+v", s)
	}
}

// A self-paced loop accumulates: each ScheduleWakeup is one iteration, its delay
// stamps the next fire, and a noop tick runs a quiet streak that a working tick
// resets. A CronDelete clears the whole count.
func TestFoldLoopAccumulatesTheSelfPacedRun(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := foldLoop(loopState{}, core.LoopOp{Kind: core.LoopSelfPaced, DelaySeconds: 300}, now)
	if s.iter != 1 || s.quiet != 0 || !s.nextFire.Equal(now.Add(300*time.Second)) {
		t.Fatalf("first iteration = %+v, want iter 1, quiet 0, nextFire +300s", s)
	}

	s = foldLoop(s, core.LoopOp{Kind: core.LoopSelfPaced, DelaySeconds: 60, Noop: true}, now)
	s = foldLoop(s, core.LoopOp{Kind: core.LoopSelfPaced, DelaySeconds: 60, Noop: true}, now)
	if s.iter != 3 || s.quiet != 2 {
		t.Errorf("after two quiet ticks = %+v, want iter 3, quiet 2", s)
	}

	s = foldLoop(s, core.LoopOp{Kind: core.LoopSelfPaced, DelaySeconds: 60}, now)
	if s.iter != 4 || s.quiet != 0 {
		t.Errorf("a working tick did not reset quiet: %+v", s)
	}

	if s = foldLoop(s, core.LoopOp{Stop: true}, now); s.iter != 0 || s.quiet != 0 || !s.nextFire.IsZero() {
		t.Errorf("a CronDelete did not clear the count: %+v", s)
	}
}

func TestLoopStatusReportsOnlyAnActiveLoop(t *testing.T) {
	if loopStatus(loopState{}) != nil {
		t.Error("loopStatus of an idle session should be nil so the report key omits")
	}
	st := loopStatus(loopState{active: true, cron: "0 * * * *"})
	if st == nil || !st.Active || st.SelfPaced || st.Cron != "0 * * * *" {
		t.Errorf("loopStatus = %+v, want active fixed with the cron", st)
	}
}

// The self-paced run's figures ride the report: the iteration count, the quiet
// streak, and the next fire as unix seconds (0 when unscheduled, so the key omits).
func TestLoopStatusCarriesTheSelfPacedRun(t *testing.T) {
	fire := time.Unix(1_700_000_300, 0)
	st := loopStatus(loopState{active: true, selfPaced: true, iter: 4, quiet: 2, nextFire: fire})
	if st == nil || st.Iter != 4 || st.Quiet != 2 || st.NextFire != fire.Unix() {
		t.Errorf("loopStatus = %+v, want iter 4, quiet 2, nextFire %d", st, fire.Unix())
	}
	if bare := loopStatus(loopState{active: true, cron: "*/5 * * * *"}); bare.NextFire != 0 {
		t.Errorf("a fixed loop reported a next fire: %+v", bare)
	}
}
