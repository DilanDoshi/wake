package ui

// The arrow remap: bare ↑↓ walk the prompt history on an empty or single-line
// draft and move the text cursor on a multi-line one; the roster moves on ⇧↑↓.
// This is Claude Code's own ↑↓, which recall the previous prompt. See keys.go.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A bare ↑ on an empty composer brings back the last thing typed, and the walk
// cycles older on ↑ and newer on ↓.
func TestBareUpWalksThePromptHistory(t *testing.T) {
	a := spokenApp(t, "the first thing", "the second thing")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := a.composer().Value(); got != "the second thing" {
		t.Fatalf("↑ put %q in the draft, want the newest prompt back", got)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := a.composer().Value(); got != "the first thing" {
		t.Fatalf("a second ↑ put %q in the draft, want the one before it", got)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if got := a.composer().Value(); got != "the second thing" {
		t.Errorf("↓ put %q in the draft, want the walk to come back newer", got)
	}
}

// And in the room, whose history is its own echo of what was typed into it.
func TestBareUpWalksTheRoomHistory(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex")
	a = a.withDraft("@alex look at the tests")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter}) // sends; draft clears; leaves an echo
	if got := a.composer().Value(); got != "" {
		t.Fatalf("draft is still %q, so nothing was sent and there is no echo to walk", got)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := a.composer().Value(); got != "@alex look at the tests" {
		t.Errorf("↑ on an empty room composer put %q in the draft, want the message back with its mention", got)
	}
}

// ⇧↑/⇧↓ move the roster cursor - the job plain ↑↓ used to do.
func TestShiftArrowsMoveTheRoster(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("alex", "bo", "cy")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftDown})
	first := a.roster.Selected
	if first == "" {
		t.Fatal("⇧↓ did not move the roster")
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftDown})
	if a.roster.Selected == first {
		t.Errorf("a second ⇧↓ left the roster on %q", first)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftUp})
	if a.roster.Selected != first {
		t.Errorf("⇧↑ left the roster on %q, want it back on %q", a.roster.Selected, first)
	}
}

// A bare ↑ on a multi-line draft moves the text cursor, not the history: the
// recall is only offered when the cursor has nowhere to climb. The conversation
// holds a prompt, so a recall would be visible if one happened.
func TestBareUpMovesTheCursorInAMultiLineDraft(t *testing.T) {
	a := spokenApp(t, "something typed earlier")
	a = a.withDraft("first").newline()
	a = a.withDraft("second") // draft "first\nsecond", cursor on line 1

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := a.composer().ta.Line(); got != 0 {
		t.Errorf("↑ did not climb to the first line: cursor on line %d", got)
	}
	if got := a.composer().Value(); got != "first\nsecond" {
		t.Errorf("↑ changed the draft to %q, so it walked the history instead of moving the cursor", got)
	}
}
