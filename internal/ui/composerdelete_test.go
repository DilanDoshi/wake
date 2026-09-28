package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// roomDraftApp is a full-width room with the roster hidden and a draft typed in,
// ready for a query-box drag.
func roomDraftApp(t *testing.T, draft string) App {
	t.Helper()
	a := newRoomApp(t).withSize(120, 40).withAgents("alex")
	a.layout.ShowRoster = false
	a.focus = ""
	a = a.applyGeometry()
	return a.withDraft(draft)
}

// dragThenKey drags across a draft row and then delivers a key through Update,
// returning the composer value afterwards.
func dragThenKey(t *testing.T, a App, x0, x1, y int, k tea.KeyMsg) string {
	t.Helper()
	a, _ = a.mouse(pressAt(x0, y))
	for x := x0 + 1; x <= x1; x++ {
		a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: y})
	}
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x1, Y: y})
	if !a.sel.inComposer || a.sel.empty() {
		t.Fatalf("no composer selection was taken to delete: %+v", a.sel)
	}
	m, _ := a.Update(k)
	return m.(App).composer().Value()
}

// Backspace over a query-box selection deletes the highlighted text rather than
// one character, and delete does the same. Nothing did this before: the
// selection is copy-only and every keystroke cleared it before the key ran.
func TestBackspaceDeletesTheHighlightedQueryText(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyBackspace}, {Type: tea.KeyDelete}} {
		a := roomDraftApp(t, "hello world\nsecond line here")
		r := a.regions()
		w, h := r.Room(), a.paneHeight()
		draftTop, _, _, _, ok := a.composerRegion("", w, 0, h)
		if !ok {
			t.Fatal("no composer region")
		}
		left := a.layout.PaneLeft(r, 0) + composerTextLeft
		// "world" is display cells 6..10 on the first draft row.
		got := dragThenKey(t, a, left+6, left+10, draftTop, key)
		if want := "hello \nsecond line here"; got != want {
			t.Errorf("%v over 'world': draft = %q, want %q", key.Type, got, want)
		}
	}
}

// A selection that wraps across two display rows of one logical line deletes the
// runes between its ends, soft-wrap boundary included, with no stray newline.
func TestDeletingASelectionThatSpansAWrappedRow(t *testing.T) {
	// One long logical line so the composer wraps it. Narrow the pane hard.
	a := newRoomApp(t).withSize(48, 40).withAgents("alex")
	a.layout.ShowRoster = false
	a.focus = ""
	a = a.applyGeometry()
	a = a.withDraft("alpha bravo charlie delta echo foxtrot golf")

	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, draftRows, _, _, ok := a.composerRegion("", w, 0, h)
	if !ok || draftRows < 2 {
		t.Fatalf("the draft did not wrap into %d rows", draftRows)
	}
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	// Drag from partway through the first row to partway through the second.
	a, _ = a.mouse(pressAt(left+6, draftTop))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: left + 4, Y: draftTop + 1})
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 4, Y: draftTop + 1})
	if !a.sel.inComposer || a.sel.empty() {
		t.Fatalf("no composer selection: %+v", a.sel)
	}
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := m.(App).composer().Value()
	// The deletion must not introduce a newline (it was one logical line) and
	// must remove a contiguous run.
	if len(got) >= len("alpha bravo charlie delta echo foxtrot golf") {
		t.Errorf("nothing was deleted: %q", got)
	}
	if got == "" {
		t.Errorf("the whole draft was deleted, not the selection")
	}
	if containsNewline(got) {
		t.Errorf("deleting within one logical line introduced a newline: %q", got)
	}
}

func containsNewline(s string) bool {
	for _, r := range s {
		if r == '\n' {
			return true
		}
	}
	return false
}

