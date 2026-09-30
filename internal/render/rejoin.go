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
	cont, first := -1, "" // where the group above continues (-1 outside prose), and its first row
	leads := runLeads(rows)
	for i, row := range rows {
		lead := leadSpaces(row)
		switch {
		case !reflowable(row):
			cont = -1
			out[i] = Rejoin{Sep: "\n", Lead: leads[i]}
		case lead == cont && !splits(first, row, cont):
			out[i] = Rejoin{Sep: wrapSep(rows[i-1], row), Lead: lead}
		default:
			cont, first = hangOf(row, lead), row
			if i+1 < len(rows) && leadSpaces(rows[i+1]) != cont {
				cont = lead // nothing hangs under it: prose that only reads like an item
			}
			out[i] = Rejoin{Sep: "\n", Lead: min(lead, int(defaultMargin))}
		}
	}
	return out
}

// splits reports whether row, at the column the group first opened continues
// at, starts an item of its own. Under a hang any item marker is a nested list;
// at the group's own lead only startsItem's rule does, so a paragraph row that
// wraps to open `2. Then` stays in its paragraph.
func splits(first, row string, cont int) bool {
	if cont > leadSpaces(first) {
		return opensItem(row)
	}
	return startsItem(first, row, "")
}

// hangOf is where a group's continuations sit: under an item's text, which
// reflowProse hangs past its marker and every bullet joinLoneBullets put
// beside it; at the lead for anything else.
func hangOf(row string, lead int) int {
	rest, hang := row[lead:], lead
	for m := itemMarker(rest, ""); m != ""; m = itemMarker(rest, "") {
		rest, hang = rest[len(m):], hang+ansi.StringWidth(m)
	}
	return hang
}

// runLeads is, for each row, the indent shared by the unreflowable run it sits
// in - a code block's, say - capped at a fence's own layout (the document margin
// and the block's), so indentation the code itself shares survives the copy.
// One pass, so a long fence costs a copy linear time.
func runLeads(rows []string) []int {
	leads := make([]int, len(rows))
	for i := 0; i < len(rows); {
		if !inRun(rows[i]) {
			i++
			continue
		}
		j, lead := i, 2*int(defaultMargin)
		for ; j < len(rows) && inRun(rows[j]); j++ {
			if !blankRow(rows[j]) {
				lead = min(lead, plainLead(rows[j]))
			}
		}
		for k := i; k < j; k++ {
			leads[k] = lead
		}
		i = j
	}
	return leads
}

// inRun reports a row that shares a run's lead. A blank row glamour painted is
// inside a fence; a plain blank ends the run, and so does a row outside the
// document margin - the speaker's name a room reply sits under.
func inRun(r string) bool {
	if strings.TrimSpace(r) == "" || reflowable(r) {
		return false
	}
	return blankRow(r) || plainLead(r) >= int(defaultMargin)
}

// plainLead is a styled row's leading spaces once its escapes are gone.
func plainLead(r string) int {
	plain := ansi.Strip(r)
	return len(plain) - len(strings.TrimLeft(plain, " "))
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
