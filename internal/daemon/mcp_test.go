package daemon

import (
	"bytes"
	"log"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

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

// An MCP answer goes to the window that asked and no other: every window
// matches an answer by agent, server and ask, so two windows asking the same
// thing at once would otherwise each take the other's.
func TestAnMCPAnswerGoesOnlyToTheWindowThatAsked(t *testing.T) {
	fakeClaudeOnPath(t, "mcp")
	d := startDaemon(t)
	asker := attach(t, d.socket)
	other := attach(t, d.socket)
	asker.spawn(idAlpha, "sydney")
	asker.awaitEvent(idAlpha, "ready")
	other.awaitEvent(idAlpha, "ready")

	asker.send(rpc.Frame{Kind: rpc.FrameMCPList, SessionID: idAlpha})
	asker.await("its MCP answer", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindMCPReply
	})

	// Anything the session says afterwards still reaches both, so once the
	// other window has it, it would have had the answer too.
	asker.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after"})
	other.awaitEvent(idAlpha, "after")
	for _, f := range other.seen {
		if f.Event != nil && f.Event.Kind == core.KindMCPReply {
			t.Fatalf("another window received the asker's MCP answer: %+v", f.Event.MCP)
		}
	}
}

// A session opens with the handshake, as its first line on stdin - which is
// what makes it load claude.ai connectors at all.
func TestASessionOpensWithTheHandshake(t *testing.T) {
	fakeClaudeOnPath(t, "")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	line := c.awaitEvent(idAlpha, "echo: ").Event.Text
	if !strings.Contains(line, `"subtype":"initialize"`) {
		t.Fatalf("the session's first line was %s, not the handshake", line)
	}
}

// After the handshake the daemon connects every claude.ai connector that is
// signed in - a reconnect is what connects one - and touches nothing else: the
// ordinary server is the operator's own to sign in to from /mcp. None of it
// reaches a window: the handshake's reply and the sweep's are the daemon's.
func TestTheHandshakeConnectsClaudeAIConnectorsQuietly(t *testing.T) {
	fakeClaudeOnPath(t, "connectors")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "reconnect asked: claude.ai Gmail")
	c.awaitEvent(idAlpha, "reconnect asked: claude.ai Slack")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after"})
	c.awaitEvent(idAlpha, "echo: ")
	for _, f := range c.seen {
		ev := f.Event
		switch {
		case ev == nil:
		case strings.Contains(ev.Text, "reconnect asked: firecrawl"):
			t.Error("the sweep reconnected an ordinary server, which is the operator's to sign in to")
		case ev.Kind == core.KindMCPReply || ev.Kind == core.KindControlReceipt:
			t.Errorf("a window received the daemon's own reply: %+v", ev)
		}
	}
}

// A refusal of the daemon's own ask reaches no window, so the log is the one
// place anybody asking why a connector never connected can look.
func TestTheDaemonLogsWhatRefusedItsOwnAsk(t *testing.T) {
	logged := lockedLog(t)
	fakeClaudeOnPath(t, "connectors")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "reconnect asked: claude.ai Slack")

	want := "claude.ai Slack: Server status: needs-auth"
	for deadline := time.Now().Add(testTimeout); !strings.Contains(logged.String(), want); {
		if time.Now().After(deadline) {
			t.Fatalf("the refused reconnect was not logged as %q:\n%s", want, logged)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A refused handshake loaded nothing to connect: it is logged, and the daemon
// asks nothing further.
func TestARefusedHandshakeIsLoggedAndAsksNothing(t *testing.T) {
	logged := lockedLog(t)
	a := &agent{id: idAlpha, initID: "init-1", in: make(chan pending, 1)}
	refusal := core.Event{Kind: core.KindControlReceipt, RequestID: "init-1", Control: &core.ControlResult{Error: "not now"}}
	if !a.handshakeAnswered(refusal) {
		t.Fatal("the handshake's refusal was not recognised as its reply")
	}
	if !strings.Contains(logged.String(), "not now") {
		t.Errorf("the refusal was not logged:\n%s", logged)
	}
	if len(a.in) != 0 {
		t.Error("a refused handshake still asked for the servers it loaded")
	}
}

// lockedLog captures logf until the test ends; the daemon logs from its own
// goroutines, so reads and writes share a lock.
func lockedLog(t *testing.T) *lockedBuffer {
	t.Helper()
	b := &lockedBuffer{}
	log.SetOutput(b)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return b
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
