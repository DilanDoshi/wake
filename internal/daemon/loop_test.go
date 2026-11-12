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

// A tick with no usable delay (a missing key, or an immediate wake) clears the
// next fire rather than leaving the last iteration's - which is already in the
// past and would render as an elapsed "next" time.
func TestFoldLoopClearsAStaleNextFire(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	s := foldLoop(loopState{}, core.LoopOp{Kind: core.LoopSelfPaced, DelaySeconds: 300}, now)
	if s.nextFire.IsZero() {
		t.Fatal("setup: the first tick should have stamped a next fire")
	}
	if s = foldLoop(s, core.LoopOp{Kind: core.LoopSelfPaced}, now); !s.nextFire.IsZero() {
		t.Errorf("a tick with no delay kept the stale next fire: %+v", s)
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

// A self-paced loop ends when its own wakeup turn completes without scheduling
// the next (claude 2.1.319+; loop-self-paced-end.jsonl's second iteration), unless
// CLAUDE_CODE_LOOP_KEEPALIVE=1 keeps claude's old rule.
func TestASelfPacedLoopEndsWhenItsWakeupTurnDoesNotScheduleTheNext(t *testing.T) {
	for _, tc := range []struct {
		keepAlive string
		active    bool
	}{{"", false}, {"1", true}} {
		t.Setenv("CLAUDE_CODE_LOOP_KEEPALIVE", tc.keepAlive)
		t.Setenv(fixtureDirEnv, fixtureDir(t))
		a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
			core.NewSession(core.Config{SessionID: idAlpha}), func() {})
		sawLoop, announcedEnd := false, false
		for _, line := range readFixture("loop-self-paced-end.jsonl") {
			evs, err := core.DecodeLine([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range evs {
				before := a.snapshot().Loop != nil
				a.observe(ev)
				// The fan-out's push: the end lands after the turn's result, so it
				// moves no state and must still reach the windows.
				pushed, after := a.changed(), a.snapshot().Loop != nil
				announcedEnd = announcedEnd || (before && !after && pushed)
				sawLoop = sawLoop || after
			}
		}
		if !sawLoop {
			t.Fatal("the fixture's first iteration never started a loop")
		}
		if got := a.snapshot().Loop != nil; got != tc.active {
			t.Errorf("keepalive %q: loop active = %v after the wakeup turn that scheduled nothing, want %v", tc.keepAlive, got, tc.active)
		}
		if announcedEnd == tc.active {
			t.Errorf("keepalive %q: the loop's end announced = %v, want %v", tc.keepAlive, announcedEnd, !tc.active)
		}
	}
}

// Only the loop's own wakeup turn ends it, and only one that scheduled nothing:
// a message Wake sent, finished inside it, ends nothing; a wakeup turn that
// reschedules keeps the loop; a fixed loop and no loop at all are left alone; a
// stop between start and end is final; and a cancelled wakeup scheduled nothing.
func TestOnlyAWakeupTurnThatSchedulesNothingEndsALoop(t *testing.T) {
	selfPaced := core.LoopOp{Kind: core.LoopSelfPaced, DelaySeconds: 60}
	ticked := foldLoop(loopState{}, selfPaced, time.Now())
	lc := func(id, state string, wakeup bool) core.Event {
		return core.Event{Kind: core.KindMessageState, MessageID: id, Text: state, LoopWakeup: wakeup}
	}
	op := func(o core.LoopOp) func(loopState) loopState {
		return func(s loopState) loopState { return foldLoop(s, o, time.Now()) }
	}
	ev := func(e core.Event) func(loopState) loopState {
		return func(s loopState) loopState { return foldWakeup(s, e, false) }
	}
	for _, tc := range []struct {
		name   string
		from   loopState
		steps  []func(loopState) loopState
		active bool
		iter   int
	}{
		{"an operator's turn between wakeups", ticked,
			[]func(loopState) loopState{ev(lc("op", "started", false)), ev(lc("op", "completed", false))}, true, 1},
		{"a wakeup turn that schedules the next", ticked,
			[]func(loopState) loopState{ev(lc("w1", "started", true)), op(selfPaced), ev(lc("w1", "completed", false))}, true, 2},
		{"a message Wake sent, finished inside a wakeup turn", ticked,
			[]func(loopState) loopState{ev(lc("w1", "started", true)), ev(lc("m1", "started", false)),
				ev(lc("m1", "completed", false))}, true, 1},
		{"that wakeup turn then completing with nothing scheduled", ticked,
			[]func(loopState) loopState{ev(lc("w1", "started", true)), ev(lc("m1", "completed", false)),
				ev(lc("w1", "completed", false))}, false, 0},
		{"a fixed loop", foldLoop(loopState{}, core.LoopOp{Kind: core.LoopFixed, Cron: "*/5 * * * *"}, time.Now()),
			[]func(loopState) loopState{ev(lc("w1", "started", true)), ev(lc("w1", "completed", false))}, true, 0},
		{"no loop at all", loopState{},
			[]func(loopState) loopState{ev(lc("w1", "started", true)), ev(lc("w1", "completed", false))}, false, 0},
		{"a stop inside a wakeup turn", ticked,
			[]func(loopState) loopState{ev(lc("w1", "started", true)), op(core.LoopOp{Stop: true}),
				ev(lc("w1", "completed", false))}, false, 0},
		{"a cancelled wakeup turn", ticked,
			[]func(loopState) loopState{ev(lc("w1", "started", true)), ev(lc("w1", "cancelled", false))}, false, 0},
	} {
		s := tc.from
		for _, step := range tc.steps {
			s = step(s)
		}
		if s.active != tc.active || s.iter != tc.iter {
			t.Errorf("%s: active=%v iter=%d, want %v %d", tc.name, s.active, s.iter, tc.active, tc.iter)
		}
	}
}

// A loop that starts on a tool call mid-turn moves no state, and is announced
// all the same: the windows learn of it at once, not at the turn's end.
func TestALoopStartingMidTurnIsAnnounced(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.observe(core.Event{Kind: core.KindMessageState, MessageID: "m1", Text: "started"})
	a.changed()
	a.observe(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{Name: "ScheduleWakeup",
		Loop: &core.LoopOp{Kind: core.LoopSelfPaced, DelaySeconds: 60}}})
	if a.snapshot().Loop == nil {
		t.Fatal("the ScheduleWakeup started no loop")
	}
	if !a.changed() {
		t.Error("a loop that started mid-turn was not announced")
	}
	// Once: a loop that stays active pushes nothing more, token after token.
	a.observe(core.Event{Kind: core.KindPartialText, Text: "tick"})
	if a.changed() {
		t.Error("an active loop was announced again on a frame that changed nothing")
	}
}
