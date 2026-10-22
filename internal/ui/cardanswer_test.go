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

	"github.com/DilanDoshi/wake/internal/core"
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

// --- ↑↓ while answering ---------------------------------------------------

// answeringOther is john's question with an earlier prompt in the conversation's
// history, the Other row picked and words typed into the box.
func answeringOther(t *testing.T, a App, words string) App {
	t.Helper()
	a = a.applyFrame(kindFrame("s1", core.KindUserText, "something typed earlier"))
	q, _ := topCard(t, a).question()
	a, _ = press(a, rune(cardFirstOption+otherIndex(q)))
	return typeDraft(a, words)
}

// ↑ out of the box goes back to the options, as it does in Claude Code's own
// question screen. It recalled the last prompt into the box instead, over the
// answer being written.
func TestUpWhileAnsweringMovesToTheOptionAboveAndKeepsTheWords(t *testing.T) {
	const mine = "half a thought"
	a := answeringOther(t, paneAsking(t), mine)
	other := otherIndex(topCard(t, a).Detail.Questions[0])

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	got := topCard(t, a)
	if v := a.composer().Value(); v != "" {
		t.Errorf("↑ while answering left %q in the box, want it back on the options with the box empty", v)
	}
	if _, writing := a.cards.writingFor(got); writing {
		t.Error("↑ left the composer in answer mode")
	}
	if got.Cursor != 0 || got.Option != other-1 {
		t.Errorf("↑ put the cursor on question %d option %d, want question 0 option %d", got.Cursor, got.Option, other-1)
	}
	if got.typed(0) != mine {
		t.Errorf("the words typed before ↑ became %q, want them kept as the Other answer %q", got.typed(0), mine)
	}
}

// Back on the Other row, the kept words are in the box to edit, and an emptied
// box takes them away.
func TestReturningToOtherPutsTheKeptWordsBackToEdit(t *testing.T) {
	const mine = "half a thought"
	a := answeringOther(t, paneAsking(t), mine)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	a = enter(a)

	if _, writing := a.cards.writingFor(topCard(t, a)); !writing {
		t.Fatal("↵ on the Other row did not open the box again")
	}
	if v := a.composer().Value(); v != mine {
		t.Fatalf("the box reopened holding %q, want the kept words %q", v, mine)
	}
	for range mine {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if got := topCard(t, a).typed(0); got != "" {
		t.Errorf("an emptied box left %q as the answer", got)
	}
}

// With nothing typed, ↑ is only a move: it records no answer and keeps the
// option already picked.
func TestUpFromAnEmptyBoxChangesNoAnswer(t *testing.T) {
	a := answeringOther(t, paneAsking(t), "")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	got := topCard(t, a)
	if _, writing := a.cards.writingFor(got); writing {
		t.Error("↑ from an empty box left the composer in answer mode")
	}
	if _, answered := got.answerOf(0); answered {
		t.Error("↑ from an empty box answered the question")
	}

	// Pick the first option, come back to the question, open Other and leave.
	a, _ = press(a, cardFirstOption)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyLeft})
	q := got.Detail.Questions[0]
	a, _ = press(a, rune(cardFirstOption+otherIndex(q)))
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if answer, _ := topCard(t, a).answerOf(0); answer != q.Options[0].Label {
		t.Errorf("↑ from an empty box changed the answer to %q, want the pick %q kept", answer, q.Options[0].Label)
	}
}

// The Other row is a single-select question's last, so ↓ there has nowhere to
// go: the key is taken and the answer stays being written.
func TestDownAtTheLastRowKeepsTheAnswerBeingWritten(t *testing.T) {
	const mine = "half a thought"
	a := answeringOther(t, paneAsking(t), mine)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if _, writing := a.cards.writingFor(topCard(t, a)); !writing {
		t.Error("↓ on the last row left answer mode with nowhere to move")
	}
	if v := a.composer().Value(); v != mine {
		t.Errorf("↓ on the last row changed the box to %q, want %q", v, mine)
	}
}

