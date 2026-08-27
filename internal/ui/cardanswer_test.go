package ui

// Answering in your own words.
//
// Every recorded question is a closed list, and the operator's answer is not
// always on it - AskUserQuestion's own contract says a custom answer is always
// available, and the deny reason is the one channel for saying what to do
// instead of retrying the identical call. Neither had a way in: card keys are
// read only from an empty composer, so the draft could not be the answer.
//
// What opens it is that answer mode is *entered deliberately* - by picking the
// Other row, or by pressing the refusal key - so ↵ on a non-empty draft means
// something other than "send a message" only in a state the operator chose.
// Outside it the composer gate is exactly what it was.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// typeDraft puts text in the focused composer one rune at a time, the way a
// keyboard does.
func typeDraft(a App, text string) App {
	for _, r := range text {
		a, _ = press(a, r)
	}
	return a
}

func TestEveryQuestionOffersAnAnswerOfYourOwn(t *testing.T) {
	card := cardFor(t, recordedAsks(t, choiceFixture)[0])
	out := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))
	if !strings.Contains(out, otherLabel) {
		t.Errorf("a question offers only the options the model supplied, so an operator whose answer is not on the list has nowhere to put it:\n%s", out)
	}
	// And it is reachable by digit, like every other row.
	q := card.Detail.Questions[0]
	if !strings.Contains(out, chooseKeys(len(q.Options)+1)) {
		t.Errorf("the digits do not reach the %q row:\n%s", otherLabel, out)
	}
}

func TestPickingOtherOpensTheComposerForThatQuestion(t *testing.T) {
	a := paneAsking(t)
	card := topCard(t, a)
	other := otherIndex(card.Detail.Questions[0])

	a, _ = press(a, rune(cardFirstOption+other))
	if _, ok := a.cards.writingFor(topCard(t, a)); !ok {
		t.Fatalf("picking %q did not open the composer for an answer", otherLabel)
	}
	if got := a.answerTitle(a.focus); got == "" {
		t.Error("the composer is not titled while an answer is being written, so nothing says the draft is an answer rather than a message")
	}
}

func TestATypedAnswerIsRecordedAndTravelsAsItsOwnLabel(t *testing.T) {
	const mine = "file a DEV ticket but gate it behind CON-5"

	a := paneAsking(t)
	card := topCard(t, a)
	other := otherIndex(card.Detail.Questions[0])
	a, _ = press(a, rune(cardFirstOption+other))
	a = typeDraft(a, mine)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	got := topCard(t, a)
	if got.typed(0) != mine {
		t.Fatalf("the typed answer was recorded as %q, want %q", got.typed(0), mine)
	}
	if !a.composerEmpty() {
		t.Errorf("the draft survived being sent as an answer: %q", a.composer().Value())
	}
	if _, ok := a.cards.writingFor(got); ok {
		t.Error("the composer is still in answer mode after the answer was taken")
	}
	// And it counts as an answer, so the review can be reached.
	if got.chosen(0) != noChoice {
		t.Errorf("a typed answer also picked option %d, so two answers exist for one question", got.chosen(0))
	}

	// Answer the rest by picking, then submit and read the wire.
	for !topCard(t, a).OnReview() {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	}
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, a, cmd)
	if f.Answers[card.Detail.Questions[0].Text] != mine {
		t.Errorf("the answer frame carries %q for the first question, want the operator's own words %q", f.Answers[card.Detail.Questions[0].Text], mine)
	}
}

// esc is the way out, and it may not also stop the turn: the operator is
// abandoning a sentence, not the agent's work.
func TestEscapeLeavesAnswerModeWithoutInterrupting(t *testing.T) {
	a := paneAsking(t)
	card := topCard(t, a)
	a, _ = press(a, rune(cardFirstOption+otherIndex(card.Detail.Questions[0])))
	a = typeDraft(a, "half a thought")

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		for _, f := range sentFrames(t, a, cmd) {
			if f.Kind == rpc.FrameInterrupt {
				t.Error("esc out of answer mode interrupted the agent's turn as well as dropping the draft")
			}
		}
	}
	if _, ok := a.cards.writingFor(topCard(t, a)); ok {
		t.Error("esc left the composer in answer mode")
	}
	if !a.composerEmpty() {
		t.Errorf("esc left the abandoned answer in the draft: %q", a.composer().Value())
	}
}

