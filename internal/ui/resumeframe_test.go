package ui

import (
	"strings"
	"testing"
	"time"
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

// The room draws the multi-select picker over its own composer: a parked session
// by @name, an on-disk stranger by short id, and the ␣-select key line.
func TestTheResumePickerDrawsOverTheRoomComposer(t *testing.T) {
	a := parkedFleetApp(t, DiskSession{ID: "abcd1234-5678-4abc-8def-000000000000", Dir: "/dev/x", Modified: time.Now()})
	frame := stripANSI(openedResumePicker(t, a).View())
	for _, want := range []string{"resume ·", "@iris", "abcd1234", "␣ select"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the room frame does not draw %q:\n%s", want, frame)
		}
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
	if strings.Contains(frame, "␣ select") {
		t.Errorf("the DM frame drew the multi-select key line:\n%s", frame)
	}
}
