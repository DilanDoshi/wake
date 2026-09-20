package ui

import (
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// parkedFleetApp is a room over one live session (alex) and one parked one
// (iris, a book record), plus a seam whose disk half the test supplies.
func parkedFleetApp(t *testing.T, disk ...DiskSession) App {
	t.Helper()
	st := rpc.Status{
		Running:  true,
		Sessions: []rpc.SessionStatus{{ID: "live1", Name: "alex", State: rpc.StateIdle}},
		Parked:   []rpc.SessionStatus{{ID: "parked1", Name: "iris", State: rpc.StateParked}},
	}
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	return a.WithSessions(fakeSessions{resumable: disk})
}

// resumeRowsFrom drops a live id, names a parked id by @name (it wakes), and
// keeps a stranger (it resumes in place) - all off the disk walk's own mtimes.
func TestResumeRowsMergeAndDedup(t *testing.T) {
	now := time.Now()
	a := parkedFleetApp(t)
	disk := []DiskSession{
		{ID: "parked1", Dir: "/p", Modified: now},
		{ID: "live1", Dir: "/l", Modified: now},
		{ID: "stranger1", Dir: "/s", Preview: "hi", Modified: now},
	}
	rows, _ := a.resumeRowsFrom(disk)
	if len(rows) != 2 {
		t.Fatalf("resumeRowsFrom gave %d rows, want 2 (a live id dropped): %+v", len(rows), rows)
	}
	byID := map[string]resumeRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	if _, ok := byID["live1"]; ok {
		t.Error("a live session appeared in the picker; it is running, not resumable")
	}
	if p := byID["parked1"]; !p.Parked || p.Name != "iris" {
		t.Errorf("parked row = %+v, want Parked with @iris", p)
	}
	if s := byID["stranger1"]; s.Parked {
		t.Errorf("stranger row = %+v, want a non-parked (resume-in-place) row", s)
	}
}

// Bare /resume in the room walks the disk and opens a multi-select picker over
// both sources.
func TestBareResumeOpensMultiInRoom(t *testing.T) {
	a := parkedFleetApp(t, DiskSession{ID: "stranger1", Dir: "/s", Modified: time.Now()})
	next, cmd := a.resume("")
	if cmd == nil {
		t.Fatal("bare /resume did not kick the disk walk")
	}
	m, _ := next.Update(cmd())
	got := m.(App)
	if !got.resumePicker.Open() {
		t.Fatal("the picker did not open after the walk returned")
	}
	if !got.resumePicker.Multi {
		t.Error("the room's picker is not multi-select")
	}
	if len(got.resumePicker.Rows) != 2 {
		t.Errorf("the picker holds %d rows, want the parked session and the stranger", len(got.resumePicker.Rows))
	}
}

// Bare /resume inside a DM opens a single-select picker.
func TestBareResumeOpensSingleInDM(t *testing.T) {
	a := parkedFleetApp(t, DiskSession{ID: "stranger1", Dir: "/s", Modified: time.Now()}).openDMWith("live1", "alex")
	next, cmd := a.resume("")
	if cmd == nil {
		t.Fatal("bare /resume did not kick the disk walk")
	}
	m, _ := next.Update(cmd())
	got := m.(App)
	if !got.resumePicker.Open() {
		t.Fatal("the picker did not open in the DM")
	}
	if got.resumePicker.Multi {
		t.Error("a DM's picker is multi-select; it should be single")
	}
}

// /resume with an argument keeps the old parked-only route: it does not open the
// picker, and it remembers the wake it asked for.
func TestResumeArgumentDoesNotOpenPicker(t *testing.T) {
	a := parkedFleetApp(t)
	next, _ := a.resume("all")
	if next.resumePicker.Open() {
		t.Error("/resume all opened the picker; an argument must stay the direct route")
	}
	if _, waiting := next.waking["parked1"]; !waiting {
		t.Error("/resume all did not ask to wake the parked session")
	}
}

// A machine with nothing parked and nothing on disk reports the empty state and
// opens no picker.
func TestBareResumeEmptyMachine(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).WithSessions(fakeSessions{})
	next, cmd := a.resume("")
	got := next
	if cmd != nil {
		m, _ := next.Update(cmd())
		got = m.(App)
	}
	if got.resumePicker.Open() {
		t.Error("the picker opened on a machine with nothing to resume")
	}
}
