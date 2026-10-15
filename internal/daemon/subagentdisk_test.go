// A plain subagent's own transcript, read back off claude's disk -
// workflowdisk_test.go's sibling for the file one directory up:
// <session>/subagents/agent-<id>.jsonl rather than
// <session>/subagents/workflows/<run>/agent-<id>.jsonl.

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// subAgentID has the shape of every recorded task_id (17 lower-case hex).
const subAgentID = "a14cc764b35e62ba1"

// sidechainFixture is the recorded on-disk transcript of one agent, every line
// isSidechain:true - the same format a plain subagent's own file has.
func sidechainFixture(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "workflow-agent.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return raw
}

// plantSubagentTranscript writes the fixture to <dir>/subagents/agent-<id>.jsonl
// and returns that path.
func plantSubagentTranscript(t *testing.T, dir, agentID string) string {
	t.Helper()
	subagents := filepath.Join(dir, "subagents")
	if err := os.MkdirAll(subagents, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", subagents, err)
	}
	path := filepath.Join(subagents, "agent-"+agentID+".jsonl")
	if err := os.WriteFile(path, sidechainFixture(t), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// askFor is the request a client writes for one agent's transcript.
func askFor(kind, session, agentID string, ask uint64) rpc.Frame {
	return rpc.Frame{Kind: kind, SessionID: session, Workflow: &rpc.WorkflowFrame{Agent: agentID, Ask: ask}}
}

// decodedCount is how many events the fixture decodes to, so no test here
// hardcodes a number that is the fixture's to state.
func decodedCount(t *testing.T) int {
	t.Helper()
	n := 0
	for _, line := range strings.Split(strings.TrimRight(string(sidechainFixture(t)), "\n"), "\n") {
		evs, err := core.DecodeSidechainLine([]byte(line))
		if err != nil {
			t.Fatalf("DecodeSidechainLine: %v", err)
		}
		n += len(evs)
	}
	if n == 0 {
		t.Fatal("the fixture decodes to no events: these tests would assert nothing")
	}
	return n
}

// The bug: a pane opened on a subagent this client never watched speak had
// nothing to draw, and the only copy of its words was this file.
func TestAgentHistoryReadsAPlainSubagentsTranscript(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	plantSubagentTranscript(t, dir, subAgentID)

	events, err := AgentHistory(wfID, subAgentID)
	if err != nil {
		t.Fatalf("AgentHistory: %v", err)
	}
	if want := decodedCount(t); len(events) != want {
		t.Fatalf("read %d events, want %d (core.DecodeSidechainLine's own count)", len(events), want)
	}
}

// A symlinked transcript is never followed, the way history.go refuses a
// symlinked conversation: a subagent's file cannot be a route to any file on
// the machine.
func TestAgentHistoryDoesNotFollowASymlinkedPlainSubagentTranscript(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	outside := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	if err := os.WriteFile(outside, sidechainFixture(t), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "subagents"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "subagents", "agent-"+subAgentID+".jsonl")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	events, err := AgentHistory(wfID, subAgentID)
	if err != nil {
		t.Fatalf("AgentHistory: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("read %d events through a symlinked transcript, want 0", len(events))
	}
}

// The same escape one level up: subagents/ itself a symlink out of the session
// directory.
func TestAgentHistoryDoesNotFollowASymlinkedSubagentsDirectory(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "agent-"+subAgentID+".jsonl"), sidechainFixture(t), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "subagents")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	events, err := AgentHistory(wfID, subAgentID)
	if err != nil {
		t.Fatalf("AgentHistory: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("read %d events through a symlinked subagents directory, want 0", len(events))
	}
}

// FrameSubagent is answered under its own reply kind, echoing the agent id it
// was asked for and carrying that subagent's transcript - the workflow pair's
// contract, not a reuse of its kinds.
func TestTheDaemonAnswersASubagentFrame(t *testing.T) {
	fakeClaudeOnPath(t, "")
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := workflowSessionDir(t, wfID)
	plantSubagentTranscript(t, dir, subAgentID)

	c.send(rpc.Frame{Kind: rpc.FrameSubagent, SessionID: wfID, Workflow: &rpc.WorkflowFrame{Agent: subAgentID}})
	f := c.await("the subagent reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameSubagentReply && f.SessionID == wfID
	})
	if f.Workflow == nil || f.Workflow.Agent != subAgentID {
		t.Fatalf("subagent reply echoes %+v, want agent %q", f.Workflow, subAgentID)
	}
	if want := decodedCount(t); len(f.Events) != want {
		t.Fatalf("subagent reply carries %d events, want %d", len(f.Events), want)
	}
}

// The ask number comes back on the reply, so a client holding two asks for one
// agent can tell which an answer is for.
func TestTheReplyEchoesTheAskNumber(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	plantSubagentTranscript(t, dir, subAgentID)
	s := newServer(filepath.Join(t.TempDir(), "s"))
	c := newClient(nil)

	s.sendAgentTranscript(c, askFor(rpc.FrameSubagent, wfID, subAgentID, 7))
	if f := sentFrame(t, c); f.Kind != rpc.FrameSubagentReply || f.Workflow == nil || f.Workflow.Ask != 7 {
		t.Fatalf("reply = %+v (workflow %+v), want a FrameSubagentReply echoing ask 7", f, f.Workflow)
	}
}

// A malformed id is the request's fault and is refused before it reaches a
// path - the fence is the one the workflow frame already has.
func TestTheDaemonRefusesASubagentFrameWithAMalformedID(t *testing.T) {
	fakeClaudeOnPath(t, "")
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	d := startDaemon(t)
	c := attach(t, d.socket)
	workflowSessionDir(t, wfID)

	c.send(rpc.Frame{Kind: rpc.FrameSubagent, SessionID: wfID, Workflow: &rpc.WorkflowFrame{Agent: "../x"}})
	f := c.await("the refusal", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "agent id") {
		t.Errorf("refusal reads %q, want the id fence's own reason (not, say, an unknown frame kind)", f.Text)
	}
}

// A tool call's command is not Text, so trimRing's accounting never counted it:
// forty 400 KB commands framed past rpc's 16 MiB cap, which ends the client's
// connection when the pane opens. The reply is bounded by what it encodes to,
// and keeps the newest events.
func TestAnAgentTranscriptReplyFitsTheFrameWhateverItsToolCallsCarry(t *testing.T) {
	dir := workflowSessionDir(t, wfID)
	big := strings.Repeat("x", 400<<10)
	var lines []string
	for i := range 40 {
		lines = append(lines, fmt.Sprintf(`{"isSidechain":true,"agentId":%q,"type":"assistant","uuid":"big-%d",`+
			`"message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_big_%d","name":"Bash","input":{"command":%q}}]}}`,
			subAgentID, i, i, big))
	}
	if err := os.MkdirAll(filepath.Join(dir, "subagents"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "subagents", "agent-"+subAgentID+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	s := newServer(filepath.Join(t.TempDir(), "s"))
	c := newClient(nil)
	s.sendAgentTranscript(c, askFor(rpc.FrameSubagent, wfID, subAgentID, 0))

	f := sentFrame(t, c)
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if limit := historyBytes + 64<<10; len(raw) > limit {
		t.Errorf("the reply encodes to %d bytes, want at most %d: it would end the client's connection", len(raw), limit)
	}
	if n := len(f.Events); n == 0 || f.Events[n-1].Tool == nil || f.Events[n-1].Tool.ID != "toolu_big_39" {
		t.Errorf("the reply does not end on the newest event: %d events", n)
	}
}
