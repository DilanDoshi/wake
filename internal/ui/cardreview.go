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
	"github.com/charmbracelet/x/ansi"
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

	// reviewAnswerRows bounds one answer's wrap. A multi-select answer is long
	// enough to need more than a row, and a typed one can be any length - a
	// card taller than its pane takes no keys, so Submit must stay on it.
	reviewAnswerRows = 3

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
		rows = append(rows, reviewAnswer(c.answerLabel(i), width))
	}
	rows = append(rows, "", mutedLine(reviewReady, width))
	for i, label := range [reviewOptions]string{reviewSubmitLabel, reviewCancelLabel} {
		numbered := fmt.Sprintf(reviewDigitFmt, i+1, label)
		rows = append(rows, optionRow(numbered, width, i == c.Option, false, AccentStyle))
	}
	return strings.Join(rows, "\n")
}

// reviewAnswer is one answer under its question, wrapped rather than cut to a
// row - the review is the last look at what goes out, and a multi-select answer
// is long - but to reviewAnswerRows at most, ending in an ellipsis.
func reviewAnswer(answer string, width int) string {
	lead := ansi.StringWidth(reviewAnswerLead)
	room := max(width-lead, 1)
	lines := strings.Split(HintStyle.Width(room).MaxWidth(room).Render(answer), "\n")
	if len(lines) > reviewAnswerRows {
		lines = lines[:reviewAnswerRows]
		last := ansi.Truncate(lines[reviewAnswerRows-1], max(room-ansi.StringWidth(ellipsis), 0), "")
		lines[reviewAnswerRows-1] = last + HintStyle.Render(ellipsis)
	}
	for i, ln := range lines {
		pad := strings.Repeat(" ", lead)
		if i == 0 {
			pad = reviewAnswerLead
		}
		lines[i] = HintStyle.Render(pad) + ln
	}
	return strings.Join(lines, "\n")
}

// answerLabel is what question i will send, or the word for a gap.
func (c Card) answerLabel(i int) string {
	answer, ok := c.answerOf(i)
	if !ok {
		return reviewUnanswered
	}
	return collapseWhitespaceOneLine(answer)
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
	// And the room records the close, so the ask's warn line there does not go
	// stale. Only reached for a question - OnReview above implies ShapeQuestion.
	a = a.recordQuestionResolved(c.AgentID, c.RequestID, true, c.resolvedAnswers())
	return a, a.write(answerFailed, c.Allow()), true
}
