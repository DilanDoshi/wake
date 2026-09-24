// Stopping a running dynamic Workflow() from the client side.
//
// FrameStopRun addresses a task id rather than a session id, and
// runningWorkflow's own Kind check is the whole gate - so the daemon has to
// refuse an id that never named a task, a subagent's own id (a different
// dispatch kind), and a workflow that has already ended, and reach only the
// one still running. Over the same live-process harness apply_test.go's own
// FrameRewind tests use, because the positive case has to prove the control
// request crossed the real stdin pipe.

package daemon

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fakeWorkflowFleet starts three dispatches on one session - a running
// workflow ("w1"), a running subagent ("s1"), and a workflow that starts and
// immediately ends ("w2") - then behaves like fakeTurns, so a control request
// written to its stdin afterward is echoed back the way
// TestFrameRewindWritesTheControlRequest reads one.
func fakeWorkflowFleet(sid string) int {
	emitText(sid, "ready")
	emitResult(sid)
	emitTaskStarted(sid, "w1", "local_workflow", "count-lines")
	emitTaskStarted(sid, "s1", "local_agent", "")
	emitTaskStarted(sid, "w2", "local_workflow", "temp")
	emitTaskEnded(sid, "w2")
	for line := range stdinLines() {
		emitText(sid, "echo: "+line)
		emitResult(sid)
	}
	return 0
}

func emitTaskStarted(sid, taskID, taskType, workflowName string) {
	fmt.Printf(`{"type":"system","subtype":"task_started","session_id":%q,"task_id":%q,`+
		`"tool_use_id":"toolu_%s","description":"probe","task_type":%q,"workflow_name":%q}`+"\n",
		sid, taskID, taskID, taskType, workflowName)
}

func emitTaskEnded(sid, taskID string) {
	fmt.Printf(`{"type":"system","subtype":"task_updated","session_id":%q,"task_id":%q,"status":"completed"}`+"\n",
		sid, taskID)
}

// TestStopRunReachesOnlyARunningWorkflow: an unknown id, a running subagent's
// id and an already-ended workflow's id are all refused with no write to the
// process; only the running workflow's own id reaches its stdin as a
// stop_task control request.
func TestStopRunReachesOnlyARunningWorkflow(t *testing.T) {
	fakeClaudeOnPath(t, "stoprun")
	d := startDaemon(t)
	c := attach(t, d.socket)

	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")
	// Gate on the last dispatch this fake emits: by the time this client has
	// seen w2 end, the daemon's own observe has already folded w1 and s1 too
	// (fanOut observes before it broadcasts), so every case below is racing
	// nothing.
	c.await("w2 to end", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha &&
			f.Event != nil && f.Event.Task != nil && f.Event.Task.ID == "w2" && f.Event.Task.Phase == core.TaskEnded
	})

	for _, id := range []string{"nope", "s1", "w2"} {
		c.send(rpc.Frame{Kind: rpc.FrameStopRun, SessionID: idAlpha, Workflow: &rpc.WorkflowFrame{Task: id}})
		c.await("a refusal naming "+id, func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameError && strings.Contains(f.Text, "no running workflow "+id)
		})
	}
	st := c.status()
	if len(live(st)) != 1 {
		t.Fatalf("status = %+v, want the session untouched by three refusals", st.Sessions)
	}

	c.send(rpc.Frame{Kind: rpc.FrameStopRun, SessionID: idAlpha, Workflow: &rpc.WorkflowFrame{Task: "w1"}})
	got := c.await("the stop_task control request echoed back", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == idAlpha && f.Event != nil &&
			strings.Contains(f.Event.Text, "stop_task")
	})
	if !strings.Contains(got.Event.Text, `"task_id":"w1"`) {
		t.Fatalf("stdin did not carry the stop_task request: %s", got.Event.Text)
	}
}
