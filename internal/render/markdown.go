// Package render turns agent output into terminal text: markdown, tool-call
// headers, tool results, and diffs. It takes plain values — strings, maps,
// ints — and never imports internal/core, so it stays testable on its own.
//
// Call Prime once during startup, before handing the terminal to Bubble Tea.
// Both the markdown style and the diff colours depend on the terminal's
// background, and detecting that is a blocking handshake with the TTY rather
// than a cheap lookup.
package render

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	gansi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
)

// minMarkdownWidth is the narrowest width we will build a renderer for. A
// width of zero disables word wrap in glamour, and the UI legitimately asks
// for width zero before the terminal reports its size.
const minMarkdownWidth = 20

var (
	mu sync.Mutex

	styleOnce sync.Once
	mdStyle   gansi.StyleConfig

	// detectStyle is the seam markdownStyle is reached through, so a test can
	// assert the terminal probe never runs while mu is held.
	detectStyle = markdownStyle

	// newRenderer is the seam a test breaks to reach the degraded path.
	//
	// It exists because the style stopped being a string. A bad *name* used to
	// be a way to make construction fail, and glamour.WithStyles takes a value
	// it cannot reject - so without a seam here the two degradation tests would
	// have been quietly asserting nothing against a renderer that always builds.
	newRenderer = glamour.NewTermRenderer
)

// Prime resolves the terminal-dependent state this package needs, so that no
// later render has to. It is safe to call from any number of goroutines and
// does nothing after the first call.
//
// Call it during startup, before tea.NewProgram. Resolving the background
// colour clears ECHO and ICANON on the TTY, writes a background query and a
// cursor-position report, then blocks reading both replies for up to
// termenv.OSCTimeout — five seconds. Once Bubble Tea owns stdin it parses
// cursor-position reports itself, so a probe issued after that point can lose
// its answer and wait out the whole timeout.
//
// One call covers both consumers: the markdown style and the AdaptiveColor
// diff palette resolve through the same cached lipgloss background detection.
func Prime() { _ = resolvedStyle() }

// Markdown renders source markdown at the given width, through a renderer
// cached for that width (renderercache.go).
//
// No line of the result is wider than width display cells, on every path
// including the degraded ones — see fitToWidth for why glamour's own word wrap
// is not enough to promise that. A width below minMarkdownWidth renders, and is
// bounded, at that floor.
//
// Unlike ToolCall and ToolResult, which cut to fit, this wraps: those two are
// deliberately collapsed summaries with an expanded form behind them, whereas
// markdown is the assistant's prose and the DM is the fidelity view. Nothing
// here is ever dropped to make it fit.
//
// A renderer failure is deliberately absorbed rather than propagated: the
// caller is a draw loop with nowhere to report to, and showing the raw source
// beats showing nothing. It is not, however, silent - see degraded.
func Markdown(src string, width int) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	width = boundedWidth(width)

	r, err := rendererFor(width)
	if err != nil {
		// Degrade to plain text rather than lose the message — but still
		// bounded, or the failure path breaks the promise the good path keeps.
		return degraded("building a markdown renderer failed", src, width, err)
	}
	out, err := lockAndRender(r, src)
	if err != nil {
		return degraded("rendering markdown failed", src, width, err)
	}
	// reflowProse re-wraps the prose glamour laid out, restoring the greedy word
	// wrap its paragraph pass loses without the muesli fork and hanging each list
	// item's continuation under its text; joinLoneBullets puts an item that opens
	// with a list back on its bullet's row, and fitToWidth is the hard width net
	// last of all, for the rows glamour could not wrap and reflowProse leaves alone;
	// sealRows closes a style either wrap broke a row inside.
	return strings.TrimRight(trimOpeningScaffold(sealRows(fitToWidth(joinLoneBullets(reflowProse(stylingOnly(out), width)), width))), "\n")
}

// boxDrawing marks a rendered line as glamour's own table or block-quote layout,
// which reflowProse must leave exactly as glamour drew it.
const boxDrawing = "│─┼┌┐└┘├┤┬┴╭╮╰╯"

