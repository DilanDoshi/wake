package main

// /wake-help's shell section is `wake help`'s own text, handed to the model the
// room is built with - not a copy that could fall behind it.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/ui"
)

// helpOn types /wake-help with a filter into a model and draws it.
func helpOn(t *testing.T, app ui.App, filter string) string {
	t.Helper()
	return drawRoom(t, app,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/wake-help " + filter)},
		tea.KeyMsg{Type: tea.KeyEnter},
	)
}

func TestTheRoomHandsWakeHelpTheUsageWakeHelpPrints(t *testing.T) {
	held := &connection{}
	t.Cleanup(held.close)
	seed := fleetOf("sydney")

	for name, app := range map[string]ui.App{
		"bare wake":   conversationRoom(tempSocket(t), seed, nil, ui.Stream{}, held),
		"wake attach": conversation(tempSocket(t), seed.Sessions[0], seed, nil, ui.Stream{}, held),
	} {
		// A verb's own row, filtered to: the whole section cannot be on a 40-row screen.
		view := helpOn(t, app, "setup-terminal")
		if !strings.Contains(view, "wake setup-terminal") {
			t.Errorf("%s: /wake-help does not draw the shell usage `wake help` prints:\n%s", name, view)
		}
	}
}
