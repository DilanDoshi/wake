package render

import (
	"errors"
	"fmt"
	gansi "github.com/charmbracelet/glamour/ansi"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
)

// ansiPattern matches SGR escape sequences so assertions can look at the text
// a user would read rather than the bytes a terminal consumes.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// --- markdown -------------------------------------------------------------

// maxLineBytes is a deliberately loose ceiling. Glamour paints every padding
// cell with its own escape pair, so a styled 40-column line measures ~600
// bytes and an 80-column one ~1250. Only an unwrapped render trips this.
const maxLineBytes = 2000

func TestMarkdownRendersAndWraps(t *testing.T) {
	out := Markdown("# Title\n\nSome **bold** text.", 40)
	if out == "" {
		t.Fatal("empty render")
	}
	if strings.Contains(out, "**") {
		t.Error("markdown syntax leaked into output")
	}
	for _, line := range strings.Split(out, "\n") {
		if len(line) > maxLineBytes { // generous: ANSI codes inflate byte length
			t.Errorf("line far exceeds width: %q", line)
		}
	}
}

// squash removes every space and newline, so an assertion can ask whether the
// characters survived without caring where the wrap points landed.
func squash(s string) string {
	return strings.Join(strings.Fields(s), "")
}

// widestLine reports the display width of the widest line in a render, which
// is the number a joined pane sizes itself on.
func widestLine(s string) int {
	widest := 0
	for _, line := range strings.Split(s, "\n") {
		if w := ansi.StringWidth(line); w > widest {
			widest = w
		}
	}
	return widest
}

// markdownWidthCases are inputs with no break opportunity inside the run that
// has to fit. Word wrap is defined over break opportunities, so wrapping alone
// cannot bound any of these — which is exactly why the earlier version of this
// test, driven by Latin prose with spaces every few columns, passed against a
// Markdown that had no width bound at all.
//
// payload is the run of characters that must survive somewhere in the output.
var markdownWidthCases = []struct {
	name    string
	src     string
	payload string
}{
	{
		name:    "cjk without spaces",
		src:     strings.Repeat("日本語のテキストです", 30), // 600 display cells
		payload: strings.Repeat("日本語のテキストです", 30),
	},
	{
		name:    "unbreakable token",
		src:     strings.Repeat("x", 200),
		payload: strings.Repeat("x", 200),
	},
	{
		name:    "fenced code block",
		src:     "```\n" + strings.Repeat("y", 200) + "\n```",
		payload: strings.Repeat("y", 200),
	},
	{
		name:    "long url",
		src:     "See https://example.com/" + strings.Repeat("segment/", 30) + "end",
		payload: "https://example.com/" + strings.Repeat("segment/", 30) + "end",
	},
	{
		name:    "heading of one long word",
		src:     "# " + strings.Repeat("H", 120),
		payload: strings.Repeat("H", 120),
	},
	{
		name:    "latin prose", // the control: this one word-wraps on its own
		src:     "A paragraph that keeps going so it certainly has to wrap more than once.",
		payload: "paragraph",
	},
}

// TestMarkdownBoundsEveryLineToRequestedWidth asserts the property this
// function's name has always claimed: what the reader sees fits the pane it was
// rendered for. Measured in display cells, because an ideograph is one rune and
// two columns, and because glamour paints padding with escape sequences that
// are bytes but not cells.
//
// What it defends: the DM cuts an over-wide line with no ellipsis and no
// horizontal scroll, and lipgloss.JoinHorizontal sizes a joined pane on its
// widest line — so one unbounded line either eats the message or shoves every
// neighbouring column out of the grid.
func TestMarkdownBoundsEveryLineToRequestedWidth(t *testing.T) {
	for _, c := range markdownWidthCases {
		t.Run(c.name, func(t *testing.T) {
			for _, width := range []int{20, 40, 60, 80} {
				for i, line := range strings.Split(Markdown(c.src, width), "\n") {
					if got := ansi.StringWidth(line); got > width {
						t.Errorf("width %d: line %d is %d cells wide: %q",
							width, i, got, stripANSI(line))
					}
				}
			}
		})
	}
}

