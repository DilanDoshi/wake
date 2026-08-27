//go:build unix

// What the picker actually draws, on a real terminal.
//
// The in-process tests assert about App state, which is the layer that would
// have said "green" while nothing appeared on screen - the failure a
// rendered-screen test exists for. A menu is drawn above a composer
// that is already sized, so it is exactly the shape that ends up one row too
// tall and scrolls the alt screen away on every draw.

package main

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/ui"
)

// A bare /effort draws the menu, headed with who it is for.
//
// The header is the half that matters most here: an unaddressed command
// follows the default addressee, so the only thing that makes it safe is that
// the target is on screen before a key is pressed.
func TestABareEffortDrawsAMenuOnScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("/effort\r")
	// Claude's own seven levels, two of which --effort does not take. The last
	// one is the one a five-level list would be missing.
	s.await("ultracode")
	if got := s.text(); !strings.Contains(got, "effort for") {
		t.Errorf("the menu does not say what it will configure:\n%s", s.dump())
	}
}

// The reported bug, on a real terminal: the picker draws at the query bar, not
// at the top of the pane.
//
// It went up through App.paneChrome, which pins above the *transcript*, so the
// menu sat rows away from the composer that opened it - "the /effort menu
// appears at the top instead of the query bar". Asserted as an ordering within
// the pane: the banner heads the transcript, the picker is below it, and the
// composer's own box is below the picker.
func TestThePickerDrawsAtTheQueryBarNotThePaneTop(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("/effort\r")
	s.await("ultracode")

	lines := s.lines()
	banner, picker, composer := -1, -1, -1
	for i, row := range lines {
		if banner < 0 && strings.Contains(row, "v"+ui.Version) {
			banner = i
		}
		if picker < 0 && strings.Contains(row, "ultracode") {
			picker = i
		}
		if strings.Contains(row, "╭") { // the composer's rounded box, last chrome
			composer = i
		}
	}
	if banner < 0 || picker < 0 || composer < 0 {
		t.Fatalf("banner=%d picker=%d composer=%d: one is not on screen\n%s", banner, picker, composer, s.dump())
	}
	if banner >= picker || picker >= composer {
		t.Errorf("rows: banner=%d picker=%d composer=%d, want banner < picker < composer - the picker is pinned above the transcript, not at the query bar\n%s",
			banner, picker, composer, s.dump())
	}
}

// The menu goes away when it is cancelled, leaving the conversation as it was.
func TestEscapeTakesThePickerOffTheScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("/model\r")
	s.await("opusplan")

	s.send("\x1b") // esc
	s.settle()
	if got := s.text(); strings.Contains(got, "opusplan") {
		t.Errorf("esc left the menu on screen:\n%s", s.dump())
	}
}

// Choosing sends the command, and the conversation shows what was sent.
//
// The echo is the assertion: a level that changed with nothing in the
// transcript is a session whose configuration has no history.
func TestChoosingALevelPutsTheCommandInTheConversation(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("/effort\r")
	s.await("ultracode")

	// Down one from the top, then choose. The list is core.EffortCommands,
	// weakest first, so this is `medium` - named here rather than derived
	// because a screen test asserts about characters.
	s.send("\x1b[B\r")
	s.await("/effort medium")
}

// The picker draws inside the frame it was given.
//
// A block one row taller than its pane scrolls the alt screen on every draw,
// which is the bug the pty harness exists to catch and which no in-process
// assertion can see. The room's own furniture is the witness: if the menu
// pushed the frame past the bottom, the composer would be gone.
func TestThePickerLeavesTheComposerOnScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	// Deliberately short: ten options in a thirty-row terminal prove nothing,
	// and the overflow is what breaks.
	s := startWakeInAConversation(t, 100, 16)
	s.await("ready")

	s.send("/model\r")
	s.settle()

	lines := s.lines()
	if len(lines) != 16 {
		t.Fatalf("the terminal reports %d rows, want the 16 it was given", len(lines))
	}
	if strings.TrimSpace(strings.Join(lines, "")) == "" {
		t.Fatalf("the screen is blank with a menu up:\n%s", s.dump())
	}
	// The composer's own prompt is the floor: it is drawn under the menu, so it
	// is the first thing lost to a block that overflows.
	if !strings.Contains(s.text(), ">") {
		t.Errorf("the composer is not on screen under the menu:\n%s", s.dump())
	}
}
