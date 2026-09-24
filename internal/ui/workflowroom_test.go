package ui

// The room's half of a dynamic Workflow() run: which of its lifecycle frames
// reach the group chat, and what the line says once it does.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
)

// decodeWorkflowFixture decodes a recorded stream-json fixture in arrival
// order, the same corpus internal/core's own workflow_test.go reads.
func decodeWorkflowFixture(t *testing.T, name string) []core.Event {
	t.Helper()
	body, err := os.ReadFile("../../testdata/stream/" + name)
	if err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	var out []core.Event
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			continue // a recorded stderr line or a frame this build does not decode
		}
		out = append(out, evs...)
	}
	return out
}

// Observing a real recorded run end to end: one row on the fleet, named for
// the workflow rather than its description, holding the last snapshot it
// reported, and done by the time the recording ends.
func TestObservingAWorkflowFixtureYieldsOneRowDoneAtTheEnd(t *testing.T) {
	f := NewFleet()
	for _, ev := range decodeWorkflowFixture(t, "workflow-run.jsonl") {
		f, _ = f.Observe(ev, "s1")
	}

	rows := f.RunningTasks("s1")
	if len(rows) != 0 {
		t.Errorf("RunningTasks() = %+v after the run ended, want none still running", rows)
	}
	// RunningTasks excludes an ended row by design, so the row itself is read
	// straight off the fold rather than through that filter.
	all := f.tasks["s1"].Rows()
	if len(all) != 1 {
		t.Fatalf("got %d rows, want the one workflow dispatch: %+v", len(all), all)
	}
	row := all[0]
	if row.Kind != core.TaskWorkflow {
		t.Errorf("Kind = %q, want %q", row.Kind, core.TaskWorkflow)
	}
	if row.Name != "count-lines" {
		t.Errorf("Name = %q, want the workflow's own short name", row.Name)
	}
	if got := row.Workflow.Done(); got != 3 {
		t.Errorf("Workflow.Done() = %d, want 3", got)
	}
	if row.Status != core.TaskDone {
		t.Errorf("Status = %q, want %q", row.Status, core.TaskDone)
	}
}

// The replay shape - a started frame that already carries a snapshot, the way
// a late-attaching client is handed one (taskreplay.go's withProgress) - sets
// it immediately rather than waiting on a task_progress this client will
// never see again.
func TestAReplayedWorkflowStartSetsItsSnapshotOnTheFirstFold(t *testing.T) {
	replayed := core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: "w1", Dispatch: "toolu_1", Kind: core.TaskWorkflow, Phase: core.TaskStarted,
		Status: core.TaskRunning, Label: "desc", Workflow: &core.WorkflowUpdate{
			Name: "count-lines", Progress: ptrSnapshot(workflowSnapshot(2, 3)),
		},
	}}

	f := NewFleet()
	f, _ = f.Observe(replayed, "s1")

	rows := f.RunningTasks("s1")
	if len(rows) != 1 {
		t.Fatalf("RunningTasks() = %+v, want the one replayed workflow", rows)
	}
	if got := rows[0].Workflow.Done(); got != 2 {
		t.Errorf("Done() = %d, want 2 straight off the one frame observed", got)
	}
}

// A workflow's ending folds into the room, headed by the agent that ran it -
// the same speaker style every other room line takes.
func TestAWorkflowEndingFoldsIntoARoomLineHeadedByTheAgent(t *testing.T) {
	f := NewFleet()
	var lastRoom []core.Event
	for _, ev := range []core.Event{
		workflowStarted("w1", "toolu_1", "desc", "count-lines"),
		workflowProgressed("w1", "toolu_1", workflowSnapshot(3, 3)),
		core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
			ID: "w1", Kind: core.TaskKindUnknown, Phase: core.TaskEnded, Status: core.TaskDone,
		}},
		core.Event{Kind: core.KindSystem, Task: workflowNotification("w1", "toolu_1", core.TaskDone)},
	} {
		var out []core.Event
		f, out = f.Observe(ev, "s1")
		if len(out) > 0 {
			lastRoom = out
		}
	}

	if len(lastRoom) != 1 {
		t.Fatalf("the room admitted %d events across the run, want exactly 1 (the task_notification alone)", len(lastRoom))
	}
	rendered := roomBlock(lastRoom[0], Agent{ID: "s1", Name: "sydney"}, roomWidth, false)
	for _, want := range []string{"sydney", "Workflow", `"count-lines"`, "finished"} {
		if !strings.Contains(rendered.text, want) {
			t.Errorf("the room line is missing %q:\n%s", want, rendered.text)
		}
	}
}

