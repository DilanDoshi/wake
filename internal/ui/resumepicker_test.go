package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	irisID     = "aaaaaaaa-1111-4111-8111-111111111111"
	strangerID = "bbbbbbbb-2222-4222-8222-222222222222"
)

func twoRows() []resumeRow {
	return []resumeRow{
		{ID: irisID, Name: "iris", Dir: "/dev/wake", Label: "feat/x", Age: "12m", Parked: true, Resumable: true},
		{ID: strangerID, Preview: "fix the parser", Age: "5d", Resumable: false},
	}
}

func openPicker(rows []resumeRow, multi bool) App {
	return App{resumePicker: ResumePicker{Rows: rows, Multi: multi, Selected: map[string]bool{}}}
}

// ⇥ toggles the cursored row's checkbox in the room's multi-select picker, keyed
// by id; in a single-select DM it is a swallowed no-op so it does not leave the
// picker mid-search. ␣ is a search character now, not a toggle.
func TestResumePickerTabTogglesOnlyInMulti(t *testing.T) {
	tab := tea.KeyMsg{Type: tea.KeyTab}

	multi := openPicker(twoRows(), true)
	got, _, handled := multi.resumePickerKey(tab)
	if !handled {
		t.Fatal("the multi-select picker did not handle ⇥")
	}
	if !got.resumePicker.Selected[irisID] {
		t.Error("⇥ did not toggle the cursored row on in the multi-select picker")
	}

	single := openPicker(twoRows(), false)
	next, _, handledSingle := single.resumePickerKey(tab)
	if !handledSingle {
		t.Error("the single-select picker let ⇥ fall through; it should swallow it mid-search")
	}
	if len(next.resumePicker.Selected) != 0 {
		t.Error("⇥ selected a row in a single-select picker")
	}
}

// A resume picker open over a blocked pane owns the keys: a search whose
// characters include a card's arm keys (a/d) does not arm the permission behind
// it, and ↵ resumes rather than settling the card. Regression for the
// modal-routing conflict — the picker is routed above cardKey in App.key.
func TestResumePickerOverACardDoesNotSettleIt(t *testing.T) {
	a := blockedPane(t)
	// The directory matches the "ad" typed below, so the row survives the filter
	// and ↵ has something to resume - the point being that ↵ resumes rather than
	// settling the card.
	a.resumePicker = ResumePicker{
		Rows:     []resumeRow{{ID: "abcd1234-5678-4abc-8def-000000000000", Dir: "/admin", Resumable: true}},
		Selected: map[string]bool{},
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if a.resumePicker.Query != "a" {
		t.Fatalf("the search box did not capture 'a' (query %q); cardKey took it to arm the permission", a.resumePicker.Query)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	next, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, next, cmd)
	if f.Kind == rpc.FrameAllow || f.Kind == rpc.FrameDeny {
		t.Errorf("↵ over the search box settled the permission card (%s); it must resume", f.Kind)
	}
	if f.Kind != rpc.FrameResume {
		t.Errorf("↵ wrote %s, want a FrameResume for the picked row", f.Kind)
	}
}

// ⌃C over the search box closes it (and parks), so its first press is visible -
// the emergency ⌃C⌃C kill switch arms on the tty byte regardless, and a first
// press that did nothing would make a natural retry an unmeant emergency exit.
// Regression for the swallowed-⌃C finding.
func TestResumePickerCtrlCClosesTheSearchBox(t *testing.T) {
	a := openPicker(twoRows(), false)
	next, _, handled := a.resumePickerKey(tea.KeyMsg{Type: tea.KeyCtrlC})
	if !handled {
		t.Fatal("⌃C was not handled by the picker; it would fall through invisibly")
	}
	if next.resumePicker.Open() {
		t.Error("⌃C did not close the picker; its first press must be visible")
	}
}

// An unmapped key does not close the search box: the picker swallows every key
// while it is up, so an arrow or a chord pressed to fix a typo mid-query does not
// silently lose it (App.update closes the picker on any key the App does not
// claim, so the picker must claim them all). ⎋/↵ leave, and ⌃C parks.
func TestResumePickerSwallowsUnmappedKeys(t *testing.T) {
	for _, m := range []tea.KeyMsg{
		{Type: tea.KeyLeft}, {Type: tea.KeyRight}, {Type: tea.KeyHome}, {Type: tea.KeyCtrlA},
	} {
		a := openPicker(twoRows(), false)
		next, _, handled := a.resumePickerKey(m)
		if !handled {
			t.Errorf("%v was not handled, so it would fall through and close the search box", m.Type)
		}
		if !next.resumePicker.Open() {
			t.Errorf("%v closed the picker; only ⎋ and ↵ should", m.Type)
		}
	}
}

// Printable keys and ␣ build the query rather than dismissing the picker, and ⌫
// edits it - the picker is a search box, so typing is how a row is found.
func TestResumePickerTypingBuildsTheQuery(t *testing.T) {
	a := openPicker(twoRows(), false)
	a, _, _ = a.resumePickerKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ir")})
	a, _, _ = a.resumePickerKey(tea.KeyMsg{Type: tea.KeySpace})
	a, _, handled := a.resumePickerKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("is")})
	if !handled {
		t.Fatal("the picker did not capture a typed key")
	}
	if a.resumePicker.Query != "ir is" {
		t.Errorf("query = %q, want %q", a.resumePicker.Query, "ir is")
	}
	if a.resumePicker.Open() == false {
		t.Error("typing closed the picker; it should filter")
	}
	a, _, _ = a.resumePickerKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if a.resumePicker.Query != "ir i" {
		t.Errorf("after ⌫ query = %q, want %q", a.resumePicker.Query, "ir i")
	}
}

