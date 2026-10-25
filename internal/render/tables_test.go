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
// So does every table glamour keeps: one whose reference link is defined outside
// it, and one whose rows hold no text at all, or end in a bare `|`.
//
// Mutation check: without the reference fence the reference-link table draws as
// records; a span one line short leaves the bare `|` row behind as a paragraph;
// without the guard for a table no cell anchors, `||` panics.
func TestATableThatFitsDrawsAsItAlwaysHas(t *testing.T) {
	for _, src := range []string{
		"| a | b |\n|---|---|\n| [x][r] | y |\n\n[r]: https://example.com/ref",
		"||\n|---|---|\n||",
		"| a | b |\n|---|---|\n| x | y |\n|",
		"| a | b |\n|---|---|\n| x | y |\n||\n\nAfter.",
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
			"| `--worktree` | path & name | none |",
		"| Supercalifragilisticexpialidocious | b |\n|---|---|\n| x | y |"} {
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

// A label is the header's text as the grid shows it, whatever punctuation it
// holds, and a pipe escaped inside a cell's code is a pipe.
//
// Mutation check: escaping every punctuation mark leaves a backslash before `/`,
// `'`, `?`, `%` and `:`; dropping the pipe's unescape draws `a \| b`.
func TestARecordsLabelIsItsHeadersText(t *testing.T) {
	heads := []string{"Pass/Fail", "Owner's", "Done?", "Coverage %", "Q&A", "a~b", "R&amp;D", "*x*", "C:\\\\"}
	src := "| " + strings.Join(heads, " | ") + " |\n|" + strings.Repeat("---|", len(heads)) + "\n| `a \\| b` |" +
		strings.Repeat(" "+strings.Repeat("z", 40)+" |", len(heads)-1)
	want := []string{"Pass/Fail: a | b", "Owner's:", "Done?:", "Coverage %:", "Q&A:", "a~b:", "R&D:", "x:", "C:\\:"}
	rows := strings.Join(plainRows(Markdown(src, 60)), "\n")
	for _, w := range want {
		if !strings.Contains(rows, "\n  "+w) && !strings.HasPrefix(rows, "  "+w) {
			t.Errorf("no row %q in\n%s", w, rows)
		}
	}
}

// A one-column grid keeps a row per body row: reflowProse used to run them
// together, `a.go b.go c.go`, since no `│` marks them as a grid's.
//
// Mutation check: without gridRows the rows run together, the grid no longer
// reads back as the table, and it draws as records.
func TestAOneColumnGridKeepsItsRows(t *testing.T) {
	src := "| Files |\n|---|\n| a.go |\n| b.go |\n| c.go |"
	got := plainRows(Markdown(src, 80))
	if !isGrid(Markdown(src, 80)) || len(got) != 5 || strings.TrimSpace(got[3]) != "b.go" {
		t.Fatalf("drew\n%s", strings.Join(got, "\n"))
	}
}

// A grid Wake cannot read back is glamour's to draw, as before; one holding a
// cell no pane can hold is records.
func TestAGridWakeCannotReadIsGlamoursAndAnUnholdableCellIsRecords(t *testing.T) {
	unread := "| a | b |\n|---|---|\n| x │ y | z |"
	for _, width := range []int{30, 120} {
		if got, want := Markdown(unread, width), rendered(unread, width); got != want {
			t.Errorf("at %d drew\n%s\nwant\n%s", width, got, want)
		}
	}
	if huge := "| a | b |\n|---|---|\n| x | " + strings.Repeat("x", maxUnwrappedWidth+1) + " |"; isGrid(Markdown(huge, 120)) {
		t.Error("a cell wider than any pane kept its grid")
	}
}

// keepsText holds a grid to the cells it was drawn from: the header whole, every
// row's words whole and in order, every row drawn, and none past four lines.
func TestKeepsTextHoldsAGridToItsCells(t *testing.T) {
	want := [][]string{{"Key", "Value"}, {"a", "one two"}, {"b", "three"}}
	for _, c := range []struct {
		name  string
		lines [][]string
		keeps bool
	}{
		{"whole", [][]string{{"Key", "Value"}, {"a", "one two"}, {"b", "three"}}, true},
		{"wrapped", [][]string{{"Key", "Value"}, {"a", "one"}, {"", "two"}, {"b", "three"}}, true},
		{"a cut header", [][]string{{"Key", "Val…"}, {"a", "one two"}, {"b", "three"}}, false},
		{"a cut word", [][]string{{"Key", "Value"}, {"a", "one tw"}, {"b", "three"}}, false},
		{"a row missing", [][]string{{"Key", "Value"}, {"a", "one two"}}, false},
		{"five lines", [][]string{{"Key", "Value"}, {"a", "one"}, {"", ""}, {"", ""}, {"", ""}, {"", "two"}, {"b", "three"}}, false},
	} {
		if got := keepsText(c.lines, want); got != c.keeps {
			t.Errorf("%s: keeps %v, want %v", c.name, got, c.keeps)
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
