package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// openedResumePicker runs a bare /resume the way Bubble Tea does - the router
// hands back the disk-walk command, the command produces a message, and Update
// folds it into an open picker - and lays the frame out.
func openedResumePicker(t *testing.T, a App) App {
	t.Helper()
	next, cmd := a.resume("")
	if cmd == nil {
		t.Fatal("bare /resume did not kick the disk walk")
	}
	m, _ := next.Update(cmd())
	return m.(App).withSize(200, 40).applyGeometry()
}

// The room draws the multi-select picker over its own composer: the count
// header, the search line, a parked session by @name, an on-disk stranger by
// short id, and the ⇥-select key line.
func TestTheResumePickerDrawsOverTheRoomComposer(t *testing.T) {
	a := parkedFleetApp(t, DiskSession{ID: "abcd1234-5678-4abc-8def-000000000000", Dir: "/dev/x", Modified: time.Now()})
	frame := stripANSI(openedResumePicker(t, a).View())
	for _, want := range []string{"resume session ·", "› search…", "@iris", "abcd1234", "⇥ select"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the room frame does not draw %q:\n%s", want, frame)
		}
	}
}

// Typing into an open picker filters the drawn frame to the matches and updates
// the count - the search box end to end, through the App key path.
func TestTheResumePickerFiltersAsYouType(t *testing.T) {
	a := parkedFleetApp(t,
		DiskSession{ID: "abcd1234-5678-4abc-8def-000000000000", Dir: "/dev/alpha", Modified: time.Now()},
		DiskSession{ID: "ffffffff-5678-4abc-8def-000000000000", Dir: "/dev/beta", Modified: time.Now().Add(-time.Hour)},
	)
	got := openedResumePicker(t, a)
	for _, r := range "alpha" {
		got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	frame := stripANSI(got.withSize(200, 40).applyGeometry().View())
	if !strings.Contains(frame, "/dev/alpha") {
		t.Errorf("the filtered frame dropped the matching row:\n%s", frame)
	}
	if strings.Contains(frame, "/dev/beta") {
		t.Errorf("the filtered frame still shows a non-matching row:\n%s", frame)
	}
	if !strings.Contains(frame, "1 of 1") {
		t.Errorf("the count header does not reflect the filter:\n%s", frame)
	}
}

// A DM draws the single-select picker: the same rows, but the ↑↓ key line and no
// checkbox-select hint.
func TestTheResumePickerDrawsSingleSelectInADM(t *testing.T) {
	a := parkedFleetApp(t, DiskSession{ID: "abcd1234-5678-4abc-8def-000000000000", Dir: "/dev/x", Modified: time.Now()}).
		openDMWith("live1", "alex")
	frame := stripANSI(openedResumePicker(t, a).View())
	if !strings.Contains(frame, "@iris") {
		t.Errorf("the DM frame does not draw the parked session:\n%s", frame)
	}
	if !strings.Contains(frame, "↑↓ move") {
		t.Errorf("the DM frame is missing the single-select key line:\n%s", frame)
	}
	if strings.Contains(frame, "⇥ select") {
		t.Errorf("the DM frame drew the multi-select key line:\n%s", frame)
	}
}

// In a short terminal every session the cursor walks onto is drawn, and the
// frame stays the terminal's height - the pane clips the picker from the bottom,
// so a window that ignored the pane's room hid what ↵ resumes.
func TestTheResumePickerKeepsTheCursorOnScreenInAShortPane(t *testing.T) {
	var disk []DiskSession
	for i := range 9 {
		disk = append(disk, DiskSession{
			ID: fmt.Sprintf("abcd123%d-5678-4abc-8def-000000000000", i), Dir: "/dev/x",
			Title: fmt.Sprintf("session %d", i), Modified: time.Now().Add(-time.Duration(i) * time.Minute),
		})
	}
	const height = 16
	got := openedResumePicker(t, parkedFleetApp(t, disk...)).withSize(120, height).applyGeometry()
	for i := range len(got.resumePicker.filtered()) {
		frame := stripANSI(got.View())
		if n := strings.Count(frame, "\n") + 1; n != height {
			t.Fatalf("with the cursor on row %d the frame is %d rows, want %d:\n%s", i, n, height, frame)
		}
		if want := "› [ ] " + got.resumePicker.filtered()[i].heading(); !strings.Contains(frame, want) {
			t.Fatalf("the cursored session %q is not drawn:\n%s", want, frame)
		}
		got, _ = pressKey(got, tea.KeyMsg{Type: tea.KeyDown})
	}
}
