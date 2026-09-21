package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func twoRows() []resumeRow {
	return []resumeRow{
		{ID: "aaaaaaaa-1111-4111-8111-111111111111", Name: "iris", Dir: "/dev/wake", Label: "feat/x", Age: "12m", Parked: true, Resumable: true},
		{ID: "bbbbbbbb-2222-4222-8222-222222222222", Age: "5d", Resumable: false},
	}
}

// ␣ toggles the cursored row's checkbox in the room's multi-select picker, and
// is not the picker's key at all in a single-select DM - where it must fall
// through so the picker dismisses on a key it does not own.
func TestResumePickerSpaceTogglesOnlyInMulti(t *testing.T) {
	space := tea.KeyMsg{Type: tea.KeySpace}

	multi := App{resumePicker: ResumePicker{Rows: twoRows(), Multi: true, Selected: map[int]bool{}}}
	got, _, handled := multi.resumePickerKey(space)
	if !handled {
		t.Fatal("the multi-select picker did not handle ␣")
	}
	if !got.resumePicker.Selected[0] {
		t.Error("␣ did not toggle the cursored row on in the multi-select picker")
	}

	single := App{resumePicker: ResumePicker{Rows: twoRows(), Multi: false, Selected: map[int]bool{}}}
	if _, _, handledSingle := single.resumePickerKey(space); handledSingle {
		t.Error("the single-select picker claimed ␣; it must fall through so the picker can dismiss")
	}
}

// The cursor clamps at both ends rather than wrapping - the ends of the list
// must be distinguishable at a glance.
func TestResumePickerMoveClamps(t *testing.T) {
	a := App{resumePicker: ResumePicker{Rows: twoRows(), Selected: map[int]bool{}}}
	if up := a.moveResume(-1); up.resumePicker.Cursor != 0 {
		t.Errorf("cursor went to %d on ↑ from the top; want a clamp at 0", up.resumePicker.Cursor)
	}
	down := a.moveResume(1).moveResume(1).moveResume(1)
	if want := len(twoRows()) - 1; down.resumePicker.Cursor != want {
		t.Errorf("cursor = %d after walking off the bottom; want a clamp at %d", down.resumePicker.Cursor, want)
	}
}

// chosen is the cursor row in single-select, the checked set in multi, and the
// cursor row again when multi has nothing checked - so ↵ on a row nobody ticked
// still resumes it.
func TestResumePickerChosen(t *testing.T) {
	rows := []resumeRow{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	single := ResumePicker{Rows: rows, Cursor: 1}
	if c := single.chosen(); len(c) != 1 || c[0].ID != "b" {
		t.Errorf("single-select chosen = %+v, want just b (the cursor row)", c)
	}
	multi := ResumePicker{Rows: rows, Multi: true, Cursor: 0, Selected: map[int]bool{0: true, 2: true}}
	if c := multi.chosen(); len(c) != 2 || c[0].ID != "a" || c[1].ID != "c" {
		t.Errorf("multi chosen = %+v, want a and c in order", c)
	}
	fallback := ResumePicker{Rows: rows, Multi: true, Cursor: 2, Selected: map[int]bool{}}
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
// no-dir note for an unprovable directory, draws checkboxes only in multi, and
// advertises its own keys.
func TestResumePickerView(t *testing.T) {
	multi := ResumePicker{Rows: twoRows(), Multi: true, Selected: map[int]bool{0: true}}
	v := multi.View(100)
	for _, want := range []string{"@iris", "[x] ", "[ ] ", "bbbbbbbb", "no directory", "␣ select"} {
		if !strings.Contains(v, want) {
			t.Errorf("the multi view is missing %q:\n%s", want, v)
		}
	}

	single := ResumePicker{Rows: twoRows()}
	sv := single.View(100)
	if strings.Contains(sv, "[x]") || strings.Contains(sv, "[ ]") {
		t.Errorf("the single-select view drew checkboxes:\n%s", sv)
	}
	if !strings.Contains(sv, "↑↓ move") {
		t.Errorf("the single-select view is missing its key hint:\n%s", sv)
	}
}