// --- the deny reason ------------------------------------------------------

func TestDenyCarriesTheOperatorsOwnWords(t *testing.T) {
	const because = "the loader ticket is already filed as DEV-3040"

	a := paneAsking(t)
	a, _ = press(a, cardDenyKey)
	if _, ok := a.cards.writingFor(topCard(t, a)); !ok {
		t.Fatal("the refusal key did not open a box for a reason")
	}
	a = typeDraft(a, because)
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	f := sentFrame(t, a, cmd)
	if f.Kind != rpc.FrameDeny {
		t.Fatalf("the refusal wrote a %q frame, want %q", f.Kind, rpc.FrameDeny)
	}
	if f.Reason != because {
		t.Errorf("the refusal carried %q, want the operator's own words %q - the reason reaches the model verbatim and is the one channel for saying what to do instead", f.Reason, because)
	}
}

// A blank refusal reads as a tool that failed for no reason, which is why the
// constant exists. Typing nothing keeps it.
func TestARefusalWithNothingTypedStillSaysSomething(t *testing.T) {
	a := paneAsking(t)
	a, _ = press(a, cardDenyKey)
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	f := sentFrame(t, a, cmd)
	if f.Reason != cardDenyReason {
		t.Errorf("an empty refusal carried %q, want the standing reason %q", f.Reason, cardDenyReason)
	}
}

// --- what review found ----------------------------------------------------

// Answer mode outlives the ask it was opened for unless something ends it, and
// Cards.Settle is where that has to happen - the same place, and for the same
// reason, that it takes back an arm. Reconcile puts an ask back whenever the
// next report still names it, which it does until the daemon has acted on the
// frame just written, so a mode left standing would let the next ↵ settle the
// returning card a second time.
func TestSettlingAnAskEndsTheAnswerBeingWrittenForIt(t *testing.T) {
	a := paneAsking(t)
	card := topCard(t, a)
	a, _ = press(a, cardDenyKey)
	if _, ok := a.cards.writingFor(card); !ok {
		t.Fatal("the refusal opened no box, so there is no mode for a settle to end")
	}

	settled := a.cards.Settle(card.AgentID, card.RequestID)
	if _, ok := settled.writingFor(card); ok {
		t.Error("the ask was settled and the composer is still writing an answer for it: a report that puts the ask back would arm a second settle nobody asked for")
	}
}

// A settle for a *different* ask leaves it alone, which is armedSettle's own
// rule: at thirty agents another one settling is most of what arrives, and a
// mode dropped by one would be an answer the operator was mid-way through.
func TestSettlingAnotherAskLeavesTheAnswerAlone(t *testing.T) {
	a := paneAsking(t)
	card := topCard(t, a)
	a, _ = press(a, cardDenyKey)

	settled := a.cards.Settle("someone-else", "another-request")
	if _, ok := settled.writingFor(card); !ok {
		t.Error("another agent's ask being settled dropped the answer this one was writing")
	}
}

// The composer is what accounts for answer mode on screen - it is titled for
// what it holds, and it is always drawn. So ↵ may commit even when the card
// itself has been pushed off, which is the state a long answer *produces*: the
// box grows with the draft and takes the room the card was using.
//
// Without this, typing past the pane's height silently turned "send answer"
// back into "send a message to the agent", with nothing saying so.
func TestATypedAnswerIsTakenEvenWhenTheCardNoLongerFits(t *testing.T) {
	a := paneAsking(t)
	card := topCard(t, a)
	a, _ = press(a, rune(cardFirstOption+otherIndex(card.Detail.Questions[0])))
	a = typeDraft(a, "a reason of my own")

	// Shrink the pane until the card cannot be drawn in full, which is exactly
	// what the gate below cardKey's mode check is measuring.
	short := a.withSize(220, 12).applyGeometry()
	if short.cardFullyDrawn() {
		t.Skip("the card still fits at this height, so the gate is not being exercised")
	}
	if _, ok := short.cards.writingFor(card); !ok {
		t.Fatal("the resize dropped answer mode, so this test cannot see the gate")
	}

	after, _ := pressKey(short, tea.KeyMsg{Type: tea.KeyEnter})
	if got := topCard(t, after).typed(0); got == "" {
		t.Error("↵ did not take the typed answer once the card stopped fitting - it went to the composer as an ordinary message instead, with nothing on screen saying the answer was lost")
	}
}
