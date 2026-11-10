//go:build unix

// Channels on a real screen: a team header picked in the roster - by a click, or
// ⇧↑↓ onto it and ↵ - titles the room with the team and keeps the words of
// everyone outside it out, and `── all ──` brings them back. internal/ui proves
// the filter, the walk and the hit in process; this is the half a person sees.

package main

import (
	"strings"
	"testing"
)

func TestAChannelPickedInTheRosterFiltersTheRoom(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 200, 40)
	teamedFleet(t, s)
	s.send("@w1 alphaword\r")
	s.await("alphaword")
	s.send("@w3 gammaword\r")
	s.await("gammaword")
	s.settle()

	// A click on backend's header: the room's title edge names the team, and the
	// turn to w3, on frontend, leaves it.
	s.click(s.cols-3, s.rowOf("─ backend ─"))
	s.await("backend ─╮")
	s.awaitGone("gammaword")
	if !strings.Contains(s.text(), "alphaword") {
		t.Fatalf("the backend channel dropped the turn to w1, a member:\n%s", s.dump())
	}
	t.Logf("SCREENSHOT the backend channel:\n%s", s.dump())

	// all is pinned at the top; a click on it is the whole room again.
	s.click(s.cols-3, s.rowOf("─ all ─"))
	s.await("group chat ─╮")
	s.await("gammaword")

	// The cursor is on all: ⇧↑ wraps onto the last row (w3) and once more onto
	// frontend's header, and ↵ picks it.
	s.send("\x1b[1;2A") // ⇧↑
	s.send("\x1b[1;2A") // ⇧↑
	s.settle()
	s.send("\r")
	s.await("frontend ─╮")
	s.awaitGone("alphaword")
	if !strings.Contains(s.text(), "gammaword") {
		t.Fatalf("the frontend channel dropped the turn to w3, its member:\n%s", s.dump())
	}
	t.Logf("SCREENSHOT the frontend channel:\n%s", s.dump())

	// ⇧↓ through w3 wraps onto all, and ↵ widens the room again.
	s.send("\x1b[1;2B") // ⇧↓
	s.send("\x1b[1;2B") // ⇧↓
	s.settle()
	s.send("\r")
	s.await("group chat ─╮")
	s.await("alphaword")
}

// Your own send to a team outside the picked channel stays in the window that
// sent it, with a notice that its replies are under all - where they are.
func TestYourOwnSendStaysInTheChannelItWasTypedIn(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 200, 40)
	teamedFleet(t, s)
	s.settle()

	s.click(s.cols-3, s.rowOf("─ frontend ─"))
	s.await("frontend ─╮")
	s.send("@backend crossword\r")
	s.await("@backend is outside the frontend channel: replies show under all")
	s.await("@backend crossword")

	// Both backend members have answered once all shows both replies, so the
	// frontend channel drawing neither is the channel's choice, not a race.
	s.click(s.cols-3, s.rowOf("─ all ─"))
	s.await("group chat ─╮")
	s.await("@backend crossword")
	s.awaitCount("agent heard: crossword", 2)

	s.click(s.cols-3, s.rowOf("─ frontend ─"))
	s.await("frontend ─╮")
	s.await("@backend crossword")
	s.awaitCount("agent heard: crossword", 0)
	t.Logf("SCREENSHOT the frontend channel after @backend:\n%s", s.dump())
}
