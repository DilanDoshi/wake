//go:build live

// The wire half of `make live`, beside cmd/wake's TestLiveJourney: one real
// headless claude, spawned with this package's argv in auto mode as the daemon
// spawns an agent, asked to do the thing whose wire shape moved under Wake
// unrecorded (BUG-41) - hand a background subagent's report back. It checks the
// decode, not the screen: on stdout and on the transcript the same session
// wrote, only the operator's own prompt may come back as the operator's turn,
// and every frame must be a shape the corpus has recorded.
//
// Not a gate: it spends money (one short turn and one follow-up) and needs a
// login. A subagent may end by task-notification instead of a hand-back - the
// model chooses - so that path is logged as not exercised, never failed.

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	liveWireBudget = 6 * time.Minute
	// liveWireFollowUp bounds the wait for the turn the subagent's ending opens.
	liveWireFollowUp = 3 * time.Minute

	liveWirePrompt = "Use the Agent tool exactly once: subagent_type general-purpose, run_in_background true, " +
		"description 'wire probe', prompt 'Write a two-line report, REPORT then DONE, and deliver it with the tool " +
		"that hands your report back. Use no other tool.'. Then end your turn. When the report arrives, acknowledge it in one word."
)

func TestLiveWire(t *testing.T) {
	id := uuid.NewString()
	s := NewSession(Config{SessionID: id, Name: "wire-probe", Dir: t.TempDir(), Model: "sonnet"})
	ctx, cancel := context.WithTimeout(context.Background(), liveWireBudget)
	defer cancel()
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start claude: %v", err)
	}
	t.Cleanup(func() { removeLiveTranscript(t, id) })
	if err := s.Send(liveWirePrompt, nil, ""); err != nil {
		t.Fatalf("send: %v", err)
	}

	stream := liveEvents(t, s)
	recorded, unrecorded := recordedShapes(t), map[string]bool{}
	for _, ev := range stream {
		if operatorTurn(ev) {
			t.Errorf("stdout: claude's own frame decoded as the operator's turn: %.80q", ev.Text)
		}
		if ev.Kind == KindUnknown {
			t.Errorf("stdout: a %s frame decoded to nothing Wake knows", shapeOf(ev.Raw))
		}
		if shape := shapeOf(ev.Raw); ev.Raw != nil && !recorded[shape] {
			unrecorded[shape] = true
		}
	}
	// Not a failure: a shape nothing reads costs nothing. It is the list to
	// record from before a feature reads one.
	t.Logf("frame shapes no recording carries: %v", slices.Sorted(maps.Keys(unrecorded)))

	lines := liveTranscript(t, id)
	var typed []string
	for _, line := range lines {
		evs, err := DecodeTranscriptLine(line)
		if err != nil {
			t.Errorf("transcript: %v", err)
		}
		for _, ev := range evs {
			if operatorTurn(ev) {
				typed = append(typed, ev.Text)
			}
		}
	}
	if len(typed) != 1 || typed[0] != liveWirePrompt {
		t.Errorf("transcript: operator turns restored = %.80q, want only the prompt this test sent", typed)
	}
	handedBack := false
	for _, line := range lines {
		handedBack = handedBack || bytes.Contains(line, []byte(`"handback":true`))
	}
	t.Logf("%d stdout events, %d transcript lines; subagent handed back: %v (false means it ended by task-notification and that path went unexercised)",
		len(stream), len(lines), handedBack)
}

// liveEvents collects until the follow-up turn ends, answering any ask with a
// deny so the run stays the one dispatch it asked for.
func liveEvents(t *testing.T, s *Session) []Event {
	var out []Event
	turns := 0
	var follow <-chan time.Time
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return out
			}
			out = append(out, ev)
			switch ev.Kind {
			case KindPermissionRequest:
				t.Logf("denied an ask the probe did not expect: %s", ev.RequestID)
				_ = s.DenyTool(ev.RequestID, "the wire probe allows nothing")
			case KindTurnEnd:
				turns++
				if turns == 1 {
					follow = time.After(liveWireFollowUp)
				} else {
					_ = s.Stop()
				}
			}
		case <-follow:
			t.Logf("no follow-up turn within %s", liveWireFollowUp)
			_ = s.Stop()
			follow = nil
		}
	}
}

// shapeOf names a frame by its type and subtype, the census unit.
func shapeOf(raw []byte) string {
	var f struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
	}
	_ = json.Unmarshal(raw, &f)
	return f.Type + "/" + f.Subtype
}

func recordedShapes(t *testing.T) map[string]bool {
	shapes := map[string]bool{}
	for _, f := range fixtureFiles(t) {
		for _, line := range fixtureLines(t, f) {
			shapes[shapeOf([]byte(line))] = true
		}
	}
	return shapes
}

func liveTranscriptPath(t *testing.T, id string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	found, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", id+".jsonl"))
	if len(found) != 1 {
		return ""
	}
	return found[0]
}

func liveTranscript(t *testing.T, id string) [][]byte {
	path := liveTranscriptPath(t, id)
	if path == "" {
		t.Fatalf("no transcript for %s under ~/.claude/projects", id)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	return bytes.Split(bytes.TrimSpace(data), []byte("\n"))
}

// removeLiveTranscript leaves the operator's projects directory as it was:
// the session's own file, its subagent directory, and the slug directory the
// probe's temp project made, if nothing else is in it.
func removeLiveTranscript(t *testing.T, id string) {
	path := liveTranscriptPath(t, id)
	if path == "" {
		return
	}
	for _, p := range []string{path, filepath.Join(filepath.Dir(path), id)} {
		if err := os.RemoveAll(p); err != nil {
			t.Logf("leave %s: %v", p, err)
		}
	}
	// claude also makes an empty memory directory per project. os.Remove only
	// ever takes an empty directory, so nothing the operator wrote can go.
	_ = os.Remove(filepath.Join(filepath.Dir(path), "memory"))
	_ = os.Remove(filepath.Dir(path))
}
