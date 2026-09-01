package ui

// A fleet report, folded.
//
// Split out of app.go when that file crossed the 800-line hard max, and at this
// seam rather than at the line count: everything here is one subject - what the
// daemon's account of the whole fleet does to this model - and it is reached
// from exactly one place, App.apply's two status cases. app.go keeps the
// messages and the frames; this keeps what a *report* means.

import (
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
	"maps"
)

// applyStatus folds a fleet report into everything that reads one.
//
// Both kinds are read, and they are not the same thing. FrameStatusPush is the
// announcement channel - retire and the liveness watchdog broadcast it, and it
// is what carries an ending here. A FrameStatusReply arriving on this connection
// is an answer to something *the process* asked for, and it is read anyway
// because a snapshot cannot invent an ending, only miss one: a session that has
// ended stays ended, and the push carries anything the reply was assembled too
// early to know.
//
// **This view never writes FrameStatus**, which is the whole of why it never has
// to tell the two apart, and it is a claim about the model rather than about the
// connection. There are two things on the other end of a reply and neither is
// this model's doing: a spawn confirmation, which `cmd/wake` waited for before it
// built this, and - since bare `wake` - the one `cmd/wake.requestFleet` asked for
// on this same socket, also consumed before this model existed. So a reply
// reaching Update is a duplicate of a seed, never an answer to a question in
// flight. `wake status` and the detach line go through daemon.Status, which
// opens its own connection.
func (a App) applyStatus(st *rpc.Status) App {
	if st == nil {
		return a
	}
	a.fleet = a.fleet.WithStatus(st)
	// In both directions: an ask the report no longer names is dead, and one it
	// names that this client never saw arrived while it was detached. See
	// Cards.Reconcile.
	a.cards = a.cards.Reconcile(st)
	// A report is exactly when this session's state can have moved out from
	// under an open rewind picker too - Cards.Reconcile's own reason, one
	// picker over. See rewind.go's reconcileRewind, and the adversarial
	// review CRITICAL finding it closes, 2026-08-26.
	a = a.reconcileRewind()
	return a.renamed(st).startArrived(st).parkArrived(st).wakeArrived(st).noteEnding(st).refreshedAgents().departedQuit().pruneCompacting().retarget().recompleted()
}

// refreshedAgents folds each live agent into the conversation this client holds
// for it, so a stored DM carries the facts its own chrome is derived from.
//
// docs/notes/bugs.md BUG-5. App.dmFor assigns d.Agent for the draw and returns a
// **copy**, so the stored DM kept a zero Agent forever - and DM.withBar compared
// a key built from the live agent against a barFrom built from that zero one,
// found them different, re-rendered, and threw the freshened cache away with the
// frame. The bar reads the filesystem (gitref walks for a .git and reads HEAD),
// so at the shimmer's 20Hz that was four stats a frame per open conversation:
// the "work per frame that could be work per change" the first non-negotiable
// forbids, measured at 20 draws over 20 frames.
//
// **On the report rather than on the draw**, which is what makes it work per
// change: a fleet report is exactly when an agent's facts move. dmFor keeps its
// assignment as the path for a conversation opened before any report has
// arrived; what changes is that the stored value is no longer always stale.
//
// One map copy for the whole fleet rather than one per agent - withDM copies the
// whole map per write, and thirty of those per report is the cost this is
// avoiding one layer up.
func (a App) refreshedAgents() App {
	var next map[string]*DM
	for id, d := range a.dms {
		agent, ok := a.fleet.Agent(id)
		if !ok || d.Agent == agent {
			continue
		}
		if next == nil {
			next = make(map[string]*DM, len(a.dms))
			maps.Copy(next, a.dms)
		}
		nd := *d
		nd.Agent = agent
		// The bar's cache is refreshed with it rather than left for the draw,
		// because barFrom lives on this stored value too: folding in the agent
		// alone would leave the key current and the cache stale, and the pane
		// would re-render from that mismatch on every frame - the same bug one
		// report later. At the width it was last sized to, which is the width
		// the draw will ask for; a later SetSize re-renders if that moves.
		stored := nd.withBar(max(nd.width, minComposerWidth))
		next[id] = &stored
	}
	if next != nil {
		a.dms = next
	}
	return a
}

// refreshedBar re-renders one stored conversation's status bar, for a fact that
// reached App without a fleet report behind it.
//
// refreshedAgents is the same rule for the report path and cannot serve this
// one: a permission mode arrives as a receipt on the event path, and the bar
// draws it. Kept to one session because that is what moved - the report path
// walks the fleet because a report is about the fleet.
//
// It stores what dmFor assembled rather than only the bar, so the stored
// composer's mode moves with it - DM.SetSize calls withBar off that stored
// value, and a mode left behind there would have it re-render at the old one.
// The arms ride along and are draw-time state (composer.go), harmless because
// every draw sets them again through dmFor.
func (a App) refreshedBar(sessionID string) App {
	d, ok := a.dms[sessionID]
	if !ok {
		return a
	}
	return a.withDM(sessionID, a.dmFor(sessionID).withBar(max(d.width, minComposerWidth)))
}

// noteEnding reports this client's own session dying, once.
//
// The daemon's push on retire is its *only* unsolicited account of a session
// dying, and recentEndings exists precisely so a client that missed the
// announcement can still find it in the next report. A crashed session also
// produces a FrameError, so ignoring these would leave exactly the clean and
// quiet endings unreported - which are the common ones, and which otherwise
// leave a live-looking conversation whose composer swallows every keystroke.
//
// Only this client's own session is announced on the notice row. Every other
// agent's ending is the roster's job, and thirty agents finishing overnight is
// thirty notices nobody asked for.
func (a App) noteEnding(st *rpc.Status) App {
	if a.ended || a.sessionID == "" {
		return a
	}
	for _, s := range st.Sessions {
		if s.ID != a.sessionID || s.State != rpc.StateEnded {
			continue
		}
		a.ended = true
		notice.Report("%s", endedText(s))
		return a
	}
	return a
}

// endedText is how a session's ending reads on the notice row. An empty Error
// is a clean exit, which is a fact worth stating rather than a blank.
func endedText(s rpc.SessionStatus) string {
	if s.Error == "" {
		return "this session has ended. " + detachAdvice
	}
	return "this session has ended: " + s.Error + ". " + detachAdvice
}
