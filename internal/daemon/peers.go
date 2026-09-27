package daemon

// The machine's other Claude sessions, asked of an idle agent.
//
// A bare /list-agents is a local command (num_turns 0, $0) that lists every
// session sharing this machine (2026-09-27-at-menu-findings.md §1), so any one
// agent's answer serves every client. It goes out as a probe (probe.go):
// idle-gated, its reply suppressed at fanOut. One is in flight at a time and
// every client that asks meanwhile gets that answer; with no agent able to
// answer now, the last listing answers at once - asked per menu opening, so no
// wait and no timer.

import (
	"slices"
	"sync"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// peerBook is the ask in flight and the last listing. Its lock is taken before
// s.mu or any a.mu and never while holding one.
type peerBook struct {
	mu      sync.Mutex
	asked   *agent    // whose /list-agents is in flight, nil for none
	waiting []*client // every client owed its answer

	self  string
	peers []core.Peer // replaced whole, never edited: frames share it
	at    time.Time   // when self and peers were taken; zero for never
}

// askPeers answers one client's FramePeers: it joins the ask in flight, starts
// one on an agent that can answer now, or answers at once from the last
// listing when none can.
func (s *server) askPeers(c *client) {
	b := &s.peers
	b.mu.Lock()
	defer b.mu.Unlock()
	if !slices.Contains(b.waiting, c) {
		b.waiting = append(b.waiting, c)
	}
	if b.asked != nil {
		return
	}
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()
	for _, a := range agents {
		if a.askPeers() {
			b.asked = a
			return
		}
	}
	b.answerLocked(b.lastLocked())
}

// askPeers queues a bare /list-agents if this agent can answer one now: idle,
// not stopping, not the manager (launched apart, with no tools; whether it
// answers a local command is unrecorded), and its last init advertised the
// command, so a claude without it is never sent a line it would answer in the
// conversation.
func (a *agent) askPeers() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stateLocked(time.Now()) != rpc.StateIdle || a.stopped || a.name == core.ManagerName ||
		!slices.Contains(a.commands, listAgentsVerb) {
		return false
	}
	return a.queueProbeLocked(peersProbe, slashPrefix+listAgentsVerb)
}

// peersAnswered takes a's absorbed /list-agents reply. A listing it can read
// becomes the last one and answers a's askers; one it cannot answers them
// empty rather than with rows it might have wrong.
func (s *server) peersAnswered(a *agent, reply string) {
	b := &s.peers
	b.mu.Lock()
	defer b.mu.Unlock()
	self, peers, ok := core.PeersFromListAgents(reply)
	if ok {
		b.self, b.peers, b.at = self, peers, time.Now()
	}
	if b.asked != a {
		return
	}
	if !ok {
		b.answerLocked(rpc.Frame{Kind: rpc.FramePeersReply, Peers: &rpc.PeersFrame{}})
		return
	}
	b.answerLocked(b.lastLocked())
}

// peersGone answers from the last listing an ask whose agent's session ended
// before it replied, so the next ask starts afresh rather than joining it.
func (s *server) peersGone(a *agent) {
	b := &s.peers
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.asked == a {
		b.answerLocked(b.lastLocked())
	}
}

// lastLocked is the last listing as a reply, aged now. The caller holds b.mu.
func (b *peerBook) lastLocked() rpc.Frame {
	p := &rpc.PeersFrame{Self: b.self, Peers: b.peers}
	if !b.at.IsZero() {
		p.AgeMS = time.Since(b.at).Milliseconds()
	}
	return rpc.Frame{Kind: rpc.FramePeersReply, Peers: p}
}

// answerLocked sends f to every waiting client and closes the ask. The caller
// holds b.mu; enqueue never blocks.
func (b *peerBook) answerLocked(f rpc.Frame) {
	for _, c := range b.waiting {
		c.enqueue(f)
	}
	b.waiting, b.asked = nil, nil
}
