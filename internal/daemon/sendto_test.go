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
// a malformed record, a nameless one and a .key file all leave the send as it was.
func TestASendByAddressCarriesWhoItReached(t *testing.T) {
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

	register(t, config, "2.json", robin, "robin", "/s/robin.sock")
	register(t, config, "3.json", "0e5a1d3c-7d1e-4c3a-9a51-1f0c3b2a9d77", "outsider", "/s/outsider.sock")
	register(t, config, "4.json", "11111111-2222-4333-8444-555555555555", "twin", "/s/twin-a.sock")
	register(t, config, "5.json", "66666666-7777-4888-8999-000000000000", "twin", "/s/twin-b.sock")
	register(t, config, "6.0a1b2c.key", robin, "keyed", "/s/keyed.sock")
	register(t, config, "8.json", "99999999-aaaa-4bbb-8ccc-dddddddddddd", "", "/s/nameless.sock")
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
		"robin":                {},
	} {
		c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: sender, Text: to})
		f := c.await("the send to "+to, func(f rpc.Frame) bool {
			return f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Tool != nil &&
				f.Event.Tool.Send != nil && f.Event.Tool.Send.To == to
		})
		if got := *f.Event.Tool.Send; got.ToName != want.ToName || got.ToID != want.ToID {
			t.Errorf("a send to %q reached %q (%q), want %q (%q)", to, got.ToName, got.ToID, want.ToName, want.ToID)
		}
	}
}
