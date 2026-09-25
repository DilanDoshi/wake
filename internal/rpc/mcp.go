package rpc

// The MCP control a client may ask of one running session: its servers' live
// status, and reconnecting or switching one on or off. Each names the session
// in SessionID; the three that act on one server name it in Text, spelled as
// that session's own status reply gave it.
//
// There is no reply frame. The session's answer reaches every client on the
// event stream as a core.KindMCPReply, the way a mode or rewind receipt does,
// and a client folds only the one it is waiting for.
//
// Declared here rather than in wire.go, which is at the file-size hard max.
// The values are Wake's own words: Claude's (mcp_status, mcp_reconnect,
// mcp_toggle) are airlock vocabulary.
const (
	FrameMCPList      = "mcp_list"    // client → daemon: every MCP server's live status
	FrameMCPReconnect = "mcp_redial"  // client → daemon: reconnect one server
	FrameMCPEnable    = "mcp_enable"  // client → daemon: switch one server on
	FrameMCPDisable   = "mcp_disable" // client → daemon: switch one server off
)