// An ordinary subagent's ending never reaches the room at all - it stays
// conversation-only, the line taskline.go already draws inside the pane.
func TestASubagentEndingNeverReachesTheRoom(t *testing.T) {
	f := NewFleet()
	admitted := 0
	for _, ev := range []core.Event{
		started("a1", "toolu_1", "Auditing the diff", "code-reviewer", core.TaskAgent),
		progressed("a1", "toolu_1", "Reading alpha.txt", "Read", 100, 0),
		ended("a1", core.TaskDone),
		core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
			ID: "a1", Dispatch: "toolu_1", Kind: core.TaskKindUnknown, Phase: core.TaskEnded, Status: core.TaskDone,
		}},
	} {
		var out []core.Event
		f, out = f.Observe(ev, "s1")
		admitted += len(out)
	}
	if admitted != 0 {
		t.Errorf("a subagent's lifecycle put %d event(s) in the room, want 0: it must stay conversation-only", admitted)
	}
}

// A background shell's ending, same as a subagent's, never reaches the room -
// widening the admission for a workflow must not widen it for every kind.
func TestAShellEndingNeverReachesTheRoom(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(started("b1", "toolu_2", "waiting for the sentinel", "", core.TaskShell), "s1")
	_, out := f.Observe(core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: "b1", Dispatch: "toolu_2", Kind: core.TaskKindUnknown, Phase: core.TaskEnded, Status: core.TaskStopped,
	}}, "s1")
	if len(out) != 0 {
		t.Errorf("a shell's ending reached the room: %+v", out)
	}
}

// task_updated - the bare ending with no dispatch - never reaches the room on
// its own, even for a workflow: taskLine's own discriminator is the frame
// that names its dispatch, and admitting the other one would draw the ending
// twice.
func TestTheBareWorkflowEndingAloneDoesNotReachTheRoom(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(workflowStarted("w1", "toolu_1", "desc", "count-lines"), "s1")
	_, out := f.Observe(core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: "w1", Kind: core.TaskKindUnknown, Phase: core.TaskEnded, Status: core.TaskDone,
	}}, "s1")
	if len(out) != 0 {
		t.Errorf("the bare task_updated reached the room: %+v - only task_notification, which names the dispatch, may", out)
	}
}

// workflowRoomEvent is fold's own admission rule, tested directly: only a
// workflow's ending naming its dispatch survives, and nothing else does -
// including a workflow frame that has not ended yet.
func TestWorkflowRoomEventAdmitsOnlyAnEndingWithADispatch(t *testing.T) {
	cases := []struct {
		name string
		u    *core.TaskUpdate
		want bool
	}{
		{"a workflow ending with a dispatch", &core.TaskUpdate{Kind: core.TaskWorkflow, Phase: core.TaskEnded, Dispatch: "toolu_1"}, true},
		{"a workflow ending with no dispatch", &core.TaskUpdate{Kind: core.TaskWorkflow, Phase: core.TaskEnded}, false},
		{"a workflow still running", &core.TaskUpdate{Kind: core.TaskWorkflow, Phase: core.TaskProgress, Dispatch: "toolu_1"}, false},
		{"a subagent's ending with a dispatch", &core.TaskUpdate{Kind: core.TaskAgent, Phase: core.TaskEnded, Dispatch: "toolu_1"}, false},
		{"a shell's ending with a dispatch", &core.TaskUpdate{Kind: core.TaskShell, Phase: core.TaskEnded, Dispatch: "toolu_1"}, false},
		{"no task at all", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := len(workflowRoomEvent(core.Event{Kind: core.KindSystem, Task: tc.u})) > 0
			if got != tc.want {
				t.Errorf("admitted = %v, want %v", got, tc.want)
			}
		})
	}
}

// The room line never outgrows its column, same claim rostersubs_test.go
// makes for the sidebar row - lipgloss joins columns on their widest line, so
// one over-wide row shoves both sidebars out of place.
func TestAWorkflowRoomLineNeverOutgrowsTheColumn(t *testing.T) {
	ev := core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		Dispatch: "toolu_1", Kind: core.TaskWorkflow, Phase: core.TaskEnded, Status: core.TaskFailed,
		Label:    "an-absurdly-long-workflow-name-nobody-would-actually-write-in-a-script",
		Elapsed:  90 * time.Second,
		Workflow: &core.WorkflowUpdate{Error: "a long error line that goes on for quite a while explaining exactly what went wrong in great detail"},
	}}
	for _, w := range []int{minBlockWidth, 20, roomWidth, 120} {
		b := roomBlock(ev, Agent{ID: "s1", Name: "sydney", Label: "a-very-long-branch-name-indeed"}, w, false)
		for _, line := range strings.Split(b.text, "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: a line measured %d: %q", w, got, line)
			}
		}
	}
}
