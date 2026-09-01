package ui

// `/quit` — ending one agent and removing it from this window.
//
// # Why it is a stop and not a park
//
// ⌃C parks the focused agent: the process stops, the transcript stays, and
// `/resume` brings it back. `/quit` is the other ending, the one `wake stop` and
// `/manager-stop` already spell - `rpc.FrameStop`, which closes stdin, lets the
// in-flight turn finish, and releases the name back to the pool. Nothing brings
// it back, which is why it is a typed verb rather than a key: a reflex must not
// reach the one thing that cannot be undone.
//
// # Why removal is a client-side fold and not a daemon change
//
// The daemon keeps an ended session in a recent ring (server.go's recentEndings)
// so a client that blinked during an ending can still account for it, and
// Fleet.WithStatus keeps the ended row for the same reason - "the roster does not
// lose a row the moment an agent dies". Both are right for an ending nobody
// asked for. An ending this operator *typed* is one they already know about, so
// this window drops the row rather than dimming it: departedQuit removes the
// agent from the fleet, the ⇥ ring, the grid and the roster cursor once a report
// confirms it ended. It is per-window - another operator's roster still shows the
// `·`, the same as any ending they did not initiate.
//
// # Why the drop waits for the report
//
// The keypress is the ask and the report is the answer, ⌃C park's own rule: a
// stop lets the in-flight turn finish, so a working agent stays visible until the
// ending lands. Dropping on the keypress would take a row away while its agent
// was still finishing, and the daemon can still refuse (the session ended between
// the read and the write). So awaitingQuit remembers the ask and departedQuit
// acts on the confirming report, which is where the row leaves.

import (
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// quitFailed names the write that could not happen, so the notice row says
	// which command was typed rather than only what the socket said.
	quitFailed = "quitting that agent"

	// quitAsked is said on the keypress, colorAsked's reason: a stop lets the
	// turn finish, so the row does not go at once, and the operator should know
	// the command was read. The row leaving is the confirmation; this is the ask.
	quitAsked = "quitting %s%s…"

	// quitUsage is what /quit takes: nothing (the conversation you are in) or one
	// @who. No value, unlike /name and /color - the target is the whole of it.
	quitUsage = quitVerb + " ends the conversation you are in, or " + quitVerb + " " + agentPrefix + "<who> ends that agent"

	// noQuitTarget is /quit in the room with no handle. The room is not one
	// conversation and quit is irreversible, so it may not be aimed by the roster
	// cursor - noNameTarget's rule, one verb over.
	noQuitTarget = "which one? " + quitUsage

	// quitParked refuses a parked agent, and it is mechanism rather than taste:
	// the daemon refuses a stop at a session with no process, so a stop written
	// here comes back as an unrelated daemon sentence. It says when it would work,
	// forkRefusal's rule - managerStopParked one verb over.
	quitParked = "%s%s is parked, and a stop only reaches a session with a process. " +
		resumeVerb + " %s wakes it, then " + quitVerb + " ends it"

	// quitIsTheManager refuses /quit on the manager and points at its own ending.
	// The manager has one lifecycle path on purpose - `/manager` parks, wake and
	// spawn, and `/manager-stop` ends - and a second verb that also *dropped its
	// row* would be the parallel implementation the non-negotiables forbid. So the
	// manager is the one name /quit declines rather than the roster cursor, which
	// mentionCommand already keeps it from aiming.
	quitIsTheManager = "that is the manager. " + managerStopVerb + " ends it, and " + managerVerb + " starts a fresh one"
)

// quitAgent ends one agent and removes it from this window once the report
// confirms the ending.
//
// It does not borrow park's blocked refusal, managerStop's own inversion: park
// refuses a blocked agent because the denial nobody made survives the wake, and
// a stop has no wake, so refusing would leave the operator unable to end the one
// session they are trying to be rid of.
func (a App) quitAgent(arg string) (App, tea.Cmd) {
	agent, ok := a.quitTarget(arg)
	if !ok {
		// quitTarget has reported why; the draft stays so a wrong handle can be
		// fixed, displayTarget's own trade.
		return a, nil
	}
	if agent.Name == core.ManagerName {
		notice.Report("%s", quitIsTheManager)
		return a, nil
	}
	if a.endedAgent(agent.ID) {
		// Already over and nothing lost, so silent - interrupt's own trade for an
		// ended session. The command did its job, so the draft clears.
		return a.clearDraft(), nil
	}
	if a.parkedAgent(agent.ID) {
		notice.Report(quitParked, agentPrefix, agent.Name, agent.Name)
		return a, nil
	}
	a = a.clearDraft()
	notice.Report(quitAsked, agentPrefix, agent.Name)
	return a.awaitingQuit(agent.ID), a.write(quitFailed, rpc.Frame{Kind: rpc.FrameStop, SessionID: agent.ID})
}

