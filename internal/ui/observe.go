package ui

// Folding one agent's event into the model: what the room draws, what an open
// DM gets, and the events this model produces for itself. Split from app.go,
// which keeps the connection, the struct and the Update loop.

import "github.com/DilanDoshi/wake/internal/core"

// observe folds one agent's event: what it does to the fleet, what the room
// draws for it, and what an open DM gets whether the room wanted it or not.
//
// The DM is unfiltered and gets everything, which is the promise §8 makes about
// it - so this is not an else.
func (a App) observe(sessionID string, ev core.Event) App {
	if ev.Kind == core.KindRateLimit {
		// A quota heartbeat is a fact about now, not conversation content: it
		// goes to the timed notice above the composer and never into a
		// transcript or the fleet. See ratelimit.go.
		return a.rateLimited(ev)
	}

	// Read before the fold, which clears it on the turn end that belongs to the
	// same turn.
	inDM := a.fleet.inDM(sessionID)

	// The live checklist is folded before both readers below - the working line
	// and the DM transcript - off one snapshotted event. See Fleet.foldChecklist.
	a.fleet, ev = a.fleet.foldChecklist(sessionID, ev)

	// Both observables of the permission mode arrive as ordinary events, and
	// neither is drawn as one. Folded here rather than beside a renderer so a
	// receipt this client is not showing still corrects the belief.
	a = a.observedMode(sessionID, ev)
	// A rewind receipt is the same non-decision, one kind over. See rewind.go.
	a = a.noteRewind(sessionID, ev)
	a = a.forgetHistoryOnReset(sessionID, ev)

	var forRoom []core.Event
	a.fleet, forRoom = a.fleet.Observe(ev, sessionID)
	agent, _ := a.fleet.Agent(sessionID)
	if ev.Session != nil {
		// The model and the context figures reach the fleet on an init or a
		// result and never on a fleet report, and the bar draws all three - so
		// without this the stored conversation falls behind at every turn
		// boundary and the pane re-renders the bar, filesystem walk included,
		// on every frame until something else happens to correct it.
		// docs/notes/bugs.md BUG-5, third path.
		//
		// Gated on ev.Session rather than folded into every event, and that is
		// the whole reason it is here rather than in refreshedAgents: an
		// Agent's TurnTokens moves on every streamed token, so a walk keyed on
		// the whole Agent would copy the dms map per token - the cost
		// App.wants exists to avoid.
		a = a.refreshedBar(sessionID)
	}

	for _, e := range forRoom {
		switch e.Kind {
		case core.KindPermissionRequest, core.KindRequestWithdrawn:
			// Add promotes an ask and retires a withdrawn one, so both go
			// through the one seam that owns which asks are outstanding. Never
			// suppressed: a blocked agent needs the operator whichever pane
			// they were last typing in.
			a.cards = a.cards.Add(sessionID, e)
			if e.Kind != core.KindPermissionRequest {
				continue
			}
			// And the room says so, as well as the card - the card is the one
			// surface that *answers* (Cards.Undrawn), and this is the record
			// that it happened. Not gated on inDM: that rule keeps a private
			// conversation private, and an agent that has stopped and is
			// waiting is the room's own filter rather than an exception to it.
			a = a.withRoom(a.room.Append(e, agent))
		default:
			// A turn held in a DM stays in the DM. Fleet.sending says which
			// turns those are; the DM below gets everything either way.
			if inDM {
				continue
			}
			a = a.withRoom(a.room.Append(e, agent))
		}
	}
	if dm, ok := a.dms[sessionID]; ok && a.wants(sessionID, ev) {
		// Named from the fold above, which has already seen this frame - an
		// ending says what it ended only once the row is consulted. The rows
		// are carried across in the same write rather than a second one, and
		// held rather than projected per draw: chromeHeight counts them, so a
		// stored DM that does not have them re-sizes on every frame.
		a = a.withDM(sessionID, dm.Append(a.fleet.named(sessionID, ev)))
	}
	a = a.foldTail(sessionID, ev)
	return a
}

// appendEvent puts one event into the conversation that is open.
//
// It is the ingest path for events this model produced itself rather than read
// off the socket - which today is nothing but a test's, since a bang addresses
// its own conversation through bangResult.
func (a App) appendEvent(ev core.Event) App {
	if a.focus == "" {
		return a.withRoom(a.room.Append(ev, Agent{}))
	}
	return a.withDM(a.focus, a.dms[a.focus].Append(ev))
}
