//go:build unix

// The sidebars, on a real screen, in a window too short for the fleet.
//
// internal/ui holds the window arithmetic; this is the half that matters to a
// person: at a size where the column cannot hold everybody, does it say so, and
// can the cursor still reach the agents below the fold.

package main

import (
	"fmt"
	"strings"
	"testing"
)

// fleetOfNine starts eight more agents beside the one bare `wake` spawns, and
// leaves the room focused with the roster open.
//
// Nine rather than a handful because the column has to be too short for the
// fleet at a window a person would actually use: at five agents that means
// five rows, which is below the composer's own floor.
func fleetOfNine(t *testing.T, s *screen) []string {
	t.Helper()
	s.await("ready")
	s.send("\x17") // ⌃W
	room := s.awaitFrame("the room", func(rows []string) bool { return rowIn(rows, "group chat") >= 0 })

	// The one agent already on the roster - `/new` opens no pane and selects
	// nothing (starts.go's draftMention), so the cursor stays on whichever
	// agent startWakeInAConversation opened, which is this one, through the
	// whole loop below.
	before := agentsIn(room)
	if len(before) != 1 {
		t.Fatalf("want one agent on the roster before the fleet is grown, got %v.\n%s", before, s.dump())
	}

	// Names the pool cannot have already given the agent bare `wake` spawns.
	// The first draft used ordinary first names and flaked: the pool assigns
	// one of those to the first session, so `/new milo` collided with the milo
	// that was already there, and the test failed on a run where the pool
	// happened to pick it.
	names := []string{"w1", "w2", "w3", "w4", "w5", "w6", "w7", "w8"}
	for _, n := range names {
		s.send("/new " + n + "\r")
		// /new opens no pane and drafts a mention instead, so this waits on the
		// notice that says the start was acknowledged rather than on a pane
		// that never opens.
		s.await("@" + n + " started")
		// The room's draft is now "@n " (starts.go's draftMention), and these
		// are raw keystrokes typed into whatever the box already holds - so
		// without clearing it first, the next /new below is typed onto the end
		// of this one's mention and reaches that agent as an ordinary message
		// instead of Wake's router. esc clears the room's draft in one press -
		// settled before the next byte, or bubbletea reads the two as one
		// alt-modified key the way escprobe_test.go documents for two escapes.
		s.send("\x1b")
		s.settle()
		s.send("\x17") // ⌃W, harmless: the room is what has the keys throughout
		s.await("group chat")
	}
	// No ⌃R: the sidebar is open on arrival and nothing above closes it. Every
	// agent started has a row before the walk below reads the bottom one.
	all := agentsIn(s.awaitFrame("the whole fleet on the roster", func(rows []string) bool {
		return len(agentsIn(rows)) == len(before)+len(names)
	}))

	// Walk the cursor onto the bottom of the fleet as drawn, so the fold this
	// helper exists to test has something below it to reach - the cursor is
	// otherwise still on the one agent that predates the loop.
	s.pickRoster(before[0], all[len(all)-1])
	return names
}

// A column too short for the fleet says how many it left out.
func TestAShortRosterSaysHowManyAgentsAreHidden(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	fleetOfNine(t, s)
	if got := len(agentsOnRoster(s)); got != 9 {
		t.Fatalf("want nine agents on the roster before shrinking, got %d.\n%s", got, s.dump())
	}

	s.resize(100, 8)
	rows := s.awaitFrame("a column too short for nine agents saying some are hidden", sayHidden)

	if got := len(rosterNamesIn(rows)); got >= 9 {
		t.Fatalf("the column claims to have hidden agents and drew all %d of them.\n%s", got, s.dump())
	}
}

// ⇧↑↓ reaches an agent below the fold, and ⌃D opens the one it lands on.
//
// This is the defect end to end: before the window, the cursor moved onto rows
// the column had stopped drawing, so ⌃D opened a conversation with an agent the
// operator had never been shown.
func TestTheCursorReachesAnAgentBelowTheFold(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	fleetOfNine(t, s)
	s.resize(100, 8)
	shown := rosterNamesIn(s.awaitFrame("the column saying some agents are hidden", sayHidden))
	if len(shown) >= 9 {
		t.Fatalf("the column drew the whole fleet, so there is no fold to reach past.\n%s", s.dump())
	}

	// Up past the first drawn row. The cursor starts on the agent whose
	// conversation was opened last, which is the bottom of the fleet, so the
	// rows above the window are the ones to reach - and the window follows, so
	// a name appears that was not on screen a moment ago. ⇧↑ is the roster's key.
	for range len(shown) {
		s.send("\x1b[1;2A") // ⇧↑
	}
	freshIn := func(rows []string) string {
		for _, name := range rosterNamesIn(rows) {
			if !contains(shown, name) {
				return name
			}
		}
		return ""
	}
	s.awaitFrame(fmt.Sprintf("an agent the column had not drawn (it drew %v)", shown), func(rows []string) bool { return freshIn(rows) != "" })
	// Settled, so every ⇧↑ has landed and the first new row is the cursor's.
	after := s.settle()
	fresh := freshIn(after)
	if fresh == "" {
		t.Fatalf("the cursor moved past the fold and the column drew the same agents: %v then %v.\n%s", shown, rosterNamesIn(after), s.dump())
	}

	s.send("\x04") // ⌃D on the agent the cursor reached
	s.await("@" + fresh)
}

// sayHidden is a frame whose roster says how many agents its window leaves out.
func sayHidden(rows []string) bool { return rosterCount.MatchString(strings.Join(rows, "\n")) }

func contains(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
