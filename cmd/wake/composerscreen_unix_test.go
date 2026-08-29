//go:build unix

// What the composer actually draws while somebody types into it.
//
// Both of these failed the first time they were run, against a tree with 2,342
// green tests: the composer set its text area's width on a throwaway copy
// inside View, so the model that took the keystrokes wrapped them at bubbles'
// default 40 columns and scrolled its one-row viewport to a wrapped row the
// real width does not have. The draft - and the prompt with it - vanished.

package main

import (
	"strings"
	"testing"
	"time"
)

// closeRoster presses ⌃R, which these need because they are about the composer
// and not about the sidebar: it is open on arrival and takes a quarter of a
// 100-column terminal, so a draft measured against the full width would be
// measuring the layout instead.
func closeRoster(s *screen) {
	s.send("\x12")
	s.settle()
}

// A draft that fits the box is on screen, whole and on one row.
func TestATypedDraftIsOnScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	closeRoster(s)

	// 80 characters into a 100-column pane: comfortably inside the box, so
	// nothing about this depends on how the composer scrolls.
	draft := strings.Repeat("abcdefghij", 8)
	s.send(draft)
	s.await(draft)
}

// A draft longer than the box keeps its tail on screen - what is being typed
// now is the part that has to be visible.
func TestADraftWiderThanTheBoxKeepsItsTail(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	closeRoster(s)

	tail := "END-OF-THE-DRAFT"
	s.send(strings.Repeat("x", 200) + tail)
	s.await(tail)
}

// A draft with a newline in it is on screen whole, because the box grows to
// hold it.
//
// The tail test above passed while this failed, and the pair is the shape of
// the bug: the text area scrolls its viewport to the cursor, so the *end* of a
// long draft was always visible - and everything before it was not, because the
// box was one row however much had been typed into it. A draft you can only
// read the last row of is one you cannot check before sending.
func TestTheComposerGrowsToHoldAMultiLineDraft(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	closeRoster(s)

	before := boxRows(s)
	s.send("first line\x0asecond line") // ⌃J is the newline key
	s.await("second line")
	s.settle()

	if !strings.Contains(s.text(), "first line") {
		t.Fatalf("the first line of the draft is not on screen: the box did not grow with it.\n%s", s.dump())
	}
	if got := boxRows(s); got <= before {
		t.Errorf("the box is %d rows and was %d empty, so it did not grow.\n%s", got, before, s.dump())
	}
}

// The first line of a soft-wrapping draft stays on screen as the box grows.
//
// The owner's report - "when I get to the second line, sometimes the first line
// disappears" - and the whole difficulty was that word. Typing an unbroken run
// one character at a time crosses a row edge exactly, leaving the cursor on the
// phantom next wrapped row; the box was sized to the content rows, so its
// viewport scrolled the first line off to reach the cursor and never scrolled
// back. Only that exact landing triggered it, which is why a space-separated
// sentence usually stepped over it and a run of letters did not.
//
// One byte at a time with a pause between, so each is its own keystroke: a burst
// arriving in a single read is coalesced into one multi-rune KeyMsg, which the
// composer inserts whole and sweeps no intermediate column - so it never sits on
// the phantom edge and a batched paste would not reach the state.
func TestTheFirstLineOfAWrappingDraftStaysVisible(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.send("\x17") // ⌃W: back to the room, where the reported draft goes
	s.await("group chat")
	s.settle()

	const head = "HEADSTART"
	draft := head + strings.Repeat("qwertyuiop", 12) // unbroken, wraps char by char
	for i := 0; i < len(draft); i++ {
		s.send(draft[i : i+1])
		time.Sleep(12 * time.Millisecond) // separate reads, a frame between each
	}
	s.settle()

	if !strings.Contains(s.text(), head) {
		t.Fatalf("the start of the draft %q is off screen: the box scrolled the first line away as it wrapped and never brought it back.\n%s", head, s.dump())
	}
}

// boxRows is how many rows the composer's box spans on screen, read off its
// border corners rather than from any model.
func boxRows(s *screen) int {
	top, bottom := -1, -1
	for i, line := range s.lines() {
		switch {
		case strings.Contains(line, "╭"):
			top = i
		case strings.Contains(line, "╰") && top >= 0 && bottom < 0:
			bottom = i
		}
	}
	if top < 0 || bottom < 0 {
		return 0
	}
	return bottom - top + 1
}

// A word that spells a key name is typed, not pressed - and it reaches the
// agent whole.
//
// This is the third bug the owner hit and the one that looked like "I can't
// even submit a query sometimes". A run of typed characters arrives as one
// KeyMsg whose String() is the characters, and bubbles matches its keymap on
// that string, so "up" was tea.KeyUp, "end" was tea.KeyEnd and "delete"
// deleted. This sentence used to leave "  space" in the composer.
func TestAWordThatSpellsAKeyNameIsTyped(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	closeRoster(s)

	draft := "clean up and shut down then go left right home end tab esc enter delete space"
	s.send(draft)
	s.await(draft)

	s.send("\r")
	s.await(heardPrefix + draft)
}

// A draft that grows the box does not push the bottom of the frame off the
// screen.
//
// The owner's report, and the fourth bug found by looking at a real one: a few
// ⌃J in the room and the box grew past the height the pane was given, because
// the transcript above it was still laid out for the chrome the box had before.
// App.View cuts the overflow off the *bottom*, so the box looks right and the
// strip under it is what disappears.
//
// The strip is the assertion for that reason - it is the last row a pane can
// push off - and it is the one the unit test cannot make, since the frame it
// measures has already been cut.
func TestAGrownComposerDoesNotPushTheStripOffTheScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.send("\x17") // ⌃W: back to the room, which is where the draft goes
	s.await("group chat")
	s.settle()

	// ⌃J between them, which is the newline key - see NewComposer.
	s.send("one\x0atwo\x0athree\x0afour\x0afive")
	s.await("five")
	s.settle()

	if got := stripRow(s); !strings.Contains(got, "idle") && !strings.Contains(got, "working") {
		t.Fatalf("a five-line draft in the room left the strip reading %q: the pane drew past the height it was given and the cut took the bottom of the frame.\n%s", got, s.dump())
	}
}
