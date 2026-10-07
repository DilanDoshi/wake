package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A send-now and a take-back each reach claude as the line recorded doing what
// they are for (testdata/input/midturn-now-human.stdin.jsonl,
// midturn-cancel.stdin.jsonl), through the agent's own input queue. The fake
// echoes every line it does not answer.
func TestASendNowAndARecallReachTheAgentAsRecorded(t *testing.T) {
	fakeClaudeOnPath(t, "mode")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "steer", MessageID: "m1", Now: true})
	c.awaitEvent(idAlpha, `"uuid":"m1","priority":"now","origin":{"kind":"human"}`)

	c.send(rpc.Frame{Kind: rpc.FrameRecall, SessionID: idAlpha, RequestID: "r1", MessageID: "m1"})
	c.awaitEvent(idAlpha, `"request_id":"r1","request":{"subtype":"cancel_async_message","message_uuid":"m1"}`)
}

// absorbedTranscript is testdata/transcript/midturn-absorbed.jsonl with the
// mid-turn message chained straight onto the opening turn (on disk it hangs
// off attachment records the fixture leaves out), so both are on the branch.
func absorbedTranscript(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "midturn-absorbed.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return strings.Replace(string(data), "ecf67b5a-62f4-4d42-a78f-ac6192be32b4", "29f1d565-3815-47c3-b9d2-eb97fc2b984d", 1)
}

// A message claude took up mid-turn is stored as an attachment, not a user
// record, and rewinding to one is unrecorded - so it is offered as no target.
func TestAMessageTakenUpMidTurnIsNoRewindTarget(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)
	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	id := uuid.NewString()
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(absorbedTranscript(t)), 0o600); err != nil {
		t.Fatal(err)
	}

	c.send(rpc.Frame{Kind: rpc.FrameRewindTargets, SessionID: id})
	f := c.await("the rewind targets reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameRewindTargetsReply && f.SessionID == id
	})
	if len(f.RewindTargets) != 1 || f.RewindTargets[0].UUID != "29f1d565-3815-47c3-b9d2-eb97fc2b984d" {
		t.Errorf("rewind targets = %+v, want only the opening turn", f.RewindTargets)
	}
}

// A fork copies the mid-turn message's record, and the room's history knows it
// by the stamp it carries - so the set a fork's inherited records are left out
// by holds that stamp as well as the record's own uuid.
func TestARecordsStampIsAmongTheUUIDsAForkInherits(t *testing.T) {
	got, err := recordUUIDs(strings.NewReader(absorbedTranscript(t)))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"29f1d565-3815-47c3-b9d2-eb97fc2b984d", "c2ca34b2-ac6b-459b-bda5-f119436a07ed", "ce8c17b6-1b7d-41d9-b48d-7592a1a19cdc"} {
		if !got[want] {
			t.Errorf("recordUUIDs lacks %s: %v", want, got)
		}
	}
}
