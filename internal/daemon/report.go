package daemon

// The fleet report: the whole fleet as one rpc.Status, assembled for every
// status reply and push. Split from server.go, which owns the accept loop and
// the dispatch - this owns what a report *is*, and it is where the team order is
// maintained and shipped (orderTeams, team.go).

import (
	"os"

	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/version"
)

// replyStatus answers a request. It is never broadcast: a client waiting for
// the answer to its own question must not be handed an announcement that was
// already in flight when it asked. See rpc.FrameStatusPush.
func (s *server) replyStatus(c *client) {
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	st := s.fleet()
	c.enqueue(rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st})
}

// pushStatus is the same report sent unasked. It is never a reply.
func (s *server) pushStatus() {
	s.reportMu.Lock()
	defer s.reportMu.Unlock()
	st := s.fleet()
	s.broadcast(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
}

// fleet is the whole fleet as one Status, and the sessions that recently left
// it - so a client learns how one ended rather than watching a row vanish, and
// can still learn it after the announcement it missed.
//
// The live agents and the remembered endings are read under one lock, which is
// what makes the two halves consistent with each other: register and retire
// each move an id between them in a single locked step, so no id is ever in
// both and none is ever in neither.
func (s *server) fleet() rpc.Status {
	st := rpc.Status{Running: true, PID: os.Getpid(), Socket: s.socket, Build: version.Build()}

	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	st.Sessions = append(st.Sessions, s.recent...)
	s.mu.Unlock()

	held := make(map[string]bool, len(agents))
	for _, a := range agents {
		st.Sessions = append(st.Sessions, a.snapshot())
		held[a.conversation()] = true
	}
	sortSessions(st.Sessions)
	st.Teams = s.orderTeams(st.Sessions)

	// The park book, on its own list. Read outside s.mu because it has its own
	// lock and holds no agent - and reported by a *running* daemon rather than
	// only by FleetOnDisk, because that is what makes /resume work in a room
	// that has been open since before anything was parked.
	// A ⌃C row is already in Sessions, so its own record is not listed again:
	// /resume all would send two wakes for one conversation.
	for _, p := range parkedStatuses(s.parked.records()) {
		if !held[p.ID] {
			st.Parked = append(st.Parked, p)
		}
	}
	sortSessions(st.Parked)
	return st
}
