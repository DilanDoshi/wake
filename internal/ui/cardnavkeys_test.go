package ui

// Which card keys survive a draft, and which may not.
//
// cardkeys.go read *every* card key only when the composer was empty, and the
// argument for that gate is about characters: `a` and `d` are letters people
// type, and the first character of any draft is typed into an empty composer.
// It was never an argument about arrows. Nobody types ↑ into a draft - so the
// arrows reverted to the roster the moment anything was typed, with the card
// still up and still asking, which is the state somebody is most likely to be
// in when they start writing a reply.

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typing puts a draft in the focused composer without going through a card key.
func typing(a App) App {
	a, _ = press(a, 'x')
	return a
}

func TestTheArrowsReachTheCardEvenWithADraftInTheComposer(t *testing.T) {
	a := typing(paneAsking(t))
	if a.composerEmpty() {
		t.Fatal("the draft did not reach the composer, so this test cannot see the gate")
	}
	before := topCard(t, a).Option

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if got := topCard(t, a).Option; got == before {
		t.Errorf("↓ left the option cursor on %d with a draft in the composer - the card is still asking, and its arrows went to the roster instead", got)
	}
}

// The other half, and the one the gate exists for. A permission grant that can
// happen while somebody writes the word "analyse" is the unsafe direction of
// exactly this failure, so the character keys keep the gate the arrows lose.
func TestTheCharacterKeysStillRequireAnEmptyComposer(t *testing.T) {
	a := typing(paneAsking(t))
	before := topCard(t, a)

	for _, r := range []rune{cardAllowKey, cardDenyKey, cardFirstOption} {
		a2, _ := press(a, r)
		card := topCard(t, a2)
		if _, armed := a2.cards.armedKey(card); armed {
			t.Errorf("%q armed a settle while a draft was in the composer", r)
		}
		if card.chosen(card.Cursor) != before.chosen(before.Cursor) {
			t.Errorf("%q chose an option while a draft was in the composer", r)
		}
	}
}

// --- ←→ walk the steps ----------------------------------------------------

func TestLeftAndRightWalkTheStepsOfAnAsk(t *testing.T) {
	a := paneAsking(t)
	if n := len(topCard(t, a).Detail.Questions); n < 2 {
		t.Fatalf("the recorded ask puts %d questions, so there is nothing to walk between", n)
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRight})
	if got := topCard(t, a).Cursor; got != 1 {
		t.Errorf("→ left the cursor on step %d, want 1", got)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyLeft})
	if got := topCard(t, a).Cursor; got != 0 {
		t.Errorf("← left the cursor on step %d, want 0", got)
	}
}

// → reaches the review without answering anything, which is what makes the
// strip's Submit tab an honest affordance rather than a label.
func TestRightReachesTheReviewStep(t *testing.T) {
	a := paneAsking(t)
	for range len(topCard(t, a).Detail.Questions) {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRight})
	}
	if !topCard(t, a).OnReview() {
		t.Errorf("walking → past the last question landed on step %d, not the review", topCard(t, a).Cursor)
	}
}

// A non-empty draft needs ←→ for its own cursor - which is also what makes an
// answer typed into the composer editable. The claim is only ever on an empty
// one, where they reached the text area and did nothing at all.
func TestLeftAndRightStayWithTheDraftWhenThereIsOne(t *testing.T) {
	a := typing(paneAsking(t))
	before := topCard(t, a).Cursor

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRight})
	if got := topCard(t, a).Cursor; got != before {
		t.Errorf("→ moved the card from step %d to step %d while a draft was in the composer, taking the key the draft's own cursor needs", before, got)
	}
}
