package ui

// `/mcp`: one agent's MCP servers, drawn the way Claude Code's own /mcp menu
// draws them - a list grouped by where each server is configured, a detail view
// with numbered actions, and the server's tools.
//
// # Why this is live rather than a health check
//
// It used to run `claude mcp list` in the agent's directory and print what came
// back. That re-dials every server from a fresh process reading the same config,
// so it could call a server connected while the agent's own connection to it
// was dead. The running session answers mcp_status itself, and mcp_reconnect and
// mcp_toggle act on it, without a model turn (testdata/stream/mcp-control.jsonl).
// So the menu asks the agent, and every action is the agent's own answer: the
// state moves on the reply, never on the keystroke - the permission mode's rule.
//
// # One menu, over the composer that opened it
//
// A bespoke type rather than the Picker for ResumePicker's reason - three levels
// and rows with several fields - and modal like it: routed above the card and
// the other pickers, it takes every key while it is up, and ⌃C closes it and
// parks (its first press stays visible for the kill switch's invariant). The
// keys are intercepted above App.key's switch, so it adds no legend entry.
//
// Signing in is mcpauth.go's: the one action the session cannot do headless.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	mcpFailed   = "asking about MCP servers"
	mcpUsage    = mcpVerb + " shows the servers of the conversation you are in, or " + mcpVerb + " " + agentPrefix + "<who> shows that agent's"
	mcpNoTarget = "whose servers? Open a conversation, or " + agentPrefix + "<who> " + mcpVerb + " from the room"
	mcpNotLive  = "%s%s is %s, so it has no servers running - " + resumeVerb + " it first"
)

type mcpLevel int

const (
	mcpList mcpLevel = iota
	mcpDetail
	mcpTools
)

// mcpAction is one numbered row of the detail view, in Claude Code's words.
type mcpAction string

const (
	actViewTools mcpAction = "View tools"
	actAuth      mcpAction = "Authenticate"
	actReconnect mcpAction = "Reconnect"
	actEnable    mcpAction = "Enable"
	actDisable   mcpAction = "Disable"
)

// mcpAskSignIn is the busy word while a sign-in holds the terminal; the other
// busy values are the core.MCPAsk* word the reply will carry.
const mcpAskSignIn = "sign-in"

// MCPMenu is the open menu. Session is the agent whose servers these are, and
// "" means there is no menu; Pane is the pane it was opened in and is drawn in.
type MCPMenu struct {
	Session string
	Name    string
	Pane    string
	Loaded  bool
	Servers []core.MCPServerStatus
	Level   mcpLevel
	Cursor  int    // the list row
	Server  string // the server the detail and tools views show
	Action  int    // the detail view's action, or the tools view's row
	Busy    string // the ask in flight on Server, until its reply lands
	Result  string // what the last action came back as
	Failed  bool
}

func (m MCPMenu) Open() bool { return m.Session != "" }

// mcpState is everything /mcp keeps on the App: the menu, cmd/wake's terminal
// hand-off, a sign-in's fleet sweep, and the asks a reply owes the daemon -
// written by mcpFollowUp, because observe returns only an App.
type mcpState struct {
	menu     MCPMenu
	handOver HandOver
	sweep    mcpSweep
	owed     []rpc.Frame
}

// mcp opens the menu for the agent the command names and asks it for its
// servers. The menu is up at once, waiting, so the keystroke visibly did
// something while the agent answers.
func (a App) mcp(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	agent, ok := a.mcpTarget(arg)
	if !ok {
		return a, nil
	}
	if agent.State == rpc.StateParked || agent.State == rpc.StateEnded {
		notice.Report(mcpNotLive, agentPrefix, agent.Name, agent.State)
		return a, nil
	}
	a.mcpUI.menu = MCPMenu{Session: agent.ID, Name: agent.Name, Pane: a.focus}
	return a, a.write(mcpFailed, rpc.Frame{Kind: rpc.FrameMCPList, SessionID: agent.ID})
}

