package ui

// The keys this build has, and the one row of them the composer still draws.
//
// **The always-on legend is gone.** The composer used to draw every key under
// the box on every frame; the owner found that row redundant with the status
// bar, which already names the permission mode and the rest of what a pane is.
// So an ordinary composer now draws no legend row at all. What survives is the
// *armed cue*: the row that says ↵ will detach, or ⎋ will clear the draft,
// drawn only while that arm is live. It is the sole on-screen signal that the
// next keypress does something irreversible, which is the whole reason it stays
// where the rest went.
//
// legendEntries itself is unchanged and still load-bearing.
// legend_test.go's TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed
// requires a bijection with the tea.Key… cases in App.key, and keymap_test.go
// derives Wake's keymap from it. Every key still works; only the drawing is
// gone. **Do not add a key to App.key without a legendEntries entry** - the
// bijection fails without one even though the entry is no longer drawn.
//
// internal/core's airlock_test.go names *this* file for the word `interrupt`,
// which is the legend's own English rather than a wire subtype.

import "github.com/charmbracelet/lipgloss"

// hintIndentWidth is the column the cue line is inset by, and so the column the
// cue itself does not get to use.
const hintIndentWidth = 1

// legendEntry is one key: the glyph, and what pressing it does.
type legendEntry struct{ glyph, what string }

// legendEntries names every key this build binds, and only what it binds.
//
// It is the canonical list the bijection guard reads from both sides -
// TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed - so a glyph here
// with no App.key case, and an App.key case with no glyph here, are both build
// failures. The order once decided what a narrow pane kept when the whole
// legend was drawn; now it only decides the order the armed cue's own parts
// truncate in (↵ before ⌃O), because a cue is the only thing drawn from this.
//
// ⌃⇧A is deliberately absent: spec §6 gives the next-agent jump that chord and
// bubbletea v1.3.10 names no ctrl+shift+<letter> at all. ⇧⇥ carries it instead.
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
	// ⌃G workspaces is gone with the left sidebar it toggled, hidden for now;
	// restored with the multi-groupchat version. See groups.go and keys.go.
	{"⌃R", "activity"},
	{"⇞⇟", "scroll"},
	{"⌃E", "expand"},
	{"⌃F", "fork"},
	{"⌃Q", "quit & park all"},
	{"⌃T", "mention mode"},
}

const (
	// escGlyph is how the legend spells the key. A constant because armedLabel
	// matches on it to swap the label.
	escGlyph = "esc"

	// escInterruptLabel is what ⎋ ordinarily does, escClearLabel is what the
	// armed second press does with a draft, and escRewindLabel is what it does
	// on an idle, empty conversation - open the rewind picker. Only the last two
	// are ever drawn, in the cue. See escape.go and rewind.go.
	escInterruptLabel = "interrupt"
	escClearLabel     = "clear draft"
	escRewindLabel    = "rewind"

	// The two glyphs a live ⌃O arm re-labels, and what it calls them. Both,
	// because either alone would be a cue that lies: armed, ↵ no longer sends and
	// ⌃O no longer detaches - it takes the arm back. See detach.go.
	sendGlyph        = "↵"
	sendLabel        = "send"
	armedSendLabel   = "detach"
	detachGlyph      = "⌃O"
	detachLabel      = "detach"
	armedDetachLabel = "cancel"
)

// hintSep separates the parts of the armed cue - a detach names two keys, so
// the cue is more than one part.
const hintSep = "   "

// legendArms is which of App's arms a cue should name, carried as one value
// rather than a bool per arm: a second parameter per arm is how the swap ends
// up spelled differently in the two places that render it.
//
// At most one is ever set - arming any of them goes through App.disarmed first,
// and esc/rewind are themselves split from the same escArmed bit by
// rewindArmable (armsFor), which is mutually exclusive with clearsOnEscape by
// construction. See escape.go and rewind.go.
type legendArms struct{ esc, rewind, detach bool }

// armedLabel is the label an arm gives one entry, and whether the arm touches
// it at all. The one place the swap is spelled, so the cue and any guard over
// it cannot drift: detach swaps two glyphs (↵ and ⌃O), esc and rewind one each,
// and an untouched entry reports its ordinary label and false.
func armedLabel(e legendEntry, arms legendArms) (string, bool) {
	switch {
	case arms.esc && e.glyph == escGlyph:
		return escClearLabel, true
	case arms.rewind && e.glyph == escGlyph:
		return escRewindLabel, true
	case arms.detach && e.glyph == sendGlyph:
		return armedSendLabel, true
	case arms.detach && e.glyph == detachGlyph:
		return armedDetachLabel, true
	}
	return e.what, false
}

// armedCueParts is the cue an arm draws: only the entries it swaps, each spelled
// `glyph label`, in legendEntries order. Empty for no arm, which is when the
// composer draws no legend row at all.
func armedCueParts(arms legendArms) []string {
	if arms == (legendArms{}) {
		return nil
	}
	var parts []string
	for _, e := range legendEntries {
		if label, armed := armedLabel(e, arms); armed {
			parts = append(parts, e.glyph+" "+label)
		}
	}
	return parts
}

// armedCue is the cue fitted to a width, joined with hintSep and cut at a part
// boundary - a whole part is dropped from the end rather than a fragment left,
// because half of `↵ detach` is worse than nothing. Empty when not even the
// first part fits, and empty for no arm.
func armedCue(arms legendArms, width int) string {
	line := ""
	for i, part := range armedCueParts(arms) {
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
