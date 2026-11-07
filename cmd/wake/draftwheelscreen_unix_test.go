//go:build unix

package main

// The wheel over a draft taller than its query box scrolls the draft, on a real
// screen: mouse reports read off a tty by the real binary, and rows vt10x drew.
// internal/ui holds the geometry (composerscroll_test.go).

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTheWheelOverTheQueryBoxScrollsALongDraft(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 40)
	s.await("ready")
	s.settle()

	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("draftline %02d", i)
	}
	s.send(strings.Join(lines, "\x0a")) // ⌃J is the newline key
	s.await("draftline 19")
	s.settle()
	if s.rowOf("draftline 00") >= 0 {
		t.Fatalf("the box shows the draft's first line already, so it is not taller than the box.\n%s", s.dump())
	}

	// A cell on one of the box's drawn rows, found on screen: SGR is 1-based.
	row := s.rowOf("draftline 19")
	line := s.lines()[row]
	x := utf8.RuneCountInString(line[:strings.Index(line, "draftline")])
	wheel := func(button int) {
		s.send(fmt.Sprintf("\x1b[<%d;%d;%dM", button, x+1, row+1))
		s.settle()
	}

	for range 4 {
		wheel(64) // up
	}
	s.await("draftline 00")
	s.awaitGone("draftline 19")

	for range 4 {
		wheel(65) // down, back to the caret's rows
	}
	s.await("draftline 19")
	s.awaitGone("draftline 00")

	// Scrolled away again, a key brings the view back to the caret, which never
	// moved: the marker lands at the draft's end.
	for range 4 {
		wheel(64)
	}
	s.await("draftline 00")
	s.send("Z")
	s.await("draftline 19Z")
	s.awaitGone("draftline 00")
}
