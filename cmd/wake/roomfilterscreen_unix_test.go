//go:build unix

// The room narrowing to a lone @name, on a real screen: two of the operator's
// own messages to two different agents, then typing @name0 collapses the room
// to that agent's thread and widens it again when the draft clears.
//
// internal/ui proves the predicate, the wire and the history fold against the
// model. This drives the real binary through a pty - the only thing that can
// say the narrowing is what a person actually sees, header and all.

package main

import (
	"strings"
	"testing"
)

func TestRoomNarrowsToAtNameOnScreen(t *testing.T) {
	s, names := gridScreen(t, 200, 40)

	// Two of the operator's own messages, addressed to two different agents,
	// both echoed into the room. Distinctive words so the assertions cannot
	// collide with a name, a label or a scripted reply.
	s.send("@" + names[0] + " alphaword\r")
	s.await("alphaword")
	s.send("@" + names[1] + " betaword\r")
	s.await("betaword")
	s.settle()

	if !strings.Contains(s.text(), "alphaword") || !strings.Contains(s.text(), "betaword") {
		t.Fatalf("baseline room is missing an echo, so the filter proves nothing:\n%s", s.dump())
	}

	// Type a lone @name0 (no send): the room narrows to that agent's thread, and
	// the header names the focus.
	s.send("@" + names[0] + " ")
	s.await("› @" + names[0])
	s.settle()

	if !strings.Contains(s.text(), "alphaword") {
		t.Fatalf("the focused room dropped the message to %s:\n%s", names[0], s.dump())
	}
	if strings.Contains(s.text(), "betaword") {
		t.Fatalf("the focused room still shows the message to %s, want only %s's thread:\n%s", names[1], names[0], s.dump())
	}

	// Clearing the draft widens the room again - the message to the other agent
	// comes back.
	s.send("\x1b") // esc clears the room draft
	s.await("betaword")
}
