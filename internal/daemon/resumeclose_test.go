package daemon

// claude's close of a turn left with no reply, written at a --resume (BUG-65),
// is read by nothing: History - a reopened conversation and the room's restore
// alike - leaves it out, and it is no prompt to rewind to.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

const resumeClose = "No response requested."

func resumeFixture(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestHistoryLeavesOutAResumesClose(t *testing.T) {
	for _, name := range []string{"resume-interrupted.jsonl", "resume-killed.jsonl"} {
		t.Run(name, func(t *testing.T) {
			plantTranscript(t, histID, resumeFixture(t, name)...)
			events, err := History(histID)
			if err != nil {
				t.Fatalf("History: %v", err)
			}
			var prompts []string
			reply := false
			for _, ev := range events {
				if ev.Text == resumeClose {
					t.Errorf("the resume's close came back as a %s", ev.Kind)
				}
				if ev.Kind == core.KindUserText && ev.Notice == "" {
					prompts = append(prompts, ev.Text)
				}
				reply = reply || (ev.Kind == core.KindAssistantText && ev.Text == "finished")
			}
			// The prompt before the close and the one after it, both kept.
			if len(prompts) != 2 || prompts[1] != "continue" || !reply {
				t.Errorf("prompts = %q, agent's reply kept = %v; want the first prompt, \"continue\" and the reply", prompts, reply)
			}
		})
	}
}

func TestAResumesCloseIsNoRewindTarget(t *testing.T) {
	plantTranscript(t, histID, resumeFixture(t, "resume-interrupted.jsonl")...)
	targets, err := RewindTargets(histID)
	if err != nil {
		t.Fatalf("RewindTargets: %v", err)
	}
	if len(targets) != 2 || targets[1].Text != "continue" {
		t.Errorf("targets = %+v, want the first prompt and \"continue\"", targets)
	}
}
