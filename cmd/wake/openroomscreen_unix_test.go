//go:build unix

// Bare `wake` on a real screen: what the front door actually draws.
//
// openroom_test.go asserts on the frames the daemon received, which is where
// the *branch* shows. It cannot see the half this file is about - which pane
// has the terminal when the alt screen comes up - because both halves end in a
// Bubble Tea program and that is exactly what those tests arrange not to reach.

package main

import (
	"testing"
	"time"
)

// First run lands on the room, and the room has the screen to itself.
//
// It used to open the agent it had just spawned in a column beside the room, so
// the surface somebody runs `wake` to see was half a terminal wide - and below
// the takeover width not drawn at all, which is every ordinary terminal. The
// agent is still spawned, because a new user's first command has to produce
// one; it arrives as a roster row rather than as a pane nobody asked to open.
func TestFirstRunOpensTheRoomAndNotTheAgentItSpawned(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	// Above dmTakeoverColumns, so a second pane would be *drawn* rather than
	// hidden by the width. Below it this test would pass with the conversation
	// holding the whole screen, which is the failure it is about.
	s := startWake(t, 200, 40)
	s.await("ready")
	s.settle()

	if got := panesOnScreen(s); got != 1 {
		t.Fatalf("first run drew %d panes, want the room alone.\n%s", got, s.dump())
	}
	if got := paneTitles(s); len(got) != 1 || got[0] != "group chat" {
		t.Fatalf("the pane first run drew is %v, want [group chat]: the room is what `wake` is a "+
			"request about, and the agent it spawned is not a conversation anybody asked to open.\n%s",
			got, s.dump())
	}
	// And it did spawn. Without this the assertions above are also satisfied by
	// a first run that produces no agent at all, which is the other half of
	// what this branch owes a new user.
	if names := agentsOnRoster(s); len(names) != 1 {
		t.Fatalf("first run left %v on the roster, want one agent.\n%s", names, s.dump())
	}
}

// And the agent it spawned is one keypress away, which is what makes not
// opening it a placement rather than a loss.
//
// The row has to be *openable*, not merely drawn: first run is the one path
// where the session the room is over was started by this client, and a roster
// row nothing can open would turn a conversation that used to be on screen into
// one with no way in at all.
func TestFirstRunsAgentOpensFromItsRosterRow(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 200, 40)
	s.await("ready")
	s.settle()

	names := agentsOnRoster(s)
	if len(names) != 1 {
		t.Fatalf("first run left %v on the roster, want one agent.\n%s", names, s.dump())
	}
	s.openAgent(names[0])
	s.settle()

	if !titled(s, "@"+names[0]) {
		t.Fatalf("opening the agent first run spawned drew panes %v.\n%s", paneTitles(s), s.dump())
	}
}

// And on a terminal too narrow to draw the roster at all, which is where not
// opening the conversation could have stranded somebody.
//
// Below sidebarsHideColumns both sidebars are hidden by the width, so the row
// the test above clicks is not on screen - and `⇥` is no help either, because
// the ring is the room plus the conversations *already opened* and first run has
// opened none. What is left is `⇧↑↓` and `↵`, which read `layout.ShowRoster` -
// the operator's toggle, still true - rather than what the width drew. The old
// behaviour reached the agent by putting it on the screen; this has to reach it
// by a key, and at 80 columns that key is the only route there is.
func TestFirstRunsAgentIsReachableWhereTheRosterIsNotDrawn(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 80, 30)
	s.await("ready")
	s.settle()

	// Asserted before the keys as well as after, because the wait below is
	// "the title is no longer the room's" - and a condition that was already
	// true when the test started is one no keystroke has to satisfy.
	if got := paneTitles(s); len(got) != 1 || got[0] != "group chat" {
		t.Fatalf("first run drew %v at 80 columns, want the room alone.\n%s", got, s.dump())
	}
	if got := s.rosterNames(); len(got) != 0 {
		t.Fatalf("the roster draws %v at 80 columns, so this test is not about the case it names.\n%s",
			got, s.dump())
	}

	// ⇧↓ onto a row nothing is drawing (⇧↑↓ are the roster's now), then ↵ on an
	// empty draft.
	s.send("\x1b[1;2B")
	s.send("\r")

	// Waited for by title rather than by await(): the pooled name is not known
	// in advance, and every string a conversation puts on screen here - "@",
	// "╭", the legend - is already on the room's own frame at this width. What
	// changes is which conversation the one drawn pane is titled for, since
	// below the takeover the room goes off screen rather than shrinking.
	for deadline := time.Now().Add(screenTimeout); ; time.Sleep(20 * time.Millisecond) {
		if got := paneTitles(s); len(got) == 1 && got[0] != "group chat" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("↑↓ and ↵ opened no conversation on a screen with no roster on it: panes are "+
				"%v. The agent first run spawned would have no way in at this width.\n%s",
				paneTitles(s), s.dump())
		}
	}
}
