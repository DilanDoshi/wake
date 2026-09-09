package daemon

// Handing a newly attached client the fleet's outstanding asks - split out of
// server.go to keep it under the hard max.

import "github.com/DilanDoshi/wake/internal/rpc"

// replayPendingAsks hands a newly attached client every ask still outstanding
// across the fleet, each as the ordinary rpc.FrameEvent a client watching live
// would have gotten for it - so its own observe/Cards.Add rebuilds the real
// card, rather than internal/ui.Cards.Reconcile's bare permission stand-in
// from RequestIDs alone (an id and nothing about what it asks, whose Allow is
// a silent FrameAllow on a question). Unicast, and called *after* addClient
// (server.go): the client is subscribed first, so an ask arriving during the
// replay reaches it live too - a double the client dedups on (AgentID,
// RequestID) - where replaying first would let it fall between the snapshot and
// the subscribe and be missed. This covers only an ask outstanding when a
// client attaches, not one a live client later drops from its own queue.
func (s *server) replayPendingAsks(c *client) {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()

	for _, a := range agents {
		for _, f := range a.pendingAskFrames() {
			c.enqueue(f)
		}
	}
}

// pendingAskFrames is this agent's outstanding asks as the ordinary
// rpc.FrameEvent a live client would have gotten - see replayPendingAsks.
func (a *agent) pendingAskFrames() []rpc.Frame {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.pending) == 0 {
		return nil
	}
	frames := make([]rpc.Frame, len(a.pending))
	for i, p := range a.pending {
		ev := p.event
		frames[i] = rpc.Frame{Kind: rpc.FrameEvent, SessionID: a.id, Event: &ev}
	}
	return frames
}