// reflowProse re-wraps the prose glamour rendered, restoring the greedy word
// wrap glamour's paragraph pass loses. glamour wraps a paragraph twice — once
// through muesli/reflow/wordwrap, then again over the document block — and the
// first pass writes a breakpoint rune without checking it fits, so the second
// re-breaks the over-long line and strands the tail word (`--resume`, a date, a
// ticket id). Wake once carried a forked muesli via a go.mod `replace` to fix
// that first pass; the replace made `go install pkg@version` refuse the module,
// so the fix moved here instead — glamour uses upstream reflow and Wake re-wraps
// the prose wake-side. glamour still lays out every block — margins, lists,
// tables, block quotes, code — at the real width; this pass only re-wraps the
// lines those never produce: unstyled prose and list-item text sitting at the
// block margin. It runs before fitToWidth, the width net.
//
// Each maximal run of reflowable lines at one indent is one paragraph or one
// list item — broken at a new list marker — and is re-wrapped as a unit. The
// join mirrors what glamour's wrap consumed: a line broken at a hyphen kept the
// hyphen and took no space, so it rejoins with none; every other break took a
// space.
//
// An item's continuation hangs under its text, which glamour v1.0.0 lays at the
// list margin instead. An item is a group glamour drew a marker for (itemTag),
// and a row mid-group splits it only where startsItem says glamour started an
// item there. Inside an item a row glamour leads with a styled span's empty
// styling is the item's too: a code block in an item sits deeper, and any other
// block is a blank row away.
func reflowProse(s string, width int) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		if !reflowable(lines[i]) {
			out = append(out, lines[i])
			i++
			continue
		}
		lead := leadSpaces(lines[i])
		// Trimmed: a lone bullet's padding would read as the space after it.
		mark := itemMarker(strings.TrimRight(lines[i][lead:], " "), itemTag)
		group := []string{lines[i]}
		for i++; i < len(lines); i++ {
			row := lines[i]
			if mark != "" {
				row = unstyledLead(row)
			}
			if !reflowable(row) || leadSpaces(row) != lead || startsItem(group[0], row, itemTag) {
				break
			}
			group = append(group, row)
		}
		out = append(out, rewrapProse(group, lead, width, mark)...)
	}
	return strings.NewReplacer(itemTag, "", headingTag, "").Replace(strings.Join(out, "\n"))
}

// startsItem reports whether row, at the lead of the group first opened, starts
// an item rather than continuing the group. glamour opens a new list only after
// a blank row, so mid-group a row is an item only of first's own list - an
// enumerator after an enumerator, a bullet or a task box after a bullet or a task
// box. A wrapped
// row that happens to open `2. Then` continues a paragraph, or a bullet.
func startsItem(first, row, tag string) bool {
	a, b := markerOf(first, tag), markerOf(row, tag)
	return a != "" && b != "" && enumerator(a) == enumerator(b)
}

// enumerator reports whether a marker is an `N. ` one.
func enumerator(mark string) bool { return mark != "" && mark[0] >= '0' && mark[0] <= '9' }

// unstyledLead drops the escapes glamour leads a wrapped row with when a styled
// span opens it. They end in a reset, so they style nothing. A heading's row is
// left as it is: it is a block of its own (headingTag).
func unstyledLead(row string) string {
	if strings.HasPrefix(row, headingTag) {
		return row
	}
	cut := 0
	for n := sgrRun(row); n > 0; n = sgrRun(row[cut:]) {
		cut += n
	}
	if cut == 0 || !strings.HasSuffix(row[:cut], sgrReset) {
		return row
	}
	return row[cut:]
}

