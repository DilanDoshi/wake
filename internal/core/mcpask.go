package core

// The write half of the MCP asks, and the one piece of state they need -
// shared by a workflow stop, whose receipt is just as bare.
//
// A reconnect, a toggle or a stop_task is answered with the bare receipt a
// permission-mode change gets - {"subtype":"success"} or an error string - so
// the airlock cannot say what a receipt answers. Only the request id can, and
// only this session minted it. So the session remembers each such ask until
// its answer arrives and labels that answer KindMCPReply or KindStopReceipt.
// Without this a refusal reads as a permission-mode refusal in every attached
// window, and clears a mode change still waiting on its own receipt.

// The caller mints the request id, unlike Interrupt and SetMode: the daemon
// has to know it before the write, to send the answer to the client that asked.

// MCPServers asks for every MCP server's live status.
func (s *Session) MCPServers(id string) error {
	return s.askMCP(id, MCPResult{Ask: MCPAskServers}, EncodeMCPStatus)
}

// sentAsk is what a remembered ask's receipt is labelled as: KindMCPReply,
// with what was asked, or KindStopReceipt.
type sentAsk struct {
	kind EventKind
	mcp  MCPResult
}

// MCPReconnect reconnects one server.
func (s *Session) MCPReconnect(id, server string) error {
	return s.askMCP(id, MCPResult{Ask: MCPAskReconnect, Server: server}, func(id string) ([]byte, error) {
		return EncodeMCPReconnect(id, server)
	})
}

// MCPSetEnabled switches one server on or off, persistently.
func (s *Session) MCPSetEnabled(id, server string, enabled bool) error {
	ask := MCPResult{Ask: MCPAskDisable, Server: server}
	if enabled {
		ask.Ask = MCPAskEnable
	}
	return s.askMCP(id, ask, func(id string) ([]byte, error) {
		return EncodeMCPToggle(id, server, enabled)
	})
}

func (s *Session) askMCP(id string, ask MCPResult, encode func(string) ([]byte, error)) error {
	return s.ask(id, sentAsk{kind: KindMCPReply, mcp: ask}, encode)
}

// ask writes one labelled ask. It is remembered before the write - its answer
// can come back before writeLine returns - and forgotten again if nothing was
// written.
func (s *Session) ask(id string, sent sentAsk, encode func(string) ([]byte, error)) error {
	line, err := encode(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.asks == nil {
		s.asks = map[string]sentAsk{}
	}
	s.asks[id] = sent
	s.mu.Unlock()
	if err := s.writeLine(line); err != nil {
		s.takeAsk(id)
		return err
	}
	return nil
}

// takeAsk removes and returns the ask id answers, if this session sent it.
func (s *Session) takeAsk(id string) (sentAsk, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sent, ok := s.asks[id]
	delete(s.asks, id)
	return sent, ok
}

func (s *Session) pendingAsks() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.asks)
}

// answeredMCP labels a receipt for an ask this session sent: a stop's as a
// stop's, and an MCP ask's with what was asked, of which server, and the
// verdict. Anything else passes untouched.
func (s *Session) answeredMCP(ev Event) Event {
	if ev.RequestID == "" || (ev.Kind != KindControlReceipt && ev.Kind != KindMCPReply) {
		return ev
	}
	sent, ok := s.takeAsk(ev.RequestID)
	if !ok {
		return ev
	}
	if sent.kind == KindStopReceipt {
		ev.Kind = KindStopReceipt // Control keeps the verdict
		return ev
	}
	ask := sent.mcp
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