// mcpTarget is /quit's rule with the room's roster pick added: bare is the
// conversation you are in (or, in the room, the agent the roster has picked),
// and @who is that agent.
func (a App) mcpTarget(arg string) (Agent, bool) {
	fields := strings.Fields(arg)
	switch {
	case len(fields) == 0:
		if agent, ok := a.conversationAgent(); ok {
			return agent, true
		}
		if agent, ok := a.pickedAgent(); ok {
			return agent, true
		}
		notice.Report("%s", mcpNoTarget)
	case len(fields) == 1 && strings.HasPrefix(fields[0], agentPrefix):
		agent, ok := a.fleet.ByName(strings.TrimPrefix(fields[0], agentPrefix))
		if ok {
			return agent, true
		}
		notice.Report("%s\n%s", noSuchAgent, a.handleList())
	default:
		notice.Report("%s", mcpUsage)
	}
	return Agent{}, false
}

func (a App) closeMCP() App {
	a.mcpUI.menu = MCPMenu{}
	return a
}

// ordered is the servers in the order the list draws them: by scope, closest
// first, keeping the session's own order within one.
func (m MCPMenu) ordered() []core.MCPServerStatus {
	out := make([]core.MCPServerStatus, 0, len(m.Servers))
	for _, g := range mcpGroups(m.Servers) {
		out = append(out, g.servers...)
	}
	return out
}

func (m MCPMenu) server() (core.MCPServerStatus, bool) {
	for _, s := range m.Servers {
		if s.Name == m.Server {
			return s, true
		}
	}
	return core.MCPServerStatus{}, false
}

// actions are what the detail view offers a server in this state: only what
// could help it, the way Claude Code's own menu hides Authenticate from a
// server that is already connected.
func (m MCPMenu) actions() []mcpAction {
	s, ok := m.server()
	if !ok {
		return nil
	}
	switch s.State {
	case core.MCPConnected:
		return []mcpAction{actViewTools, actReconnect, actDisable}
	case core.MCPNeedsAuth:
		return []mcpAction{actAuth, actDisable}
	case core.MCPDisabled:
		return []mcpAction{actEnable}
	}
	return []mcpAction{actReconnect, actDisable}
}

// mcpMenuKey is the menu's keys, read above App.key's switch. It takes every
// key while the menu is up, so nothing typed reaches the draft behind it.
func (a App) mcpMenuKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	menu := a.mcpUI.menu
	if !menu.Open() || menu.Pane != a.focus {
		return a, nil, false
	}
	if m.Type == tea.KeyCtrlC {
		model, cmd, _ := a.closeMCP().park()
		return model.(App), cmd, true
	}
	switch menu.Level {
	case mcpDetail:
		next, cmd := a.mcpDetailKey(menu, m)
		return next, cmd, true
	case mcpTools:
		return a.mcpToolsKey(menu, m), nil, true
	}
	return a.mcpListKey(menu, m), nil, true
}

func (a App) mcpListKey(menu MCPMenu, m tea.KeyMsg) App {
	servers := menu.ordered()
	switch m.Type {
	case tea.KeyEsc:
		return a.closeMCP()
	case tea.KeyUp, tea.KeyDown:
		menu.Cursor = clamp(menu.Cursor+arrowStep(m), 0, max(len(servers)-1, 0))
	case tea.KeyEnter:
		if menu.Cursor < len(servers) {
			if name := servers[menu.Cursor].Name; name != menu.Server {
				// Busy is the shown server's; another's in-flight ask must not
				// block this one, and its reply is then no longer this view's.
				menu.Server, menu.Busy, menu.Result, menu.Failed = name, "", "", false
			}
			menu.Level, menu.Action = mcpDetail, 0
		}
	}
	a.mcpUI.menu = menu
	return a
}

