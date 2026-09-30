package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// leadingCols is the number of leading space columns on an ANSI-stripped line.
func leadingCols(line string) int {
	s := ansi.Strip(line)
	return len(s) - len(strings.TrimLeft(s, " "))
}

// nonBlank keeps only the lines with visible content.
func nonBlank(out string) []string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			kept = append(kept, line)
		}
	}
	return kept
}

// TestListItemContinuationHangsUnderItsText is the bug this change fixes:
// glamour v1.0.0 drops a wrapped list-item continuation back to the list margin
// (under the bullet) instead of hanging it under the item's text. Claude Code
// hangs it under the text, at the column where the marker ends.
func TestListItemContinuationHangsUnderItsText(t *testing.T) {
	const src = "- First bullet that is also long enough to wrap onto a second " +
		"visual line to reveal the hanging-indent behaviour of the renderer"
	lines := nonBlank(Markdown(src, 56))
	if len(lines) < 2 {
		t.Fatalf("item did not wrap; got %d line(s):\n%s", len(lines), strings.Join(lines, "\n"))
	}
	// The marker line is `  • First…`: bullet at col 2, item text at col 4.
	markerLead := leadingCols(lines[0])
	hang := markerLead + 2 // "• " is two columns
	if got := leadingCols(lines[0]); got != 2 {
		t.Fatalf("marker line lead = %d, want 2 (a top-level bullet)", got)
	}
	for i, line := range lines[1:] {
		if got := leadingCols(line); got != hang {
			t.Errorf("continuation line %d has lead %d, want %d (hung under the item text): %q",
				i+1, got, hang, ansi.Strip(line))
		}
	}
}

// TestNestedListItemContinuationHangsUnderItsText covers the +2-per-level case:
// a nested bullet's text starts two columns deeper, and its wrap must hang there.
func TestNestedListItemContinuationHangsUnderItsText(t *testing.T) {
	const src = "- Outer item\n" +
		"  - Nested bullet under the outer one that is long enough to wrap onto " +
		"a second visual line here for certain now"
	lines := nonBlank(Markdown(src, 56))
	// Find the nested marker line (lead 4, begins with the bullet).
	var idx = -1
	for i, line := range lines {
		if leadingCols(line) == 4 && strings.HasPrefix(strings.TrimLeft(ansi.Strip(line), " "), bullet) {
			idx = i
			break
		}
	}
	if idx == -1 || idx+1 >= len(lines) {
		t.Fatalf("no wrapped nested bullet found:\n%s", strings.Join(lines, "\n"))
	}
	hang := 4 + 2 // nested bullet text starts at column 6
	for i := idx + 1; i < len(lines); i++ {
		// Stop at the next marker (there is none here, but stay honest).
		if strings.HasPrefix(strings.TrimLeft(ansi.Strip(lines[i]), " "), bullet) {
			break
		}
		if got := leadingCols(lines[i]); got != hang {
			t.Errorf("nested continuation line %d has lead %d, want %d: %q",
				i, got, hang, ansi.Strip(lines[i]))
		}
	}
}

// TestOrderedListContinuationHangsUnderItsText is the enumerator's version of
// the bullet fix: a wrapped `N. ` item's continuation hangs under the item text,
// at the column the enumerator ends - wider for a wider number, and deeper for a
// nested list.
func TestOrderedListContinuationHangsUnderItsText(t *testing.T) {
	for _, tc := range []struct {
		name, src, marker string
		lead              int
	}{
		{"one digit", "1. First numbered item that is also long enough to wrap onto a " +
			"second visual line to reveal the hanging indent for enumerations", "1. ", 2},
		{"two digits", "9. nine\n10. tenth item padded out so the marker is three digits " +
			"wide and still wraps onto a second visual line here", "10. ", 2},
		{"nested", "1. outer\n   1. nested ordered item long enough to wrap onto a " +
			"second visual line here for certain now", "1. ", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertHangsUnder(t, nonBlank(Markdown(tc.src, 56)), tc.marker, tc.lead)
		})
	}
}

// TestAListRightAfterAHeadingOrAFenceHangs pins what the item rule rests on: a
// list that opens straight after a heading or a closing fence, with no blank line
// in the source, still starts after a row reflow cannot touch (glamour's own
// blank, or the styled row itself), so its first item is an item and hangs - at
// the widths the prose case splits on and around them.
func TestAListRightAfterAHeadingOrAFenceHangs(t *testing.T) {
	const item = "1. an ordered item right under it that is long enough to wrap onto a second visual line here"
	for _, src := range []string{"# A heading\n" + item, "```\ncode\n```\n" + item} {
		for width := 40; width <= 80; width++ {
			assertHangsUnder(t, nonBlank(Markdown(src, width)), "1. ", 2)
		}
	}
}

