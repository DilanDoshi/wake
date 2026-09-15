package daemon

// Which dispatches a session has running, for fork-safety.
//
// A background subagent streams past the parent's own turn end: the parent's
// KindTurnEnd clears owed and the tool, so it reports idle - its working line
// off (the KindToolUse gate in observe) - while the subagent's forwarded frames
// still write the conversation. forkRefusal reads State/Name/ID alone
// (forkgate_test.go) and so cannot see that, and forking a transcript a subagent
// is still writing is the unrecorded concurrent-flush case forkRefusal refuses a
// working parent for. So the daemon keeps its own liveness track of running
// dispatches, and forkSource refuses off it.
//
// It is the daemon's own liveness fact, not the UI's row: ui.Tasks folds the
// same lifecycle for display (labels, tokens, a cursor), while this keeps only
// the running-and-openable subset a fork has to wait on. The daemon owns
// liveness; the UI owns the row.
//
// Split from agent.go when the field pushed that file past the hard max, the
// same seam agentstatus.go took for snapshot.

import "github.com/DilanDoshi/wake/internal/core"

// trackSub folds one dispatch-lifecycle frame into the running-subagent set.
// The caller holds a.mu.
//
// Keyed on phase, not status: task_started is the only frame that carries the
// kind and the dispatch, so membership is decided when the dispatch opens (an
// agent with a transcript of its own - ui.Task.Openable's predicate) and the id
// alone retires it, since an ending frame names neither. A progress frame is
// neither phase and leaves the row as task_started set it.
func (a *agent) trackSub(u *core.TaskUpdate) {
	switch u.Phase {
	case core.TaskStarted:
		if u.Kind == core.TaskAgent && u.Dispatch != "" {
			if a.runningSubs == nil {
				a.runningSubs = make(map[string]struct{})
			}
			a.runningSubs[u.ID] = struct{}{}
		}
	case core.TaskEnded:
		delete(a.runningSubs, u.ID)
	}
}

// hasRunningSubagent reports whether an agent dispatch with a transcript of its
// own is still running - the fact forkSource refuses a fork on. Read on the
// client goroutine while observe writes the set on the fan-out one, so it takes
// the lock.
func (a *agent) hasRunningSubagent() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.runningSubs) > 0
}
