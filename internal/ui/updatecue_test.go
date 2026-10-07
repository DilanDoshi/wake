package ui

// A room left open for days still learns that a newer wake is out: the check
// runs on the first frame and again on a keystroke once an hour has passed - never
// on a timer, so a room nobody touches does nothing - and once a newer release is
// known the strip says so until the process ends.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// fakeCheck counts the asks and answers with what it is set to.
type fakeCheck struct {
	asked int
	newer string
}

func (f *fakeCheck) check() string { f.asked++; return f.newer }

// pinClock pins the UI clock for one test.
func pinClock(t *testing.T, now time.Time) {
	t.Helper()
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
}

// asks reports whether msg sets a check off, running it the way Bubble Tea would.
func asks(a App, msg tea.Msg) (App, bool) {
	next, cmd := a.dueUpdateCheck(msg)
	if cmd == nil {
		return next, false
	}
	m, _ := next.Update(cmd())
	return m.(App), true
}

var anyKey = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}

func TestTheFirstFrameAsksAndAKeyAsksAgainOnlyAfterAnHour(t *testing.T) {
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	pinClock(t, start)
	f := &fakeCheck{}
	a := newRoomApp(t).WithUpdateCheck(f.check)

	a, ok := asks(a, tea.WindowSizeMsg{Width: 120, Height: 40})
	if !ok || f.asked != 1 {
		t.Fatalf("the first frame did not ask (asked %d)", f.asked)
	}
	if _, ok := asks(a, anyKey); ok {
		t.Error("a key straight after the first check asked again")
	}

	pinClock(t, start.Add(updateRecheckEvery-time.Minute))
	if _, ok := asks(a, anyKey); ok {
		t.Error("a key inside the hour asked again")
	}
	pinClock(t, start.Add(updateRecheckEvery))
	if _, ok := asks(a, anyKey); !ok || f.asked != 2 {
		t.Errorf("a key an hour on did not ask (asked %d)", f.asked)
	}
}

// Cheap to leave open: what arrives while nobody types - frames off the socket,
// a resize, the mouse - never asks, however long it has been.
func TestARoomNobodyTypesInNeverAsksAgain(t *testing.T) {
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	pinClock(t, start)
	f := &fakeCheck{}
	a, _ := asks(newRoomApp(t).WithUpdateCheck(f.check), tea.WindowSizeMsg{Width: 120, Height: 40})

	pinClock(t, start.Add(48*time.Hour))
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: 100, Height: 30},
		tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown},
		updateCheckedMsg{},
	} {
		if _, ok := asks(a, msg); ok {
			t.Errorf("%T asked with nobody typing", msg)
		}
	}
	if f.asked != 1 {
		t.Errorf("asked %d times, want only the first frame's", f.asked)
	}
}

// One check at a time: a key while one is still out does not start another.
func TestAKeyWhileACheckIsOutDoesNotAskTwice(t *testing.T) {
	start := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	pinClock(t, start)
	a, cmd := newRoomApp(t).WithUpdateCheck((&fakeCheck{}).check).dueUpdateCheck(tea.WindowSizeMsg{})
	if cmd == nil {
		t.Fatal("baseline: the first frame did not ask")
	}
	pinClock(t, start.Add(2*updateRecheckEvery))
	if _, again := a.dueUpdateCheck(anyKey); again != nil {
		t.Error("a key asked again while the first check had not answered")
	}
}

func TestNoCheckMeansNoAskAndNoMarker(t *testing.T) {
	a := newRoomApp(t)
	if _, cmd := a.dueUpdateCheck(tea.WindowSizeMsg{}); cmd != nil {
		t.Error("a room with no check (WAKE_NO_UPDATE_CHECK) asked")
	}
	if strings.Contains(stripANSI(a.withSize(120, 40).View()), upgradeGlyph) {
		t.Error("a room with no check drew the upgrade marker")
	}
}

// Once a newer release is known the strip says so for good: a later check that
// fails, offline, answers "" and must not take the marker away.
func TestTheStripNamesTheNewerReleaseAndKeepsIt(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40)
	a = a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})

	m, _ := a.Update(updateCheckedMsg{newer: "0.1.9"})
	a = m.(App)
	if strip := lastRows(a, 3); !strings.Contains(strip, upgradeGlyph+" wake 0.1.9") {
		t.Fatalf("the strip does not name the newer release:\n%s", strip)
	}
	m, _ = a.Update(updateCheckedMsg{})
	if strip := lastRows(stripped(m), 3); !strings.Contains(strip, upgradeGlyph+" wake 0.1.9") {
		t.Errorf("a check that found nothing took the marker away:\n%s", strip)
	}
}

// A partial "↑ wake 0.1" names a release that does not exist: the marker goes
// whole, and first - the counts the strip exists for stay.
func TestTheMarkerIsDroppedWholeBeforeTheCounts(t *testing.T) {
	row := awarenessStrip([]Agent{{ID: "s1", Name: "alex", State: rpc.StateIdle}}, nil, "", 40)
	counts := strings.TrimSpace(stripANSI(row))
	for _, width := range []int{40, len(counts) + 6} {
		got := strings.TrimRight(stripANSI(upgradeMarked(awarenessStrip([]Agent{{ID: "s1", Name: "alex", State: rpc.StateIdle}}, nil, "", width), "0.1.9", width)), " ")
		if !strings.Contains(got, counts) {
			t.Errorf("width %d: the counts were cut for the marker: %q", width, got)
		}
		if strings.Contains(got, upgradeGlyph) && !strings.Contains(got, upgradeGlyph+" wake 0.1.9") {
			t.Errorf("width %d: the marker was cut rather than dropped: %q", width, got)
		}
	}
	wide := stripANSI(upgradeMarked(awarenessStrip([]Agent{{ID: "s1", Name: "alex", State: rpc.StateIdle}}, nil, "", 80), "0.1.9", 80))
	if !strings.Contains(wide, upgradeGlyph+" wake 0.1.9") {
		t.Errorf("a wide strip dropped the marker: %q", wide)
	}
	if n := len([]rune(wide)); n != 80 {
		t.Errorf("the marked strip is %d cells, want the frame's 80", n)
	}
}

func stripped(m tea.Model) App { return m.(App) }

// lastRows is the bottom of the drawn frame, where the strip and the notice row sit.
func lastRows(a App, n int) string {
	rows := strings.Split(stripANSI(a.View()), "\n")
	return strings.Join(rows[max(len(rows)-n, 0):], "\n")
}

// The gate is reached through the room's own Update, the way every message
// arrives - not only through the helper the tests above call.
func TestTheRoomsUpdateSetsTheCheckOff(t *testing.T) {
	pinClock(t, time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC))
	m, _ := newRoomApp(t).WithUpdateCheck((&fakeCheck{}).check).Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if !m.(App).upgrade.asking {
		t.Error("the first frame through Update did not set a check off")
	}
}