// assertHangsUnder finds the item opening with marker at lead and checks every
// continuation of it sits under its text, the column the marker ends.
func assertHangsUnder(t *testing.T, lines []string, marker string, lead int) {
	t.Helper()
	at := -1
	for i, line := range lines {
		if leadingCols(line) == lead && strings.HasPrefix(strings.TrimLeft(ansi.Strip(line), " "), marker) {
			at = i
		}
	}
	if at < 0 || at+1 >= len(lines) {
		t.Fatalf("no wrapped item opening %q at lead %d:\n%s", marker, lead, strings.Join(lines, "\n"))
	}
	hang := lead + ansi.StringWidth(marker)
	for i := at + 1; i < len(lines); i++ {
		if got := leadingCols(lines[i]); got != hang {
			t.Errorf("continuation %d of %q has lead %d, want %d (hung under the item text): %q",
				i-at, marker, got, hang, ansi.Strip(lines[i]))
		}
	}
}

// TestHangIndentNeverReindentsCode is the critical guard: a fenced code block is
// laid out verbatim, and the hanging-indent pass must not move any of its lines,
// even when it directly follows a list.
func TestHangIndentNeverReindentsCode(t *testing.T) {
	const width = 56
	// A code fence rendered on its own, to capture glamour's native indent.
	alone := nonBlank(Markdown("```go\nx := 1\ny := 2\n```", width))
	var wantLead int
	for _, line := range alone {
		if strings.Contains(ansi.Strip(line), "x := 1") {
			wantLead = leadingCols(line)
		}
	}
	// The same fence after a wrapping list. A pass that let list state bleed
	// across the block boundary would push these code lines further right.
	const src = "- an item that is long enough to wrap onto a second visual line " +
		"here for sure absolutely now indeed\n- item two\n\n```go\nx := 1\ny := 2\n```"
	for _, line := range nonBlank(Markdown(src, width)) {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "x := 1") || strings.Contains(plain, "y := 2") {
			if got := leadingCols(line); got != wantLead {
				t.Errorf("code line was reindented to lead %d, want %d (unchanged): %q",
					got, wantLead, plain)
			}
		}
	}
}

// TestHangIndentKeepsEveryListLineWithinWidth is the adversarial guard: an item
// is re-wrapped at the hang, so no row it lays out may grow past the layout glamour
// gives a paragraph - the width less its far margin - checked down to a width where the
// marker and its hang eat most of the line, across deep nesting, wide
// enumerators and task boxes. The hang used to be a shift applied after glamour's
// wrap, which spent that margin and at width 56 drew a bullet's continuation 55
// cells wide.
func TestHangIndentKeepsEveryListLineWithinWidth(t *testing.T) {
	sources := []string{
		"- one item long enough to wrap several times over so the continuation is exercised at every width here now indeed",
		"- outer\n  - nested and long enough to wrap onto more than one visual line for sure absolutely here\n    - deeper still and also long enough to wrap around at least once here now",
		"1. first ordered item long enough to wrap onto a second and maybe a third visual line at narrow widths here\n2. second\n10. tenth item padded out so the marker is three digits wide and still wraps here now indeed",
		"- \n- non-empty item that wraps onto a second visual line here for certain now absolutely indeed yes",
		"- [ ] a task item long enough to wrap onto a second and maybe a third visual line at narrow widths\n- [x] and a ticked one that wraps as well here now",
		"99. a two-digit enumerator\n100. and a three-digit one long enough to wrap onto a second visual line at any width here",
		"- bullet item text long enough so that the next word lands at the start `inline code` and more words after it to wrap",
		// Unbreakable tokens: glamour cannot wrap these, so the shift overruns
		// width and fitToWidth must re-wrap them — the fitToWidth-hardwrap path
		// the padding-reclaim assumption does not cover.
		"- See https://example.com/a/very/long/path/segment/that/has/no/spaces/anywhere for the write-up and the fine details",
		"- verylongidentifierwithnobreaksatall_" + strings.Repeat("x", 60) + " and trailing words to wrap here now",
		"- " + strings.Repeat("日本語", 40) + " and some trailing text to force a wrap after the run",
	}
	for _, src := range sources {
		for width := minMarkdownWidth; width <= 80; width++ {
			for i, line := range strings.Split(Markdown(src, width), "\n") {
				// A styled token glamour could not wrap (the link) is never reflowed:
				// fitToWidth hard-wraps it, and width is its only bound.
				bound := width
				if reflowable(line) {
					bound -= int(defaultMargin)
				}
				if got := ansi.StringWidth(line); got > bound {
					t.Errorf("width %d: line %d is %d cells wide, past %d: %q", width, i, got, bound, ansi.Strip(line))
				}
			}
		}
	}
}

