//go:build unix

package main

// A subagent, end to end, on a real screen: the row it puts under the
// conversation, the fact that its work is not in the conversation, and the two
// keys that go into its transcript and back.
//
// Everything below the airlock is asserted in unit tests. This is the only
// thing in the tree that puts a dispatch through the real binary, a real pty
// and a terminal emulator, which is what the last round of layout bugs needed:
// the rows are chrome, and chrome that is not budgeted for makes a frame taller
// than its terminal.

import (
	"strings"
	"testing"
)

// dispatching starts the fleet with an agent that runs a subagent per turn,
// opens the conversation, and takes one turn.
func dispatchingScreen(t *testing.T) *screen {
	t.Helper()
	withScriptedAgent(t, scriptDispatches)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 110, 34)
	s.await("ready")
	s.send("\x04") // ⌃D opens the conversation into the focused pane
	s.settle()

	s.send("go\r")
	s.await(heardPrefix + "dispatched")
	return s
}

// The row is on screen, carrying what Claude Code's own row carries: the type,
// what the subagent is doing, and what it has spent.
func TestADispatchGetsARowUnderTheConversation(t *testing.T) {
	s := dispatchingScreen(t)

	s.await(dispatchType)
	s.await(dispatchDoing)
	// 29.0k rather than the 27.0k task_progress reported: the ending frame
	// carries a final usage and the newest number wins, which is what a
	// finished row should show.
	if got := s.text(); !strings.Contains(got, "29.0k tokens") {
		t.Errorf("the row is missing its token count:\n%s", s.dump())
	}
}

// And the subagent's own words are *not* in the conversation, which is the bug
// this whole change is about. Asserted after settle, because "it is not there"
// is not something waiting can decide.
func TestASubagentsWorkIsNotInTheConversation(t *testing.T) {
	s := dispatchingScreen(t)
	s.settle()

	if strings.Contains(s.text(), dispatchSaid) {
		t.Errorf("the subagent's own line is in the conversation:\n%s", s.dump())
	}
}

// ⌃N walks onto the dispatch and ↵ opens it, which is where its work went.
func TestOpeningADispatchShowsItsTranscript(t *testing.T) {
	s := dispatchingScreen(t)

	s.send("\x0e") // ⌃N onto the conversation's own row
	s.send("\x0e") // ⌃N onto the dispatch
	s.send("\r")

	s.await(dispatchSaid)
}

// And ↵ on the conversation's own row is the way back.
func TestLeavingADispatchShowsTheConversationAgain(t *testing.T) {
	s := dispatchingScreen(t)

	s.send("\x0e")
	s.send("\x0e")
	s.send("\r")
	s.await(dispatchSaid)

	s.send("\x10") // ⌃P back onto the conversation's row
	s.send("\r")
	s.await(heardPrefix + "dispatched")

	s.settle()
	if strings.Contains(s.text(), dispatchSaid) {
		t.Errorf("the subagent's transcript is still drawn after leaving it:\n%s", s.dump())
	}
}

// The rows are chrome, so they come out of the transcript's height rather than
// making the frame taller. A frame one row too tall scrolls the alt screen away
// on every draw, which no in-process test can see.
func TestTheDispatchListDoesNotPushTheComposerOffScreen(t *testing.T) {
	s := dispatchingScreen(t)
	s.settle()

	lines := s.lines()
	if len(lines) != 34 {
		t.Fatalf("the screen is %d rows, want the 34 it was started at", len(lines))
	}
	// The composer's own box has to still be on screen, and below the rows.
	box := -1
	for i, l := range lines {
		if strings.Contains(l, "╰") {
			box = i
		}
	}
	if box < 0 {
		t.Fatalf("the composer's box is not on screen at all:\n%s", s.dump())
	}
	if row := s.rowOf(dispatchLabel); row < 0 {
		t.Fatalf("the dispatch row is not on screen:\n%s", s.dump())
	} else if row > box {
		t.Errorf("the dispatch row is at %d, below the composer at %d", row, box)
	}
}

// The line the dispatch leaves behind, on a real screen. It is the half of a
// dispatch that belongs in the scrollback: the rows say what is happening now,
// this says what happened.
func TestADispatchEndingLeavesALineOnScreen(t *testing.T) {
	s := dispatchingScreen(t)

	// The dispatch's own description, not the last status the row was showing:
	// the row answers "what is it doing" and this answers "what was it for".
	s.await(`Subagent "` + dispatchLabel + `" finished`)
	s.await(dispatchDoing)
}

// And it stays put as the conversation grows, which is what makes it a record
// rather than a status. The rows above it are redrawn every frame; this is not.
func TestTheEndingLineStaysInTheScrollback(t *testing.T) {
	s := dispatchingScreen(t)
	s.await(`Subagent "` + dispatchLabel + `" finished`)

	// A second turn, which puts a second dispatch under the first.
	s.send("again\r")
	s.await(heardPrefix + "dispatched")
	s.settle()

	if n := strings.Count(s.text(), `" finished`); n != 2 {
		t.Errorf("%d ending lines on screen, want 2 - one per dispatch:\n%s", n, s.dump())
	}
}
