//go:build live

// The wire half of `make live`, beside cmd/wake's TestLiveJourney: one real
// headless claude, spawned with this package's argv in auto mode as the daemon
// spawns an agent, asked to do the thing whose wire shape moved under Wake
// unrecorded (BUG-42) - hand a background subagent's report back. It checks the
// decode, not the screen: on stdout and on the transcript the same session
// wrote, only the operator's own prompt may come back as the operator's turn,
// and every frame must be a shape the corpus has recorded.
//
// Not a gate: it spends money (one short turn and one follow-up) and needs a
// login. It fails unless both turns ran; a subagent that ends by
// task-notification instead of a hand-back - the model's choice - is logged.

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
	started := false
	t.Cleanup(func() {
		// The process goes first: a claude still flushing would write back a
		// transcript removed before it exited.
		cancel()
		if started {
			for range s.Events() { // closes once the process is gone
			}
		}
		removeLiveTranscript(t, id)
	})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("start claude: %v", err)
	}
	started = true
	if err := s.Send(liveWirePrompt, nil, ""); err != nil {
		t.Fatalf("send: %v", err)
	}
	stream, turns := liveEvents(t, s)
	if err := s.Err(); err != nil || turns < 2 {
		t.Fatalf("saw %d turn ends, want the prompt's and the one the subagent's ending opens: the scenario never ran (claude: %v)", turns, err)
	}
	onStdout, onDisk := checkLiveStream(t, stream), checkLiveTranscript(t, id)
	if onStdout != onDisk {
		t.Errorf("a hand-back on stdout: %v, on disk: %v - the two wires disagree", onStdout, onDisk)
	}
	if !onStdout && !onDisk {
		// The model chose task-notification: everything above held, but the
		// path this probe exists for went unexercised, so it is not a pass.
		t.Skip("inconclusive: the subagent ended by task-notification, not a hand-back - run it again")
	}
}

// checkLiveStream holds stdout to what the corpus says claude sends, and
// reports whether a hand-back arrived on it.
func checkLiveStream(t *testing.T, stream []Event) (handedBack bool) {
	recorded, unrecorded := recordedShapes(t), map[string]bool{}
	for _, ev := range stream {
		handedBack = handedBack || bytes.Contains(ev.Raw, []byte(`"handback":true`))
		switch {
		case operatorTurn(ev):
			t.Errorf("stdout: claude's own frame decoded as the operator's turn: %.80q", ev.Text)
		case ev.Kind == KindUnknown:
			t.Errorf("stdout: a %s frame decoded to nothing Wake knows", shapeOf(ev.Raw))
		case ev.Kind == KindAPIError:
			t.Errorf("stdout: the API refused a turn: %.80q", ev.Text)
		}
		if shape := shapeOf(ev.Raw); ev.Raw != nil && !recorded[shape] {
			unrecorded[shape] = true
		}
	}
	// Reported, not failed: a shape nothing reads costs nothing. It is the list
	// to record from before a feature reads one.
	t.Logf("frame shapes no recording carries: %v", slices.Sorted(maps.Keys(unrecorded)))
	return handedBack
}

// checkLiveTranscript holds the transcript the session wrote to the same rule,
// and reports whether the subagent handed its report back.
func checkLiveTranscript(t *testing.T, id string) (handedBack bool) {
	var typed []string
	for _, line := range liveTranscript(t, id) {
		handedBack = handedBack || bytes.Contains(line, []byte(`"handback":true`))
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
	return handedBack
}

// liveEvents collects until the follow-up turn ends, answering any ask with a
// deny so the run stays the one dispatch it asked for.
func liveEvents(t *testing.T, s *Session) (out []Event, turns int) {
	var follow <-chan time.Time
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				return out, turns
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
// probe's temp project made. A failure, or anything of the probe's still there
// afterwards, fails the test.
func removeLiveTranscript(t *testing.T, id string) {
	path := liveTranscriptPath(t, id)
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	for _, p := range []string{path, filepath.Join(dir, id)} {
		if err := os.RemoveAll(p); err != nil {
			t.Errorf("remove %s: %v", p, err)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("the probe left %s behind", p)
		}
	}
	// claude also makes an empty memory directory per project. os.Remove only
	// ever takes an empty directory, so nothing the operator wrote can go; the
	// directory is the probe's own temp project's, so anything left is reported.
	_ = os.Remove(filepath.Join(dir, "memory"))
	if err := os.Remove(dir); err != nil && !os.IsNotExist(err) {
		left, _ := os.ReadDir(dir)
		names := make([]string, 0, len(left))
		for _, e := range left {
			names = append(names, e.Name())
		}
		t.Errorf("the probe's project directory %s is left holding %v: %v", dir, names, err)
	}
}