// reflowable reports whether a line is glamour-rendered prose this pass may
// re-wrap: it opens with the margin's spaces — code and block quotes open with an
// SGR escape, since glamour colours them — and holds no table or quote
// box-drawing.
//
// The `line[0] != ' '` guard is conservative on purpose: a paragraph
// *continuation* that begins with an inline-styled span (inline code, bold, a
// link) also opens with an SGR escape, so it is excluded too and left at
// glamour's wrap. That only forgoes fixing a strand in that one styled paragraph
// — never corrupts it — and it is what keeps a fenced code block (indistinguishable
// from styled prose once the leading SGR is stripped) safe from being re-wrapped.
// A list item takes such a row back, where position alone tells it from code
// (reflowProse).
func reflowable(line string) bool {
	if line == "" || line[0] != ' ' {
		return false
	}
	plain := ansi.Strip(line)
	if strings.TrimSpace(plain) == "" {
		return false // a blank row separates groups
	}
	return !strings.ContainsAny(plain, boxDrawing)
}

// leadSpaces is the count of leading space bytes, which for a reflowable line is
// its block indent (spaces are one byte each).
func leadSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// opensItem reports whether a line reads as the start of a list item, marker by
// marker as it is drawn: after reflowProse no tag is left to say which are real.
func opensItem(line string) bool { return markerOf(line, "") != "" }

// markerOf is the marker a row opens with at its lead, with tag as itemMarker
// takes it. A lone bullet counts - its padding reads as the space after it - and
// otherwise the row is trimmed of that padding, so a sentence-final number
// wrapped onto a row of its own (`3035.`) is not an `N. `.
func markerOf(row, tag string) string {
	lead := leadSpaces(row)
	if bulletMarker(row, lead) {
		return bullet
	}
	return itemMarker(strings.TrimRight(row[lead:], " "), tag)
}

// itemMarker is the list marker text opens with - a bullet, an `N. ` enumerator
// or a task box, the last two carrying tag the way withTag splices it - or "" for
// none. It reads raw text: a real marker is unstyled, so code reading `• ` is not.
func itemMarker(text, tag string) string {
	if strings.HasPrefix(text, bullet) {
		return bullet
	}
	for _, m := range []string{withTag(unticked, tag), withTag(ticked, tag)} {
		if strings.HasPrefix(text, m) {
			return m
		}
	}
	n := 0
	for n < len(text) && text[n] >= '0' && text[n] <= '9' {
		n++
	}
	if dot := withTag(". ", tag); n > 0 && strings.HasPrefix(text[n:], dot) {
		return text[:n+len(dot)]
	}
	return ""
}

// withTag is a marker with tag before its trailing space.
func withTag(marker, tag string) string { return strings.TrimSuffix(marker, " ") + tag + " " }

// rewrapProse re-wraps one paragraph or list item — the group shares an indent —
// greedily to the width glamour laid it out for, padding each result line to that
// budget. Budget is width less the indent and the far margin, which is the content
// width glamour itself wrapped to (bs.Width = width - indent - margin*2, with
// indent+margin the lead). An item's text after mark wraps at the budget less the
// marker, its continuations laid under the text, so the hang costs no row its width.
func rewrapProse(group []string, lead, width int, mark string) []string {
	budget := width - lead - int(defaultMargin)
	hang := ansi.StringWidth(mark)
	if budget-hang < 1 {
		mark, hang = "", 0
	}
	if budget < 1 {
		return group
	}
	var joined strings.Builder
	prev := "" // the row above's text, which a hyphen break is read from
	for k, line := range group {
		content := trimRightCells(line[lead:])
		text := ansi.Strip(content)
		if k > 0 && !hyphenJoin(prev, text) {
			joined.WriteByte(' ')
		}
		joined.WriteString(content)
		prev = text
	}
	first, rest := strings.Repeat(" ", lead)+mark, strings.Repeat(" ", lead+hang)
	var out []string
	// ansi.Wrap, not ansi.Wordwrap: Wrap checks the limit before it writes a
	// breakpoint rune, so a run of two (`--resume`) does not strand, which is the
	// exact defect the muesli fork existed to fix; Wordwrap shares the bug.
	for k, wl := range strings.Split(ansi.Wrap(joined.String()[len(mark):], budget-hang, ""), "\n") {
		if k > 0 {
			first = rest
		}
		out = append(out, first+padRight(wl, budget-hang))
	}
	return out
}

