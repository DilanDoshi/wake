package render

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// copiedAt is what a clipboard gets from src drawn whole at width: each row
// stripped of styling, its layout lead and its pad, then joined as Rejoins says.
func copiedAt(src string, width int) string {
	return copiedRows(strings.Split(Markdown(src, width), "\n"), src)
}

func copiedRows(rows []string, src string) string {
	var b strings.Builder
	for i, j := range Rejoins(rows, src) {
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
		if got := copiedAt(src, width); got != src {
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
	if got := copiedAt(src, 40); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// A paragraph glamour wraps so a row opens `2. Then` copies back as the one
// sentence it is: the row is not an item, so it is a wrap like any other.
func TestAParagraphWhoseWrapOpensAnEnumeratorCopiesWhole(t *testing.T) {
	hit := false
	for width := 40; width <= 80; width++ {
		hit = hit || glamourOpens(t, stepTwo, width, "2. ")
		if got := copiedAt(stepTwo, width); got != stepTwo {
			t.Errorf("width %d: copy is\n%q\nwant\n%q", width, got, stepTwo)
		}
	}
	if !hit {
		t.Fatal("glamour wrapped no row to open `2. ` at any width: this asserts nothing")
	}
}

// A styled span glamour breaks at its own hyphens comes back whole, drawn and
// copied: the join reads the rows' text, not the escapes after a trailing `-`.
// The copy is held where the span fits a row; narrower, the reflow cuts it
// mid-token, and no copy can tell that cut from a space.
func TestAHyphenatedSpanRejoinsWithoutASpace(t *testing.T) {
	const token = "--resume-session-token-value,"
	const src = "- an item that names a flag long enough to wrap, `--resume-session-token-value`, and more words"
	const want = "• an item that names a flag long enough to wrap, " + token + " and more words"
	for width := minMarkdownWidth; width <= 80; width++ {
		out := Markdown(src, width)
		for _, row := range strings.Split(ansi.Strip(out), "\n") {
			// A hyphen then a space mid-row, after a letter or a hyphen, is a join.
			row = strings.TrimRight(row, " ")
			if at := strings.Index(row, "- "); at > 0 && row[at-1] != ' ' && row[at-1] != ',' {
				t.Errorf("width %d: a space was joined into the span: %q", width, row)
			}
		}
		if hang := 4; width-int(defaultMargin)-hang >= len(token) {
			if got := copiedRows(strings.Split(out, "\n"), src); got != want {
				t.Errorf("width %d: copy is\n%q\nwant\n%q", width, got, want)
			}
		}
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
			if got := copiedAt(tc.src, width); got != tc.want {
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
	got := copiedAt(src, 40)
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
	if got := copiedAt(src, 60); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// A wrapped row that opens with a styled span - bold, inline code, a link -
// looks like code once rendered, which is why reflowProse leaves it at
// glamour's wrap. Matched back to the source it rejoins like any other row, and
// so does the row after it, at every width.
//
// Mutation check: classifying rows by reflowProse's predicates (the old rule)
// fails this at the first width that wraps before a styled word.
func TestARowOpeningWithAStyledSpanRejoins(t *testing.T) {
	words := [][2]string{
		{"**bold words**", "bold words"}, {"`make ci`", "make ci"}, {"[the docs](https://example.com/a)", "the docs https://example.com/a"},
		{"plain", "plain"}, {"end-to-end", "end-to-end"}, {"--resume", "--resume"}, {"words", "words"}, {"here", "here"},
	}
	rng := rand.New(rand.NewSource(11))
	for range 300 {
		var src, want []string
		for range 10 + rng.Intn(30) {
			w := words[rng.Intn(len(words))]
			src, want = append(src, w[0]), append(want, w[1])
		}
		width := 20 + rng.Intn(100)
		if got := copiedAt(strings.Join(src, " "), width); got != strings.Join(want, " ") {
			t.Fatalf("at width %d the copy of\n%q\nis\n%q\nwant\n%q", width, strings.Join(src, " "), got, strings.Join(want, " "))
		}
	}
}

// A paragraph whose first row opens styled, and a bold span split across a
// bullet's hang, rejoin too.
func TestAStyledOpeningAndAStyledHangRejoin(t *testing.T) {
	for src, want := range map[string]string{
		"**Note:** the first row opens bold and the paragraph runs long enough to wrap twice over at forty.": "Note: the first row opens bold and the paragraph runs long enough to wrap twice over at forty.",
		"- item one that is long enough to wrap around the width for sure **bold start** yes and more words": "• item one that is long enough to wrap around the width for sure bold start yes and more words",
	} {
		if got := copiedAt(src, 40); got != want {
			t.Errorf("copy is\n%q\nwant\n%q", got, want)
		}
	}
}

// A token too long for any row is hard-wrapped by fitToWidth onto a row with no
// margin; the break is inside the token, so it rejoins with nothing.
func TestAHardWrappedTokenRejoinsWithNothing(t *testing.T) {
	src := "See https://example.com/a/very/long/path/that/cannot/fit/in/forty/columns/at/all ok"
	if got := copiedAt(src, 40); got != src {
		t.Errorf("copy is\n%q\nwant\n%q", got, src)
	}
}

// A fence inside a list item is drawn straight under the item, at the item's
// hang and with a styled row's escapes - which is what ruled out telling the
// two apart by their rows. The code is its own line.
func TestAFenceInAListItemStaysItsOwnLine(t *testing.T) {
	src := "- item\n  ```\n  code in item\n  ```\n- next"
	want := "• item\ncode in item\n\n• next"
	if got := copiedAt(src, 40); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// A row the source does not hold - the room's speaker above a reply - is kept
// as drawn, and the rows after it still rejoin.
func TestARowTheSourceDoesNotHoldIsKept(t *testing.T) {
	src := "alex said this and then kept on talking long enough that the paragraph wraps"
	rows := append([]string{"alex"}, strings.Split(Markdown(src, 30), "\n")...)
	if got, want := copiedRows(rows, src), "alex\n"+src; got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// A table is laid out to whatever width it is given, so it must not widen the
// render, and nothing widens it past maxUnwrappedWidth.
func TestTheUnwrappedRenderWidensOnlyForWhatWraps(t *testing.T) {
	table := "| a | b |\n|---|---|\n| 1 | 2 |\n\nshort prose"
	if _, w := unwrappedRows(table); w != unwrappedWidth {
		t.Errorf("a table and a short paragraph rendered at %d, want %d", w, unwrappedWidth)
	}
	if _, w := unwrappedRows(strings.Repeat("y", maxUnwrappedWidth+1)); w != maxUnwrappedWidth {
		t.Errorf("a token past the bound rendered at %d, want the bound %d", w, maxUnwrappedWidth)
	}
}

// The match-back needs its source on one row per paragraph, so the unwrapped
// render widens until nothing in it wraps: a paragraph past unwrappedWidth, and
// a token past it that fitToWidth would otherwise hard-wrap, are one row each.
//
// Mutation check: rendering at unwrappedWidth alone gives this paragraph a
// break at the unwrapped render's own wrap, about 2,040 cells in.
func TestAParagraphWiderThanTheUnwrappedRenderStillRejoins(t *testing.T) {
	para := strings.TrimSpace(strings.Repeat("abcdefgh ", 3*unwrappedWidth/9))
	token := strings.Repeat("x", 5*unwrappedWidth/2)
	if rows, _ := unwrappedRows(para + "\n\n- " + para + "\n\n" + token); len(rows) != 3 {
		t.Errorf("a %d-cell paragraph, the same as an item, and a %d-cell token render as %d rows, want 3", len(para), len(token), len(rows))
	}
	if got := copiedAt(para, 80); got != para {
		t.Errorf("a %d-cell paragraph copied with %d breaks", len(para), strings.Count(got, "\n"))
	}
}

// A blank line inside a fence is still the fence: glamour paints it, which is
// what tells it from the plain blank row between two blocks. Splitting the run
// there stripped the code after it to column 0.
func TestABlankLineInsideAFenceKeepsTheCodesIndent(t *testing.T) {
	src := "## Head\n\n```python\ndef f():\n    return 1\n\n    x = 2\n\n        y\n```"
	want := "Head\n\ndef f():\n    return 1\n\n    x = 2\n\n        y"
	if got := copiedAt(src, 40); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// Only the renderer's own lead is layout. A snippet whose every line is
// indented in the source keeps that indent; the fence strips the document
// margin and its own, nothing more.
func TestAFenceKeepsIndentEveryLineShares(t *testing.T) {
	src := "```python\n    if ready:\n        ship()\n```"
	want := "    if ready:\n        ship()"
	if got := copiedAt(src, 40); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}

// An item that opens with a list draws its bullets on one row (joinLoneBullets),
// and its wrap hangs under the text past the last of them.
func TestABulletChainRejoinsItsHang(t *testing.T) {
	src := "- - - three deep item that opens its parents and is long enough to wrap onto rows"
	want := "• • • three deep item that opens its parents and is long enough to wrap onto rows"
	if got := strings.TrimRight(copiedAt(src, 40), "\n"); got != want {
		t.Errorf("copy is\n%q\nwant\n%q", got, want)
	}
}
