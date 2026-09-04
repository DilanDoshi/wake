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
// (pendingProbes, swallowTurnEnd, confirmedEffort, probed, probeWanted) live on
// the agent and are written only under a.mu. It is also the daemon's only
// unprompted stdin write, so probeEffort refuses to send one while a real turn
// is owed - wantProbe/probeIfWanted defer it to the next idle instead of
// dropping it. Split from agent.go/effort.go as its own subject.

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// probeEffort queues a bare /model to read the session's reasoning level back -
// a local CLI reply (num_turns:0, $0) the daemon suppresses. Best-effort: it is
// skipped for an agent that is gone or blocked on an ask, whose stdin is a
// closed decision, or that owes a turn - this is the daemon's only unprompted
// stdin write, and one sent while a real turn is in flight is what let its
// reply interleave with that turn's own frames and have absorbProbe swallow
// the wrong turn end. Dropped silently if the queue is full (the level simply
// does not refresh this cycle). Reports whether it queued, so a deferred
// caller (wantProbe, probeIfWanted) knows whether to keep waiting. The reply
// is consumed by absorbProbe.
func (a *agent) probeEffort() bool {
	if a.blockedOnAsk() || a.turnOwed() {
		return false
	}
	select {
	case <-a.gone:
		return false
	default:
	}
	select {
	case a.in <- pending{probe: true, frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: a.id, Text: slashPrefix + modelVerb}}:
		return true
	default:
		return false
	}
}

// turnOwed reports whether this agent currently owes a turn end - probeEffort's
// idle gate. The caller takes no lock.
func (a *agent) turnOwed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.owed
}

// wantProbe requests a startup or re-probe for the next time this agent is
// idle. Called while a turn it belongs to is normally still in flight (the
// startup probe's own init is that turn's header; the /effort and /model
// re-probe follows noteSent in the same breath), so the common case defers:
// probeWanted is set and fanOut fires it from probeIfWanted once that turn's
// end is observed. If the agent is already idle when this runs - the turn
// ended before the trigger reached this goroutine - there is no future turn
// end to catch a deferred request, so it fires now instead; a failure there
// still falls back to probeWanted; a future turn's end retries it.
func (a *agent) wantProbe() {
	a.mu.Lock()
	owed := a.owed
	a.probeWanted = owed
	a.mu.Unlock()
	if !owed && !a.probeEffort() {
		a.mu.Lock()
		a.probeWanted = true
		a.mu.Unlock()
	}
}

// probeIfWanted fires a probe deferred by wantProbe, once this agent's turn
// end has been observed. Called from fanOut after observe returns - never
// from inside it, which holds a.mu. Left armed if probeEffort could not send
// it (a fresh turn raced the old one's end, say), so the next turn end retries.
func (a *agent) probeIfWanted() {
	a.mu.Lock()
	wanted := a.probeWanted
	a.mu.Unlock()
	if wanted && a.probeEffort() {
		a.mu.Lock()
		a.probeWanted = false
		a.mu.Unlock()
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
// arms swallowTurnEnd, which carries the window one frame further so the probe
// turn's end is swallowed too and decrements the counter - so two probes in
// flight suppress two replies, not one. The agent's state never moves for a
// question the operator did not ask.
func (a *agent) absorbProbe(ev core.Event) (suppress, publish bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pendingProbes > 0 && ev.Kind == core.KindAssistantText && core.IsModelReply(ev.Text) {
		// Armed whether or not the level parses. Arming only inside the ok
		// branch below used to leave a reply this build cannot read as neither
		// - not published, and its window never closed - so pendingProbes
		// never came back down and stuck every later turn's own end under a
		// window that could never legitimately claim it again.
		a.swallowTurnEnd = true
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
			return true, true
		}
		return true, false
	}
	// !a.owed is what keeps this from being the probe's own only by
	// assumption: probeEffort refuses to send while a turn is owed, so a
	// reply's genuine turn end always arrives with none in flight, and a real
	// turn racing the reply (a new send landing before it answers) is read
	// here and let through instead of swallowed - the failure this trades for
	// is a leaked probe reply, never a real turn end eaten and its agent
	// stuck owing one forever.
	if a.swallowTurnEnd && !a.owed && ev.Kind == core.KindTurnEnd {
		a.swallowTurnEnd = false
		if a.pendingProbes > 0 {
			a.pendingProbes--
		}
		return true, false
	}
	return false, false
}

// firstInit reports whether ev is this session's init and no probe has fired
// yet, marking it fired. init is a turn header - a spawned-and-unprompted
// session sends none at all (session.go) - so this is the header of the
// operator's first turn, not a frame that precedes one; wantProbe is what
// defers the startup probe behind that turn rather than sending it into it.
// Every later turn carries its own init, which this ignores.
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