// TestMarkdownKeepsEveryCharacterWhileBounding is the other half of the bound.
// Truncating would satisfy the width assertion above and still lose the
// message, which is the failure this fix exists to end: §8 makes the DM the
// fidelity view, so markdown wraps rather than cuts. ToolCall and ToolResult
// cut instead, and say so — they are deliberately collapsed summaries with an
// expanded mode behind them; assistant prose has neither.
func TestMarkdownKeepsEveryCharacterWhileBounding(t *testing.T) {
	for _, c := range markdownWidthCases {
		t.Run(c.name, func(t *testing.T) {
			for _, width := range []int{20, 40, 80} {
				got := squash(stripANSI(Markdown(c.src, width)))
				if !strings.Contains(got, c.payload) {
					t.Errorf("width %d: bounding lost content; want a run of %d chars %.20q… in\n%s",
						width, len(c.payload), c.payload, got)
				}
			}
		})
	}
}

// TestMarkdownBoundsToTheFloorBelowIt pins what a sub-floor width means.
// glamour cannot lay out a document narrower than minMarkdownWidth, and the UI
// asks for width zero before the terminal reports its size, so those widths
// render at the floor — and must be bounded there rather than left unbounded.
func TestMarkdownBoundsToTheFloorBelowIt(t *testing.T) {
	src := strings.Repeat("日本語のテキストです", 30)
	for _, width := range []int{-1, 0, 1, minMarkdownWidth - 1} {
		if got := widestLine(Markdown(src, width)); got > minMarkdownWidth {
			t.Errorf("width %d: widest line is %d cells, want <= the %d-cell floor",
				width, got, minMarkdownWidth)
		}
	}
}

// TestMarkdownKeepsAnsiSequencesIntactWhileBounding is the markdown-side
// counterpart of the tool-result guard: a break landing inside an escape
// sequence bleeds colour across the rest of the Bubble Tea frame.
func TestMarkdownKeepsAnsiSequencesIntactWhileBounding(t *testing.T) {
	out := Markdown("**"+strings.Repeat("b", 300)+"**", 40)
	if strings.Contains(stripANSI(out), "\x1b") {
		t.Errorf("bounding severed an escape sequence: %q", out)
	}
}

// TestFitToWidthLeavesFittingLinesByteIdentical is what makes the bound safe to
// apply to every render: glamour already lays out margins, indents and table
// columns correctly for everything it can wrap, so the bound must be provably
// invisible on that path and intervene only where glamour's own accounting
// failed.
func TestFitToWidthLeavesFittingLinesByteIdentical(t *testing.T) {
	for _, width := range []int{20, 40, 80} {
		rendered := Markdown("# Title\n\n- alpha\n- beta\n\nSome **bold** prose here.\n", width)
		if got := fitToWidth(rendered, width); got != rendered {
			t.Errorf("width %d: fitToWidth altered a render that already fit:\n%q\nwant\n%q",
				width, got, rendered)
		}
	}
}

func TestFitToWidthHardWrapsOnlyTheOverflowingLine(t *testing.T) {
	in := "short\n" + strings.Repeat("z", 25) + "\nalso short"
	got := fitToWidth(in, 10)
	want := "short\nzzzzzzzzzz\nzzzzzzzzzz\nzzzzz\nalso short"
	if got != want {
		t.Errorf("fitToWidth =\n%q\nwant\n%q", got, want)
	}
}

// TestFitToWidthIsIdentityBelowOneColumn keeps the degenerate width from
// looping or emptying the output; Markdown clamps to the floor long before
// this, and this pins the primitive's own edge.
func TestFitToWidthIsIdentityBelowOneColumn(t *testing.T) {
	for _, width := range []int{0, -1} {
		if got := fitToWidth("abc", width); got != "abc" {
			t.Errorf("fitToWidth(%q, %d) = %q, want it unchanged", "abc", width, got)
		}
	}
}

