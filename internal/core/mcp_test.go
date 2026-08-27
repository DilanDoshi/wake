package core

import (
	"os"
	"strings"
	"testing"
)

// An init frame's MCP roster crosses the airlock, read off a real recording.
//
// The fixture rather than a hand-built frame, because the shape is Claude's and
// a frame this file wrote would only prove this file agrees with itself. Every
// one of the recorded corpus's server rows is one of three statuses; the one
// that matters is needs-auth, which is what Claude Code puts a warning row on.
func TestAnInitFramesMCPRosterCrossesTheAirlock(t *testing.T) {
	line := firstInitLine(t, "../../testdata/stream/bare-effort.jsonl", "mcp_servers")

	ev := onlyEvent(t, line, 0)
	if ev.Session == nil {
		t.Fatal("a recorded init frame produced no session facts at all")
	}
	if len(ev.Session.MCPServers) == 0 {
		t.Fatal("the init frame lists MCP servers and none of them crossed the airlock: the banner " +
			"has nothing to warn about, and the absence looks exactly like a session holding none")
	}

	var needsAuth, connected int
	for _, s := range ev.Session.MCPServers {
		if s.Name == "" {
			t.Errorf("a server crossed with no name: %+v", s)
		}
		switch s.State {
		case MCPNeedsAuth:
			needsAuth++
		case MCPConnected:
			connected++
		case MCPPending:
		default:
			t.Errorf("server %q has state %q, which is none of the three the corpus records - it "+
				"must still arrive intact rather than be flattened, so this is a note that the set grew",
				s.Name, s.State)
		}
	}
	if needsAuth == 0 || connected == 0 {
		t.Errorf("the recording has %d needing auth and %d connected; it was chosen because it has "+
			"both, so a zero here means the statuses are not surviving the decode", needsAuth, connected)
	}
}

// A session holding no MCP servers reports none, and that is not a warning.
//
// nil rather than an empty slice: "no servers" and "nobody has asked yet" must
// not be two values that render the same, and most sessions hold none.
func TestASessionWithNoMCPServersCarriesNoRoster(t *testing.T) {
	ev := onlyEvent0(t, `{"type":"system","subtype":"init","session_id":"s1","model":"claude-opus-4-6"}`)
	if ev.Session == nil {
		t.Fatal("an init frame with a model produced no facts")
	}
	if ev.Session.MCPServers != nil {
		t.Errorf("a session with no MCP servers carries %+v, want nil", ev.Session.MCPServers)
	}
}

// The roster rides on init and on nothing else.
//
// A result frame carries usage and no servers, so folding one must not blank a
// roster the init established - which is the same trap rpc.SessionStatus.Tool
// carries, one frame kind over.
func TestOnlyAnInitFrameCarriesTheMCPRoster(t *testing.T) {
	ev := onlyEvent0(t, `{"type":"system","subtype":"compact_boundary","session_id":"s1","model":"claude-opus-4-6"}`)
	if ev.Session != nil {
		t.Errorf("a non-init system frame produced session facts %+v: only init describes a session", ev.Session)
	}
}

// firstInitLine is the first system/init line of a recording carrying key.
//
// The key is a parameter rather than baked in because two facts ride the same
// frame - the MCP roster and the advertised command set - and a second copy of
// this scan would be the parallel implementation this project forbids.
func firstInitLine(t *testing.T, path, key string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `"subtype":"init"`) && strings.Contains(line, key) {
			return line
		}
	}
	t.Fatalf("%s has no init frame carrying %s, so this test would assert nothing", path, key)
	return ""
}

// onlyEvent0 is onlyEvent for a literal, which carries no fixture line number.
func onlyEvent0(t *testing.T, line string) Event {
	t.Helper()
	return onlyEvent(t, line, 0)
}
