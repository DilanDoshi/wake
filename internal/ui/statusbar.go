package ui

// The line under the composer: where this session is, what it is running as,
// and how much room it has left. Drawn above the legend rather than below it,
// so the info row sits over the keys - what this session is, then what a key
// would do.
//
//	~/Documents/wake  feat/fidelity  Sonnet 5 (1M context)  ctx:74%  effort:xhigh  permissions: auto
//
// The permission mode is the newest segment and it arrived as a repair:
// docs/notes/bugs.md BUG-1 records ⇧⇥ moving the belief with no surface a
// conversation pane draws saying so. Its only home was the legend's tail, which
// is the first entry a narrow pane cuts and is withheld from a blurred composer
// outright - so an operator watching one pane could cycle the mode and see
// nothing move. It sits here because the two lines answer different questions:
// the legend says what a key would do, and the bar says what this session is.
// That is also why the legend's blurred-withholding does not transfer - a
// conversation has exactly one agent, so there is no second one for a mode in
// this pane to be confused with.
//
// It is last, so it is the segment a narrow pane truncates first. That is the
// legend's own ordering read once more: the path is what this line is most read
// for.
//
// Effort is here now, and it was not for a long time. No frame Claude sends
// unasked carries the level - "effort" appears in the recorded corpus only as
// an entry in init.slash_commands, never as a value - so the bar could once
// only have repeated what Wake asked for, with nothing able to contradict it.
// What changed is the probe: the daemon sends a bare /model, whose reply names
// the level (`Current model: … (effort: xhigh)`), reads it back, and carries it
// on the report as a confirmed fact. So the segment shows the confirmed level,
// or the asked-for one until the probe answers, or nothing when Wake chose none
// and no probe has returned. See internal/daemon/effort.go and the airlock's
// EffortFromModelReply.
//
// Every segment is dropped rather than guessed when its fact is unknown, and
// the whole bar is dropped when none of them are - a row of separators with
// nothing between them is worse than no row.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/gitref"
)

const (
	// statusSep is the gap between segments. Two spaces rather than a glyph:
	// this line is read at a glance and a separator is one more thing on it.
	statusSep = "  "

	// ctxLabel prefixes the remaining-context percentage.
	ctxLabel = "ctx:"

	// effortLabel prefixes the reasoning level, the way ctxLabel prefixes the
	// context figure - so a bare level like `xhigh` is not read as a model name.
	effortLabel = "effort:"

	// homeGlyph replaces the operator's home directory in a path.
	homeGlyph = "~"
)

// statusBar draws the bar for one conversation, bounded to width, or "" when
// nothing about the session is known yet.
func statusBar(a Agent, mode string, width int) string {
	if width < 1 {
		return ""
	}
	segments := []string{
		shortPath(a.Cwd),
		gitref.Of(a.Cwd).Name(),
		modelName(a.Model, a.ContextWindow),
		contextLeft(a.ContextTokens, a.ContextWindow),
		effortSegment(a.Effort),
	}
	kept := segments[:0]
	for _, s := range segments {
		if s != "" {
			kept = append(kept, s)
		}
	}
	// The mode is added after the drop rather than among the segments, because
	// it is the one fact Wake always has an answer for and so would keep the
	// whole row alive on its own. That costs a row of transcript in a pane
	// already at its floor - minDMHeight is composer plus one line of
	// conversation - for a session nothing else is known about. Nothing real
	// lands there: a fleet report carries the spawn directory from the moment a
	// session exists (daemon.agent.runningIn), so a conversation's bar has a
	// path before its agent has said a word, and the mode is drawn beside it.
	if len(kept) == 0 {
		return ""
	}
	// One row, and this is the guard rather than the styling. chromeHeight
	// budgets exactly one row whenever this is non-empty, so a segment carrying
	// a newline draws two and the pane is a row taller than it was given -
	// which scrolls the alt screen away on every frame at the ticker's rate.
	// The facts here are not all Wake's: the model is whatever a claude process
	// reported, and a directory may legally contain a newline.
	// The mode is drawn whole or not at all, which is hintFitting's ruling one
	// line down: this bar is a plain right-cut, and a cut landing inside the
	// last segment leaves `permissions: …`, a label announcing a value nobody
	// can read. The facts above it are still cut, because half a path is still
	// a path and half a mode is not a mode.
	//
	// **What it is not any more is the first thing dropped.** Appending it only
	// when the whole finished row fit meant it was never drawn at a realistic
	// width: a home-relative path, a feature branch and a model name fill the
	// row on their own, so the segment reached the operator who asked for it
	// exactly never (docs/notes/bugs.md BUG-8, and BUG-1 before it - the fix
	// that was proved against a short path nobody works in).
	//
	// So the facts in front of it give way instead, rightmost first: how full
	// the context is, then which model, then which branch. That order is the
	// priority statement. **The mode is the only segment here that moves under
	// a keystroke**, and a row that cannot show the thing the operator just
	// pressed a key to change is not a status bar. The path stays longest
	// because it is the answer to "which of these thirty panes is this".
	//
	// oneRow here as well as at App.notedMode: this function is total over its
	// arguments, and the width measurement is only right for one row.
	seg := oneRow(permissionSegment(mode))
	line := oneRow(strings.Join(kept, statusSep))
	if seg != "" {
		for {
			if ansi.StringWidth(line+statusSep+seg) <= width {
				line += statusSep + seg
				break
			}
			// Down to the path and it still will not fit: the mode is dropped
			// rather than the path, because a bar naming neither where you are
			// nor anything else is a row spent on a separator.
			if len(kept) <= 1 {
				break
			}
			kept = kept[:len(kept)-1]
			line = oneRow(strings.Join(kept, statusSep))
		}
	}
	// The bar recedes in the muted grey every bar wears, and it does not take the
	// agent's /color hue: it is the least urgent thing on screen and the one that
	// is about the session rather than the turn, so it stays chrome. The identity
	// hue - including the manager's yellow default (identityStyleFor) - is where
	// the operator's eye rests instead: the room name-tag, the roster row, and
	// the composer they type into (speakerStyle, headStyle, Composer.boxStyle).
	return HintStyle.Render(ansi.Truncate(line, width, ellipsis))
}

