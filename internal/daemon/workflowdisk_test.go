// A dynamic Workflow() run's own records and its agents' own transcripts,
// read back off claude's disk - workflowdisk.go's counterpart to
// history_test.go, one directory over.

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const wfID = "66666666-7777-8888-9999-000000000000"

// runRecordA is a hand-trimmed copy of the failed run's own wf_*.json record
// used by internal/core/workflow_test.go's runRecordFixture (task
// wsmc7r0xw) - copied rather than imported, since a _test.go const is not
// visible outside its own package.
const runRecordA = `{
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
		{"type": "workflow_agent", "index": 1, "label": "echo red", "phaseIndex": 1, "phaseTitle": "Fan", "agentId": "ad377aac14ee2acea", "model": "claude-haiku-4-5-20251001", "state": "done", "tokens": 15194}
	]
}`

// runRecordB is a later, successful run - a distinct task id and a startTime
// after runRecordA's, so WorkflowRuns' newest-first order is checkable.
const runRecordB = `{
	"taskId": "wsucceeded01",
	"workflowName": "count-lines",
	"summary": "Counted the lines",
	"status": "completed",
	"startTime": 1790224400000,
	"durationMs": 4200,
	"totalTokens": 5100,
	"script": "phase('Count')\n",
	"workflowProgress": []
}`

// workflowSessionDir plants a transcript for id and returns the session
// directory beside it - the tree claudeSessionDir points at - creating
// workflows/ and subagents/workflows/ under it.
func workflowSessionDir(t *testing.T, id string) string {
	t.Helper()
	path := plantTranscript(t, id, userLine("hello"))
	dir := strings.TrimSuffix(path, ".jsonl")
	if err := os.MkdirAll(filepath.Join(dir, "workflows"), 0o700); err != nil {
		t.Fatalf("mkdir workflows: %v", err)
	}
	return dir
}

func writeWorkflowFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, "workflows", name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// plantWorkflowAgentTranscript copies testdata/transcript/workflow-agent.jsonl
// (every line isSidechain:true) to <dir>/subagents/workflows/<run>/agent-<id>.jsonl.
func plantWorkflowAgentTranscript(t *testing.T, dir, run, agentID string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "workflow-agent.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	agentDir := filepath.Join(dir, "subagents", "workflows", run)
	if err := os.MkdirAll(agentDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", agentDir, err)
	}
	path := filepath.Join(agentDir, "agent-"+agentID+".jsonl")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestWorkflowRunsOrdersNewestFirstAndSkipsACorruptRecord plants two valid
// records and one that is not JSON at all, and checks the valid two come
// back newest first with the corrupt one silently dropped.
func TestWorkflowRunsOrdersNewestFirstAndSkipsACorruptRecord(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	writeWorkflowFile(t, dir, "wf_a.json", runRecordA)
	writeWorkflowFile(t, dir, "wf_b.json", runRecordB)
	writeWorkflowFile(t, dir, "wf_c.json", "{not json at all")

	runs, err := WorkflowRuns(wfID)
	if err != nil {
		t.Fatalf("WorkflowRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("read %d runs, want 2 (the corrupt record must be skipped, not fatal): %+v", len(runs), runs)
	}
	if runs[0].TaskID != "wsucceeded01" || runs[1].TaskID != "wsmc7r0xw" {
		t.Errorf("order = [%s, %s], want the later run (wsucceeded01) before the earlier one (wsmc7r0xw)", runs[0].TaskID, runs[1].TaskID)
	}
}

// TestWorkflowRunsSkipsASymlinkedRecord proves a symlinked wf_*.json is
// refused the way history.go refuses a symlinked transcript - Lstat, regular
// files only, so a run record cannot be read from anywhere on the machine.
func TestWorkflowRunsSkipsASymlinkedRecord(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	writeWorkflowFile(t, dir, "wf_a.json", runRecordA)

	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(outside, []byte(runRecordB), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	link := filepath.Join(dir, "workflows", "wf_b.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	runs, err := WorkflowRuns(wfID)
	if err != nil {
		t.Fatalf("WorkflowRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].TaskID != "wsmc7r0xw" {
		t.Fatalf("runs = %+v, want only the real record: a symlinked run record was read", runs)
	}
}

// TestWorkflowAgentHistoryReturnsTheSidechainEvents checks the count against
// what core.DecodeSidechainLine itself gives back for the same fixture,
// rather than a hardcoded number - the fixture's own event count is not
// this test's claim to make.
func TestWorkflowAgentHistoryReturnsTheSidechainEvents(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	plantWorkflowAgentTranscript(t, dir, "wf_a", "abc123")

	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "workflow-agent.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	want := 0
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		evs, err := core.DecodeSidechainLine([]byte(line))
		if err != nil {
			t.Fatalf("DecodeSidechainLine: %v", err)
		}
		want += len(evs)
	}
	if want == 0 {
		t.Fatal("the fixture itself decodes to no events: this test asserts nothing")
	}

	events, err := WorkflowAgentHistory(wfID, "abc123")
	if err != nil {
		t.Fatalf("WorkflowAgentHistory: %v", err)
	}
	if len(events) != want {
		t.Fatalf("read %d events, want %d (core.DecodeSidechainLine's own count for this fixture)", len(events), want)
	}
	for i, ev := range events {
		if ev.SessionID != wfID {
			t.Errorf("event %d carries session %q, want %q", i, ev.SessionID, wfID)
		}
	}
}

// TestWorkflowAgentHistoryRefusesATraversalID proves an agent id is fenced
// by ValidWorkflowAgentID before it ever reaches a path.
func TestWorkflowAgentHistoryRefusesATraversalID(t *testing.T) {
	workflowSessionDir(t, wfID)

	if _, err := WorkflowAgentHistory(wfID, "../x"); err == nil {
		t.Fatal("WorkflowAgentHistory accepted \"../x\" as an agent id")
	}
}

// TestWorkflowAgentHistoryOfAnUnknownAgentIsEmptyAndNotAnError mirrors
// History's own ruling for a session with no transcript: the caller draws
// nothing, which is what it would have drawn anyway.
func TestWorkflowAgentHistoryOfAnUnknownAgentIsEmptyAndNotAnError(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	plantWorkflowAgentTranscript(t, dir, "wf_a", "abc123")

	events, err := WorkflowAgentHistory(wfID, "nosuchagent")
	if err != nil {
		t.Fatalf("WorkflowAgentHistory of an unknown agent failed: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("read %d events for an unknown agent", len(events))
	}
}

// TestTheDaemonAnswersAWorkflowsFrame drives FrameWorkflows through dispatch
// over a real socket, the way history_test.go's own dispatch test does for
// FrameHistory.
func TestTheDaemonAnswersAWorkflowsFrame(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := workflowSessionDir(t, wfID)
	writeWorkflowFile(t, dir, "wf_a.json", runRecordA)

	c.send(rpc.Frame{Kind: rpc.FrameWorkflows, SessionID: wfID})
	f := c.await("the workflows reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameWorkflowsReply && f.SessionID == wfID
	})
	if f.Workflow == nil || len(f.Workflow.Runs) != 1 || f.Workflow.Runs[0].TaskID != "wsmc7r0xw" {
		t.Fatalf("workflows reply = %+v, want one run (wsmc7r0xw)", f.Workflow)
	}
}

// TestTheDaemonAnswersAWorkflowAgentFrame drives FrameWorkflowAgent through
// dispatch, checking the reply echoes the agent id it was asked for and
// carries that agent's own transcript.
func TestTheDaemonAnswersAWorkflowAgentFrame(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := workflowSessionDir(t, wfID)
	plantWorkflowAgentTranscript(t, dir, "wf_a", "abc123")

	c.send(rpc.Frame{Kind: rpc.FrameWorkflowAgent, SessionID: wfID, Workflow: &rpc.WorkflowFrame{Agent: "abc123"}})
	f := c.await("the workflow agent reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameWorkflowAgentReply && f.SessionID == wfID
	})
	if f.Workflow == nil || f.Workflow.Agent != "abc123" {
		t.Fatalf("workflow agent reply echoes %+v, want agent \"abc123\"", f.Workflow)
	}
	if len(f.Events) == 0 {
		t.Fatal("the workflow agent reply carries no events")
	}
}

// TestWorkflowRunsSkipsARecordThroughASymlinkedIntermediateDirectory proves
// containment is checked on the whole resolved path, not just the final
// component: regularTranscript's Lstat follows a symlinked *directory* to
// reach the file it names and reports it an ordinary regular file, which is
// exactly what let a symlinked workflows/ read a record from anywhere on the
// machine before withinSessionDir existed.
func TestWorkflowRunsSkipsARecordThroughASymlinkedIntermediateDirectory(t *testing.T) {
	path := plantTranscript(t, wfID, userLine("hello"))
	sessionDir := strings.TrimSuffix(path, ".jsonl")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", sessionDir, err)
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "wf_a.json"), []byte(runRecordA), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// workflows/ itself is the symlink, not a file under it.
	if err := os.Symlink(outside, filepath.Join(sessionDir, "workflows")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	runs, err := WorkflowRuns(wfID)
	if err != nil {
		t.Fatalf("WorkflowRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("runs = %+v, want none: a record reached through a symlinked workflows/ directory was read", runs)
	}
}

// TestWorkflowAgentHistoryDoesNotFollowASymlinkedIntermediateDirectory is
// the same escape one level deeper: a symlinked run directory under
// subagents/workflows/ rather than a symlinked file.
func TestWorkflowAgentHistoryDoesNotFollowASymlinkedIntermediateDirectory(t *testing.T) {
	path := plantTranscript(t, wfID, userLine("hello"))
	sessionDir := strings.TrimSuffix(path, ".jsonl")

	outsideRun := t.TempDir()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "workflow-agent.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outsideRun, "agent-abc123.jsonl"), raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	subagentsWorkflows := filepath.Join(sessionDir, "subagents", "workflows")
	if err := os.MkdirAll(subagentsWorkflows, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// wf_a itself is the symlink - "evil -> outsideRun" in the reviewer's terms.
	if err := os.Symlink(outsideRun, filepath.Join(subagentsWorkflows, "wf_a")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	events, err := WorkflowAgentHistory(wfID, "abc123")
	if err != nil {
		t.Fatalf("WorkflowAgentHistory: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("read %d events through a symlinked run directory, want 0: the escape was not caught", len(events))
	}
}

// TestWorkflowAgentHistoryStampsTheClientFacingSessionID plants the agent
// transcript under a *different* id than the one the client asked with -
// simulating a /clear the way history_test.go's
// TestAClearedSessionReadsTheTranscriptClaudeIsWritingNow does - and checks
// the reply's events carry the id the client asked with, not the translated
// one WorkflowAgentHistory read the file under.
func TestWorkflowAgentHistoryStampsTheClientFacingSessionID(t *testing.T) {
	s := newServer(filepath.Join(t.TempDir(), "s"))
	a := &agent{id: wfID, name: "alex"}
	s.agents[wfID] = a

	const cleared = "77777777-8888-9999-0000-111111111111"
	a.observe(core.Event{Kind: core.KindAssistantText, SessionID: cleared, Text: "after the clear"})
	if got := s.transcriptID(wfID); got != cleared {
		t.Fatalf("transcriptID = %q, want %q: the fixture below is planted under this id", got, cleared)
	}

	dir := workflowSessionDir(t, cleared)
	plantWorkflowAgentTranscript(t, dir, "wf_a", "abc123")

	c := newClient(nil)
	s.sendWorkflowAgent(c, wfID, "abc123")

	var f rpc.Frame
	select {
	case f = <-c.out:
	default:
		t.Fatal("sendWorkflowAgent enqueued nothing")
	}
	if f.Kind != rpc.FrameWorkflowAgentReply || f.SessionID != wfID {
		t.Fatalf("reply = %+v, want FrameWorkflowAgentReply addressed to %q", f, wfID)
	}
	if len(f.Events) == 0 {
		t.Fatal("the reply carries no events")
	}
	for i, ev := range f.Events {
		if ev.SessionID != wfID {
			t.Errorf("event %d carries session %q, want the client-facing %q (not the translated claude id %q)",
				i, ev.SessionID, wfID, cleared)
		}
	}
}
