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
	mu        sync.Mutex
	renderers = map[int]*glamour.TermRenderer{}

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

// Markdown renders source markdown at the given width. Renderers are cached
// per width: constructing one parses a full style definition, and the render
// loop calls this on every frame.
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
	return strings.TrimRight(trimOpeningScaffold(fitToWidth(stylingOnly(out), width)), "\n")
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

// rendererFor returns the renderer cached for width, building one on first
// use. Only the map lookup and the style parse happen under mu; the terminal
// probe is resolved before the lock is taken.
func rendererFor(width int) (*glamour.TermRenderer, error) {
	width = boundedWidth(width)
	style := resolvedStyle() // must precede mu.Lock: this can block on the TTY

	mu.Lock()
	defer mu.Unlock()
	if r, ok := renderers[width]; ok {
		return r, nil
	}
	r, err := newRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	renderers[width] = r
	return r, nil
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