// permissionSegment is the mode this pane's session is in, spelled the way the
// legend spells it - one format constant, because two spellings of one fact on
// two lines of the same pane is a difference a reader would go looking for.
//
// Dropped rather than guessed when the caller has none, which is every other
// segment's rule. Nothing *drawn* reaches here empty - a pane goes through
// App.dmFor, and App.modeOf falls back to the spawn mode, so a session that has
// said nothing yet still names the mode Wake started it in. A pane being
// re-sized off the stored map can, and the drop is what makes that harmless.
func permissionSegment(mode string) string {
	if mode == "" {
		return ""
	}
	return fmt.Sprintf(modeFormat, mode)
}

// oneRow flattens anything that would open a second line. Control characters
// become spaces rather than being dropped, so a value stays as legible as it
// can be while still occupying one row.
func oneRow(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
}

// shortPath is a directory with the home directory replaced by ~. Absolute
// paths are what the daemon carries, and at 15-30 sessions the interesting half
// is always the tail.
func shortPath(dir string) string {
	if dir == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return dir
	}
	if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
		if rel == "." {
			return homeGlyph
		}
		return filepath.Join(homeGlyph, rel)
	}
	return dir
}

// modelDisplay maps Claude's model ids to the names Claude Code shows. A model
// this build has not been told about is drawn as the id it arrived as, which is
// still true and still useful - never as nothing, and never as a guess at a
// prettier name.
var modelDisplay = map[string]string{
	"claude-opus-5":     "Opus 5",
	"claude-sonnet-5":   "Sonnet 5",
	"claude-fable-5":    "Fable 5",
	"claude-haiku-4-5":  "Haiku 4.5",
	"claude-haiku-4.5":  "Haiku 4.5",
	"claude-3-5-haiku":  "Haiku 3.5",
	"claude-3-7-sonnet": "Sonnet 3.7",
}

// largeContext is the window above which Claude Code names the context in the
// model's own label, the way the screenshot's "Opus 5 (1M context)" does.
const largeContext = 1_000_000

// modelName is what the bar calls the model, with the long-context note Claude
// adds when the window is one.
func modelName(id string, window int) string {
	if id == "" {
		return ""
	}
	name, ok := modelDisplay[id]
	if !ok {
		name = id
	}
	if window >= largeContext {
		return fmt.Sprintf("%s (1M context)", name)
	}
	return name
}

// contextLeft is how much of the window is still free, as a percentage. Empty
// when either half is unknown: a percentage of an unknown window is not a
// smaller claim than a wrong one, it is the same claim.
//
// The division is on what is *left*, not on what is used, so the rounding is
// toward empty: one token short of a full window reads 0%, never 1%. Flooring
// the used half instead reports 100% free for a window that is 0.1% gone and
// 1% free for one that is 99.99% gone - wrong in the direction that matters at
// both ends. Clamped low because a window can be exceeded before a compaction
// lands.
func contextLeft(used, window int) string {
	if window <= 0 || used <= 0 {
		return ""
	}
	left := max((window-used)*100/window, 0)
	return ctxLabel + fmt.Sprintf("%d%%", left)
}

// effortSegment is the reasoning level this session runs at: the one a bare
// /model probe read back, or the one Wake asked for until the probe answers.
// Empty when neither is known - a session Wake chose nothing for, whose probe
// has not returned - and dropped rather than guessed, like every segment here.
func effortSegment(level string) string {
	if level == "" {
		return ""
	}
	return effortLabel + level
}
