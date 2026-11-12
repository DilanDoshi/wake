package daemon

// Noting what an agent starts, and sweeping it when the fleet ends. world.go
// holds what may be signalled and why.

import (
	"context"
	"errors"
	"os"
	"slices"

	"github.com/DilanDoshi/wake/internal/core"
)

// listProcesses is one bounded ps.
func listProcesses() ([]procRow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	return processRows(ctx)
}

// noteWorld notes what this running agent has started. It signals nothing, and a
// listing that fails notes nothing.
func (a *agent) noteWorld() {
	a.mu.Lock()
	gone := a.ended || a.parked
	a.mu.Unlock()
	if gone {
		return
	}
	rows, err := listProcesses()
	if err != nil {
		logf("wake: session %s: what it started is not noted for its ending: %v", a.id, err)
		return
	}
	a.noteWorldFrom(rows)
}

// noteWorldFrom is noteWorld over a listing already taken, for a fleet at once.
// A parked or ended agent has no process, so its group number may be anyone's.
func (a *agent) noteWorldFrom(rows []procRow) {
	pgid := a.sess.Pgid()
	a.mu.Lock()
	if a.ended || a.parked {
		a.mu.Unlock()
		return
	}
	next := a.world.refreshed(rows, pgid)
	var started []int
	for _, g := range next.foreign {
		if !slices.ContainsFunc(a.world.foreign, func(h procGroup) bool { return h.pgid == g.pgid }) {
			started = append(started, g.pgid)
		}
	}
	a.world = next
	a.mu.Unlock()
	for _, g := range started {
		logf("wake: session %s started a process group of its own (%d); noted for its ending", a.id, g)
	}
}

func (a *agent) worldGroups() []procGroup {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.world.groups()
}

func (a *agent) foreignGroups() []procGroup {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]procGroup(nil), a.world.foreign...)
}

// beginWorldRefresh and worldRefreshAgain coalesce a burst of shell starts into
// one listing and at most one more: a start that lands during a listing asks for
// another, however many land.
func (a *agent) beginWorldRefresh() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.worldBusy {
		a.worldAgain = true
		return false
	}
	a.worldBusy = true
	return true
}

func (a *agent) worldRefreshAgain() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.worldAgain {
		a.worldAgain = false
		return true
	}
	a.worldBusy = false
	return false
}

// noteWorldEvent notes the agent's world when claude says a background shell
// started, so a shell that exits before the ending still has its group held, and
// looks again at a stopped agent when its turn ends.
func (s *server) noteWorldEvent(a *agent, ev core.Event) {
	if ev.Kind == core.KindTurnEnd {
		a.endIfHeldOpen(true)
		return
	}
	if ev.Task == nil || ev.Task.Phase != core.TaskStarted || ev.Task.Kind != core.TaskShell {
		return
	}
	if !a.beginWorldRefresh() {
		return
	}
	s.start(func() {
		for {
			a.noteWorld()
			if !a.worldRefreshAgain() {
				return
			}
		}
	})
}

// noteFleet notes every running agent from one listing, before any is stopped.
func (s *server) noteFleet(agents []*agent) {
	var live []*agent
	for _, a := range agents {
		if !a.finished() {
			live = append(live, a)
		}
	}
	if len(live) == 0 {
		return
	}
	rows, err := listProcesses()
	if err != nil {
		logf("wake: what the fleet started is not noted for its ending: %v", err)
		return
	}
	for _, a := range live {
		a.noteWorldFrom(rows)
	}
}

// sweepWorld ends what a non-parked ending left that retire's sweep of the
// agent's own group cannot reach: the groups its members led, and anything an
// earlier life of this session left when it parked.
func (s *server) sweepWorld(a *agent) {
	s.sweepGroups(append(a.foreignGroups(), s.worlds.take(a.id, a.conversation())...))
}

// sweepFleet is the daemon's last act on what it started: everything held for a
// parked agent, and whatever a taken agent's retire did not get to.
func (s *server) sweepFleet(agents []*agent) {
	groups := s.worlds.takeAll()
	for _, a := range agents {
		groups = append(groups, a.worldGroups()...)
	}
	s.sweepGroups(groups)
}

// sweepGroups signals each group the fresh listing proves is still the fleet's.
func (s *server) sweepGroups(groups []procGroup) {
	if len(groups) == 0 {
		return
	}
	rows, err := listProcesses()
	if err != nil {
		// Held again, so the daemon's exit can try once more: nothing is signalled on a guess.
		s.worlds.hold([]string{unsweptID}, groups)
		logf("wake: %d process group(s) the fleet started are left running, since none can be proved its own without a listing: %v", len(groups), err)
		return
	}
	for _, pgid := range sweepable(rows, groups) {
		if err := core.KillGroup(pgid); err != nil && !errors.Is(err, os.ErrProcessDone) {
			logf("wake: process group %d, started by the fleet, could not be ended: %v", pgid, err)
		}
	}
}

// unsweptID is the ledger key of groups a failed listing could not prove.
const unsweptID = "unswept"
