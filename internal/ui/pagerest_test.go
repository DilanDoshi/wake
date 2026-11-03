package ui

// Where a page rests is the step's and the draft's: an answer in your own
// words, a stray character deleted, or a wheel on one question leaves the next
// step whole and answerable. A rest keyed on the ask alone outlived the step it
// was set on and opened the next with its top cut and its keys dead (BUG-59 by
// another path; the second review).

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// clippedByComposer is the recorded card in a pane that holds it only with the
// composer scrolled away: the rest the page chooses is what decides its keys.
func clippedByComposer(t *testing.T) App {
	t.Helper()
	a := askingPage(t, 14)
	if !a.cardFullyDrawn() || hasRow(pageRows(a), "@john") {
		t.Fatal("the card is not whole with the composer away, so this pane is not the one under test")
	}
	return a
}

func TestAnAnswerInYourOwnWordsLeavesTheNextStepWhole(t *testing.T) {
	a := clippedByComposer(t)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown}) // Other…
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	a = a.withDraft("my own words")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if c := topCard(t, a); c.Cursor != 1 || c.typed(0) == "" {
		t.Fatalf("the typed answer did not take and move on: cursor %d, typed %q", c.Cursor, c.typed(0))
	}
	if !a.cardFullyDrawn() {
		t.Errorf("the second question opened with its keys dead:\n%s", frameOf(a))
	}
}

func TestAStrayCharacterDeletedLeavesTheCardWhole(t *testing.T) {
	a := clippedByComposer(t)
	a = a.withDraft("x")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyBackspace})
	if !a.composerEmpty() {
		t.Fatal("the character was not deleted")
	}
	if !a.cardFullyDrawn() {
		t.Errorf("the card stayed cut after its draft emptied:\n%s", frameOf(a))
	}
}

func TestAWheelOnOneQuestionDoesNotCutTheNext(t *testing.T) {
	var seen bool
	for height := 12; height <= 30; height++ {
		a := askingPage(t, height)
		a = wheeled(a, tea.MouseButtonWheelUp, 1)
		if !a.cardFullyDrawn() {
			continue // the notch cut the first question: nothing to press
		}
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
		if c := topCard(t, a); c.Cursor != 1 {
			t.Fatalf("at %d rows ↵ did not move on to the second question: cursor %d", height, c.Cursor)
		}
		width, pane, _ := a.focusedPane()
		if lipgloss.Height(a.cardBlock("s1", width, false)) > pane {
			continue // the second question outgrows the pane itself
		}
		seen = true
		if !a.cardFullyDrawn() {
			t.Errorf("at %d rows the second question inherited the first one's scroll and opened cut:\n%s", height, frameOf(a))
		}
	}
	if !seen {
		t.Fatal("no height let the first question be answered after a notch: the test asserted nothing")
	}
}

// An emptied box in answer mode is still the answer's: the page stays on it.
func TestAnEmptiedAnswerBoxStaysInView(t *testing.T) {
	a := clippedByComposer(t)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardDenyKey}})
	a = a.withDraft("n")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyBackspace})
	if rows := pageRows(a); !hasRow(rows[len(rows)-3:], "deny reason") {
		t.Errorf("the reason box left the screen when its words were deleted:\n%s", frameOf(a))
	}
}

func frameOf(a App) string { return stripANSI(a.View()) }
