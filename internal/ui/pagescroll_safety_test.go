package ui

// What the page must never do to the card's rule: a key is live only while the
// whole card is on screen, and nothing the operator cannot see is sent.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// cardWholeOn reports whether every row of the card, headline through key line,
// is among rows: its top border and its bottom border are both drawn, exactly
// as many rows apart as the card is tall.
func cardWholeOn(rows []string, cardRows int) bool {
	top := paneRow(rows, "has a question")
	if top < 0 {
		return false
	}
	for i := top + 1; i < len(rows); i++ {
		if strings.HasPrefix(strings.TrimSpace(rows[i]), "╰") {
			return i-top+1 == cardRows
		}
	}
	return false
}

// cardFullyDrawn is a claim about the screen, so it is held to the screen: over
// every height, history, draft and scroll the claim and the rows agree - the
// keys are live when the whole card is drawn and dead when any row of it is not.
// The draft is blank lines, which grow the composer and leave the card's keys
// unpaused, and the status bar is on, because both change the rows under the card.
func TestCardFullyDrawnAgreesWithTheRowsDrawn(t *testing.T) {
	var sawWhole, sawCut bool
	for _, bar := range []bool{false, true} {
		for _, history := range []int{0, 40} {
			for height := 8; height <= 22; height++ {
				base := askingWith(t, pageSetup{height: height, history: history, bar: bar})
				for _, blank := range []int{0, 3, 9, 12} {
					grown := base
					for range blank {
						grown, _ = pressKey(grown, tea.KeyMsg{Type: tea.KeyCtrlJ})
					}
					for _, notches := range []int{-2, 0, 1, 4, 5} {
						a := wheeled(grown, tea.MouseButtonWheelUp, notches)
						if notches < 0 {
							a = wheeled(grown, tea.MouseButtonWheelDown, -notches)
						}
						width, _, _ := a.focusedPane()
						rows := pageRows(a)
						drawn := cardWholeOn(rows, lipgloss.Height(a.cardBlock("s1", width, false)))
						if claim := a.cardFullyDrawn(); claim != drawn {
							t.Fatalf("status bar %v, history %d, height %d, %d blank lines, %d notches: cardFullyDrawn()=%v but the whole card is drawn=%v:\n%s",
								bar, history, height, blank, notches, claim, drawn, strings.Join(rows, "\n"))
						}
						sawWhole, sawCut = sawWhole || drawn, sawCut || !drawn
					}
				}
			}
		}
	}
	if !sawWhole || !sawCut {
		t.Fatalf("the sweep never crossed the boundary (whole=%v cut=%v): the invariant was not exercised", sawWhole, sawCut)
	}
}

// An answer is irreversible, so ↵ commits only the box the operator can see: a
// page scrolled away from it brings it back, and the second ↵ is the one that sends.
func TestEnterOnAnAnswerScrolledOutOfSightBringsItBackFirst(t *testing.T) {
	a := askingPage(t, 40)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardDenyKey}})
	a = a.withDraft("because")
	a = wheeled(a, tea.MouseButtonWheelUp, 5)
	if hasRow(pageRows(a), "because") {
		t.Fatal("the reason is still on screen, so this test is not exercising a scrolled-away box")
	}

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Fatal("↵ answered with the box out of sight")
	}
	if !hasRow(pageRows(a), "because") {
		t.Errorf("↵ did not bring the box back:\n%s", strings.Join(pageRows(a), "\n"))
	}
	if _, cmd = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Error("the second ↵, with the box in view, did not answer")
	}
}

// A draft changed by any means is a draft the operator must be able to see: the
// next ↵ sends it. The write is what is watched, not the key that made it.
func TestAChangedDraftBringsTheComposerBack(t *testing.T) {
	a := askingWith(t, pageSetup{height: 40, history: pageHistory, sent: "earlier prompt"})
	a = wheeled(a, tea.MouseButtonWheelUp, 5)
	if a.cardFullyDrawn() || hasRow(pageRows(a), "@john") {
		t.Fatal("the card is whole or the composer is on screen, so nothing is scrolled away")
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp}) // recalls the sent prompt into the box

	rows := pageRows(a)
	if !hasRow(rows[len(rows)-3:], "earlier prompt") {
		t.Errorf("the recalled prompt is in a composer nobody can see:\n%s", strings.Join(rows, "\n"))
	}
}

// Home is for the card in front of it: another ask in a short pane opens with
// the composer out of sight again, and typing there brings it back.
func TestTypingForASecondCardBringsItsComposerBackToo(t *testing.T) {
	a := askingAfter(t, 10, 0, 0)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	first, _ := a.cards.For("s1")
	a.cards = a.cards.Settle("s1", first.RequestID)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyBackspace})

	next := *questionAsk(t)
	next.RequestID = first.RequestID + "-next"
	a = a.applyFrame(rpcEvent("s1", &next))
	if hasRow(pageRows(a), "@john") {
		t.Fatal("the second card opens with the composer on screen, so there is nothing to bring back")
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !hasRow(pageRows(a), "@john") {
		t.Errorf("typing under the second card left the composer out of sight:\n%s", strings.Join(pageRows(a), "\n"))
	}
}

// Choosing "Other…" opens the box the answer is typed in, as the refusal does.
func TestChoosingOtherBringsTheComposerBack(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 1)
	if hasRow(pageRows(a), "@john") || !a.cardFullyDrawn() {
		t.Fatal("one notch should take the composer off screen and leave the card whole")
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardFirstOption + 2}}) // 3 is Other…

	if rows := pageRows(a); !hasRow(rows[len(rows)-3:], a.answerTitle("s1")) {
		t.Errorf("the answer box opened out of sight:\n%s", strings.Join(rows, "\n"))
	}
}

// However deep the page is scrolled, drawing it costs the pane, not the
// transcript: a frame at the top of a long conversation allocates what a frame
// at the top of a short one does.
func TestADeepScrollCostsWhatAShallowOneDoes(t *testing.T) {
	cost := func(history int) float64 {
		a := askingAfter(t, 40, history, 0)
		card, _ := a.cards.For("s1")
		dm := *a.dms["s1"]
		dm.page = pageState{req: card.RequestID, up: 1 << 30, set: true} // the very top, clamped at the draw
		a = a.withDM("s1", dm)
		return testing.AllocsPerRun(5, func() { _ = a.View() })
	}
	shallow, deep := cost(100), cost(1500)

	if deep > shallow*1.5 {
		t.Errorf("a frame at the top of 1500 lines allocates %.0f, against %.0f at the top of 100: the draw is transcript-sized", deep, shallow)
	}
}

// A composer written back to its pane by name - what taking queued messages back,
// deleting a selection and a dropped image all do - changes the draft the same.
func TestADraftWrittenBackToAPaneBringsItsComposerBack(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 5)
	if hasRow(pageRows(a), "@john") {
		t.Fatal("the composer is on screen, so nothing is scrolled away")
	}

	a = a.withComposerFor("s1", a.dms["s1"].Composer().WithDraft("taken back"))

	if rows := pageRows(a); !hasRow(rows[len(rows)-3:], "taken back") {
		t.Errorf("the draft was written to a composer nobody can see:\n%s", strings.Join(rows, "\n"))
	}
}
