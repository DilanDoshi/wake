package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The tables the Claude Code sweep used (2026-10-24-narrow-table-findings.md).
const (
	flagTable = "| Flag | Default | What it does |\n|:---|:---:|---:|\n" +
		"| `--effort` | medium | Sets how hard the model thinks before it answers, from low all the way up to max |\n" +
		"| --model | sonnet | Picks the model the session runs on |"
	crewTable = "| Name | Role | Status |\n|---|---|---|\n| atlas | builder | working |\n" +
		"| birch | reviewer | idle |\n| cedar | **manager** | blocked |"
	keyTable = "| Key | Value |\n|---|---|\n| a | word01 word02 word03 word04 word05 word06 word07 word08 word09 word10 |"
)

// boldLabel is a record's label: bold, ending in a colon. glamour's grid never
// bolds a header these tables leave plain.
var boldLabel = regexp.MustCompile("\x1b\\[1m[^\x1b]*:\x1b\\[0m")

// isGrid reports a render that drew its table as glamour's grid, not as records.
func isGrid(s string) bool { return !boldLabel.MatchString(s) }

// A table that fits draws exactly as it always has, wherever it sits: the table
// drawn alone in its place is byte for byte the document glamour drew whole.
//
// Mutation check: dropping the blank line written before the placeholder fails
// the case with a paragraph directly above the table, which the placeholder
// would otherwise continue.
func TestATableThatFitsDrawsAsItAlwaysHas(t *testing.T) {
	for _, src := range []string{
		flagTable,
		"Before.\n\n" + flagTable + "\n\nAfter.",
		"Before, with no blank line.\n" + flagTable + "\n\nAfter.",
		"- a list\n\n" + crewTable + "\n\n> a quote",
		"```\ncode\n```\n" + crewTable + "\n\n# A heading",
		crewTable + "\n\nBetween.\n\n" + flagTable,
		"| Link |\n|---|\n| [docs](https://example.com/debug) |",
	} {
		if got, want := Markdown(src, 120), rendered(src, 120); got != want {
			t.Errorf("%q drew\n%s\nwant\n%s", src, got, want)
		}
	}
}

