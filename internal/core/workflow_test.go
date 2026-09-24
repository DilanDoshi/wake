// The workflow dimension of the airlock: a dynamic Workflow() run, decoded
// through the same five task_* subtypes an ordinary subagent uses, and told
// apart by TaskUpdate.Kind and the fields only it carries.
//
// Fixtures recorded 2026-09-23 against 2.1.281; see
// docs/superpowers/notes/2026-09-23-workflow-findings.md.

package core

import (
	"path/filepath"
	"strings"
	"testing"
)

// fixtureTasks decodes one fixture in arrival order and keeps only the
// events that carry a task, so every helper below reads a short list rather
// than filtering a whole fixture itself.
func fixtureTasks(t *testing.T, fixture string) []*TaskUpdate {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "stream", fixture)
	var out []*TaskUpdate
	for _, d := range decodeFixture(t, path) {
		if d.Task != nil {
			out = append(out, d.Task)
		}
	}
	return out
}

// firstTask returns the first task frame at the given phase, and fails the
// test if the fixture never reaches it.
func firstTask(t *testing.T, fixture string, phase TaskPhase) *TaskUpdate {
	t.Helper()
	for _, task := range fixtureTasks(t, fixture) {
		if task.Phase == phase {
			return task
		}
	}
	t.Fatalf("%s: no task frame at phase %q", fixture, phase)
	return nil
}

// lastSnapshot returns the most recent workflow_progress snapshot in a
// fixture - every one is whole, so the last is also the most resolved.
func lastSnapshot(t *testing.T, fixture string) WorkflowSnapshot {
	t.Helper()
	var last *WorkflowSnapshot
	for _, task := range fixtureTasks(t, fixture) {
		if task.Workflow != nil && task.Workflow.Progress != nil {
			last = task.Workflow.Progress
		}
	}
	if last == nil {
		t.Fatalf("%s: no workflow snapshot in this fixture", fixture)
	}
	return *last
}

// endings returns every task_updated/task_notification frame, in arrival
// order - a workflow's run ends on both, one after the other.
func endings(t *testing.T, fixture string) []*TaskUpdate {
	t.Helper()
	var out []*TaskUpdate
	for _, task := range fixtureTasks(t, fixture) {
		if task.Phase == TaskEnded {
			out = append(out, task)
		}
	}
	return out
}

// allTasks returns every task frame in a fixture, in arrival order.
func allTasks(t *testing.T, fixture string) []*TaskUpdate {
	t.Helper()
	return fixtureTasks(t, fixture)
}

func TestAWorkflowStartsAsAWorkflowAndNamesItself(t *testing.T) {
	start := firstTask(t, "workflow-run.jsonl", TaskStarted)
	if start.Kind != TaskWorkflow || start.Workflow == nil || start.Workflow.Name != "count-lines" {
		t.Fatalf("start = %+v, want a workflow named count-lines", start)
	}
	if !strings.HasPrefix(start.Workflow.Script, "export const meta") {
		t.Fatalf("script = %.40q, want the script task_started carries", start.Workflow.Script)
	}
}

func TestEveryWorkflowSnapshotIsWholeAndResolved(t *testing.T) {
	last := lastSnapshot(t, "workflow-failed.jsonl")
	if len(last.Phases) != 3 || len(last.Agents) != 7 {
		t.Fatalf("phases/agents = %d/%d, want 3/7", len(last.Phases), len(last.Agents))
	}
	for _, a := range last.Agents {
		if a.State != WorkflowAgentDone || a.AgentID == "" || a.Phase == 0 {
			t.Fatalf("agent %+v: want done, with an id and a phase", a)
		}
	}
	if got := last.Done(); got != 7 {
		t.Fatalf("Done() = %d, want 7", got)
	}
}

func TestAFailedWorkflowCarriesItsError(t *testing.T) {
	end := endings(t, "workflow-failed.jsonl")
	if end[0].Status != TaskFailed || end[0].Workflow == nil || !strings.Contains(end[0].Workflow.Error, "deliberate probe failure") {
		t.Fatalf("task_updated = %+v, want failed with the error", end[0])
	}
	if end[1].Status != TaskFailed {
		t.Fatalf("task_notification status = %q, want failed", end[1].Status)
	}
}

func TestAStoppedWorkflowEndsHalted(t *testing.T) {
	for _, u := range endings(t, "workflow-stop.jsonl") {
		if u.Status != TaskStopped {
			t.Fatalf("ending %+v, want TaskStopped (killed/stopped)", u)
		}
	}
}

func TestANonWorkflowTaskCarriesNoWorkflow(t *testing.T) {
	for _, u := range allTasks(t, "subagent-background.jsonl") {
		if u.Workflow != nil {
			t.Fatalf("%+v: a subagent frame carries no workflow", u)
		}
	}
}

// TestASidechainLineDecodesWhereATranscriptLineDoesNot is DecodeSidechainLine's
// whole reason: a workflow agent forwards nothing live (findings.md §3), so its
// words exist only in its own on-disk transcript, whose lines are all
// isSidechain:true - the one shape DecodeTranscriptLine drops.
func TestASidechainLineDecodesWhereATranscriptLineDoesNot(t *testing.T) {
	var side, plain int
	for _, line := range fixtureLines(t, "../../testdata/transcript/workflow-agent.jsonl") {
		evs, err := DecodeSidechainLine([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		side += len(evs)
		p, _ := DecodeTranscriptLine([]byte(line))
		plain += len(p)
	}
	if side == 0 || plain != 0 {
		t.Fatalf("sidechain %d events, transcript %d; want >0 and 0", side, plain)
	}
}

// runRecordFixture is a hand-trimmed, scrubbed copy of the failed run's own
// wf_*.json record (task wsmc7r0xw, workflow-failed.jsonl): the keys are
// copied verbatim from the recording, with every path dropped and the
// snapshot cut to one phase and two agents.
const runRecordFixture = `{
	"taskId": "wsmc7r0xw",
	"workflowName": "wide-then-fail",
	"summary": "Six parallel echo agents, one reducer, then a deliberate failure",
	"status": "failed",
	"error": "Error: deliberate probe failure\n    at <anonymous> (workflow.js:8:7)",
	"startTime": 1790224377827,
	"durationMs": 10402,
	"totalTokens": 106361,
	"script": "phase('Fan')\n",
	"workflowProgress": [
		{"type": "workflow_phase", "index": 1, "title": "Fan"},
		{"type": "workflow_agent", "index": 1, "label": "echo red", "phaseIndex": 1, "phaseTitle": "Fan", "agentId": "ad377aac14ee2acea", "model": "claude-haiku-4-5-20251001", "state": "done", "tokens": 15194},
		{"type": "workflow_agent", "index": 2, "label": "echo green", "phaseIndex": 1, "phaseTitle": "Fan", "agentId": "aa81ac9dab30431e5", "model": "claude-haiku-4-5-20251001", "state": "done", "tokens": 15192}
	]
}`

func TestARunRecordDecodes(t *testing.T) {
	run, err := DecodeWorkflowRun([]byte(runRecordFixture))
	if err != nil || run.TaskID != "wsmc7r0xw" || run.Status != TaskFailed || run.Error == "" ||
		run.Progress == nil || len(run.Progress.Agents) != 2 || run.Started.IsZero() {
		t.Fatalf("run = %+v err = %v", run, err)
	}
}