// A query narrows the matches and re-homes the cursor, and the count header
// reflects the filtered set.
func TestResumePickerSearchFilters(t *testing.T) {
	p := ResumePicker{Rows: twoRows(), Selected: map[string]bool{}, Query: "iris"}
	f := p.filtered()
	if len(f) != 1 || f[0].ID != irisID {
		t.Fatalf("query 'iris' matched %+v, want just iris", f)
	}
	// A term that matches the stranger's preview, not iris.
	p.Query = "parser"
	if f := p.filtered(); len(f) != 1 || f[0].ID != strangerID {
		t.Errorf("query 'parser' matched %+v, want just the stranger (its preview)", f)
	}
	// A term nobody has narrows to nothing, and the header says so.
	p.Query = "zzzznope"
	if f := p.filtered(); len(f) != 0 {
		t.Errorf("query 'zzzznope' matched %+v, want nothing", f)
	}
	if h := p.header(p.filtered()); !strings.Contains(h, "no match") {
		t.Errorf("empty-match header = %q, want it to say no match", h)
	}
}

// A row checked before a filter is still resumed after it: selection is keyed by
// id, not by the index the filter would renumber.
func TestResumePickerSelectionSurvivesFilter(t *testing.T) {
	p := ResumePicker{Rows: twoRows(), Multi: true, Selected: map[string]bool{irisID: true}, Query: "repos"}
	// The query hides iris, but it stays chosen.
	chosen := p.chosen()
	if len(chosen) != 1 || chosen[0].ID != irisID {
		t.Errorf("chosen = %+v after filtering out a checked row, want iris still chosen", chosen)
	}
}

// The cursor clamps at both ends of the filtered set rather than wrapping.
func TestResumePickerMoveClamps(t *testing.T) {
	a := openPicker(twoRows(), false)
	if up := a.moveResume(-1); up.resumePicker.Cursor != 0 {
		t.Errorf("cursor went to %d on ↑ from the top; want a clamp at 0", up.resumePicker.Cursor)
	}
	down := a.moveResume(1).moveResume(1).moveResume(1)
	if want := len(twoRows()) - 1; down.resumePicker.Cursor != want {
		t.Errorf("cursor = %d after walking off the bottom; want a clamp at %d", down.resumePicker.Cursor, want)
	}
}

