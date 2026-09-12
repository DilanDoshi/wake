//go:build unix

package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Clicking a character in the query box moves the insertion point onto it, on a
// real terminal. Proven by the observable effect rather than the caret's cell:
// click on the 'w' of "world", type a marker, and it lands right before the 'w'
// - which is only true if the click moved the caret there off the arrow keys.
func TestClickingTheQueryBoxMovesTheCaret(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	s.send("hello world")
	s.await("hello world")
	s.settle()

	row := s.rowOf("hello world")
	if row < 0 {
		t.Fatalf("the typed draft is not on screen to click.\n%s", s.dump())
	}
	// The cell column of 'w', not the byte offset: the box border '│' is a
	// multi-byte rune, so strings.Index (what colOf uses) sits two bytes past the
	// cell. A click two cells off the character lands on chrome and moves nothing.
	line := s.lines()[row]
	x := utf8.RuneCountInString(line[:strings.Index(line, "world")])

	s.click(x, row)
	s.settle()
	s.send("Z")
	s.await("hello Zworld")
	s.settle()

	got := s.rowOf("hello Zworld")
	if got < 0 {
		t.Errorf("after clicking the 'w' and typing a marker, the draft never read \"hello Zworld\":\n"+
			"the click did not move the caret onto the 'w'.\n%s", s.dump())
	}
}
