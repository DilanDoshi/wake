package daemon

// The room's own question about a transcript, over a real socket.
//
// It reads the same file FrameHistory reads and answers under a different kind,
// and the reason is a ledger on the other side rather than a payload here: the
// client asks once per session per surface, and one shared answer would leave
// whichever surface asked second holding nothing. See internal/ui/history.go.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestTheDaemonAnswersARoomHistoryFrameAboutTheSessionItNames(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	first, second := uuid.NewString(), uuid.NewString()
	for id, text := range map[string]string{first: "what the first said", second: "what the second said"} {
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(userLine(text)+"\n"), 0o600); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
	}

	for id, want := range map[string]string{first: "what the first said", second: "what the second said"} {
		c.send(rpc.Frame{Kind: rpc.FrameRoomHistory, SessionID: id})
		f := c.await("the room history reply for "+id, func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameRoomHistoryReply && f.SessionID == id
		})
		if len(f.Events) != 1 {
			t.Fatalf("the reply for %s carries %d events, want 1", id, len(f.Events))
		}
		if f.Events[0].Text != want {
			t.Errorf("the reply for %s carries %q, want %q", id, f.Events[0].Text, want)
		}
		if f.Events[0].SessionID != id {
			t.Errorf("the reply's event is stamped %q, want the id the client asked with", f.Events[0].SessionID)
		}
	}
}

// A session with no transcript is answered rather than ignored: the room is
// waiting on this ask before it folds anything, and a reply that never comes
// is a group chat that stays empty for a reason nothing reports.
func TestTheDaemonAnswersARoomHistoryFrameWithNoTranscript(t *testing.T) {
	fakeClaudeOnPath(t, "")
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	d := startDaemon(t)
	c := attach(t, d.socket)

	id := uuid.NewString()
	c.send(rpc.Frame{Kind: rpc.FrameRoomHistory, SessionID: id})
	f := c.await("the empty room history reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameRoomHistoryReply && f.SessionID == id
	})
	if len(f.Events) != 0 {
		t.Errorf("a session with no transcript was answered with %d events", len(f.Events))
	}
}

// The two questions do not answer each other. A client holding two ledgers
// would fill the wrong one, and the surface that asked would sit empty while
// the other drew the conversation twice.
func TestTheTwoHistoryQuestionsAnswerUnderTheirOwnKinds(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := uuid.NewString()
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(userLine("said once")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	c.send(rpc.Frame{Kind: rpc.FrameHistory, SessionID: id})
	c.await("the conversation's reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameHistoryReply && f.SessionID == id
	})
	c.send(rpc.Frame{Kind: rpc.FrameRoomHistory, SessionID: id})
	c.await("the room's reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameRoomHistoryReply && f.SessionID == id
	})
}
