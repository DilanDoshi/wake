//go:build unix

package main

import (
	"strings"
	"testing"
)

// The board takes over the frame and gives it back, on a real screen.
//
// The board is the one surface that replaces the whole grid, and HANDOFF's
// lesson is that 2,342 in-process tests were green over rendered-frame bugs -
// so the takeover and the way back are asserted on a pty rather than assumed.
func TestTheBoardTakesTheFrameAndEscGivesItBack(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	names := agentsOnRoster(s)
	if len(names) != 1 {
		t.Fatalf("want one agent on the roster, got %v.\n%s", names, s.dump())
	}
	name := names[0]

	s.send("/board\r")
	s.await("BOARD")
	s.settle()
	if !strings.Contains(s.text(), name) {
		t.Fatalf("the board draws no row for @%s.\n%s", name, s.dump())
	}
	// The takeover is real: the composer's legend is not on screen.
	if strings.Contains(s.text(), "interrupt") {
		t.Fatalf("the pane legend is still drawn under the board.\n%s", s.dump())
	}

	s.send("\x1b") // esc
	s.await("interrupt") // the legend is back, a durable fact on every frame
}
