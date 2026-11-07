package ui

// How the agent's own last turn ended, as this client believes it: when it
// finished (doneAt, captured at Fleet.WithStatus's working→idle edge) and whether
// the API failed it. turnDone (dmbeat.go) reads both, and the ✔, the DM's done
// line and the strip's `N done` read turnDone. Split from fleet.go by subject.

import (
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// withTurnOutcome folds whether the API failed the agent's own turn. A failure
// reaches the UI only as a KindAPIError, which observe routes to the notice row
// before Fleet.Observe, so it is marked here, ahead of that return; the result
// behind it still reads idle and captures doneAt, so turnDone reads this bit.
// The agent's own model output is the API answering again and clears it; a
// local command's reply (/context) ran no inference and proves nothing. A
// subagent's frames are not the agent's turn. Forgotten where doneAt is.
func (f Fleet) withTurnOutcome(id string, ev core.Event) Fleet {
	failed := ev.Kind == core.KindAPIError
	if ev.Subagent != nil || (!failed && !ev.ModelOutput()) {
		return f
	}
	a, ok := f.agents[id]
	if !ok || a.failed == failed {
		return f
	}
	a.failed = failed
	f = f.copy()
	f.agents[id] = a
	return f
}

// notDone forgets the last turn's done summary, so the agent's own new-turn
// content - prose, a tool call, or reasoning - drops the `✻ … done` line.
// WithStatus's working→idle→working report edge normally ends it, but that never
// fires for a turn Wake did not initiate: --brief self-starts a turn and a long
// job goes idle between turns, so stateLocked reports idle (`!owed`) while the
// agent works and no working report arrives. The event stream is the only
// observable of that turn here - the mirror of WithStatus reconciling inDM off
// the report because a gap can eat the KindTurnEnd. Its callers gate on
// ev.Subagent==nil: a subagent streams past the parent's result and is not the
// agent's own turn. A streaming preview forgets it too, but on the DM's own
// showsDone - a partial never reaches fold.
func (a Agent) notDone() Agent {
	a.doneAt, a.turnDur = time.Time{}, 0
	return a
}
