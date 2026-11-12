package daemon

// A headless claude does not exit on stdin EOF while a background shell runs, so
// a stop or a park of an agent with one never completed: the row sat idle and the
// shell outlived /quit (BUG-75, BUG-76). Such an agent is ended once nothing it
// was given is still to run.

import (
	"github.com/DilanDoshi/wake/internal/core"
)

// stopWorld is the stop of an agent whose world is noted first, and which is then
// ended if a shell holds it open.
func (a *agent) stopWorld() error {
	a.noteWorld()
	err := a.stop()
	a.endIfHeldOpen(false)
	return err
}

// endIfHeldOpen ends a stopped agent claude would otherwise keep, once it is between
// turns: none open (claude opens one itself when a background task finishes), none
// owed, and no message Wake wrote still to run. endProcess, not kill, so a park's
// label survives for completePark. refresh notes the world once more first: from a
// turn's end, a shell may have started after the stop's listing.
func (a *agent) endIfHeldOpen(refresh bool) {
	a.mu.Lock()
	held := a.stopped && !a.ended && !a.owed && !a.turnOpen && len(a.unstarted) == 0 && a.runsShellLocked()
	a.mu.Unlock()
	if !held {
		return
	}
	if refresh {
		a.noteWorld()
	}
	logf("wake: session %s is held open by a background shell after its stop; ending its process", a.id)
	a.endProcess()
}

func (a *agent) runsShellLocked() bool {
	for _, ev := range a.runningTasks {
		if ev.Task != nil && ev.Task.Kind == core.TaskShell {
			return true
		}
	}
	return false
}

// markUnstarted notes a message written to claude that it has not taken up, and
// markTaken forgets it: its lifecycle says it started or will never run.
func (a *agent) markUnstarted(id string) {
	if id == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.unstarted == nil {
		a.unstarted = map[string]struct{}{}
	}
	a.unstarted[id] = struct{}{}
}

func (a *agent) markTaken(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.unstarted, id)
}

// opensTurn is whether an event is the parent's own words: the turn is running
// until its result, whoever asked for it.
func opensTurn(ev core.Event) bool {
	return ev.Subagent == nil && (ev.ModelOutput() || ev.Kind == core.KindMessageStart || ev.Kind == core.KindPartialText)
}
