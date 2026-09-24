package core

import "github.com/google/uuid"

// The write half of the MCP asks, and the one piece of state they need.
//
// A reconnect or toggle is answered with the bare receipt a permission-mode
// change gets - {"subtype":"success"} or an error string - so the airlock
// cannot say what a receipt answers. Only the request id can, and only this
// session minted it. So the session remembers each MCP ask until its answer
// arrives and labels that answer KindMCPReply. Without this an MCP refusal
// reads as a permission-mode refusal in every attached window.

// MCPServers asks for every MCP server's live status.
func (s *Session) MCPServers() (string, error) {
	return s.askMCP(MCPResult{Ask: MCPAskServers}, EncodeMCPStatus)
}

// MCPReconnect reconnects one server.
func (s *Session) MCPReconnect(server string) (string, error) {
	return s.askMCP(MCPResult{Ask: MCPAskReconnect, Server: server}, func(id string) ([]byte, error) {
		return EncodeMCPReconnect(id, server)
	})
}

// MCPSetEnabled switches one server on or off, persistently.
func (s *Session) MCPSetEnabled(server string, enabled bool) (string, error) {
	ask := MCPResult{Ask: MCPAskDisable, Server: server}
	if enabled {
		ask.Ask = MCPAskEnable
	}
	return s.askMCP(ask, func(id string) ([]byte, error) {
		return EncodeMCPToggle(id, server, enabled)
	})
}

// askMCP writes one ask and returns its request id. The ask is remembered
// before the write - its answer can come back before writeLine returns - and
// forgotten again if nothing was written.
func (s *Session) askMCP(ask MCPResult, encode func(string) ([]byte, error)) (string, error) {
	id := uuid.NewString()
	line, err := encode(id)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	if s.mcpAsks == nil {
		s.mcpAsks = map[string]MCPResult{}
	}
	s.mcpAsks[id] = ask
	s.mu.Unlock()
	if err := s.writeLine(line); err != nil {
		s.takeMCPAsk(id)
		return "", err
	}
	return id, nil
}

// takeMCPAsk removes and returns the ask id answers, if this session sent it.
func (s *Session) takeMCPAsk(id string) (MCPResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ask, ok := s.mcpAsks[id]
	delete(s.mcpAsks, id)
	return ask, ok
}

func (s *Session) pendingMCPAsks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mcpAsks)
}

// answeredMCP labels a receipt for an MCP ask this session sent: what was
// asked, of which server, and the verdict. Anything else passes untouched.
func (s *Session) answeredMCP(ev Event) Event {
	if ev.RequestID == "" || (ev.Kind != KindControlReceipt && ev.Kind != KindMCPReply) {
		return ev
	}
	ask, ok := s.takeMCPAsk(ev.RequestID)
	if !ok {
		return ev
	}
	if ev.MCP != nil {
		ask.Servers = ev.MCP.Servers
		ask.Error = ev.MCP.Error
	}
	if ev.Control != nil {
		ask.Error = ev.Control.Error
	}
	ev.Kind = KindMCPReply
	ev.Control = nil
	ev.MCP = &ask
	return ev
}
