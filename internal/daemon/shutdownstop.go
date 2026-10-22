package daemon

import (
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// stopForShutdown is shutdown's stop of one agent.
//
// Under a signal a busy agent is interrupted first, as Esc would, so its turn
// ends at once and it parks inside the grace rather than being killed at the
// grace's end and lost (owner's 2026-10-22 ruling). An idle one is sent
// nothing: what claude does with an interrupt between turns is unrecorded.
//
// The interrupt is written on its own goroutine because a write can block where
// a stop cannot, and the stop follows it there, so stdin closes behind the
// interrupt. A write that never returns leaves the agent to the grace's kill.
func (s *server) stopForShutdown(a *agent, interrupt bool) {
	if !interrupt || !a.busy() {
		stopLogged(a)
		return
	}
	s.start(func() {
		if _, err := a.sess.Interrupt(); err != nil {
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

// busy reports a turn in flight: working, blocked on an ask, or quiet mid-turn.
func (a *agent) busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch a.stateLocked(time.Now()) {
	case rpc.StateWorking, rpc.StateBlocked, rpc.StateSilent:
		return true
	}
	return false
}
