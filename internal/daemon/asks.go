// One supervised session's permission asks: the ones it is blocked on, and the
// bookkeeping the daemon does about them. Split from agent.go, which crossed the
// 800-line hard max - the subject subagenttrack.go and agentstatus.go were each
// split out the same way.
//
// The daemon holds each ask rather than leaving it to the client because this is
// the only layer that has both facts at once: core sees the ask go past and
// forgets it, and a client can be told the ask exists and then answer it with a
// frame that carries no evidence of what it was answering. So the daemon watches
// the ask arrive and holds it until it is settled - the only place that can
// notice an answer settling it wrongly (see allow) or hand a late-attaching
// client the same event a live one got (see pendingAskFrames in askreplay.go).

package daemon

import "github.com/DilanDoshi/wake/internal/core"

// ask is one permission request an agent is blocked on: which one, and what it
// wants from the operator. An agent holds a slice of these - see agent.pending.
type ask struct {
	id string

	// event is the KindPermissionRequest this ask arrived as, retained so a
	// late-attaching client can be handed the same event a live one got (see
	// pendingAskFrames) rather than only rpc.SessionStatus.RequestIDs, from
	// which Cards.Reconcile can build only a bare permission stand-in.
	event core.Event
}

// addPending records one outstanding ask, oldest first. The caller holds a.mu.
//
// A duplicate id is not appended twice: a reattaching client's replay can
// deliver the same ask as an event a second time, and a doubled ask would keep
// the agent blocked after its one answer took only the first copy down.
func (a *agent) addPending(ev core.Event) {
	if ev.RequestID == "" {
		return
	}
	for _, p := range a.pending {
		if p.id == ev.RequestID {
			return
		}
	}
	a.pending = append(a.pending, ask{id: ev.RequestID, event: ev})
}

// withoutAsk is the pending set with one id removed, as a new slice. An empty
// id matches nothing, since pending never holds one - see addPending.
func withoutAsk(asks []ask, id string) []ask {
	out := make([]ask, 0, len(asks))
	for _, p := range asks {
		if p.id != id {
			out = append(out, p)
		}
	}
	return out
}

// noteAnswered takes down the one ask this answer settled, leaving any others
// the agent is still blocked on. An empty id settles nothing: an answer with no
// correlator names no ask.
func (a *agent) noteAnswered(requestID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = withoutAsk(a.pending, requestID)
}

// awaitsChoice reports whether the ask this id names is one whose answer has
// to ride inside the allow. It is asked *before* the answer goes out, because
// noteAnswered is about to forget which ask this was.
//
// An id this agent is not blocked on answers false. That is the honest reading
// rather than a cautious one: an answer naming an ask the daemon never saw, or
// one already settled, is not evidence that a choice is being dropped, and
// warning about it would put a notice on the screen for every stale answer a
// reattaching client sends.
func (a *agent) awaitsChoice(requestID string) bool {
	if requestID == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, p := range a.pending {
		if p.id == requestID {
			return p.event.Ask == core.AskChoice
		}
	}
	return false
}

// blockedOnAsk reports whether any permission ask is outstanding. apply reads
// it on the agent's input goroutine while observe writes pending on the fan-out
// one, so the read takes the lock. What the lock cannot close - an ask already
// in flight on stdout when apply's frame reaches stdin - is the process's own
// asynchrony, and no check on this side of the pipe can.
func (a *agent) blockedOnAsk() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pending) > 0
}

// pendingIDsLocked is the outstanding asks' ids, oldest first, or nil for none.
// The caller holds a.mu. nil rather than an empty slice so the field is omitted
// from the wire when nothing is blocked, matching what one empty id used to do.
func (a *agent) pendingIDsLocked() []string {
	if len(a.pending) == 0 {
		return nil
	}
	ids := make([]string, len(a.pending))
	for i, p := range a.pending {
		ids[i] = p.id
	}
	return ids
}
