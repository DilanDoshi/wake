package ui

// What a /clear does to an open conversation. The command reaches the agent
// byte for byte (slash.go passes it through), claude mints a new session id and
// drops the conversation it had, and Wake sees that as a core.KindSessionReset -
// which DM.Append routes here.

import "github.com/DilanDoshi/wake/internal/core"

// clearedBySessionReset blanks the conversation back to a fresh one: the model
// dropped everything this transcript describes, so it is history the agent no
// longer has. Identity, size, the composer and the status bar stay - they are
// the session, and /clear changed the conversation, not the session. Everything
// else is an accumulation over the gone events: the subagent transcripts and the
// view onto one, the board of tasks the model set itself, the last-read marks,
// the tool-call state, the live run fold, and the preview of a block that will
// never complete. The empty re-render is one banner, the same one a brand-new DM
// draws, and resettleBoard reclaims the rows the board no longer needs.
//
// It does not re-ask history the way a rewind does (App.resetDM). A rewind keeps
// the session and has a tree to restore; a /clear leaves an empty successor, and
// the daemon has not yet relearned which id it is written under - agent.go clears
// claudeID and relearns on the next event - so a read now would return the
// pre-clear transcript. A later reopen reads the fresh one off disk, the ordinary
// path any conversation takes.
func (d DM) clearedBySessionReset() DM {
	d.events = chunked[core.Event]{}
	d.subs, d.viewing = nil, ""
	d.checklist = checklist{}
	d.marks = nil
	d.calls, d.outcomes, d.opened = nil, nil, nil
	d.runOpen, d.runKey, d.runTally = nil, "", nil
	d.partial = d.partial.cleared()
	d.tr = d.tr.replace(renderTranscript(d)).toBottom()
	return d.resettleBoard()
}

// forgetHistoryOnReset drops a pending pre-clear history ask when a /clear lands
// (a no-op for every other event), so a reply still in flight is not folded onto
// the pane clearedBySessionReset just blanked. The blanking returns events.len()
// to the count askHistory recorded at open, so the count guard alone would read
// "nothing arrived since" and Before would prepend the pre-clear transcript;
// forgetting the ask is what lets historyArrived's membership check drop it.
func (a App) forgetHistoryOnReset(sessionID string, ev core.Event) App {
	if ev.Kind == core.KindSessionReset {
		return a.forgetHistoryAsk(sessionID)
	}
	return a
}
