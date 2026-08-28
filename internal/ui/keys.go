package ui

// Which key means what, for the keys the App itself owns.

// Split out of app.go when that file crossed the 800-line hard max, and at this
// seam rather than at the line count: the switch below is one subject - the
// bindings the room and its panes answer to - and it already had a neighbour in
// cardkeys.go, which holds the keys a card reads before this ever sees them.

// TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed reads *this file* for
// the tea.Key cases and holds them to ui.legendEntries in both directions, so
// the file name is part of that guard rather than an organisational detail.

import (
	tea "github.com/charmbracelet/bubbletea"
)

// key handles the bindings the App owns, reporting whether it took the message.
// Anything it does not take goes to the focused pane's composer.
//
// # Card keys come first, and every other key takes an armed settle back
//
// Which keys an answerable card reads, why the composer has to be empty for
// it, why that gate is necessary and not sufficient on its own, why the card
// also has to be on screen, and why settling takes a key and then ↵ are all in
// cardkeys.go. They are runes rather than chords deliberately: the card
// advertises them itself, on the card, which is where an affordance that only
// exists while a card is up belongs - and it is also where the override is
// advertised, because while a settle is armed ↵ confirms it rather than sending
// a draft there is not.
//
// The disarm below is the half a card cannot reach: a chord goes nowhere near
// cardKey, so without it an arm outlives every other thing the operator does
// and is confirmed by a ↵ pressed later for another reason. See App.disarmed
// for the four call sites and why they are four.
//
// Every key in the switch below is named in the composer's legend and every
// glyph in that legend is here - see
// TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed and the hint line's
// own comment for why that is a rule rather than a habit.
func (a App) key(m tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	// A dragged image path arrives as a bracketed paste. Taken above everything
	// so the path becomes a chip rather than being read as a chord or typed into
	// the draft; only an actual paste of image paths is claimed. See imagedrop.go.
	if model, cmd, dropped := a.droppedImage(m); dropped {
		return model, cmd, true
	}
	// The board first, above everything: while it is up nothing under it is
	// drawn, so a card's keys or the roster's arrows firing from under it
	// would answer a surface nobody can see. A key it does not claim closes
	// it and goes on to do its own job - which is why the model is kept even
	// when the key is not taken. See board.go.
	boardWasUp := a.board.Up
	if next, cmd, handled := a.boardKey(m); handled {
		return next, cmd, true
	} else {
		a = next
	}
	// Read before the disarm takes it, because the disarm is what makes "every
	// other key takes the arm back" true without a call site per key. See
	// detach.go.
	//
	// **Above the card and the picker rather than beside the disarm below**,
	// and that placement is this arm's alone: cardKey returns before the disarm
	// below, so an arm read there would survive every card key. Conditional,
	// because an unconditional disarm here would take a card's own settle back
	// before cardKey has read it.
	//
	// **A drawn card still wins ↵, and that is the safe way round.** Only a
	// question card claims it unarmed, and only to move a cursor - chooseCursored
	// goes through pickOption, which writes no frame - so the cost of the card
	// winning is a pick that can be re-picked and an arm the legend immediately
	// shows as gone. The other way round costs a detach somebody aimed at an
	// answer. A permission card hands ↵ back, so the confirm below still fires.
	//
	// The card is the *only* thing that wins it, and the discriminator is what
	// is drawn: a card puts its own key line on screen saying `↵ choose`, so
	// there are two statements about the key and the nearer one is right. The
	// roster's pick and the dispatch cursor draw no such line, so for them the
	// legend's `↵ detach` is the only statement there is and it has to be true.
	detachArmed := a.detachArmed
	if detachArmed {
		a = a.disarmed()
	}
	// Not while the board was up when the key arrived: the card was on no
	// screen then, and a rune answering it would arm a settle on an ask nobody
	// saw - cardFullyDrawn cannot see this, because the board is already
	// closed by the time the gate runs. The rune goes to the draft instead,
	// which is the unclaimed-key rule's own promise.
	if !boardWasUp {
		if model, cmd, handled := a.cardKey(m); handled {
			return model, cmd, true
		}
	}
	// Above the switch for cardKey's reason - it claims ↑↓ and ↵, which mean
	// something else while a menu is up - and below the card, because a card is
	// an agent waiting on an answer and a picker is not.
	//
	if next, cmd, handled := a.pickerKey(m); handled {
		return next, cmd, true
	}
	// pickerKey's own placement and reason: a rewind picker claims ↑↓ and ↵
	// while it is up, which mean something else once it closes. See rewind.go.
	if next, cmd, handled := a.rewindKey(m); handled {
		return next, cmd, true
	}
	// Read before the disarm below takes it, because the disarm is what makes
	// "every other key takes the arm back" true without a call site per key.
	// See escape.go.
	escArmed := a.escArmed
	a = a.disarmed()
	// Below the disarm and above the switch: ⇥ finishes a word being typed
	// before it moves the keys to another pane, and ⌃N/⌃P walk the offers. Only
	// while a menu is up, and it claims neither ↑↓ nor ↵ - see completion.go.
	if next, cmd, handled := a.completionKey(m); handled {
		return next, cmd, true
	}
	switch m.Type {
	case tea.KeyCtrlC:
		return a.park()
	case tea.KeyCtrlQ:
		return a.parkFleet()
	case tea.KeyCtrlO:
		// Detach: the TUI exits and the fleet keeps working. That is the
		// property the daemon exists to provide, and it reaches the daemon as
		// nothing at all - the client just disconnects. It survives as its own
		// key because "close the terminal, let twenty agents keep working" is
		// what the background daemon is for; losing it would make the daemon
		// pointless.
		//
		// **This key never quits.** It arms, and a second press cancels;
		// tea.Quit is ↵'s, below. ⌃O expands a truncated tool result in Claude
		// Code, and a confirm on the same key fires on the repeat that reflex
		// produces. See detach.go.
		return a.armDetach(detachArmed)
	case tea.KeyEsc:
		// m.Alt is ⎋⎋ that shared one read, which is what a double press
		// actually looks like. See escape.go.
		return a.escape(escArmed, m.Alt)
	case tea.KeyEnter:
		// **⌥↵ is a newline and not a send**, so it goes on to the composer.
		//
		// This switch is on m.Type alone, and bubbletea reports ⌥↵ as KeyEnter
		// with Alt set - so without this arm the App took it as send, and the
		// newline binding beside it could never fire. ESC CR is what a terminal
		// set up for ⇧↵ emits, which makes this the arm that decides whether the
		// chord works at all.
		//
		// Caught by the pty harness and by nothing else: an in-process test
		// hands the composer the KeyMsg directly, so it passes whatever this
		// does. See TestADraftTakesASecondLineFromRealKeyBytes.
		if m.Alt {
			return a, nil, false
		}
		// The armed detach's confirm, below ⌥↵ because that one is a newline
		// the composer owns and so is an input like any other. Above the send,
		// because that is what being armed *means* - and the legend says so for
		// as long as it is: `↵ detach` in every pane on screen. See detach.go.
		if detachArmed {
			return a, tea.Quit, true
		}
		// With nothing to send, ↵ opens the conversation the cursor is on -
		// which is what every list does, and what ↑↓ leads somebody to try.
		// From inside a conversation as well as from the room: the cursor only
		// counts while the sidebar holding it is drawn, so this fires exactly
		// when somebody is looking at the list they are moving through.
		// submit already returns unchanged on a blank draft, so this claims a
		// key that was doing nothing rather than taking one from the composer.
		// A card takes ↵ before this; see cardKey.
		if a.composerEmpty() {
			if agent, ok := a.pickedAgent(); ok {
				return a.openDMWith(agent.ID, agent.Name), nil, true
			}
		}
		model, cmd := a.submit()
		return model, cmd, true
	case tea.KeyUp:
		// **⌥↑ walks the prompt history and the bare arrow moves the roster.**
		//
		// Same trap as ⌥↵ below: this switch is on m.Type alone and bubbletea
		// reports ⌥↑ as KeyUp with Alt set, so without this arm the roster
		// swallows it and the history key could never fire. See prompts.go, and
		// TestAltArrowsWalkThePromptHistoryFromRealKeyBytes for the bytes.
		if m.Alt {
			return a.walkPrompts(1)
		}
		return a.pickAgent(-1), nil, true
	case tea.KeyDown:
		if m.Alt {
			return a.walkPrompts(-1)
		}
		return a.pickAgent(1), nil, true
	case tea.KeyCtrlD:
		return a.openDM(), nil, true
	case tea.KeyCtrlF:
		return a.fork()
	// Letters rather than the ⌃⇧→/⌃⇧↓ these shipped as: macOS takes every
	// ctrl+shift+arrow for spaces and Mission Control before a terminal sees
	// one. TestNoKeyIsACtrlArrow holds the class.
	case tea.KeyCtrlY:
		return a.openPicked(App.openRight), nil, true
	case tea.KeyCtrlB:
		return a.openPicked(App.openBelow), nil, true
	case tea.KeyCtrlW:
		return a.closeDM(), nil, true
	// Claude Code's own key for this is ⌃O, which detaches here. See expand.go
	// for why the text area's "line end" is what this shadows instead.
	case tea.KeyCtrlE:
		return a.toggleExpanded()
	// Shift rather than the ⌘+arrow asked for, and for the same reason as the
	// two letters above: bubbletea's arrow table knows modifier params 2-8 and
	// cmd is bit 8, so ⌘→ is param 9 and the library names nothing for it -
	// and no macOS terminal transmits ⌘ to a tty anyway. ⌃+arrow is named and
	// delivered and still wrong: macOS spends all four on spaces and Mission
	// Control. Shift is the one arrow family free at every layer, the text area
	// under the composer included.
	case tea.KeyShiftLeft:
		return a.movePane(Left), nil, true
	case tea.KeyShiftRight:
		return a.movePane(Right), nil, true
	case tea.KeyShiftUp:
		return a.movePane(Up), nil, true
	case tea.KeyShiftDown:
		return a.movePane(Down), nil, true
	case tea.KeyCtrlG:
		return a.toggleGroups(), nil, true
	case tea.KeyCtrlR:
		return a.toggleRoster(), nil, true
	case tea.KeyTab:
		return a.nextChat(), nil, true
	case tea.KeyShiftTab:
		// The founding message's one named shortcut. It held next-blocked until
		// there was a mechanism behind this; goals.md §5 is the debt and ⌃X is
		// the payment.
		return a.cycleMode()
	case tea.KeyCtrlX:
		return a.nextBlocked(), nil, true
	case tea.KeyCtrlT:
		return a.flipMention()
	case tea.KeyPgUp:
		return a.scrollFocused(a.pageLines()), nil, true
	case tea.KeyPgDown:
		return a.scrollFocused(-a.pageLines()), nil, true
	}
	return a, nil, false
}
