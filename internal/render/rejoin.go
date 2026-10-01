package render

// Undoing the wrap for the clipboard. A pane keeps only rendered rows, so a copy
// has to tell the row breaks Markdown made from the ones the source had, and it
// asks the source.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// A Rejoin is how one rendered row goes back into the text it was drawn from.
type Rejoin struct {
	// Sep stands between this row and the one above it: "\n" for a break the
	// source had, or what a wrap consumed - a space, or nothing inside a token.
	Sep string
	// Lead is how many leading cells of the row are layout rather than text.
	Lead int
}

// Rejoins is how each Markdown row drawn from src goes back into the text on a
// copy. The rows are matched back to src rendered at unwrappedWidth, where each
// paragraph, item and line of code is one row, the way a typed turn is matched
// back to what was typed. A row that continues its unwrapped row is a wrap and
// takes exactly the whitespace the wrap consumed - a space, or nothing inside a
// token - whatever produced it: glamour, reflowProse or fitToWidth, and a row
// opening with a styled span alike. Every other break is kept. A row the render
// does not hold (a table laid out anew, the room's speaker, a fold) is kept
// whole, and the next row that opens an unwrapped one picks the match back up.
func Rejoins(rows []string, src string) []Rejoin {
	whole, _ := unwrappedRows(src)
	leads := runLeads(rows)
	out := make([]Rejoin, len(rows))
	k, pos := -1, 0 // the unwrapped row the rows above reached, and how far in
	for i, row := range rows {
		text := strings.TrimSpace(ansi.Strip(row))
		if k >= 0 && text != "" {
			if sep, ok := wrapped(whole[k][pos:], text); ok {
				out[i] = Rejoin{Sep: sep, Lead: plainLead(row)}
				pos += len(sep) + len(text)
				continue
			}
		}
		out[i] = Rejoin{Sep: "\n", Lead: leads[i]}
		if reflowable(row) {
			out[i].Lead = min(leadSpaces(row), int(defaultMargin))
		}
		if j := opened(whole, k+1, row, text); j >= 0 {
			k, pos = j, len(text)
		}
	}
	return out
}

// wrapped is the whitespace at the head of rest a wrap consumed before a row
// reading text: all of it, since text is trimmed and never opens with a space.
func wrapped(rest, text string) (string, bool) {
	body := strings.TrimLeft(rest, " ")
	return rest[:len(rest)-len(body)], strings.HasPrefix(body, text)
}

// opened is the first unwrapped row from on that row begins, or -1. Only a row
// inside the document margin can: the room's speaker and a DM's label sit
// outside it, and would otherwise match a reply that opens with their word.
func opened(whole []string, from int, row, text string) int {
	if text == "" || plainLead(row) < int(defaultMargin) {
		return -1
	}
	for j := from; j < len(whole); j++ {
		if strings.HasPrefix(whole[j], text) {
			return j
		}
	}
	return -1
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

// unwrappedWidth is the width a copy first re-renders its source at: wide
// enough that nearly nothing wraps, and one width, so one cached renderer.
// maxUnwrappedWidth bounds the doubling past it: glamour pads every row to the
// width and spends time quadratic in an unbreakable token's length (about 70 ms
// for one past 8,192 cells), so a copy past the bound keeps what still wraps.
const (
	unwrappedWidth    = 2048
	maxUnwrappedWidth = unwrappedWidth << 2
)

// unwrappedRows is src rendered with nothing in it wrapped - one plain row per
// paragraph, item or line of code, the blank rows between them gone - and the
// width that took. A wrap at width w leaves the row it ended, or the one the
// moved word opens, at least half of w, so the render doubles until every row
// is narrower than that. A table or quote is laid out to the width it gets and
// never rejoins, so its rows do not count.
func unwrappedRows(src string) ([]string, int) {
	for w := unwrappedWidth; ; w *= 2 {
		var rows []string
		widest := 0
		for _, row := range strings.Split(Markdown(src, w), "\n") {
			plain := strings.TrimRight(ansi.Strip(row), " ")
			if text := strings.TrimLeft(plain, " "); text != "" {
				rows = append(rows, text)
			}
			if !strings.ContainsAny(plain, boxDrawing) {
				widest = max(widest, ansi.StringWidth(plain))
			}
		}
		if 2*(widest+int(defaultMargin)) < w || w >= maxUnwrappedWidth {
			return rows, w
		}
	}
}
