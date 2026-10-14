//go:build unix

package main

// The completion menu's ↵ and ⇥ through the real binary (the owner's 2026-10-12
// ruling, internal/ui/completionaccept_test.go): ↵ completes a half-typed word
// instead of sending it, and ⇥ stays in a `/` or `@` word nothing completes
// instead of jumping to another conversation. The unit tests decide each rule;
// this puts the key bytes through a pty.

import (
	"strings"
	"testing"
)

func completionKeysScreen(t *testing.T) *screen {
	t.Helper()
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	s := startWakeInAConversation(t, 110, 30)
	s.await("ready")
	s.settle()
	return s
}

// ↵ over a half-typed Wake command completes it: nothing is sent, and the word
// that was being typed becomes the whole of it.
func TestEnterCompletesAHalfTypedCommandOnScreen(t *testing.T) {
	s := completionKeysScreen(t)

	s.send("/resu")
	s.await(completionKeyHint)
	s.send("\r")
	s.awaitGone(completionKeyHint)
	s.settle()

	if strings.Contains(s.text(), heardPrefix+"/resu") {
		t.Fatalf("↵ sent the half-typed command to the agent.\n%s", s.dump())
	}
	if rows := s.rowsWith("/resume"); len(rows) != 1 {
		t.Fatalf("%d rows hold /resume, want the one in the composer.\n%s", len(rows), s.dump())
	}
}

// The bug as it was reported: a misspelled skill leaves no menu, and ⇥ used to
// take the keys to the next conversation. The next key still lands in the draft.
func TestTabStaysInAMisspelledSkillOnScreen(t *testing.T) {
	s := completionKeysScreen(t)

	s.send("/zzzskil")
	s.settle()
	if strings.Contains(s.text(), completionKeyHint) {
		t.Fatalf("`/zzzskil` drew a menu, so this is not the no-offer case.\n%s", s.dump())
	}
	s.send("\t")
	s.settle()
	s.send("x")
	s.await("/zzzskilx")
}

// On a draft that is not a word being typed ⇥ is still the ring: the keys leave
// the conversation, so the next key does not extend its draft.
func TestTabStillMovesTheKeysOnPlainTextOnScreen(t *testing.T) {
	s := completionKeysScreen(t)

	s.send("hello")
	s.await("hello")
	s.send("\t")
	s.settle()
	s.send("y")
	s.settle()

	if strings.Contains(s.text(), "helloy") {
		t.Fatalf("⇥ left the keys in the conversation's draft.\n%s", s.dump())
	}
}
