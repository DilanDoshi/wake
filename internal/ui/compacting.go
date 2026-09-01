package ui

// Which sessions are inside a compaction, and when each began.
//
// Held on App and folded from the two notices the airlock resolves a
// compaction's bracketing status frames to (NoticeCompacting, NoticeCompacted).
// On App rather than Agent or Fleet for tails.go's reason: it is set and cleared
// by two events a session sends *between* turns, and a map keyed by session id
// keeps Agent comparable for Observe's now==was. The DM reads it at draw time
// through WithCompacting; the ticker reads anyCompacting.
//
// Indeterminate by necessity. The wire carries the "compacting" start flag and
// the outcome, never a progress figure - Claude Code's own bar computes its
// percentage inside its interactive TUI, off nothing on the stream - so all this
// holds is the start time: enough to drive the shimmer, and nothing to draw a
// bar from.

import (
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// observeCompaction folds a compaction's bracketing notices: the start stamps
// the session's begin time, the outcome clears it. Every other event passes
// through untouched - including the several result frames a compaction emits
// while it runs, which is why the end keys on the outcome notice and nothing
// else. Immutable: the map is copied on a change.
func (a App) observeCompaction(sessionID string, ev core.Event) App {
	if ev.Kind != core.KindSystem {
		return a
	}
	switch ev.Notice {
	case core.NoticeCompacting:
		a.compacting = withCompacting(a.compacting, sessionID, clock())
	case core.NoticeCompacted:
		a.compacting = withoutCompacting(a.compacting, sessionID)
	}
	return a
}

// compactingSince is when the session's compaction began, or the zero time when
// none is running - what the DM draws its compacting line from.
func (a App) compactingSince(sessionID string) time.Time { return a.compacting[sessionID] }

// anyCompacting reports whether any session is mid-compaction, which is what the
// heartbeat ticker asks so the shimmer keeps moving while a compaction runs even
// though no turn is in flight.
func (a App) anyCompacting() bool { return len(a.compacting) > 0 }

// pruneCompacting drops any session that is not idle or working, so a compaction
// whose outcome never arrives cannot leave the line up and the ticker running
// forever. A compaction runs *between* turns, with the session idle - nothing is
// owed, since each of its result frames clears the turn - so it is never silent
// or blocked while it runs, and any state but idle or working means the process
// has gone or wedged and the outcome is not coming. A compaction that finishes
// normally is already cleared by its outcome notice; this is only the backstop
// for the one that never sends one. Immutable, a no-op when nothing needs it.
func (a App) pruneCompacting() App {
	if len(a.compacting) == 0 {
		return a
	}
	for id := range a.compacting {
		if agent, ok := a.fleet.Agent(id); ok && liveState(agent.State) {
			continue
		}
		a.compacting = withoutCompacting(a.compacting, id)
	}
	return a
}

// liveState is whether a session is in a state a compaction could still be
// running in - idle, where it runs, or working. Ended, parked, silent and
// blocked are not, so a compacting entry left in any of them is stale.
func liveState(state string) bool {
	return state == rpc.StateWorking || state == rpc.StateIdle
}

func withCompacting(m map[string]time.Time, id string, since time.Time) map[string]time.Time {
	next := make(map[string]time.Time, len(m)+1)
	for k, v := range m {
		next[k] = v
	}
	next[id] = since
	return next
}

func withoutCompacting(m map[string]time.Time, id string) map[string]time.Time {
	if _, ok := m[id]; !ok {
		return m
	}
	next := make(map[string]time.Time, len(m))
	for k, v := range m {
		if k != id {
			next[k] = v
		}
	}
	return next
}
