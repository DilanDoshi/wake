package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// A turn you sent to a team reads in that team's thread and in each member's,
// and nowhere else - not in the thread of an agent it never reached.
func TestATeamSendIsAddressedToItsMembers(t *testing.T) {
	a := teamRoom(t)
	a, _ = pressKey(a.withDraft("@backend run the migrations"), tea.KeyMsg{Type: tea.KeyEnter})
	const echo = "run the migrations"
	assertRoomShows(t, a, []string{echo}, nil)
	for _, c := range []struct {
		draft string
		seen  bool
	}{
		{"@backend ", true}, {"@sydney ", true}, {"@thea ", true},
		{"@delta ", false}, {"@iris ", false}, {"@frontend ", false},
	} {
		got := a.withDraft(c.draft)
		if seen := strings.Contains(shown(got), echo); seen != c.seen {
			t.Errorf("under %q your team turn is drawn = %v, want %v", c.draft, seen, c.seen)
		}
	}
}

// The room seed gives a never-opened DM the turns the operator addressed to its
// agent alone. A team turn is left out as a broadcast is: it is on each member's
// disk, and a member's queue is not something the seed can reconcile with, so a
// queued one would draw twice.
func TestTheRoomSeedLeavesOutATeamTurn(t *testing.T) {
	a := teamRoom(t)
	a, _ = pressKey(a.withDraft("@backend run the migrations"), tea.KeyMsg{Type: tea.KeyEnter})
	for _, id := range []string{"s1", "s2", "s3", "s4"} {
		for _, ev := range a.room.turnsFor(id) {
			if strings.Contains(ev.Text, "run the migrations") {
				t.Errorf("the room seed for %s holds the team turn", id)
			}
		}
	}
}
