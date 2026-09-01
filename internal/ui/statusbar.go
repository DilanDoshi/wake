package ui

// The line under the composer: where this session is, what it is running as,
// and how much room it has left. Drawn inside the composer, below the box - and
// above the armed cue on the rare frame one is up, so the info row still sits
// over the keys when there are any.
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

	"github.com/charmbracelet/lipgloss"
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

	// modeFormat is how the permission mode is spelled here. It used to be shared
	// with the legend's tail; the legend no longer draws a mode, so the bar is
	// its only home now.
	modeFormat = "permissions: %s"
)

const (
	// dmBarRows is how many rows the conversation and room bars may use. The
	// overflow wraps onto a second when one row is too narrow (the owner's
	// report: `permissions: auto` alone, the model and context gone) rather than
	// dropping the facts. chromeHeight counts the bar's actual height, so a
	// second row costs a row of transcript rather than scrolling the alt screen.
	dmBarRows = 2

	// tileBarRows keeps the board's per-agent tile to one row: its tiles have a
	// fixed height, so a wrapped bar there would grow the grid.
	tileBarRows = 1
)

// drawStatusBar is the seam the bar is rendered through, so a test can count
// how often it actually runs. Reach it through this, never by calling
// statusBar directly: drawing the bar reads the filesystem, and a direct call
// is invisible to the counter that keeps that off the draw loop. It fixes the
// row budget at dmBarRows, the conversation and room surfaces it serves.
var drawStatusBar = func(a Agent, mode string, width int) string {
	return statusBar(a, mode, width, dmBarRows)
}

// barKey is everything statusBar reads. A value type so "has anything changed"
// is one comparison rather than a list somebody has to keep in step.
//
// The identity colour is deliberately absent: the bar recedes in the muted grey
// every bar wears and does not take the hue (see statusBar), so a /color change
// moves nothing here and belongs in no key that would redraw for it.
type barKey struct {
	width     int
	dir       string
	model     string
	confModel string
	effort    string
	mode      string
	state     string
	used      int
	window    int
	prs       *prSet // a PR arrives mid-turn with no other bar fact moving, so the key must carry it; prSet.same keeps the pointer stable so it does not redraw per frame
}

// withBar re-renders the status bar if anything it is drawn from has moved, and
// returns the receiver untouched otherwise.
//
// The mode comes off this pane's own composer, which App.dmFor has already set
// from App.modeOf - the same value the legend names, so the two lines of one
// pane cannot disagree about it. It is part of the key because the bar is drawn
// per change: a mode left out of it would be the one fact here that goes stale.
func (d DM) withBar(width int) DM {
	mode := d.composer.Mode()
	key := barKey{
		width: width, dir: d.Agent.Cwd, model: d.Agent.Model, confModel: d.Agent.ConfirmedModel,
		effort: d.Agent.Effort, mode: mode, state: d.Agent.State,
		used: d.Agent.ContextTokens, window: d.Agent.ContextWindow, prs: d.Agent.prs,
	}
	if key == d.barFrom {
		return d
	}
	d.bar, d.barFrom = drawStatusBar(d.Agent, mode, width), key
	return d
}

