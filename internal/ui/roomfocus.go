package ui

import "github.com/DilanDoshi/wake/internal/core"

// focusAdmits reports whether a room line is drawn while the room is focused on
// one agent. It is a pure id comparison - the room holds focus, managerID and
// the line's own ids, and never resolves a name - so it stays testable without a
// fleet, the way attention.go does.
//
// focus == "" is the unfocused room and admits everything, so the filter is the
// identity until a target resolves. A user line (the operator's own turn) is
// admitted when it is a broadcast (to == "") or addressed to the focused agent;
// an agent-produced line (prose, turn-end, permission ask) when it is the
// focused agent's or the manager's. Everything else is hidden.
func focusAdmits(l roomLine, focus, managerID string) bool {
	if focus == "" {
		return true
	}
	if l.ev.Kind == core.KindUserText {
		return l.to == "" || l.to == focus
	}
	if l.ev.Kind == core.KindCrossSession {
		// A peer message belongs to both threads: the sender's (l.by, resolved
		// at Append) and the receiver's (l.ev.SessionID). Either narrowing shows
		// it. l.by.ID is empty for an outside sender or a restore, which leaves
		// it in the receiver's thread only - still an id comparison.
		return l.by.ID == focus || l.ev.SessionID == focus
	}
	return l.ev.SessionID == focus || l.ev.SessionID == managerID
}