func TestMarkdownEmptyInputIsEmpty(t *testing.T) {
	if got := Markdown("", 40); strings.TrimSpace(got) != "" {
		t.Errorf("Markdown(\"\") = %q, want empty", got)
	}
}

func TestMarkdownWhitespaceOnlyInputIsEmpty(t *testing.T) {
	for _, src := range []string{" ", "\n\n", "\t \n"} {
		if got := Markdown(src, 40); got != "" {
			t.Errorf("Markdown(%q) = %q, want empty", src, got)
		}
	}
}

func TestMarkdownKeepsProseAndCodeContent(t *testing.T) {
	out := stripANSI(Markdown("A list:\n\n- alpha\n- beta\n\n```go\nfmt.Println(\"hi\")\n```\n", 60))
	for _, want := range []string{"A list:", "alpha", "beta", "fmt.Println"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered markdown lost %q:\n%s", want, out)
		}
	}
}

// TestMarkdownStylesEmphasisRatherThanEchoingIt guards the reason this package
// selects a glamour style itself instead of using WithAutoStyle: auto falls
// back to a plain-text style when stdout is not a terminal, which prints
// **bold** literally.
func TestMarkdownStylesEmphasisRatherThanEchoingIt(t *testing.T) {
	out := Markdown("Some **bold** text.", 40)
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("render carries no ANSI styling: %q", out)
	}
	if !strings.Contains(stripANSI(out), "bold") {
		t.Errorf("render lost the emphasized word: %q", out)
	}
}

func TestMarkdownDoesNotEndWithBlankLines(t *testing.T) {
	out := Markdown("paragraph", 40)
	if strings.HasSuffix(out, "\n") {
		t.Errorf("render has trailing newline: %q", out)
	}
}

// The pair of the test above, and the one that was missing. Glamour opens a
// document with its own newline and then the first block opens with another,
// laid out *inside* the two-column margin - so a reply whose first block is a
// list, a quote, a fence or a table comes back led by a row of spaces. A
// newline is trimmed by every caller that joins blocks; a row of spaces is
// not, so it reaches the screen as a blank row between an attribution and the
// text it introduces. Recorded as docs/notes/bugs.md BUG-2.
func TestMarkdownDoesNotBeginWithBlankLines(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"paragraph", "paragraph"},
		{"list", "- alpha\n- beta"},
		{"ordered list", "1. one\n2. two"},
		{"quote", "> quoted"},
		{"fence", "```\ncode\n```"},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |"},
		{"heading", "# Heading\n\ntext"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Markdown(tc.src, 40)
			first := strings.SplitN(out, "\n", 2)[0]
			if strings.TrimSpace(stripANSI(first)) == "" {
				t.Errorf("render opens with a blank row %q:\n%s", first, out)
			}
		})
	}
}

// The other half of the rule above, and the one that decides how it is written:
// a blank line the *model* wrote is not scaffolding and may not be trimmed.
//
// A fenced block whose first or last line is blank renders as a row of spaces
// indistinguishable from glamour's own block prefix, so a trim that loops while
// the row is blank deletes it. Counted against the same source without the blank
// line, which is exact and needs no assumption about how many rows the renderer
// opens with.
func TestMarkdownKeepsBlankLinesTheModelWrote(t *testing.T) {
	for _, tc := range []struct{ name, plain, spaced string }{
		{"a fence opening on a blank line", "```\ncode\n```", "```\n\ncode\n```"},
		{"a fence closing on one", "```\ncode\n```", "```\ncode\n\n```"},
		{"two of them", "```\ncode\n```", "```\n\n\ncode\n```"},
		{"a fence after a paragraph", "text\n\n```\ncode\n```", "text\n\n```\ncode\n\n```"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plain := strings.Count(Markdown(tc.plain, 40), "\n")
			spaced := strings.Count(Markdown(tc.spaced, 40), "\n")
			want := plain + strings.Count(tc.spaced, "\n") - strings.Count(tc.plain, "\n")
			if spaced != want {
				t.Errorf("%d rows for %q against %d for %q, want %d - a written blank line was trimmed",
					spaced+1, tc.spaced, plain+1, tc.plain, want+1)
			}
		})
	}
}

