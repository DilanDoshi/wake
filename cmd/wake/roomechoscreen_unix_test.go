//go:build unix

// A message routed from the room, on a real screen: it is in the room, it is
// in the conversation it was addressed to, and the conversation says where it
// came from.
//
// internal/ui asserts the same three facts against the model. This drives the
// real binary through a pty, which is the only thing that can say the second
// copy is drawn where a person would look for it rather than merely held.

package main

import (
	"strings"
	"testing"
)

// The room's message appears in the addressed agent's pane, headed as one from
// the room and still carrying the mention.
func TestARoomMessageIsDrawnInTheAddressedPane(t *testing.T) {
	s, names := gridScreen(t, 200, 40)

	s.send("\x04") // ⌃D opens the picked agent beside the room
	s.await("@" + names[0])
	s.send("\t") // ⇥ puts the keys back on the room
	s.settle()

	s.send("@" + names[0] + " ship it\r")
	s.await("from the room")
	s.settle()

	if n := strings.Count(s.text(), "ship it"); n < 2 {
		t.Fatalf("the message is drawn %d times, want it in the room and in %s's pane:\n%s", n, names[0], s.dump())
	}
	if !strings.Contains(s.text(), "@"+names[0]+" ship it") {
		t.Errorf("the pane dropped the mention, which is what says the message was routed rather than typed there:\n%s", s.dump())
	}
}
