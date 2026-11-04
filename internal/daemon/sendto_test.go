package daemon

// The send fake (main_test.go's fakeSend) registers in Claude Code's session
// registry as claude does, under its own process; the records written here are
// sessions no fleet agent's process holds.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// outside writes a record for a process in no agent's group: pid 1.
func outside(t *testing.T, config, file, id, name, socket string) {
	t.Helper()
	if err := writeRecord(config, file, 1, id, name, socket); err != nil {
		t.Fatal(err)
	}
}

// pidOf is the process the registry records for session id.
func pidOf(t *testing.T, config, id string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(config, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		raw, _ := os.ReadFile(filepath.Join(config, "sessions", e.Name()))
		if rec, ok := core.DecodeSessionRecord(raw); ok && rec.ID == id {
			return rec.PID
		}
	}
	t.Fatalf("no record for %s", id)
	return 0
}

// sendFleet is a sender and robin, both registered, under a config directory
// whose name a glob would misread.
func sendFleet(t *testing.T) (c *testClient, config, sender, robin string) {
	config = filepath.Join(t.TempDir(), "cfg [work]")
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	fakeClaudeOnPath(t, "send")
	d := startDaemon(t)
	c = attach(t, d.socket)
	t.Cleanup(c.close)
	sender, robin = testSessionID("5e4d"), testSessionID("a0b1")
	c.spawn(sender, "sender")
	c.spawn(robin, "robin")
	c.awaitEvent(sender, "ready")
	c.awaitEvent(robin, "ready")
	return c, config, sender, robin
}

// sendTo has sender send to `to`, and is who the daemon says it reached.
func sendTo(c *testClient, sender, to string) core.PeerSend {
	c.t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: sender, Text: to})
	return *c.await("the send to "+to, func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == sender && f.Event != nil && f.Event.Tool != nil &&
			f.Event.Tool.Send != nil && f.Event.Tool.Send.To == to
	}).Event.Tool.Send
}

// A send by address names who it reached: a fleet agent by its name and id, any
// other session by its name. A bare name, an address nothing holds, a name two
// sessions share, a nameless record, a .key file, and another process holding a
// fleet agent's conversation all leave the send as it was, or name no agent. A
// malformed record leaves every name unresolved, since it may be the one named,
// and a socket as it was.
func TestASendByAddressCarriesWhoItReached(t *testing.T) {
	c, config, sender, robin := sendFleet(t)
	robinSocket := "uds:" + filepath.Join(config, "socks", "robin.sock")
	outside(t, config, "3.json", "0e5a1d3c-7d1e-4c3a-9a51-1f0c3b2a9d77", "outsider", "/s/outsider.sock")
	outside(t, config, "4.json", "11111111-2222-4333-8444-555555555555", "twin", "/s/twin-a.sock")
	outside(t, config, "5.json", "66666666-7777-4888-8999-000000000000", "twin", "/s/twin-b.sock")
	outside(t, config, "6.0a1b2c.key", robin, "keyed", "/s/keyed.sock")
	if err := writeRecord(config, "8.json", pidOf(t, config, robin), robin, "", "/s/nameless.sock"); err != nil {
		t.Fatal(err) // robin's own process, with no name
	}
	outside(t, config, "9.json", robin, "robin-resumed", "/s/elsewhere.sock")

	for to, want := range map[string]core.PeerSend{
		robinSocket:             {ToName: "robin", ToID: robin},
		"robin [0a1b2c]":        {ToName: "robin", ToID: robin},
		"uds:/s/outsider.sock":  {ToName: "outsider"},
		"uds:/s/elsewhere.sock": {ToName: "robin-resumed"},
		"twin [0a1b2c]":         {},
		"uds:/s/keyed.sock":     {},
		"uds:/s/nowhere.sock":   {},
		"uds:/s/nameless.sock":  {},
		"robin":                 {},
	} {
		if got := sendTo(c, sender, to); got.ToName != want.ToName || got.ToID != want.ToID {
			t.Errorf("a send to %q reached %q (%q), want %q (%q)", to, got.ToName, got.ToID, want.ToName, want.ToID)
		}
	}

	if err := os.WriteFile(filepath.Join(config, "sessions", "7.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := sendTo(c, sender, "robin [1a2b3c]"); got.ToName != "" || got.ToID != "" {
		t.Errorf("a name beside a malformed record reached %q (%q), want nobody", got.ToName, got.ToID)
	}
	if got := sendTo(c, sender, robinSocket); got.ToID != robin {
		t.Errorf("a socket beside a malformed record reached %q, want robin (%q)", got.ToID, robin)
	}
}

// After a /clear claude registers under the new conversation, and a send to it
// still names the agent by the id Wake spawned it under.
func TestASendByAddressFindsAnAgentAfterItsClear(t *testing.T) {
	c, config, sender, robin := sendFleet(t)
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: robin, Text: clearCommand})
	c.await("robin's conversation after its /clear", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == robin && f.Event != nil &&
			f.Event.SessionID != "" && f.Event.SessionID != robin
	})
	to := "uds:" + filepath.Join(config, "socks", "robin.sock")
	if got := sendTo(c, sender, to); got.ToID != robin || got.ToName != "robin" {
		t.Errorf("a send to robin's cleared conversation reached %q (%q), want robin (%q)", got.ToName, got.ToID, robin)
	}
}
