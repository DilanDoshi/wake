//go:build unix

package main

// The wheel over the right sidebar scrolls the roster, on a real screen: a fleet
// taller than the column brings the agents it hid into view under the pointer,
// says which way the rest are, and the conversation beside it stays put.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// rosterCount is the roster's line for the agents its window leaves out.
var rosterCount = regexp.MustCompile(`\+\d+ (above|below|more)`)

// sidebarWheel is one SGR wheel report over the sidebar of a cols-wide screen,
// on its row y (0-based).
func sidebarWheel(down bool, cols, y int) string {
	button := 64
	if down {
		button = 65
	}
	return fmt.Sprintf("\x1b[<%d;%d;%dM", button, cols-2, y+1)
}

// leftOf is every row of the screen cut to the columns left of the sidebar.
func leftOf(s *screen, cols int) []string {
	out := slices.Clone(s.lines())
	for i, ln := range out {
		out[i] = ansi.Cut(ln, 0, cols-sidebarCols)
	}
	return out
}

// sidebarCols is the right sidebar's width with its rule.
const sidebarCols = 24

func TestTheWheelOverTheSidebarScrollsTheRosterOnAScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	const cols = 100
	s := startWakeInAConversation(t, cols, 30)
	fleetOfNine(t, s) // the cursor ends on the last agent
	s.resize(cols, 12)
	s.settle()

	before := agentsOnRoster(s)
	if len(before) >= 9 {
		t.Fatalf("a 12-row screen drew all nine agents, so there is nothing to scroll to.\n%s", s.dump())
	}
	if m := rosterCount.FindStringSubmatch(s.text()); m == nil || m[1] != "above" {
		t.Fatalf("with the cursor on the last agent the hidden ones are above, and the count reads %v.\n%s", m, s.dump())
	}
	left := leftOf(s, cols)

	s.send(sidebarWheel(false, cols, 4))
	s.settle()
	after := agentsOnRoster(s)
	if slices.Equal(after, before) || !slices.Contains(before, after[len(after)-1]) || slices.Contains(after, before[len(before)-1]) {
		t.Fatalf("a wheel notch up over the sidebar drew %v, want the window one agent back from %v.\n%s", after, before, s.dump())
	}
	if !strings.Contains(s.text(), "below") {
		t.Errorf("after scrolling back the count does not say an agent is below.\n%s", s.dump())
	}
	if got := leftOf(s, cols); !slices.Equal(got, left) {
		t.Errorf("the wheel over the sidebar moved the screen beside it.\n%s", s.dump())
	}

	s.send(sidebarWheel(true, cols, 4))
	s.settle()
	if got := agentsOnRoster(s); !slices.Equal(got, before) {
		t.Errorf("a notch back down drew %v, want %v.\n%s", got, before, s.dump())
	}
}
