package ui

// A conversation that comes back with what it already said.
//
// `/resume` brought the process back and the pane opened with no account of
// itself: the model still had the whole conversation - claude keeps the
// transcript, which is why Wake can own almost no state - and the operator had
// none of it. The same emptiness greets a reattach, and a room reopened over a
// fleet that was running before this window existed.
//
// Wake does not read that file. The daemon does, through core's airlock, and
// answers with events - so this asks a question and folds an answer, which is
// the whole of the feature on this side.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// historyFailed names the write that could not happen, so the notice row says
// what was being asked for rather than only what the socket said about it.
const historyFailed = "asking what this conversation already said"

// askHistory requests the conversation on disk for a session, once.
//
// Only for a DM this client has just built: a conversation already holding
// events has either been read once or is live, and asking again would prepend a
// second copy of everything under it. `asked` is per client and per session,
// which is the same scope App.dms has - a reattach builds new DMs and asks
// again, which is right, because it is a new window with nothing in it.
func (a App) askHistory(id string) App {
	if _, asked := a.askedHistory[id]; id == "" || asked {
		return a
	}
	a = a.withHistoryAsked(id, a.dms[id].events.len())
	a.pendingHistory = append(append([]string(nil), a.pendingHistory...), id)
	return a
}

// takeHistoryAsks drains what openDMWith queued into one command.
//
// Queued rather than written where the conversation opens, because opening one
// is reached from six places - a key, a click, the ring, next-blocked, a fork
// arriving, a spawn arriving - and only two of them are in a position to return
// a tea.Cmd. One drain at the top of Update is one seam instead of six
// signatures, and it is the same shape App.write already has: one command,
// however many frames.
func (a App) takeHistoryAsks() (App, tea.Cmd) {
	if len(a.pendingHistory) == 0 {
		return a, nil
	}
	frames := make([]rpc.Frame, 0, len(a.pendingHistory))
	for _, id := range a.pendingHistory {
		frames = append(frames, rpc.Frame{Kind: rpc.FrameHistory, SessionID: id})
	}
	a.pendingHistory = nil
	return a, a.write(historyFailed, frames...)
}

// historyArrived folds the answer into the conversation it names.
//
// **Dropped if anything has arrived since the ask**, and that is the finding an
// adversarial review made against the first draft: this comment claimed the
// check and the code did not make it. The race is real rather than theoretical.
// claude writes a turn to its transcript *and* streams it, so an event that
// landed while the disk was being read is in both - folding then would draw it
// twice, in two places, with the older copy below the newer one.
//
// Losing the history is the safe side of that trade: an empty pane is what the
// operator had before this feature, and a conversation that appears to have
// happened twice is worse than one that appears not to have happened at all.
// It is also rare - the ask is written the moment a pane opens and answered off
// local disk.
//
// Also dropped if the conversation has gone: ⌃W keeps the transcript, so this
// is a DM that was never opened, or one whose client reattached mid-flight.
//
// And dropped if the ask was invalidated - a /clear blanks the pane and drops
// its ask (observe), so a pre-clear reply still in flight lands with the DM
// back at the empty count the ask recorded at open. The count alone would then
// read "nothing arrived since" and prepend the pre-clear transcript, resurrecting
// history the model no longer has. Membership, not the count, is what tells the
// two apart, so the drop is keyed on whether the ask still stands. See clear.go.
func (a App) historyArrived(f rpc.Frame) App {
	// A reply for a conversation the pane world does not hold is the board's - see
	// boardHistoryArrived (boardtranscript.go). The two asks are mutually exclusive
	// per id (the board is drawn instead of panes, so no pane opens while it is up),
	// so this never steals a pane's reply. One wire and one DM.Before fold serve
	// both; only the map the reply lands in differs.
	dm, ok := a.dms[f.SessionID]
	if !ok {
		return a.boardHistoryArrived(f)
	}
	if len(f.Events) == 0 {
		return a
	}
	held, asked := a.askedHistory[f.SessionID]
	if !asked || dm.events.len() != held {
		// Forgotten as well as dropped, so the next open asks again. Without
		// this the drop is permanent: opening a conversation with a working
		// agent is the ordinary case at 15-30 agents, and any single event in
		// the ask-to-reply window would leave the operator with exactly the
		// empty pane this exists to remove, with no second chance.
		return a.forgetHistoryAsk(f.SessionID)
	}
	return a.withDM(f.SessionID, dm.Before(f.Events))
}

// withHistoryAsked records that this session's transcript has been asked for,
// and how many events its conversation held at the time - which is what makes
// "nothing has arrived since" checkable when the answer comes back.
//
// Copied on write for the reason App.dms is: Bubble Tea hands models around by
// value, and a shared map makes a discarded App's decisions stick to the live
// one.
// forgetHistoryAsk lets a conversation be asked about again.
func (a App) forgetHistoryAsk(id string) App {
	next := make(map[string]int, len(a.askedHistory))
	for k, v := range a.askedHistory {
		if k != id {
			next[k] = v
		}
	}
	a.askedHistory = next
	return a
}

// boardHistoryArrived is replaced by the board DM's own fold in boardtranscript.go.
// Until App.boardDMs exists there is nowhere to fold a board reply, so a stray one
// is dropped. (Temporary shim for the routing seam; superseded in the next commit.)
func (a App) boardHistoryArrived(rpc.Frame) App { return a }

func (a App) withHistoryAsked(id string, held int) App {
	next := make(map[string]int, len(a.askedHistory)+1)
	for k, v := range a.askedHistory {
		next[k] = v
	}
	next[id] = held
	a.askedHistory = next
	return a
}
