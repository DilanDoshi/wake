package daemon

// Handing a newly attached client the fleet's running dispatches - askreplay.go
// for a subagent instead of an ask, and split out of server.go for its reason.

import (
	"sort"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// replayRunningTasks hands a newly attached client each dispatch still running
// across the fleet, as the ordinary rpc.FrameEvent a live client would have
// gotten for its task_started - so its own Fleet.foldTask rebuilds the real
// sidebar row rather than nothing. task_* frames are the one running-state Wake
// never persists (BUG-33) and never puts on the report (rpc.SessionStatus has
// no task field), so without this a reattach or a second window shows no
// dispatch a subagent started before it looked.
//
// Unicast, and called *after* addClient (server.go), the shape askreplay.go
// takes: the client is subscribed first, so a dispatch ending in the replay
// window is delivered live rather than missed. Delivered, not necessarily
// folded after the replayed start - the client folds a started as running
// unconditionally (internal/ui/tasks.go's updated), so in the sub-millisecond
// overlap where the live ending's frame is queued ahead of this replayed start
// the row is drawn running and stays until the agent's next turn or a park.
// That is the same bounded, self-clearing residual a dispatch that never gets
// its terminal frame already has (docs/notes/bugs.md BUG-35), and the trade the
// minimal replay was chosen for. Replaying *before* subscribe would be worse:
// an ending in that window would be missed outright, stranding the row forever.
// It covers only a dispatch running when a client attaches, not one a
// still-attached client later drops.
func (s *server) replayRunningTasks(c *client) {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()

	for _, a := range agents {
		for _, f := range a.runningTaskFrames() {
			c.enqueue(f)
		}
	}
}

// runningTaskFrames is this agent's still-running dispatches as the ordinary
// rpc.FrameEvent a live client would have gotten - see replayRunningTasks.
// Ordered by task id so the replay is deterministic; the client folds by id, so
// the order is not otherwise load-bearing.
func (a *agent) runningTaskFrames() []rpc.Frame {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.runningTasks) == 0 {
		return nil
	}
	ids := make([]string, 0, len(a.runningTasks))
	for id := range a.runningTasks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	frames := make([]rpc.Frame, len(ids))
	for i, id := range ids {
		ev := forClients(a.runningTasks[id])
		frames[i] = rpc.Frame{Kind: rpc.FrameEvent, SessionID: a.id, Event: &ev}
	}
	return frames
}

// withWorkflow returns a copy of ev with a copied Task and Workflow, edited by
// edit - never mutating the retained event (agent.observe holds it under a.mu)
// or the one already fanned out to a live client.
func withWorkflow(ev core.Event, edit func(*core.WorkflowUpdate)) core.Event {
	task := *ev.Task
	workflow := *task.Workflow
	edit(&workflow)
	task.Workflow = &workflow
	ev.Task = &task
	return ev
}

// forClients is ev as a client receives it: a workflow's script stays the
// daemon's - its save reads it off the retained start - since no client draws
// one and it is the largest thing a start carries.
func forClients(ev core.Event) core.Event {
	if ev.Task == nil || ev.Task.Workflow == nil || ev.Task.Workflow.Script == "" {
		return ev
	}
	return withWorkflow(ev, func(w *core.WorkflowUpdate) { w.Script = "" })
}

// runningWorkflow reports whether id names a running workflow dispatch - the
// fact FrameStopRun may act on. Its own Kind check is the whole gate: a
// subagent or shell id is refused for being a different dispatch kind, not
// for anything the wire is known to do with one.
func (a *agent) runningWorkflow(id string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	ev, ok := a.runningTasks[id]
	return ok && ev.Task != nil && ev.Task.Kind == core.TaskWorkflow
}
