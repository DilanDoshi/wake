package daemon

// The fleet report: the whole fleet as one rpc.Status, assembled for every
// status reply and push. Split from server.go, which owns the accept loop and
// the dispatch - this owns what a report *is*, the endings it carries after the
// fact (s.recent), and it is where the team order is maintained and shipped
// (orderTeams, team.go).

import (
	"os"
	"slices"

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
	st := s.fleet()
	s.broadcast(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	s.reportMu.Unlock()
	// Every change a wake reads - a /clear's conversation, an /effort - ends in
	// a push, so this is where the park-ready file catches up with them all.
	s.syncReady()
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
	root, _ := StateRoot()
	at := s.fleetSocket()
	st := rpc.Status{Running: true, PID: os.Getpid(), Socket: at, Fleet: fleetNameOf(root, at), Build: version.Build(), MaxAgents: s.agentCap()}

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
	// From the agents just listed, not a second snapshot: the name then comes
	// from the fleet this report lists, and no agent lock is taken before the
	// listing is.
	mgr, _ := managerIn(agents)
	st.ManagerClaude = s.managerClaudeOf(mgr)
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

// recentEndings is how many endings a status report carries after the fact.
// Enough that a client which blinked during a fleet-wide stop can still
// account for every session, small enough to be a courtesy rather than a log.
const recentEndings = 32

// rememberLocked records an ending. The caller holds s.mu, and that is the
// contract for the same reason forgetLocked's is: leaving s.agents and
// entering s.recent have to be one step, or the two are briefly both false and
// a status reply reports a session that is neither running nor ended. See
// retire.
func (s *server) rememberLocked(ended rpc.SessionStatus) {
	s.recent = appendRecent(s.recent, ended)
}

// forgetLocked drops any ending for an id, so a respawned session is not
// reported twice - once alive and once dead - in the same status.
//
// The caller holds s.mu, and that is the contract rather than a convenience:
// forgetting the ending and taking the id have to be one step, or a retire
// landing between them reinstates the ending the spawn just cleared. See
// register.
func (s *server) forgetLocked(id string) {
	s.recent = slices.DeleteFunc(s.recent, func(r rpc.SessionStatus) bool { return r.ID == id })
}

// appendRecent adds an ending, replacing any earlier one for the same session
// and dropping the oldest once the bound is reached. It returns a new slice
// rather than editing in place.
func appendRecent(recent []rpc.SessionStatus, ended rpc.SessionStatus) []rpc.SessionStatus {
	out := make([]rpc.SessionStatus, 0, len(recent)+1)
	for _, r := range recent {
		if r.ID != ended.ID {
			out = append(out, r)
		}
	}
	out = append(out, ended)
	if len(out) > recentEndings {
		out = out[len(out)-recentEndings:]
	}
	return out
}
