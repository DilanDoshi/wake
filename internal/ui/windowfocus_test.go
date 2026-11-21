package ui

// A terminal window that loses focus draws no caret and no focus colour.
//
// Bubble Tea reports it (tea.WithReportFocus) as BlurMsg and FocusMsg. The
// composer's accent border and its caret are the two things that say "your
// keystrokes land here", and neither is true while another window has the
// keyboard. The zero value of the flag is focused, so a terminal that never
// reports anything draws exactly what it always did.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func windowBlur(a App) (App, tea.Cmd) {
	m, cmd := a.Update(tea.BlurMsg{})
	return m.(App), cmd
}

func windowFocus(a App) (App, tea.Cmd) {
	m, cmd := a.Update(tea.FocusMsg{})
	return m.(App), cmd
}

// fastBlink makes the focused composer's blink tick land in a millisecond, so a
// test can run the real command rather than a stand-in for it.
func fastBlink(a App) App {
	c := a.composer()
	c.ta.Cursor.BlinkSpeed = time.Millisecond
	return a.withComposer(c)
}

// A window nobody has reported on is focused: the caret and the accent are drawn.
// This pins the zero value; a flag that began blurred fails both assertions.
func TestAWindowThatNeverReportsFocusIsFocused(t *testing.T) {
	forceColour(t)
	a := newRoomApp(t).withSize(200, 40)

	if !hasCursor(a.room.Composer(), reverseEscape(t)) {
		t.Error("a window that was never told it lost focus draws no caret")
	}
	if !accented(a.room.Composer(), accentEscape(t)) {
		t.Error("a window that was never told it lost focus draws no accent border")
	}
}

// Blur takes the caret and the accent from the pane that had the keys. The caret
// is bubbles' own doing (its cursor blurs on BlurMsg, which update falls through to
// the focused composer), pinned here because an upgrade could change it.
//
// Mutation check: dropping the BlurMsg case fails the accent assertion.
func TestBlurHidesTheCaretAndTheAccent(t *testing.T) {
	forceColour(t)
	a := newRoomApp(t).withSize(200, 40)

	a, _ = windowBlur(a)

	if hasCursor(a.room.Composer(), reverseEscape(t)) {
		t.Error("a blurred window still draws the room's caret")
	}
	if accented(a.room.Composer(), accentEscape(t)) {
		t.Error("a blurred window still outlines the room's box in the accent")
	}
}

// Every pane goes dark, not only the one that had the keys.
//
// Mutation check: applying the flag to the room alone fails the DM assertions.
func TestBlurDarkensEveryPane(t *testing.T) {
	forceColour(t)
	a, _ := pressKey(pick(gridApp(t), "s1"), tea.KeyMsg{Type: tea.KeyCtrlY})
	a, _ = pressKey(pick(a, "s2"), tea.KeyMsg{Type: tea.KeyCtrlY})
	if got := panesOf(a.grid); len(got) != 3 {
		t.Fatalf("the grid is %v, want the room and two conversations drawn - the rest of this proves nothing", got)
	}

	a, _ = windowBlur(a)

	for id, c := range map[string]Composer{"": a.room.Composer(), "s1": a.dms["s1"].Composer(), "s2": a.dms["s2"].Composer()} {
		if hasCursor(c, reverseEscape(t)) {
			t.Errorf("pane %q still draws a caret in a blurred window", id)
		}
		if accented(c, accentEscape(t)) {
			t.Errorf("pane %q still draws the accent in a blurred window", id)
		}
	}
}

// The blink loop stops: the tick already in flight is taken and nothing re-arms
// it, so a window nobody is looking at runs no timer.
//
// The control is the same tick delivered to a focused window, which re-arms; a
// test that cannot tell the two apart proves nothing about the blurred one.
//
// Two things stop the tick (bubbles' cursor on BlurMsg, and WindowFocused's own
// Cursor.Blur), so only both mutated lets it through: the caret returns and it
// re-arms.
func TestBlurStopsTheBlinkLoop(t *testing.T) {
	forceColour(t)
	a := fastBlink(newRoomApp(t).withSize(200, 40))
	c, tick := a.composer().Refocus() // the loop App.Init starts
	a = a.withComposer(c)
	flight := runCmd(t, tick)

	if _, again := a.Update(flight); again == nil {
		t.Fatal("control: a focused window's blink tick did not re-arm, so this test cannot tell a stopped loop from a live one")
	}

	blurred, _ := windowBlur(a)
	next, again := blurred.Update(flight)
	if again != nil {
		t.Error("a blurred window re-armed its blink: a timer runs for a window nobody is looking at")
	}
	if hasCursor(next.(App).room.Composer(), reverseEscape(t)) {
		t.Error("the tick in flight when the window blurred brought the caret back")
	}
}

