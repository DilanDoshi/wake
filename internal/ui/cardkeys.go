package ui

// The keys an answerable card advertises, and the only place they are read.
//
// # Why these are runes and not chords
//
// Because they exist only while a card is up. A legend under the composer names
// what this build always does; an affordance that appears and disappears with a
// blocked agent belongs on the thing that appeared - which is where
// cards_blocks.go draws it. That is also why the legend guard cannot see these
// and does not need to: the card is its own legend, and it is rendered from the
// same Card the keys act on.
//
// # Why the composer has to be empty, and why that is not the defence
//
// `a` and `d` are letters people type. A permission grant that can happen while
// somebody writes the word "analyse" is the unsafe direction of exactly the
// failure the hint line's rule exists for, so cardKey reads a rune only when
// there is no draft for it to be a character of.
//
// **That paragraph ended there for four days and was wrong to.** The gate
// defends the second character of a draft onward and leaves the **first**,
// which is the whole exposure: the first character of every draft is typed into
// an empty composer. `a` and `d` begin a great deal of what somebody types at a
// room full of agents, and one keystroke was a tool call granted or a refusal
// the model was told about, with nothing anywhere reporting it.
//
// What actually closes it is the arm-and-confirm in armCard and confirmCard,
// and the two gates in cardKey. The header said otherwise while naming App.key,
// which is the function the gate was moved *out of* - a sentence about the rest
// of the build, in the file CLAUDE.md's key-locations table tells a reader to
// start at.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// cardAllowKey settles the top card affirmatively - allow, answer or
	// approve, whichever the shape makes it - and cardDenyKey refuses it.
	cardAllowKey = 'a'
	cardDenyKey  = 'd'

	// cardDenyReason is what the model is told. It reaches the model verbatim
	// as the tool result, and it is deliberately not an empty string: "the
	// operator refused this" is a fact the model can act on, and a blank
	// refusal reads as a tool that failed for no reason. There is no way to
	// type a longer one yet - the composer has to be empty for this key to be
	// read at all - which is recorded in docs/notes/deferred.md.
	cardDenyReason = "the operator denied this from the room"

	// cardFirstOption is the digit that picks the first option of a question.
	// Digits rather than arrows, so a question is answerable without spending a
	// legend entry on a key that only means anything while a card is up.
	cardFirstOption = '1'

	// cardMaxOptions is how many options a digit can reach. Nine, because that
	// is how many single digits there are; a question with more would have to
	// be answered in a DM, and no recorded ask has come close.
	cardMaxOptions = 9
)