func TestRendererIsCachedPerWidth(t *testing.T) {
	a, err := rendererFor(41)
	if err != nil {
		t.Fatalf("rendererFor(41): %v", err)
	}
	b, err := rendererFor(41)
	if err != nil {
		t.Fatalf("rendererFor(41) again: %v", err)
	}
	if a != b {
		t.Error("renderer for the same width was rebuilt instead of reused")
	}
	c, err := rendererFor(42)
	if err != nil {
		t.Fatalf("rendererFor(42): %v", err)
	}
	if a == c {
		t.Error("different widths must not share a renderer")
	}
}

func TestRendererClampsUnusablyNarrowWidths(t *testing.T) {
	floor, err := rendererFor(minMarkdownWidth)
	if err != nil {
		t.Fatalf("rendererFor(%d): %v", minMarkdownWidth, err)
	}
	for _, width := range []int{-1, 0, minMarkdownWidth - 1} {
		got, err := rendererFor(width)
		if err != nil {
			t.Fatalf("rendererFor(%d): %v", width, err)
		}
		if got != floor {
			t.Errorf("rendererFor(%d) built its own renderer; want the %d-wide floor", width, minMarkdownWidth)
		}
	}
}

func TestMarkdownIsSafeForConcurrentUse(t *testing.T) {
	const goroutines = 8
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			width := 40 + (i % 2)
			if out := Markdown("# Heading\n\nbody text", width); out == "" {
				t.Errorf("concurrent Markdown at width %d returned empty", width)
			}
		}(i)
	}
	wg.Wait()
}

// --- priming --------------------------------------------------------------

// unprime forces the package back to its cold state so a test can exercise the
// first, terminal-probing resolution rather than a cached answer.
func unprime(t *testing.T) {
	t.Helper()
	styleOnce = sync.Once{}
	mdStyle = gansi.StyleConfig{}
	t.Cleanup(Prime)
}

// TestRendererForResolvesStyleBeforeTakingTheLock is the regression test for
// the real hazard. Resolving the terminal background is a blocking read/write
// handshake with the TTY — termenv clears ECHO and ICANON, writes a background
// query plus a cursor-position report, and waits up to OSCTimeout (five
// seconds) for replies. Running that while holding mu would stall every
// session's render behind one unlucky first frame.
//
// The probe is reached through the detectStyle seam so this test can observe
// exactly when it runs: if mu is already held at that moment, TryLock fails.
func TestRendererForResolvesStyleBeforeTakingTheLock(t *testing.T) {
	unprime(t)

	original := detectStyle
	t.Cleanup(func() { detectStyle = original })

	heldDuringProbe := make(chan bool, 1)
	detectStyle = func() gansi.StyleConfig {
		if mu.TryLock() {
			mu.Unlock()
			heldDuringProbe <- false
		} else {
			heldDuringProbe <- true
		}
		return original()
	}

	if _, err := rendererFor(37); err != nil {
		t.Fatalf("rendererFor(37): %v", err)
	}
	select {
	case held := <-heldDuringProbe:
		if held {
			t.Error("the terminal probe ran while mu was held; it must be resolved before the lock")
		}
	default:
		t.Fatal("the style was never resolved")
	}
}

// TestPrimeDoesNotTakeTheRenderMutex keeps Prime off the lock entirely, so a
// later change that primes renderers as well as the style cannot reintroduce a
// blocking probe inside mu.
func TestPrimeDoesNotTakeTheRenderMutex(t *testing.T) {
	unprime(t)

	mu.Lock()
	defer mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		Prime()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Prime blocked on the render mutex: style resolution must happen off the lock")
	}
}

