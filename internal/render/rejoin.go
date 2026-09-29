package render

// Undoing the wrap for the clipboard. A pane keeps only rendered rows, so a copy
// has to tell the row breaks Markdown made from the ones the source had.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A Rejoin is how one rendered row goes back into the text it was drawn from.
type Rejoin struct {
	// Sep stands between this row and the one above it: "\n" for a break the
	// source had, or what a wrap consumed - a space, or nothing at a hyphen.
	Sep string
	// Lead is how many leading cells of the row are layout rather than text.
	Lead int
}

// Rejoins classifies rendered Markdown rows with reflowProse's own predicates,
// so the package that made the wraps is the one that undoes them. Rows it
// grouped as one paragraph or list item are wraps by construction - markdown
// renders a source newline as a space - and every other break is kept. A row
// reflowable refuses (code, a quote, a row opening with a styled span) is kept
// whole, which is reflowProse's own conservative limit.
func Rejoins(rows []string) []Rejoin {
	out := make([]Rejoin, len(rows))
	cont := -1 // the lead the group above continues at; -1 outside prose
	for i, row := range rows {
		lead := leadSpaces(row)
		switch {
		case !reflowable(row):
			cont = -1
			out[i] = Rejoin{Sep: "\n", Lead: runLead(rows, i)}
		case lead == cont && !opensItem(row):
			out[i] = Rejoin{Sep: wrapSep(rows[i-1], row), Lead: lead}
		default:
			// hangIndentLists moved a bullet's continuations under its text.
			cont = lead
			if bulletMarker(row, lead) {
				cont += ansi.StringWidth(bullet)
			}
			out[i] = Rejoin{Sep: "\n", Lead: min(lead, int(defaultMargin))}
		}
	}
	return out
}

// runLead is the indent shared by the unreflowable rows around i - a code
// block's, say - so the code's own indentation survives the copy. A blank row
// glamour painted is inside a fence and joins the run; a plain one ends it.
func runLead(rows []string, i int) int {
	kept := func(r string) bool { return strings.TrimSpace(r) != "" && !reflowable(r) }
	if !kept(rows[i]) {
		return 0
	}
	from, to := i, i
	for from > 0 && kept(rows[from-1]) {
		from--
	}
	for to+1 < len(rows) && kept(rows[to+1]) {
		to++
	}
	lead := -1
	for _, r := range rows[from : to+1] {
		if blankRow(r) {
			continue
		}
		plain := ansi.Strip(r)
		if n := len(plain) - len(strings.TrimLeft(plain, " ")); lead < 0 || n < lead {
			lead = n
		}
	}
	return max(lead, 0)
}

// wrapSep is what a wrap between prev and next consumed, judged the way
// rewrapProse joins the same two fragments.
func wrapSep(prev, next string) string {
	p := strings.TrimRight(ansi.Strip(prev), " ")
	n := strings.TrimLeft(ansi.Strip(next), " ")
	if hyphenJoin(p, n) {
		return ""
	}
	return " "
}
