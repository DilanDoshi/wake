package ui

// A multi-select question, drawn and answered the way Claude Code's own
// question screen does it (2.1.302, docs/superpowers/notes/2026-10-18-multiselect-
// question-findings.md): a box on every row, ↵ / a digit / space ticks without
// moving on, the Other row is a box too, a Next or Submit row walks on, and the
// answer is the labels in the order ticked with the operator's words last.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const multiFixture = "question-multiselect.jsonl"

// paneAskingMany is john's conversation with john blocked on the recorded
// multi-select question.
func paneAskingMany(t *testing.T) App {
	t.Helper()
	ev := recordedAsks(t, multiFixture)[0]
	a := newRoomApp(t).withSize(220, 40).withAgents("john", "sydney").
		openDMWith("s1", "john").opened(t)
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
	if q, ok := topCard(t, a).question(); !ok || !q.Multi {
		t.Fatal("the recorded multi-select ask did not reach the card as one")
	}
	return a
}

func enter(a App) App {
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	return a
}

func cardView(c Card) string {
	return ansi.Strip(oneCard(c).topView(wideRoom, Agent{Name: "john"}))
}

func TestAMultiSelectQuestionDrawsABoxOnEveryRow(t *testing.T) {
	card := cardFor(t, recordedAsks(t, multiFixture)[0])
	q := card.Detail.Questions[0]
	out := cardView(card)
	for _, o := range q.Options {
		if !strings.Contains(out, multiOff+o.Label) {
			t.Errorf("option %q has no empty box:\n%s", o.Label, out)
		}
	}
	if !strings.Contains(out, multiOff+otherLabel) {
		t.Errorf("the Other row has no box:\n%s", out)
	}
	if row := rowAfter(out, multiOff+otherLabel); !strings.Contains(row, stripSubmitLabel) || strings.Contains(row, multiNextLabel) {
		t.Errorf("the row under the last question's Other row is %q, want %q:\n%s", row, stripSubmitLabel, out)
	}

	out = cardView(card.Toggle(0, 1))
	if !strings.Contains(out, multiOn+q.Options[1].Label) {
		t.Errorf("a ticked option is not drawn ticked:\n%s", out)
	}
	if strings.Contains(out, cardChosen) {
		t.Errorf("a multi-select row wears the single choice's marker:\n%s", out)
	}
	if !strings.Contains(out, "[1-5] toggle") || strings.Contains(out, "choose") {
		t.Errorf("the key line does not say the digits toggle:\n%s", out)
	}
}

// rowAfter is the drawn row under the first one containing marker.
func rowAfter(out, marker string) string {
	lines := strings.Split(out, "\n")
	for i, l := range lines[:len(lines)-1] {
		if strings.Contains(l, marker) {
			return lines[i+1]
		}
	}
	return ""
}

// ↵ ticks the cursored option and stays put, and a second ↵ unticks it: one
// question takes several answers, so choosing one cannot move on.
func TestEnterTicksTheCursoredOptionAndStays(t *testing.T) {
	a := paneAskingMany(t)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a = enter(a)

	card := topCard(t, a)
	if !card.ticked(0, 1) || card.Cursor != 0 || card.Option != 1 {
		t.Fatalf("↵ did not tick option 1 in place: ticks %v, cursor %d, option %d", card.Ticks, card.Cursor, card.Option)
	}
	if card := topCard(t, enter(a)); card.ticked(0, 1) {
		t.Error("a second ↵ left option 1 ticked")
	}
}

// A digit ticks its option and space ticks the cursored one, both in place.
func TestADigitOrSpaceTicksInPlace(t *testing.T) {
	a := paneAskingMany(t)
	a, _ = press(a, cardFirstOption+2)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})

	card := topCard(t, a)
	if !card.ticked(0, 2) || !card.ticked(0, 0) || card.Cursor != 0 || card.Option != 0 {
		t.Errorf("digit 3 then space ticked %v with the cursor at %d/%d", card.Ticks, card.Cursor, card.Option)
	}
	if !a.composerEmpty() {
		t.Errorf("space reached the composer as a character: %q", a.composer().Value())
	}
}

