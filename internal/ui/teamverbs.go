package ui

// Acting on a whole team: `/park @team`, `/quit @team` behind a confirm, and
// `/resume <team>` (owner's 2026-11-09 ask; decisions.md).
//
// A team verb's members are the set `@team` reaches - App.live's sessions
// wearing the tag, never the manager - so a verb and a message cannot disagree
// about who is on a team. The fan-out is this window's, N one-agent frames in
// one write as /reauth and /resume all write theirs; the daemon has no team verb
// and needs none. A typed `@who` resolves the way core.Resolve routes one: a live
// agent, then a team with a live member, then a parked agent.

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	parkUsage = parkVerb + " parks the conversation you are in, or " + parkVerb + " " + agentPrefix +
		"<who> parks that agent, or every live agent of that team"
	noParkCommandTarget = "which one? " + parkUsage

	// teamParking is said on the keypress; teamParkConfirmed when the reports show
	// every member it asked for parked - once, rather than a line per member
	// fighting over the one notice slot.
	teamParking       = "parking %s…"
	teamParkSkipped   = "; skipped %s, blocked on a permission ask - answer it, or esc withdraws it, then %s %s"
	teamParkConfirmed = "%s%s is parked - " + resumeVerb + " %s%s brings it back"
	teamParkBlocked   = "every live %s%s agent is blocked on a permission ask, and parking closes stdin under it - " +
		"which the agent is told is a denial. Answer them, or esc withdraws each, then " + parkVerb + " %s%s"

	// teamAtRest is a team verb over a team with nothing running.
	teamAtRest = "nothing of %s%s is running: %s parked - " + resumeVerb + " %s%s brings it back"

	// quitNeverMind is the quit confirm's first row, where its cursor opens, so the
	// ↵ that accepted `@backend` and the ↵ that sent the command cannot also stop
	// the team. quitTeamOption is the row that does.
	quitNeverMind  = "never mind"
	quitTeamOption = "quit %s for good"
	quitTeamHeader = "end %s%s for good: %s - a quit cannot be resumed"
	quitTeamParked = "%s stay%s parked - " + resumeVerb + " %s%s, then " + quitVerb + " %s%s ends them too"
	teamQuitting   = "quitting %s…"
	teamQuitGone   = "none of the %s%s agents that confirm named is running any more"

	parkedAlready = "%s%s is parked already - " + resumeVerb + " %s brings it back"

	resumeAskedTeam = "bringing %s%s back (%s)…"
)

// aim is what a team verb's `@who` names: one agent, or a team and its live
// members (none when nothing of it is running).
type aim struct {
	agent   Agent
	team    string
	members []core.Addressee
}

// aimAt resolves `@who` for /park and /quit in core.Resolve's order - a live
// agent, a team with a live member, a parked agent - then a team at rest, and
// reports why when there is nothing to aim at.
func (a App) aimAt(fields []string, usage string) (aim, bool) {
	if len(fields) != 1 || !strings.HasPrefix(fields[0], agentPrefix) {
		notice.Report("%s", usage)
		return aim{}, false
	}
	who := strings.TrimPrefix(fields[0], agentPrefix)
	agent, named := a.fleet.ByName(who)
	if named && agent.State != rpc.StateParked {
		return aim{agent: agent}, true
	}
	team := strings.ToLower(who)
	if members := a.teamLive(team); len(members) > 0 {
		return aim{team: team, members: members}, true
	}
	if named {
		return aim{agent: agent}, true
	}
	if len(a.teamParked(team)) > 0 {
		return aim{team: team}, true
	}
	notice.Report("%s\n%s", noSuchAgent, a.handleList())
	return aim{}, false
}

// teamLive is the team's members `@team` reaches, by name so a confirm reads in
// one order and the writes have one.
func (a App) teamLive(team string) []core.Addressee {
	var out []core.Addressee
	for _, addr := range a.live() {
		if addr.Team == team {
			out = append(out, addr)
		}
	}
	slices.SortFunc(out, func(x, y core.Addressee) int { return strings.Compare(x.Name, y.Name) })
	return out
}

// teamParked is the team's parked sessions, from the roster and the park book
// alike, leaving out a manager wearing its tag: its lifecycle is /manager's.
func (a App) teamParked(team string) []Agent {
	var out []Agent
	for _, agent := range a.parkedAgents() {
		if agent.Team == team && !a.fleet.isManager(agent.Name) {
			out = append(out, agent)
		}
	}
	return out
}

// sayAtRest says a team verb found nothing of the team running.
func (a App) sayAtRest(team string) {
	n := len(a.teamParked(team))
	notice.Report(teamAtRest, agentPrefix, team, plural(n, "agent"), agentPrefix, team)
}

// parkAgent is /park: bare, ⌃C's target; `@who`, that agent or team.
func (a App) parkAgent(arg string) (App, tea.Cmd) {
	fields := strings.Fields(arg)
	if len(fields) == 0 {
		id, ok := a.interruptTarget()
		if !ok {
			notice.Report("%s", a.unseenOr(noParkCommandTarget))
			return a, nil
		}
		return a.parkTyped(id)
	}
	target, ok := a.aimAt(fields, parkUsage)
	switch {
	case !ok:
		return a, nil
	case target.team == "":
		return a.parkTyped(target.agent.ID)
	case len(target.members) == 0:
		a.sayAtRest(target.team)
		return a, nil
	}
	return a.parkTeam(target.team, target.members)
}

// parkTyped is ⌃C's park for a typed command, which also takes its draft unless
// the park was refused - then the draft stays to be fixed, displayTarget's trade.
// A command that changed nothing says so, where a key stays silent.
func (a App) parkTyped(id string) (App, tea.Cmd) {
	if a.parkedAgent(id) {
		name := a.agentName(id)
		notice.Report(parkedAlready, agentPrefix, name, name)
		return a.clearDraft(), nil
	}
	next, cmd, done := a.parkOne(id)
	if !done {
		return a, nil
	}
	return next.clearDraft(), cmd
}

