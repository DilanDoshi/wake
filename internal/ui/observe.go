package ui

// Folding one agent's event into the model: what the room draws, what an open
// DM gets, and the events this model produces for itself. Split from app.go,
// which keeps the connection, the struct and the Update loop.

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

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
	if ev.Kind == core.KindAPIError {
		// A turn that failed on the API is infrastructure, not the model
		// speaking, so it never reaches the transcript as agent text: pop a
		// notice and remember the session for /reauth. See apierror.go.
		return a.apiErrored(sessionID, ev)
	}
	// A healthy turn clears any auth-failed mark, so /reauth never re-parks a
	// session that has already recovered. Cheap: a no-op unless it was marked.
	a = a.clearedAuthFailedOn(sessionID, ev)

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
	// And an MCP reply, which only the /mcp menu and a sign-in's sweep read.
	a = a.observedMCP(sessionID, ev)
	a = a.forgetHistoryOnReset(sessionID, ev)
	// A compaction brackets itself with two status frames; the DM draws a
	// "compacting…" line between them. See compacting.go.
	a = a.observeCompaction(sessionID, ev)
	// A message Wake sent reports its own fate; a completed/cancelled one frees
	// the agent to take the next queued message. Folded here for observedMode's
	// reason - it drives no renderer, only the queue. See queue.go.
	a = a.observeMessageState(sessionID, ev)

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

	// A DM-sent turn (inDM) is held out of the room only while its DM is on
	// screen and being read; once the reader leaves it - the pane stops being
	// drawn - the rest of the turn's prose flows to the room, so someone watching
	// the group chat does not miss a reply to a DM they walked away from. The
	// layout walk is gated on a room candidate existing, so a streamed partial
	// (which yields none) never pays for it - work per prose block, not per token,
	// and App.wants already walks it per token, so the marginal cost is nil.
	//
	// drawnConversations reads the committed layout (the one View draws), which
	// lags the terminal through the 80ms resize settle - so a reply landing during
	// a wide→narrow resize can briefly miss promotion and fall back to the roster
	// badge. The exact-visible set is an old-layout∩pending intersection out of
	// proportion to an 80ms edge App.wants already lives with; see deferred.md.
	dmDrawn := false
	if inDM && len(forRoom) > 0 {
		dmDrawn = a.drawnConversations()(sessionID)
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
			// A re-delivered ask - the daemon replaying at attach one this
			// client also got live in the subscribe-then-replay window - is
			// already in the room. The card dedups on (AgentID, RequestID); the
			// room line would not, so a second "needs you" would appear. Keyed
			// on what the room has actually announced, *not* on whether a card
			// exists: a Cards.Reconcile stand-in from a report's RequestIDs is a
			// card with no room line, so keying on the card would suppress the
			// one announce the canonical reattach does have.
			if _, announced := a.roomAsked[[2]string{sessionID, e.RequestID}]; announced {
				continue
			}
			// And the room says so, as well as the card - the card is the one
			// surface that *answers* (Cards.Undrawn), and this is the record
			// that it happened. Not gated on inDM: that rule keeps a private
			// conversation private, and an agent that has stopped and is
			// waiting is the room's own filter rather than an exception to it.
			a = a.markRoomAsked(sessionID, e.RequestID).withRoom(a.room.Append(e, agent))
		case core.KindCrossSession:
			// A peer's message, attributed to the sender rather than the
			// receiving session (crossSpeaker resolves FromName), and not held by
			// inDM: it is not the operator's own DM turn, so it belongs in the
			// room whatever the receiver was last sent from. The receiver is this
			// stream's own session (agent, resolved above), so the room heads the
			// line "sender → recipient"; "" when unknown drops the arrow.
			e.ToName = agent.Name
			a = a.withRoom(a.room.Append(e, a.fleet.crossSpeaker(e.FromName)))
		default:
			// A DM-sent turn stays private only while its DM is drawn; once the
			// reader has left it, its prose promotes to the room. Fleet.sending
			// says which turns are DM-sent; dmDrawn (above) whether the pane is
			// still on screen. The DM below gets everything either way.
			if inDM && dmDrawn {
				continue
			}
			a = a.withRoom(a.room.Append(e, agent))
		}
	}
	if dm, ok := a.dms[sessionID]; ok && a.wants(sessionID, ev) && !replayedUserEcho(ev) {
		// Named from the fold above, which has already seen this frame - an
		// ending says what it ended only once the row is consulted. The rows
		// are carried across in the same write rather than a second one, and
		// held rather than projected per draw: chromeHeight counts them, so a
		// stored DM that does not have them re-sizes on every frame.
		a = a.withDM(sessionID, dm.Append(a.fleet.named(sessionID, ev)))
	}
	a = a.foldBoard(sessionID, ev)
	return a
}

// markRoomAsked records that the room has announced a permission ask, so a
// re-delivered one (a replay of an ask also seen live) draws no second line.
// Copy-on-write like quitting; pruneRoomAsked retires an entry once its ask is
// no longer outstanding, so the set stays bounded rather than growing for the
// life of the process.
func (a App) markRoomAsked(sessionID, requestID string) App {
	next := make(map[[2]string]struct{}, len(a.roomAsked)+1)
	for k := range a.roomAsked {
		next[k] = struct{}{}
	}
	next[[2]string{sessionID, requestID}] = struct{}{}
	a.roomAsked = next
	return a
}

// pruneRoomAsked drops the announcement record of any ask the report no longer
// names outstanding - the same reconciliation Cards.Reconcile does on the same
// frame, so roomAsked tracks the fleet's live asks rather than every one ever
// raised. Safe because re-delivery of an ask (replay or the attach race) only
// happens while it is outstanding, so a retired entry is never needed again.
func pruneRoomAsked(asked map[[2]string]struct{}, st *rpc.Status) map[[2]string]struct{} {
	if len(asked) == 0 {
		return asked
	}
	live := make(map[[2]string]struct{}, len(asked))
	for _, s := range st.Sessions {
		for _, id := range s.RequestIDs {
			live[[2]string{s.ID, id}] = struct{}{}
		}
	}
	next := make(map[[2]string]struct{}, len(live))
	for k := range asked {
		if _, ok := live[k]; ok {
			next[k] = struct{}{}
		}
	}
	return next
}

// replayedUserEcho reports a replayed user frame the live DM feed drops.
// --replay-user-messages replays every inbound user message on the stream, so
// without this the DM's live feed fills with echoes: the operator's own sends
// (sendDM already drew the local echo - a second copy double-renders, and the
// DM does not de-duplicate), the manager's sends, and Claude's own user-frame
// echoes. The room already drops all of these (fold's typedByHand), and the DM
// live feed is the same conversation, so it drops them too and stays what it
// was before the flag - the single-source rule core/event.go's Echoed comment
// reserves for the App that owns the local echo.
//
// The cost, taken deliberately: a compaction summary and <local-command-stdout>
// are replayed user frames too, so they no longer land in an *open* DM as they
// stream - they return on reopen, which re-reads the transcript (history.go, an
// unaffected path), and a compaction is still announced live by its own system
// notice (NoticeContextCompacted). KindCrossSession is a different kind and is
// kept - the one inbound the flag exists to surface, with no local echo to
// double it.
func replayedUserEcho(ev core.Event) bool {
	return ev.Echoed && ev.Kind == core.KindUserText
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
