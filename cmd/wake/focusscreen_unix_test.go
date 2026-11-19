//go:build unix

package main

// A window that loses the terminal's focus, on a real screen: no caret and no
// accent while another window has the keyboard, both back at once when it returns.
//
// The unit tests hand the App a BlurMsg, which is the library having decided what
// the bytes mean. These write the bytes a terminal sends (DECSET 1004's `ESC [ O`
// and `ESC [ I`) to the real binary, and read the screen vt10x drew.

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/hinshun/vt10x"
)

// caretWindow is how long a caret is watched for: two blink periods, so a caret
// that is drawn at all is seen in its on phase.
const caretWindow = 1300 * time.Millisecond

// reverseVideo is vt10x's mode bit for a cell drawn in reverse video, which is
// how the caret is drawn; the package keeps the constant to itself. The control
// in the test below - a caret seen while focused - is what notices it moving.
const reverseVideo = 1

// composerTells reads the two things the query box says about focus, off the row
// the draft is on: the colour of the border above it, and whether the cell after
// the draft is drawn in reverse video - the caret.
func (s *screen) composerTells(draft string) (border vt10x.Color, caret bool) {
	s.t.Helper()
	lines := s.lines()
	row := rowIn(lines, draft)
	if row < 1 || !strings.Contains(lines[row-1], "╭") {
		s.t.Fatalf("the draft %q is not under the query box's top edge.\n%s", draft, s.dump())
	}
	line := lines[row]
	end := utf8.RuneCountInString(line[:strings.Index(line, draft)+len(draft)])
	corner := utf8.RuneCountInString(lines[row-1][:strings.Index(lines[row-1], "╭")])
	s.term.Lock()
	defer s.term.Unlock()
	return s.term.Cell(corner, row-1).FG, s.term.Cell(end, row).Mode&reverseVideo != 0
}

// draftIs says whether the query box holds exactly draft: nothing but spaces sit
// between its end and the box's right edge, where a stray `[O` would show.
func (s *screen) draftIs(draft string) bool {
	lines := s.lines()
	row := rowIn(lines, draft)
	if row < 0 {
		return false
	}
	line := lines[row]
	rest, _, _ := strings.Cut(line[strings.Index(line, draft)+len(draft):], "│")
	return strings.TrimSpace(rest) == ""
}

// caretSeenWithin says whether the caret is drawn at any moment of d.
func (s *screen) caretSeenWithin(draft string, d time.Duration) bool {
	s.t.Helper()
	for deadline := time.Now().Add(d); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if _, caret := s.composerTells(draft); caret {
			return true
		}
	}
	return false
}

// awaitBorder waits for the box's border to be (or stop being) the colour given.
func (s *screen) awaitBorder(draft string, color vt10x.Color, want bool) {
	s.t.Helper()
	for deadline := time.Now().Add(testTimeout); ; time.Sleep(20 * time.Millisecond) {
		if border, _ := s.composerTells(draft); (border == color) == want {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("the border never %s the colour %v.\n%s", map[bool]string{true: "became", false: "left"}[want], color, s.dump())
		}
	}
}

// Wake asks the terminal for focus reports, and gives the terminal back without
// them: left on, the shell is sent `^[[I` and `^[[O` on every window switch.
//
// Mutation check: dropping tea.WithReportFocus fails the first assertion;
// dropping the disable from the exit path fails the second.
func TestTheRoomAsksForFocusReportsAndLetsGoOfThemOnExit(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	if !strings.Contains(s.raw.String(), "\x1b[?1004h") {
		t.Errorf("the window never asked the terminal to report focus.\n%s", s.dump())
	}

	s.send("\x0f\r") // ⌃O arms the detach and ↵ confirms it
	s.await("Detached")
	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the detach ended the window with %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatalf("the window did not close after the detach.\n%s", s.dump())
	}
	raw := s.raw.String()
	if on, off := strings.LastIndex(raw, "\x1b[?1004h"), strings.LastIndex(raw, "\x1b[?1004l"); off < on {
		t.Errorf("the window left focus reporting on (enabled at %d, last disabled at %d)", on, off)
	}
}

