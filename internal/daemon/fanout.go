package daemon

// Carrying one session's events to every attached client - split from spawn.go
// when the effort-probe wiring pushed that file past the hard max. The subject
// is its own: spawn.go starts a process, this forwards what the process says.

import (
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fanOut carries one session's events to every attached client, so a
// reattaching TUI joins the same stream.
//
// The frame is addressed by the id Wake spawned under, not by the id on the
// event. Those differ after a /clear, which mints a new session id mid-process
// - and a client keyed on the frame would watch its session vanish and a
// stranger appear. The event keeps whatever core stamped it with, so nothing
// is lost: re-keying the roster is the registry's job (Phase 3) and this is
// the id every client already holds.
func (s *server) fanOut(a *agent) {
	defer s.retire(a)
	for ev := range a.sess.Events() {
		// An effort probe's reply is consumed here, before observe and before
		// the broadcast, so it never touches this agent's state and never
		// reaches a client. The one push it earns carries the confirmed level.
		if suppress, publish := a.absorbProbe(ev); suppress {
			if publish {
				s.broadcast(s.statusPush())
			}
			continue
		}
		a.observe(ev)
		// One Event, one pointer, shared by every client's copy of the
		// frame. Nothing mutates an Event after the airlock decodes it, and
		// at 30 sessions a copy per client per event is the difference
		// between fan-out being free and being the bottleneck.
		s.broadcast(rpc.Frame{Kind: rpc.FrameEvent, SessionID: a.id, Event: &ev})

		// An ask, an answer and a turn end each change what the roster draws
		// and what ⇧⇥ can find. Left to watchLiveness, that took up to one
		// 30-second tick, so an agent blocked on a permission request read as
		// idle and "next blocked" said nothing was waiting on anybody.
		//
		// After the event, never before it: the report names the ask by id and
		// the event is what carries what it is asking.
		//
		// changed() records the state it announces, so the watchdog does not
		// repeat this, and it broadcasts on a transition rather than per event.
		if a.changed() {
			s.broadcast(s.statusPush())
		}

		// The session's init arrives before any input, so it is where the
		// startup effort probe belongs - the one place Wake can read a level it
		// never chose. Fires once; the /effort re-probe is in apply.
		if a.firstInit(ev) {
			a.probeEffort()
		}
	}
}
