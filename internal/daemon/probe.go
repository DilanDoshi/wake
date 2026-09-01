package daemon

// The effort probe: how the daemon reads a session's reasoning level back, and
// keeps the reply invisible.
//
// Effort is on no frame Claude sends unasked, so the only way to confirm a level
// is to ask - a bare /model, whose reply names it (`Current model: … (effort:
// xhigh)`) and which the CLI answers locally (num_turns:0, $0, no inference).
// probeEffort sends it, absorbProbe swallows the reply at fanOut before it
// reaches a client, and the level lands on agent.confirmedEffort. The command
// counts as no turn (apply.go skips noteSent) and the fields it touches
// (pendingProbes, swallowTurnEnd, confirmedEffort, probed) live on the agent and
// are written only under a.mu. Split from agent.go/effort.go as its own subject.

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// probeEffort queues a bare /model to read the session's reasoning level back -
// a local CLI reply (num_turns:0, $0) the daemon suppresses. Best-effort: it is
// skipped for an agent that is gone or blocked on an ask, whose stdin is a
// closed decision, and dropped silently if the queue is full (the level simply
// does not refresh this cycle). The reply is consumed by absorbProbe.
func (a *agent) probeEffort() {
	if a.blockedOnAsk() {
		return
	}
	select {
	case <-a.gone:
		return
	default:
	}
	select {
	case a.in <- pending{probe: true, frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: a.id, Text: slashPrefix + modelVerb}}:
	default:
	}
}

// incProbe and decProbe open and close one probe's suppression window. The
// window is opened before /model reaches stdin and closed if the write fails,
// so a probe that never went out expects no reply.
func (a *agent) incProbe() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pendingProbes++
}

func (a *agent) decProbe() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pendingProbes > 0 {
		a.pendingProbes--
	}
}

// absorbProbe consumes a /model probe's reply so it never reaches a client, and
// reports whether the caller should publish the newly confirmed effort.
//
// It keys on the probe's own reply - an assistant frame carrying the /model
// text - not on a bare in-flight flag. Keying on the reply's shape is what makes
// it safe for a probe to be armed on another goroutine: a previous turn's frames
// still draining here do not match, so they pass through untouched. Each reply
// records the level (last one wins) and arms swallowTurnEnd, which carries the
// window one frame further so the probe turn's end is swallowed too and
// decrements the counter - so two probes in flight suppress two replies, not
// one. The agent's state never moves for a question the operator did not ask.
func (a *agent) absorbProbe(ev core.Event) (suppress, publish bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pendingProbes > 0 && ev.Kind == core.KindAssistantText && core.IsModelReply(ev.Text) {
		// A /model reply always names the level, so requiring the (effort: …)
		// clause as well as the "Current model:" prefix is what keeps a
		// coincidental line - a with-argument /effort's own confirmation, say -
		// from being mistaken for the probe's reply and closing the window early.
		if lvl, ok := core.EffortFromModelReply(ev.Text); ok {
			a.confirmedEffort = lvl
			// The same reply names the model; read it back for the status bar so a
			// runtime /model shows at once rather than at the next turn's init.
			if model, ok := core.ModelFromModelReply(ev.Text); ok {
				a.confirmedModel = model
			}
			a.swallowTurnEnd = true
			return true, true
		}
	}
	if a.swallowTurnEnd && ev.Kind == core.KindTurnEnd {
		a.swallowTurnEnd = false
		if a.pendingProbes > 0 {
			a.pendingProbes--
		}
		return true, false
	}
	return false, false
}

// firstInit reports whether ev is this session's init and no probe has fired
// yet, marking it fired. The init is the one frame that arrives before any
// input, so it is where the startup effort probe belongs; every later turn
// carries its own init, which this ignores.
func (a *agent) firstInit(ev core.Event) bool {
	if ev.Kind != core.KindSystem || ev.Session == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.probed {
		return false
	}
	a.probed = true
	return true
}
