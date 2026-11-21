package ui

// A room left open for days still learns that a newer wake is out: the check
// runs on the first frame and again on a keystroke once an hour has passed - never
// on a timer, so a room nobody touches does nothing - and the strip names the
// newest release any answer has brought, until the process ends.

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// Once a newer release is known the strip names it - on the board too, which
// draws the same strip under its own rows.
func TestTheStripNamesTheNewerRelease(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40)
	a = a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})

	m, _ := a.Update(updateCheckedMsg{newer: "0.1.9"})
	a = m.(App)
	if strip := lastRows(a, 3); !strings.Contains(strip, upgradeGlyph+" wake 0.1.9") {
		t.Fatalf("the strip does not name the newer release:\n%s", strip)
	}
	a.board.Up = true
	if strip := lastRows(a, 3); !strings.Contains(strip, upgradeGlyph+" wake 0.1.9") {
		t.Errorf("the board's strip does not name the newer release:\n%s", strip)
	}
}

// A room that learned of 0.1.15 keeps asking on the hourly keystroke, so the
// 0.1.16 that went out after it opened replaces the marker (the strip too).
func TestARoomThatKnowsOfOneReleaseLearnsOfANewerOneAnHourOn(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	pinClock(t, start)
	f := &fakeCheck{newer: "0.1.15"}
	// Sized before it has a check, so the first frame's ask is still to come.
	a := newRoomApp(t).withSize(120, 40)
	a = a.applyStatus(&rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "alex", State: rpc.StateIdle}}})
	a, _ = asks(a.WithUpdateCheck(f.check), tea.WindowSizeMsg{Width: 120, Height: 40})
	if strip := lastRows(a, 3); !strings.Contains(strip, upgradeGlyph+" wake 0.1.15") {
		t.Fatalf("baseline: the strip does not name 0.1.15:\n%s", strip)
	}

	f.newer = "0.1.16"
	pinClock(t, start.Add(updateRecheckEvery))
	a, ok := asks(a, anyKey)
	if !ok {
		t.Fatal("a key an hour on did not ask a room that already knows of a newer release")
	}
	if strip := lastRows(a, 3); !strings.Contains(strip, upgradeGlyph+" wake 0.1.16") || strings.Contains(strip, "0.1.15") {
		t.Errorf("the strip still names the older release:\n%s", strip)
	}
}

// Only a strictly newer answer moves the marker: a failed check ("" - the
// answer for an offline one too), an equal answer and an older one (a release
// pulled, a stale cache) leave what the room already knows.
func TestALaterAnswerOnlyEverMovesTheMarkerForward(t *testing.T) {
	for _, tc := range []struct{ name, known, answer, want string }{
		{"a failed check", "0.1.15", "", "0.1.15"},
		{"the same release", "0.1.15", "0.1.15", "0.1.15"},
		{"an older release", "0.1.15", "0.1.14", "0.1.15"},
		{"a newer release", "0.1.15", "0.1.16", "0.1.16"},
		{"a newer minor release", "0.1.15", "0.2.0", "0.2.0"},
		// Numbers, not strings: "0.1.10" sorts before "0.1.9" as text.
		{"a patch past nine", "0.1.9", "0.1.10", "0.1.10"},
		{"nine after ten", "0.1.10", "0.1.9", "0.1.10"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
			pinClock(t, start)
			f := &fakeCheck{newer: tc.known}
			a, _ := asks(newRoomApp(t).WithUpdateCheck(f.check), tea.WindowSizeMsg{Width: 120, Height: 40})

			f.newer = tc.answer
			pinClock(t, start.Add(updateRecheckEvery))
			a, ok := asks(a, anyKey)
			if !ok {
				t.Fatal("a key an hour on did not ask")
			}
			if got := a.upgrade.newer; got != tc.want {
				t.Errorf("after %s the marker names %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// Knowing a release changes what the room does with an answer, not when it
// asks: still only on a key, still at most once an hour - a room nobody types in
// does nothing, however long it has been.
func TestAKnownReleaseKeepsTheHourlyKeystrokeCadence(t *testing.T) {
	start := time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC)
	pinClock(t, start)
	f := &fakeCheck{newer: "0.1.15"}
	a, _ := asks(newRoomApp(t).WithUpdateCheck(f.check), tea.WindowSizeMsg{Width: 120, Height: 40})

	pinClock(t, start.Add(48*time.Hour))
	for _, msg := range []tea.Msg{
		tea.WindowSizeMsg{Width: 100, Height: 30},
		tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown},
		updateCheckedMsg{newer: "0.1.15"},
	} {
		if _, ok := asks(a, msg); ok {
			t.Errorf("%T asked with nobody typing", msg)
		}
	}
	pinClock(t, start.Add(updateRecheckEvery-time.Minute))
	if _, ok := asks(a, anyKey); ok {
		t.Error("a key inside the hour asked a room that knows of a release")
	}
	pinClock(t, start.Add(updateRecheckEvery))
	a, ok := asks(a, anyKey)
	if !ok {
		t.Fatal("a key an hour on did not ask a room that knows of a release")
	}
	if _, ok := asks(a, anyKey); ok {
		t.Error("a key straight after that answer asked again")
	}
	if f.asked != 2 {
		t.Errorf("asked %d times, want the first frame's and the hour's", f.asked)
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
	forceTrueColour(t) // the styled path: trimming the pad must not eat an escape
	coloured := upgradeMarked(awarenessStrip([]Agent{{ID: "s1", Name: "alex", State: rpc.StateIdle}}, nil, "tally", 80), "0.1.9", 80)
	if got := lipgloss.Width(coloured); got != 80 {
		t.Errorf("the coloured marked strip is %d cells, want 80: %q", got, coloured)
	}
	wide := stripANSI(upgradeMarked(awarenessStrip([]Agent{{ID: "s1", Name: "alex", State: rpc.StateIdle}}, nil, "", 80), "0.1.9", 80))
	if !strings.Contains(wide, upgradeGlyph+" wake 0.1.9") {
		t.Errorf("a wide strip dropped the marker: %q", wide)
	}
	if n := len([]rune(wide)); n != 80 {
		t.Errorf("the marked strip is %d cells, want the frame's 80", n)
	}
}

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
