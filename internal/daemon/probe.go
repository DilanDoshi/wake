package daemon

// The effort probe: how the daemon reads a session's reasoning level back, and
// keeps the reply invisible.
//
// Effort is on no frame Claude sends unasked, so the only way to confirm a level
// is to ask - a bare /model, whose reply names it (`Current model: … (effort:
// xhigh)`) and which the CLI answers locally (num_turns:0, $0, no inference).
// tryProbe sends it, absorbProbe swallows the reply at fanOut before it
// reaches a client, and the level lands on agent.confirmedEffort. The command
// counts as no turn (apply.go skips noteSent) and the fields it touches
// (pendingProbes, swallowTurnEnd, confirmedEffort, probed, probeWanted) live on
// the agent and are written only under a.mu. It is also the daemon's only
// unprompted stdin write, so tryProbe refuses to send one while a real turn is
// owed - wantProbe/probeIfWanted defer it to the next idle instead of dropping
// it. Split from agent.go/effort.go as its own subject.

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// wantProbe marks a startup or re-probe due and fires it at once if the agent
// is already idle. Called while the turn it belongs to is normally still in
// flight (the startup probe's own init is that turn's header; the /effort and
// /model re-probe follows noteSent in the same breath), so the common case
// defers: probeWanted stays set and fanOut fires it from probeIfWanted once
// that turn's end is observed. If the agent is already idle when this runs -
// the turn ended before the trigger reached this goroutine - there is no future
// turn end to catch the request, so tryProbe fires it now instead.
func (a *agent) wantProbe() {
	a.mu.Lock()
	a.probeWanted = true
	a.mu.Unlock()
	a.tryProbe()
}

// probeIfWanted fires a due probe once this agent's turn end has been observed.
// Called from fanOut after observe returns - never from inside it, which holds
// a.mu. A no-op unless a probe is due and the agent is now idle.
func (a *agent) probeIfWanted() {
	a.tryProbe()
}

// tryProbe queues a bare /model to read the session's reasoning level back - a
// local CLI reply (num_turns:0, $0) absorbProbe suppresses - when one is due
// (probeWanted) and the agent is idle. It is the daemon's only unprompted stdin
// write, and one sent while a real turn is owed is what let its reply interleave
// with that turn's own frames, so it defers while owed or blocked on an ask
// (whose stdin is a closed decision). probeWanted is cleared only in the same
// locked step that queues the probe, so a re-probe requested by a concurrent
// wantProbe between two turn ends is never cleared without having fired.
// Best-effort past the idle gate: skipped for an agent that is gone, and dropped
// if the queue is full - the level does not refresh this cycle and the next turn
// end retries. The reply is consumed by absorbProbe.
func (a *agent) tryProbe() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.probeWanted || a.owed || len(a.pending) > 0 {
		return
	}
	select {
	case <-a.gone:
		return
	default:
	}
	select {
	case a.in <- pending{probe: true, frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: a.id, Text: slashPrefix + modelVerb}}:
		a.probeWanted = false
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
// arms swallowTurnEnd, which carries the window one frame further so the probe
// turn's own end is swallowed too and decrements the counter - so two probes in
// flight suppress two replies, not one. The end is swallowed only when it is a
// local command (num_turns==0, Event.LocalCommand): the arm is content-matched
// and a real turn's prose can begin "Current model:", but only the probe's own
// turn ran no inference, so a look-alike real turn's end passes through. The
// agent's state never moves for a question the operator did not ask.
func (a *agent) absorbProbe(ev core.Event) (suppress, publish bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pendingProbes > 0 && ev.Kind == core.KindAssistantText && core.IsModelReply(ev.Text) {
		// Content-matched, so a real turn whose prose merely begins "Current
		// model:" also arms and has this block suppressed (and, if it carries an
		// (effort: …) clause, records a level that the real probe's reply then
		// corrects). That mis-suppression of the block is pre-existing and not
		// what the LocalCommand gate below addresses - that gate protects only
		// the turn *end*, so a look-alike real turn keeps its end even though it
		// loses this one block.
		//
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
	// Swallow the probe's own turn end. The arm above fires on any
	// "Current model:" assistant frame, which a real turn's prose can match,
	// so the end is swallowed only when it is a local command (num_turns==0)
	// - the shape of the bare /model the daemon sends (bare-model.jsonl) and of
	// no real inference turn. A real turn whose text merely began "Current
	// model:" disarms here and passes through, and the actual probe's reply and
	// end follow and re-arm. Keying this on !a.owed instead ate a real turn's
	// end whenever a racing send had set owed, and keying it on the arm alone
	// ate the end of any real turn that started "Current model:".
	//
	// pendingProbes decrements only here, so it relies on the probe's own end
	// being a local command; that is the recorded shape of a bare /model, and
	// were it ever to run a turn the window would disarm without draining. An
	// operator's own num_turns==0 passthrough (/model <arg>, /clear) that armed
	// on a "Current model:" prefix would also be swallowed here, the same as it
	// was before this gate - a pre-existing limit of content-matched arming.
	if a.swallowTurnEnd && ev.Kind == core.KindTurnEnd {
		a.swallowTurnEnd = false
		if ev.LocalCommand {
			if a.pendingProbes > 0 {
				a.pendingProbes--
			}
			return true, false
		}
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
