// Forking a parent while a background subagent it launched is still running.
//
// The heartbeat fix (see TestASubagentsToolDoesNotKeepTheParentWorkingAfterIts-
// TurnEnds) makes such a parent read idle - its own turn ended, only the
// subagent is live. forkRefusal reads State/Name/ID alone (forkgate_test.go), so
// it cannot see the subagent and would accept the fork; forking a transcript a
// subagent is still writing is the unrecorded concurrent-flush case forkRefusal
// refuses a working parent for. So forkSource refuses it off the live agent, and
// the daemon tracks the dispatch lifecycle to know when it may again.

package daemon

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The dispatch lifecycle, folded: task_started opens the row, a progress frame
// (which names neither kind nor dispatch) leaves it running, and only an ending
// frame retires it - the id alone, since the ending carries no kind either.
func TestABackgroundSubagentIsTrackedAsRunningUntilItEnds(t *testing.T) {
	a := newAgent(idAlpha, "alex", "dev", "/repo/api", "", nil, func() {})

	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t1", Kind: core.TaskAgent, Dispatch: "toolu_1", Phase: core.TaskStarted, Status: core.TaskRunning}})
	if !a.hasRunningSubagent() {
		t.Fatal("a subagent that has started is not tracked as running")
	}

	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t1", Phase: core.TaskProgress, Status: core.TaskRunning}})
	if !a.hasRunningSubagent() {
		t.Fatal("a progress frame - which names no kind or dispatch - retired a subagent still running")
	}

	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t1", Phase: core.TaskEnded, Status: core.TaskDone}})
	if a.hasRunningSubagent() {
		t.Fatal("a subagent that ended is still tracked as running")
	}
}

// A background shell forwards nothing into the parent's conversation, so it is
// not the concurrent-write hazard a subagent is and is not tracked - the same
// distinction ui.Task.Openable makes, which a shell fails.
func TestABackgroundShellIsNotTrackedAsARunningSubagent(t *testing.T) {
	a := newAgent(idAlpha, "alex", "dev", "/repo/api", "", nil, func() {})
	a.observe(core.Event{Task: &core.TaskUpdate{ID: "sh1", Kind: core.TaskShell, Phase: core.TaskStarted, Status: core.TaskRunning}})
	if a.hasRunningSubagent() {
		t.Error("a background shell is tracked as a running subagent; it writes nothing into the parent's transcript")
	}
}

// A running workflow forwards nothing into the parent's own transcript
// either - its agents write only their own sidechain transcripts
// (2026-09-23-workflow-findings.md §3), the same distinction that excuses a
// background shell. So it is not tracked as a running subagent and a fork of
// an idle parent with one still running is not refused.
func TestARunningWorkflowDoesNotBlockAFork(t *testing.T) {
	a := newAgent(idAlpha, "alex", "dev", "/repo/api", "", nil, func() {})
	a.observe(core.Event{Task: &core.TaskUpdate{ID: "w1", Kind: core.TaskWorkflow, Phase: core.TaskStarted, Status: core.TaskRunning}})
	if a.hasRunningSubagent() {
		t.Error("a running workflow is tracked as a running subagent; it writes nothing into the parent's transcript")
	}
}

// Two concurrent subagents are two ids: the set is keyed on the task id, so one
// ending leaves the parent guarded while the other runs. The recorded shape is
// testdata/stream/subagent-parallel.jsonl.
func TestTwoConcurrentSubagentsAreTrackedIndependently(t *testing.T) {
	a := newAgent(idAlpha, "alex", "dev", "/repo/api", "", nil, func() {})
	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t1", Kind: core.TaskAgent, Dispatch: "toolu_1", Phase: core.TaskStarted, Status: core.TaskRunning}})
	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t2", Kind: core.TaskAgent, Dispatch: "toolu_2", Phase: core.TaskStarted, Status: core.TaskRunning}})

	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t1", Phase: core.TaskEnded, Status: core.TaskDone}})
	if !a.hasRunningSubagent() {
		t.Fatal("one of two concurrent subagents ended and the parent is unguarded, but the other is still running")
	}

	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t2", Phase: core.TaskEnded, Status: core.TaskDone}})
	if a.hasRunningSubagent() {
		t.Fatal("both subagents ended and the parent is still guarded")
	}
}

// The gate itself: an idle parent with a live background subagent is refused,
// and forkable again the moment the subagent ends.
func TestAForkIsRefusedWhileABackgroundSubagentRuns(t *testing.T) {
	s := newServer(tempSocket(t))
	a := newAgent(idAlpha, "alex", "dev", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	s.agents[idAlpha] = a

	if got := a.snapshot().State; got != rpc.StateIdle {
		t.Fatalf("the parent is %q, want idle - the case the guard exists for (a working parent is already refused)", got)
	}

	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t1", Kind: core.TaskAgent, Dispatch: "toolu_1", Phase: core.TaskStarted, Status: core.TaskRunning}})

	if _, err := s.forkSource(idAlpha); err == nil {
		t.Fatal("forkSource allowed a fork of an idle parent whose background subagent is still writing its transcript")
	} else if !strings.Contains(err.Error(), "subagent") || !strings.Contains(strings.ToLower(err.Error()), "stop") {
		t.Errorf("refusal = %q, want it to name the running subagent and a next step", err)
	}

	a.observe(core.Event{Task: &core.TaskUpdate{ID: "t1", Phase: core.TaskEnded, Status: core.TaskDone}})
	if _, err := s.forkSource(idAlpha); err != nil {
		t.Errorf("forkSource refused an idle parent after its subagent ended: %v", err)
	}
}
