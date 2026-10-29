package render

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// hyphenatedProse is the reply that exposed the wrap: an ordinary agent
// sentence whose three hyphenated tokens - a ticket id, a compound adjective
// and a slash command - are what made it come back a line too long.
const hyphenatedProse = "I'll work ticket DEV-3035 end-to-end following the " +
	"/lima-report-sync skill. First, let me load the Linear tools and " +
	"fetch the ticket."

// wrapWords builds the corpus. The `--flag` spellings are here deliberately:
// a run of two breakpoints is a case a single hyphen does not reach, and it
// was the one a first attempt at this fix left behind.
var wrapWords = []string{
	"wake", "agent", "the", "session", "park", "resume", "pass", "and",
	"DEV-3035", "end-to-end", "byte-identical", "--resume", "--fork-session",
	"--session-id", "read-only", "2026-08-22", "parent", "transcript",
	"stays", "first", "let", "me", "load", "tools", "ticket",
}

// content strips what the layout added back off a rendered line: the document
// margin on the left, the padding on the right. What is left is the text the
// wrap was deciding about.
//
// Sound for a top-level paragraph, which is all this file renders. A list or a
// block quote carries its own indent and glamour gives it a narrower budget
// than the one below, so pointing this at one would measure the gutter as
// prose and quietly compare against the wrong number.
func content(line string) string {
	return strings.TrimPrefix(strings.TrimRight(line, " "), strings.Repeat(" ", int(defaultMargin)))
}

// TestProseWrapsGreedily holds Markdown to the only wrap a reader expects: a
// word moves to the next line when it does not fit on this one, and never
// before.
//
// The failure it was written for is not a line that is too wide - every line
// was inside the pane - but a word stranded on a line of its own with room to
// spare above it:
//
//	I'll work ticket DEV-3035 ... skill. First,
//	let
//	me load the Linear tools and fetch the ticket.
//
// glamour wraps a paragraph twice, once through muesli/reflow/wordwrap and
// again over the document block, and glamour's first pass writes a breakpoint
// rune without counting it or checking that it fits. So the first pass hands
// the second an over-long line, and the second re-breaks it. Wake fixes this in
// render.reflowProse rather than in glamour: it re-wraps the prose glamour laid
// out with x/ansi.Wrap (which does check the limit before a breakpoint), so
// glamour can use upstream reflow and go install works — see reflowProse.
//
// Stated as a property over a corpus rather than as golden output: the
// stranded word moves with the pane, so a fixed string would pin one width and
// miss every other. Against unfixed glamour this reports thousands of stranded
// lines; the double-hyphen tokens in wrapWords (`--resume`, `--fork-session`)
// are the case a single-hyphen fix leaves behind, which is why reflowProse uses
// x/ansi.Wrap and not x/ansi.Wordwrap.
func TestProseWrapsGreedily(t *testing.T) {
	sources := []string{hyphenatedProse}
	r := rand.New(rand.NewSource(1))
	for range 100 {
		parts := make([]string, 6+r.Intn(18))
		for i := range parts {
			parts[i] = wrapWords[r.Intn(len(wrapWords))]
		}
		sources = append(sources, strings.Join(parts, " ")+".")
	}

	var stranded int
	for _, src := range sources {
		for width := minMarkdownWidth; width <= 120; width++ {
			// The budget the wrap is working to: glamour lays a document out
			// inside its margin on both sides.
			budget := width - 2*int(defaultMargin)
			var lines []string
			for _, line := range strings.Split(strings.Trim(Markdown(src, width), "\n"), "\n") {
				lines = append(lines, content(line))
			}
			for i := 0; i+1 < len(lines); i++ {
				here, next := lines[i], lines[i+1]
				if here == "" || next == "" {
					continue
				}
				// A break at a breakpoint keeps the rune on this line and
				// rejoins with no space - `linear-` and `ticket` are one
				// token. Charging a space there would measure a join that
				// does not exist and hide the case this test is about.
				join := 1
				if strings.HasSuffix(here, "-") {
					join = 0
				}
				word := strings.Fields(next)[0]
				if got := ansi.StringWidth(here) + join + ansi.StringWidth(word); got <= budget {
					stranded++
					if stranded <= 5 {
						t.Errorf("width %d: %q was wrapped early - %q would have fit after it "+
							"(%d of %d cells)", width, here, word, got, budget)
					}
				}
			}
		}
	}
	if stranded > 5 {
		t.Errorf("%d stranded lines in total (first five above)", stranded)
	}
}

