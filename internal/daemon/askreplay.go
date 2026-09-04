package daemon

// Handing a newly attached client the fleet's outstanding asks - split out of
// server.go to keep it under the hard max.

// replayPendingAsks hands a newly attached client every ask still outstanding
// across the fleet, each as the ordinary rpc.FrameEvent a client watching live
// would have gotten for it - so its own observe/Cards.Add rebuilds the real
// card, rather than internal/ui.Cards.Reconcile's bare permission stand-in
// from RequestIDs alone (an id and nothing about what it asks, whose Allow is
// a silent FrameAllow on a question). Unicast, and called before addClient so
// it can never race a broadcast of the same ask.
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
