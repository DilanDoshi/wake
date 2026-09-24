package daemon

import "github.com/DilanDoshi/wake/internal/rpc"

// askMCP writes the MCP ask a client's frame names. The answer is the session's
// KindMCPReply on the event stream, which reaches every client the way a mode
// receipt does, so nothing is held here.
//
// Not refused while a permission ask is outstanding, unlike FrameMode:
// reading, reconnecting or switching a server changes nothing about the ask,
// and a blocked agent is when somebody is most likely to be looking. A blank
// server comes back ErrNotWritten, which apply refuses to the asker.
func (a *agent) askMCP(p pending) error {
	f := p.frame
	var err error
	switch f.Kind {
	case rpc.FrameMCPList:
		_, err = a.sess.MCPServers()
	case rpc.FrameMCPReconnect:
		_, err = a.sess.MCPReconnect(f.Text)
	default:
		_, err = a.sess.MCPSetEnabled(f.Text, f.Kind == rpc.FrameMCPEnable)
	}
	return err
}
