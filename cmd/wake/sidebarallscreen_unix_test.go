//go:build unix

package main

// `all` is the first row of the roster's list, on a real screen: the wheel scrolls it
// away like any row, the channel a team's header picked stands while it is gone, and a
// click on it, once it is back, picks the whole room again. internal/ui holds the
// arithmetic; this is the half a person sees.

import (
	"strings"
	"testing"
)

func TestAllScrollsAwayWithTheListAndAPickedChannelStands(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	const cols = 100
	s := startWakeInAConversation(t, cols, 30)
	teamedFleet(t, s)  // the manager and a pool-named agent with no team; w1, w2 in backend; w3 in frontend
	s.resize(cols, 14) // a roster taller than its list, with backend's header still on screen
	s.awaitFrame("all drawn under the search bar", func(rows []string) bool { return rowIn(rows, "─ all · ") >= searchBarRows })
	s.settle()

	// Pick backend by its header: the room's title edge names it.
	s.click(boxDivider+8, boxRowOf(s, hasEdge("backend")))
	s.await("backend ─╮")

	// One notch down scrolls the roster by its first item, which is all.
	s.send(sidebarWheel(true, cols, 6))
	s.awaitFrame("all scrolled off", func(rows []string) bool { return rowIn(rows, "─ all · ") < 0 })
	s.settle()
	if !strings.Contains(s.text(), "backend ─╮") {
		t.Errorf("the room left the backend channel when all scrolled off:\n%s", s.dump())
	}
	if s.rowOf("no team") >= 0 && boxRowOf(s, hasEdge("no team")) < 0 {
		t.Errorf("the no team header is neither drawn nor gone with its box:\n%s", s.dump())
	}

	// A notch back brings all home, and a click on it picks the whole room.
	s.send(sidebarWheel(false, cols, 6))
	s.awaitFrame("all back under the bar", func(rows []string) bool { return rowIn(rows, "─ all · ") >= searchBarRows })
	s.settle()
	s.click(boxDivider+8, s.allRow())
	s.await("group chat ─╮")
}
