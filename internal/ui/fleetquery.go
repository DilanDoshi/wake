package ui

// Reading the fleet: the immutability contract and the accessors a surface asks
// for a roster, a name or the open DM through. Split from fleet.go, which keeps
// the Agent type and the event fold that writes these fields, when the file
// crossed the 800-line hard max - the subject seam is write versus read.

import (
	"maps"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// copy is the immutability contract. The map and the order slice are both
// duplicated, so a caller holding an older Fleet holds the fleet it had.
func (f Fleet) copy() Fleet {
	out := Fleet{
		agents:     make(map[string]Agent, len(f.agents)+1),
		tasks:      make(map[string]Tasks, len(f.tasks)+1),
		checklists: make(map[string]checklist, len(f.checklists)+1),
		focused:    f.focused,
	}
	maps.Copy(out.agents, f.agents)
	// A session's list is replaced wholesale by foldChecklist, never appended in
	// place, so the shallow copy is all this needs - Tasks' own reason.
	maps.Copy(out.checklists, f.checklists)
	// Copied for agents' own reason: a caller holding an older Fleet keeps the
	// dispatches it had. Tasks is a value whose slice is never written in
	// place - Tasks.Observe copies before it appends - so the shallow copy is
	// the whole of what this needs.
	maps.Copy(out.tasks, f.tasks)
	out.order = append(make([]string, 0, len(f.order)+1), f.order...)
	// Carried, not rebuilt: Observe copies for every event that moves an agent,
	// and a book dropped there would take /resume's only index with it between
	// one status report and the next.
	out.parked = append([]Agent(nil), f.parked...)
	return out
}

func (f Fleet) Agent(id string) (Agent, bool) { a, ok := f.agents[id]; return a, ok }

// OnRoster is every agent the sidebar draws and the cursor can land on: the
// fleet minus the ones that have ended.
//
// An ended session cannot be messaged, resumed or parked, and its name has
// already gone back to the pool - so a row for one is a name on screen that
// nothing can be done with, and it can be a name a *live* agent now also has.
// Its ending is announced in the room and kept by `wake status`, which are the
// two surfaces whose subject is what happened rather than what to do next.
func (f Fleet) OnRoster() []Agent {
	out := make([]Agent, 0, len(f.order))
	for _, id := range f.order {
		if a := f.agents[id]; a.State != rpc.StateEnded {
			out = append(out, a)
		}
	}
	return Rank(out)
}

// sending records where the turn about to start was sent from, so the room can
// draw a turn addressed to the room and leave a private one in its own DM.
//
// It is per client and lasts one turn: a reattached window has no memory of a
// turn it did not send, and treats what arrives as public. That is the safe
// direction of the two - a reply in the room is noise, a reply nowhere is lost.
func (f Fleet) sending(id string, inDM bool) Fleet {
	a, known := f.agents[id]
	if !known || a.inDM == inDM {
		return f
	}
	a.inDM = inDM
	f = f.copy()
	f.agents[id] = a
	return f
}

// inDM says whether this session's in-flight turn was sent from its own DM.
func (f Fleet) inDM(id string) bool { return f.agents[id].inDM }

// ByName resolves a display name to an agent. Exact, never a prefix: the
// daemon guarantees no two *live* sessions share a name, which is what makes
// this unambiguous, and prefix resolution belongs to `wake attach` where a
// human is typing at a shell rather than routing a message.
func (f Fleet) ByName(name string) (Agent, bool) {
	for _, id := range f.order {
		if a := f.agents[id]; a.Name == name && a.State != rpc.StateEnded {
			return a, true
		}
	}
	return Agent{}, false
}

// Agents is every agent in attention order.
//
// It sorts on every call rather than caching a ranked slice inside the Fleet,
// and the reason is the batch: App folds every frame that arrived while it was
// busy and draws once, so a rank computed on every change would run many times
// per drawn frame, while a rank computed per call runs once. Lazy is the
// cheaper of the two here, not the lazier.
//
// Measured at 30 agents: 6.8µs against a View that costs ~250µs. Affordable
// once per frame and not more than once - a caller wanting the roster twice in
// one draw should hold the slice.
func (f Fleet) Agents() []Agent {
	out := make([]Agent, 0, len(f.order))
	for _, id := range f.order {
		out = append(out, f.agents[id])
	}
	return Rank(out)
}

// MarkRead clears one agent's unread count.
func (f Fleet) MarkRead(id string) Fleet {
	a, ok := f.agents[id]
	if !ok || a.Unread == 0 {
		return f
	}
	f = f.copy()
	a.Unread = 0
	f.agents[id] = a
	return f
}

// Focus records whose DM is open, and reads what has accumulated for them.
//
// It records no agent of its own. "" is how "no DM is open" is spelled, and an
// id nobody has reported yet is not a row - inventing one here would put an
// empty line in the roster for whatever the caller last had selected.
func (f Fleet) Focus(id string) Fleet {
	f = f.copy()
	f.focused = id
	return f.MarkRead(id)
}

// Focused is the agent whose DM is open, or "" for none.
func (f Fleet) Focused() string { return f.focused }

// needsAuth is how many servers are waiting to be authenticated.
//
// The comparison is against core's word, never a literal: internal/ui does not
// spell Claude's vocabulary, which is the rule the permission modes carry.
func needsAuth(servers []core.MCPServer) int {
	n := 0
	for _, s := range servers {
		if s.State == core.MCPNeedsAuth {
			n++
		}
	}
	return n
}