// Under a multi-select question's Other row is the row that walks on, and the
// words go with ↓ as a ticked answer.
func TestDownWhileAnsweringAManyQuestionReachesTheWalkOnRow(t *testing.T) {
	const mine = "a reason of my own"
	a := answeringOther(t, paneAskingMany(t), mine)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	got := topCard(t, a)
	q, _ := got.question()
	if got.Option != nextIndex(q) {
		t.Errorf("↓ put the cursor on option %d, want the walk-on row %d", got.Option, nextIndex(q))
	}
	if got.typed(0) != mine {
		t.Errorf("the words went with ↓ as %q, want %q ticked", got.typed(0), mine)
	}
	if v := a.composer().Value(); v != "" {
		t.Errorf("↓ left %q in the box", v)
	}
}

// A draft of more than one line keeps its line keys: ↑ climbs the draft until
// the cursor is on its top row.
func TestUpInsideAMultiLineAnswerMovesTheTextCursor(t *testing.T) {
	a := answeringOther(t, paneAsking(t), "line one")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlJ})
	a = typeDraft(a, "line two")

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if _, writing := a.cards.writingFor(topCard(t, a)); !writing {
		t.Fatal("↑ on the second line of an answer left answer mode")
	}
	if v := a.composer().Value(); v != "line one\nline two" {
		t.Errorf("↑ on the second line changed the box to %q", v)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if _, writing := a.cards.writingFor(topCard(t, a)); writing {
		t.Error("↑ on the answer's top line stayed in the box, want it back on the options")
	}
}

// The same box holding a multi-select answer keeps ↓ for its own lines, which
// is where ↓ has a row to reach on the card as well.
func TestDownInsideAMultiLineAnswerMovesTheTextCursor(t *testing.T) {
	a := answeringOther(t, paneAskingMany(t), "line one")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlJ})
	a = typeDraft(a, "line two")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyDown})
	if _, writing := a.cards.writingFor(topCard(t, a)); !writing {
		t.Error("↓ on the first line of an answer left answer mode")
	}
}

// A menu over the draft walks with the arrows, as it does in any box.
func TestAMenuOverTheAnswerKeepsTheArrows(t *testing.T) {
	a := answeringOther(t, paneAsking(t), "/")
	if !a.completionUp() {
		t.Fatal("no menu came up over a / in the box, so this test cannot see the rule")
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyUp})
	if _, writing := a.cards.writingFor(topCard(t, a)); !writing {
		t.Error("↑ over a menu left answer mode instead of walking the menu")
	}
}

// ⌥↑ is the prompt history's on every surface, and the card never claims it.
func TestAltUpWhileAnsweringIsStillThePromptHistory(t *testing.T) {
	a := answeringOther(t, paneAsking(t), "half a thought")
	a, _ = pressKey(a, alt(tea.KeyUp))
	if v := a.composer().Value(); v != "something typed earlier" {
		t.Errorf("⌥↑ while answering left %q in the box, want the earlier prompt", v)
	}
}

// The refusal's box is not an answer to one question, so an arrow neither
// leaves it nor puts an earlier prompt where the reason was being written.
func TestAnArrowInTheRefusalsBoxKeepsTheReason(t *testing.T) {
	a := paneAsking(t)
	a = a.applyFrame(kindFrame("s1", core.KindUserText, "something typed earlier"))
	a, _ = press(a, cardDenyKey)
	a = typeDraft(a, "not now")
	for _, k := range []tea.KeyType{tea.KeyDown, tea.KeyUp} {
		a, _ = pressKey(a, tea.KeyMsg{Type: k})
		if q, writing := a.cards.writingFor(topCard(t, a)); !writing || q != denyReasonStep {
			t.Errorf("%v while writing a refusal's reason left the refusal", k)
		}
		if v := a.composer().Value(); v != "not now" {
			t.Errorf("%v while writing a refusal's reason changed the box to %q", k, v)
		}
	}
}

// Answer mode is read before the card has to fit, since the box growing with
// the answer is what pushes the card off: ↑ there still leaves the box rather
// than walking the prompt history over the answer.
func TestUpWhileAnsweringInAPaneTooShortForTheCard(t *testing.T) {
	a := answeringOther(t, paneAsking(t), "half a thought")
	short := a.withSize(220, 12).applyGeometry()
	if short.cardFullyDrawn() {
		t.Fatal("the card still fits at this height, so this test cannot see the gate")
	}
	short, _ = pressKey(short, tea.KeyMsg{Type: tea.KeyUp})
	if got := topCard(t, short).typed(0); got != "half a thought" {
		t.Errorf("↑ in a short pane left %q as the answer and %q in the box", got, short.composer().Value())
	}
}