// TestReflowKeepsSpacingFaithful is the fidelity guard the greedy-wrap test
// cannot be: TestProseWrapsGreedily's own join rule assumes a trailing `-` is a
// token break, so it cannot see a standalone dash glued to the next word. This
// checks the spacing reflowProse reconstructs when it rejoins glamour's wrapped
// fragments — the reviewer's two cases: a standalone spaced dash, and hyphen
// tokens (`--resume`, `--fork-session`) including next to inline styling.
//
// The properties are read per rendered line (a line break at a `-` is fine and
// expected; the defect is an inserted or dropped space *within* one line):
//   - a standalone dash (sources whose only `-` is a spaced ` - `) must be
//     followed by a space or the line end, never glued to a letter;
//   - a flag/token dash (sources with `--flag`) must never be followed by a
//     space, so `--resume` never renders `-- resume` or `--fork- session`.
func TestReflowKeepsSpacingFaithful(t *testing.T) {
	dashSources := []string{
		"alpha - beta and gamma - delta wrapping across several lines here now for sure absolutely indeed yes",
		"one - two - three - four - five - six - seven - eight - nine - ten - eleven - twelve - thirteen here",
	}
	flagSources := []string{
		"Run wake with --resume and --fork-session and the --session-id flag to bring the agent back now here",
		"Use the inline token and pass --resume plus --fork-session across a paragraph long enough to wrap now",
		"The **important** flags are --resume and --fork-session across a wrapping paragraph long enough here now",
		"Prefer `--session-id` and --add-dir over the defaults in a paragraph long enough to wrap at these widths",
		// Hyphen-prefixed numbers: no standalone dash here, so a `- ` is corruption.
		"Exit codes like -1 and -42 and offsets -7 and -128 across a paragraph long enough to wrap at these widths",
	}
	for _, src := range dashSources {
		for w := minMarkdownWidth; w <= 80; w++ {
			for _, line := range strings.Split(Markdown(src, w), "\n") {
				plain := strings.TrimRight(ansi.Strip(line), " ")
				for i := 0; i < len(plain)-1; i++ {
					if plain[i] == '-' && plain[i+1] != ' ' {
						t.Fatalf("w=%d standalone dash glued to a word: %q", w, plain)
					}
				}
			}
		}
	}
	for _, src := range flagSources {
		for w := minMarkdownWidth; w <= 80; w++ {
			for _, line := range strings.Split(Markdown(src, w), "\n") {
				plain := strings.TrimRight(ansi.Strip(line), " ")
				if strings.Contains(plain, "- ") {
					t.Fatalf("w=%d a flag/token hyphen gained a space: %q", w, plain)
				}
			}
		}
	}
}

// A wrap that opens a paragraph's row with a link's label is still prose, a
// grid's list above it or not: a label starts a row of its own only in the run a
// list's `[1]: ` opened, and only the next label in order. So the paragraph wraps
// exactly as it would with any other label there.
//
// Mutation check: splitting at any row a label opens keeps glamour's early break
// before `[1]:` at width 24; a list that runs on past its blank row takes `[3]`
// after `[2]`; and one that takes a label out of order takes `[1]` from the
// paragraph a list item puts straight under its list (Codex).
func TestAWrapOpeningARowWithALinkLabelStaysProse(t *testing.T) {
	para := "DEV-3035 --resume tools park byte-identical the first ticket [1]: --session-id parent pass end-to-end me."
	grid, _ := linkTable(2)
	for _, c := range []struct{ above, label string }{
		{"", "[1]"},
		{grid + "\n", "[3]"},
		{"- an item\n\n  " + strings.ReplaceAll(grid, "\n", "\n  ") + "\n  ", "[1]"}, // no blank row under its list
	} {
		ref := "\n\n[r]: https://example.com/r" // a reference keeps a grid in the document
		src := c.above + strings.Replace(para, "[1]", c.label, 1)
		opened := false
		for width := minMarkdownWidth; width <= 120; width++ {
			got := plainRows(Markdown(src+ref, width))
			want := plainRows(Markdown(c.above+strings.Replace(para, "[1]", "[a]", 1)+ref, width))
			for i, row := range got {
				opened = opened || strings.HasPrefix(content(row), c.label+": --")
				if len(got) != len(want) || row != strings.Replace(want[i], "[a]", c.label, 1) {
					t.Fatalf("width %d drew\n%s\nwant\n%s", width, strings.Join(got, "\n"), strings.Join(want, "\n"))
				}
			}
		}
		if !opened {
			t.Errorf("%q: no width opened a row with the label, so nothing was tested", src)
		}
	}
}