func (a App) mcpDetailKey(menu MCPMenu, m tea.KeyMsg) (App, tea.Cmd) {
	acts := menu.actions()
	switch m.Type {
	case tea.KeyEsc:
		menu.Level = mcpList
	case tea.KeyUp, tea.KeyDown:
		menu.Action = clamp(menu.Action+arrowStep(m), 0, max(len(acts)-1, 0))
	case tea.KeyEnter:
		return a.mcpRun(menu, menu.Action)
	case tea.KeyRunes:
		if d := digit(m); d > 0 {
			return a.mcpRun(menu, d-1)
		}
	}
	a.mcpUI.menu = menu
	return a, nil
}

func (a App) mcpToolsKey(menu MCPMenu, m tea.KeyMsg) App {
	s, _ := menu.server()
	switch m.Type {
	case tea.KeyEsc:
		menu.Level, menu.Action = mcpDetail, 0
	case tea.KeyUp, tea.KeyDown:
		menu.Action = clamp(menu.Action+arrowStep(m), 0, max(len(s.Tools)-1, 0))
	}
	a.mcpUI.menu = menu
	return a
}

func arrowStep(m tea.KeyMsg) int {
	if m.Type == tea.KeyUp {
		return -1
	}
	return 1
}

// digit is 1-9 for a single digit rune, and 0 for anything else.
func digit(m tea.KeyMsg) int {
	if len(m.Runes) != 1 || m.Runes[0] < '1' || m.Runes[0] > '9' {
		return 0
	}
	return int(m.Runes[0] - '0')
}

// mcpRun is one action, chosen. Nothing moves until the agent answers, and a
// second action waits for the first's reply rather than racing it.
func (a App) mcpRun(menu MCPMenu, i int) (App, tea.Cmd) {
	acts := menu.actions()
	if i < 0 || i >= len(acts) || menu.Busy != "" {
		a.mcpUI.menu = menu
		return a, nil
	}
	menu.Action, menu.Result, menu.Failed = i, "", false
	s, _ := menu.server()
	kind, ask := "", ""
	switch acts[i] {
	case actViewTools:
		menu.Level, menu.Action = mcpTools, 0
		a.mcpUI.menu = menu
		return a, nil
	case actAuth:
		return a.authenticate(menu, s)
	case actReconnect:
		kind, ask = rpc.FrameMCPReconnect, core.MCPAskReconnect
	case actEnable:
		kind, ask = rpc.FrameMCPEnable, core.MCPAskEnable
	case actDisable:
		kind, ask = rpc.FrameMCPDisable, core.MCPAskDisable
	}
	menu.Busy = ask
	a.mcpUI.menu = menu
	return a, a.write(mcpFailed, rpc.Frame{Kind: kind, SessionID: menu.Session, Text: s.Name})
}

// observedMCP folds an agent's MCP reply into the menu showing its servers and
// into a sign-in's sweep. A reply nobody is waiting for changes nothing - every
// window receives every agent's replies.
func (a App) observedMCP(sessionID string, ev core.Event) App {
	if ev.Kind != core.KindMCPReply || ev.MCP == nil {
		return a
	}
	r := *ev.MCP
	a = a.sweepSaw(sessionID, r)
	menu := a.mcpUI.menu
	if !menu.Open() || menu.Session != sessionID {
		return a
	}
	if r.Ask == core.MCPAskServers {
		menu.Loaded, menu.Servers = true, r.Servers
		menu.Cursor = clamp(menu.Cursor, 0, max(len(r.Servers)-1, 0))
		if _, still := menu.server(); menu.Level != mcpList && !still {
			menu.Level = mcpList
		}
		menu.Action = clamp(menu.Action, 0, max(len(menu.actions())-1, 0))
		a.mcpUI.menu = menu
		return a
	}
	if r.Server != menu.Server || r.Ask != menu.Busy {
		return a
	}
	menu.Busy = ""
	if r.Error != "" {
		menu.Result, menu.Failed = r.Error, true
	} else {
		menu.Result = mcpDone[r.Ask]
		a = a.oweMCP(rpc.Frame{Kind: rpc.FrameMCPList, SessionID: sessionID})
	}
	a.mcpUI.menu = menu
	return a
}

