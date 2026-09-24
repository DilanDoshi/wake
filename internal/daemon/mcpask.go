package daemon

import (
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
		a.takeMCPAsker(id)
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

func (a *agent) takeMCPAsker(id string) *client {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.mcpAskers[id]
	delete(a.mcpAskers, id)
	return c
}

// mcpAsker is the client that asked the MCP question ev answers, or nil for
// every other event - which is broadcast as before.
func (a *agent) mcpAsker(ev core.Event) *client {
	if ev.Kind != core.KindMCPReply || ev.RequestID == "" {
		return nil
	}
	return a.takeMCPAsker(ev.RequestID)
}
