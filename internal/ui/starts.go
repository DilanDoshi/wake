package ui

// The wait a client keeps on a session it asked the daemon to start.
//
// # Why one wait and not one per verb
//
// ⌃F and `/new` differ in exactly one thing — the frame they write — and agree
// about everything after it. Both mint a UUID, because Wake originates identity
// and `maySpawn` refuses anything that is not one; both hand the daemon a frame
// and decide nothing else; both have to wait, because **the session does not
// exist until the daemon has started it** and a pane opened on the keypress is
// the empty conversation with a working-looking header `cmd/wake.reattach`
// exists to prevent; and both are settled by a `rpc.FrameError` addressed to
// the id they minted.
//
// A second set beside this one would be the parallel implementation CLAUDE.md
// forbids, and it would drift in the place that costs most: the arm that stops
// waiting. `startSettled` is keyed on the id in both of its callers, and an
// error frame about *another* agent has to leave every other wait alone — a
// rule that is easy to state once and easy to get wrong twice.
//
// # And what a start *says* when it arrives is derived, not remembered
//
// A fork says it is a snapshot; a fresh agent says where it is running. The
// obvious design remembers which verb asked, and then holds two facts that can
// disagree — a fork announced as fresh is a lie about what its transcript
// contains. The report already carries the answer: `SessionStatus.ParentID` is
// written by `daemon.launch` and is the parent's id for a fork and empty for a
// spawn. `startedNotice` is the one place that reads it. Today there is one
// sentence, because ⌃F is the only verb in the set.

