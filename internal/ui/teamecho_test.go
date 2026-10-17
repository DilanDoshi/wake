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
// agent. A team turn was addressed to each member, and is on each one's disk.
func TestTheRoomSeedHoldsATeamTurnForEachMemberAndNoOneElse(t *testing.T) {
	a := teamRoom(t)
	a, _ = pressKey(a.withDraft("@backend run the migrations"), tea.KeyMsg{Type: tea.KeyEnter})
	for id, want := range map[string]bool{"s1": true, "s2": true, "s3": false, "s4": false} {
		var got bool
		for _, ev := range a.room.turnsFor(id) {
			got = got || strings.Contains(ev.Text, "run the migrations")
		}
		if got != want {
			t.Errorf("the room seed for %s holds the team turn = %v, want %v", id, got, want)
		}
	}
}
