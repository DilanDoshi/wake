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
// than it keeps, so a reclaim renders a chunk once per chunk appended. It cuts
// only before an event of no tool call - so no run is split and no result is
// kept without its call, fold-exempt ones included - looking a chunk past the
// mark at most. While a subagent is on screen only the events go: the
// transcript is the subagent's, and coming back draws what was kept.
func (d DM) retained() DM {
	if d.events.count() < dmRetentionEvents+chunkSize {
		return d
	}
	cut, end := d.events.len()-dmRetentionEvents, d.events.len()-dmRetentionEvents+chunkSize
	for cut < end && d.events.at(cut).Tool != nil {
		cut++
	}
	if d.events.at(cut).Tool != nil {
		return d // a chunk of tool calls at the mark: tried again as more arrive
	}
	if d.viewing == "" {
		rows := d.rowsBefore(cut)
		if !d.reclaimed() {
			d.tr.prefix = HintStyle.Render(dmReclaimedHistory)
		}
		d.tr = d.tr.trimBefore(d.tr.lines.first() + rows)
		d.tr.scroll = max(d.tr.scroll, d.tr.first())
	}
	d.seed = nil
	d.events = d.events.trimBefore(cut)
	d.marks = slices.DeleteFunc(slices.Clone(d.marks), func(m int) bool { return m < cut })
	return d
}

// rowsBefore is how many scrollback lines the events before cut hold, with the
// banner and seed above them until the first reclaim: their render once more,
// plus a rule per absence the scrollback still draws past maxLastReadRules.
// Those were drawn when the absence ended and are no longer anchored, so a
// render drops them (lastread.go); only the live lines can say where they are.
func (d DM) rowsBefore(cut int) int {
	head := d
	head.events = chunked[core.Event]{base: d.events.first(), n: d.events.first()}.
		append(d.events.slice(d.events.first(), cut)...)
	rows := 0
	for k, b := range renderTranscript(head) {
		rows += len(blockLines(b, k == 0 && !d.reclaimed()))
	}
	rule := lastReadLine(d.blockWidth())
	perRule := len(blockLines(block{text: rule}, false))
	drawn := func(from, to int) int {
		n := 0
		for i := from; i < to; i++ {
			if d.tr.lines.at(i) == rule {
				n++
			}
		}
		return n
	}
	// The stale rules push the chunk's edge past rows by perRule each: the edge
	// is the first span whose rules outnumber the anchored ones by exactly the
	// span's excess. No two rules are adjacent - an event is drawn between any
	// two - so the first such span is the true one.
	start := d.tr.lines.first()
	anchored := len(slices.DeleteFunc(slices.Clone(d.marks), func(m int) bool { return m >= cut }))
	for extra := 0; ; {
		stale := drawn(start, start+rows+extra) - anchored
		if stale*perRule <= extra {
			return rows + extra
		}
		extra = stale * perRule
	}
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