// trimRightCells drops a row's trailing spaces, including the ones glamour keeps
// inside a styled span at a wrap (`bold \x1b[0m`) or pads a styled row with. The
// escapes among them stay, in order, so no span is left open.
func trimRightCells(s string) string {
	var kept []string // gathered last first, so a quote row's escape per cell stays linear
	end := len(s)
	for end > 0 {
		if s[end-1] == ' ' {
			end--
			continue
		}
		at := strings.LastIndexByte(s[:end], 0x1b)
		if at < 0 || sgrRun(s[at:end]) != end-at {
			break
		}
		kept, end = append(kept, s[at:end]), at
	}
	slices.Reverse(kept)
	return s[:end] + strings.Join(kept, "")
}

// hyphenJoin reports whether next should abut prev with no space, because
// glamour's wrap broke inside a token at a hyphen (which consumes no space)
// rather than at a standalone dash (which does). glamour keeps the `-` on the
// line, so prev ends in `-` for both; the tells disambiguate. next beginning with
// `-` means the token continues across the break — `--resume` split as `-`/`-resume`
// — and next beginning with a digit means a hyphen-prefixed number — `-42`, or the
// tail of `DEV-3035` — so both abut with no space. Otherwise the rune before prev's
// trailing `-` decides: a non-space is a token break (`end-to-`, `--`), a space (or
// a lone `-`) is a standalone dash (`one - two`, rendered `one -`), which keeps it.
//
// This cannot be perfect: whether the source had a space *after* the hyphen is
// gone once glamour wraps, and a hyphen-prefixed token whose tail is a letter and
// whose head has a space before it (a rare `-v`-style flag, or a standalone dash
// before a number) is indistinguishable from a standalone dash on the fragments
// alone. Those land a spurious space only at the exact widths glamour splits on
// that hyphen; the common cases (spaced dashes, `--flags`, negative numbers,
// compound and dotted tokens) are exact.
func hyphenJoin(prev, next string) bool {
	if !strings.HasSuffix(prev, "-") || next == "" {
		return false
	}
	if next[0] == '-' || (next[0] >= '0' && next[0] <= '9') {
		return true
	}
	before := prev[:len(prev)-1]
	if before == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(before)
	return r != ' '
}

