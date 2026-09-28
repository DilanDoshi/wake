package render

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// plainLines is a render as text, one right-trimmed line per row, blanks kept.
func plainLines(out string) []string {
	var lines []string
	for _, l := range strings.Split(ansi.Strip(out), "\n") {
		lines = append(lines, strings.TrimRight(l, " "))
	}
	return lines
}

// A bullet whose text opens with a number is, to CommonMark, a bullet holding
// an ordered list that starts there - how an agent keeps a document's own
// numbering (`- 28. …`), since a bare `28.` after a line of text is not a list
// at all. glamour enters every list on a fresh line, so the bullet was drawn
// alone and its text a row below it.
//
// Mutation check: dropping joinLoneBullets from Markdown fails this.
func TestABulletOpeningWithANumberKeepsItOnTheBulletsLine(t *testing.T) {
	const src = "**Contracts in the portal**\n" +
		"- 28. The contracts portal tab is structurally unreachable, and there is no user action to deprecate a contract\n" +
		"- 30. The steward portal never shows the actual rule values\n"
	lines := nonBlank(Markdown(src, 60))
	got := plainLines(strings.Join(lines, "\n"))
	for _, l := range got {
		if strings.TrimSpace(l) == "•" {
			t.Fatalf("a bullet is drawn alone on its row:\n%s", strings.Join(got, "\n"))
		}
	}
	if !strings.HasPrefix(got[1], "  • 28. The contracts portal") {
		t.Errorf("row 1 = %q, want the bullet and its text on one row", got[1])
	}
	// The wrapped rest sits under the number, where glamour put it.
	if lead := leadingCols(lines[2]); lead != 4 {
		t.Errorf("the item's second row starts at column %d, want 4 (under %q):\n%s",
			lead, "28.", strings.Join(got, "\n"))
	}
	if !slices.ContainsFunc(got, func(l string) bool { return strings.HasPrefix(l, "  • 30. The steward portal") }) {
		t.Errorf("no row holds the second item on its bullet's row:\n%s", strings.Join(got, "\n"))
	}
}

// The same for a bullet opening with a bullet, and for a chain of them: each
// lone marker takes the one beneath it, and a nested bullet's wrapped text still
// hangs under that text.
func TestABulletOpeningWithABulletKeepsItOnTheBulletsLine(t *testing.T) {
	lines := plainLines(Markdown("- - nested bullet long enough to wrap onto a second row here", 40))
	nb := nonBlank(strings.Join(lines, "\n"))
	if !strings.HasPrefix(nb[0], "  • • nested bullet") {
		t.Errorf("row 0 = %q, want both bullets and the text on one row", nb[0])
	}
	if lead := leadingCols(nb[1]); lead != 6 {
		t.Errorf("the nested bullet's second row starts at column %d, want 6 (under its text):\n%s",
			lead, strings.Join(nb, "\n"))
	}
	if got := nonBlank(strings.Join(plainLines(Markdown("- - - deep", 40)), "\n")); len(got) != 1 ||
		!strings.HasPrefix(got[0], "  • • • deep") {
		t.Errorf("a chain of lone bullets rendered as %q, want one row", got)
	}
}

// A nested list of several items joins only its first to the bullet; the rest
// stay where glamour laid them, under it.
func TestOnlyTheFirstItemOfANestedListJoinsTheBullet(t *testing.T) {
	got := nonBlank(strings.Join(plainLines(Markdown("- 1. alpha\n  2. beta\n- 3. gamma", 40)), "\n"))
	want := []string{"  • 1. alpha", "    2. beta", "  • 3. gamma"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rendered\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A bullet opening with a code block or a quote is left exactly as glamour drew
// it: both are painted, and the join takes only an unstyled list marker.
func TestABulletOpeningWithCodeOrAQuoteIsLeftAlone(t *testing.T) {
	for _, src := range []string{"- ```\n  - 1. code\n  ```", "- > quoted"} {
		r, err := rendererFor(40)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := lockAndRender(r, src)
		if err != nil {
			t.Fatal(err)
		}
		before := hangIndentLists(reflowProse(stylingOnly(raw), 40))
		if after := joinLoneBullets(before); after != before {
			t.Errorf("%q: the join changed a bullet opening with a painted block:\n%q\nto\n%q", src, before, after)
		}
	}
}

// Joining spends the columns the bullet's own row had, never more: the item's
// text starts where it did, so no row grows past the width it was built for.
func TestJoiningABulletKeepsEveryRowWithinWidth(t *testing.T) {
	sources := []string{
		"- 3. The unstructured lane's accuracy above the OCR ceiling, which is what the next release decides from the measurements",
		"- - a nested bullet long enough to wrap several times over at the narrowest widths here now indeed",
		"- 100. " + strings.Repeat("unbreakable", 12) + " and some trailing words to wrap",
	}
	for _, src := range sources {
		for width := minMarkdownWidth; width <= 80; width++ {
			for i, line := range strings.Split(Markdown(src, width), "\n") {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("width %d: line %d is %d cells wide: %q", width, i, got, ansi.Strip(line))
				}
			}
		}
	}
}

// An empty item draws the same lone bullet, but what follows it is its sibling
// at the same indent, not its content, and the two stay two rows.
//
// Mutation check: dropping the column test from joinLoneBullets fails this.
func TestAnEmptyItemIsNotJoinedToTheNextOne(t *testing.T) {
	got := nonBlank(strings.Join(plainLines(Markdown("- \n- next item", 40)), "\n"))
	want := []string{"  •", "  • next item"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("rendered\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// Only a row of bullets is lone: text that happens to end in one is not, even
// with an item marker sitting two columns past it.
//
// Mutation check: dropping the bullets-only test from loneBulletAt fails this.
func TestARowEndingInABulletCharacterIsNotALoneBullet(t *testing.T) {
	const in = "  x •\n      • item"
	if got := joinLoneBullets(in); got != in {
		t.Errorf("joinLoneBullets(%q) = %q, want it untouched", in, got)
	}
}

// Only an item marker moves up: a row two columns in that opens no item is the
// bullet's content drawn as a block of its own, and stays under it.
//
// Mutation check: dropping opensItem from joinLoneBullets fails this.
func TestOnlyAnItemMarkerJoinsALoneBullet(t *testing.T) {
	const in = "  •\n    plain words"
	if got := joinLoneBullets(in); got != in {
		t.Errorf("joinLoneBullets(%q) = %q, want it untouched", in, got)
	}
}