// parkTeam parks every live member but a blocked one, in one write.
// A new ask replaces the team's old wait, so a member that never parked cannot
// hold back every later confirm.
func (a App) parkTeam(team string, members []core.Addressee) (App, tea.Cmd) {
	a = a.unfiled(func(_, held string) bool { return held == team })
	var frames []rpc.Frame
	var blocked []string
	for _, m := range members {
		next, park, ok := a.parkable(m.ID)
		if !ok {
			blocked = append(blocked, agentPrefix+m.Name) // parkWouldDeny's reason
			continue
		}
		a = next.awaitingTeamPark(m.ID, team)
		frames = append(frames, park)
	}
	if len(frames) == 0 {
		notice.Report(teamParkBlocked, agentPrefix, team, agentPrefix, team)
		return a, nil
	}
	said := fmt.Sprintf(teamParking, plural(len(frames), agentPrefix+team+" agent"))
	if len(blocked) > 0 {
		retry := blocked[0]
		if len(blocked) > 1 {
			retry = agentPrefix + team
		}
		said += fmt.Sprintf(teamParkSkipped, strings.Join(blocked, " "), parkVerb, retry)
	}
	notice.Report("%s", said)
	return a.clearDraft(), a.write(parkFailed, frames...)
}

// awaitingTeamPark files a member's park under its team, so the reports confirm
// the team once rather than each member.
func (a App) awaitingTeamPark(id, team string) App {
	next := maps.Clone(a.teamParks)
	if next == nil {
		next = map[string]string{}
	}
	next[id] = team
	a.teamParks = next
	return a
}

// unfiled takes the waits drop matches off its team's park, copy on write.
func (a App) unfiled(drop func(id, team string) bool) App {
	next := maps.Clone(a.teamParks)
	maps.DeleteFunc(next, drop)
	a.teamParks = next
	return a
}

// teamParkArrived takes one confirmed member off its team's wait, saying the
// team is parked once none is left; false for a park no team verb asked for.
func (a App) teamParkArrived(id string) (App, bool) {
	team, ours := a.teamParks[id]
	if !ours {
		return a, false
	}
	a = a.unfiled(func(held, _ string) bool { return held == id })
	if !slices.Contains(slices.Collect(maps.Values(a.teamParks)), team) {
		notice.Report(teamParkConfirmed, agentPrefix, team, agentPrefix, team)
	}
	return a, true
}

// restingTeam is a room draft `@team /park` or `@team /quit` at a team with
// nothing running, which core.Resolve routes past the team (it fans out to live
// members only) to the manager as text; the fold gives it the typed form's answer.
func (a App) restingTeam(wire string) (team, text string, ok bool) {
	head, rest, _ := strings.Cut(strings.TrimSpace(wire), " ")
	who, mention := strings.CutPrefix(head, agentPrefix)
	team = strings.ToLower(who)
	if _, named := a.fleet.ByName(who); !mention || named || len(a.teamLive(team)) > 0 || len(a.teamParked(team)) == 0 {
		return "", "", false
	}
	word, cmd := leadingRoomTargetCommand(rest)
	if _, takesTeam := teamTargetCommands[word]; !cmd || !takesTeam {
		return "", "", false
	}
	return team, strings.TrimSpace(rest), true
}

// confirmTeamQuit opens the quit confirm over the members, resolved now and held:
// the fleet moves, and a confirm that re-resolved would stop somebody it never named.
func (a App) confirmTeamQuit(team string, members []core.Addressee) (App, tea.Cmd) {
	ids := make([]string, 0, len(members))
	names := make([]string, 0, len(members))
	for _, m := range members {
		ids, names = append(ids, m.ID), append(names, m.Name)
	}
	a.picker = Picker{
		Word: quitCommand, Team: team, Targets: ids, Names: names,
		Options: []string{quitNeverMind, fmt.Sprintf(quitTeamOption, plural(len(ids), "agent"))},
	}
	if parked := a.teamParked(team); len(parked) > 0 {
		handles := make([]string, 0, len(parked))
		for _, p := range parked {
			handles = append(handles, agentPrefix+p.Name)
		}
		verb := "s"
		if len(parked) > 1 {
			verb = ""
		}
		notice.Report(quitTeamParked, strings.Join(handles, " "), verb, agentPrefix, team, agentPrefix, team)
	}
	return a.clearDraft(), nil
}

// quitTeam stops the targets the confirm named that are still live; a parked or
// ended one is left as it is.
func (a App) quitTeam(team string, targets []string) (App, tea.Cmd) {
	var frames []rpc.Frame
	for _, id := range targets {
		agent, ok := a.fleet.Agent(id)
		if !ok || agent.State == rpc.StateEnded || agent.State == rpc.StateParked {
			continue
		}
		a = a.awaitingQuit(id)
		frames = append(frames, rpc.Frame{Kind: rpc.FrameStop, SessionID: id})
	}
	if len(frames) == 0 {
		notice.Report(teamQuitGone, agentPrefix, team)
		return a, nil
	}
	notice.Report(teamQuitting, plural(len(frames), agentPrefix+team+" agent"))
	return a, a.write(quitFailed, frames...)
}

// bringBackTeam is /resume <team>: bringBack, named for the team.
func (a App) bringBackTeam(team string, agents []Agent) (App, tea.Cmd) {
	notice.Report(resumeAskedTeam, agentPrefix, team, plural(len(agents), "agent"))
	return a.clearDraft().rouse(agents)
}