// cardKey reads one key against the top card, reporting whether it took it.
//
// # Two gates, and neither is sufficient alone
//
// The composer gate is here rather than at the call site because the argument
// for it is this file's: `a` and `d` are letters people type, and a permission
// grant that can happen while somebody writes the word "analyse" is the unsafe
// direction of exactly the failure the legend rule exists for. It is necessary
// and it is not sufficient - see settleCard.
//
// The second gate is that the card has to be **on screen in full**. Neither
// pane draws one below its own floor, and both clip what they do draw, key line
// first - so a card can be blocking an agent with nothing on screen saying it is
// answerable. Reading a key against that is a confirmation with no account of
// itself anywhere, which is the whole of what the arm below is for.
//
// A key the card has no meaning for is handed back rather than swallowed: the
// composer is empty, so that rune is the first character of what somebody is
// about to type.
//
// # Which card
//
// The one the *focused* pane draws, never the fleet's oldest. A conversation
// puts its own agent's ask and the room puts the rest, so the keys act on what
// the operator is looking at. Read against Cards.Top instead, a key answers
// whichever agent has been blocked longest - which with two blocked agents and
// a conversation open on the second is a card the operator never saw, settled
// with no way back. TestTheKeysAnswerTheCardTheFocusedPaneDraws is that
// arrangement; nothing with one blocked agent can tell the two apart.
func (a App) cardKey(m tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	top, ok := a.cardOf(a.focus)
	if !ok {
		return a, nil, false
	}

	// # Answer mode takes ↵ and nothing else
	//
	// The composer is holding an answer rather than a message, so ↵ takes it
	// and ⎋ abandons it - and *every other key goes to the draft*, which is
	// what "the draft is the answer" has to mean. A digit that still reached
	// the options while a refusal's reason was being written was the accident
	// the disarm rule exists to stop, arriving through the box that replaced
	// the arm: press `d`, think better of it, press `1` to choose instead, and
	// the ↵ after it denied the agent. Now the `1` is a character of the
	// reason, visible in a box the card says is live. See cardanswer.go.
	if q, writing := a.cards.writingFor(top); writing {
		if m.Type != tea.KeyEnter {
			return a, nil, false
		}
		return a.commitAnswer(top, q)
	}

	// Below here the card itself has to be on screen in full, which is what
	// makes its rune keys safe to read. Answer mode is above it deliberately:
	// what accounts for *that* on screen is the composer, which is titled for
	// what it holds and is always drawn - and a long answer is exactly what
	// pushes the card out of view, since the box grows with the draft. Gated
	// here too, ↵ would silently go back to meaning "send a message" at the
	// moment somebody finished typing an answer.
	if !a.cardFullyDrawn() {
		return a, nil, false
	}

	// The navigation keys, which are read whether or not there is a draft.
	//
	// The composer gate below is an argument about *characters* - `a` and `d`
	// are letters people type, and the first character of any draft is typed
	// into an empty composer. It was never an argument about arrows, and
	// applying it to them meant the card's own keys reverted to the roster the
	// moment anything was typed, with the agent still blocked and still
	// asking. That is the state somebody is most likely to be in: they started
	// writing a reply to the thing that stopped them.
	switch m.Type {
	case tea.KeyUp, tea.KeyDown:
		// The card advertises the bare arrows on itself. ⌥↑↓ is the prompt
		// history's on every surface, and a card that took it would make an
		// advertised key mean something else for as long as an agent is asking.
		if m.Alt {
			return a, nil, false
		}
		if m.Type == tea.KeyUp {
			return a.moveOption(top, -1)
		}
		return a.moveOption(top, 1)
	case tea.KeyLeft, tea.KeyRight:
		// ←→ walk the steps, but only on an empty composer: a draft needs them
		// for its own cursor, which is also what makes a typed answer
		// editable. On an empty one they reached the text area and did nothing
		// at all, so the claim costs nothing. ⌥←→ stays word movement.
		if m.Alt || !a.composerEmpty() {
			return a, nil, false
		}
		if m.Type == tea.KeyLeft {
			return a.moveStep(top, -1)
		}
		return a.moveStep(top, 1)
	}

	// Everything below is a character, or ↵ - which has to stay the composer's
	// whenever there is a draft for it to send.
	if !a.composerEmpty() {
		return a, nil, false
	}
	switch m.Type {
	case tea.KeyEnter:
		// Two meanings, and the arm is what separates them: armed, ↵ completes
		// the settle and nothing else; unarmed, it answers the question the
		// cursor is on, which is the key claude's own question screen uses.
		//
		// A mode rather than an ambiguity, because the card says which one is
		// live - keyLine already switches on the arm, so the line under the
		// options reads "↵ choose" or "↵ answer · cannot be undone" and never
		// both. On any other shape this hands the key back and ↵ stays the
		// composer's, which is what opens the picked conversation.
		if _, armed := a.cards.armedKey(top); armed {
			return a.confirmCard(top)
		}
		if top.OnReview() {
			return a.reviewChoose(top)
		}
		return a.chooseCursored(top)
	}
	if m.Type != tea.KeyRunes || len(m.Runes) != 1 {
		return a, nil, false
	}
	switch r := m.Runes[0]; {
	case r == cardAllowKey:
		// Not offered on a question: that shape is settled on its review step,
		// which shows every answer before the press. The arm showed a verb.
		return a.armCard(top, r, top.Shape() != ShapeQuestion)
	case r == cardDenyKey:
		// The reason reaches the model verbatim and is the one channel for
		// saying what to do instead of retrying the identical call, so the
		// refusal opens a box for it. This *is* the arm: nothing is written
		// until ↵, and ⎋ takes it back. An empty box still sends the standing
		// reason, because a blank refusal reads as a tool that failed for no
		// reason.
		return a.beginDeny(top)
	case r >= cardFirstOption && r < cardFirstOption+cardMaxOptions:
		return a.pickOption(top, int(r-cardFirstOption))
	}
	return a, nil, false
}

