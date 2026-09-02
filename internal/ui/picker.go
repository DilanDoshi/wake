package ui

// The menu Wake draws for the commands claude cannot draw headless.
//
// Typing `/effort` at an agent prints a usage line, because every session runs
// `--print --input-format stream-json` and there is no interactive picker on
// that side - no frame could carry one. So Wake draws it and sends the level
// the operator chose as ordinary text, down the passthrough that already works
// and that daemon.noteEffort already watches.
//
// # Why this is not a Card
//
// Cards.Reconcile rebuilds the open set from every fleet report, keyed on
// (AgentID, RequestID), and drops what is absent. A picker has no request id
// and appears in no report, so one held there would be deleted by the next
// status push - which lands whenever any of thirty agents changes state. It
// would vanish in milliseconds on a busy fleet, and never on a quiet one, which
// is the worst version of that bug.
//
// So the lifecycle is the opposite of a card's in every respect: Wake creates
// it, Wake settles it, and the daemon never hears about it until the command it
// builds goes out as text. What it shares is the drawing - optionRow and
// detailRow below are cards_blocks.go's, not a second bordered list.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
)

// typedEscape is the last row of the model picker, and it is claude's own
// wording for the thing it does: the recorded reply ends its list of models
// with "or a full model ID".
//
// It exists because the list is a snapshot of one binary. A model shipped
// tomorrow has to be reachable without a Wake release, so the picker may never
// become the fence that a closed list would make it.
const typedEscape = "type one…"

// Picker is a choice Wake is putting to the operator about one session's
// configuration.
type Picker struct {
	// Word is the command this answers, without the prefix.
	Word string

	// Targets is who the chosen command goes to, resolved before the picker
	// opened. Held rather than re-resolved on confirm: the fleet moves, and a
	// picker that retargeted itself between opening and confirming would
	// configure a session nobody chose.
	Targets []string

	// Names is what those targets are called, for the header. Held for
	// Targets' reason - a name resolved at confirm time could name a different
	// session, or none.
	Names []string

	// Current is the value the one target is already at, shown so a bare command
	// answers "what is it now" before it changes anything. Empty when there is
	// more than one target (no single answer) or the value is unknown. For
	// /effort it is one of Options - the level words - so the matching row is
	// marked and the cursor opens on it; for /model it is a display name that
	// does not reverse-map to an alias, so it is a line only. See App.pickerCurrent.
	Current string

	Options []string
	Cursor  int
}

// Open reports whether there is a picker at all. The zero value is closed,
// which is what makes "no picker" the default state of an App.
func (p Picker) Open() bool { return p.Word != "" }

// pickerOptions is what each command offers.
//
// Keyed on the same words bareOnlyCommands holds, and a word in one and not the
// other is a build failure - see TestEveryBareOnlyCommandHasOptions. Both lists
// are core's: the levels because `/effort` prints them in its usage line, the
// models because the bare `/model` enumerates them in its reply.
func pickerOptions(word string) []string {
	switch word {
	case effortCommand:
		return core.EffortCommands
	case modelCommand:
		// The escape is appended rather than held in core, because it is a
		// row in a menu rather than a value anything may send.
		return append(append([]string{}, core.ModelAliases...), typedEscape)
	}
	return nil
}

// openPicker puts one up, replacing whatever was there.
//
// One at a time: a second `/effort` is somebody changing their mind, not a
// second question, and a stack would put the older one back after a choice
// nobody remembers making.
func (a App) openPicker(word string, targets []string) App {
	names := make([]string, 0, len(targets))
	for _, id := range targets {
		names = append(names, a.agentName(id))
	}
	current, cursor := a.pickerCurrent(word, targets)
	a.picker = Picker{Word: word, Targets: targets, Names: names, Options: pickerOptions(word), Current: current, Cursor: cursor}
	return a.clearDraft()
}

// pickerCurrent is the value the one target is already at, and where the cursor
// should open. Empty and zero when there is more than one target - a broadcast
// has no single current value - or when the target's value is unknown.
//
// Effort is a level word, so it is one of the options: the cursor opens on it
// and View marks the row. A model is a display name that does not reverse-map to
// an alias (opus, opus[1m] and opusplan all render the same), so it is returned
// as a line and the cursor stays at the top.
func (a App) pickerCurrent(word string, targets []string) (current string, cursor int) {
	if len(targets) != 1 {
		return "", 0
	}
	ag, ok := a.fleet.Agent(targets[0])
	if !ok {
		return "", 0
	}
	switch word {
	case effortCommand:
		for i, opt := range pickerOptions(word) {
			if opt == ag.Effort {
				return ag.Effort, i
			}
		}
	case modelCommand:
		if m := ag.ConfirmedModel; m != "" {
			return m, 0
		}
		return modelName(ag.Model, ag.ContextWindow), 0
	}
	return "", 0
}

