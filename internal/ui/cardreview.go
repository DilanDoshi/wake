package ui

// The review step: every answer laid out before the one press that sends it.
//
// It is drawn through the same optionRow the questions use, so ↑↓, ↵ and the
// digits reach Submit and Cancel with no binding of their own - see
// cardsteps.go on why that matters to the legend bijection.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	// The two options, and their order. Submit first because it is what the
	// operator walked here to do; Cancel is the way back, not the safe
	// default - there is no unsafe press on this step, only an unfinished one.
	reviewSubmit  = 0
	reviewCancel  = 1
	reviewOptions = 2

	reviewSubmitLabel = "Submit answers"
	reviewCancelLabel = "Cancel"

	// The two headings. The first says what the rows are; the second asks the
	// question the two options answer.
	reviewTitle = "Review your answers"
	reviewReady = "Ready to submit your answers?"

	// reviewAnswerLead marks the answer under the question it belongs to.
	reviewAnswerLead = "  → "

	// reviewQuestionLead marks a question in the list. The same bullet a
	// finished thing wears elsewhere in this build.
	reviewQuestionLead = "● "

	// reviewUnanswered stands where an answer would be. A gap the operator can
	// see is what makes Submit's refusal legible before it happens.
	reviewUnanswered = "not answered"

	// reviewDigitFmt numbers the two options, because the digits pick them.
	reviewDigitFmt = "%d. %s"
)

// reviewKeys is the key line for this step. No [a]nswer: the whole point of
// the review is that ↵ on Submit is the press, so a second way to send it
// would be a second account of one irreversible thing.
const reviewKeys = cardMoveKeys + cardKeySep + "[1-2] choose" + cardKeySep + cardDenyLabel

// reviewBody is the review step's rows: the answers, then the two options.
func (c Card) reviewBody(width int) string {
	rows := []string{stepHeader(c, width), "", AccentStyle.MaxWidth(width).Render(reviewTitle), ""}
	for i, q := range c.Detail.Questions {
		rows = append(rows, TextStyle.Width(width).MaxWidth(width).Render(reviewQuestionLead+collapseWhitespaceOneLine(q.Text)))
		rows = append(rows, mutedLine(reviewAnswerLead+c.answerLabel(i), width))
	}
	rows = append(rows, "", mutedLine(reviewReady, width))
	for i, label := range [reviewOptions]string{reviewSubmitLabel, reviewCancelLabel} {
		numbered := fmt.Sprintf(reviewDigitFmt, i+1, label)
		rows = append(rows, optionRow(numbered, width, i == c.Option, false, AccentStyle))
	}
	return strings.Join(rows, "\n")
}

// answerLabel is what question i will send, or the word for a gap.
//
// It reads the typed answer first, for the reason Card.answers does: an
// operator's own words are the answer when they wrote one, and the option
// label only when they picked one.
func (c Card) answerLabel(i int) string {
	if typed := c.typed(i); typed != "" {
		return collapseWhitespaceOneLine(typed)
	}
	o := c.chosen(i)
	if o < 0 || o >= len(c.Detail.Questions[i].Options) {
		return reviewUnanswered
	}
	return collapseWhitespaceOneLine(c.Detail.Questions[i].Options[o].Label)
}

// reviewChoose settles the ask, or does the one thing that is not settling it.
//
// Three arms, and the middle one is why Submit is not simply gated. A review
// reached with a question unanswered cannot send - the encoder refuses a short
// answer with nothing written - so the press takes the operator to the gap
// instead. A refusal they have to read is worse than being shown what is
// missing, and a dead key is worse than both.
func (a App) reviewChoose(c Card) (tea.Model, tea.Cmd, bool) {
	if !c.OnReview() {
		return a, nil, false
	}
	if c.Option == reviewCancel {
		// Not a deny. Nothing is written and the ask stays up, because all the
		// operator said is that they were not finished.
		a.cards = a.cards.With(c.toStep(len(c.Detail.Questions) - 1)).disarm()
		return a, nil, true
	}
	if q, ok := c.firstUnanswered(); ok {
		a.cards = a.cards.With(c.toStep(q)).disarm()
		return a, nil, true
	}
	// The card comes down here rather than on an acknowledgement, because an
	// allow produces no frame of its own. Cards.Reconcile puts back any ask the
	// next report still names, which is what makes that safe.
	a.cards = a.cards.Settle(c.AgentID, c.RequestID)
	return a, a.write(answerFailed, c.Allow()), true
}
