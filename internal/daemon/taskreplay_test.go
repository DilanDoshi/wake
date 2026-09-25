// A running subagent shows in the right sidebar only from the live task_*
// system frames a client folds - and those are the one thing a late client
// cannot recover: Claude never writes them to the on-disk transcript, and the
// fleet report carries no running-dispatch field. So a reattach, a second
// window, or a bare wake reopening the room after a subagent started drew no
// row for it, and the operator had no visibility that the agent was dispatching
// at all.
//
// replayRunningTasks closes it for the *at-attach* case, the shape askreplay.go
// already takes for an outstanding ask: a newly accepted connection is handed
// each still-running dispatch's task_started as the ordinary rpc.FrameEvent a
// live client would have gotten, so its own Fleet.foldTask rebuilds the real
// sidebar row with no new client code and no wire field.

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// subagentDispatchFixture carries a task_started (task_type local_agent) before
// the subagent's own permission ask, so stopping the replay at it leaves the
// dispatch open with no ending frame.
const subagentDispatchFixture = "subagent-permission.jsonl"

// fakeSubagentDispatch replays the recording up to and including its
// task_started and then holds - no ending frame, reading stdin to exhaustion -
// so the dispatch is genuinely running while a test attaches a second client.
func fakeSubagentDispatch() int {
	lines := readFixture(subagentDispatchFixture)
	at := -1
	for i, line := range lines {
		if strings.Contains(line, `"subtype":"task_started"`) {
			at = i
			break
		}
	}
	if at < 0 {
		fmt.Fprintf(os.Stderr, "fake: %s holds no task_started\n", subagentDispatchFixture)
		os.Exit(1)
	}
	emitLines(lines[:at+1])
	for range stdinLines() {
	}
	return 0
}

// TestRunningTaskFramesCarryTheStartedDispatch is the unit half: what an agent
// hands back for replay is the task_started it decoded - carrying the Kind and
// Dispatch a sidebar row needs to be openable - addressed by the agent's own id
// the way fanOut addresses a live one, and dropped the moment the dispatch ends.
func TestRunningTaskFramesCarryTheStartedDispatch(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	if got := a.runningTaskFrames(); got != nil {
		t.Fatalf("a freshly built agent already has %d dispatch frames to replay", len(got))
	}

	started := core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: "task-1", Dispatch: "toolu_1", Kind: core.TaskAgent,
		Phase: core.TaskStarted, Status: core.TaskRunning, Type: "general-purpose",
	}}
	a.observe(started)

	frames := a.runningTaskFrames()
	if len(frames) != 1 {
		t.Fatalf("runningTaskFrames() returned %d frames, want 1", len(frames))
	}
	f := frames[0]
	if f.Kind != rpc.FrameEvent || f.SessionID != idAlpha {
		t.Fatalf("frame = %+v, want a FrameEvent addressed to %q", f, idAlpha)
	}
	if f.Event == nil || f.Event.Task == nil {
		t.Fatalf("event = %+v, want the started dispatch intact", f.Event)
	}
	// The two fields Openable() reads: without them the client folds a row it
	// will never draw in the sidebar (rostersubs.go / fleettasks.go).
	if f.Event.Task.Phase != core.TaskStarted || f.Event.Task.Kind != core.TaskAgent || f.Event.Task.Dispatch != "toolu_1" {
		t.Fatalf("the replayed dispatch lost what makes it an openable running row: %+v", f.Event.Task)
	}

	// The ending drops it: a client attaching after the subagent finished has no
	// running row to rebuild, the same rule RunningTasks itself follows.
	a.observe(core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: "task-1", Phase: core.TaskEnded, Status: core.TaskDone,
	}})
	if got := a.runningTaskFrames(); got != nil {
		t.Fatalf("a finished dispatch is still offered for replay: %d frames", len(got))
	}
}

// TestASessionResetForgetsRunningDispatches: a /clear drops the pre-clear
// conversation, so a dispatch it held must not be replayed to a client that
// attaches afterwards - the mirror of internal/ui's Fleet.Observe dropping
// f.tasks on a reset, without which a late client draws a stale running row.
func TestASessionResetForgetsRunningDispatches(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.observe(core.Event{Kind: core.KindSystem, Task: &core.TaskUpdate{
		ID: "task-1", Dispatch: "toolu_1", Kind: core.TaskAgent, Phase: core.TaskStarted, Status: core.TaskRunning,
	}})
	if len(a.runningTaskFrames()) != 1 {
		t.Fatalf("the dispatch was not retained before the reset")
	}
	a.observe(core.Event{Kind: core.KindSessionReset})
	if got := a.runningTaskFrames(); got != nil {
		t.Fatalf("a reset left %d dispatches to replay, want none", len(got))
	}
}

