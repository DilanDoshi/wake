//go:build unix

package main

// The room's working line on a real screen, while an agent is provably working.
//
// The in-process guards hold the arithmetic - one row, the frame's height, the
// chrome the transcript is sized against. This holds the thing they cannot: a
// terminal actually drawing it, in a room with no conversation beside it.

import (
	"strings"
	"testing"
)

func TestTheRoomDrawsAWorkingLineOnARealScreen(t *testing.T) {
	withScriptedAgent(t, scriptInterruptible)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()

	s.send("\x17") // ⌃W - close the conversation, leaving the room alone
	s.await("group chat")
	s.send("@" + name + " take your time\r")
	s.await(workingMarker)
	s.settle()

	// One row carrying both the agent's name and the minimal line's " for 49s"
	// age. Neither alone is evidence: the roster draws the name beside a spinner
	// glyph on every frame, so asserting on the name alone stays green with this
	// whole feature deleted - and " for " is the connector this line uses in
	// place of the DM line's parenthesised clause.
	found := ""
	for _, line := range s.lines() {
		if strings.Contains(line, name) && strings.Contains(line, " for ") {
			found = line
			break
		}
	}
	if found == "" {
		t.Fatalf("no row names %s beside the age of its turn: the room drew no working line while an agent was mid-turn.\n%s", name, s.dump())
	}

	// And the frame still fits: a row that appears without a resize is the
	// case that scrolls the alt screen away on every draw.
	if lines := s.lines(); len(lines) != 30 {
		t.Fatalf("the screen has %d rows, want 30.\n%s", len(lines), s.dump())
	}
	if strings.TrimSpace(s.lines()[0]) == "" {
		t.Fatalf("the top row is blank: the frame is taller than the terminal and has scrolled.\n%s", s.dump())
	}
}
