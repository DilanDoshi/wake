package ui

// ↑↓ and ↵ against a question card.
//
// The two keys the room already spends on the roster and on opening a
// conversation, claimed *only* while a question is being put in the focused
// pane. Everything here is about the boundary: a permission card must not take
// them, because a yes/no has nothing for a cursor to walk, and taking ↑↓ for as
// long as any agent is blocked would cost the roster its keys.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// paneAsking is john's conversation open and holding the keys with john blocked
// on the recorded question, at a width where the card is drawn in full. A
// conversation because that is the only surface a card is on: the room draws
// none. See App.cardOf.
func paneAsking(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(220, 40).withAgents("john", "sydney").
		openDMWith("s1", "john").opened(t)
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: questionAsk(t)})
}

// topCard is the card the focused pane is putting.
func topCard(t *testing.T, a App) Card {
	t.Helper()
	c, ok := a.cardOf(a.focus)
	if !ok {
		t.Fatal("the focused pane is putting no ask, so there is nothing to press a key against")
	}
	return c
}

func TestArrowsWalkTheOptionsOfAQuestionCard(t *testing.T) {
	a := paneAsking(t)
	if got := topCard(t, a).Option; got != 0 {
		t.Fatalf("the cursor starts on option %d, want 0", got)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if got := topCard(t, a).Option; got != 1 {
		t.Errorf("↓ left the cursor on option %d, want 1", got)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := topCard(t, a).Option; got != 0 {
		t.Errorf("↑ left the cursor on option %d, want 0", got)
	}
}

// ↵ with nothing armed answers the question the cursor is on, which is the key
// claude's own question screen uses. With something armed it keeps its one
// existing meaning - see the card's key line, which says which is live.
func TestEnterChoosesTheCursoredOptionWhenNothingIsArmed(t *testing.T) {
	a := paneAsking(t)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	card := topCard(t, a)
	if got := card.chosen(0); got != 1 {
		t.Errorf("↵ recorded option %d for the first question, want the cursored 1", got)
	}
	// And it advances, the way a digit does: the point of the key is to get
	// through the questions.
	if card.Cursor != 1 {
		t.Errorf("↵ left the cursor on question %d, want it moved on to 1", card.Cursor)
	}
}

// A permission card has nothing for a cursor to walk, so it must hand both keys
// back: ↑↓ is the roster's and ↵ opens the picked conversation, and an agent
// blocked on a yes/no would otherwise hold them for as long as it stayed
// blocked.
func TestAPermissionCardLeavesTheArrowsToTheRoster(t *testing.T) {
	a := newRoomApp(t).withSize(220, 40).withAgents("john", "sydney").
		openDMWith("s1", "john").opened(t)
	a.cards = a.cards.Add("s1", ask("r1", "Bash", "rm -rf build/"))
	if _, ok := a.cardOf(a.focus); !ok {
		t.Fatal("the focused pane is putting no card, so this test is not exercising the boundary")
	}

	before := a.roster.Selected
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if a.roster.Selected == before {
		t.Errorf("↓ did not move the roster while a permission card was up: the card took a key it has no meaning for")
	}
}
