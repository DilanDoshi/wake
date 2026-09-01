package ui

// Answering in your own words: the Other row, the refusal's reason, and the
// one state in which ↵ on a draft is not a message.
//
// # Why this needed designing rather than adding
//
// cardkeys.go reads `a`, `d` and the digits only from an empty composer,
// because they are letters people type and the first character of every draft
// is typed into an empty composer. That gate is also why the draft could not
// simply *be* the answer: a build where ↵ on a non-empty draft settles a card
// is one where the reflex that sends a message grants a tool call.
//
// What makes it safe is that answer mode is entered deliberately - by picking
// the Other row, or by pressing the refusal key - and says so on the card while
// it lasts. Outside it nothing about the composer changed. Inside it, ↵ takes
// the draft as an answer and ⎋ abandons it, and neither reaches the agent's
// turn.
//
// # Why the refusal opens the same box
//
// The reason reaches the model verbatim as the tool result and is the one
// channel for saying what to do instead of retrying the identical call; a
// constant is a weaker answer than the operator's own words. The two-press
// property is kept exactly: `d` opens the box - which is the arm - and ↵
// sends. An empty box still sends the standing reason, because a blank refusal
// reads as a tool that failed for no reason.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// denyReasonStep marks a draft being written as a refusal's reason rather
	// than as an answer to one question. Negative because it is not an index
	// into anything - the refusal is about the whole ask.
	denyReasonStep = -1

	// otherLabel is the row past the options the model supplied. It is Wake's
	// own word, not a wire one: the answer travels as a *label*, and this row
	// is how the operator says the label is theirs.
	otherLabel = "Other…"

	// The composer's title while it holds something other than a message, so
	// the box says what ↵ will do with it.
	answerTitleFmt = "answering: %s"
	denyTitle      = "deny reason"

	// The key lines for the two modes. Both name the way out, because a mode
	// somebody entered by accident with no visible exit is worse than no mode.
	answerModeKeys = cardConfirmGlyph + " send answer" + cardDot + escGlyph + " back to the options"
	denyModeKeys   = cardConfirmGlyph + " deny" + cardDot + cardArmedFinal + cardDot + escGlyph + " cancels"
)

// writingAnswer names one ask and what is being written for it.
//
// Both halves of the ask, for armedSettle's reason: the drawn card changes on
// its own as reports arrive, and a mode that named only "a question index"
// would follow whichever card came forward. The zero value is not writing,
// because Cards never holds a card with an empty RequestID.
type writingAnswer struct {
	agentID   string
	requestID string
	question  int
}

// startWriting puts the composer into answer mode for one question, or for the
// refusal when question is denyReasonStep.
func (cs Cards) startWriting(c Card, question int) Cards {
	cs.writing = writingAnswer{agentID: c.AgentID, requestID: c.RequestID, question: question}
	return cs
}

// stopWriting takes the mode back. Every path out goes through here - the
// answer being taken, ⎋, and the card being settled underneath it.
func (cs Cards) stopWriting() Cards {
	cs.writing = writingAnswer{}
	return cs
}

// writingFor is the question a draft is being written for on this card, and
// whether the composer is in answer mode for it at all.
func (cs Cards) writingFor(c Card) (int, bool) {
	if cs.writing.requestID == "" {
		return 0, false
	}
	if cs.writing.agentID != c.AgentID || cs.writing.requestID != c.RequestID {
		return 0, false
	}
	return cs.writing.question, true
}

// otherIndex is the row past the options the model supplied.
func otherIndex(q core.Question) int { return len(q.Options) }

// answerTitle is what the composer of this pane is titled while it holds an
// answer, and "" when it holds an ordinary draft.
//
// Read at draw time from the card the pane is putting, so a mode that ended
// because the ask was settled underneath it cannot leave a stale title behind.
func (a App) answerTitle(id string) string {
	card, ok := a.cardOf(id)
	if !ok {
		return ""
	}
	q, writing := a.cards.writingFor(card)
	if !writing {
		return ""
	}
	if q == denyReasonStep {
		return denyTitle
	}
	if q < 0 || q >= len(card.Detail.Questions) {
		return ""
	}
	return fmt.Sprintf(answerTitleFmt, stripTabName(card.Detail.Questions[q].Header, q))
}

// beginAnswer opens the composer for a question the operator wants to answer
// themselves, and puts the option cursor on the row that says so.
func (a App) beginAnswer(c Card, question int) (tea.Model, tea.Cmd, bool) {
	a.cards = a.cards.With(c.atOption(otherIndex(c.Detail.Questions[c.Cursor]))).startWriting(c, question).disarm()
	return a, nil, true
}

// beginDeny opens the composer for a refusal's reason. It is the arm: nothing
// is written until ↵, and ⎋ takes it back.
func (a App) beginDeny(c Card) (tea.Model, tea.Cmd, bool) {
	a.cards = a.cards.startWriting(c, denyReasonStep).disarm()
	return a, nil, true
}

// commitAnswer takes what was typed - as one question's answer, or as the
// refusal's reason - and leaves answer mode either way.
func (a App) commitAnswer(c Card, question int) (tea.Model, tea.Cmd, bool) {
	text := strings.TrimSpace(a.composer().Value())
	if question == denyReasonStep {
		// An empty box keeps the standing reason: a blank refusal reads as a
		// tool that failed for no reason.
		reason := cardDenyReason
		if text != "" {
			reason = text
		}
		a.cards = a.cards.Settle(c.AgentID, c.RequestID).stopWriting()
		// A refused question leaves a room record too, the counterpart of the
		// answered one - questions only, since a permission or a plan deny is a
		// verb the room does not announce specially (see cardroom.go).
		if c.Shape() == ShapeQuestion {
			a = a.recordQuestionResolved(c.AgentID, false)
		}
		return a.clearDraft(), a.write(answerFailed, c.Deny(reason)), true
	}
	if text == "" {
		// Nothing typed is not an answer. The mode stays, because the operator
		// is still in the middle of writing one - ⎋ is how they say otherwise.
		return a, nil, true
	}
	a.cards = a.cards.With(c.answer(question, text).Move(1, 0)).stopWriting().disarm()
	return a.clearDraft(), nil, true
}

// cancelAnswer abandons what was being written, without touching the agent.
func (a App) cancelAnswer() App {
	a.cards = a.cards.stopWriting()
	return a.clearDraft()
}

// answer records the operator's own words for one question, clearing any
// option that had been picked for it - two answers to one question is the one
// state answers() could not resolve honestly.
func (c Card) answer(question int, text string) Card {
	if c.Detail == nil || question < 0 || question >= len(c.Detail.Questions) {
		return c
	}
	typed := append([]string(nil), c.Typed...)
	for len(typed) < len(c.Detail.Questions) {
		typed = append(typed, "")
	}
	typed[question] = text
	c.Typed = typed

	choice := append([]int(nil), c.Choice...)
	if question < len(choice) {
		choice[question] = noChoice
	}
	c.Choice = choice
	return c
}