// chosen is the cursor match in single-select, the checked set in multi, and the
// cursor match again when multi has nothing checked.
func TestResumePickerChosen(t *testing.T) {
	rows := []resumeRow{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	single := ResumePicker{Rows: rows, Cursor: 1}
	if c := single.chosen(); len(c) != 1 || c[0].ID != "b" {
		t.Errorf("single-select chosen = %+v, want just b (the cursor row)", c)
	}
	multi := ResumePicker{Rows: rows, Multi: true, Cursor: 0, Selected: map[string]bool{"a": true, "c": true}}
	if c := multi.chosen(); len(c) != 2 || c[0].ID != "a" || c[1].ID != "c" {
		t.Errorf("multi chosen = %+v, want a and c in order", c)
	}
	fallback := ResumePicker{Rows: rows, Multi: true, Cursor: 2, Selected: map[string]bool{}}
	if c := fallback.chosen(); len(c) != 1 || c[0].ID != "c" {
		t.Errorf("multi-with-nothing-checked chosen = %+v, want the cursor row c", c)
	}
}

// A parked row wakes and an on-disk row resumes in place - the frame kind is
// chosen by Parked, and both travel in one batch.
func TestResumeFramesByParked(t *testing.T) {
	rows := []resumeRow{{ID: "p", Parked: true}, {ID: "d", Parked: false}}
	got := resumeFrames(rows)
	if len(got) != 2 {
		t.Fatalf("resumeFrames gave %d frames, want 2", len(got))
	}
	if got[0].Kind != rpc.FrameWake || got[0].SessionID != "p" {
		t.Errorf("parked row → %+v, want a FrameWake for p", got[0])
	}
	if got[1].Kind != rpc.FrameResume || got[1].SessionID != "d" {
		t.Errorf("disk row → %+v, want a FrameResume for d", got[1])
	}
}

// The view names a parked row by @name and a disk row by short id, shows the
// no-dir note, draws checkboxes only in multi, carries the count header and the
// search line, and advertises its own keys - ⇥ for select only in multi.
func TestResumePickerView(t *testing.T) {
	multi := ResumePicker{Rows: twoRows(), Multi: true, Selected: map[string]bool{irisID: true}}
	v := multi.View(100, 100)
	for _, want := range []string{"resume session · 1 of 2", "› search…", "@iris", "[x] ", "[ ] ", "bbbbbbbb", "no directory", "⇥ select"} {
		if !strings.Contains(v, want) {
			t.Errorf("the multi view is missing %q:\n%s", want, v)
		}
	}

	single := ResumePicker{Rows: twoRows()}
	sv := single.View(100, 100)
	if strings.Contains(sv, "[x]") || strings.Contains(sv, "[ ]") {
		t.Errorf("the single-select view drew checkboxes:\n%s", sv)
	}
	if strings.Contains(sv, "⇥ select") {
		t.Errorf("the single-select view offered ⇥ select:\n%s", sv)
	}
	if !strings.Contains(sv, "↑↓ move") {
		t.Errorf("the single-select view is missing its key hint:\n%s", sv)
	}
}

// boxInterior is a drawn row with the picker's own frame taken off, so a test
// can read what is inside the box rather than its walls.
func boxInterior(line string) string {
	return strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "│"))
}

