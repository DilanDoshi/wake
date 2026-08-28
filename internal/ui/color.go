package ui

// `/color` — the identity hue an agent's turns, status bar and roster row are
// drawn in, from the outside. labelAgent's shape exactly: the same `[@who]
// <value>` grammar through displayTarget, and one frame its caller has resolved.
//
// The colour is not validated here. rpc.NormalizeColor is the one fence and the
// daemon owns it, so the client sends what was typed - `none` included, which
// the daemon reads as clear - and a wrong colour comes back as the daemon's own
// refusal, which names the seven that exist. A copy of that check here would be
// the parallel implementation this project forbids, stale the day the set moves.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// colorFailed names the write that could not happen, so the notice row says
	// which command was typed rather than only what the socket said about it.
	colorFailed = "colouring that agent"

	// colorAsked is said on the keypress, renameAsked's reason: the daemon may
	// refuse - the session is parked, the colour is not one - and the operator
	// should know the command was read either way. It does not echo the value,
	// because the daemon is what decides the stored colour.
	colorAsked = "colouring %s%s…"
)

// colorAgent sets what one agent is drawn in.
func (a App) colorAgent(arg string) (App, tea.Cmd) {
	agent, color, ok := a.displayTarget(arg, colorUsage, noColorTarget)
	if !ok {
		return a, nil
	}
	a = a.clearDraft()
	notice.Report(colorAsked, agentPrefix, agent.Name)
	return a, a.write(colorFailed, rpc.Frame{Kind: rpc.FrameColor, SessionID: agent.ID, Text: color})
}
