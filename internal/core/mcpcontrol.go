package core

// Wake's vocabulary for the MCP control requests a running session answers:
// what its servers are doing, and reconnecting or switching one on or off. The
// Claude spelling of each request lives in encode.go; these are the words the
// rest of Wake reads.

// The asks, in Wake's words. MCPResult.Ask names which one a reply answers.
const (
	MCPAskServers   = "servers"
	MCPAskReconnect = "reconnect"
	MCPAskEnable    = "enable"
	MCPAskDisable   = "disable"
)

// MCPTool is one tool a connected server offers. The session reports no
// description (recorded against 2.1.281, with tool search on and off), so a
// name and whether it only reads are all there is to show.
type MCPTool struct {
	Name     string `json:"name"`
	ReadOnly bool   `json:"read_only,omitempty"`
}

// MCPServerStatus is one server as the running session sees it - live, rather
// than as a fresh `claude mcp list` would re-dial it from the same config.
//
// State is an open set (MCPConnected, MCPPending, MCPNeedsAuth, MCPFailed,
// MCPDisabled so far): a value this build has not seen arrives intact.
type MCPServerStatus struct {
	Name  string `json:"name"`
	State string `json:"state"`

	// Error is why a failed server failed, in the server's or the OS's words.
	Error string `json:"error,omitempty"`

	// Scope is the config it came from: user, project, local, dynamic, ...
	Scope string `json:"scope,omitempty"`

	// Transport is stdio, http, sse, ...; Target is the URL for a remote server
	// and the command line for a local one.
	Transport string `json:"transport,omitempty"`
	Target    string `json:"target,omitempty"`

	// Info is the server's own name and version, once it has connected.
	Info string `json:"info,omitempty"`

	Tools []MCPTool `json:"tools,omitempty"`
}

// MCPResult is the answer to one MCP ask: the servers for MCPAskServers, and
// nothing but a verdict for the others. Error is empty on success.
type MCPResult struct {
	Ask     string            `json:"ask"`
	Server  string            `json:"server,omitempty"`
	Servers []MCPServerStatus `json:"servers,omitempty"`
	Error   string            `json:"error,omitempty"`
}
