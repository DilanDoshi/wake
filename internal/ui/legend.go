package ui

// The legend under the box: which keys this build has, in the order a narrow
// pane keeps them.
//
// Split out of composer.go when that file reached 786 lines, and at this seam
// rather than at the line count: the legend is a fact about the bindings in
// App.key - a bijection with them, enforced both ways by legend_test.go - and
// the composer is a text box that happens to draw it. internal/core's
// airlock_test.go names *this* file for the word `interrupt`, which is the
// legend's own English rather than a wire subtype.

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// hintIndentWidth is the column the hint line is inset by, and so the column
// the hint itself does not get to use.
const hintIndentWidth = 1

// legendEntry is one key the hint line advertises: the glyph, and what
// pressing it does.
type legendEntry struct{ glyph, what string }

// legendEntries is the legend under the box, and it names only what this build
// actually does.
//
// It used to advertise two things that did not exist: ⇧⇥ to cycle the
// permission mode, which changed a label and never reached the agent, and ⌃⇧A
// to jump to the next agent wanting attention, which was bound to nothing at
// all. A missing feature is not trusted; an advertised one is, and a
// permissions control that silently does nothing fails in the direction that
// costs somebody a repository. ⎋ was added when it started working, which is
// the same rule read the other way.
//
// It is a slice rather than a format string so the guard over it can be exact.
// TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed reads *this* and
// the cases in App.key's own switch, and requires a bijection - so a glyph
// added here with no binding, and a binding added there with no glyph, are
// both build failures. Its first draft iterated a hand-written list of the
// five pairs that already existed, which pinned them and enforced nothing
// about anything new: adding `⇥ next agent` to the legend with no key, or a
// `case tea.KeyTab` with no glyph, both left the package green. A legend that
// lies is the failure this whole rule exists for, so a guard over it that
// cannot see a new lie is not a guard.
// The order is the truncation order, and it is a priority statement rather
// than a grouping. The legend is cut to the pane, so the last entries are the
// ones an 80-column window loses.
//
// ⌃⇧A is deliberately absent. Spec §6 gives the next-agent jump that chord and
// bubbletea v1.3.10 names no ctrl+shift+<letter> at all - probed, both
// encodings, and neither produces a KeyMsg. ⇧⇥ is what carries it instead; see
// docs/notes/decisions.md.
//
// ⇥ took over pane focus because that is what ⇥ means everywhere else and
// because this build is the first with two composers on screen: without it the
// room's composer is reachable only with a mouse, which makes @all mouse-only
// for as long as a conversation is open. That is why it sits fourth here -
// above opening and closing a pane, because with a pane already open it is the
// only way back to the fleet.
//
// ⌃O sits third and ⌃C fourth, which is where ⌃C alone used to sit and for the
// same reason read one key over: leaving is the one thing an operator must be
// able to do from a window too narrow to explain itself, and leaving is ⌃O
// now. ⌃C is beside it because park is the other key somebody reaches for
// without looking, and because a ⌃C that is *not* explained is a ⌃C somebody
// assumes still detaches.
//
// ⌃F sits second to last, which is the truncation order doing its job rather
// than an accident: forking is rarer than scrolling and far rarer than
// leaving, and at 80 columns - a takeover width, where the conversation has the
// whole pane - `wake fork <who>` is one shell line away. It shadows the text
// area's CharacterForward, which is the third such shadow after ⌃D and ⌃W and
// the cheapest of them: ctrl+f moves one character, which is → by another name,
// and → is unshadowed. The box growing with the draft does not change that -
// CharacterForward was never about rows.
//
// ⌃T sits last, and it is the entry the truncation cuts first. That is the
// order working: the mode's *current value* is on the target line whenever it
// decides anything - `→ @john · open · 20 turns` - so the legend entry is
// advertising the key rather than carrying the state, and a pane too narrow
// for it is a pane where the state is still visible above it. It is also the
// rarest key here: a mode is set once and then typed under.
//
// **It shadows the text area's TransposeCharacterBackward**, which is the
// fourth such shadow after ⌃D, ⌃W and ⌃F, and the cheapest of the four -
// transposing the two characters behind the cursor is an emacs nicety with
// retyping as its alternative. It is taken
// rather than one of the two keys bubbletea and bubbles both leave free,
// because ⌃T is the key spec §7 names and because those two are ⌃X and ⌃Y with
// deferred I7 already owed one of them. Nothing here is built on ⇧⇥.
//
// ⌃Q sits second to last, below ⌃F, and that is deliberate rather than a leftover.
// Parking the fleet and closing Wake is the rarest thing here and the most
// recoverable of the consequential ones - `wake` reopens the room and offers
// back whatever finished in time - so an 80-column pane losing it costs a key
// that has two alternatives (⌃O and closing the terminal both leave the fleet
// alive). Losing ⌃O instead would leave a pane with no advertised way out.
//
// **Its label reads `quit & park all` rather than `park all & quit`, and the
// order of those four words is the whole of why.** Sitting last, ⌃Q is the
// entry the truncation cuts, and the cut is a plain right-truncate: `park all
// & quit` becomes `park` at 164 columns - which is the widest room pane this
// product has, and is ⌃C's entire label. An operator would read a finished,
// correct entry advertising `⌃Q park` beside `⌃C park`, where one of the two
// closes the workspace. That is worse than the entry being absent, and it is
// this file's own quoted-forward ruling arriving from the renderer. `quit`
// survives the same cut and is true. TestNoWidthCutsALegendEntryIntoADifferentKeysLabel
// holds the class rather than this instance, at every width.
//
// The unshadowed keys bubbletea and bubbles both leave free were ⌃O, ⌃X and
// ⌃Y - detach, next blocked, and open right. **That set is spent**, so every
// key from here shadows something, and ⌃B open below already does: it takes the
// text area's CharacterBackward, which moves one character and so is ← by
// another name. Both grid keys are letters because macOS eats ctrl+shift+arrows; see
// keys.go and docs/notes/decisions.md. ⌃Q is not
// from that set: it is a chord nothing in this build or in bubbles binds, and
// keyprobe_test.go asserts bubbletea names \x11 as ctrl+q. The tty half of that
// question is settled by reading rather than assumed: ⌃Q is XON and ⌃C is INTR,
// both of which the driver would otherwise eat, and bubbletea's initInput calls
// MakeRaw from github.com/charmbracelet/x/term@v0.2.2 - the module on this
// path, not golang.org/x/term - which clears IXON and ISIG. What is left is anything between
// the keystroke and Wake that is not the driver - tmux, ssh, a terminal that
// ignores raw mode - and that is in docs/live-testing.md, because go test has no
// tty to try it on.
const (
	// escGlyph is how the legend spells the key. A constant because hintParts
	// matches on it to swap the label.
	escGlyph = "esc"

	// escInterruptLabel is what the legend calls ⎋ ordinarily, escClearLabel is
	// what it calls ⎋ while the next press would clear the draft instead, and
	// escRewindLabel is what it calls ⎋ while the next press would open the
	// rewind picker instead - an idle, empty conversation's second press, which
	// has no draft for escClearLabel to describe. See escape.go and rewind.go.
	escInterruptLabel = "interrupt"
	escClearLabel     = "clear draft"
	escRewindLabel    = "rewind"

	// The two glyphs a live ⌃O arm re-labels, and what it calls them.
	//
	// **Both, because either alone would be a legend that lies.** Armed, ↵ no
	// longer sends and ⌃O no longer detaches - it takes the arm back - so a
	// swap on one glyph leaves the other advertising a meaning that is not
	// live. `↵ detach` is also the entry that survives every truncation, being
	// first, which is what makes the arm visible in a pane too narrow for
	// anything else. See detach.go.
	sendGlyph        = "↵"
	sendLabel        = "send"
	armedSendLabel   = "detach"
	detachGlyph      = "⌃O"
	detachLabel      = "detach"
	armedDetachLabel = "cancel"
)

