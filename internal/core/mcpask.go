package core

// The write half of the MCP asks, and the one piece of state they need -
// shared by a workflow stop and a file rewind, whose receipts are just as bare.
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

// Initialize is the handshake that makes this session load claude.ai
// connectors. Its reply is an ordinary receipt carrying id, and the caller
// that minted id owns it; nothing here remembers it.
func (s *Session) Initialize(id string) error {
	line, err := EncodeInitialize(id)
	if err != nil {
		return err
	}
	return s.writeLine(line)
}

// MCPServers asks for every MCP server's live status.
func (s *Session) MCPServers(id string) error {
	return s.askMCP(id, MCPResult{Ask: MCPAskServers}, EncodeMCPStatus)
}

// sentAsk is what a remembered ask's receipt is labelled as: KindMCPReply,
// with what was asked, KindStopReceipt, or KindFilesRewindReceipt, with the
// message it was aimed at and whether it was a preview.
type sentAsk struct {
	kind  EventKind
	mcp   MCPResult
	files FilesRewind
}

// RewindFiles asks this session to restore its files to their state at the
// user message target, or with preview only to say what that would change.
// The caller mints id, as for an MCP ask: a preview's answer goes only to the
// window that asked, and a restore's success may have a conversation rewind
// waiting on it.
func (s *Session) RewindFiles(id, target string, preview bool) error {
	return s.ask(id, sentAsk{kind: KindFilesRewindReceipt, files: FilesRewind{Target: target, Preview: preview}},
		func(id string) ([]byte, error) { return EncodeRewindFiles(id, target, preview) })
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
// stop's, a file rewind's with its target, and an MCP ask's with what was
// asked, of which server, and the verdict. Anything else passes untouched.
func (s *Session) answeredMCP(ev Event) Event {
	switch {
	case ev.RequestID == "":
		return ev
	case ev.Kind != KindControlReceipt && ev.Kind != KindMCPReply && ev.Kind != KindFilesRewindReceipt:
		return ev
	}
	sent, ok := s.takeAsk(ev.RequestID)
	if !ok {
		return ev
	}
	switch sent.kind {
	case KindStopReceipt:
		ev.Kind = KindStopReceipt // Control keeps the verdict
		return ev
	case KindFilesRewindReceipt:
		return answeredFiles(ev, sent.files)
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

// answeredFiles labels a file rewind's receipt with what was asked. A refused
// restore arrives bare (Control.Error), a preview or a restore with its payload.
func answeredFiles(ev Event, asked FilesRewind) Event {
	files := asked
	switch {
	case ev.Files != nil:
		files = *ev.Files
		files.Target, files.Preview = asked.Target, asked.Preview
	case ev.Control != nil:
		files.Error = ev.Control.Error
	}
	ev.Kind = KindFilesRewindReceipt
	ev.Control = nil
	ev.Files = &files
	return ev
}
