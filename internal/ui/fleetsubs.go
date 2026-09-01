package ui

// A subagent's forwarded frames, held per session so a dispatch is not lost
// the moment its DM opens.
//
// # Why it is a second map and not on DM
//
// DM.subs already holds this, but App.observe only reaches a DM that is in
// App.dms - a conversation somebody has *opened*. The sidebar's whole promise
// is that any running dispatch is openable (rostersubs.go), including one
// under an agent nobody has looked at yet, and Fleet.tasks already answers
// *that* such a dispatch exists for exactly that agent (fleettasks.go, for
// the same reason). This answers what it said: opening a dispatch this
// client never watched live used to draw an empty transcript, because DM.subs
// had nothing to show it - the wire's own floor for a dispatch that truly
// forwarded nothing, arriving instead for one that forwarded plenty.
//
// A DM that opens a dispatch it never watched live seeds itself from here
// once, in viewingPicked (rostersubs.go).
//
// # The fleet's copy always wins, never only the DM's own
//
// foldSub runs unconditionally in Fleet.Observe, on the very same events
// App.observe then hands to a live DM's Append - so Fleet.subs is always a
// prefix-complete history of a dispatch and DM.subs is either that same
// history or a *suffix* of it, never more: a DM that was not yet open when a
// dispatch's first frames arrived captures only what came after it opened.
// Skipping the seed whenever DM.subs already held anything - the first
// version of this - read that partial capture as complete, which permanently
// dropped the dispatch's opening lines the moment anything live landed before
// the first open. So withSubBacklog always replaces from the fleet's copy
// when it is non-empty, which costs a rebuild on every open rather than only
// the first, and is correct on all three of them.
//
// # Unlike fleettasks.go, this is a duplicate rather than a move
//
// A task row is a few strings and counters; a dispatch's forwarded events can
// carry full assistant text and tool payloads, so folding here as well as into
// DM.subs (dm.go's appendForwarded, still gated on the DM being open) doubles
// the memory of every dispatch a client watches live, for the life of the
// session - neither map is ever trimmed. Read-only and non-clobbering keeps it
// safe; it does not keep it free. Moving DM off its own copy entirely, the way
// Tasks moved, is the further step this file does not take.

import "github.com/DilanDoshi/wake/internal/core"

// foldSub folds one of a subagent's forwarded frames into its session's
// backlog, keyed on the dispatch it belongs to, and reports whether there was
// one to fold - the same discriminator foldTask returns, for the same reason:
// a forwarded frame moves nothing on Agent, so Observe's early return must not
// drop it.
func (f Fleet) foldSub(ev core.Event, sessionID string) (map[string]chunked[core.Event], bool) {
	dispatch := forwardedTo(ev)
	if dispatch == "" {
		return nil, false
	}
	bySession := make(map[string]chunked[core.Event], len(f.subs[sessionID])+1)
	for k, v := range f.subs[sessionID] {
		bySession[k] = v
	}
	bySession[dispatch] = bySession[dispatch].append(ev)
	return bySession, true
}

// SubBacklog is what a session's dispatch has said so far, for seeding a DM
// that opens it for the first time. Nil for a dispatch nothing has forwarded.
func (f Fleet) SubBacklog(sessionID, dispatch string) []core.Event {
	events, ok := f.subs[sessionID][dispatch]
	if !ok {
		return nil
	}
	return events.slice(0, events.len())
}

// withSubBacklog replaces one dispatch's frames with SubBacklog, the fleet's
// always-complete copy - see the header for why this must not be conditional
// on whether the DM already holds something.
//
// Each event is also folded through observedTool, which Append already does
// for every event on the live path (dm.go, before the forwarding branch) but
// this seeding path bypasses entirely. Without it a seeded tool call's ⏺ never
// leaves DM.bulletFor's running state: d.outcomes is what a completed call's
// result recolours, and a seeded result was never told to. settled is not
// needed alongside it - it rewrites an *already-drawn* transcript line, and
// nothing here is drawn yet; renderForwarded builds the block fresh once
// Viewing runs, reading d.outcomes at that point.
func (d DM) withSubBacklog(dispatch string, backlog []core.Event) DM {
	if dispatch == "" || len(backlog) == 0 {
		return d
	}
	subs := make(map[string]chunked[core.Event], len(d.subs)+1)
	for k, v := range d.subs {
		subs[k] = v
	}
	subs[dispatch] = chunked[core.Event]{}.append(backlog...)
	d.subs = subs
	for _, ev := range backlog {
		d = d.observedTool(ev)
	}
	return d
}
