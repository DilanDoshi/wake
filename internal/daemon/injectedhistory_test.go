package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// injectedFixtures are transcripts of lines claude wrote into a conversation
// itself, each keyed by the uuid its first line names as parent: a typed turn
// planted under that uuid puts every fixture line on the active branch, so a
// line dropped here is dropped by the decoder, not by the rewind walk.
var injectedFixtures = map[string]string{
	"subagent-handback.jsonl": "33333333-3333-4333-8333-333333333333",
	"injected-meta.jsonl":     "77777777-0000-4000-8000-000000000000",
}

const typedTurn = "the only turn the operator typed"

func injectedTranscript(t *testing.T, fixture, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", fixture))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	typed := fmt.Sprintf(`{"type":"user","isSidechain":false,"uuid":%q,"timestamp":"2026-10-03T00:00:00.000Z","message":{"role":"user","content":[{"type":"text","text":%q}]}}`, root, typedTurn)
	return append([]string{typed}, strings.Split(strings.TrimRight(string(data), "\n"), "\n")...)
}

// A reopened conversation shows the operator's own turns and nothing claude
// injected as one - a subagent's hand-back, a skill's body, an idle notice.
func TestARestoredConversationHasNoInjectedTurn(t *testing.T) {
	for fixture, root := range injectedFixtures {
		t.Run(fixture, func(t *testing.T) {
			plantTranscript(t, histID, injectedTranscript(t, fixture, root)...)
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
			if len(turns) != 1 || turns[0] != typedTurn {
				t.Errorf("restored user turns = %q, want only %q", turns, typedTurn)
			}
		})
	}
}

// The rewind picker offers the operator's prompts only: an injected line is
// not a point anybody typed, so it is never a target.
func TestRewindTargetsOfferNoInjectedTurn(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	d := startDaemon(t)
	c := attach(t, d.socket)
	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for fixture, root := range injectedFixtures {
		id := uuid.NewString()
		lines := injectedTranscript(t, fixture, root)
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatalf("write transcript: %v", err)
		}
		c.send(rpc.Frame{Kind: rpc.FrameRewindTargets, SessionID: id})
		f := c.await("the rewind targets reply", func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameRewindTargetsReply && f.SessionID == id
		})
		if len(f.RewindTargets) != 1 || f.RewindTargets[0].UUID != root {
			t.Errorf("%s: rewind targets = %+v, want only the typed turn %s", fixture, f.RewindTargets, root)
		}
	}
}
