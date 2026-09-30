package ui

// How much scrollback a conversation keeps: its newest dmRetentionEvents
// events, the oldest reclaimed a chunk at a time under one fixed line.

import (
	"slices"

	"github.com/DilanDoshi/wake/internal/core"
)

const (
	// dmRetentionEvents bounds what a conversation holds, and so what a width
	// change re-wraps: every event goes back through glamour. 3,000 is where a
	// re-wrap was measured at 248ms on resize_bench_test.go's cheapest turns and
	// accepted - the settle in geometry.go exists to pay it once per drag - and
	// BenchmarkReWrapAtTheRetentionCap records it (73ms, 2026-09-30). The room's
	// bound is its own, over far cheaper rows.
	dmRetentionEvents = 3_000

	dmReclaimedHistory = "… older conversation reclaimed"
)

// retained reclaims the oldest events once the conversation holds a chunk more
// than it keeps, so a reclaim renders a chunk once per chunk appended. While a
// subagent is on screen only the events go: the transcript is the subagent's,
// and coming back draws what was kept.
func (d DM) retained() DM {
	if d.events.count() < dmRetentionEvents+chunkSize {
		return d
	}
	cut, clean := d.cutFrom(d.events.len() - dmRetentionEvents)
	if cut < 0 {
		return d
	}
	following, first := d.tr.atBottom(), !d.reclaimed()
	rows, exact := 0, false
	if d.viewing == "" && clean {
		rows, exact = d.rowsBefore(cut)
	}
	d.seed = nil
	d.events = d.events.trimBefore(cut)
	d.marks = slices.DeleteFunc(slices.Clone(d.marks), func(m int) bool { return m < cut })
	switch {
	case d.viewing != "":
	case exact:
		if first {
			d.tr.prefix = HintStyle.Render(dmReclaimedHistory)
		}
		d.tr = d.tr.trimBefore(d.tr.lines.first() + rows)
		d.tr.scroll = max(d.tr.scroll, d.tr.first())
	default:
		// A cut into a run, or lines that no longer match a render: laid out again,
		// a reader scrolled back kept as far from the newest line as they were.
		fromBottom := d.tr.bottom() - d.tr.scroll
		if d.tr = d.rewrapped(); following {
			d.tr = d.tr.toBottom()
		} else {
			d.tr.scroll = max(d.tr.bottom()-fromBottom, d.tr.first())
		}
	}
	if !clean {
		d = d.withTrailingRun()
	}
	return d
}

// cutFrom is where a reclaim cuts, looking a chunk past mark at most: before an
// event of no tool call, so no run is split and no result - fold-exempt ones
// included - is kept without its call (clean); failing that, a stream of nothing
// but tool calls is cut before a call, never a result, and laid out again. -1
// is a chunk with neither.
func (d DM) cutFrom(mark int) (int, bool) {
	for cut := mark; cut < mark+chunkSize; cut++ {
		if d.events.at(cut).Tool == nil {
			return cut, true
		}
	}
	for cut := mark; cut < mark+chunkSize; cut++ {
		if isToolUse(d.events.at(cut)) {
			return cut, false
		}
	}
	return -1, false
}

// rowsBefore is how many scrollback lines the events before cut hold, with the
// banner and seed above them until the first reclaim, and whether it could tell.
// It walks the live lines against a render of those events, stepping past each
// last-read rule the scrollback still draws past maxLastReadRules - drawn when
// its absence ended, no longer anchored, gone at the next re-wrap (lastread.go).
// Lines that match nothing are not guessed past: the caller lays out again.
func (d DM) rowsBefore(cut int) (int, bool) {
	head := d
	head.events = chunked[core.Event]{base: d.events.first(), n: d.events.first()}.
		append(d.events.slice(d.events.first(), cut)...)
	var want []string
	for k, b := range renderTranscript(head) {
		want = append(want, blockLines(b, k == 0 && !d.reclaimed())...)
	}
	rule := blockLines(block{text: lastReadLine(d.blockWidth())}, false)
	ruleAt := func(lines func(int) string, at int) bool {
		for k, l := range rule {
			if lines(at+k) != l {
				return false
			}
		}
		return true
	}
	live := d.tr.lines.at
	wanted := func(j int) string {
		if j < len(want) {
			return want[j]
		}
		return "\x00" // past the render: matches no line
	}
	start := d.tr.lines.first()
	i := start
	for j := 0; j < len(want); {
		switch {
		case i >= d.tr.lines.len():
			return 0, false
		case ruleAt(live, i) && !ruleAt(wanted, j):
			i += len(rule)
		case live(i) == want[j]:
			i, j = i+1, j+1
		default:
			return 0, false
		}
	}
	// A rule drawn above the cut belongs to it only while anchored there.
	for !slices.Contains(d.marks, cut) && ruleAt(live, i) {
		i += len(rule)
	}
	return i - start, true
}

// reclaimed reports whether the conversation's oldest events have gone.
func (d DM) reclaimed() bool { return d.events.first() > 0 }

// rewrapped is the scrollback laid out again from the events, under the
// reclaimed line when the conversation is what the pane draws. The numbering is
// kept: a reader's place is held in it, and a click that re-wraps keeps that.
func (d DM) rewrapped() transcript {
	prefix := ""
	if d.viewing == "" && d.reclaimed() {
		prefix = HintStyle.Render(dmReclaimedHistory)
	}
	return d.tr.replaceFrom(renderTranscript(d), d.tr.lines.first(), prefix)
}
