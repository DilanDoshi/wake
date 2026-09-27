package daemon

// Probes: the local commands the daemon sends on its own, and how their replies
// are kept invisible.
//
// A probe is a bare slash command the CLI answers locally (num_turns:0, $0, no
// inference), sent to read back something no frame carries unasked. Each kind
// is its command, its reply's shape, and what the reply is for:
//
//	modelProbe  /model        the session's effort and model (effort.go)
//	peersProbe  /list-agents  the machine's other sessions (peers.go)
//
// A kind is added by naming it below, giving probeReply its matcher and
// absorbed its consequence. queueProbeLocked sends one only while the agent is
// idle, absorbProbe swallows its reply at fanOut before any client sees it,
// and the command counts as no turn (apply.go skips noteSent). The fields it
// touches (pendingProbes, swallowTurnEnd, confirmedEffort, probed, probeWanted)
// live on the agent and are written only under a.mu. A probe is the daemon's
// only unprompted stdin write, so one is never queued while a real turn is
// owed - the model probe's wantProbe/probeIfWanted defer it to the next idle
// instead of dropping it.

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// probeKind is which local command a probe is; notProbe is an operator's line.
type probeKind int

const (
	notProbe probeKind = iota
	modelProbe
	peersProbe
	probeKinds
)

// probeReply recognises each kind's reply. Content-matched, so a reply is
// claimed only while its own kind has one in flight.
var probeReply = [probeKinds]func(string) bool{
	modelProbe: core.IsModelReply,
	peersProbe: core.IsListAgentsReply,
}

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

// tryProbe queues a bare /model to read the session's reasoning level back when
// one is due (probeWanted) and the agent is idle. probeWanted is cleared only in
// the same locked step that queues the probe, so a re-probe requested by a
// concurrent wantProbe between two turn ends is never cleared without having
// fired. A probe skipped because the queue is full does not refresh this cycle;
// the next turn end retries. The reply is consumed by absorbProbe.
func (a *agent) tryProbe() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.probeWanted && a.queueProbeLocked(modelProbe, slashPrefix+modelVerb) {
		a.probeWanted = false
	}
}

// queueProbeLocked queues one probe and reports whether it did. One sent while
// a real turn is owed is what let a reply interleave with that turn's own
// frames, so it refuses while owed or blocked on an ask (whose stdin is a
// closed decision), for an agent that is gone, and on a full queue. The caller
// holds a.mu.
func (a *agent) queueProbeLocked(kind probeKind, text string) bool {
	if a.owed || len(a.pending) > 0 {
		return false
	}
	select {
	case <-a.gone:
		return false
	default:
	}
	select {
	case a.in <- pending{probe: kind, frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: a.id, Text: text}}:
		return true
	default:
		return false
	}
}

// incProbe and decProbe open and close one probe's suppression window. The
// window is opened before the command reaches stdin and closed if the write
// fails, so a probe that never went out expects no reply.
func (a *agent) incProbe(kind probeKind) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pendingProbes[kind]++
}

func (a *agent) decProbe(kind probeKind) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pendingProbes[kind] > 0 {
		a.pendingProbes[kind]--
	}
}

// absorbed takes a probe's own frame off the stream and carries out what its
// reply is for, reporting whether ev is kept from every client. Called by
// fanOut before observe, so a probe never moves this agent's state.
func (s *server) absorbed(a *agent, ev core.Event) bool {
	suppress, answered := a.absorbProbe(ev)
	switch answered {
	case modelProbe:
		s.broadcast(s.statusPush()) // the level and model it confirmed
	case peersProbe:
		s.peersAnswered(a, ev.Text)
	}
	return suppress
}

// absorbProbe consumes a probe's reply and its turn end so neither reaches a
// client, and names the kind whose reply the server must now act on - notProbe
// for a turn end, and for a /model reply that confirmed nothing.
//
// It keys on the probe's own reply - an assistant frame of its kind's shape,
// while that kind has one in flight - not on a bare in-flight flag. Keying on
// the reply's shape is what makes it safe for a probe to be armed on another
// goroutine: a previous turn's frames still draining here do not match, so they
// pass through untouched. Each reply arms swallowTurnEnd, which carries the
// window one frame further so the probe turn's own end is swallowed too and
// decrements its kind's counter - so two probes in flight suppress two replies,
// not one. The agent's state never moves for a question the operator did not
// ask.
func (a *agent) absorbProbe(ev core.Event) (suppress bool, answered probeKind) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if kind := a.replyKindLocked(ev); kind != notProbe {
		// Armed whether or not the reply parses. Arming only once it parsed used
		// to leave a /model reply this build cannot read neither published nor
		// closed, so its counter never came back down and stuck every later
		// turn's own end under a window nothing could legitimately claim again.
		a.swallowTurnEnd = kind
		if kind == modelProbe && !a.confirmModelLocked(ev.Text) {
			return true, notProbe
		}
		return true, kind
	}
	// Swallow the probe's own turn end - only when it is a local command
	// (num_turns==0), the shape of every probe's recorded end and of no real
	// inference turn. The arm is content-matched and a real turn's prose can
	// begin like a reply, so a look-alike real turn disarms here and passes
	// through, and the actual probe's reply and end follow and re-arm. Keying
	// this on !a.owed instead ate a real turn's end whenever a racing send had
	// set owed, and keying it on the arm alone ate the end of any real turn that
	// merely began "Current model:".
	//
	// The counter decrements only here, so it relies on the probe's own end
	// being a local command; were one ever to run a turn the window would disarm
	// without draining. An operator's own num_turns==0 passthrough that armed on
	// a look-alike reply would also be swallowed - a limit of content matching.
	if a.swallowTurnEnd != notProbe && ev.Kind == core.KindTurnEnd {
		kind := a.swallowTurnEnd
		a.swallowTurnEnd = notProbe
		if ev.LocalCommand {
			if a.pendingProbes[kind] > 0 {
				a.pendingProbes[kind]--
			}
			return true, notProbe
		}
	}
	return false, notProbe
}

// replyKindLocked is the kind in flight whose reply ev is, or notProbe. The
// caller holds a.mu.
func (a *agent) replyKindLocked(ev core.Event) probeKind {
	if ev.Kind != core.KindAssistantText {
		return notProbe
	}
	for kind := modelProbe; kind < probeKinds; kind++ {
		if a.pendingProbes[kind] > 0 && probeReply[kind](ev.Text) {
			return kind
		}
	}
	return notProbe
}

// confirmModelLocked records the level a /model reply names, and the model
// beside it, reporting whether the level parsed. Requiring the (effort: …)
// clause as well as the "Current model:" prefix keeps a coincidental line - a
// with-argument /effort's own confirmation, say - from being recorded as one.
// A real turn whose prose merely begins "Current model:" still has that block
// suppressed by absorbProbe's content match - a pre-existing limit the
// LocalCommand gate on the end does not address. The caller holds a.mu.
func (a *agent) confirmModelLocked(text string) bool {
	lvl, ok := core.EffortFromModelReply(text)
	if !ok {
		return false
	}
	a.confirmedEffort = lvl
	// The same reply names the model; read it back for the status bar so a
	// runtime /model shows at once rather than at the next turn's init.
	if model, ok := core.ModelFromModelReply(text); ok {
		a.confirmedModel = model
	}
	return true
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