// settleFrame is what confirming this key writes. It is built at the confirm
// rather than remembered at the arm, so a choice made between the two presses
// travels - and so the arm carries a key rather than a frame, which is what
// lets it be matched against the card it names.
func (c Card) settleFrame(key rune) (rpc.Frame, bool) {
	if key == cardDenyKey {
		return c.Deny(cardDenyReason), true
	}
	if key != cardAllowKey || !c.Answered() {
		return rpc.Frame{}, false
	}
	return c.Allow(), true
}

// armCard takes the first press of a settle key: it remembers which key, on
// which ask, and writes nothing.
//
// # Why two keys, and why this is the only place an undo can exist
//
// A settled card cannot be unsettled. An allow runs the tool. A deny reaches
// the model verbatim as the tool result, so it has then been told - and nothing
// on this wire retracts either: a well-formed allow written after an ask is
// gone produces no frame, no error and no tool run, and whether a denied ask is
// ever re-put is unrecorded. So there is no undo on the far side of the socket,
// and the only moment in which the operator can still change their mind is the
// one before the frame is written. The arm is that moment.
//
// # What makes the second key worth its cost
//
// Not "are you sure". The composer gate above reads a rune only when there is
// no draft for it to be a character of - and the first character of every draft
// is typed into an empty composer, so the gate defends the second character
// onward and leaves the first, which is the whole exposure. `a` and `d` begin a
// great deal of what somebody types at a room full of agents. One keystroke was
// a tool call granted or a refusal the model was told about, with no way back
// and nothing anywhere reporting it.
//
// # And why the confirm is not the same rune
//
// Because the rune is what the accident produces. This shipped once with the
// arming key as its own confirm, defended by "no draft produces the same letter
// twice in a row at position one" - which is a claim about the draft the
// operator *intended*, and says nothing about what they type once a character
// has vanished. The universal recovery for that is to type it again. Worse, it
// needs no confusion at all: `add` is three card keys in a row, so typing `add
// the tests` denied the card. The confirm is ↵, which no draft produces at an
// empty composer, and every other key takes the arm back.
//
// offered gates the allow key on Card.Answered and is checked before the arm: a
// question short one choice is refused beneath the daemon with nothing written,
// and a refusal the operator has to read is worse than a key that was not
// offered. What it must never do instead is fall back to a bare allow - that is
// the defect the whole mechanism exists to close - so an unanswered question
// hands the rune back rather than swallowing it into an arm that advertises
// nothing, and the card keeps saying which digits are left.
//
// **That ordering is currently unobservable and is kept anyway.** Moving the
// gate below the arm was constructed and survived the whole suite, because
// App.key scopes cardKey's model to the `if handled` it is tested in and then
// calls disarmed() on its own copy - so an arm set on the not-handled path is
// discarded twice over. It is an equivalent mutant today and stops being one
// the moment anything threads that model through, which is the change this
// branch already had to make one level up.
func (a App) armCard(c Card, key rune, offered bool) (tea.Model, tea.Cmd, bool) {
	if !offered {
		return a, nil, false
	}
	a.cards = a.cards.arm(c, key)
	return a, nil, true
}

// confirmCard writes the frame an armed settle stands for, and takes the card
// down.
//
// With nothing armed it takes nothing: ↵ is the composer's key, and a card that
// is up is not a reason to swallow it. The gate above has already established
// the composer is empty, so an unarmed ↵ falls through to a submit with no
// draft, which does nothing at all.
//
// The card comes down here rather than when the daemon acknowledges, because
// there is no acknowledgement: an allow produces no frame of its own. What
// makes that safe is Cards.Reconcile, which puts back any ask the next report
// still names.
func (a App) confirmCard(c Card) (tea.Model, tea.Cmd, bool) {
	key, armed := a.cards.armedKey(c)
	if !armed {
		return a, nil, false
	}
	f, ok := c.settleFrame(key)
	if !ok {
		// The card changed under the arm - a question that had every choice
		// when the key was pressed and has fewer now. Take the arm back rather
		// than send something the operator did not arm.
		return a.disarmed(), nil, true
	}
	a.cards = a.cards.Settle(c.AgentID, c.RequestID)
	return a, a.write(answerFailed, f), true
}

