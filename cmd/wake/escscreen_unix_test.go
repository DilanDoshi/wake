//go:build unix

package main

// ⎋⎋ clears a conversation's draft, from real bytes through a real terminal.
//
// The unit tests drive tea.KeyMsg values, which is the library already having
// decided what the bytes mean - and for this key that decision is the whole
// feature. Two escapes sharing one read arrive as a single `alt+esc`, so a
// build that handled only two separate messages would pass every in-process
// test and do nothing under a finger.

import (
	"strings"
	"testing"
)

func TestADoubleEscClearsTheDraftFromRealKeyBytes(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("scrap this")
	s.await("scrap this")

	// One press stops the turn and leaves the draft, which is the older rule
	// and the thing the second press exists to work around.
	s.send("\x1b")
	s.settle()
	if !strings.Contains(s.text(), "scrap this") {
		t.Fatalf("one ⎋ destroyed the draft in a conversation.\n%s", s.dump())
	}

	s.send("\x1b")
	s.settle()
	if strings.Contains(s.text(), "scrap this") {
		t.Fatalf("⎋⎋ left the draft in the composer.\n%s", s.dump())
	}
}

// And at the speed a person presses it, which is the case the in-process
// probe says arrives as one message rather than two.
func TestAFastDoubleEscClearsTheDraftFromRealKeyBytes(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")

	s.send("scrap this too")
	s.await("scrap this too")

	// One write, so the two escapes share a read exactly as they do under a
	// finger.
	s.send("\x1b\x1b")
	s.settle()
	if strings.Contains(s.text(), "scrap this too") {
		t.Fatalf("⎋⎋ in one read left the draft in the composer - the collapsed press was taken as a single ⎋.\n%s", s.dump())
	}
}
