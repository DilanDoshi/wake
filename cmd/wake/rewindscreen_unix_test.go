//go:build unix

// The rewind picker, end to end: a real terminal, a real daemon reading a
// real (planted) transcript tree-aware, and a genuine rewind_conversation
// control request round-tripped through a scripted claude.
//
// The in-process tests in internal/ui prove the gate and the fold against
// tea.KeyMsg values and a hand-built rpc.Frame - this is the seam none of
// them can see: that `esc esc` on a real pty actually opens the picker drawn
// from the daemon's own disk read, and that picking a row carries a real
// wire round trip back into the composer. historyscreen_unix_test.go is the
// model for planting a transcript at the id the daemon minted.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The active-branch prompts testdata/transcript/rewind-tree.jsonl holds, and
// their uuids - internal/daemon/rewindtargets_test.go's own
// wantFirstUUID/wantLastUUID, restated here because the two packages share no
// test constants. The fixture rewinds past a "remember 42" turn and its
// answer, so the active branch is exactly these two prompts: the first turn
// and the post-rewind continuation, not the turn the rewind left behind.
const (
	rewindFixtureOldestUUID = "f4579490-9d9c-4bbb-ab21-eb956db99945"
	rewindFixtureOldestText = "Remember the number 7. Reply with exactly: ok"
	rewindFixtureNewestText = "List every number I asked you to remember, digits only, comma separated."
)

// TestEscEscOpensARewindPickerOfTheRealActiveBranch presses the real key
// bytes against a real daemon and a planted transcript, picks the older of
// two real prompts, and asserts the composer carries what a genuine
// rewind_conversation receipt said back - proving the picker's own uuid, not
// only that a receipt arrived at all.
func TestEscEscOpensARewindPickerOfTheRealActiveBranch(t *testing.T) {
	withScriptedAgent(t, scriptRewinds)
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	// The id is the daemon's own mint, so the transcript is found the way a
	// real one would be: by filename, never by a path this test chose.
	id := liveSessionID(t, name)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "rewind-tree.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(fixture), "\n"), "\n")
	plantedTranscript(t, projects, id, lines...)

	// Idle - the agent's opening turn already answered "ready" - and empty:
	// the first esc arms harmlessly (nothing is running and nothing is
	// typed), and the second opens the picker rather than clearing a draft
	// that does not exist. Two sends rather than one, so this is the slow
	// press; escscreen_unix_test.go covers the collapsed one for the
	// mechanism this reuses.
	s.send("\x1b")
	s.settle()
	s.send("\x1b")

	// Both active-branch prompts, drawn from the daemon's own tree-aware read
	// of the planted file - not from anything this client remembers saying.
	s.await(rewindFixtureNewestText)
	s.await(rewindFixtureOldestText)

	// Down from the cursor's resting place (newest, index 0) onto the older
	// prompt, then confirm. Moving first is what makes this prove the
	// picker's uuid mapping rather than only that index 0 happens to work.
	s.send("\x1b[B") // ↓
	s.settle()
	s.send("\r") // ↵

	// scriptRewinds' receipt carries the *target* uuid back inside
	// prefillText, so finding it in the composer proves the specific prompt
	// this test moved onto - not merely that some receipt landed.
	s.await(rewindPrefillPrefix + rewindFixtureOldestUUID)
}