var legendEntries = []legendEntry{
	{sendGlyph, sendLabel},
	{escGlyph, escInterruptLabel},
	{detachGlyph, detachLabel},
	{"⌃C", "park"},
	{"⇥", "next chat"},
	{"⇧⇥", "permissions"},
	{"⌃X", "next blocked"},
	{"↑↓", "prompt history"},
	// Beside the bare arrows rather than with the other pane keys: ⇧↑↓ is the
	// same two keys under a modifier, and a legend that separated them would make
	// a reader hunt for the difference. The roster used to be the bare ↑↓; it
	// moved here when those took Claude Code's prompt-history recall.
	{"⇧↑↓", "pick agent"},
	// After the arrows rather than beside ⇥: it is a movement key, but it moves
	// among panes already drawn, so it is worth less than the permission mode to
	// somebody in an 80-column pane. Placing it higher would push ⇧⇥ out of that
	// set, which is a change to what a narrow pane advertises.
	{"⇧←→", "move focus"},
	{"⌃D", "open DM"},
	{"⌃Y", "open right"},
	{"⌃B", "open below"},
	{"⌃W", "close pane"},
	{"⌃G", "workspaces"},
	{"⌃R", "activity"},
	{"⇞⇟", "scroll"},
	{"⌃E", "expand"},
	{"⌃F", "fork"},
	{"⌃Q", "quit & park all"},
	{"⌃T", "mention mode"},
}

