package daemon

// A park or an end kills the process group, and no task_ended arrives for what
// it was running - a background shell above all. What replayRunningTasks hands
// a late client must go with the process, or a parked row carries a running
// shell that is gone.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestRunningTaskFramesAreForgottenWhenTheProcessEnds(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.observe(core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: "b1", Dispatch: "toolu_1", Kind: core.TaskShell,
		Phase: core.TaskStarted, Status: core.TaskRunning, Label: "make ci",
	}})
	if len(a.runningTaskFrames()) != 1 {
		t.Fatal("baseline: the running shell is not retained for replay")
	}

	a.finish(nil)
	if got := a.runningTaskFrames(); got != nil {
		t.Errorf("an ended agent still hands a late client %d running task(s)", len(got))
	}
}

// A shell moved to the background is still running, and a late client is still
// handed it: the recorded task_updated that does it patches is_backgrounded and
// names no outcome (core.TestATaskUpdatedThatNamesNoOutcomeIsNotAnEnding).
func TestABackgroundedShellIsStillReplayedToALateClient(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	for _, ev := range decodeStreamFixture(t, "midturn-now-human.jsonl") {
		if ev.Kind == core.KindTurnEnd {
			break
		}
		a.observe(ev)
	}
	frames := a.runningTaskFrames()
	if len(frames) != 1 || frames[0].Event.Task.Kind != core.TaskShell {
		t.Errorf("a late client is replayed %d task(s), want the one backgrounded shell", len(frames))
	}
}
