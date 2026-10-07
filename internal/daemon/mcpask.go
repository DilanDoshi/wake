package daemon

import (
	"strings"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// askMCP writes the MCP ask a client's frame names, remembering which client
// asked so fanOut can send the answer to it alone: every window matches an
// answer by agent, server and ask, so two windows asking the same thing at once
// would each take the other's. The id is minted and the asker recorded before
// the write, because the answer can arrive before the write returns.
//
// Not refused while a permission ask is outstanding, unlike FrameMode:
// reading, reconnecting or switching a server changes nothing about the ask,
// and a blocked agent is when somebody is most likely to be looking. A blank
// server comes back ErrNotWritten, which apply refuses to the asker.
func (a *agent) askMCP(p pending) error {
	f := p.frame
	id := uuid.NewString()
	a.noteMCPAsker(id, p.from)
	var err error
	switch f.Kind {
	case rpc.FrameMCPList:
		err = a.sess.MCPServers(id)
	case rpc.FrameMCPReconnect:
		err = a.sess.MCPReconnect(id, f.Text)
	default:
		err = a.sess.MCPSetEnabled(id, f.Text, f.Kind == rpc.FrameMCPEnable)
	}
	if err != nil {
		_, _ = a.takeMCPAsker(id)
	}
	return err
}

func (a *agent) noteMCPAsker(id string, c *client) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.mcpAskers == nil {
		a.mcpAskers = map[string]*client{}
	}
	a.mcpAskers[id] = c
}

func (a *agent) takeMCPAsker(id string) (*client, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.mcpAskers[id]
	delete(a.mcpAskers, id)
	return c, ok
}

// mcpAsker is the client that asked the MCP question ev answers, and whether
// anybody did: nil and true is the daemon's own ask. Every other event is
// nobody's, and is broadcast as before.
func (a *agent) mcpAsker(ev core.Event) (*client, bool) {
	if ev.Kind != core.KindMCPReply || ev.RequestID == "" {
		return nil, false
	}
	return a.takeMCPAsker(ev.RequestID)
}

// handshake opens the session the way every SDK host does: without it a
// headless session never loads the operator's claude.ai connectors (probed
// 2026-09-27, 2.1.281). Written straight to stdin before serveInput starts, so
// it is the first line; its reply is the daemon's alone and reaches no client.
func (a *agent) handshake() {
	id := uuid.NewString()
	a.mu.Lock()
	a.initID = id
	a.mu.Unlock()
	if err := a.sess.Initialize(id); err != nil {
		logf("wake: session %s: handshake not sent: %v", a.id, err)
	}
}

// handshakeAnswered swallows the handshake's reply and asks, as the daemon, for
// the servers it loaded. A loaded connector reads needs-auth until something
// reconnects it, even one signed in on claude.ai (mcp-connectors.jsonl), so
// connectorsReported is what makes one usable. A refused handshake loaded none.
// The reply also names the session's commands, which learnCommands keeps.
func (a *agent) handshakeAnswered(ev core.Event) bool {
	a.mu.Lock()
	mine := ev.RequestID != "" && ev.RequestID == a.initID
	if mine {
		a.initID = ""
	}
	a.mu.Unlock()
	switch {
	case !mine:
	case ev.Control != nil && ev.Control.Error != "":
		logf("wake: session %s: handshake refused: %s", a.id, ev.Control.Error)
	default:
		a.learnCommands(ev)
		a.askOwn(rpc.Frame{Kind: rpc.FrameMCPList, SessionID: a.id})
	}
	return mine
}

// learnCommands keeps the commands the handshake's reply names, for the report:
// a fresh agent sends no init until its first turn, so this is all its menu has.
// Never over an init's, which is newer and renews itself every turn.
func (a *agent) learnCommands(ev core.Event) {
	if ev.Session == nil || len(ev.Session.SlashCommands) == 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.commands) == 0 {
		a.commands = ev.Session.SlashCommands
	}
}

// connectorsReported reconnects each claude.ai connector a status reply shows
// waiting. One not signed in on claude.ai refuses at once, so trying costs
// nothing; an ordinary server is the operator's to sign in to from /mcp.
func (a *agent) connectorsReported(ev core.Event) {
	if ev.MCP == nil {
		return
	}
	if ev.MCP.Error != "" { // no window will say so, so the log does
		logf("wake: session %s: %s: %s", a.id, strings.TrimSpace(ev.MCP.Ask+" "+ev.MCP.Server), ev.MCP.Error)
		return
	}
	if ev.MCP.Ask != core.MCPAskServers {
		return
	}
	for _, s := range ev.MCP.Servers {
		if s.Scope == core.MCPScopeClaudeAI && s.State == core.MCPNeedsAuth {
			a.askOwn(rpc.Frame{Kind: rpc.FrameMCPReconnect, SessionID: a.id, Text: s.Name})
		}
	}
}

// askOwn queues an ask the daemon makes for itself, with no client behind it.
func (a *agent) askOwn(f rpc.Frame) {
	if err := a.submit(nil, f); err != nil {
		logf("wake: session %s: %v", a.id, err)
	}
}
