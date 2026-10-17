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
