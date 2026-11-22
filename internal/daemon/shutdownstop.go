package daemon

// How the daemon stops one agent: shutdown's stop of each, and /quit.
//
// A stop interrupts first, as Esc does, so a turn in flight ends at once and an
// ask outstanding is withdrawn where a bare stdin close would deny it; a queued
// message is cancelled, since run after the interrupt it would start a turn the
// stop then kills. An interrupt between turns is a no-op claude answers
// (interrupt-idle.jsonl), so nothing judges which agents are busy. Closing stdin
// follows, and what the agent started is swept as it ends. Only an agent that
// ignored both is ended by force, once the grace has run (owner's 2026-11-22
// ruling; ⌃Q's interrupt is 2026-11-07's).

import (
	"context"
	"fmt"
	"time"
)

// interruptThenStop is that order for one agent. It is run on its own goroutine
// because a write can block where a stop cannot: stdin closes behind the
// interrupt, and a write that never returns leaves the agent to the grace's kill.
func interruptThenStop(a *agent) {
	if _, err := a.sess.InterruptAll(); err != nil {
		logf("wake: could not interrupt session %s on the way out: %v", a.id, err)
	}
	stopLogged(a)
}

// stopForShutdown is shutdown's stop of one agent.
func (s *server) stopForShutdown(a *agent, interrupt bool) {
	if !interrupt || !a.inputOpen() {
		stopLogged(a)
		if !interrupt {
			a.endIfHeldOpen(false) // a stop that waits out the grace for a shell it would then kill
		}
		return
	}
	s.start(func() { interruptThenStop(a) })
}

// quitAgent is /quit: the agent is interrupted, stopped and swept, and ended by
// force if it has not exited by the end of the grace. The world is noted while
// claude lives, since once it exits its shells are nobody's child. The grace is
// armed on a goroutine of its own and whatever becomes of the interrupt: an
// agent that reads nothing is the one it exists for. A second /quit starts
// nothing more, and an agent that has ended is refused.
func (s *server) quitAgent(a *agent) error {
	select {
	case <-a.gone:
		return fmt.Errorf("session %s has ended", a.id)
	default:
	}
	if !a.beginQuit() {
		return nil
	}
	s.forgetReady(a.id) // an operator's quit is not a crash for the next daemon to undo
	grace := quitGrace
	logf("wake: session %s is being quit: interrupted, then stopped, and ended by force if it outlasts %v", a.id, grace)
	s.start(func() {
		a.noteWorld()
		interruptThenStop(a)
	})
	s.start(func() { s.endAfterGrace([]*agent{a}, grace, false) })
	return nil
}

// endAfterGrace waits up to grace for each agent to end on its own, then notes
// the world of those that have not, so a group started during the grace is
// caught, and ends them by force. The wait is on the agent's real end, never a
// poll.
func (s *server) endAfterGrace(agents []*agent, grace time.Duration, parking bool) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	for _, a := range agents {
		select {
		case <-a.gone:
		case <-ctx.Done():
		}
	}
	var late []*agent
	for _, a := range agents {
		if !a.finished() {
			late = append(late, a)
		}
	}
	s.noteFleet(late)
	for _, a := range late {
		logf("wake: session %s did not end within %v of being stopped; ending its process group", a.id, grace)
		a.endForShutdown(parking)
	}
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
