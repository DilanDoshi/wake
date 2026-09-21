package daemon

// The fleet report: the whole fleet as one rpc.Status, assembled for every
// status reply and push. Split from server.go, which owns the accept loop and
// the dispatch - this owns what a report *is*, and it is where the team order is
// maintained and shipped (orderTeams, team.go).

import (
	"os"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// fleet is the whole fleet as one Status, and the sessions that recently left
// it - so a client learns how one ended rather than watching a row vanish, and
// can still learn it after the announcement it missed.
//
// The live agents and the remembered endings are read under one lock, which is
// what makes the two halves consistent with each other: register and retire
// each move an id between them in a single locked step, so no id is ever in
// both and none is ever in neither.
func (s *server) fleet() rpc.Status {
	st := rpc.Status{Running: true, PID: os.Getpid(), Socket: s.socket}

	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	st.Sessions = append(st.Sessions, s.recent...)
	s.mu.Unlock()

	for _, a := range agents {
		st.Sessions = append(st.Sessions, a.snapshot())
	}
	sortSessions(st.Sessions)
	st.Teams = s.orderTeams(st.Sessions)

	// The park book, on its own list. Read outside s.mu because it has its own
	// lock and holds no agent - and reported by a *running* daemon rather than
	// only by FleetOnDisk, because that is what makes /resume work in a room
	// that has been open since before anything was parked.
	st.Parked = parkedStatuses(s.parked.records())
	sortSessions(st.Parked)
	return st
}