// mcpDone is what a successful action says, beside the ✔.
var mcpDone = map[string]string{
	core.MCPAskReconnect: "reconnected",
	core.MCPAskEnable:    "enabled",
	core.MCPAskDisable:   "disabled",
}

func (a App) oweMCP(f rpc.Frame) App {
	a.mcpUI.owed = append(append([]rpc.Frame(nil), a.mcpUI.owed...), f)
	return a
}

// mcpFollowUp writes what the folds owed the daemon: a fresh list after an
// action landed, and a sweep's reconnects. Run from settle.
func (a App) mcpFollowUp() (App, tea.Cmd) {
	if len(a.mcpUI.owed) == 0 {
		return a, nil
	}
	owed := a.mcpUI.owed
	a.mcpUI.owed = nil
	return a, a.write(mcpFailed, owed...)
}

// reconciledMCP closes a menu whose agent has stopped running and drops a
// sweep's agents that did, run on every fleet report the way reconcileRewind is.
func (a App) reconciledMCP() App {
	if id := a.mcpUI.menu.Session; id != "" && !a.mcpLive(id) {
		a = a.closeMCP()
	}
	return a.prunedSweep()
}

// mcpNoAnswer is a menu action whose answer may have been lost in a gap.
const mcpNoAnswer = "no answer came back; the list below is fresh - try again"

// forgotMCP settles every MCP ask whose answer may have been lost - a frame gap
// or a reattach - so nothing waits forever on it: the menu stops waiting and
// re-asks for its list, and a sweep reports what it has.
func (a App) forgotMCP() App {
	menu := a.mcpUI.menu
	if menu.Open() {
		if menu.Busy != "" && menu.Busy != mcpAskSignIn {
			menu.Busy, menu.Result, menu.Failed = "", mcpNoAnswer, true
			a.mcpUI.menu = menu
		}
		a = a.oweMCP(rpc.Frame{Kind: rpc.FrameMCPList, SessionID: menu.Session})
	}
	// Not reported as failing: their answers are unknown, not refusals.
	sw := a.mcpUI.sweep
	for id := range sw.asked {
		sw = sw.without(id)
	}
	for id := range sw.redial {
		sw = sw.without(id)
	}
	a.mcpUI.sweep = sw
	return a.finishedSweep()
}

// mcpRefused is the daemon refusing an ask about id (the session ended, its
// queue was full, a server name was blank): the ask will never be answered, so
// it stops being waited on. The refusal itself reaches the notice row as ever.
func (a App) mcpRefused(id, why string) App {
	menu := a.mcpUI.menu
	switch {
	case !menu.Open() || menu.Session != id:
	case !menu.Loaded:
		a = a.closeMCP()
	case menu.Busy != "" && menu.Busy != mcpAskSignIn:
		menu.Busy, menu.Result, menu.Failed = "", why, true
		a.mcpUI.menu = menu
	}
	sw := a.mcpUI.sweep
	if sw.asked[id] || sw.redial[id] {
		a.mcpUI.sweep = sw.without(id).failing(a.agentName(id))
		return a.finishedSweep()
	}
	return a
}

func (a App) mcpLive(id string) bool {
	agent, ok := a.fleet.Agent(id)
	return ok && agent.State != rpc.StateParked && agent.State != rpc.StateEnded
}

// title heads the menu's box.
func (m MCPMenu) title() string {
	switch m.Level {
	case mcpDetail:
		return fmt.Sprintf("%s · %s%s", m.Server, agentPrefix, m.Name)
	case mcpTools:
		s, _ := m.server()
		return m.Server + " · " + plural(len(s.Tools), "tool")
	}
	return "Manage MCP servers · " + agentPrefix + m.Name
}
