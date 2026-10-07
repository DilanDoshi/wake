package render

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// postingURL is hyphen-heavy on purpose: glamour breaks it at the hyphens, so
// the link wraps across rows at most widths.
const postingURL = "https://jobs.example.com/careers/senior-platform-engineer-remote-friendly-team-opening-2026"

// longerURL is long enough to take three rows (two wraps) at the narrow widths.
const longerURL = postingURL + "/apply-with-a-short-cover-letter-and-a-portfolio-of-recent-work-samples"

// styledPadShapes is every block a styled span can wrap inside, and the table,
// which never carried a styled pad.
var styledPadShapes = []struct{ name, src string }{
	{"list of links", "- [first listing, a long link text](" + postingURL + ")\n- [second listing](" + postingURL + ")"},
	{"paragraph with a bare url", "Read the posting at " + postingURL + " before the end of the week, then reply."},
	{"line opening with a url", postingURL + " is where the posting lives, so check it first."},
	{"line opening with a link", "[the posting, which is long](" + postingURL + ") is where it lives."},
	{"block quote", "> See " + postingURL + " for the full details of the listing."},
	{"heading", "# " + postingURL},
	{"strikethrough", "~~a long struck phrase about end-to-end rollouts that never ship~~ and words after it"},
	{"image", "![a long alternative text for the picture](" + postingURL + "/photo.png)"},
	{"list item bold", "- **a long bold phrase about hyphen-heavy end-to-end rollouts that wraps** and words after"},
	{"span wrapped twice", "Read " + longerURL + " today."},
	// glamour pads code rows inside a foreground-only span, invisible on a blank
	// cell; parkPadding moves those pads too.
	{"fenced code", "```\n" + strings.Repeat("a long line of code, hyphen-heavy end-to-end, ", 4) + "\nshort\n```"},
	{"table", "| name | link |\n|---|---|\n| first listing | " + postingURL + " |\n| second | none |"},
}

// TestAWrappedStyledSpanNeverStylesThePadding: glamour pads a wrapped row's blank
// cells before it closes the span the row ends in, so a link's underline, a
// heading, a quote or a strikethrough ran through the padding to the pane's
// right edge. A trailing blank is drawn with no style on.
func TestAWrappedStyledSpanNeverStylesThePadding(t *testing.T) {
	widest := 0 // past its longest line a shape wraps nowhere, so the sweep stops there
	for _, c := range styledPadShapes {
		for line := range strings.SplitSeq(c.src, "\n") {
			widest = max(widest, ansi.StringWidth(line))
		}
	}
	for _, c := range styledPadShapes {
		t.Run(c.name, func(t *testing.T) {
			for width := minMarkdownWidth; width <= widest; width++ {
				for _, row := range strings.Split(Markdown(c.src, width), "\n") {
					if at := styledTrailingBlank(row); at >= 0 {
						t.Fatalf("width %d: a trailing blank at byte %d is drawn in a style: %q", width, at, row)
					}
				}
			}
		})
	}
}

// styledTrailingBlank is the byte offset of the first blank after a row's last
// glyph that is drawn with a style on, or -1.
func styledTrailingBlank(row string) int {
	on, blank := false, -1
	for i := 0; i < len(row); {
		if n := sgrRun(row[i:]); n > 0 {
			on = row[i:i+n] != sgrReset && row[i:i+n] != "\x1b[m"
			i += n
			continue
		}
		switch {
		case row[i] != ' ':
			blank = -1
		case on && blank < 0:
			blank = i
		}
		i++
	}
	return blank
}

// TestParkPaddingKeepsTheRowsCells: moving the blanks past the row's last escape
// changes what they are drawn in, never how many cells the row takes.
func TestParkPaddingKeepsTheRowsCells(t *testing.T) {
	const underline = "\x1b[4m"
	for _, c := range []struct{ name, row, want string }{
		{"pad before the reset", "  see " + underline + "a-b" + "   " + sgrReset, "  see " + underline + "a-b" + sgrReset + "   "},
		{"pad after the reset", "  see " + underline + "a-b" + sgrReset + "   ", "  see " + underline + "a-b" + sgrReset + "   "},
		{"no escape", "  plain   ", "  plain   "},
		{"all blank", "     ", "     "},
		{"a background paints the blanks", "\x1b[48;5;237m-gone  " + sgrReset + "   ", "\x1b[48;5;237m-gone  " + sgrReset + "   "},
		{"a true-colour background", "\x1b[48;2;1;2;3mword  " + sgrReset, "\x1b[48;2;1;2;3mword  " + sgrReset},
		{"a basic background", "\x1b[44mword  " + sgrReset, "\x1b[44mword  " + sgrReset},
		{"reverse video", "\x1b[7mword  " + sgrReset, "\x1b[7mword  " + sgrReset},
		{"an indexed foreground reading like a background", "\x1b[38;5;45mword  " + sgrReset, "\x1b[38;5;45mword" + sgrReset + "  "},
		{"a true-colour foreground reading like one", "\x1b[38;2;48;5;100mword  " + sgrReset, "\x1b[38;2;48;5;100mword" + sgrReset + "  "},
		{"a background closed before the blanks", "\x1b[48;5;237mword\x1b[49m\x1b[4m  " + sgrReset, "\x1b[48;5;237mword\x1b[49m\x1b[4m" + sgrReset + "  "},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := parkPadding(c.row)
			if got != c.want {
				t.Errorf("parkPadding(%q) = %q, want %q", c.row, got, c.want)
			}
			if ansi.StringWidth(got) != ansi.StringWidth(c.row) {
				t.Errorf("the row is %d cells after, %d before", ansi.StringWidth(got), ansi.StringWidth(c.row))
			}
		})
	}
}

// TestADiffFenceKeepsItsOwnTrailingBlanks: chroma paints a diff line's
// background over the line's own trailing spaces, and a painted blank is text.
func TestADiffFenceKeepsItsOwnTrailingBlanks(t *testing.T) {
	rows := strings.Split(Markdown("```diff\n-removed  \n+added   \n```", 60), "\n")
	for _, want := range []string{"-removed  " + sgrReset, "+added   " + sgrReset} {
		if !slices.ContainsFunc(rows, func(row string) bool { return strings.Contains(row, want) }) {
			t.Errorf("no row keeps %q inside its span:\n%q", want, rows)
		}
	}
}
