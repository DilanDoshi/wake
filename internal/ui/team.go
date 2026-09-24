package ui

// `/team` — the team an agent is grouped under, from the outside. colorAgent's
// shape exactly: the same `[@who] <value>` grammar through displayTarget, and
// one frame its caller has resolved.
//
// The tag is not validated here. rpc.NormalizeTeam is the one fence and the
// daemon owns it, so the client sends what was typed - `none` included, which
// the daemon reads as clear - with only its spaces hyphenated, /name's fold, so
// `/team front end` asks for `front-end`. A bad tag comes back as the daemon's
// own refusal. A copy of that check here would be the parallel implementation this
// project forbids, stale the day the fence moves.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// teamFailed names the write that could not happen, so the notice row says
	// which command was typed rather than only what the socket said about it.
	teamFailed = "assigning that agent to a team"

	// teamAsked is said on the keypress, colorAsked's reason: the daemon may
	// refuse - the session is parked, the tag is not one token - and the operator
	// should know the command was read either way. It does not echo the value,
	// because the daemon is what decides the stored tag.
	teamAsked = "assigning %s%s to a team…"
)

// teamAgent sets which team one agent is grouped under.
func (a App) teamAgent(arg string) (App, tea.Cmd) {
	agent, team, ok := a.displayTarget(arg, teamUsage, noTeamTarget)
	if !ok {
		return a, nil
	}
	a = a.clearDraft()
	notice.Report(teamAsked, agentPrefix, agent.Name)
	return a, a.write(teamFailed, rpc.Frame{Kind: rpc.FrameTeam, SessionID: agent.ID, Text: hyphenateName(team)})
}
