package ui

// The room seed: a DM opened after an agent already spoke in the room shows
// those turns straight away, drawn from the room's own memory.
//
// A never-opened DM misses an agent's room turns entirely - they are never
// live-appended to a conversation that does not exist, and the on-disk restore
// that would otherwise carry them on open (history.go) is dropped whenever an
// event races the reply, the ordinary case for an active agent. So a fresh DM
// seeds itself from the room, which held those turns all along.
//
// The seed is provisional and the on-disk read stays authoritative: a clean
// fold supersedes and clears it (withoutSeed, so a turn never draws twice),
// while a race-dropped read leaves it standing (so the turn shows on the first
// open, not only after a clean reopen). It never enters DM.events, so it cannot
// disturb the marks, the run fold, or Before's prepend - it is a render-only
// lane above the transcript (dmtranscript.go). core.Event carries no uuid, so a
// dedup-merge is impossible; provisional-then-superseded is the shape that fits.

import "github.com/DilanDoshi/wake/internal/core"

// turnsFor is the conversation this agent has had in the room: the agent's own
// replies, and the operator's turns routed to it. Chronological, the append
// order said keeps.
//
// Deliberately narrow - an assistant reply the agent produced, and a user turn
// the operator addressed to this agent alone (l.to == id, the FromRoom echo the
// live DM shows). A broadcast (to == "") and the manager's own lines are left
// out: seeding either could show a turn the disk read would not, and the seed
// must never claim more than the authoritative read will. FromRoom is stamped
// on the user turn so the DM heads it "from the room", the way the live echo is.
func (r Room) turnsFor(id string) []core.Event {
	if id == "" {
		return nil
	}
	var out []core.Event
	for _, l := range r.said.slice(0, r.said.len()) {
		switch l.ev.Kind {
		case core.KindUserText:
			if l.to == id {
				ev := l.ev
				ev.SessionID, ev.FromRoom = id, true
				out = append(out, ev)
			}
		case core.KindAssistantText:
			if l.ev.SessionID == id {
				out = append(out, l.ev)
			}
		}
	}
	return out
}

// withRoomSeed provisions this DM with the room's account of the agent's turns,
// for a DM opening after those turns happened. It renders above the transcript
// and is cleared the moment the authoritative on-disk read folds.
func (d DM) withRoomSeed(events []core.Event) DM {
	if len(events) == 0 {
		return d
	}
	d.seed = events
	d.tr = d.tr.replace(renderTranscript(d)).toBottom()
	return d
}

// withoutSeed drops the provisional room seed: the on-disk read is folding, and
// it carries the same turns, so keeping the seed would draw them twice. (/clear
// drops it the same way, inline in clearedBySessionReset, where the seed's
// pre-clear turns must not survive the conversation the model just dropped.)
func (d DM) withoutSeed() DM {
	d.seed = nil
	return d
}