// A table too narrow for its pane draws as records, the way Claude Code draws
// one: a bold `Header:` and the cell on one line per column, a value wrapping
// back to the margin, no blank row between fields, and a rule between records
// as wide as the text, at most 40.
func TestANarrowTableDrawsAsRecords(t *testing.T) {
	out := Markdown(flagTable, 40)
	want := []string{
		"  Flag: --effort",
		"  Default: medium",
		"  What it does: Sets how hard the",
		"  model thinks before it answers, from",
		"  low all the way up to max",
		"  " + strings.Repeat("─", 36),
		"  Flag: --model",
		"  Default: sonnet",
		"  What it does: Picks the model the",
		"  session runs on",
	}
	if got := plainRows(out); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("drew\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !strings.Contains(out, "\x1b[1mWhat it does:") {
		t.Errorf("the label is not bold:\n%q", out)
	}
}

// A table glamour reads straight off a paragraph's last line, with no blank line
// between, is laid out anew like any other: its stand-in is a paragraph of its own.
//
// Mutation check: without the blank line before the placeholder the paragraph
// swallows it and the whole source falls back to glamour's grid.
func TestATableUnderAParagraphLineStillDrawsAsRecords(t *testing.T) {
	got := plainRows(Markdown("Before.\n"+flagTable, 40))
	if len(got) < 3 || got[0] != "  Before." || got[1] != "" || got[2] != "  Flag: --effort" {
		t.Fatalf("drew\n%s", strings.Join(got, "\n"))
	}
}

// The rule between records is 40 cells, or the text's width when that is less.
func TestTheRecordRuleIsFortyCellsAtMost(t *testing.T) {
	for width, want := range map[int]int{30: 26, 44: 40, 60: 40} {
		src := "| A | B |\n|---|---|\n| x | " + strings.Repeat("y", 70) + " |\n| z | w |"
		var rule string
		for _, row := range plainRows(Markdown(src, width)) {
			if strings.HasPrefix(strings.TrimSpace(row), "─") {
				rule = strings.TrimSpace(row)
			}
		}
		if got := ansi.StringWidth(rule); got != want {
			t.Errorf("at %d the rule is %d cells, want %d", width, got, want)
		}
	}
}

// A grid row past four lines is Claude Code's switch: keyTable's one body row is
// as tall as glamour's grid draws it, so the grid stays exactly while that is
// four lines or fewer. Four is Claude Code's, read off its screen.
//
// Mutation check: maxRowLines at 5 keeps the grid at the widths where glamour's
// row takes five lines.
func TestATableBecomesRecordsWhenARowWouldPassFourLines(t *testing.T) {
	seen := map[bool]bool{}
	for width := 24; width <= 60; width++ {
		lines := bodyLines(rendered(keyTable, width))
		wantGrid := lines <= 4
		seen[wantGrid] = true
		if got := isGrid(Markdown(keyTable, width)); got != wantGrid {
			t.Errorf("at %d glamour's row is %d lines: grid %v, want %v", width, lines, got, wantGrid)
		}
	}
	if !seen[true] || !seen[false] {
		t.Fatalf("the sweep never crossed the switch: %v", seen)
	}
}

// bodyLines is how many lines a one-row grid's body takes: the rows after its
// header rule, up to the first blank one.
func bodyLines(grid string) int {
	n, body := 0, false
	for _, row := range plainRows(grid) {
		switch {
		case strings.Contains(row, "┼"):
			body = true
		case body && strings.TrimSpace(row) == "":
			return n
		case body:
			n++
		}
	}
	return n
}

// Every grid Wake keeps shows every word whole: where glamour would cut a header
// (`What it do…`), break a word or cut a cell (`workin`), the table is records.
// That is earlier than Claude Code, which breaks words in its own grid
// (decisions.md, 2026-10-24).
//
// Mutation check: a whole that keeps every grid fails here at the widths where
// glamour cuts crewTable's cells; one that skips the links fails where glamour
// cuts a long address in the list under its grid.
func TestAGridIsKeptOnlyWhileEveryWordIsWhole(t *testing.T) {
	for _, src := range []string{flagTable, crewTable, keyTable,
		"| Page |\n|---|\n| [docs](https://example.com/a/rather/long/path/to/the/page/about/debugging) |",
		"| `Flag` | **Kind** | Link |\n|---|---|---|\n| `--debug` |  | [docs](https://example.com/debug) |\n" +
			"| `--worktree` | path & name | none |"} {
		var words []string
		for _, row := range plainRows(rendered(src, 200)) {
			if !strings.Contains(row, "─") {
				words = append(words, strings.Fields(strings.ReplaceAll(row, "│", " "))...)
			}
		}
		records := false
		for width := minMarkdownWidth; width <= 120; width++ {
			out := Markdown(src, width)
			if !isGrid(out) {
				records = true
				continue
			}
			text := strings.Join(plainRows(out), "\n")
			for _, w := range words {
				if !strings.Contains(text, w) {
					t.Errorf("%.20q at %d keeps a grid without %q:\n%s", src, width, w, text)
				}
			}
		}
		if !records {
			t.Errorf("%.20q never drew as records", src)
		}
	}
}

// A record draws its cell's markdown as the grid would: bold stays bold, code
// and a link keep their styling, an escaped pipe is a pipe, and an empty cell
// keeps its label. The label is the header's text, whatever styled it.
func TestARecordKeepsItsCellsMarkdown(t *testing.T) {
	src := "| `Flag` | **Kind** | Note |\n|---|---|---|\n| `--debug` |  | a \\| b &amp; **c** [docs](https://e.io) |\n" +
		"| x | y | " + strings.Repeat("z", 60) + " |"
	out := Markdown(src, 50)
	want := []string{
		"  Flag: --debug",
		"  Kind:",
		"  Note: a | b & c docs https://e.io",
	}
	if got := plainRows(out); strings.Join(got[:3], "\n") != strings.Join(want, "\n") {
		t.Fatalf("drew\n%s\nwant first\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, styled := range []string{"\x1b[1mFlag:", "\x1b[1mKind:", "\x1b[1mc\x1b[0m"} {
		if !strings.Contains(out, styled) {
			t.Errorf("no %q in\n%q", styled, out)
		}
	}
}

// Only a table the document holds at its top level is laid out anew: one in a
// fence is text, and one inside a list item or a quote keeps glamour's grid
// (deferred.md).
func TestATableInAFenceAListOrAQuoteIsLeftToGlamour(t *testing.T) {
	narrow := "| A | B |\n|---|---|\n| x | " + strings.Repeat("y ", 40) + "|"
	for _, src := range []string{
		"```\n" + narrow + "\n```",
		"- an item\n\n  " + strings.ReplaceAll(narrow, "\n", "\n  "),
		"> " + strings.ReplaceAll(narrow, "\n", "\n> "),
	} {
		if got, want := Markdown(src, 30), rendered(src, 30); got != want {
			t.Errorf("%q drew\n%s\nwant\n%s", src, got, want)
		}
	}
}

// A source that already holds the placeholder's mark draws as glamour draws it,
// so a typed mark can never put a table in the wrong place.
func TestASourceHoldingThePlaceholderMarkIsLeftToGlamour(t *testing.T) {
	src := placeholder(0) + "\n\n" + crewTable
	if got, want := Markdown(src, 30), rendered(src, 30); got != want {
		t.Errorf("drew\n%s\nwant\n%s", got, want)
	}
}

// A resize flips the layout both ways: the decision is the width's alone.
func TestATableFlipsBackToItsGridWhenThePaneWidens(t *testing.T) {
	if !isGrid(Markdown(flagTable, 120)) || isGrid(Markdown(flagTable, 40)) || !isGrid(Markdown(flagTable, 120)) {
		t.Fatal("the table did not follow the width")
	}
}
