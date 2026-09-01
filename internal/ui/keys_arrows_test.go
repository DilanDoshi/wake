package ui

// Plain up/down walk the prompt history when the draft has no row to move into,
// and move the query cursor when it does. The roster moved to ⇧↑↓, and ⇧←→ move
// the keys between panes. See keys.go.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// newline inserts a hard line break the way alt+enter / ctrl+j does, so a test
// can build a genuinely multi-line draft through the same path a person types one.
func (a App) newline() App {
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	return m.(App)
}

// An empty or single-line query has no row for the cursor to move into, so ↑
// recalls the previous prompt - Claude Code's own history key - and the roster
// does not move for it.
func TestPlainArrowsWalkHistoryWhenQueryHasNoRowToMoveInto(t *testing.T) {
	for _, tc := range []struct {
		what  string
		build func(App) App
	}{
		{"empty query", func(a App) App { return a }},
		{"single-line query", func(a App) App { return a.withDraft("hello") }},
	} {
		a := spokenApp(t, "the older", "the newer")
		a.layout.ShowRoster = true
		a.roster.Selected = "s1"
		a = tc.build(a)

		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
		if got := a.composer().Value(); got != "the newer" {
			t.Errorf("%s: ↑ put %q in the draft, want the newest prompt recalled", tc.what, got)
		}
		if a.roster.Selected != "s1" {
			t.Errorf("%s: ↑ moved the roster to %q; the roster is ⇧↑↓ now", tc.what, a.roster.Selected)
		}
	}
}

// A multi-line query hands up/down to the text cursor and leaves the roster and
// the history alone - the recall is only offered when the cursor has nowhere to
// climb.
func TestPlainArrowsMoveCursorInsideMultiLineQuery(t *testing.T) {
	a := spokenApp(t, "an earlier prompt")
	a = a.withDraft("first").newline()
	a = a.withDraft("second") // draft is "first\nsecond", cursor at the end

	if got := a.composer().ta.Line(); got != 1 {
		t.Fatalf("draft did not end on its second line: cursor on line %d", got)
	}

	// Up climbs to the first line; the draft is untouched, so nothing was recalled.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := a.composer().ta.Line(); got != 0 {
		t.Errorf("up did not move the cursor up inside the draft: still on line %d", got)
	}
	if got := a.composer().Value(); got != "first\nsecond" {
		t.Errorf("up changed the draft to %q, so it walked the history instead of moving the cursor", got)
	}

	// Down returns to the second line, again without touching the draft.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if got := a.composer().ta.Line(); got != 1 {
		t.Errorf("down did not move the cursor down inside the draft: on line %d", got)
	}
	if got := a.composer().Value(); got != "first\nsecond" {
		t.Errorf("down changed the draft to %q while editing a multi-line draft", got)
	}
}

// At the top of a multi-line draft up has nowhere to move the cursor, so it
// falls through to the history - the same edge CanCursorUp encodes, one surface
// out.
func TestPlainUpAtTopOfDraftWalksHistory(t *testing.T) {
	a := spokenApp(t, "an earlier prompt")
	a = a.withDraft("first").newline()
	a = a.withDraft("second") // draft "first\nsecond", cursor on the last line

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp}) // onto the first line, draft intact
	if got := a.composer().Value(); got != "first\nsecond" {
		t.Fatalf("the first up should have moved the cursor, not walked history: draft is %q", got)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp}) // at the top edge now: history
	if got := a.composer().Value(); got != "an earlier prompt" {
		t.Errorf("up at the top of the draft did not walk the history: draft is %q", got)
	}
}

// A single-line draft is not the composer's cursor's however its runes group
// into graphemes. bubbles' edge CursorUp/CursorDown can shuffle the column
// within a multi-rune grapheme (a combining accent, a ZWJ sequence) without
// changing the row, so a predicate that watched the column would read that as
// vertical movement and nudge the cursor into the middle of the glyph. With no
// history behind it, ↑/↓ hold the draft exactly rather than nudging it. Graphemes
// are built from rune code points so the source carries no ambiguous literal.
func TestArrowsOnSingleLineGraphemesDoNotNudgeTheCursor(t *testing.T) {
	combining := "e" + string(rune(0x0301)) + "llo"                                    // e + combining acute -> single glyph
	zwj := string(rune(0x1F468)) + string(rune(0x200D)) + string(rune(0x1F4BB)) + " x" // man + ZWJ + laptop
	for _, tc := range []struct {
		what  string
		draft string
	}{
		{"a combining accent", combining},
		{"a ZWJ emoji sequence", zwj},
	} {
		for _, key := range []tea.KeyType{tea.KeyDown, tea.KeyUp} {
			a := newRoomApp(t).withSize(160, 40).withAgents("alex", "bo")
			a = a.withDraft(tc.draft) // one logical line, cursor at the end, no history
			a, _ = pressKey(a, tea.KeyMsg{Type: key})
			if got := a.composer().Value(); got != tc.draft {
				t.Errorf("%s with key %v: the draft became %q - a grapheme-boundary column shift was misread as vertical movement",
					tc.what, key, got)
			}
		}
	}
}

// ⇧+arrows never leak into the draft: ⇧↑↓ move the roster, ⇧←→ move the keys
// between panes. Whatever the query holds, the cursor does not move.
func TestShiftArrowsDoNotMoveTheQueryCursor(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex", "bo")
	a = a.withDraft("first").newline()
	a = a.withDraft("second") // cursor on line 1

	for _, key := range []tea.KeyType{tea.KeyShiftUp, tea.KeyShiftDown, tea.KeyShiftLeft, tea.KeyShiftRight} {
		next, _ := pressKey(a, tea.KeyMsg{Type: key})
		if got := next.composer().ta.Line(); got != 1 {
			t.Errorf("%v moved the query cursor to line %d: a ⇧+arrow must not edit the draft", key, got)
		}
	}
}