// After a deletion the cursor sits where the removed run began, so the next
// character typed lands there rather than at the end of the draft.
func TestTheCursorFollowsADeletionSoTypingLandsThere(t *testing.T) {
	a := roomDraftApp(t, "hello world foo")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, _ := a.composerRegion("", w, 0, h)
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	// Select and delete "world" (cells 6..10).
	a, _ = a.mouse(pressAt(left+6, draftTop))
	for x := left + 7; x <= left+10; x++ {
		a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: draftTop})
	}
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 10, Y: draftTop})
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	a = m.(App)
	if got := a.composer().Value(); got != "hello  foo" {
		t.Fatalf("after delete, draft = %q, want %q", got, "hello  foo")
	}
	// Type X: it must land at the deletion point (offset 6), giving "hello X foo".
	m, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	if got := m.(App).composer().Value(); got != "hello X foo" {
		t.Errorf("typing after a delete gave %q, want %q: the cursor did not follow the deletion", got, "hello X foo")
	}
}

// A highlight in a scrolled draft deletes exactly the run under it: the drawn
// rows are a window into the draft, mapped from the caret, not its first rows.
func TestBackspaceDeletesTheHighlightInAScrolledDraft(t *testing.T) {
	draft := tallDraft(20)
	a := roomDraftApp(t, draft)
	drawn := drawnDraftText(t, a)
	lines := strings.Split(draft, "\n")
	line := slices.Index(lines, drawn[0])
	if line <= 0 {
		t.Fatalf("the draft is not scrolled: drawn %q", drawn)
	}
	r := a.regions()
	draftTop, _, _, _, _ := a.composerRegion("", r.Room(), 0, a.paneHeight())
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	got := dragThenKey(t, a, left, left+3, draftTop, tea.KeyMsg{Type: tea.KeyBackspace}) // "line"
	lines[line] = strings.TrimPrefix(lines[line], "line")
	if want := strings.Join(lines, "\n"); got != want {
		t.Errorf("deleting 'line' on drawn row 0 (%q) gave\n%q\nwant\n%q", drawn[0], got, want)
	}
}

// A deletion near the top of a tall draft leaves the caret where the run was,
// however many lines below it the draft goes on - typing lands there.
func TestTypingAfterADeletionNearTheTopOfATallDraft(t *testing.T) {
	draft := tallDraft(30)
	var m tea.Model = roomDraftApp(t, draft)
	for range 26 {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	}
	a := m.(App)
	if drawn := drawnDraftText(t, a); drawn[1] != "line 01" {
		t.Fatalf("want the box drawn from line 0: drawn %q", drawn)
	}
	r := a.regions()
	draftTop, _, _, _, _ := a.composerRegion("", r.Room(), 0, a.paneHeight())
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	a, _ = a.mouse(pressAt(left, draftTop+1))
	for x := left + 1; x <= left+3; x++ {
		a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: draftTop + 1})
	}
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 3, Y: draftTop + 1})
	m, _ = a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	lines := strings.Split(draft, "\n")
	lines[1] = "X 01"
	if got, want := m.(App).composer().Value(), strings.Join(lines, "\n"); got != want {
		t.Errorf("typing after deleting 'line' on line 1 gave\n%q\nwant\n%q", got, want)
	}
}

// A selection over wide (CJK) characters deletes exactly those runes: the
// cell-to-rune conversion holds when double-width characters sit under the drag.
func TestDeletingAWideCharacterSelection(t *testing.T) {
	a := roomDraftApp(t, "AB你好世界CD")
	r := a.regions()
	w, h := r.Room(), a.paneHeight()
	draftTop, _, _, _, _ := a.composerRegion("", w, 0, h)
	left := a.layout.PaneLeft(r, 0) + composerTextLeft
	// Cells: A=0 B=1 你=2,3 好=4,5 世=6,7 界=8,9 C=10 D=11. Select 你好世 (cells 2..7).
	got := dragThenKey(t, a, left+2, left+7, draftTop, tea.KeyMsg{Type: tea.KeyBackspace})
	if want := "AB界CD"; got != want {
		t.Errorf("deleting a CJK span gave %q, want %q", got, want)
	}
}