// TestTheHangLeavesNonListRowsWhereGlamourLaysThem holds the hang to lists: in a
// render with no list in it, every row sits at a lead glamour itself laid a row
// at, so nothing was hung. The prose case is the adversarial one - at widths 53-55
// glamour wraps it so a row opens `2. Then`, which reads as an enumerator on its
// own and must not hang the row under it: a real item follows a blank row, a
// change of indent, or another item.
func TestTheHangLeavesNonListRowsWhereGlamourLaysThem(t *testing.T) {
	for _, src := range []string{
		"A plain paragraph that is long enough to wrap onto a second visual line at this width here now.",
		"Upgrade the service to the newest release in step 2. Then restart it and check that every worker comes back up healthy again",
		"# Heading\n\nbody text that wraps onto a second visual line here for certain now absolutely indeed",
		"```go\nfunc main() { println(1) }\nvar x = averylongidentifierthatwrapsaround\n```",
		"> a quoted line long enough to wrap onto a second visual line here for sure now indeed absolutely",
		"| col a | col b |\n|---|---|\n| 1 | 2 |",
	} {
		for width := 40; width <= 80; width++ {
			r, err := rendererFor(width)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := lockAndRender(r, src)
			if err != nil {
				t.Fatal(err)
			}
			laid := map[int]bool{}
			for _, line := range nonBlank(raw) {
				laid[leadingCols(line)] = true
			}
			for _, line := range nonBlank(Markdown(src, width)) {
				if !laid[leadingCols(line)] {
					t.Errorf("width %d: a row of a non-list render was moved to lead %d: %q",
						width, leadingCols(line), ansi.Strip(line))
				}
			}
		}
	}
}

// TestCodeLineThatLooksLikeABulletIsNotAMarker is the adversarial guard for the
// critical rule: a fenced code block whose text opens with `• ` must not be read
// as a list, or the code lines after it get reindented. glamour paints code with
// a colour, so a real (unstyled) marker and a coloured code bullet are tellable
// apart — that escape is the whole discriminator.
func TestCodeLineThatLooksLikeABulletIsNotAMarker(t *testing.T) {
	const src = "```\n• not a real bullet, just a line of code here\n" +
		"second code line that must not move at all\n```"
	var leads []int
	for _, line := range nonBlank(Markdown(src, 56)) {
		leads = append(leads, leadingCols(line))
	}
	for i := 1; i < len(leads); i++ {
		if leads[i] != leads[0] {
			t.Errorf("code line %d lead %d != first %d — a code bullet was read as a marker",
				i, leads[i], leads[0])
		}
	}
}

// TestTaskListContinuationHangsUnderItsText is the same for a checkbox item:
// glamour's Task style draws `[ ] text` in the bullet's place, and the wrap hangs
// under the text after the box, ticked or not.
func TestTaskListContinuationHangsUnderItsText(t *testing.T) {
	for _, box := range []string{"[ ]", "[x]"} {
		src := "- " + box + " a task item that is long enough to wrap onto a second " +
			"visual line here for sure now indeed absolutely yes"
		marker := unticked
		if box == "[x]" {
			marker = ticked
		}
		t.Run(box, func(t *testing.T) { assertHangsUnder(t, nonBlank(Markdown(src, 56)), marker, 2) })
	}
}

// TestParagraphAfterListIsNotHangIndented guards the other misclassification: a
// plain paragraph following a list sits at the list margin too, but it is not a
// continuation and must keep its margin.
func TestParagraphAfterListIsNotHangIndented(t *testing.T) {
	const src = "- item one\n- item two\n\nA regular paragraph that follows the " +
		"list and is long enough to wrap onto a second visual line here."
	for _, line := range nonBlank(Markdown(src, 56)) {
		plain := ansi.Strip(line)
		if strings.Contains(plain, "regular paragraph") || strings.Contains(plain, "second visual") {
			if got := leadingCols(line); got != 2 {
				t.Errorf("paragraph line was hang-indented to lead %d, want 2: %q", got, plain)
			}
		}
	}
}