// Focus brings everything back at once, and the only thing it starts is the
// caret's blink: it writes nothing to the daemon and sends no message.
//
// Mutation check: dropping the FocusMsg case fails the accent assertion; a
// FocusMsg that stops at the App leaves the caret off and returns no command.
func TestFocusRestoresTheCaretTheAccentAndTheBlink(t *testing.T) {
	forceColour(t)
	conn := newRecorder(t)
	a := fastBlink(NewRoomApp(conn, Stream{}, nil).withSize(200, 40))
	a, off := windowBlur(a)
	if off != nil {
		t.Error("blur returned a command: it has nothing to start")
	}

	a, on := windowFocus(a)

	if !hasCursor(a.room.Composer(), reverseEscape(t)) {
		t.Error("focus did not bring the caret back")
	}
	if !accented(a.room.Composer(), accentEscape(t)) {
		t.Error("focus did not bring the accent back")
	}
	if on == nil {
		t.Fatal("focus returned no command, so the caret is solid rather than blinking")
	}
	if got := fmt.Sprintf("%T", runCmd(t, on)); got != "cursor.BlinkMsg" {
		t.Errorf("focus started %s, want only the caret's blink", got)
	}
	if frames := conn.taken(t); len(frames) != 0 {
		t.Errorf("blur and focus wrote frames to the daemon: %+v", frames)
	}
}

// What the operator had is what they get back: the draft, where the cursor sits
// in it, and the whole frame - scroll, selection and all.
func TestBlurAndFocusLeaveTheDraftAndTheFrameAlone(t *testing.T) {
	forceColour(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney").withDraft("hello wor")
	before := a.View()
	atEnd := a.composer().AtEnd()

	a, _ = windowBlur(a)
	if got := a.composer().Value(); got != "hello wor" {
		t.Errorf("blur changed the draft to %q", got)
	}
	a, _ = windowFocus(a)

	if got := a.composer().Value(); got != "hello wor" {
		t.Errorf("focus changed the draft to %q", got)
	}
	if a.composer().AtEnd() != atEnd {
		t.Error("blur and focus moved the cursor inside the draft")
	}
	if a.View() != before {
		t.Errorf("the frame after blur and focus is not the frame before:\nbefore:\n%s\nafter:\n%s", stripANSI(before), stripANSI(a.View()))
	}
}

// A pane that takes the keys while the window is blurred stays dark: a fork
// arriving, a conversation opening, a click that is delivered before the focus
// report. refocusBlink must not light the caret behind a blurred window, and
// withFocus must not accent it.
//
// Mutation check: Refocus ignoring the flag brings the caret back at the click;
// withFocus applying Focused alone brings the accent back at the open; skipping
// Cursor.Blur in WindowFocused leaves the new conversation's caret lit.
func TestAPaneTakingTheKeysInABlurredWindowStaysDark(t *testing.T) {
	forceColour(t)
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a, _ = windowBlur(a)

	a = a.openDMWith("s1", "sydney")
	dm := a.dms["s1"].Composer()
	if a.focus != "s1" {
		t.Fatalf("the keys are on %q, want s1", a.focus)
	}
	if accented(dm, accentEscape(t)) || hasCursor(dm, reverseEscape(t)) {
		t.Error("a conversation opened in a blurred window is drawn lit")
	}

	a = clickThrough(a, 2, 2) // into the room column, moving the keys through Update
	if hasCursor(a.composer(), reverseEscape(t)) || accented(a.composer(), accentEscape(t)) {
		t.Error("the keys moved in a blurred window and the pane that took them lit up")
	}

	a, _ = windowFocus(a)
	if !hasCursor(a.composer(), reverseEscape(t)) || !accented(a.composer(), accentEscape(t)) {
		t.Error("focus did not light the pane that holds the keys")
	}
}

// A typed key can only come from a window that has the keyboard, so it is proof
// of a focus report that never arrived. A caret hidden for good would be worse
// than one shown too long.
//
// Mutation check: dropping the heal leaves the caret hidden after the key.
func TestAKeystrokeHealsAMissedFocusReport(t *testing.T) {
	forceColour(t)
	a := newRoomApp(t).withSize(200, 40)
	a, _ = windowBlur(a)

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})

	if got := a.composer().Value(); got != "x" {
		t.Errorf("the key that healed the window was lost: draft %q, want x", got)
	}
	if !hasCursor(a.composer(), reverseEscape(t)) || !accented(a.composer(), accentEscape(t)) {
		t.Error("a keystroke in a window thought blurred left it dark")
	}
}

