package render

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// unwrapped is what a clipboard gets from whole rendered rows: each row
// stripped of styling, its layout lead and its pad, then joined as Rejoins says.
func unwrapped(rendered string) string {
	rows := strings.Split(rendered, "\n")
	var b strings.Builder
	for i, j := range Rejoins(rows) {
		if i > 0 {
			b.WriteString(j.Sep)
		}
		plain := strings.TrimRight(ansi.Strip(rows[i]), " ")
		b.WriteString(plain[min(j.Lead, len(plain)):])
	}
	return b.String()
}

// A paragraph copied whole is the paragraph that was sent, at every width:
// every row break inside rendered prose is a wrap, since markdown renders a
// source newline as a space. The corpus carries `--resume` and `end-to-end`,
// which the wrap splits at a hyphen and which must rejoin with nothing.
//
// Mutation check: joining every row with "\n" (the old copy) fails this at the
// first width that wraps; joining with " " unconditionally fails it at the
// first width that splits a hyphenated word.
func TestAWrappedParagraphRejoinsToWhatWasSent(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for range 300 {
		words := make([]string, 10+rng.Intn(40))
		for i := range words {
			words[i] = wrapWords[rng.Intn(len(wrapWords))]
		}
		src := strings.Join(words, " ")
		width := 20 + rng.Intn(100)
		if got := unwrapped(Markdown(src, width)); got != src {
			t.Fatalf("at width %d the copy is\n%q\nwant\n%q", width, got, src)
		}
	}
}

// An email is the owner's report: paragraphs stay paragraphs, a blank row
// stays a blank line, each bullet is one line, and nothing keeps the margin.
func TestAnEmailCopiesAsParagraphsAndItems(t *testing.T) {
	src := "Hi Sam,\n\nThanks for getting back to me so quickly about the quarterly planning review.\n\n" +
		"- First, the budget numbers need a second look before Friday afternoon.\n" +
		"- Second, the vendor contract renewal is due at the end of the month.\n\nBest regards,\nDilan"
	want := "Hi Sam,\n\nThanks for getting back to me so quickly about the quarterly planning review.\n\n" +
		"• First, the budget numbers need a second look before Friday afternoon.\n" +
		"• Second, the vendor contract renewal is due at the end of the month.\n\nBest regards, Dilan"
	if got := unwrapped(Markdown(src, 40)); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// A paragraph glamour wraps so a row opens `2. Then` copies back as the one
// sentence it is: the row is not an item, so it is a wrap like any other.
func TestAParagraphWhoseWrapOpensAnEnumeratorCopiesWhole(t *testing.T) {
	hit := false
	for width := 40; width <= 80; width++ {
		hit = hit || glamourOpens(t, stepTwo, width, "2. ")
		if got := unwrapped(Markdown(stepTwo, width)); got != stepTwo {
			t.Errorf("width %d: copy is\n%q\nwant\n%q", width, got, stepTwo)
		}
	}
	if !hit {
		t.Fatal("glamour wrapped no row to open `2. ` at any width: this asserts nothing")
	}
}

// An item's wrap that glamour opens with a styled span copies back into the
// item, and prose that only reads like a marker copies back as the one line it is.
func TestAStyledWrapAndALookalikeMarkerCopyWhole(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{styledWrap("-", "**bold words**"), "• an item whose text is long enough to wrap so that a later row can open with bold words and then keep going for a while longer"},
		{styledWrap("-", "`inline code`"), "• an item whose text is long enough to wrap so that a later row can open with inline code and then keep going for a while longer"},
		{"1\\. an escaped enumerator opening a paragraph long enough to wrap onto a second visual line here",
			"1. an escaped enumerator opening a paragraph long enough to wrap onto a second visual line here"},
	} {
		for width := 30; width <= 90; width++ {
			if got := unwrapped(Markdown(tc.src, width)); got != tc.want {
				t.Errorf("width %d: copy is\n%q\nwant\n%q", width, got, tc.want)
			}
		}
	}
}

// A nested item and an enumerated one are items of their own - a new marker
// ends the item above - and a nested one keeps its depth under the margin. An
// enumerated or task item's wrap hangs past its marker, and rejoins from there.
func TestAListItemRejoinsItsHangButNotTheNextItem(t *testing.T) {
	src := "- First, the budget numbers need a second look before Friday afternoon.\n" +
		"  - nested item here\n\n1. an enumerated item that is long enough to wrap onto a second row\n\n" +
		"- [ ] a task item that is long enough to wrap onto a second row here\n\n" +
		"10. a two-digit one that is long enough to wrap onto a second row too"
	got := unwrapped(Markdown(src, 40))
	for _, line := range []string{
		"• First, the budget numbers need a second look before Friday afternoon.",
		"  • nested item here",
		"1. an enumerated item that is long enough to wrap onto a second row",
		"10. a two-digit one that is long enough to wrap onto a second row too",
		"[ ] a task item that is long enough to wrap onto a second row here",
	} {
		if !strings.Contains(got, line+"\n") && !strings.HasSuffix(got, line) {
			t.Errorf("copy lacks the line %q:\n%s", line, got)
		}
	}
}

// Code is never prose: every row is a line of its own, and only the block's
// common indent is layout, so the code's own indentation survives.
func TestACodeBlockCopiesLineForLine(t *testing.T) {
	src := "```go\nfunc main() {\n    fmt.Println(\"hi\")\n}\n```"
	want := "func main() {\n    fmt.Println(\"hi\")\n}"
	if got := unwrapped(Markdown(src, 60)); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// The limit reflowProse documents, held here so it is a decision rather than a
// surprise: a wrapped row that opens with an inline-styled span looks like code
// once rendered, so it stays a row break.
func TestARowOpeningWithAStyledSpanStaysABreak(t *testing.T) {
	src := "Use plain words in the middle of a sentence so that a wrapped row may **open with bold** text."
	got := unwrapped(Markdown(src, 40))
	if !strings.Contains(got, "\nopen with bold") {
		t.Errorf("a row opening with bold was joined, which the rule cannot prove safe:\n%q", got)
	}
}

// A blank line inside a fence is still the fence: glamour paints it, which is
// what tells it from the plain blank row between two blocks. Splitting the run
// there stripped the code after it to column 0.
func TestABlankLineInsideAFenceKeepsTheCodesIndent(t *testing.T) {
	src := "## Head\n\n```python\ndef f():\n    return 1\n\n    x = 2\n\n        y\n```"
	want := "Head\n\ndef f():\n    return 1\n\n    x = 2\n\n        y"
	if got := unwrapped(Markdown(src, 40)); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// Only the renderer's own lead is layout. A snippet whose every line is
// indented in the source keeps that indent; the fence strips the document
// margin and its own, nothing more.
func TestAFenceKeepsIndentEveryLineShares(t *testing.T) {
	src := "```python\n    if ready:\n        ship()\n```"
	want := "    if ready:\n        ship()"
	if got := unwrapped(Markdown(src, 40)); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// An item that opens with a list draws its bullets on one row (joinLoneBullets),
// and its wrap hangs under the text past the last of them.
func TestABulletChainRejoinsItsHang(t *testing.T) {
	src := "- - - three deep item that opens its parents and is long enough to wrap onto rows"
	want := "• • • three deep item that opens its parents and is long enough to wrap onto rows"
	if got := strings.TrimRight(unwrapped(Markdown(src, 40)), "\n"); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}