// closePicker takes it down with nothing sent.
func (a App) closePicker() App {
	a.picker = Picker{}
	return a
}

// pickerKey is ↑↓ to move, ↵ to choose and esc to cancel, and nothing else.
//
// **No digit keys, deliberately.** cardKey's arm-then-confirm exists because a
// card arrives *unbidden* and the first character of a draft lands in an empty
// composer, so a stray `a` would grant a tool call. A picker is opened by the
// operator two keystrokes ago and cannot surprise anybody, so the accident that
// rule manages does not arise - and taking no runes at all removes the class
// rather than managing it.
//
// Read before App.key's own switch, exactly as cardKey is, which is also why
// this adds no legend entry: legendEntries is a bijection with the cases in
// that switch, and this is an interception above it.
//
// A key that is not one of those four goes to the composer, and **the picker is
// dismissed there rather than here** - see App.update, which does the same for
// an armed card and for the same reason: a key this function declines is one
// whose App this function's caller throws away. Typing is somebody who has
// stopped choosing, and a menu that stayed up through a draft would take the ↵
// meant to send it, which is how `/model` typed over an open `/effort` picker
// confirmed the effort and lost the command.
func (a App) pickerKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	if !a.picker.Open() {
		return a, nil, false
	}
	switch m.Type {
	case tea.KeyUp:
		return a.movePicker(-1), nil, true
	case tea.KeyDown:
		return a.movePicker(1), nil, true
	case tea.KeyEsc:
		return a.closePicker(), nil, true
	case tea.KeyEnter:
		next, cmd := a.confirmPicker()
		return next, cmd, true
	}
	return a, nil, false
}

// movePicker walks the list without wrapping: a cursor that wraps makes the
// ends of a ten-row list indistinguishable at a glance.
func (a App) movePicker(by int) App {
	p := a.picker
	p.Cursor = clamp(p.Cursor+by, 0, max(len(p.Options)-1, 0))
	a.picker = p
	return a
}

// confirmPicker sends the command the choice built, or hands the draft back.
func (a App) confirmPicker() (App, tea.Cmd) {
	p := a.picker
	if p.Cursor < 0 || p.Cursor >= len(p.Options) {
		return a.closePicker(), nil
	}
	chosen := p.Options[p.Cursor]
	a = a.closePicker()

	if chosen == typedEscape {
		// Not a second input mode: the picker hands back a half-typed draft
		// and gets out of the way. What the operator finishes has an argument,
		// so the fence no longer claims it and it passes through byte for byte
		// like any hand-typed command.
		return a.withComposer(a.composer().WithDraft(configureVerb(p.Word) + " ")), nil
	}

	text := configureVerb(p.Word) + " " + chosen
	// Echoed the way a typed message is. A level that changed with nothing in
	// the transcript is a session whose configuration has no history, and both
	// send paths already append their own text for exactly that reason.
	if a.focus != "" {
		a = a.withDM(a.focus, a.dms[a.focus].Append(core.Event{Kind: core.KindUserText, SessionID: a.focus, Text: text}))
	} else {
		a = a.withRoom(a.room.Append(core.Event{Kind: core.KindUserText, Text: text}, Agent{}))
	}
	for _, id := range p.Targets {
		a.fleet = a.fleet.sending(id, a.focus != "")
	}
	// One command writing N frames rather than N commands, which is
	// /resume all's rule: bubbletea runs every tea.Cmd on its own goroutine and
	// rpc's write lock is process-wide.
	return a, a.write(sendFailed, sendFrames(p.Targets, text, nil)...)
}

// pickerHeader says what is being configured and who for, which is what makes
// an unaddressed command safe: the target is on screen before a key is pressed
// rather than discovered afterwards.
//
// One name when there is one and a count when there are more - bringBack's rule
// and its reason, that "1 agents" is worse than a name.
func (p Picker) Header() string {
	switch len(p.Names) {
	case 0:
		return p.Word
	case 1:
		return fmt.Sprintf("%s for %s%s", p.Word, agentPrefix, p.Names[0])
	default:
		return fmt.Sprintf("%s for %d agents", p.Word, len(p.Names))
	}
}

// View draws it, through the same rows a card draws.
func (p Picker) View(width int) string {
	if !p.Open() {
		return ""
	}
	rows := make([]string, 0, len(p.Options)+2)
	rows = append(rows, detailRow(p.Header(), width))
	// What the one target is already at, so the menu answers "what is it now"
	// before it changes anything.
	if p.Current != "" {
		rows = append(rows, detailRow("current: "+p.Current, width))
	}
	for i, option := range p.Options {
		// chosen marks the current row - true only for /effort, where Current is
		// one of the options; a /model alias never equals a display name, so its
		// menu carries the current on the line above and marks nothing.
		rows = append(rows, optionRow(option, width, i == p.Cursor, option == p.Current, AccentStyle))
	}
	return strings.Join(rows, "\n")
}
