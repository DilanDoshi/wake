//go:build unix

package main

// A subagent, end to end, on a real screen: the fact that its work is not in
// the conversation, and the line its ending leaves behind.
//
// The running subagent's own row is the right sidebar's now (subroster_unix_test.go),
// and diving into one is the roster cursor's (rostersubs_test.go); this file keeps
// the two facts that are the conversation's own - what the subagent does *not* put
// in it, and what its ending *does*.

import (
	"strings"
	"testing"
)

// dispatchingScreen starts the fleet with an agent that runs a subagent per turn,
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

// The subagent's own words are *not* in the conversation: its work is a
// conversation of its own, not a paragraph in this one. Asserted after settle,
// because "it is not there" is not something waiting can decide.
func TestASubagentsWorkIsNotInTheConversation(t *testing.T) {
	s := dispatchingScreen(t)
	s.settle()

	if strings.Contains(s.text(), dispatchSaid) {
		t.Errorf("the subagent's own line is in the conversation:\n%s", s.dump())
	}
}

// The line the dispatch leaves behind, on a real screen. It is the half of a
// dispatch that belongs in the scrollback: the sidebar row says what is happening
// now, this says what happened - and it names the dispatch's description, not the
// last status a row was showing.
func TestADispatchEndingLeavesALineOnScreen(t *testing.T) {
	s := dispatchingScreen(t)

	s.await(`Subagent "` + dispatchLabel + `" finished`)
}

// And it stays put as the conversation grows, which is what makes it a record
// rather than a status. The rows above it are redrawn every frame; this is not.
func TestTheEndingLineStaysInTheScrollback(t *testing.T) {
	s := dispatchingScreen(t)
	s.await(`Subagent "` + dispatchLabel + `" finished`)

	// A second turn, which runs a second dispatch.
	s.send("again\r")
	s.await(heardPrefix + "dispatched")
	s.settle()

	if n := strings.Count(s.text(), `" finished`); n != 2 {
		t.Errorf("%d ending lines on screen, want 2 - one per dispatch:\n%s", n, s.dump())
	}
}