// Neither the wheel nor a paste is proof: a terminal delivers the wheel over a
// window without focus, and a paste can arrive in one (a drop on a background
// window, tmux's paste-buffer). Both go through Update, where the heal is.
//
// Mutation check: healing on any message fails the wheel; healing on a paste, the
// paste.
func TestNeitherAWheelNorAPasteHealsABlurredWindow(t *testing.T) {
	forceColour(t)
	for name, msg := range map[string]tea.Msg{
		"a wheel notch": tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: 40, Y: 10},
		"a paste":       tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pasted"), Paste: true},
	} {
		t.Run(name, func(t *testing.T) {
			a, _ := windowBlur(newRoomApp(t).withSize(200, 40))

			m, _ := a.Update(msg)
			a = m.(App)

			if hasCursor(a.composer(), reverseEscape(t)) || accented(a.composer(), accentEscape(t)) {
				t.Errorf("%s lit up a blurred window", name)
			}
		})
	}
}

// The resume picker's search box is the one other input that wears the accent,
// and it goes plain with the window.
//
// Mutation check: leaving the box on ComposerStyle fails the blurred assertion;
// the focused one is the control.
func TestBlurPlainsTheResumePickersSearchBox(t *testing.T) {
	forceColour(t)
	a := newRoomApp(t).withSize(160, 30).withParkBook(
		rpc.SessionStatus{ID: "s1", Name: "kwame", State: rpc.StateParked},
	)
	a, _, _ = a.slash("/resume")
	if !a.resumePicker.Open() {
		t.Fatal("/resume opened no picker")
	}
	// The search box is the frame inside the picker's own, so its top edge is the
	// first row to open a second corner; the title row above it is accented
	// whatever the window does.
	top := func(a App) string {
		view := a.resumeView(a.focus, 160, 30, "")
		for _, line := range strings.Split(view, "\n") {
			if strings.Contains(stripANSI(line), "│ ╭") {
				return line
			}
		}
		t.Fatalf("the picker draws no search box:\n%s", stripANSI(view))
		return ""
	}
	if !strings.Contains(top(a), accentEscape(t)) {
		t.Fatalf("control: the focused picker's search box carries no accent:\n%s", a.resumeView(a.focus, 160, 30, ""))
	}

	a, _ = windowBlur(a)

	if strings.Contains(top(a), accentEscape(t)) {
		t.Error("the picker's search box keeps the accent in a blurred window")
	}
	a, _ = windowFocus(a)
	if !strings.Contains(top(a), accentEscape(t)) {
		t.Error("focus did not bring the picker's accent back")
	}
}

// The /workflows save dialog draws its own caret, a reverse-video cell after the
// name, and it goes with the window's focus like the composer's.
//
// Mutation check: drawing it whatever the window does fails the blurred assertion.
func TestBlurTakesTheWorkflowSaveDialogsCaret(t *testing.T) {
	forceColour(t)
	// saveCursor is rendered once, at init, when a test has no colour to render.
	was := saveCursor
	saveCursor = lipgloss.NewStyle().Reverse(true).Render(" ")
	t.Cleanup(func() { saveCursor = was })
	a := workflowFleet(t).openDMWith("s1", "alex").openWorkflow("s1", wfTask)
	a.workflow.view.Save = &saveDialog{Name: "count-lines", Scope: rpc.ScopeProject}
	caretDrawn := func(a App) bool {
		for _, line := range strings.Split(a.View(), "\n") {
			if strings.Contains(stripANSI(line), "Save as: > count-lines") {
				return strings.Contains(line, reverseEscape(t))
			}
		}
		t.Fatalf("the save dialog is not drawn:\n%s", shown(a))
		return false
	}
	if !caretDrawn(a) {
		t.Fatal("control: the focused save dialog draws no caret")
	}

	a, _ = windowBlur(a)
	if caretDrawn(a) {
		t.Error("the save dialog keeps its caret in a blurred window")
	}
	a, _ = windowFocus(a)
	if !caretDrawn(a) {
		t.Error("focus did not bring the save dialog's caret back")
	}
}