// statusBar draws the bar for one conversation, bounded to width and to rows
// lines, or "" when nothing about the session is known yet.
//
// Row 1 is the historical single-row layout unchanged - the facts joined, the
// mode appended after dropping facts rightmost-first to fit it whole - so a
// caller passing rows == 1 (the board tile) gets exactly what it always did.
// Above that, the facts row 1 could not hold wrap onto further rows rather than
// being dropped: the owner's report was a narrow pane showing `permissions:
// auto` alone with the model and context gone. The mode only ever rides row 1,
// so a wrapped bar never puts `permissions: …` on a line of its own.
func statusBar(a Agent, mode string, width, rows int) string {
	if width < 1 || rows < 1 {
		return ""
	}
	// The confirmed model wins: it is the name a /model probe read back, which
	// stays current through a runtime /model where the init frame's id does not.
	// The init id is the fallback until the probe answers - see modelName.
	model := a.ConfirmedModel
	if model == "" {
		model = modelName(a.Model, a.ContextWindow)
	}
	segments := []string{
		shortPath(a.Cwd),
		gitref.Of(a.Cwd).Name(),
		model,
		contextLeft(a.ContextTokens, a.ContextWindow),
		effortSegment(a.Effort),
		prSegment(a.prs),
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
	// Flatten each fact first, because the only row break here is the deliberate
	// wrap: a newline in a model name or a directory - facts that are not Wake's,
	// so hostile ones must be assumed - would otherwise draw a row chromeHeight
	// did not budget and scroll the alt screen. oneRow over the joined line did
	// this before, and wrapping makes it per-segment.
	for i := range kept {
		kept[i] = oneRow(kept[i])
	}
	modeSeg := oneRow(permissionSegment(mode))

	// Row 1: the facts joined, then the mode appended after dropping facts
	// rightmost-first to fit it whole. The mode is drawn whole or not at all -
	// a cut landing inside it leaves `permissions: …`, a label naming a value
	// nobody can read - and it is the one fact that moves under a keystroke, so
	// a row that could not show it would not be a status bar (docs/notes/bugs.md
	// BUG-1, BUG-8). The path is last to go: it answers which of thirty panes
	// this is.
	row1 := kept
	line := strings.Join(row1, statusSep)
	if modeSeg != "" {
		for {
			if ansi.StringWidth(line+statusSep+modeSeg) <= width {
				line += statusSep + modeSeg
				break
			}
			if len(row1) <= 1 {
				break
			}
			row1 = row1[:len(row1)-1]
			line = strings.Join(row1, statusSep)
		}
	}
	out := []string{ansi.Truncate(line, width, ellipsis)}

	// The facts row 1 dropped flow onto further rows, rightmost order preserved,
	// up to the budget. A single fact wider than a whole row takes one alone and
	// is truncated - the same right-cut row 1 gives an over-long path.
	for overflow := kept[len(row1):]; len(out) < rows && len(overflow) > 0; {
		row, rest := takeRow(overflow, width)
		out = append(out, ansi.Truncate(row, width, ellipsis))
		overflow = rest
	}

	// The bar recedes in the muted grey every bar wears, and it does not take the
	// agent's /color hue: it is the least urgent thing on screen and the one that
	// is about the session rather than the turn, so it stays chrome. The identity
	// hue - including the manager's yellow default (identityStyleFor) - is where
	// the operator's eye rests instead: the room name-tag, the roster row, and
	// the composer they type into (speakerStyle, headStyle, Composer.boxStyle).
	return HintStyle.Render(strings.Join(out, "\n"))
}

// takeRow greedily packs whole segments into one row of at most width cells,
// returning the row and the segments that did not fit. A single segment wider
// than the whole row takes it alone (the caller truncates it), so the row
// always consumes at least one segment and a pack loop always makes progress.
func takeRow(segs []string, width int) (row string, rest []string) {
	i := 0
	for i < len(segs) {
		cand := segs[i]
		if row != "" {
			cand = row + statusSep + segs[i]
		}
		if ansi.StringWidth(cand) <= width {
			row, i = cand, i+1
			continue
		}
		if row == "" {
			return segs[i], segs[i+1:]
		}
		break
	}
	return row, segs[i:]
}

// barRows is how many rows a rendered bar occupies, which is what chromeHeight
// must budget: zero when empty, else its real height - one or, since wrapping,
// up to dmBarRows. Both the DM and the room count the bar this way, so the
// sizing and the draw read the same stored string and cannot disagree.
func barRows(bar string) int {
	if bar == "" {
		return 0
	}
	return lipgloss.Height(bar)
}

// permissionSegment is the mode this pane's session is in. The bar is the only
// surface that draws it now - the legend's always-on hints, the mode among
// them, moved here whole.
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

// prSegment names the pull requests this session has opened - `PR #29` for one,
// `PR #29, #30` for several - and "" when it has opened none, dropped like every
// other segment. The daemon scrapes the numbers from a `gh pr create` tool
// result; nothing on Claude's wire names a PR. See prSet.
func prSegment(p *prSet) string {
	nums := p.numbers()
	if len(nums) == 0 {
		return ""
	}
	parts := make([]string, len(nums))
	for i, n := range nums {
		parts[i] = fmt.Sprintf("#%d", n)
	}
	return "PR " + strings.Join(parts, ", ")
}
