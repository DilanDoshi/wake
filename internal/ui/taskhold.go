package ui

// An agent's early ending, held until it is the last.
//
// A subagent that hands its report back while a task it started for itself still runs
// is told as `completed`, starts again under the same id when that task ends, and is
// told as `completed` a second time (2.1.329, subagent-handback-then-rewoken.jsonl). A
// line per ending says "finished" twice for one dispatch. Whether an ending settles its
// dispatch is two questions with one answer: what the frame says (core reads a notice
// that the agent has not reported as progress, subagentWaiting, and the row stays
// running) and what the fold knows (a task the agent owns is still running), asked here.
//
// The hold rewrites the *stored* copy of the early ending as a non-ending (Tasks.named),
// since a block is a pure function of the event it is stored against (taskline.go). The
// rows are never touched, so the sidebar reads as it did. No timer: a restart drops the
// hold, and a held ending is drawn at the first of the session's next turn ending with
// nothing it owns still running, the agent leaving the report (ended or parked), or a gap
// in what this client saw. A /clear drops it with the conversation it belonged to.

import (
	"slices"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// owns reports whether a task the agent dispatched itself is still running.
func (t Tasks) owns(id string) bool {
	return slices.ContainsFunc(t.rows, func(row Task) bool { return row.Parent == id && row.Status == core.TaskRunning })
}

// isOwnAgent reports whether the row for id is a subagent the session dispatched itself.
func (t Tasks) isOwnAgent(id string) bool {
	row, ok := t.forID(id)
	return ok && row.Kind == core.TaskAgent && row.Parent == ""
}

// holding folds what an event, already in the rows, means for the held endings: a restart
// ends the hold; any later ending of the agent replaces it, and is itself held only when
// it is a `completed` with a task of its own still running (the frame that names its
// dispatch, the one a line is drawn from).
func (t Tasks) holding(ev core.Event) Tasks {
	u := ev.Task
	switch {
	case u.Phase == core.TaskStarted:
		t.held = without(t.held, u.ID)
	case u.Phase == core.TaskEnded && u.Dispatch != "" && t.isOwnAgent(u.ID):
		t.held = without(t.held, u.ID)
		if u.Status == core.TaskDone && t.owns(u.ID) {
			ev.Task = t.named(u)
			t.held = append(t.held, ev)
		}
	}
	return t
}

// without is held less the ending for id, as a new slice when there is one to take out.
func without(held []core.Event, id string) []core.Event {
	if !slices.ContainsFunc(held, func(h core.Event) bool { return h.Task.ID == id }) {
		return held
	}
	return slices.DeleteFunc(slices.Clone(held), func(h core.Event) bool { return h.Task.ID == id })
}

// released is the held endings this event settles - the session's own turn ending, with
// nothing the agent owns still running - and the Tasks without them, in the order held.
func (t Tasks) released(ev core.Event) (Tasks, []core.Event) {
	if len(t.held) == 0 || ev.Kind != core.KindTurnEnd || ev.Subagent != nil {
		return t, nil
	}
	var kept, out []core.Event
	for _, h := range t.held {
		if t.owns(h.Task.ID) {
			kept = append(kept, h)
		} else {
			out = append(out, h)
		}
	}
	t.held = kept
	return t, out
}

// drained is every held ending, in the order held, and the Tasks without them.
func (t Tasks) drained() (Tasks, []core.Event) {
	out := t.held
	t.held = nil
	return t, out
}

// releaseHeld is the endings an event settles for one session. The fleet is copied only
// when there are some, as Observe does.
func (f Fleet) releaseHeld(sessionID string, ev core.Event) (Fleet, []core.Event) {
	next, out := f.tasks[sessionID].released(ev)
	return f.withReleased(sessionID, next, out)
}

// drainHeld is every held ending of one session.
func (f Fleet) drainHeld(sessionID string) (Fleet, []core.Event) {
	next, out := f.tasks[sessionID].drained()
	return f.withReleased(sessionID, next, out)
}

func (f Fleet) withReleased(sessionID string, next Tasks, out []core.Event) (Fleet, []core.Event) {
	if len(out) == 0 {
		return f, nil
	}
	f = f.copy()
	f.tasks[sessionID] = next
	return f, out
}

// settleHeld draws what an event settles. It follows the event's own append, so a turn's
// ending line stands after the turn.
func (a App) settleHeld(sessionID string, ev core.Event) App {
	var out []core.Event
	a.fleet, out = a.fleet.releaseHeld(sessionID, ev)
	return a.drawEndings(sessionID, out)
}

// settleGone draws the held endings of each session a report no longer shows running:
// ended, or parked, which leaves Sessions. A report is read before the fleet folds it.
func (a App) settleGone(st *rpc.Status) App {
	live := map[string]bool{}
	for _, s := range st.Sessions {
		live[s.ID] = s.State != rpc.StateEnded
	}
	return a.drainHeldWhere(func(id string) bool { return !live[id] })
}

// settleAll draws every held ending: what a returning client missed cannot be told.
func (a App) settleAll() App { return a.drainHeldWhere(func(string) bool { return true }) }

func (a App) drainHeldWhere(gone func(id string) bool) App {
	for id, t := range a.fleet.tasks {
		if len(t.held) == 0 || !gone(id) {
			continue
		}
		var out []core.Event
		a.fleet, out = a.fleet.drainHeld(id)
		a = a.drawEndings(id, out)
	}
	return a
}

// drawEndings appends released endings where a live one goes: the open conversation and
// the board's tile. Each carries what named gave it when it was held.
func (a App) drawEndings(sessionID string, evs []core.Event) App {
	for _, ev := range evs {
		if dm, ok := a.dms[sessionID]; ok {
			a = a.withDM(sessionID, dm.Append(ev))
		}
		a = a.foldBoard(sessionID, ev)
	}
	return a
}
