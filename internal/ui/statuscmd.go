package ui

// `/status`: one session's own facts, the Status tab of Claude Code's dialog.
//
// Headless claude refuses the command with no model turn
// (testdata/stream/bare-status.jsonl), and a local command sent to a live
// session stays in its context, so Wake draws the facts it already holds and
// writes nothing. A read-only menu over the composer, like /mcp's: drawn by
// menuBlock, closed by any key, and so it adds no legend entry. Claude Code's
// Config and Usage tabs are /config and /usage, which a session still answers.

import (
	"cmp"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/gitref"
	"github.com/DilanDoshi/wake/internal/version"
)

const (
	statusUsage    = statusVerb + " shows the conversation you are in, or " + statusVerb + " " + agentPrefix + "<who> shows that agent's"
	statusNoTarget = "whose status? Open a conversation, or " + agentPrefix + "<who> " + statusVerb + " from the room"
	statusTitle    = "Status"
	statusHint     = "any key closes"
)

// StatusView is the open view: Session is the agent it describes, "" meaning
// none, and Pane is where it was opened and is drawn. Branch is read once, at
// open: the view redraws at the shimmer's rate, and gitref reads the disk.
type StatusView struct{ Session, Pane, Branch string }

func (v StatusView) Open() bool { return v.Session != "" }

func (a App) status(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	if agent, ok := a.commandTarget(arg, statusUsage, statusNoTarget); ok {
		a.statusUI = StatusView{Session: agent.ID, Pane: a.focus, Branch: gitref.Of(agent.Cwd).Name()}
	}
	return a, nil
}

// statusKey closes the view on any key and swallows it, read above App.key's
// switch. ⌃C closes it and parks, so its first press stays visible for the kill
// switch's invariant. An agent gone from the fleet draws no view, and a card in
// the pane comes first and may clip the view out of sight, so in both cases the
// key passes on rather than vanishing into a view nobody can see.
func (a App) statusKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	v := a.statusUI
	if !v.Open() || v.Pane != a.focus {
		return a, nil, false
	}
	a.statusUI = StatusView{}
	_, carded := a.cardOf(v.Pane)
	if _, ok := a.fleet.Agent(v.Session); !ok || carded {
		return a, nil, false
	}
	if m.Type == tea.KeyCtrlC {
		model, cmd, _ := a.park()
		return model.(App), cmd, true
	}
	return a, nil, true
}

// statusView is the view if it belongs to pane id, sized to the rows left under
// what is stacked over it.
func (a App) statusView(id string, width, height int, over string) string {
	v := a.statusUI
	agent, ok := a.fleet.Agent(v.Session)
	if !v.Open() || v.Pane != id || !ok {
		return ""
	}
	room := a.menuRoom(id, width, height) - lipgloss.Height(over)
	w := max(width, minBlockWidth)
	rows := a.statusRows(agent, v.Branch)
	if room < 3 {
		return HintStyle.Render(ansi.Truncate(statusTitle, w, ellipsis))
	}
	body := lipgloss.NewStyle().PaddingLeft(1).Render(strings.Join(windowed(statusLines(rows, max(w-cardFrameWidth, 1)), 0, room-2), "\n"))
	return titledBox(body, w, BoxStyle.Padding(0), statusTitle, statusHint, AccentStyle, HintStyle)
}

// statusRows are the facts, label and value, built from what the status bar and
// the banner already say; a fact this window does not hold is left out.
func (a App) statusRows(agent Agent, branch string) [][2]string {
	rows := [][2]string{{"Name", agent.Name}, {"Session ID", agent.ID}}
	if agent.Conversation != "" && agent.Conversation != agent.ID {
		rows = append(rows, [2]string{"Conversation", agent.Conversation})
	}
	if agent.Name == core.ManagerName && a.daemon.managerClaude != "" {
		rows = append(rows, [2]string{"Claude sessions know it as", a.daemon.managerClaude})
	}
	rows = append(rows,
		[2]string{"Directory", shortPath(agent.Cwd)},
		[2]string{"Branch", branch},
		[2]string{"Model", barModel(agent)},
		[2]string{"Effort", agent.Effort},
		[2]string{"Context", contextLeft(agent.ContextTokens, agent.ContextWindow)},
		[2]string{"Permission mode", a.modeOf(agent.ID) + " (as this window last heard)"},
		[2]string{"MCP", cmp.Or(mcpWarningText(agent.MCPNeedsAuth), mcpVerb+" lists its servers")},
		[2]string{"Team", agent.Team},
		[2]string{"Task", agent.Label},
		[2]string{"Wake", a.wakeBuilds()},
	)
	return rows
}

// wakeBuilds is this window's version, and the daemon's build beside it when the
// two differ - the one version fact an operator acts on (staledaemon.go's).
func (a App) wakeBuilds() string {
	v := "v" + version.Version
	if b := a.daemon.build; b != "" && b != version.Build() {
		v += fmt.Sprintf(" · daemon on %s", b)
	}
	return v
}

// statusLines lays the rows out as a label column and a value, dropping a row
// whose value is empty and flattening one that is not Wake's to one line.
func statusLines(rows [][2]string, width int) []string {
	pad := 0
	for _, r := range rows {
		pad = max(pad, lipgloss.Width(r[0]))
	}
	var out []string
	for _, r := range rows {
		if r[1] == "" {
			continue
		}
		label := HintStyle.Render(fmt.Sprintf("%-*s", pad, r[0]))
		out = append(out, ansi.Truncate(label+"  "+oneRow(r[1]), width, ellipsis))
	}
	return out
}
