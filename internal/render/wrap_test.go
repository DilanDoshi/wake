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
// again over the document block, and upstream's first pass writes a breakpoint
// rune without counting it or checking that it fits. So the first pass hands
// the second an over-long line, and the second re-breaks it. See
// third_party/reflow/WAKE-PATCH.md.
//
// Stated as a property over a corpus rather than as golden output: the
// stranded word moves with the pane, so a fixed string would pin one width and
// miss every other. Against upstream reflow this reports 8,693 stranded lines;
// counting the breakpoint rune without also checking that it fits still leaves
// 254, which is why the patch does both.
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
