package daemon

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// An agent carries onto its report the slash commands its init advertised, so a
// client that attached after that init - which is every reattach - still learns
// them from the report and its completion menu is not empty.
//
// The commands ride the one-per-turn init *event*, and no event is replayed to a
// late client, so the report is the only route it has to them. Same shape as
// Effort - see rpc.SessionStatus.Commands.
func TestAnAgentReportsTheCommandsItsInitAdvertised(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{
		SlashCommands: []string{"compact", "commit-push"},
	}})

	if got := a.snapshot().Commands; len(got) != 2 {
		t.Errorf("snapshot().Commands = %v after an init advertising two, want both: the report is the only "+
			"route a client that attached after the init has to them", got)
	}
}

// A frame that names no commands - every result and tool frame - leaves the
// advertised set alone rather than blanking it, the same guard withFacts keeps.
func TestAReportKeepsTheCommandsWhenAFrameNamesNone(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{SlashCommands: []string{"compact"}}})
	a.observe(core.Event{Kind: core.KindTurnEnd, Session: &core.SessionFacts{}}) // a result frame names no commands

	if got := a.snapshot().Commands; len(got) != 1 {
		t.Errorf("snapshot().Commands = %v after a later frame named none, want the init's one kept", got)
	}
}

// The handshake's reply is a fresh agent's only word on its commands: init
// arrives with a turn, so until the first one a completion menu has nothing to
// offer. These two are what that reply names; the init a turn brings later
// names others, so a test can tell which one the report carries.
var (
	handshakeCommands = []string{"compact", "deploy-prod"}
	initCommands      = []string{"deploy-prod", "notebook"}
)

// fakeHandshake is a session whose init is withheld until a turn, as a real
// one's is, and whose handshake reply names handshakeCommands - or refuses. It
// speaks first, as a real one's SessionStart hooks do, so the reply is not the
// event that first moves the agent's reported state: the report that carries
// the commands has to be pushed for the reply's own sake.
func fakeHandshake(sid string, refuse bool) int {
	emitText(sid, "hooked")
	for line := range stdinLines() {
		id := controlRequestID(line)
		switch {
		case strings.Contains(line, `"subtype":"initialize"`) && refuse:
			fmt.Printf(`{"type":"control_response","response":{"subtype":"error","request_id":%q,"error":"not now"}}`+"\n", id)
			emitText(sid, "refused")
		case strings.Contains(line, `"subtype":"initialize"`):
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"commands":[{"name":%q},{"name":%q}]}}}`+"\n",
				id, handshakeCommands[0], handshakeCommands[1])
		case strings.Contains(line, `"subtype":"mcp_status"`):
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"mcpServers":[]}}}`+"\n", id)
		default:
			fmt.Printf(`{"type":"system","subtype":"init","session_id":%q,"model":"claude-opus-5","cwd":"/tmp/repo","slash_commands":[%q,%q]}`+"\n",
				sid, initCommands[0], initCommands[1])
			emitText(sid, "echo: "+line)
			emitResult(sid)
		}
	}
	return 0
}

// pushedCommands waits for a status push naming the session's commands: the
// report is the only way a client learns them without an init.
func (c *testClient) pushedCommands(sessionID string, want []string) {
	c.t.Helper()
	c.await(fmt.Sprintf("a status push giving %s the commands %v", sessionID, want), func(f rpc.Frame) bool {
		if f.Kind != rpc.FrameStatusPush || f.Status == nil {
			return false
		}
		return slices.Equal(sessionRow(*f.Status, sessionID).Commands, want)
	})
}

// A fresh agent has taken no turn and so sent no init, yet its report carries
// the commands its handshake reply named - pushed to clients, since nothing else
// about a quiet agent changes and no event is replayed - and the reply itself
// reaches no window.
func TestAFreshAgentReportsTheCommandsItsHandshakeNamed(t *testing.T) {
	fakeClaudeOnPath(t, "handshake")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.pushedCommands(idAlpha, handshakeCommands)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "after"})
	c.awaitEvent(idAlpha, "echo: ")
	for _, f := range c.seen {
		if ev := f.Event; ev != nil && (ev.Kind == core.KindControlReceipt || ev.Kind == core.KindMCPReply) {
			t.Errorf("a window received the daemon's own reply: %+v", ev)
		}
	}
}

// The init a turn brings is the newer word: it replaces what the handshake
// named, as it replaces its own list every turn.
func TestALaterInitReplacesTheHandshakesCommands(t *testing.T) {
	fakeClaudeOnPath(t, "handshake")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.pushedCommands(idAlpha, handshakeCommands)

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "go"})
	c.awaitEvent(idAlpha, "echo: ") // the init folded ahead of it
	if got := sessionRow(c.status(), idAlpha).Commands; !slices.Equal(got, initCommands) {
		t.Errorf("Commands = %v after a turn's init, want its %v", got, initCommands)
	}
}

// A refused handshake names nothing: the report stays empty until an init.
func TestARefusedHandshakeLeavesTheReportWithoutCommands(t *testing.T) {
	fakeClaudeOnPath(t, "handshakerefused")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")
	c.awaitEvent(idAlpha, "refused") // said after the refusal, so it was handled first

	if got := sessionRow(c.status(), idAlpha).Commands; len(got) != 0 {
		t.Errorf("Commands = %v after a refused handshake, want none", got)
	}
}

// An init's list is never replaced by the handshake's: init is the word a turn
// renews, and a handshake reply that arrived behind one is the older.
func TestTheHandshakeNeverReplacesAnInitsCommands(t *testing.T) {
	a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
		core.NewSession(core.Config{SessionID: idAlpha}), func() {})
	a.observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{SlashCommands: initCommands}})

	a.initID = "init-1"
	reply := core.Event{Kind: core.KindControlReceipt, RequestID: "init-1",
		Session: &core.SessionFacts{SlashCommands: handshakeCommands}}
	if !a.handshakeAnswered(reply) {
		t.Fatal("the handshake's reply was not recognised")
	}
	if got := a.snapshot().Commands; !slices.Equal(got, initCommands) {
		t.Errorf("Commands = %v after a handshake behind an init, want the init's %v", got, initCommands)
	}
}

// A reply naming no commands, or one that is not the handshake's, folds nothing.
func TestOnlyTheHandshakesOwnReplyTeachesTheCommands(t *testing.T) {
	for name, tc := range map[string]struct {
		id    string
		facts *core.SessionFacts
	}{
		"no facts":       {id: "init-1"},
		"someone else's": {id: "other", facts: &core.SessionFacts{SlashCommands: handshakeCommands}},
	} {
		a := newAgent(idAlpha, "sydney", "dev-5748", spawnedIn, "",
			core.NewSession(core.Config{SessionID: idAlpha}), func() {})
		a.initID = "init-1"
		a.handshakeAnswered(core.Event{Kind: core.KindControlReceipt, RequestID: tc.id, Session: tc.facts})
		if got := a.snapshot().Commands; len(got) != 0 {
			t.Errorf("%s: Commands = %v, want none", name, got)
		}
	}
}
