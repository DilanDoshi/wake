//go:build unix

package main

// A double- and a triple-click on a real screen. What was taken is the cells a
// highlight lands on, so only a terminal can show that a double-click took
// exactly the word and a triple-click exactly the row.

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// clickRunGap is longer than a run's window, so the click after it starts a
// new run rather than continuing the last one.
const clickRunGap = 700 * time.Millisecond

// A double-click highlights the word under it, a path whole; a triple-click
// after it highlights the row from its first character to its last.
func TestADoubleClickHighlightsTheWordAndATripleTheRow(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.send("check internal/ui/mouse.go today\r")
	s.await(heardPrefix + "check")
	s.settle()

	row := s.rowOf(heardPrefix)
	col := s.colOf(row, "internal/ui/mouse.go")
	if col < 0 {
		t.Fatalf("the reply is not on screen.\n%s", s.dump())
	}
	before := s.highlightedCells()

	s.click(col+3, row)
	s.click(col+3, row)
	s.settle()
	if got, want := s.highlightedCells()-before, len("internal/ui/mouse.go"); got != want {
		t.Errorf("a double-click highlighted %d cells, want the %d of the word under it.\n%s", got, want, s.dump())
	}

	time.Sleep(clickRunGap)
	line := strings.TrimSpace(strings.TrimRight(s.lines()[row], "│"))
	for range 3 {
		s.click(col+3, row)
	}
	s.settle()
	if got, want := s.highlightedCells()-before, utf8.RuneCountInString(line); got != want {
		t.Errorf("a triple-click highlighted %d cells, want the %d of the row %q.\n%s", got, want, line, s.dump())
	}
}