// disarmed takes back a settle that has been armed.
//
// The property is *every* input that is not the confirm, and reaching it takes
// four callers because they cover disjoint paths into the model: App.key for
// the keys its own switch claims, App.Update's key arm for the ones that go on
// to the composer, App.Update's mouse arm for a wheel or a click - which never
// reach App.key at all - and pickOption for a digit the card itself takes.
//
// Missing any one of them leaves an arm alive across something the operator
// plainly did instead, to be confirmed by a ↵ pressed later for another reason.
// The mouse is the likeliest of the four: it is what somebody reaches for to
// scroll the room and read the card they are being asked about.
//
// ⌃O's and ⎋'s own arms ride here rather than in disarms beside it: the
// property is the same one - every input that is not the confirm - and the four
// paths are the same four. See detach.go and escape.go.
func (a App) disarmed() App {
	a.cards = a.cards.disarm()
	a.detachArmed = false
	a.escArmed = false
	return a
}

// moveOption walks the option cursor of a question, so the panel beside it
// follows and ↵ has something to answer with.
//
// Handed back on every other shape, which is the whole care in it. ↑↓ is the
// roster's key and a permission ask is a yes/no with nothing for a cursor to
// walk - so a card that claimed them would take the roster's keys for as long as
// any agent stayed blocked, which on a fleet of thirty is most of the time. The
// claim lasts exactly as long as a question is being put in the focused pane.
func (a App) moveOption(c Card, by int) (tea.Model, tea.Cmd, bool) {
	if c.Shape() != ShapeQuestion {
		return a, nil, false
	}
	if _, ok := c.question(); !ok && !c.OnReview() {
		// A question learned from a fleet report, whose options are gone with
		// the event that carried them. There is nothing to walk and the card
		// says so; the key goes back to the roster.
		return a, nil, false
	}
	// Moving is not choosing, so it takes an armed settle back like every other
	// key that is not the confirm.
	a.cards = a.cards.With(c.Move(0, by)).disarm()
	return a, nil, true
}

// moveStep walks between the questions of an ask and the review past them.
//
// Always handled while the card has steps, the way moveOption is: a walk that
// has reached its end is a bounded list, not a key with no meaning. Handing it
// back at the ends would put ← on an empty composer through to the text area,
// which is where it did nothing before this claimed it.
func (a App) moveStep(c Card, by int) (tea.Model, tea.Cmd, bool) {
	if !c.hasSteps() {
		return a, nil, false
	}
	// Moving is not choosing, so it takes an armed settle back like every
	// other key that is not the confirm.
	a.cards = a.cards.With(c.Move(by, 0)).disarm()
	return a, nil, true
}

// chooseCursored answers the question the cursor is on with the option the
// cursor is on. It is ↵'s unarmed meaning, and it goes through pickOption so
// the digit and the key cannot disagree about what choosing does.
func (a App) chooseCursored(c Card) (tea.Model, tea.Cmd, bool) {
	if c.Shape() != ShapeQuestion {
		return a, nil, false
	}
	return a.pickOption(c, c.Option)
}

// pickOption chooses an option for the question the cursor is on and moves to
// the next one.
//
// A digit outside this question's options is not this card's key. It goes back
// to the composer as the character it is, rather than being clamped onto an
// option nobody chose - which would be answering a question with a guess.
func (a App) pickOption(c Card, option int) (tea.Model, tea.Cmd, bool) {
	if c.Shape() != ShapeQuestion {
		return a, nil, false
	}
	// The review's own two options. A digit reaches them the way it reaches a
	// question's, so the key and the arrows cannot disagree about what is
	// selected - see cardsteps.go on why the review borrows the question's
	// bindings rather than earning its own.
	if c.OnReview() {
		if option < 0 || option >= reviewOptions {
			return a, nil, false
		}
		return a.reviewChoose(c.atOption(option))
	}
	if q, ok := c.question(); ok && option == otherIndex(q) {
		// The row past the model's own options: an answer in the operator's
		// words, which is a label the list did not contain.
		return a.beginAnswer(c, c.Cursor)
	}
	if !c.inRange(c.Cursor, option) {
		return a, nil, false
	}
	// Choosing is not confirming, so it takes an armed settle back like every
	// other key. It is also the likeliest deliberate one: the operator meant to
	// answer and pressed the settle key first.
	a.cards = a.cards.With(c.Pick(c.Cursor, option).Move(1, 0)).disarm()
	return a, nil, true
}
