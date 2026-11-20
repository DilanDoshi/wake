package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// loopTranscript is a /loop conversation as claude wrote it (2.1.329): the /loop
// the operator typed, its first tick, and the user record claude wrote for the
// wakeup turn it started itself. Returns its lines and the typed turn's uuid.
func loopTranscript(t *testing.T) ([]string, string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "loop-wakeup-turn.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	var first struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("fixture's first line: %v", err)
	}
	return lines, first.UUID
}

// A reopened /loop conversation shows the /loop the operator typed once: the user
// record claude wrote for each wakeup is its own turn, not another typed one.
func TestARestoredLoopConversationHasNoWakeupTurn(t *testing.T) {
	lines, _ := loopTranscript(t)
	plantTranscript(t, histID, lines...)
	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var turns []string
	for _, ev := range events {
		if ev.Kind == core.KindUserText {
			turns = append(turns, ev.Text)
		}
	}
	if len(turns) != 1 {
		t.Errorf("restored user turns = %q, want the one /loop the operator typed", turns)
	}
}

// The rewind picker offers the /loop the operator typed and not the wakeups
// claude started: nobody typed those, so they are no point to rewind to.
func TestRewindTargetsOfferNoLoopWakeupTurn(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)
	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lines, typed := loopTranscript(t)
	id := uuid.NewString()
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	c.send(rpc.Frame{Kind: rpc.FrameRewindTargets, SessionID: id})
	f := c.await("the rewind targets reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameRewindTargetsReply && f.SessionID == id
	})
	if len(f.RewindTargets) != 1 || f.RewindTargets[0].UUID != typed {
		t.Errorf("rewind targets = %+v, want only the typed /loop %s", f.RewindTargets, typed)
	}
}
