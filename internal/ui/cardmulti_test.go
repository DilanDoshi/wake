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

// A reattach replays every pending ask. The replay names the same ask, so the
// ticks made before it survive rather than being reset under the operator.
func TestAReplayedAskKeepsWhatWasTicked(t *testing.T) {
	ev := recordedAsks(t, multiFixture)[0]
	cs := Cards{}.Add("s1", ev)
	card, _ := cs.Top()
	cs = cs.With(card.Toggle(0, 1).Move(0, 2))

	card, _ = cs.Add("s1", ev).Top()
	if !card.ticked(0, 1) || card.Option != 2 {
		t.Errorf("the replayed ask reset the card: ticks %v, option %d", card.Ticks, card.Option)
	}

	// A card known only from a report still takes the replay's payload.
	bare := Cards{}.Reconcile(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", RequestIDs: []string{ev.RequestID}, State: rpc.StateBlocked}}})
	if card, _ := bare.Add("s1", ev).Top(); card.Detail == nil {
		t.Error("a report-only card was not filled in by the ask's own event")
	}
}

// The review is the last look at what goes out, so a joined answer too long for
// the pane wraps rather than being cut.
func TestTheReviewWrapsALongJoinedAnswer(t *testing.T) {
	card := cardFor(t, recordedAsks(t, multiFixture)[0])
	for o := range card.Detail.Questions[0].Options {
		card = card.Toggle(0, o)
	}
	card = card.toStep(1)
	want, _ := card.answerOf(0)
	out := strings.Join(strings.Fields(ansi.Strip(card.reviewBody(30))), " ")
	if !strings.Contains(out, strings.Join(strings.Fields(want), " ")) {
		t.Errorf("the review at 30 columns does not show the whole answer %q:\n%s", want, ansi.Strip(card.reviewBody(30)))
	}
}

// Space is Claude Code's tick key on a multi-select question only. Everywhere
// else it is a character, so it reaches the composer and leaves the card alone.
func TestSpaceIsACharacterAnywhereButAMultiSelectQuestion(t *testing.T) {
	spaceOn := func(t *testing.T, a App) {
		t.Helper()
		before := topCard(t, a)
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
		if got := a.composer().Value(); got != " " {
			t.Errorf("space did not reach the composer: %q", got)
		}
		if after := topCard(t, a); after.Cursor != before.Cursor || after.Option != before.Option || after.chosen(0) != before.chosen(0) {
			t.Errorf("space moved or answered the card")
		}
	}
	t.Run("single-select", func(t *testing.T) { spaceOn(t, paneAsking(t)) })
	t.Run("review", func(t *testing.T) {
		a := paneAskingMany(t)
		a, _ = press(a, cardFirstOption)
		spaceOn(t, enter(walkTo(a, nextIndex(topCard(t, a).Detail.Questions[0]))))
	})
	t.Run("permission", func(t *testing.T) { spaceOn(t, blockedPane(t)) })
}

// The Next row under an earlier question walks on to the next question.
func TestTheNextRowWalksToTheNextQuestion(t *testing.T) {
	a := paneAsking(t)
	card := Card{AgentID: "s1", RequestID: "r9", Ask: core.AskChoice, Detail: &core.AskDetail{Questions: []core.Question{
		{Text: "Which toppings?", Multi: true, Options: []core.Option{{Label: "Cheese"}, {Label: "Olives"}}},
		{Text: "Which size?", Options: []core.Option{{Label: "Small"}, {Label: "Large"}}},
	}}, Choice: []int{noChoice, noChoice}, Typed: []string{"", ""}, Ticks: make([][]int, 2)}
	a.cards = Cards{}.with(card)
	if c, ok := a.cardOf(a.focus); !ok || c.RequestID != "r9" {
		t.Fatal("the hand-built card is not the focused pane's")
	}
	a = enter(walkTo(a, nextIndex(card.Detail.Questions[0])))
	if got := topCard(t, a); got.Cursor != 1 || got.OnReview() {
		t.Errorf("↵ on Next left the cursor on step %d, want the second question", got.Cursor)
	}
}

// Words typed in the Other row land the cursor on the row that walks on, so a
// reflexive second ↵ moves on rather than clearing what was just typed.
func TestTypingInOtherLandsOnTheRowThatWalksOn(t *testing.T) {
	a := paneAskingMany(t)
	q := topCard(t, a).Detail.Questions[0]
	a, _ = press(a, rune(cardFirstOption+otherIndex(q)))
	a = enter(typeDraft(a, "anchovies"))
	if got := topCard(t, a); got.Option != nextIndex(q) || got.typed(0) != "anchovies" {
		t.Fatalf("after typing, the cursor is on row %d with %q, want the Next row %d", got.Option, got.typed(0), nextIndex(q))
	}
	if got := topCard(t, enter(a)); !got.OnReview() || got.typed(0) != "anchovies" {
		t.Errorf("a second ↵ did not walk on with the typed words kept: review %v, typed %q", got.OnReview(), got.typed(0))
	}
}

// The wrap is bounded: an answer taller than the pane would push Submit off the
// card, and a card not drawn in full takes no keys. A long typed answer still
// reaches the review and submits whole.
func TestAnAnswerTallerThanThePaneStillSubmits(t *testing.T) {
	a := paneAskingMany(t)
	long := strings.Repeat("a long answer of the operator's own ", 120)
	card := topCard(t, a)
	a.cards = a.cards.With(card.answer(0, long).atOption(nextIndex(card.Detail.Questions[0])))
	a = enter(a)
	if !topCard(t, a).OnReview() || !a.cardFullyDrawn() {
		t.Fatalf("the review is not drawn in full: review %v, drawn %v", topCard(t, a).OnReview(), a.cardFullyDrawn())
	}
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if got := sentFrame(t, a, cmd).Answers[card.Detail.Questions[0].Text]; got != strings.TrimSpace(long) {
		t.Errorf("sent %d bytes, want the whole %d-byte answer", len(got), len(strings.TrimSpace(long)))
	}
}
