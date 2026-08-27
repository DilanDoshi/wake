package ui

// The review step: what a question ask shows before it is sent, and the two
// options that end it.
//
// This is what replaced arm-and-confirm on ShapeQuestion. The arm named a verb
// and nothing else; the review names every answer that is about to travel. It
// is a stronger guard than the one it replaces, which is the only reason the
// question shape is allowed to drop the two-press settle that ShapePermission
// and ShapePlan keep.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// answerEvery walks the recorded ask, choosing the first option of each
// question, and leaves the cursor wherever that put it.
func answerEvery(a App) App {
	for {
		card, ok := a.cardOf(a.focus)
		if !ok || card.OnReview() {
			return a
		}
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	}
}

// Answering the last question has to arrive somewhere. It used to arrive
// nowhere: Move clamped at the last question, so ↵ re-picked the same option
// forever and the only way to send was a key the card had started advertising
// only once every question was answered.
func TestAnsweringTheLastQuestionLandsOnTheReview(t *testing.T) {
	a := paneAsking(t)
	card := topCard(t, a)
	n := len(card.Detail.Questions)
	if n == 0 {
		t.Fatal("the recorded ask puts no questions")
	}

	a = answerEvery(a)
	got := topCard(t, a)
	if !got.OnReview() {
		t.Fatalf("after answering all %d questions the cursor is on question %d, not the review - there is no way to submit", n, got.Cursor)
	}
}

// The whole point of the step: you see what will be sent before you send it.
func TestTheReviewNamesEveryQuestionAndTheAnswerItWillSend(t *testing.T) {
	a := answerEvery(paneAsking(t))
	card := topCard(t, a)
	out := ansi.Strip(oneCard(card).topView(wideRoom, Agent{Name: "sydney"}))

	for i, q := range card.Detail.Questions {
		if !strings.Contains(out, firstWords(q.Text)) {
			t.Errorf("the review omits question %d, so its answer is being sent unseen:\n%s", i+1, out)
		}
		chosen := card.chosen(i)
		if chosen < 0 {
			t.Fatalf("question %d was left unanswered by answerEvery", i+1)
		}
		if label := q.Options[chosen].Label; !strings.Contains(out, firstWords(label)) {
			t.Errorf("the review omits the answer %q to question %d:\n%s", label, i+1, out)
		}
	}
	for _, want := range []string{reviewSubmitLabel, reviewCancelLabel} {
		if !strings.Contains(out, want) {
			t.Errorf("the review offers no %q, so it cannot be ended:\n%s", want, out)
		}
	}
}

// Submit is the irreversible press, and it carries the answers.
func TestSubmitOnTheReviewSendsEveryAnswer(t *testing.T) {
	a := answerEvery(paneAsking(t))
	card := topCard(t, a)
	if card.Option != reviewSubmit {
		t.Fatalf("the review opened with the cursor on option %d, want Submit at %d", card.Option, reviewSubmit)
	}

	a2, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, a2, cmd)
	if f.Kind != rpc.FrameAnswer {
		t.Fatalf("Submit wrote a %v frame, want %v - a question settled by a bare allow tells the model nobody replied", f.Kind, rpc.FrameAnswer)
	}
	if len(f.Answers) != len(card.Detail.Questions) {
		t.Errorf("Submit carried %d answers for %d questions: %+v", len(f.Answers), len(card.Detail.Questions), f.Answers)
	}
	for i, q := range card.Detail.Questions {
		want := q.Options[card.chosen(i)].Label
		if got := f.Answers[q.Text]; got != want {
			t.Errorf("question %q was answered %q, want %q", firstWords(q.Text), got, want)
		}
	}
}

// Cancel is not a deny. Nothing is written and the ask stays up, because the
// only thing the operator said was that they were not finished.
func TestCancelOnTheReviewWritesNothingAndGoesBack(t *testing.T) {
	a := answerEvery(paneAsking(t))
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if got := topCard(t, a).Option; got != reviewCancel {
		t.Fatalf("↓ on the review left the cursor on option %d, want Cancel at %d", got, reviewCancel)
	}

	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("Cancel wrote a frame: %+v", sentFrames(t, a, cmd))
	}
	card := topCard(t, a)
	if card.OnReview() {
		t.Error("Cancel left the cursor on the review, so it did nothing at all")
	}
	if want := len(card.Detail.Questions) - 1; card.Cursor != want {
		t.Errorf("Cancel returned to question %d, want the last one at %d", card.Cursor, want)
	}
}

// A review reached with a question still unanswered may not send - the daemon
// refuses a short answer beneath us, and a refusal the operator has to read is
// worse than being taken to the question that is missing.
func TestSubmitWithAQuestionUnansweredGoesToThatQuestion(t *testing.T) {
	a := paneAsking(t)
	card := topCard(t, a)
	if len(card.Detail.Questions) < 2 {
		t.Skip("the recorded ask puts one question, so none can be left behind")
	}
	// Answer only the second, then walk to the review.
	card = card.Move(1, 0)
	card = card.Pick(card.Cursor, 0).Move(1, 0)
	a.cards = a.cards.With(card)
	if !topCard(t, a).OnReview() {
		t.Fatal("the cursor is not on the review, so this test cannot press Submit")
	}

	a2, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Errorf("Submit sent a short answer: %+v", sentFrames(t, a2, cmd))
	}
	if got := topCard(t, a2); got.Cursor != 0 {
		t.Errorf("Submit with question 1 unanswered left the cursor on step %d, want it taken to question 1", got.Cursor)
	}
}
