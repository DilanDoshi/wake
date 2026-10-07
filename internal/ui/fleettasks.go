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

// RunningTasks is the dispatches a session is running *now*, in start order: its
// subagents, workflows and background shells.
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
// **A shell is listed but never selectable** (Task.Selectable). It is work the
// operator is paying for, and "is anything still running" - what turnDone reads
// off this list - must be true while one goes: an idle agent with a shell
// running is not done. It forwards no frames, so the cursor and a click treat
// its row as the agent's own. A workflow is the other row with no transcript:
// ↵, ⌃D or a click on it opens the /workflows view (viewingPicked).
func (f Fleet) RunningTasks(sessionID string) []Task {
	rows := f.tasks[sessionID].Rows()
	out := make([]Task, 0, len(rows))
	for _, row := range rows {
		if row.Status == core.TaskRunning && (row.Selectable() || row.Kind == core.TaskShell) {
			out = append(out, row)
		}
	}
	return out
}
