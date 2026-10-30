package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// register writes a session's record into the registry under config, as Claude
// Code writes one (testdata/sessions/78871.json), under file.
func register(t *testing.T, config, file, id, name, socket string) {
	t.Helper()
	dir := filepath.Join(config, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"pid": 1, "sessionId": id, "name": name, "messagingSocketPath": socket})
	if err := os.WriteFile(filepath.Join(dir, file), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// A send by address reaches its clients naming who it reached, read off Claude
// Code's registry: a fleet agent by its name and id, a session outside the fleet
// by its name. A bare name, an address nothing holds, a name two sessions share,
// a nameless record and a .key file all leave the send as it was, and a
// malformed record costs nothing else. The config directory's name is one a
// glob would misread.
func TestASendByAddressCarriesWhoItReached(t *testing.T) {
	config := filepath.Join(t.TempDir(), "cfg [work]")
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	fakeClaudeOnPath(t, "send")
	d := startDaemon(t)
	c := attach(t, d.socket)
	defer c.close()
	sender, robin := testSessionID("5e4d"), testSessionID("a0b1")
	c.spawn(sender, "sender")
	c.spawn(robin, "robin")
	c.awaitEvent(sender, "ready")

	register(t, config, "2.json", robin, "robin", "/s/robin.sock")
	register(t, config, "3.json", "0e5a1d3c-7d1e-4c3a-9a51-1f0c3b2a9d77", "outsider", "/s/outsider.sock")
	register(t, config, "4.json", "11111111-2222-4333-8444-555555555555", "twin", "/s/twin-a.sock")
	register(t, config, "5.json", "66666666-7777-4888-8999-000000000000", "twin", "/s/twin-b.sock")
	register(t, config, "6.0a1b2c.key", robin, "keyed", "/s/keyed.sock")
	register(t, config, "8.json", robin, "", "/s/nameless.sock")
	if err := os.WriteFile(filepath.Join(config, "sessions", "7.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	for to, want := range map[string]core.PeerSend{
		"uds:/s/robin.sock":    {ToName: "robin", ToID: robin},
		"robin [0a1b2c]":       {ToName: "robin", ToID: robin},
		"uds:/s/outsider.sock": {ToName: "outsider"},
		"twin [0a1b2c]":        {},
		"uds:/s/keyed.sock":    {},
		"uds:/s/nowhere.sock":  {},
		"uds:/s/nameless.sock": {},
		"robin":                {},
	} {
		c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: sender, Text: to})
		f := c.await("the send to "+to, func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameEvent && f.SessionID == sender && f.Event != nil && f.Event.Tool != nil &&
				f.Event.Tool.Send != nil && f.Event.Tool.Send.To == to
		})
		if got := *f.Event.Tool.Send; got.ToName != want.ToName || got.ToID != want.ToID {
			t.Errorf("a send to %q reached %q (%q), want %q (%q)", to, got.ToName, got.ToID, want.ToName, want.ToID)
		}
	}
}

// After a /clear claude writes its registry record under the new conversation's
// id, and a send to it still names the agent by the id Wake spawned it under.
func TestASendByAddressFindsAnAgentAfterItsClear(t *testing.T) {
	config := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	fakeClaudeOnPath(t, "send")
	d := startDaemon(t)
	c := attach(t, d.socket)
	defer c.close()
	sender, robin := testSessionID("5e4d"), testSessionID("a0b1")
	c.spawn(sender, "sender")
	c.spawn(robin, "robin")
	c.awaitEvent(sender, "ready")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: robin, Text: clearCommand})
	successor := c.await("robin's conversation after its /clear", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == robin && f.Event != nil &&
			f.Event.SessionID != "" && f.Event.SessionID != robin
	}).Event.SessionID
	register(t, config, "2.json", successor, "robin", "/s/robin.sock")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: sender, Text: "uds:/s/robin.sock"})
	f := c.await("the send to robin", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == sender && f.Event != nil && f.Event.Tool != nil &&
			f.Event.Tool.Send != nil && f.Event.Tool.Send.To == "uds:/s/robin.sock"
	})
	if got := f.Event.Tool.Send; got.ToID != robin || got.ToName != "robin" {
		t.Errorf("a send to robin's cleared conversation reached %q (%q), want robin (%q)", got.ToName, got.ToID, robin)
	}
}
