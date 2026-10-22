package daemon

// stopForShutdown is shutdown's stop of one agent.
//
// Under a signal every agent whose stdin still takes a line is first sent the
// interrupt Esc sends, cancelling what is queued behind its turn too, so a turn
// in flight ends at once and the session parks inside the grace rather than
// being killed at its end and lost (owner's 2026-10-22 ruling). An interrupt
// between turns is a no-op claude answers (interrupt-idle.jsonl), so nothing
// judges which agents are busy. A message already queued is dropped: run after
// the interrupt, it would start a turn the grace then kills.
//
// The interrupt is written on its own goroutine because a write can block where
// a stop cannot, and the stop follows it there, so stdin closes behind the
// interrupt. A write that never returns leaves the agent to the grace's kill.
func (s *server) stopForShutdown(a *agent, interrupt bool) {
	if !interrupt || !a.inputOpen() {
		stopLogged(a)
		return
	}
	s.start(func() {
		if _, err := a.sess.InterruptAll(); err != nil {
			logf("wake: could not interrupt session %s on the way out: %v", a.id, err)
		}
		stopLogged(a)
	})
}

func stopLogged(a *agent) {
	if err := a.stop(); err != nil {
		logf("wake: stopping session %s: %v", a.id, err)
	}
}

// inputOpen reports whether this agent's stdin still takes a line: not stopped,
// ended or parked. An interrupt to one that is would only fail and be logged.
func (a *agent) inputOpen() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !a.stopped && !a.ended && !a.parked
}