// The answer is what Claude Code sends: the labels in the order ticked, the
// operator's own words last, joined by the airlock.
func TestTheOrderTickedIsTheOrderSent(t *testing.T) {
	const mine = "anchovies"
	a := paneAskingMany(t)
	q := topCard(t, a).Detail.Questions[0]

	a, _ = press(a, cardFirstOption+1)
	a, _ = press(a, cardFirstOption)
	a, _ = press(a, rune(cardFirstOption+otherIndex(q)))
	a = enter(typeDraft(a, mine))
	card := topCard(t, a)
	if card.Cursor != 0 || card.typed(0) != mine || len(card.Ticks[0]) != 2 {
		t.Fatalf("a typed answer moved on or cost the ticks: cursor %d, typed %q, ticks %v", card.Cursor, card.typed(0), card.Ticks)
	}

	want := core.JoinChoices([]string{q.Options[1].Label, q.Options[0].Label, mine})
	a = enter(walkTo(a, nextIndex(q)))
	if !topCard(t, a).OnReview() {
		t.Fatal("↵ on the Submit row did not reach the review")
	}
	if out := cardView(topCard(t, a)); !strings.Contains(out, want) {
		t.Errorf("the review does not show %q:\n%s", want, out)
	}
	if rows := topCard(t, a).resolvedAnswers(); len(rows) != 1 || !strings.HasSuffix(rows[0], want) {
		t.Errorf("the room record reads %q, want it to end %q", rows, want)
	}

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if got := sentFrame(t, a, cmd).Answers[q.Text]; got != want {
		t.Errorf("sent %q, want %q", got, want)
	}
}

// walkTo puts the option cursor on one row of the current question.
func walkTo(a App, option int) App {
	for range option {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	}
	return a
}

// The Other row is a box: words in it tick it, and choosing it again clears
// them rather than opening the composer over an answer already given.
func TestChoosingAFilledOtherRowClearsIt(t *testing.T) {
	a := paneAskingMany(t)
	other := rune(cardFirstOption + otherIndex(topCard(t, a).Detail.Questions[0]))
	a, _ = press(a, other)
	a = enter(typeDraft(a, "anchovies"))
	if out := cardView(topCard(t, a)); !strings.Contains(out, multiOn+"anchovies") {
		t.Errorf("the typed answer is not drawn ticked:\n%s", out)
	}

	a, _ = press(a, other)
	card := topCard(t, a)
	if card.typed(0) != "" {
		t.Errorf("choosing the filled Other row kept %q", card.typed(0))
	}
	if _, writing := a.cards.writingFor(card); writing {
		t.Error("choosing the filled Other row opened the composer instead of clearing it")
	}
}

// Nothing ticked is no answer: the strip says so, and Submit goes back to it.
func TestAMultiSelectQuestionWithNothingTickedIsUnanswered(t *testing.T) {
	a := paneAskingMany(t)
	q := topCard(t, a).Detail.Questions[0]
	if strip := topCard(t, a).stepStrip(wideRoom); !strings.Contains(ansi.Strip(strip), stripUnchecked) {
		t.Errorf("an untouched question reads answered: %q", ansi.Strip(strip))
	}

	a = enter(enter(walkTo(a, nextIndex(q))))
	if card := topCard(t, a); card.OnReview() || card.Cursor != 0 {
		t.Fatalf("Submit with nothing ticked did not go back to the question: cursor %d", card.Cursor)
	}
	a, _ = press(a, cardFirstOption)
	if strip := topCard(t, a).stepStrip(wideRoom); !strings.Contains(ansi.Strip(strip), stripChecked) {
		t.Errorf("a ticked question reads unanswered: %q", ansi.Strip(strip))
	}

	// Ticks alone are an answer: Submit sends them.
	a = enter(walkTo(a, nextIndex(q)))
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if got := sentFrame(t, a, cmd).Answers[q.Text]; got != q.Options[0].Label {
		t.Errorf("a question answered by one tick sent %q, want %q", got, q.Options[0].Label)
	}
}

// The row under a question that is not the last walks on to the next one.
func TestTheRowUnderAnEarlierQuestionSaysNext(t *testing.T) {
	card := Card{AgentID: "s1", RequestID: "r1", Ask: core.AskChoice, Detail: &core.AskDetail{Questions: []core.Question{
		{Text: "Which toppings?", Multi: true, Options: []core.Option{{Label: "Cheese"}, {Label: "Olives"}}},
		{Text: "Which size?", Options: []core.Option{{Label: "Small"}, {Label: "Large"}}},
	}}, Choice: []int{noChoice, noChoice}, Typed: []string{"", ""}}
	if out := cardView(card); !strings.Contains(out, multiNextLabel) {
		t.Errorf("the first of two questions does not offer %q:\n%s", multiNextLabel, out)
	}
	if out := cardView(card.toStep(1)); strings.Contains(out, multiOff) {
		t.Errorf("a single-select question drew boxes:\n%s", out)
	}
}

// Picking an option after typing an answer replaces the typed one: before, the
// typed words still travelled and both rows wore the chosen marker.
func TestPickingAnOptionReplacesATypedAnswer(t *testing.T) {
	a := paneAsking(t)
	q := topCard(t, a).Detail.Questions[0]
	a, _ = press(a, rune(cardFirstOption+otherIndex(q)))
	a = enter(typeDraft(a, "something else"))
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyLeft})
	a, _ = press(a, cardFirstOption)

	card := topCard(t, a)
	if card.typed(0) != "" || card.answers()[q.Text] != q.Options[0].Label {
		t.Errorf("after picking option 1 the answer is %q and the typed words %q", card.answers()[q.Text], card.typed(0))
	}
}