// padRight pads s with trailing spaces to width display cells, the trailing
// padding glamour lays every wrapped line out with.
func padRight(s string, width int) string {
	if n := width - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// stylingOnly keeps the escape sequences this renderer produced and neutralises
// every other control character in its output.
//
// It exists because containing the *source* is not enough, which was measured
// rather than reasoned: `&#27;]52;c;…&#7;` carries no control rune at all, so
// core.Contained passes it through untouched, and glamour decodes the character
// references on its way out - handing back a live OSC 52 that sets the
// operator's clipboard. Markdown says entity references decode, so this is the
// renderer working correctly and the fence being in the wrong place for it.
//
// The rule is exact rather than a guess: glamour emits **only** SGR. A document
// with a heading, bold, inline code, a link, a list, a fence, a table, a quote
// and a rule produced 162 escape sequences and not one control rune outside
// them. So a complete `ESC [ … m` run is kept and anything else is a space.
//
// Run before fitToWidth, because a neutralised escape stops being zero cells
// the moment it becomes a space, and the width has to be measured on what is
// actually drawn.
func stylingOnly(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if n := sgrRun(s[i:]); n > 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\n' || r == '\t' {
			b.WriteRune(r)
		} else if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			b.WriteByte(' ')
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// sgrRun is the length of a complete `ESC [ digits-and-semicolons m` at the
// start of s, or 0. Anything else beginning with ESC - an OSC, a CSI that ends
// in J or H, a bare escape - is not a run this renderer emits.
func sgrRun(s string) int {
	if len(s) < 3 || s[0] != 0x1b || s[1] != '[' {
		return 0
	}
	for i := 2; i < len(s); i++ {
		switch c := s[i]; {
		case c == 'm':
			return i + 1
		case (c >= '0' && c <= '9') || c == ';':
		default:
			return 0
		}
	}
	return 0
}

// trimOpeningScaffold drops the rows glamour opens a document with, and only
// those.
//
// There are two, and they are different shapes. The document's own BlockPrefix
// is a bare newline, so it arrives as a zero-length row; the first block's
// prefix is laid out *inside* the two-column margin, so it arrives as a row of
// spaces. The second is what reached the screen: a caller that joins blocks
// trims newlines and a row of spaces survives that untouched, so a reply whose
// first block was a list, a quote, a fence or a table gained a blank row under
// its attribution where a paragraph-first reply gained none - two replies from
// one agent spaced differently, on the surface a fleet is read by scanning
// (docs/notes/bugs.md BUG-2).
//
// **One row of each, rather than trimming while blank, and that bound is the
// whole correctness of this.** A fenced block whose first line the model left
// blank renders as a *second* row of spaces, and nothing about the row itself
// tells it apart from the prefix above it - so a loop deletes the model's own
// layout, which is the one thing this package may never do. One of each is what
// glamour emits; anything past that was written.
//
// The trailing edge needs none of this and gets none: glamour closes with bare
// newlines, which the TrimRight above has always removed, and a row of spaces at
// the end is therefore the model's.
func trimOpeningScaffold(s string) string {
	lines := strings.Split(s, "\n")
	cut := 0
	if cut < len(lines) && lines[cut] == "" {
		cut++
	}
	if cut < len(lines) && blankRow(lines[cut]) {
		cut++
	}
	return strings.Join(lines[cut:], "\n")
}

// blankRow is a row with nothing visible on it - spaces, or styling wrapped
// around neither. The styling matters: glamour's blank rows carry the block's
// colour, so a plain TrimSpace would keep one.
func blankRow(line string) bool { return strings.TrimSpace(ansi.Strip(line)) == "" }

// degraded returns the plain-text fallback and says so once.
//
// These two branches used to fall back in silence, which CLAUDE.md's
// log-and-skip rule forbids: a persistent style failure renders every message
// in the fleet as raw markdown, forever, with no signal anywhere. The obvious
// fix was wrong twice over - this package runs inside the TUI process, so
// writing to log corrupts the alt screen, and a draw loop failing every frame
// across 15-30 sessions makes an unconditional log a flood.
//
// internal/notice is the answer to both: it holds one entry per distinct
// message with a count, and it draws nothing itself - whoever owns the
// terminal decides where it appears. This package still knows nothing about a
// UI.
func degraded(what, src string, width int, err error) string {
	notice.Report("%s, showing plain text instead: %v", what, err)
	return fitToWidth(src, width)
}

// boundedWidth is the width a render is both laid out at and bounded to.
// glamour cannot lay out a document narrower than minMarkdownWidth — a width of
// zero disables its word wrap entirely — and the UI legitimately asks for zero
// before the terminal reports its size. One function so the width the renderer
// is built for and the width the output is measured against cannot drift.
func boundedWidth(width int) int {
	if width < minMarkdownWidth {
		return minMarkdownWidth
	}
	return width
}

// resolvedStyle returns the glamour style for this terminal, resolving it at
// most once. Callers must not hold mu: the first call can block on a terminal
// handshake, and mu serializes rendering for every session.
func resolvedStyle() gansi.StyleConfig {
	styleOnce.Do(func() { mdStyle = detectStyle() })
	return mdStyle
}

// markdownStyle is Claude Code's markdown rendering for this terminal.
//
// We deliberately do not use glamour.WithAutoStyle: it inspects os.Stdout and
// falls back to a plain-text style whenever stdout is not a terminal, which
// echoes raw markdown syntax (**bold**) instead of styling it. Wake renders
// into Bubble Tea's frame buffer rather than writing to stdout, so that probe
// asks the wrong question and makes output depend on how the process was
// launched. Background darkness is the question that actually matters, and
// lipgloss already answers it for the diff colours.
//
// The style itself is markdownstyle.go's. glamour's stock themes are not
// Claude's and were never close - a heading in ANSI 39, inline code in 203 on
// a grey block - which is what that file exists to end.
//
// This is the blocking call Prime exists to schedule. Reach it through
// resolvedStyle, never directly.
func markdownStyle() gansi.StyleConfig {
	return claudeStyle(lipgloss.HasDarkBackground())
}

// fitToWidth bounds every line of a render to width display cells, hard-wrapping
// the ones glamour left too wide.
//
// Why this is needed at all: glamour.WithWordWrap wraps at break opportunities
// and does nothing without them. It feeds muesli/reflow/wordwrap, which never
// breaks inside a word, and it is applied to paragraphs and headings only —
// fenced code is not wrapped at any width. So 600 cells of space-free Japanese,
// a 200-character token and a long URL all come back at their full width from a
// renderer built for eighty columns. glamour v1.0.0 has no hard-wrap option to
// prefer over this; WithWordWrap is its only wrapping knob.
//
// Why per line rather than one Hardwrap over the whole document: every line
// glamour did bound already carries its margins, indents and table columns laid
// out correctly, and re-flowing those would be a regression traded for nothing.
// A line that fits comes back byte-identical, so the intervention is confined to
// exactly the lines where glamour's own accounting failed.
//
// Why wrap rather than truncate: a bounded pane is a layout requirement, not a
// licence to drop the message. lipgloss.JoinHorizontal sizes a joined pane on
// its widest line, so an unbounded line shoves every neighbouring column out of
// the grid — but cutting to fit would silently lose the response instead, which
// is what the DM does today. Continuation lines land flush against the left edge
// rather than under glamour's two-column margin; that is the cost, and it is
// paid only on lines glamour could not lay out in the first place.
func fitToWidth(s string, width int) string {
	if width < 1 {
		return s
	}
	lines := strings.Split(s, "\n")
	fitted := make([]string, 0, len(lines))
	for _, line := range lines {
		if ansi.StringWidth(line) <= width {
			fitted = append(fitted, line)
			continue
		}
		fitted = append(fitted, strings.Split(ansi.Hardwrap(line, width, false), "\n")...)
	}
	return strings.Join(fitted, "\n")
}

// bulletMarker reports whether the rendered line opens a bullet item: the bullet
// must sit at the margin unstyled. glamour draws a real list marker with no
// colour, so the raw line carries the `• ` literally after its leading spaces;
// a `• ` inside a fenced block is painted with the code foreground, so an escape
// precedes it and this returns false — which is what keeps the pass off code.
func bulletMarker(raw string, lead int) bool {
	return strings.HasPrefix(raw, strings.Repeat(" ", lead)+bullet)
}

// sealRows closes a style still on at the end of a row, after its text, and
// reopens it after the next row's indent. A wrap inside a styled span - the
// reflow's, or fitToWidth's hard wrap - leaves the row ending with it on, and Wake
// draws a divider and another pane on the rest of that terminal row. It first
// parks each row's blanks past its last escape, so a span stops at its text.
func sealRows(s string) string {
	rows := strings.Split(s, "\n")
	open := ""
	for i, row := range rows {
		row = parkPadding(row)
		if open != "" {
			lead := leadSpaces(row)
			row = row[:lead] + open + row[lead:]
		}
		if open = styleOn(row); open != "" {
			end := len(strings.TrimRight(row, " "))
			row = row[:end] + sgrReset + row[end:]
		}
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

// sgrReset turns every style off.
const sgrReset = "\x1b[0m"

// styleOn is the styling a row leaves on at its end - its SGR runs since the
// last reset - or "" when it ends with none.
func styleOn(row string) string {
	var on strings.Builder
	for i := 0; i < len(row); {
		n := sgrRun(row[i:])
		if n == 0 {
			i++
			continue
		}
		if run := row[i : i+n]; run == sgrReset || run == "\x1b[m" {
			on.Reset()
		} else {
			on.WriteString(run)
		}
		i += n
	}
	return on.String()
}

// lockAndRender acquires mu and renders through the shared renderer. Callers
// must NOT already hold mu — it is a plain, non-reentrant mutex. The lock is
// required because a glamour TermRenderer carries mutable state across calls
// (ansi.BlockStack), so two goroutines rendering through one cached instance
// would race.
func lockAndRender(r *glamour.TermRenderer, src string) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	return r.Render(src)
}

// joinLoneBullets puts a list item's text back on its bullet's row. An item
// whose content opens with a list - `- 28. …` (how an agent keeps a document's
// numbering), `- - …` - drew its bullet alone, because glamour enters every list
// on a fresh line: right after an item's text, wrong when the list is the start
// of it. The row beneath, an item marker two columns in, moves up beside the
// bullet and keeps its column, so no row grows; its wrapped rest stays put.
// A painted block (code, a quote) leads with an escape, so it never matches; a
// table is the one unstyled block, and underTableRule keeps a centred header out.
func joinLoneBullets(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for i, line := range lines {
		if n := len(out); n > 0 {
			if col, ok := loneBulletAt(out[n-1]); ok && leadSpaces(line) == col+2 && opensItem(line) && !underTableRule(lines, i) {
				out[n-1] = strings.TrimRight(out[n-1], " ") + line[col+1:]
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// underTableRule reports whether row i has a table's rule beneath it - a table
// header, the only table row that can follow a lone bullet, whatever its columns.
func underTableRule(lines []string, i int) bool {
	return i+1 < len(lines) && strings.ContainsAny(ansi.Strip(lines[i+1]), boxDrawing)
}

// loneBulletAt is the column of a row's last bullet when the row holds nothing
// else - unstyled bullets and spaces, as glamour draws an item with no text of
// its own (or a chain of them this pass has already joined).
func loneBulletAt(line string) (int, bool) {
	t := strings.TrimRight(line, " ")
	mark := strings.TrimSpace(bullet)
	if !strings.HasSuffix(t, mark) || strings.Trim(t, " "+mark) != "" {
		return 0, false
	}
	return ansi.StringWidth(t) - 1, true
}

// parkPadding moves a row's trailing blanks past its last escape: muesli pads a
// row before it resets, so the blanks would otherwise be drawn in the span's style.
// A row whose trailing blank is painted keeps them: a diff fence's background on
// a line's own trailing spaces is the text's.
func parkPadding(row string) string {
	kept := trimRightCells(row)
	if len(kept) == len(row) || paintsTrailingBlank(row) {
		return row
	}
	return kept + strings.Repeat(" ", len(row)-len(kept))
}

// paintsTrailingBlank reports whether a blank after the row's last glyph is drawn
// with a background or in reverse video, the two styles a blank cell shows.
func paintsTrailingBlank(row string) bool {
	bg, reverse, painted := false, false, false
	for i := 0; i < len(row); {
		if n := sgrRun(row[i:]); n > 0 {
			bg, reverse = sgrPaint(row[i:i+n], bg, reverse)
			i += n
			continue
		}
		painted = row[i] == ' ' && (painted || bg || reverse)
		i++
	}
	return painted
}

// sgrPaint folds one SGR run into whether a blank is painted by a background or
// by reverse video. An extended colour's arguments (`38;5;45`) are skipped, never
// read as codes.
func sgrPaint(run string, bg, reverse bool) (bool, bool) {
	codes := strings.Split(run[len("\x1b["):len(run)-1], ";")
	for i := 0; i < len(codes); i++ {
		code, _ := strconv.Atoi(codes[i]) // "" is 0, as `ESC[m` is a reset
		switch {
		case code == 0:
			bg, reverse = false, false
		case code == 7 || code == 27:
			reverse = code == 7
		case code == 49:
			bg = false
		case code >= 40 && code <= 48 || code >= 100 && code <= 107:
			bg = true
		}
		if (code == 38 || code == 48 || code == 58) && i+1 < len(codes) {
			switch codes[i+1] {
			case "5":
				i += 2
			case "2":
				i += 4
			}
		}
	}
	return bg, reverse
}