func TestPrimeIsIdempotent(t *testing.T) {
	Prime()
	first := resolvedStyle()
	Prime()
	if second := resolvedStyle(); !reflect.DeepEqual(second, first) {
		t.Error("the style changed across Prime calls")
	}
	// It is one of Claude's two, and not glamour's. The check is on a field the
	// stock styles set and this one deliberately does not: a coloured heading is
	// exactly what WithStandardStyle was drawing.
	if first.H2.Color != nil {
		t.Errorf("the resolved style colours an H2 (%q); Claude Code renders headings bold and "+
			"uncoloured, and a tinted one is glamour's stock theme showing through", *first.H2.Color)
	}
	if !reflect.DeepEqual(first, claudeStyle(true)) && !reflect.DeepEqual(first, claudeStyle(false)) {
		t.Error("the resolved style is neither of claudeStyle's two")
	}
}

func TestPrimeIsSafeForConcurrentUse(t *testing.T) {
	unprime(t)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Prime()
		}()
	}
	wg.Wait()

	if reflect.DeepEqual(resolvedStyle(), gansi.StyleConfig{}) {
		t.Error("Prime left the style unresolved")
	}
}

// --- diffs ----------------------------------------------------------------

func TestDiffMarksAddedAndRemovedLines(t *testing.T) {
	out := Diff("a\nb\nc\n", "a\nB\nc\n", 60)
	if !strings.Contains(out, "-") || !strings.Contains(out, "+") {
		t.Errorf("diff lacks +/- markers:\n%s", out)
	}
	if !strings.Contains(out, "B") {
		t.Errorf("diff lost the added line:\n%s", out)
	}
}

func TestDiffOnIdenticalInputIsEmpty(t *testing.T) {
	if got := Diff("same\n", "same\n", 60); strings.TrimSpace(got) != "" {
		t.Errorf("Diff of identical input = %q, want empty", got)
	}
}