// quitTarget resolves the agent /quit is about: an @who, else the conversation
// you are in, else a refusal. It is quitAgent's own resolver rather than
// displayTarget because /quit carries no value - the target is the whole grammar,
// so a bare form is not "the value with no @who" but "no target named".
func (a App) quitTarget(arg string) (Agent, bool) {
	fields := strings.Fields(arg)
	switch {
	case len(fields) == 0:
		agent, ok := a.conversationAgent()
		if !ok {
			notice.Report("%s", noQuitTarget)
			return Agent{}, false
		}
		return agent, true
	case len(fields) == 1 && strings.HasPrefix(fields[0], agentPrefix):
		who := strings.TrimPrefix(fields[0], agentPrefix)
		agent, ok := a.fleet.ByName(who)
		if !ok {
			notice.Report("%s\n%s", noSuchAgent, a.handleList())
			return Agent{}, false
		}
		return agent, true
	default:
		notice.Report("%s", quitUsage)
		return Agent{}, false
	}
}

// awaitingQuit remembers a quit this client asked for, so the report that
// confirms the ending can be told from the daemon's own recent-ending row.
// awaitingPark's shape, for its reason.
func (a App) awaitingQuit(id string) App {
	next := make(map[string]struct{}, len(a.quitting)+1)
	for held := range a.quitting {
		next[held] = struct{}{}
	}
	next[id] = struct{}{}
	a.quitting = next
	return a
}

// departedQuit removes every agent this window asked to /quit that a report has
// now confirmed ended - from the fleet, the ⇥ ring, the grid and the roster
// cursor. Run from applyStatus after the fold, on every report: the daemon
// re-reports the ending until it falls out of its recent ring, so this re-applies
// the drop rather than remembering it.
//
// The watch set is pruned when the daemon stops reporting an id at all - it has
// left the recent ring, so there is nothing left to re-drop. That is where this
// differs from awaitingPark, which prunes on the *confirmation*: park keeps the
// row and this removes it, so the ending has to be re-applied for as long as the
// daemon keeps reporting it. Pruning on the forgetting rather than never is what
// keeps `quitting` bounded.
func (a App) departedQuit() App {
	if len(a.quitting) == 0 {
		return a
	}
	gone := map[string]struct{}{}
	forgotten := map[string]struct{}{}
	for id := range a.quitting {
		agent, ok := a.fleet.Agent(id)
		switch {
		case !ok:
			forgotten[id] = struct{}{}
		case agent.State == rpc.StateEnded:
			gone[id] = struct{}{}
		}
	}
	for id := range gone {
		a = a.forgetConversation(id)
	}
	if len(gone) > 0 {
		a.fleet = a.fleet.drop(gone)
	}
	if len(forgotten) > 0 {
		next := maps.Clone(a.quitting)
		for id := range forgotten {
			delete(next, id)
		}
		a.quitting = next
	}
	return a
}

// forgetConversation takes one conversation off every surface that draws it: the
// stored DM, the ⇥ ring, the grid, and the roster cursor if it rests there.
//
// Unlike hideDM (⌃W), which keeps the transcript for a reopen, this forgets it:
// a quit agent has left the fleet, so nothing can reopen its conversation and a
// kept transcript is only a leak.
func (a App) forgetConversation(id string) App {
	if _, held := a.dms[id]; held {
		next := maps.Clone(a.dms)
		delete(next, id)
		a.dms = next
	}
	if at := slices.Index(a.dmOrder, id); at >= 0 {
		a.dmOrder = slices.Delete(slices.Clone(a.dmOrder), at, at+1)
	}
	if a.grid.Has(id) {
		next := a.grid.Neighbour(id)
		a.grid = a.grid.Close(id)
		// The keys move only when they were on the closed pane; unlike hideDM,
		// which only ever closes the focused column, this can close a background
		// one (`@who /quit` from the room). But the resize is unconditional: a
		// column freed anywhere widens the survivors, and a stored width left
		// stale re-wraps that pane every frame - the "work per frame" the first
		// non-negotiable forbids.
		if a.focus == id {
			a.fleet = a.fleet.Focus(next)
			a = a.withFocus(next)
		}
		a = a.resizePanes()
	}
	if a.roster.Selected == id {
		// The cursor was on the agent that left. "" is nobody; the next ↑↓ lands
		// on the first row rather than counting from a row that is gone.
		a.roster = Roster{}
	}
	return a
}

// drop returns a new Fleet without the given ids - agents, their dispatches and
// their checklists. Used only by departedQuit: a session the operator quit and
// the report has confirmed ended, taken off every surface Fleet.Agents() feeds.
//
// It is here rather than in fleet.go for the plain reason that fleet.go is at the
// 800-line hard max; it reads the same unexported fields, this being one package.
func (f Fleet) drop(ids map[string]struct{}) Fleet {
	out := f.copy()
	for id := range ids {
		delete(out.agents, id)
		delete(out.tasks, id)
		delete(out.checklists, id)
	}
	out.order = slices.DeleteFunc(out.order, func(id string) bool {
		_, gone := ids[id]
		return gone
	})
	if _, gone := ids[out.focused]; gone {
		out.focused = ""
	}
	return out
}
