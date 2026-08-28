package ui

// ⎋, and the second one.
//
// The room has cleared its draft on a single press since 3f8c662, because the
// room is not one agent and stopping "whichever one the cursor rested on" is a
// turn stopped somewhere nobody was looking. A conversation pane has the
// opposite rule and keeps it: ⎋ stops the turn and the draft survives, because
// a person types the correction and stops the turn in either order.
//
// That left a conversation with no way to clear a draft short of holding ⌫,
// which is what ⎋⎋ is for.
//
// # Why an arm and not a timer
//
// This tree already has arm-then-confirm: a card's a/d are read only from an
// empty composer and settled by a second ↵, and every input that is not the
// confirm takes the arm back. A double-key on a stopwatch would be a second
// mechanism for the same idea, and the first thing in this program running on
// a clock - which the first non-negotiable is about. So the two presses are
// consecutive keystrokes with nothing between them, and the disarm is
// App.disarmed's, which already reaches the four paths into the model.
//
// # Why it arms only when there is something to clear
//
// Mashing ⎋ at a runaway agent has to keep stopping it. With an empty draft
// there is nothing for a second meaning to take, so every press is an
// interrupt and nothing is ever armed.
//
// # Why it adds no glyph to the legend
//
// TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed requires a bijection
// between legendEntries and the tea.Key… cases in App.key. This adds no case -
// it is still tea.KeyEsc - so a `⎋⎋ clear` entry would be a glyph with no
// binding, which is a build failure, and the legend is already wider than any
// terminal draws. The armed pane changes ⎋'s *label* instead: same glyph, same
// binding, one word different, at the only moment it means anything. The three
// words live beside legendEntries in legend.go, which is the file the airlock's
// allowlist exempts for the label "interrupt". ⌃O's arm swaps two labels there
// for the same reason; see detach.go.

import (
	tea "github.com/charmbracelet/bubbletea"
)

// escape is ⎋ in whichever pane holds the keys.
//
// armed is read before App.key's disarm rather than off the model, for the
// reason the disarm is unconditional: every other key has to take the arm back,
// and the cheapest way to be sure of that is to clear it for all of them and
// hand this one the value it had.
//
// collapsed is a double press bubbletea handed over as **one** message. A
// terminal delivers whatever bytes are pending in a single read, and bubbletea
// reads a lone ESC followed by another byte as that byte with Alt set - so
// ⎋⎋ pressed at any speed a person actually presses it arrives as `alt+esc`
// and not as two. Measured, and held by
// TestTwoEscapesInOneReadCollapseIntoAltEsc. Without this branch the feature
// works only for somebody pressing slowly enough to straddle two reads, which
// is the failure nobody reports and everybody feels.
//
// The two speeds end in the same place deliberately: a collapsed press stops
// the turn *and* clears the draft, because that is what the slow one does
// across its two presses.
//
// # The third case: an idle, empty conversation opens the rewind picker
//
// A non-empty composer's second press has a draft to clear; an idle agent's
// empty one has neither a draft nor a turn worth a second interrupt, so its
// second press means "show me what to rewind to" instead. rewindArmable is
// what recognises the case, and it is mutually exclusive with clearsOnEscape
// by construction - one requires an empty composer and the other a non-empty
// one - which is what lets escArmed's second-press meaning be read off
// composer state alone, with no third field to keep in step. The **first**
// press still interrupts, harmlessly, on an agent already idle: arming and
// interrupting together is the move clearsOnEscape's own first press already
// makes, and this reuses it rather than adding a second arm. A **collapsed**
// press is the same two presses arriving as one message, so it interrupts
// too, alongside the ask - the two speeds agreeing is the rule two paragraphs
// up, and this is that rule's own case. rewindArmable is read fresh on both
// presses, so a status push that starts a turn between them is seen: the
// second press then falls through to a genuine interrupt instead of opening a
// picker on stale idleness. See rewind.go.
func (a App) escape(armed, collapsed bool) (tea.Model, tea.Cmd, bool) {
	// A draft being written as an answer is abandoned, and the agent's turn is
	// left alone: the operator is dropping a sentence, not the work. Checked
	// first because answer mode is the one state in which the draft is not a
	// message, so neither of the rules below is about it. See cardanswer.go.
	if card, ok := a.cardOf(a.focus); ok {
		if _, writing := a.cards.writingFor(card); writing {
			return a.cancelAnswer(), nil, true
		}
	}
	clears := a.clearsOnEscape()
	if armed && clears {
		// No second interrupt. The first press already sent one, and a frame
		// about a turn that is already being torn down says nothing.
		return a.clearDraft(), nil, true
	}
	rewindable := a.rewindArmable()
	if rewindable && (armed || collapsed) {
		model, cmd, handled := a.askRewindTargets()
		if next, ok := model.(App); ok && collapsed {
			// A collapsed press is both presses landing in one message, and
			// the slow path's first press still interrupts here - harmlessly,
			// on an agent already idle, clearsOnEscape's own reason a few
			// lines up. Without this the two speeds disagree about whether
			// ⎋ interrupts, which is the one thing every path through this
			// function has to agree on.
			_, interruptCmd, _ := next.interrupt()
			cmd = tea.Batch(cmd, interruptCmd)
		}
		return model, cmd, handled
	}
	if clears || rewindable {
		// Nothing to arm when both presses have already happened. Reached with
		// collapsed true only from the clears side - a collapsed press while
		// rewindable already returned above.
		a.escArmed = !collapsed
	}
	model, cmd, handled := a.interrupt()
	if next, ok := model.(App); ok && collapsed && clears {
		model = next.clearDraft()
	}
	return model, cmd, handled
}

// clearsOnEscape is whether a second ⎋ has anything to do here: a conversation
// pane with a draft in it.
//
// The room is excluded because its *first* ⎋ already clears - see interrupt -
// so arming there would put a second meaning on a key that has nothing left to
// take.
func (a App) clearsOnEscape() bool { return a.focus != "" && !a.composerEmpty() }