// The caret and the accent go with the window's focus and come back with it, and
// the draft is where it was left.
//
// Mutation check: the UI ignoring BlurMsg fails the wait for the border to change;
// ignoring FocusMsg fails the wait for it to return; a pump that held a report as
// unfinished fails both. (Not enabling the reports is the first test's: a report
// decodes whether or not the terminal was asked for it.)
func TestAWindowThatLosesFocusDrawsNoCaretAndNoAccent(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	s.send("hello")
	s.await("hello")
	s.settle()

	accent, _ := s.composerTells("hello")
	if !s.caretSeenWithin("hello", caretWindow) {
		t.Fatalf("control: a focused window draws no caret.\n%s", s.dump())
	}

	s.send("\x1b[O") // the terminal window lost focus
	s.awaitBorder("hello", accent, false)
	if s.caretSeenWithin("hello", caretWindow) {
		t.Errorf("a window that lost focus still draws its caret.\n%s", s.dump())
	}
	if !strings.Contains(s.text(), "hello") {
		t.Errorf("the draft went with the focus.\n%s", s.dump())
	}

	s.send("\x1b[I") // and got it back
	s.awaitBorder("hello", accent, true)
	if !s.caretSeenWithin("hello", caretWindow) {
		t.Errorf("a window that regained focus draws no caret.\n%s", s.dump())
	}
}

// The click that focuses a window sends the report and the press together, and a
// window that missed the report would stay dark under the pointer that just
// brought it back: the box is lit again, with no key pressed.
//
// Mutation check: a pipe that does not end a read at the report fails the wait for
// the border to return (the click's own reports do not heal: only a key does).
func TestAClickThatFocusesTheWindowLightsItAtOnce(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	s.send("hello")
	s.await("hello")
	s.settle()
	accent, _ := s.composerTells("hello")

	s.send("\x1b[O")
	s.awaitBorder("hello", accent, false)

	row := s.rowOf("hello") + 1 // the report's rows are 1-based
	click := fmt.Sprintf("\x1b[<0;40;%dM\x1b[<0;40;%dm", row, row) // past the draft: the caret stays at its end
	s.send("\x1b[I" + click) // one write: one read, as a terminal's click arrives
	s.awaitBorder("hello", accent, true)
	if !s.caretSeenWithin("hello", caretWindow) {
		t.Errorf("the window the click focused draws no caret.\n%s", s.dump())
	}
}

// A focus report is never a key: nothing is typed, and an Escape keypress after
// one is the first Escape rather than the second. In one read with the bytes
// behind it, too - the click that focuses a window sends its report and the press
// together.
//
// Mutation check: a report delivered as ESC [ and a letter in separate reads types
// `[O` after the draft; one counted as an escape lets the next ⎋ clear it.
func TestFocusReportsAreNeverKeysAndNeverEscape(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.send("scrap this")
	s.await("scrap this")
	s.settle()

	for _, burst := range []string{"\x1b[O", "\x1b[I", "\x1b[O\x1b[I", "\x1b[I\x1b[<35;40;10M", "\x1b[O\x1b[<35;40;10M\x1b[<35;41;10M"} {
		s.send(burst)
		s.settle()
	}
	s.send("\x1b[I") // ends focused, whatever the bursts decided
	s.settle()
	if !s.draftIs("scrap this") {
		t.Fatalf("a focus report changed the draft.\n%s", s.dump())
	}

	s.send("\x1b") // one real ⎋: the first of two, which leaves the draft
	s.settle()
	if !strings.Contains(s.text(), "scrap this") {
		t.Fatalf("a focus report was counted as an Escape.\n%s", s.dump())
	}
	s.send("\x1b") // the second clears it, so ⎋ is alive and the draft was still there to clear
	s.settle()
	if strings.Contains(s.text(), "scrap this") {
		t.Fatalf("⎋⎋ no longer clears the draft after focus reports.\n%s", s.dump())
	}
}

// ⌃C⌃C closes a window wherever the terminal's reports fall around it.
func TestTwoCtrlCStillClosesTheWindowBehindFocusReports(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	s.send("\x1b[O")
	s.send("\x1b[I")
	s.send("\x03\x03")

	done := make(chan error, 1)
	go func() { done <- s.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatalf("⌃C⌃C did not close the window after focus reports.\n%s", s.dump())
	}
	raw := s.raw.String()
	if on, off := strings.LastIndex(raw, "\x1b[?1004h"), strings.LastIndex(raw, "\x1b[?1004l"); off < on {
		t.Errorf("the emergency exit left focus reporting on (enabled at %d, last disabled at %d)", on, off)
	}
}
