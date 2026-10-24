package ui

// Channels: a team picked by its roster header is the group chat's base, so the
// room shows that team's thread - what a lone @team narrows to - until the
// operator picks `all` or another team. A lone @name or @team still narrows,
// within it: a line draws when both keys admit it, through the one predicate
// (focusAdmits), so `all`, whose key is empty, is the room as it always was.
//
// The pick is per window and in memory, as the composer's target is: a reattach
// opens on all. Its members are the agents the roster draws under the header -
// parked ones included, since the header still is - and its key follows them on
// every report; a team left with no header returns the window to all. The
// manager has a seat in every channel (the owner, 2026-10-24; spec §12): its
// prose and the turns you sent it - an unaddressed draft is its - draw in each.

import (
	"slices"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

// channelGone is what the room says when the team it showed has left the roster.
const channelGone = "%s has no one on the roster now - the group chat shows everyone"

// roomFilter is what the room draws through, the channel's key and the effective
// focus, as one comparable value.
type roomFilter struct{ channel, focus string }

func (r Room) filter() roomFilter { return roomFilter{r.channelKey, r.effectiveFocus()} }

// admits is whether a line draws: it is in the channel's thread and, within it,
// the focus's. An empty key admits everything.
func (r Room) admits(l roomLine) bool {
	return focusAdmits(l, r.channelKey) && focusAdmits(l, r.effectiveFocus())
}

// WithChannel makes a team's thread the room's base, or all again for "". A new
// channel reads its newest line, a focus change's rule; the same channel whose
// members changed keeps the reader's place, as a standing @team does. The caller
// clears a room selection on a change.
func (r Room) WithChannel(name, key string) Room {
	old := r
	r.channel, r.channelKey = name, key
	if name != "" && name == old.channel && key != old.channelKey {
		return r.refiltered(old)
	}
	return r.rerenderOnFocusChange(old.filter())
}

// channelKey is a team's thread as the roster draws it: the members under its
// header, parked ones included, and the manager's seat; "" when no header is
// drawn for it.
func (f Fleet) channelKey(team string) string {
	for _, s := range f.sections(f.OnRoster()) {
		if team == "" || s.Team != team {
			continue
		}
		ids := make([]string, 0, len(s.Agents)+1)
		for _, a := range s.Agents {
			ids = append(ids, a.ID)
		}
		if m, ok := f.manager(); ok {
			ids = append(ids, m.ID)
		}
		return joinIDs(ids)
	}
	return ""
}

// selectChannel shows a header's channel in the room - a team's, or every
// agent's for all - and gives the room the keys, as picking an agent gives them
// to its conversation. A team that has left the roster picks nothing.
func (a App) selectChannel(header string) App {
	team := header
	if header == core.BroadcastName {
		team = ""
	}
	key := a.fleet.channelKey(team)
	if team != "" && key == "" {
		return a
	}
	prev := a.room.filter()
	a.room = a.room.WithChannel(team, key)
	a.roster = Roster{Header: header}
	return a.clearedSelOnFocusChange(prev).refocus("")
}

// rechanneled follows the channel's members on a fleet report, and returns the
// window to all, saying so, when its team has no header left.
func (a App) rechanneled() App {
	if a.room.channel == "" {
		return a
	}
	prev := a.room.filter()
	name, key := a.room.channel, a.fleet.channelKey(a.room.channel)
	if key == "" {
		notice.Report(channelGone, name)
		name = ""
	}
	a.room = a.room.WithChannel(name, key)
	return a.clearedSelOnFocusChange(prev)
}

// pickedHeader is the channel header the cursor is on, while the sidebar drawing
// it is shown - pickedAgent's rule, for ↵.
func (a App) pickedHeader() (string, bool) {
	if !a.layout.ShowRoster || a.roster.Header == "" {
		return "", false
	}
	return a.roster.Header, true
}

// working is who the room's working line may name: the channel's thread, or the
// fleet under all. A focus within the channel does not narrow it.
func (a App) working() []Agent {
	agents := a.fleet.OnRoster()
	if key := a.room.channelKey; key != "" {
		agents = slices.DeleteFunc(agents, func(ag Agent) bool { return !addressedTo(key, ag.ID) })
	}
	return agents
}