import (
	"maps"
	"strings"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// awaitingStart and startSettled are the only two writers of App.pendingStarts,
// and that is a property a guard derives rather than a habit - see
// TestTheOnlyWritersOfThePendingStartSetAreTheOnesWithAVerdictHere.
//
// # Why a set rather than a slot
//
// Because two presses are the feature. Fork exists to explore two approaches
// from one context (docs/notes/v1_goals.md), and two approaches is ⌃F ⌃F across
// a `claude` spawn that takes seconds - so a single slot drops the first fork on
// exactly the input the key was built for. It ran, it had a roster row, and no
// key reached it: not in dmOrder so ⇥ missed it, and ⌃D reopens dmTarget's,
// which by then is the fork that did open. `/new` `/new` is the same shape one
// verb over.
//
// Copied on write, like dms and dmOrder and for the same reason: Bubble Tea
// hands models around by value, and a shared map makes a discarded App's wait
// keep growing.
func (a App) awaitingStart(id string) App {
	next := make(map[string]struct{}, len(a.pendingStarts)+1)
	maps.Copy(next, a.pendingStarts)
	next[id] = struct{}{}
	a.pendingStarts = next
	return a
}

// startSettled stops waiting for one session - because it arrived, or because
// the daemon refused it. An id nothing is waiting for returns the receiver
// untouched, which is what makes this callable on every error frame rather than
// only on the ones that are about a start: at 30 agents most of them are not.
//
// It is also why the arm that calls it needs no "is this id even set" guard. An
// empty SessionID cannot be a member - every member is a UUID this client
// minted - so the sentinel comparison that a single slot needed is gone with the
// slot.
func (a App) startSettled(id string) App {
	if _, waiting := a.pendingStarts[id]; !waiting {
		return a
	}
	next := make(map[string]struct{}, len(a.pendingStarts))
	maps.Copy(next, a.pendingStarts)
	delete(next, id)
	a.pendingStarts = next
	return a
}

// startArrived settles the wait on every session this client asked for, the
// first time a report names it, and says what it is - opening its conversation
// for a fork or an import, and drafting a mention for a fresh spawn instead
// (see draftMention).
//
// Waiting for the report rather than acting on the keypress: the session does
// not exist until the daemon has started it, and a DM for an id nothing holds
// is the empty conversation with a working-looking header that
// cmd/wake.reattach exists to prevent.
//
// The daemon's confirmation is a FrameStatusReply and its announcement is a
// FrameStatusPush; applyStatus folds both, so this needs neither to know which
// it is. It settles the wait first, so a later report cannot steal the arrival
// a second time.
func (a App) startArrived(st *rpc.Status) App {
	if len(a.pendingStarts) == 0 {
		return a
	}
	for _, s := range st.Sessions {
		if !isTheAwaitedStart(s, a.pendingStarts) {
			continue
		}
		a = a.startSettled(s.ID)
		notice.Report("%s", a.startedNotice(s))
		switch {
		case s.Name == core.ManagerName:
			// The manager is a service, not a conversation: cmd/wake/manager.go
			// settles it that way, whose surface is the room's own composer. A
			// pane on it would put the operator in a composer whose unaddressed
			// text goes to the session they are looking at - and `/manager` is
			// most often typed by nobody at all, since cmd/wake starts one
			// before the room is drawn. ⌃D on its roster row is how somebody who
			// wants the conversation gets it.
		case s.ParentID == "":
			// A fresh spawn - `/new`'s own arrival - has no conversation to
			// open either, and opening one would replace whatever pane the
			// operator was on for one that has said nothing yet (owner's
			// ruling). ⌃F and `/adopt` always carry a ParentID and take the
			// arm below.
			a = a.draftMention(s.Name)
		default:
			a = a.openDMWith(s.ID, s.Name)
		}
	}
	return a
}

// draftMention pre-fills the room's composer with `@name ` for a fresh spawn's
// arrival, so the operator can message it at once without losing whatever pane
// they were on.
//
// It writes only Room.Composer and never App.focus - retarget reads the room's
// own composer directly rather than through App.composer, which is the
// focused pane's - so this leaves the keys exactly where they were. retarget
// also narrows the room to the agent it just named, the same way typing
// `@name` by hand would.
//
// **Gated to an empty draft.** WithDraft replaces rather than inserts, and a
// spawn takes seconds - long enough to start typing a room message before the
// confirmation lands - so an unconditional draft would silently discard
// whatever the operator was writing. Losing that is worse than the mention
// never appearing, so a non-empty draft is left untouched and the operator
// types the mention themselves. The same gate decides two rapid `/new`s: the
// second arrival finds the first's mention still sitting in the box and
// leaves it rather than clobbering it, so the first spawn keeps the draft.
func (a App) draftMention(name string) App {
	if strings.TrimSpace(a.room.Composer().Value()) != "" {
		return a
	}
	a.room = a.room.WithComposer(a.room.Composer().WithDraft(agentPrefix + name + " "))
	return a.retarget()
}

// startedNotice is what a session this client asked for says when it arrives.
//
// Keyed on the report's own ParentID rather than on which verb asked, so the
// sentence cannot disagree with the row: a fork is a snapshot of a conversation
// it inherited and says so, and a fresh agent has no conversation to be a
// snapshot of. daemon.launch is the single writer of that field - the parent's
// id for a fork, empty for a spawn - so the two cannot come apart.
//
// The fork half goes through parentName, not agentName. They differ on exactly
// the case the DM header was fixed for: a parent whose name has gone back to
// the pool and now belongs to a live agent. The snapshot sentence says what
// that handle *does next*, so naming the wrong one is worse here than in the
// header - the reader can type it.
func (a App) startedNotice(s rpc.SessionStatus) string {
	if s.ParentID != "" {
		return SnapshotNotice(s.Name, a.parentName(s.ID))
	}
	return startedLine(s.Name, s.Label)
}

// isTheAwaitedStart says whether one row of a fleet report is a session this
// client asked for and has not yet seen arrive.
//
// The id and the state, and deliberately nothing else. **Every state with a
// process behind it is acted on** - startArrived opens a conversation for a
// fork or an import and drafts a mention for a fresh spawn: fan-out starts
// before the spawn's confirmation is enqueued, so the first report naming a
// new session can already show it working, blocked or silent, and an arm that
// waited for idle would lose exactly the session that got going quickly -
// leaving it in the pending set for a later session given that id to steal the
// arrival. Ended and parked are the two exceptions because there is nothing
// behind that pane or that mention; a start that failed says so in a
// FrameError addressed to this same id, which is what clears the wait instead.
//
// Parked is here for ended's reason and not by analogy with it: the process has
// exited either way, and a composer over a dead process swallows every
// keystroke. What separates them is that a park is meant to be taken back.
//
// **Revisited 2026-08-11, when the room grew `/resume`, and left alone.** The
// verb does change one thing: a parked pane is no longer a dead end, because a
// bare `/resume` typed into it brings that session back and `submit` asks the
// router before it asks whether there is anything to send to. It does not
// change what the pane would *show*. A session reported parked by the first
// report that names it has produced no events at all, so a fork or an import
// opens an empty transcript under a working-looking header, which is exactly
// the thing `cmd/wake.reattach` exists to prevent - and the operator pressed
// ⌃F or typed `/adopt` for a live conversation, not for one they have to
// revive first. `/new` never opens a pane at all, but the same session is
// behind the mention it would draft, so it stays excluded here too rather than
// naming an agent that has already gone. So the cell stays false and the
// improvement `/resume` unlocks here is in the sentence rather than the pane.
//
// A function rather than a condition inside the loop so the contract - the
// answer depends on the id and the state alone - is a static property of
// something, which is the only way to close a narrowing on a *value*. See
// TestTheDecisionThatAStartHasArrivedReadsNothingButTheIdAndTheState.
func isTheAwaitedStart(s rpc.SessionStatus, pending map[string]struct{}) bool {
	_, waiting := pending[s.ID]
	return waiting && s.State != rpc.StateEnded && s.State != rpc.StateParked
}

// The name is read straight off the report row, with no fallback for a row that
// carries none, because no such row can arrive here. Every session in a
// FrameStatusPush or FrameStatusReply comes from daemon.fleet() - either
// agent.snapshot() or a retire-time snapshot in s.recent - and an agent's name
// is nameRegistry.claim's return value, which is the requested name or one
// drawn from the pool and never empty. A "(unnamed)" branch was written here,
// carried a comment explaining itself, and was deleted: it is a verdict over an
// input no producer can make, which is docs/notes/decisions.md's rung 4 and
// this project's most-repeated way of writing dead code that reads as care.
// cmd/wake's displayName keeps its fallback because its rows can also come off
// the on-disk roster.
