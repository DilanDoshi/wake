package daemon

// The daemon's answer to "what could this session be rewound to": its
// active-branch user prompts, uuid and text, in file order. FrameRewind's own
// RewindTarget and RewindLastSeen are a transcript message's own uuid, and
// core.Event carries neither - this is where the UI gets one.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The fixture (testdata/transcript/rewind-tree.jsonl) rewinds past a "Now
// also remember the number 42" turn and its "7,42" answer, then continues
// with a second "List every number..." turn off the pre-rewind leaf. The
// active branch therefore holds exactly two user prompts: the first turn, and
// the post-rewind continuation - not the rewound turn in between, and not the
// rewound branch's own copy of "List every number...".
func TestRewindTargetsAreTheActiveBranchUserPrompts(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)

	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "rewind-tree.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")

	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := uuid.NewString()
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	c.send(rpc.Frame{Kind: rpc.FrameRewindTargets, SessionID: id})
	f := c.await("the rewind targets reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameRewindTargetsReply && f.SessionID == id
	})

	const (
		wantFirstUUID = "f4579490-9d9c-4bbb-ab21-eb956db99945"
		wantFirstText = "Remember the number 7. Reply with exactly: ok"
		wantLastUUID  = "54bef517-1e4c-48a2-a09b-0fc6fbac2841"
		wantLastText  = "List every number I asked you to remember, digits only, comma separated."
	)

	if len(f.RewindTargets) != 2 {
		t.Fatalf("got %d rewind targets, want 2 (the two active-branch user prompts): %+v", len(f.RewindTargets), f.RewindTargets)
	}
	// Oldest first: the first turn of the conversation is the first target.
	if got := f.RewindTargets[0]; got.UUID != wantFirstUUID || got.Text != wantFirstText {
		t.Errorf("first target = %+v, want {%s %s}", got, wantFirstUUID, wantFirstText)
	}
	// The last entry is the newest active user turn - the last_seen tip a
	// FrameRewind's RewindLastSeen wants.
	if got := f.RewindTargets[len(f.RewindTargets)-1]; got.UUID != wantLastUUID || got.Text != wantLastText {
		t.Errorf("last target (the last_seen tip) = %+v, want {%s %s}", got, wantLastUUID, wantLastText)
	}

	for _, target := range f.RewindTargets {
		if target.UUID == "" {
			t.Errorf("a rewind target carries no uuid: %+v", f.RewindTargets)
		}
		if strings.Contains(target.Text, "Now also remember the number 42") {
			t.Errorf("the rewound turn's text reached the reply: %+v", target)
		}
	}
}

// A transcript with more active-branch user turns than rewindTargetsMax is
// answered with exactly the newest rewindTargetsMax of them, and LastSeen -
// the last_seen tip a FrameRewind's RewindLastSeen wants - is still the true
// newest prompt, not merely the newest one the bound kept room for.
func TestRewindTargetsIsBoundedToTheNewestEntries(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)

	const turns = rewindTargetsMax + 30
	id := uuid.NewString()
	lines, want := linearTranscript(id, turns)

	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	c.send(rpc.Frame{Kind: rpc.FrameRewindTargets, SessionID: id})
	f := c.await("the bounded rewind targets reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameRewindTargetsReply && f.SessionID == id
	})

	if len(f.RewindTargets) != rewindTargetsMax {
		t.Fatalf("got %d rewind targets, want the bound of %d", len(f.RewindTargets), rewindTargetsMax)
	}
	wantTail := want[len(want)-rewindTargetsMax:]
	for i, target := range f.RewindTargets {
		if target != wantTail[i] {
			t.Errorf("target %d = %+v, want %+v (the newest %d, oldest first)", i, target, wantTail[i], rewindTargetsMax)
		}
	}
	if got, wantNewest := f.RewindTargets[len(f.RewindTargets)-1].UUID, want[len(want)-1].UUID; got != wantNewest {
		t.Errorf("the last entry (the last_seen tip) has uuid %s, want the true newest %s", got, wantNewest)
	}
}

// linearTranscript builds turns user/assistant pairs chained by parentUuid -
// a straight line with no rewind marker, so core.ActiveBranch's own "with no
// rewind, start from the last node written" puts every one of them on the
// active branch. It returns the file's lines, oldest first, and the user
// prompts RewindTargets should answer with, in the same order.
func linearTranscript(sessionID string, turns int) ([]string, []rpc.RewindTarget) {
	var lines []string
	var want []rpc.RewindTarget
	parent := ""
	for i := range turns {
		userUUID := uuid.NewString()
		text := fmt.Sprintf("prompt %d", i)
		lines = append(lines, fmt.Sprintf(
			`{"parentUuid":%q,"isSidechain":false,"type":"user","message":{"role":"user","content":[{"type":"text","text":%q}]},"uuid":%q,"timestamp":"2026-08-26T00:00:00.000Z","sessionId":%q}`,
			parent, text, userUUID, sessionID))
		want = append(want, rpc.RewindTarget{UUID: userUUID, Text: text})

		replyUUID := uuid.NewString()
		lines = append(lines, fmt.Sprintf(
			`{"parentUuid":%q,"isSidechain":false,"message":{"role":"assistant","content":[{"type":"text","text":"ok"}]},"type":"assistant","uuid":%q,"timestamp":"2026-08-26T00:00:00.000Z","sessionId":%q}`,
			userUUID, replyUUID, sessionID))
		parent = replyUUID
	}
	return lines, want
}

// A session with no transcript is answered with an empty list, not an error -
// on History's own terms: a session that has never taken a turn has nothing
// to rewind to, and the caller draws nothing, which is what it would have
// drawn anyway.
func TestRewindTargetsWithNoTranscriptIsEmptyAndNotAnError(t *testing.T) {
	fakeClaudeOnPath(t, "")
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	d := startDaemon(t)
	c := attach(t, d.socket)

	id := uuid.NewString()
	c.send(rpc.Frame{Kind: rpc.FrameRewindTargets, SessionID: id})
	f := c.await("the empty rewind targets reply", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameRewindTargetsReply && f.SessionID == id
	})
	if len(f.RewindTargets) != 0 {
		t.Errorf("a session with no transcript was answered with %d rewind targets", len(f.RewindTargets))
	}
}
