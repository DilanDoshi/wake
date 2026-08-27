package ui

// Assembling a conversation's transcript from its events: the block, the
// whole-transcript re-derivation, and the incremental fold a live tool run
// takes. dm.go owns the model and how the pane is sized; dm_blocks.go owns what
// one event looks like; this owns how those blocks become the scrollback.

import "github.com/DilanDoshi/wake/internal/core"

// block is one rendered unit of transcript.
type block struct {
	text string

	// laidOut is the blockLines result when a full Room render has already laid
	// the block out. transcript.replaceFrom consumes it instead of splitting the
	// same block a second time. Incremental and DM blocks leave it nil.
	laidOut []string

	// joined marks a block that belongs directly under the one before it - a
	// tool result under its ⏺ call - rather than opening a new turn. The pair
	// is Claude Code's most recognizable idiom and a blank row between the two
	// halves reads as two unrelated things.
	joined bool

	// tool is the tool_use id this block belongs to, and "" for every other
	// block. It is what a click resolves to; see transcript.mark.
	tool string

	// head marks the half of a call that opens with the ⏺ line, which is the
	// row a result recolours when it settles.
	head bool

	// run is the key of the folded tool run this block stands in for, and ""
	// for every block that is not a rollup. It is what a click on the summary
	// line resolves to - see rollup.go and transcript.mark.
	run string
}

// renderTranscript is the seam renderAll is reached through, so a test can
// count how often the whole transcript goes back through glamour. Rendering is
// the expensive half of this package and View's fast path is a contract a
// caller can silently break, which makes the count the thing worth asserting.
//
// Reach renderAll through this, never directly: a direct call is invisible to
// the counter, and the fast-path tests would keep passing while no longer
// discriminating.
var renderTranscript = DM.renderAll

// renderAll re-derives every block at the current width. Call it through
// renderTranscript, never directly.
//
// A run of tool calls folds to one rollup line unless it is expanded, which is
// the same decision Append makes incrementally - so a re-wrap, or a conversation
// restored from disk, redraws a run exactly as the live path drew it. The two
// agree because they walk the same events: Append never stores an event that
// draws nothing, and Before drops the same ones (see DM.Before), so d.events
// holds only tool blocks and blocks that draw something. That is what lets the
// boundary be structural here - a run ends at a marker or at any non-tool block
// - rather than a render this would have to repeat on every re-wrap.
func (d DM) renderAll() []block {
	if d.viewing != "" {
		return d.renderForwarded()
	}
	events := d.events.slice(0, d.events.len())
	blocks := make([]block, 0, len(events)+2)
	// First, so it scrolls away as the conversation fills - see banner.go.
	blocks = append(blocks, dmBanner(d.Agent, d.blockWidth()))

	runStart := -1 // the open run's first event, or -1 for none
	// Rebuilds each checklist op's snapshot as we walk - never stored, so a re-wrap
	// redraws it (see checklist.go). Fold-exempt events never join a run, so every
	// one reaches the per-event render below, which keeps this fold complete.
	var cl checklist
	flush := func(end int) {
		if runStart < 0 {
			return
		}
		if key := events[runStart].Tool.ID; d.expandedRun(key) {
			blocks = d.appendPerCall(blocks, events[runStart:end])
		} else {
			blocks = append(blocks, d.rollupBlock(key, events[runStart:end]))
		}
		runStart = -1
	}

	for i := range events {
		// Before the event, so the boundary lands in the same place whether the
		// events around it rendered to anything or not. A boundary breaks a run.
		if m, ok := d.markerBefore(i); ok {
			flush(i)
			blocks = append(blocks, m)
		}
		ev := events[i]
		if d.isToolBlock(ev) {
			if runStart < 0 && isToolUse(ev) {
				runStart = i // a run starts at a use, never at a stray result
			}
			if runStart >= 0 {
				continue // folded into the open run, drawn at the flush
			}
		}
		// A non-tool block, or a stray result with no open run: it ends any run
		// above it and draws itself if it draws anything.
		flush(i)
		// A subagent's op stays out of the parent's list here too (foldChecklist's
		// rule), so a restored forwarded op cannot fold in where the live path did not.
		if ev.Subagent == nil && ev.Tool != nil && ev.Tool.Checklist != nil {
			cl = cl.apply(ev.Tool.Checklist)
			ev = cl.withSnapshot(ev)
		}
		if b := d.renderEvent(ev); b.text != "" {
			blocks = append(blocks, b)
		}
	}
	flush(len(events))
	return blocks
}

// renderForwarded is the open dispatch's transcript: its own frames and
// nothing else.
//
// No banner and no last-read boundaries. Both belong to the conversation - the
// banner names the session, and a boundary records where the reader left *it* -
// and neither is a fact about a subagent that ran for four seconds inside one
// turn of it.
func (d DM) renderForwarded() []block {
	events := d.subs[d.viewing]
	blocks := make([]block, 0, events.len())
	for _, ev := range events.slice(0, events.len()) {
		if b := d.renderEvent(ev); b.text != "" {
			blocks = append(blocks, b)
		}
	}
	return blocks
}

// advanceRun updates the live run's key and running tally for a fold event, in
// O(1): a use starts or extends the run and bumps its category; a result changes
// neither count. Called for every fold event whether the pane draws it or not,
// so the summary is formatted from a kept tally rather than a walk back over the
// whole run - the difference between O(run) and O(run²) for a long batch on the
// one goroutine every session's events pass through.
func (d DM) advanceRun(ev core.Event) DM {
	if d.runKey == "" {
		if !isToolUse(ev) {
			return d // a stray result with no use to belong to; drawFold draws it
		}
		d.runKey, d.runTally = ev.Tool.ID, nil // a fresh run
	}
	if isToolUse(ev) {
		d.runTally = countUse(d.runTally, ev.Tool.Name)
	}
	return d
}

// drawFold draws the tool run ev belongs to, with runKey and runTally already
// advanced. A collapsed run is one summary line, rewritten in place from the
// kept tally as the run grows - the settle machinery's own trick, one line
// rather than a re-render. An expanded run draws the call whole. A result the
// last-read boundary orphaned from its run draws as itself.
func (d DM) drawFold(ev core.Event) DM {
	key := d.runKey
	if key == "" {
		d.tr = d.tr.add(d.renderEvent(ev))
		return d
	}
	switch {
	case d.expandedRun(key):
		if b := d.renderEvent(ev); b.text != "" {
			d.tr = d.tr.add(b)
		}
	case d.tr.runHead(key) >= 0:
		d.tr = d.tr.restyle(d.tr.runHead(key), d.blockWidth(), d.rollupLine(d.runTally))
	default:
		d.tr = d.tr.add(block{text: d.rollupLine(d.runTally), run: key})
	}
	return d
}