// TestASecondClientAttachingMidDispatchSeesItRunning is the end-to-end half: a
// real daemon replaying a recorded subagent that starts a dispatch and then
// blocks on a permission ask - so the dispatch is genuinely running, never
// ending, while a second client dials in. Before replayRunningTasks that client
// learned nothing about the dispatch: task_* frames are live-only and the report
// carries no running-task field, so its sidebar stayed empty while the agent
// worked. See rpc.SessionStatus (no task field) and docs/notes/bugs.md BUG-33
// (task_* never reach the on-disk transcript).
func TestASecondClientAttachingMidDispatchSeesItRunning(t *testing.T) {
	replayingClaudeOnPath(t, "subagent")
	d := startDaemon(t)

	c1 := attach(t, d.socket)
	c1.spawn(idAlpha, "sydney")
	// Gate on the live task_started: once this client has it, the daemon has
	// observed and retained it, and the fake holds the dispatch open from here.
	c1.await("the dispatch starting live", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha &&
			f.Event != nil && f.Event.Task != nil && f.Event.Task.Phase == core.TaskStarted
	})

	// Attaches after the dispatch is already running - nothing about it has been
	// read live by this connection.
	c2 := attach(t, d.socket)
	replayed := c2.await("the running dispatch replayed at attach", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha &&
			f.Event != nil && f.Event.Task != nil && f.Event.Task.Phase == core.TaskStarted
	})
	task := replayed.Event.Task
	if task.Kind != core.TaskAgent || task.Dispatch == "" {
		t.Fatalf("the replayed dispatch is not an openable running row: %+v - the sidebar would draw nothing", task)
	}
}

// TestAReplayedWorkflowCarriesItsLatestSnapshot: a workflow's task_started
// carries no progress at all (it precedes every task_progress), so replaying
// it unchanged would hand a late client a dispatch with an empty sidebar -
// no phase, no agent, nothing running. withWorkflow keeps the retained row
// current as each task_progress arrives, the way a live client's own fold
// already is.
func TestAReplayedWorkflowCarriesItsLatestSnapshot(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	var lastSnapshot *core.WorkflowSnapshot
	progressSeen := 0
	for _, ev := range decodeStreamFixture(t, "workflow-run.jsonl") {
		if ev.Task == nil {
			continue
		}
		a.observe(ev)
		if ev.Task.Workflow != nil && ev.Task.Workflow.Progress != nil {
			lastSnapshot = ev.Task.Workflow.Progress
		}
		if ev.Task.Phase == core.TaskProgress {
			progressSeen++
			if progressSeen == 3 {
				break
			}
		}
	}
	if progressSeen != 3 {
		t.Fatalf("the fixture carried %d task_progress frames, want at least 3", progressSeen)
	}
	if lastSnapshot == nil {
		t.Fatal("no task_progress before the third carried a snapshot to replay")
	}

	frames := a.runningTaskFrames()
	if len(frames) != 1 {
		t.Fatalf("runningTaskFrames() = %d frames, want 1", len(frames))
	}
	task := frames[0].Event.Task
	if task.Phase != core.TaskStarted || task.Kind != core.TaskWorkflow || task.Workflow == nil || task.Workflow.Name != "count-lines" {
		t.Fatalf("replayed task = %+v, want the started count-lines workflow", task)
	}
	if !reflect.DeepEqual(task.Workflow.Progress, lastSnapshot) {
		t.Fatalf("replayed snapshot = %+v, want the latest observed %+v", task.Workflow.Progress, lastSnapshot)
	}
}

// TestATaskProgressSnapshotIsIgnoredForARetainedNonWorkflowTask: withWorkflow
// dereferences the *retained* started event's own Workflow, and a task_started
// this agent retained for a non-workflow dispatch (or a malformed one) never
// had one. A task_progress that still carries a snapshot for that id - a
// wire shape nothing here is known to produce, but not one observe may take
// on faith - must be ignored rather than crash the fan-out goroutine that
// would otherwise take the whole daemon down with it (no recover in
// internal/daemon).
func TestATaskProgressSnapshotIsIgnoredForARetainedNonWorkflowTask(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	started := core.Event{Task: &core.TaskUpdate{
		ID: "t1", Dispatch: "toolu_1", Kind: core.TaskAgent, Phase: core.TaskStarted, Status: core.TaskRunning,
	}}
	a.observe(started)

	progress := core.Event{Task: &core.TaskUpdate{
		ID: "t1", Phase: core.TaskProgress, Status: core.TaskRunning,
		Workflow: &core.WorkflowUpdate{Progress: &core.WorkflowSnapshot{Phases: []core.WorkflowPhase{{Index: 1, Title: "Count"}}}},
	}}
	a.observe(progress) // must not panic

	frames := a.runningTaskFrames()
	if len(frames) != 1 {
		t.Fatalf("runningTaskFrames() = %d frames, want 1", len(frames))
	}
	if !reflect.DeepEqual(*frames[0].Event, started) {
		t.Fatalf("retained event = %+v, want it untouched by a snapshot its own kind cannot carry: %+v", *frames[0].Event, started)
	}
}

// decodeStreamFixture decodes a recorded stream-json fixture directly off
// testdata/stream, in arrival order - the same corpus internal/core's own
// tests read, reached here rather than through readFixture's env var because
// that one only exists for the fake claude subprocess to find its own script.
func decodeStreamFixture(t *testing.T, name string) []core.Event {
	t.Helper()
	path := filepath.Join(fixtureDir(t), name)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var out []core.Event
	for _, line := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		out = append(out, evs...)
	}
	return out
}
