package daemon

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Each MCP frame reaches the agent's stdin as the control request it names.
func TestEachMCPFrameWritesItsControlRequest(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	for _, tc := range []struct {
		frame rpc.Frame
		want  []string
	}{
		{rpc.Frame{Kind: rpc.FrameMCPList, SessionID: idAlpha}, []string{`"subtype":"mcp_status"`}},
		{rpc.Frame{Kind: rpc.FrameMCPReconnect, SessionID: idAlpha, Text: "linear"},
			[]string{`"subtype":"mcp_reconnect"`, `"serverName":"linear"`}},
		{rpc.Frame{Kind: rpc.FrameMCPDisable, SessionID: idAlpha, Text: "echo"},
			[]string{`"subtype":"mcp_toggle"`, `"serverName":"echo"`, `"enabled":false`}},
		{rpc.Frame{Kind: rpc.FrameMCPEnable, SessionID: idAlpha, Text: "echo"},
			[]string{`"subtype":"mcp_toggle"`, `"enabled":true`}},
	} {
		c.send(tc.frame)
		line := c.awaitEvent(idAlpha, tc.want[0]).Event.Text
		for _, w := range tc.want {
			if !strings.Contains(line, w) {
				t.Errorf("%s: stdin line %s lacks %s", tc.frame.Kind, line, w)
			}
		}
	}
}

// A frame that names no server is refused to the client that sent it, with
// nothing written.
func TestAnMCPFrameWithoutAServerIsRefused(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameMCPReconnect, SessionID: idAlpha})
	f := c.await("an error", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "server") {
		t.Errorf("refusal %q does not say what was missing", f.Text)
	}
}

// The answers come back as MCP replies - never as generic receipts a window
// would read as a permission-mode refusal - carrying what was asked of which
// server.
func TestTheAgentsAnswersArriveAsMCPReplies(t *testing.T) {
	fakeClaudeOnPath(t, "mcp")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "ready")

	reply := func() *core.MCPResult {
		t.Helper()
		f := c.await("an MCP reply", func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindMCPReply
		})
		return f.Event.MCP
	}

	c.send(rpc.Frame{Kind: rpc.FrameMCPList, SessionID: idAlpha})
	if r := reply(); r.Ask != core.MCPAskServers || len(r.Servers) != 1 || r.Servers[0].State != core.MCPNeedsAuth {
		t.Errorf("status reply = %+v", r)
	}
	c.send(rpc.Frame{Kind: rpc.FrameMCPReconnect, SessionID: idAlpha, Text: "linear"})
	if r := reply(); r.Ask != core.MCPAskReconnect || r.Server != "linear" || r.Error != "Server status: needs-auth" {
		t.Errorf("reconnect reply = %+v", r)
	}
	c.send(rpc.Frame{Kind: rpc.FrameMCPDisable, SessionID: idAlpha, Text: "linear"})
	if r := reply(); r.Ask != core.MCPAskDisable || r.Error != "" {
		t.Errorf("disable reply = %+v", r)
	}
}

// Reading or reconnecting a server touches no permission ask, so an agent
// blocked on one still answers - which is when somebody is most likely to be
// looking at why it is stuck.
func TestAnAgentBlockedOnAnAskStillReportsItsServers(t *testing.T) {
	fakeClaudeOnPath(t, "ask")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitState(idAlpha, rpc.StateBlocked)

	// The fake says nothing back, so the proof is ordering: a refusal of the
	// list would be the first error to come back, ahead of the one a
	// deliberately bad frame behind it earns.
	c.send(rpc.Frame{Kind: rpc.FrameMCPList, SessionID: idAlpha})
	c.send(rpc.Frame{Kind: rpc.FrameMCPReconnect, SessionID: idAlpha})
	f := c.await("an error", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })
	if !strings.Contains(f.Text, "server") {
		t.Errorf("the first refusal was %q; the status ask was refused while blocked", f.Text)
	}
}
