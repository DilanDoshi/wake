package ui

// Plain up/down move the query cursor when the draft has a row to move into,
// and the roster otherwise. Shift+arrows stay the panes'. See keys.go.

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

// An empty or single-line query keeps up/down on the roster: there is no cursor
// movement to make, so the arrow does the thing it did before this change.
func TestPlainArrowsMoveRosterWhenQueryHasNoRowToMoveInto(t *testing.T) {
	for _, tc := range []struct {
		what  string
		build func(App) App
	}{
		{"empty query", func(a App) App { return a }},
		{"single-line query", func(a App) App { return a.withDraft("hello") }},
	} {
		a := newRoomApp(t).withSize(160, 40).withAgents("alex", "bo", "cy")
		a = tc.build(a)
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
		if a.roster.Selected == "" {
			t.Errorf("%s: down did not move the roster, so a query that cannot take the cursor lost its roster nav", tc.what)
		}
	}
}

// A multi-line query hands up/down to the text cursor and leaves the roster
// where it was - the whole point of the remap.
func TestPlainArrowsMoveCursorInsideMultiLineQuery(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex", "bo")
	a = a.withDraft("first").newline()
	a = a.withDraft("second") // draft is "first\nsecond", cursor at the end

	if got := a.composer().ta.Line(); got != 1 {
		t.Fatalf("draft did not end on its second line: cursor on line %d", got)
	}
	before := a.roster.Selected

	// Up climbs to the first line; the roster does not move.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := a.composer().ta.Line(); got != 0 {
		t.Errorf("up did not move the cursor up inside the draft: still on line %d", got)
	}
	if a.roster.Selected != before {
		t.Errorf("up moved the roster from %q to %q while editing a multi-line draft", before, a.roster.Selected)
	}

	// Down returns to the second line; the roster still does not move.
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if got := a.composer().ta.Line(); got != 1 {
		t.Errorf("down did not move the cursor down inside the draft: on line %d", got)
	}
	if a.roster.Selected != before {
		t.Errorf("down moved the roster while editing a multi-line draft")
	}
}

// At the top of a multi-line draft up has nowhere to move the cursor, so it
// falls through to the roster - the same edge behavior CanCursorUp encodes.
func TestPlainUpAtTopOfDraftMovesRoster(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex", "bo")
	a = a.withDraft("first").newline()
	a = a.withDraft("second")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp}) // onto the first line, roster untouched
	if a.roster.Selected != "" {
		t.Fatalf("the first up should have moved the cursor, not the roster (selected %q)", a.roster.Selected)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp}) // at the top edge now: roster
	if a.roster.Selected == "" {
		t.Error("up at the top of the draft did not fall through to the roster")
	}
}

// Down at the very bottom of a multi-line draft must do something - move the
// cursor or move the roster - never nothing. This is the exact-width swallow
// (bubbles counts a synthetic wrapped row the cursor cannot occupy) seen from
// the App: if the predicate ever over-reports movement, down reaches a composer
// that no-ops and the roster is skipped, so the key does nothing at all.
func TestPlainDownAtBottomOfDraftIsNeverSwallowed(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex", "bo")
	a = a.withDraft("first").newline()
	a = a.withDraft("second") // cursor at the very end of the last line

	beforeCursor := a.composer().ta
	beforeRoster := a.roster.Selected
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})

	movedCursor := cursorRow(a.composer().ta) != cursorRow(beforeCursor)
	movedRoster := a.roster.Selected != beforeRoster
	if !movedCursor && !movedRoster {
		t.Error("down at the bottom of a multi-line draft moved neither the cursor nor the roster: the arrow was swallowed")
	}
}

// A single-line draft is the roster's however its runes group into graphemes.
// bubbles' edge CursorUp/CursorDown can shuffle the column within a multi-rune
// grapheme (a combining accent, a ZWJ sequence) without changing the row, so a
// predicate that watched the column would read that as vertical movement and
// nudge the cursor into the middle of the glyph instead of moving the roster.
// The move keys stay vertical: up/down here must reach the roster. Graphemes are
// built from rune code points so the source carries no ambiguous literal.
func TestArrowsOnSingleLineGraphemesMoveRoster(t *testing.T) {
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
			a = a.withDraft(tc.draft) // one logical line, cursor at the end
			before := a.roster.Selected
			a, _ = pressKey(a, tea.KeyMsg{Type: key})
			if a.roster.Selected == before {
				t.Errorf("%s with key %v: the arrow did not move the roster - a grapheme-boundary column shift was misread as vertical movement", tc.what, key)
			}
		}
	}
}

// Shift+arrows are the panes' and never leak into the draft, whatever the query
// holds. A single room pane has nowhere to move, so the guard is that the cursor
// does not move: the key was taken as movePane, not passed to the text.
func TestShiftArrowsDoNotMoveTheQueryCursor(t *testing.T) {
	a := newRoomApp(t).withSize(160, 40).withAgents("alex", "bo")
	a = a.withDraft("first").newline()
	a = a.withDraft("second") // cursor on line 1

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftUp})
	if got := a.composer().ta.Line(); got != 1 {
		t.Errorf("shift+up moved the query cursor to line %d: it must stay a pane key, not edit the draft", got)
	}
}
