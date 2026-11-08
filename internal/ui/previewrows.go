package ui

// The rows a pane draws of the answer being written, and how the transcript's
// window reaches them: under its last line, so the answer scrolls as the newest
// part of the conversation (partial.go).

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/render"
)

// previewRows is the answer's rows in five runs: the blank row above it, the
// finished blocks' first shown rows, the gap a block keeps from what follows it,
// the finished lines, and the open rows. Only the open rows are laid out per
// token; the runs above them never change while the message lasts, so a reader
// scrolled back over them sees nothing move. The open rows are the open block as
// plain text, laid out once more as formatted rows when it finishes.
type previewRows struct {
	lead  []string
	head  chunked[string]
	shown int
	gap   []string
	lines chunked[string]
	open  []string
}

func (r previewRows) len() int {
	return len(r.lead) + r.shown + len(r.gap) + r.lines.count() + len(r.open)
}

// slice is rows [from, to) of the answer, clamped to what it holds.
func (r previewRows) slice(from, to int) []string {
	from, to = max(from, 0), min(to, r.len())
	if to <= from {
		return nil
	}
	out := make([]string, 0, to-from)
	run := func(rows []string, at int) {
		out = append(out, rows[min(max(from-at, 0), len(rows)):min(max(to-at, 0), len(rows))]...)
	}
	at := 0
	run(r.lead, at)
	at += len(r.lead)
	out = append(out, r.head.slice(max(from-at, 0), min(to-at, r.shown))...)
	at += r.shown
	run(r.gap, at)
	at += len(r.gap)
	out = append(out, r.lines.slice(from-at, to-at)...)
	at += r.lines.count()
	run(r.open, at)
	return out
}

// under is the answer drawn under t's last line, with the blank row blockLines
// puts above the block that lands, so the landing moves no row.
func (r previewRows) under(t transcript) previewRows {
	r.lead = nil
	if r.len() > 0 && (t.lines.count() > 0 || t.prefix != "") {
		r.lead = []string{""}
	}
	return r
}

// laidOut is what the pane draws: the finished blocks, then the finished lines and
// the open text as the landed block will lay them out, a block's gap between them.
// Trailing blank rows of the last block are drawn only once something follows
// them, so the answer never ends on a blank row.
func (p partial) laidOut() partial {
	p.prosed = p.prose()
	var fin blocks
	if p.fin != nil {
		fin = *p.fin
	}
	n := fin.stack.count()
	last := fin.stack.slice(n-2, n)
	open := p.openRows()
	p.rows = previewRows{head: fin.stack, shown: n - fin.trailing}
	switch {
	case p.lines.count() > 0:
		p.rows.shown, p.rows.gap, p.rows.lines, p.rows.open = n, gapUnder(last), p.lines, open
	case len(open) > 0:
		p.rows.gap = append(fin.stack.slice(n-fin.trailing, n), gapUnder(last)...)
		p.rows.open = open
	}
	return p
}

// openRows is the open text as the rows it draws, without the blank lines a cut
// leaves at its head when no finished line is above it.
func (p partial) openRows() []string {
	text, width := strings.ToValidUTF8(p.text, ""), max(p.width, minBlockWidth)
	settled := p.lines.count() > 0
	if !p.prose() {
		if text == "" && !settled {
			return nil
		}
		return strings.Split(ansi.Wrap(text, width, ""), "\n")
	}
	if !settled {
		for {
			line, rest, found := strings.Cut(text, "\n")
			if !found || strings.TrimSpace(line) != "" {
				break
			}
			text = rest
		}
		if strings.TrimSpace(text) == "" {
			return nil
		}
	}
	return strings.Split(render.Prose(text, width), "\n")
}

// gapUnder is the blank row render.Stack puts between rows and what follows them,
// decided off their last two rows alone.
func gapUnder(rows []string) []string {
	above := rows[max(len(rows)-2, 0):]
	stacked := render.Stack(above, []string{""})
	return stacked[len(above) : len(stacked)-1]
}

// trimmedBlank is rows without their trailing blank ones.
func trimmedBlank(rows []string) []string {
	for len(rows) > 0 && strings.TrimSpace(ansi.Strip(rows[len(rows)-1])) == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}
