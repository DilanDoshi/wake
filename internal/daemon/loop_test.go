package daemon

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

// foldLoop tracks the mode across the scheduler ops, and loopStatus reports it
// or nil for an idle session.
func TestFoldLoopTracksTheMode(t *testing.T) {
	s := foldLoop(loopState{}, core.LoopOp{Kind: core.LoopFixed, Cron: "*/5 * * * *"})
	if !s.active || s.selfPaced || s.cron != "*/5 * * * *" {
		t.Fatalf("fixed fold = %+v, want active fixed with the cron", s)
	}

	s = foldLoop(s, core.LoopOp{Kind: core.LoopSelfPaced})
	if !s.active || !s.selfPaced || s.cron != "" {
		t.Errorf("self-paced fold = %+v, want active self-paced with no cron", s)
	}

	if s = foldLoop(s, core.LoopOp{Stop: true}); s.active {
		t.Errorf("after a CronDelete, still active: %+v", s)
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
