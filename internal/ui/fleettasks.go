package ui

// What each session has dispatched, held per session on the Fleet.
//
// # Why it is a second map and not a field on Agent
//
// Fleet.Observe skips its own copy when the folded Agent is unchanged, which is
// what keeps a busy fleet costing a lookup and a struct comparison per frame
// rather than a fleet-sized copy. That comparison is `now == was` on a struct,
// so every field on Agent has to be comparable - Agent.MCPNeedsAuth is a count
// for this reason and says so. Tasks holds a slice, so it cannot go there
// without either breaking the comparison or making it a deep one.
//
// # Why it moved off DM
//
// It was folded in DM.observedTask, and App.observe only reaches a DM that is in
// App.dms - which holds conversations somebody has *opened*. The sidebar draws
// every agent, so an agent nobody had opened had no dispatches to draw. The fold
// is the same fold; only its owner changed, and DM keeps the cursor because the
// cursor is per-conversation.

import "github.com/DilanDoshi/wake/internal/core"

// Tasks is what one session has dispatched, and an empty list for a session
// that has dispatched nothing.
func (f Fleet) Tasks(sessionID string) Tasks { return f.tasks[sessionID] }

// foldTask folds a dispatch frame into one session's list, reporting whether
// there was one to fold.
//
// The flag is the discriminator Observe's early return needs: a task frame
// moves nothing on Agent, so `now == was` is true for every one of them and a
// return taken on that alone drops the whole fold. Tasks.Observe already
// answers "was this a lifecycle frame" by returning the receiver untouched, so
// the test is the same one it makes rather than a second opinion about it.
func (f Fleet) foldTask(ev core.Event, sessionID string) (Tasks, bool) {
	if ev.Task == nil {
		return Tasks{}, false
	}
	return f.tasks[sessionID].Observe(ev), true
}

// named fills in what an ending frame does not carry about itself, from the row
// the fold already holds, and hands back the event a transcript should store.
//
// At ingest rather than in the renderer: DM.renderAll re-derives every block
// from its own event at a new width, and a block that had to consult the fold
// would draw something different after a re-wrap than it did on arrival. It is
// called after Observe, so the row it reads includes this frame.
func (f Fleet) named(sessionID string, ev core.Event) core.Event {
	if ev.Task == nil {
		return ev
	}
	ev.Task = f.tasks[sessionID].named(ev.Task)
	return ev
}

// RunningTasks is the subagents a session is dispatching *now*, in start order.
//
// Two filters, and they answer two different questions.
//
// **Running only**, which is the sidebar's rule and not the pane's: the pane
// keeps a finished dispatch because its transcript is readable and dropping the
// row at the moment it becomes worth opening is the complaint that surface
// exists to answer. The sidebar answers a different question - what is this
// costing me right now - and a column of finished work is what it is scanned
// past. It is also what bounds the column: every agent keeps its dispatches for
// the life of the session, so a sidebar drawing all of them grows without limit
// next to thirty agents, where the running ones are few and self-clearing.
//
// **Openable only**, which the pane deliberately does not apply - it lists a
// background shell because what is running is worth showing whether or not it
// can be read, and marks it with a glyph of its own. This column has neither
// the width for that distinction nor the standing to make it: a shell carries
// no subagent_type, so subagentName falls through to its description and it
// reads exactly like a subagent - under a heading that says these are
// subagents. And every row here is something ⌃D will open, where the pane's
// rows are walked by a cursor that refuses the ones with nothing behind them.
// Task.Openable is the one place that decides which have a conversation, so
// this asks it rather than restating it.
func (f Fleet) RunningTasks(sessionID string) []Task {
	rows := f.tasks[sessionID].Rows()
	out := make([]Task, 0, len(rows))
	for _, row := range rows {
		if row.Status == core.TaskRunning && row.Openable() {
			out = append(out, row)
		}
	}
	return out
}