// diffText is a diff's content without its band: ANSI stripped and the
// right-hand padding that makes each line a rectangle trimmed off. The padding
// is asserted by TestDiffLinesAreFullWidthBands, so the content tests below do
// not restate it on every line.
func diffText(s string) string {
	lines := strings.Split(stripANSI(s), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

func TestDiffTrimsCommonPrefixAndSuffix(t *testing.T) {
	got := diffText(Diff("a\nb\nc\n", "a\nB\nc\n", 60))
	want := "  - b\n  + B"
	if got != want {
		t.Errorf("Diff =\n%q\nwant\n%q", got, want)
	}
}

func TestDiffPureAddition(t *testing.T) {
	got := diffText(Diff("", "x\ny\n", 60))
	want := "  + x\n  + y"
	if got != want {
		t.Errorf("Diff of an empty original =\n%q\nwant\n%q", got, want)
	}
}

func TestDiffPureDeletion(t *testing.T) {
	got := diffText(Diff("x\ny\n", "", 60))
	want := "  - x\n  - y"
	if got != want {
		t.Errorf("Diff to an empty result =\n%q\nwant\n%q", got, want)
	}
}

func TestDiffAppendKeepsExistingLines(t *testing.T) {
	got := diffText(Diff("a\nb\n", "a\nb\nc\n", 60))
	want := "  + c"
	if got != want {
		t.Errorf("Diff of an append =\n%q\nwant\n%q", got, want)
	}
}

// Claude Code draws a changed line as a band: the ground is the signal, so it
// has to reach the right-hand edge. A line that stops at its text reads as
// coloured text instead, which is what Wake drew before.
func TestDiffLinesAreFullWidthBands(t *testing.T) {
	for _, width := range []int{20, 40, 60} {
		out := Diff("alpha\nbeta\n", "alpha\nbetter\n", width)
		for _, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w != width {
				t.Errorf("width %d: band line is %d cells, want exactly %d: %q",
					width, w, width, stripANSI(line))
			}
		}
	}
}

// The band and the word ground are two nearby greens, so this forces the
// 24-bit profile specifically: at 4-bit both would quantise to the same code
// and every assertion below would pass against a diff that highlights nothing.
// `go test` is not a terminal, so without a forced profile lipgloss correctly
// emits no escapes at all.
func forceTrueColour(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// Which words get the brighter ground. Asserted on the spans rather than on
// the escapes around them, because the run that matters is the *text* - the
// styling is one lookup away and TestDiffPaintsTwoGroundsPerPairedLine covers
// that it happens at all.
func TestEmphasiseMarksOnlyTheChangedRun(t *testing.T) {
	for _, tc := range []struct {
		name, line, against string
		want                []span
	}{
		{
			name: "one word replaced mid-line",
			line: "the quick red fox", against: "the quick brown fox",
			want: []span{{text: "the quick "}, {text: "red", emph: true}, {text: " fox"}},
		},
		{
			name: "a suffix appended",
			line: "alpha beta", against: "alpha",
			want: []span{{text: "alpha"}, {text: " beta", emph: true}},
		},
		{
			name: "no counterpart is one flat span",
			line: "a brand new line", against: "",
			want: []span{{text: "a brand new line"}},
		},
		{
			name: "nothing in common is one flat span",
			line: "wholly different", against: "entirely other",
			want: []span{{text: "wholly different"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := emphasise(tc.line, tc.against)
			if len(got) != len(tc.want) {
				t.Fatalf("emphasise(%q, %q) = %v, want %v", tc.line, tc.against, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("span %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// The two grounds actually reach the output: a paired line paints the band and
// the changed word differently.
func TestDiffPaintsTwoGroundsPerPairedLine(t *testing.T) {
	forceTrueColour(t)
	added := strings.Split(Diff("the quick brown fox\n", "the quick red fox\n", 60), "\n")[1]
	if grounds := distinctBackgrounds(added); grounds < 2 {
		t.Errorf("added line uses %d backgrounds, want the band plus a word ground:\n%q", grounds, added)
	}
}

// A line with no counterpart on the other side has nothing to compare against,
// so it is one flat band rather than an all-over highlight.
func TestDiffWithoutACounterpartIsOneFlatBand(t *testing.T) {
	forceTrueColour(t)
	out := Diff("", "a brand new line\n", 60)
	if got := distinctBackgrounds(out); got != 1 {
		t.Errorf("unpaired added line uses %d backgrounds, want 1:\n%q", got, out)
	}
}

// distinctBackgrounds counts the different 24-bit background SGR sequences in s.
func distinctBackgrounds(s string) int {
	seen := map[string]bool{}
	for _, m := range ansiPattern.FindAllString(s, -1) {
		if strings.Contains(m, "48;2;") {
			seen[m] = true
		}
	}
	return len(seen)
}

func TestDiffIgnoresTrailingNewlineDifference(t *testing.T) {
	if got := Diff("a\nb", "a\nb\n", 60); strings.TrimSpace(got) != "" {
		t.Errorf("Diff differing only by a trailing newline = %q, want empty", got)
	}
}

// A line too wide for the pane wraps rather than ending in an ellipsis, and
// every row keeps the marker so it still says which side of the change it is.
// An edit whose point sits past column 80 used to be unreadable.
func TestAWideDiffLineWrapsWithItsMarkerRepeated(t *testing.T) {
	const width = 20
	got := stripANSI(Diff("", strings.Repeat("z", 100)+"\n", width))
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("a 100-column line did not wrap at width %d: %q", width, got)
	}
	if strings.Contains(got, "…") {
		t.Errorf("a wrapped diff line was cut as well:\n%s", got)
	}
	zs := 0
	for i, line := range lines {
		if !strings.HasPrefix(line, diffAddMarker) {
			t.Errorf("row %d lost its marker, so it no longer says which side it is: %q", i, line)
		}
		if w := ansi.StringWidth(line); w != width {
			t.Errorf("row %d is %d cells, want exactly %d: %q", i, w, width, line)
		}
		zs += strings.Count(line, "z")
	}
	if zs != 100 {
		t.Errorf("wrapping kept %d of 100 characters", zs)
	}
}

// TestDiffFitsWideRunesToDisplayWidth is the diff-side counterpart: diff lines
// are raw file content, so wide runes are ordinary there too.
func TestDiffFitsWideRunesToDisplayWidth(t *testing.T) {
	for _, width := range []int{10, 20, 40} {
		out := Diff("", strings.Repeat("世界", 60)+"\n", width)
		for _, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Errorf("width %d: diff line is %d cells wide: %q", width, w, stripANSI(line))
			}
		}
	}
}

// --- degradation ----------------------------------------------------------

// breakTheRenderer makes every renderer this package builds fail, and puts
// the package back afterwards.
//
// The cache has to be emptied as well as the style broken: renderers are keyed
// by width and one built by an earlier test would answer before the broken
// style ever got asked for, which would leave these tests passing against the
// good path.
func breakTheRenderer(t *testing.T) {
	t.Helper()

	unprime(t)
	originalNew := newRenderer
	t.Cleanup(func() { newRenderer = originalNew })
	newRenderer = func(...glamour.TermRendererOption) (*glamour.TermRenderer, error) {
		return nil, errors.New("no such style")
	}

	mu.Lock()
	kept := renderers
	renderers = map[int]*glamour.TermRenderer{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		renderers = kept
		mu.Unlock()
	})

	// Undone before unprime's own Prime runs, so the rest of the package does
	// not inherit a style that cannot be built.
	t.Cleanup(func() {
		styleOnce = sync.Once{}
		mdStyle = gansi.StyleConfig{}
	})

	notice.Reset()
	t.Cleanup(notice.Reset)
}

// Both fallbacks used to be silent, which CLAUDE.md's log-and-skip rule
// forbids: a persistent failure renders every message in the fleet as raw
// markdown, forever, with nothing anywhere saying so.
func TestARenderThatDegradedSaysSo(t *testing.T) {
	breakTheRenderer(t)

	out := Markdown("**still readable**", 40)

	if !strings.Contains(out, "still readable") {
		t.Errorf("the message was lost on the degraded path: %q", out)
	}
	n, ok := notice.Latest()
	if !ok {
		t.Fatal("a render that fell back to plain text reported nothing")
	}
	if !strings.Contains(n.Text, "plain text") {
		t.Errorf("notice = %q, want it to say the render degraded", n.Text)
	}
}

// The other half of why this is not a log call: at 30 sessions a draw loop
// failing on every frame is thousands of identical lines a second.
func TestARepeatedDegradationIsOneNotice(t *testing.T) {
	breakTheRenderer(t)

	for range 100 {
		_ = Markdown("**still readable**", 40)
	}

	n, ok := notice.Latest()
	if !ok {
		t.Fatal("a render that fell back to plain text reported nothing")
	}
	if n.Count != 100 {
		t.Errorf("Count = %d after 100 failed renders, want 100", n.Count)
	}
	if got := notice.Count(n.Text); got != 100 {
		t.Errorf("the sink holds %d reports of %q, want one entry counting 100", got, n.Text)
	}
}

// A render that works must not report anything: a notice row that is always
// lit says nothing at all.
func TestAWorkingRenderReportsNothing(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	_ = Markdown("**ordinary prose**", 40)

	if n, ok := notice.Latest(); ok {
		t.Errorf("a successful render reported %q", n.Text)
	}
}

// The cap decides what is *rendered*, not what is kept.
//
// A review found this: Diff word-diffed, styled and padded every changed line
// and the caller then threw nearly all of it away, so two edits at opposite ends
// of a 50,000-line file built a hundred thousand styled rows inside the draw
// loop before anything bounded them.
func TestAnEnormousDiffIsBoundedBeforeItIsRendered(t *testing.T) {
	old := strings.Repeat("x\n", 50_000)
	nw := "CHANGED\n" + strings.Repeat("x\n", 49_998) + "ALSO\n"

	out := Diff(old, nw, 80)
	if got := strings.Count(out, "\n") + 1; got > MaxDiffLines+1 {
		t.Errorf("rendered %d lines, cap is %d", got, MaxDiffLines)
	}
	if !strings.Contains(stripANSI(out), "lines not shown") {
		t.Errorf("nothing said what was cut:\n%s", stripANSI(out))
	}
	// Both polarities survive, or an over-long diff is a wall of one colour.
	plain := stripANSI(out)
	if !strings.Contains(plain, "  - ") || !strings.Contains(plain, "  + ") {
		t.Errorf("a capped diff kept only one polarity:\n%s", plain)
	}
}

// An ordinary refactor still renders whole - the cap must not be what a reader
// meets on a forty-line rewrite.
func TestAnOrdinaryRefactorIsNotCappedByRender(t *testing.T) {
	var oldB, newB strings.Builder
	for i := range 40 {
		fmt.Fprintf(&oldB, "old line %d\n", i)
		fmt.Fprintf(&newB, "new line %d\n", i)
	}
	if out := stripANSI(Diff(oldB.String(), newB.String(), 60)); strings.Contains(out, "lines not shown") {
		t.Errorf("a forty-line rewrite was capped:\n%s", out)
	}
}

// Tabs are expanded before anything measures a line. ansi.StringWidth counts a
// tab as zero and a terminal does not, so a band padded to fill 20 drew 24 and
// wrapped - and source is tab-indented far more often than not.
func TestATabbedDiffLineStaysInsideItsBand(t *testing.T) {
	for _, width := range []int{20, 40, 80} {
		out := Diff("", "\tfunc main() {\n\t\treturn\n", width)
		for _, line := range strings.Split(out, "\n") {
			if w := ansi.StringWidth(line); w != width {
				t.Errorf("width %d: a tabbed band is %d cells: %q", width, w, stripANSI(line))
			}
			if strings.Contains(line, "\t") {
				t.Errorf("width %d: a tab survived into the output: %q", width, stripANSI(line))
			}
		}
	}
}

// A character reference is printable, so core.Contained passes it through - and
// markdown says entity references decode, so glamour hands back the control
// character the model spelled out. Measured: `&#27;]52;c;…&#7;` produced a live
// OSC 52 that set the operator's clipboard. The source fence cannot see this
// one; the renderer's output fence is where it stops.
func TestAnEntityCannotSmuggleAnEscapeThroughTheRenderer(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"decimal", "before&#27;]52;c;cHduZWQ=&#7;after"},
		{"hexadecimal", "before&#x1b;[2J&#x1b;[Hafter"},
		{"a C1 introducer", "a&#155;2Jb"},
		{"named", "a&Tab;b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := Markdown(tc.src, 60)
			// stripANSI is SGR-only, which is exactly the question here: what
			// is left after removing what glamour emits is what nothing in
			// this package should have produced.
			for _, r := range stripANSI(out) {
				if r == '\n' || r == '\t' {
					continue
				}
				if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
					t.Errorf("a character reference reached the render as %#x: %q", r, out)
				}
			}
			if !strings.Contains(stripANSI(out), "a") && !strings.Contains(stripANSI(out), "before") {
				t.Errorf("the fence ate the message: %q", out)
			}
		})
	}
}

// Wake's own colour has to survive all of that, or the fence has taken the
// renderer with it.
func TestStylingSurvivesTheOutputFence(t *testing.T) {
	out := Markdown("Some **bold** text and `code`.", 40)
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("the output fence took glamour's own styling: %q", out)
	}
	if !strings.Contains(stripANSI(out), "bold") {
		t.Errorf("the styled word is gone: %q", out)
	}
}
