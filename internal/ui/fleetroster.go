package ui

// The fleet as a roster: reading it back out, in the shapes the sidebar, the
// room and routing each want. Split from fleet.go once the subagent backlog
// (fleetsubs.go) pushed the fold half past the 800-line hard max - fold and
// read are two subjects sharing one type, not one subject that grew.

import (
	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

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
