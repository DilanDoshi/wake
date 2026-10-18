package ui

import (
	"iter"
	"slices"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
)

// focusSep joins the ids of a team into one key. A session id never holds it.
const focusSep = ","

// joinIDs is the key the room keeps for a thread: an agent's own id when the
// thread is one agent's, so a lone @name compares and filters as it always did,
// and a team's ids sorted, so the key follows who is on the team and not the
// order a report lists them in. A roomLine's `to` is the same key.
func joinIDs(ids []string) string {
	return strings.Join(slices.Sorted(slices.Values(ids)), focusSep)
}

func splitIDs(key string) iter.Seq[string] { return strings.SplitSeq(key, focusSep) }

// focusAdmits reports whether a room line is drawn while the room is focused on
// one thread: an agent's, or a team's, which is the union of its members'. It is
// a pure id comparison - the room holds focus, managerID and the line's own ids,
// and never resolves a name - so it stays testable without a fleet, the way
// attention.go does.
//
// focus == "" is the unfocused room and admits everything, so the filter is the
// identity until a target resolves. A user line (the operator's own turn) is
// admitted when it is a broadcast (to == "") or addressed to a member; an
// agent-produced line (prose, turn-end, permission ask) when it is a member's or
// the manager's. Everything else is hidden.
func focusAdmits(l roomLine, focus, managerID string) bool {
	if focus == "" {
		return true
	}
	for id := range splitIDs(focus) {
		var admit bool
		switch l.ev.Kind {
		case core.KindUserText:
			admit = l.to == "" || addressedTo(l.to, id)
		case core.KindCrossSession:
			// A peer message belongs to both threads: the sender's (l.by,
			// resolved at Append) and the receiver's (l.ev.SessionID). Either
			// narrowing shows it. l.by.ID is empty for an outside sender or a
			// restore, which leaves it in the receiver's thread only - still an
			// id comparison.
			admit = l.by.ID == id || l.ev.SessionID == id
		default:
			admit = l.ev.SessionID == id || l.ev.SessionID == managerID
		}
		if admit {
			return true
		}
	}
	return false
}

// sharesAgent is whether two thread keys name a common agent.
func sharesAgent(a, b string) bool {
	for id := range splitIDs(a) {
		if addressedTo(b, id) {
			return true
		}
	}
	return false
}

// addressedTo is whether the thread key a user line was addressed to holds id.
func addressedTo(to, id string) bool {
	for t := range splitIDs(to) {
		if t == id {
			return true
		}
	}
	return false
}