// modeFormat is how a permission mode is spelled on either line that names one.
//
// Here it is the tail of the legend: the current value of the thing ⇧⇥ six
// entries above it changes. It carries no glyph of its own because it is not a
// binding - ⇧⇥ is the binding, and this is what that key is looking at. The
// conversation's status bar uses the same constant for the same fact, so the
// two lines of one pane cannot spell it differently; the rulings below are the
// legend's own and statusbar.go states where they stop.
//
// **Whose mode it is, is App.hintMode's ruling and not this file's**: the agent
// ⇧⇥ would act on. A value here naming a different agent from the key beside it
// would be the exact lie this pair was rebuilt to stop telling.
//
// Which is why a **blurred** composer draws no mode at all. With both panes up
// there are two legends on screen, and a DM's would name its own agent while the
// key names the roster's pick - one visible `permissions: …` describing
// something ⇧⇥ will not touch. Every other glyph in a blurred legend already
// refers to the focused target; the mode is the only entry that reads as a claim
// about the pane it sits in, so it is the only one withheld.
//
// It stays last, which is the truncation order rather than a demotion. A key is
// something to do and the mode is something to know, and the narrow pane that
// loses this is one where the roster is still on screen. See
// TestANarrowLegendKeepsTheKeysAndDropsTheMode.
const modeFormat = "permissions: %s"

// hintSep separates the groups in the legend.
const hintSep = "   "

// legendArms is which of App's arms a legend should name, carried as one value
// rather than a bool per arm: a second parameter per arm is how the swap ends
// up spelled differently in the two places that render it.
//
// At most one is ever set - arming any of them goes through App.disarmed
// first, and esc/rewind are themselves split from the same escArmed bit by
// rewindArmable (armsFor), which is mutually exclusive with clearsOnEscape by
// construction - so the legend never has two swapped labels at once. See
// escape.go and rewind.go.
type legendArms struct{ esc, rewind, detach bool }

// hintLine renders the legend for one permission mode.
func hintLine(mode string) string {
	return strings.Join(hintParts(mode, legendArms{}), hintSep)
}

// hintParts is the legend in priority order: every key, then the mode.
//
// One place builds this, because the order is a statement rather than a
// grouping - it decides what survives a narrow pane, and every ordinary
// terminal truncates (see CLAUDE.md). A second builder would let the order the
// tests read drift from the order the composer draws.
//
// An empty mode draws the keys and no tail, which is how a blurred composer is
// spelled - see modeFormat for why that pane says nothing about a mode.
//
// An arm swaps labels rather than adding entries: every glyph here is already
// bound, and a second glyph for a second meaning would be a glyph with no
// tea.Key case behind it, which this legend's own guard refuses. See escape.go
// and detach.go.
func hintParts(mode string, arms legendArms) []string {
	parts := make([]string, 0, len(legendEntries)+1)
	for _, e := range legendEntries {
		switch {
		case arms.esc && e.glyph == escGlyph:
			e.what = escClearLabel
		case arms.rewind && e.glyph == escGlyph:
			e.what = escRewindLabel
		case arms.detach && e.glyph == sendGlyph:
			e.what = armedSendLabel
		case arms.detach && e.glyph == detachGlyph:
			e.what = armedDetachLabel
		}
		parts = append(parts, e.glyph+" "+e.what)
	}
	if mode == "" {
		return parts
	}
	return append(parts, fmt.Sprintf(modeFormat, mode))
}

// hintFitting is the legend cut to a width **at an entry boundary**.
//
// lipgloss's MaxWidth cuts by cell, which left the line ending in a bare glyph
// advertising nothing - `… ⇧⇥ next blocked   ⌃D` - at 80 columns and at most
// other widths, because the legend is cut to a *pane* and every ordinary
// terminal truncates it. An entry that does not fit is dropped whole.
//
// That closes both halves of the same defect at once. The ragged half is this
// one; the dangerous half is a cut landing inside a label so that what remains
// reads as a *different* key's whole label, which is why `⌃Q` is `quit & park
// all` rather than `park all & quit` and why
// TestNoWidthCutsALegendEntryIntoADifferentKeysLabel exists. Cutting at a
// boundary means neither can happen rather than one being spelled around.
//
// Returning an empty string when not even the first entry fits is deliberate:
// a pane that narrow has nothing useful to say, and half of `↵ send` is worse
// than silence.
func hintFitting(mode string, width int, arms legendArms) string {
	line := ""
	for i, part := range hintParts(mode, arms) {
		next := part
		if i > 0 {
			next = line + hintSep + part
		}
		if lipgloss.Width(next) > width {
			break
		}
		line = next
	}
	return line
}
