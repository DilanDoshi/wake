package ui

// Authenticate: the one /mcp action a headless session cannot do for itself.
//
// Claude Code's /mcp signs a server in by opening the browser and listening for
// the redirect, inside its own interactive process. A Wake agent is headless
// and has no control request for it, and `claude mcp login <server>` - the same
// flow from a shell - refuses anything but a terminal on its stdin. So the
// action hands the operator's real terminal to that command for as long as it
// runs (cmd/wake's HandOver: Wake stops drawing and reading, nothing emulated),
// and the browser opens exactly as it does from Claude's own menu.
//
// Signing in stores the token where every session reads it, but a running
// session only picks it up when it reconnects. So a finished sign-in reconnects
// the agent whose menu asked, then asks every other live agent for its servers
// and reconnects each one stuck on the same server: one sign-in fixes the fleet.

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// HandOver runs cmd on the operator's own terminal until it exits, printing
// header first. cmd/wake supplies it; with none (no terminal) Authenticate says
// what to run instead.
type HandOver func(cmd *exec.Cmd, header string) tea.ExecCommand

func (a App) WithHandOver(h HandOver) App { a.mcpUI.handOver = h; return a }

// mcpSignedInMsg is the hand-over returning: the sign-in exited, and Err says
// whether it finished.
type mcpSignedInMsg struct {
	Session, Server string
	Err             error
}

const (
	mcpSignInHeader   = "Signing in to %s for Wake - approve it in your browser. ⌃C cancels."
	mcpSignInHere     = "sign in to %s from a terminal: %s"
	mcpFlagName       = "a server named like a flag cannot be signed in to from here"
	mcpSignInUnfinish = "sign-in did not finish: %v"
)

// mcpLoginArgs is claude's own sign-in for one server.
func mcpLoginArgs(server string) []string { return []string{"claude", "mcp", "login", server} }

func (a App) authenticate(menu MCPMenu, s core.MCPServerStatus) (App, tea.Cmd) {
	if strings.HasPrefix(s.Name, "-") {
		menu.Result, menu.Failed = mcpFlagName, true
		a.mcpUI.menu = menu
		return a, nil
	}
	agent, _ := a.fleet.Agent(menu.Session)
	args := mcpLoginArgs(s.Name)
	if a.mcpUI.handOver == nil {
		line := strings.Join(args, " ")
		if agent.Cwd != "" {
			line = "cd " + shortPath(agent.Cwd) + " && " + line
		}
		notice.Report(mcpSignInHere, s.Name, line)
		a.mcpUI.menu = menu
		return a, nil
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = agent.Cwd
	menu.Busy = mcpAskSignIn
	a.mcpUI.menu = menu
	id, name := menu.Session, s.Name
	return a, tea.Exec(a.mcpUI.handOver(cmd, fmt.Sprintf(mcpSignInHeader, name)), func(err error) tea.Msg {
		return mcpSignedInMsg{Session: id, Server: name, Err: err}
	})
}

// mcpSignedIn is the terminal coming back. A sign-in that finished reconnects
// the asking agent and starts the sweep; one that did not says so and changes
// nothing.
func (a App) mcpSignedIn(m mcpSignedInMsg) (App, tea.Cmd) {
	menu := a.mcpUI.menu
	mine := menu.Open() && menu.Session == m.Session && menu.Server == m.Server
	if m.Err != nil {
		if mine {
			menu.Busy, menu.Result, menu.Failed = "", fmt.Sprintf(mcpSignInUnfinish, m.Err), true
			a.mcpUI.menu = menu
		} else {
			notice.Report("%s: "+mcpSignInUnfinish, m.Server, m.Err)
		}
		return a, nil
	}
	if mine {
		menu.Busy, menu.Result = core.MCPAskReconnect, ""
		a.mcpUI.menu = menu
	}
	sw := mcpSweep{server: m.Server, asked: map[string]bool{}, redial: map[string]bool{m.Session: true}}
	frames := []rpc.Frame{{Kind: rpc.FrameMCPReconnect, SessionID: m.Session, Text: m.Server}}
	for _, agent := range a.live() {
		if agent.ID != m.Session {
			sw.asked[agent.ID] = true
			frames = append(frames, rpc.Frame{Kind: rpc.FrameMCPList, SessionID: agent.ID})
		}
	}
	a.mcpUI.sweep = sw
	return a, a.write(mcpFailed, frames...)
}

// mcpSweep is a finished sign-in's reconnect across the fleet: the agents asked
// for their servers, the ones being reconnected, and how each reconnect went.
type mcpSweep struct {
	server        string
	asked, redial map[string]bool
	done, failed  []string
}

func (sw mcpSweep) active() bool { return sw.server != "" }

// without is sw with id no longer waited on, maps copied rather than edited.
func (sw mcpSweep) without(id string) mcpSweep {
	asked, redial := make(map[string]bool, len(sw.asked)), make(map[string]bool, len(sw.redial))
	for k := range sw.asked {
		if k != id {
			asked[k] = true
		}
	}
	for k := range sw.redial {
		if k != id {
			redial[k] = true
		}
	}
	sw.asked, sw.redial = asked, redial
	return sw
}

// sweepSaw folds one reply into the sweep: a server list from an asked agent
// (reconnect it if it is stuck on the server), or a reconnect's verdict.
func (a App) sweepSaw(id string, r core.MCPResult) App {
	sw := a.mcpUI.sweep
	switch {
	case !sw.active():
		return a
	case r.Ask == core.MCPAskServers && sw.asked[id]:
		stuck := false
		for _, s := range r.Servers {
			stuck = stuck || (s.Name == sw.server && s.State == core.MCPNeedsAuth)
		}
		sw = sw.without(id)
		if stuck {
			sw.redial[id] = true
			a = a.oweMCP(rpc.Frame{Kind: rpc.FrameMCPReconnect, SessionID: id, Text: sw.server})
		}
	case r.Ask == core.MCPAskReconnect && r.Server == sw.server && sw.redial[id]:
		name := a.agentName(id)
		sw = sw.without(id)
		if r.Error == "" {
			sw.done = append(append([]string(nil), sw.done...), name)
		} else {
			sw.failed = append(append([]string(nil), sw.failed...), name)
		}
	default:
		return a
	}
	a.mcpUI.sweep = sw
	return a.finishedSweep()
}

// prunedSweep stops waiting on agents that stopped running, whose answers will
// never come.
func (a App) prunedSweep() App {
	sw := a.mcpUI.sweep
	if !sw.active() {
		return a
	}
	for id := range sw.asked {
		if !a.mcpLive(id) {
			sw = sw.without(id)
		}
	}
	for id := range sw.redial {
		if !a.mcpLive(id) {
			sw = sw.without(id)
		}
	}
	a.mcpUI.sweep = sw
	return a.finishedSweep()
}

// finishedSweep says how the sweep went, once nothing is left to wait on.
func (a App) finishedSweep() App {
	sw := a.mcpUI.sweep
	if !sw.active() || len(sw.asked) > 0 || len(sw.redial) > 0 {
		return a
	}
	a.mcpUI.sweep = mcpSweep{}
	line := sw.server + " signed in"
	if len(sw.done) > 0 {
		line += " · reconnected on " + andList(sw.done)
	}
	if len(sw.failed) > 0 {
		line += " · still failing on " + andList(sw.failed)
	}
	notice.Report("%s", line)
	return a
}

// andList is "a", "a and b", "a, b and c", sorted so the line reads the same
// whichever reply landed first.
func andList(names []string) string {
	s := append([]string(nil), names...)
	sort.Strings(s)
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}