// The picker is a box - the count in its top edge, the keys in its bottom -
// holding a search box of its own, and each session is two rows: its name on
// top, then its short id, age, directory, branch and last prompt beneath, with a
// blank row before the next session so the list is not one crowded block.
func TestResumePickerDrawsABoxedListOfNamedSessions(t *testing.T) {
	rows := append(twoRows(), resumeRow{
		ID: "cccccccc-3333-4333-8333-333333333333", Title: "gmail helper", Dir: "/dev/mail",
		Preview: "connect gmail", Age: "just now", Resumable: true,
	})
	lines := strings.Split(stripANSI(ResumePicker{Rows: rows}.View(100, 100)), "\n")

	if top := lines[0]; !strings.HasPrefix(top, "╭") || !strings.Contains(top, "resume session · 1 of 3") {
		t.Errorf("the top edge is not a box carrying the count: %q", top)
	}
	if bottom := lines[len(lines)-1]; !strings.HasPrefix(bottom, "╰") || !strings.Contains(bottom, "↑↓ move") {
		t.Errorf("the bottom edge is not a box carrying the keys: %q", bottom)
	}
	for _, l := range lines {
		if !strings.HasSuffix(l, "│") && !strings.HasSuffix(l, "╮") && !strings.HasSuffix(l, "╯") {
			t.Errorf("a row of the box has lost its right wall: %q", l)
		}
	}
	search := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "› search…") })
	if search < 1 || !strings.Contains(lines[search-1], "╭") || !strings.Contains(lines[search+1], "╰") {
		t.Fatalf("the search line is not inside a box of its own:\n%s", strings.Join(lines, "\n"))
	}

	for _, s := range []struct{ name, details, notDetails string }{
		{name: "@iris", details: "aaaaaaaa · 12m · /dev/wake · feat/x"},
		{name: "fix the parser", details: "bbbbbbbb · 5d · (no directory", notDetails: "fix the parser"},
		{name: "gmail helper", details: `cccccccc · just now · /dev/mail · "connect gmail"`},
	} {
		at := slices.IndexFunc(lines, func(l string) bool { return strings.HasSuffix(boxInterior(l), s.name) })
		if at < 0 || at+1 >= len(lines) {
			t.Fatalf("no row is headed %q:\n%s", s.name, strings.Join(lines, "\n"))
		}
		details := boxInterior(lines[at+1])
		if !strings.HasPrefix(details, s.details) {
			t.Errorf("%q's details row = %q, want it to start %q", s.name, details, s.details)
		}
		if s.notDetails != "" && strings.Contains(details, s.notDetails) {
			t.Errorf("%q's details row repeats the name it is headed by: %q", s.name, details)
		}
		if next := lines[at+2]; boxInterior(next) != "" && !strings.HasPrefix(next, "╰") {
			t.Errorf("%q is not followed by a blank row before the next session: %q", s.name, next)
		}
	}
}

// Search reaches a session's name, since the name is now what a row is headed by.
func TestResumePickerSearchesTheSessionName(t *testing.T) {
	rows := append(twoRows(), resumeRow{ID: "cccccccc-3333-4333-8333-333333333333", Title: "gmail helper", Resumable: true})
	p := ResumePicker{Rows: rows, Query: "gmail"}
	if f := p.filtered(); len(f) != 1 || f[0].Title != "gmail helper" {
		t.Errorf("searching the name found %+v, want only the gmail helper row", f)
	}
}

// A pane clips a menu from the bottom, so the picker draws only the sessions the
// rows it is given hold - and always the one the cursor is on, or ↵ would resume
// a session nobody can see.
func TestResumePickerFitsTheRowsItIsGiven(t *testing.T) {
	rows := append(twoRows(), resumeRow{ID: "cccccccc-3333-4333-8333-333333333333", Title: "gmail helper", Resumable: true})
	for _, maxRows := range []int{3, 7, 10, 13, 100} {
		v := stripANSI(ResumePicker{Rows: rows, Cursor: 2}.View(100, maxRows))
		if !strings.Contains(v, "› gmail helper") {
			t.Errorf("at %d rows the cursored session is not drawn:\n%s", maxRows, v)
		}
		if h := lipgloss.Height(v); maxRows >= 7 && h > maxRows {
			t.Errorf("at %d rows the picker drew %d:\n%s", maxRows, h, v)
		}
	}
}

// With room to spare the window is four sessions, and it slides with the cursor
// so the cursored session is always among them.
func TestResumePickerPagesFourSessionsAtATime(t *testing.T) {
	var rows []resumeRow
	for i := range 7 {
		rows = append(rows, resumeRow{ID: fmt.Sprintf("%08d-0000-4000-8000-000000000000", i), Title: fmt.Sprintf("session %d", i), Resumable: true})
	}
	for cursor := range rows {
		v := stripANSI(ResumePicker{Rows: rows, Cursor: cursor}.View(100, 100))
		if n := strings.Count(v, "  session ") + strings.Count(v, "› session "); n != resumeWindow {
			t.Errorf("cursor %d: drew %d sessions, want %d:\n%s", cursor, n, resumeWindow, v)
		}
		if !strings.Contains(v, fmt.Sprintf("› session %d", cursor)) {
			t.Errorf("cursor %d: the cursored session is not in the window:\n%s", cursor, v)
		}
	}
}
