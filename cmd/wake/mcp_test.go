// `wake mcp` against a daemon: what reaches the socket, and what a tool result
// says when the daemon refuses.

package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	mcpPeter = "1e5c1b8a-0000-4000-8000-000000000001"
	mcpJohn  = "1e5c1b8a-0000-4000-8000-000000000002"
)

// oneAgentFleet is a status report with one working agent in it.
func oneAgentFleet(id, name string) rpc.Status {
	return rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: id, Name: name, Label: "api-v2", Dir: "/repos/api", State: rpc.StateWorking},
	}}
}

// toolCall drives one tools/call through serveMCP the way a manager's tool
// runner does - a JSON-RPC line in on stdin, a line out on stdout - and returns
// the text and whether the server marked it an error.
//
// Through the real entrypoint rather than through mcp.Serve directly: what this
// package owns is the *Fleet*, and a fleet that writes the right frame into a
// socket nobody dialled is exactly the failure the whole file is about.
func toolCall(t *testing.T, socket, name string, args map[string]any) (string, bool) {
	t.Helper()

	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	line, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": params,
	})
	if err != nil {
		t.Fatalf("marshalling a request: %v", err)
	}

	var out strings.Builder
	if err := serveMCP(socket, strings.NewReader(string(line)+"\n"), &out); err != nil {
		t.Fatalf("serveMCP: %v", err)
	}

	var res struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out.String()), &res); err != nil {
		t.Fatalf("the server's own output is not readable: %v (%s)", err, out.String())
	}
	if res.Error != nil {
		t.Fatalf("%s came back as a JSON-RPC error, which a model can do nothing with: %s", name, res.Error.Message)
	}
	if len(res.Result.Content) != 1 {
		t.Fatalf("%s returned %d content blocks, want 1: %s", name, len(res.Result.Content), out.String())
	}
	return res.Result.Content[0].Text, res.Result.IsError
}

func TestWakeMCPListsTheFleetOverASocket(t *testing.T) {
	d := startFakeDaemon(t, 0, oneAgentFleet(mcpPeter, "peter"))

	text, isErr := toolCall(t, d.socket, "list_agents", nil)
	if isErr {
		t.Fatalf("list_agents failed against a daemon that answered: %s", text)
	}
	if !strings.Contains(text, mcpPeter) {
		t.Errorf("list_agents over a real socket did not name the running agent:\n%s", text)
	}
}

// A message becomes one frame on the socket, addressed by id and carrying the
// text unchanged.
func TestSendToAgentPutsOneFrameOnTheSocketAddressedById(t *testing.T) {
	const text = "pause and write up where you got to"
	d := startFakeDaemon(t, 0, oneAgentFleet(mcpPeter, "peter"))

	if out, isErr := toolCall(t, d.socket, "send_to_agent", map[string]any{
		"agent_id": mcpPeter, "message": text,
	}); isErr {
		t.Fatalf("send_to_agent failed: %s", out)
	}

	sent := d.lastOfKind(rpc.FrameSend)
	if sent.SessionID != mcpPeter || sent.Text != text {
		t.Errorf("the daemon was sent %+v, want the id and the text: a message the manager wrote is the manager's words", sent)
	}
}

func TestInterruptPutsOneFrameOnTheSocketAddressedById(t *testing.T) {
	d := startFakeDaemon(t, 0, oneAgentFleet(mcpPeter, "peter"))

	if out, isErr := toolCall(t, d.socket, "interrupt", map[string]any{"agent_id": mcpPeter}); isErr {
		t.Fatalf("interrupt failed: %s", out)
	}
	if got := d.lastOfKind(rpc.FrameInterrupt); got.SessionID != mcpPeter {
		t.Errorf("the daemon was sent %+v, want an interrupt addressed to the agent", got)
	}
}

// The verb is followed by a question on the same connection, and the answer to
// that question is what "Sent." rests on.
//
// # Why an acting call waits at all
//
// Nothing on this socket acknowledges a send. The daemon dispatches a
// connection's frames **synchronously and in order**, so a status request
// written behind the verb cannot be answered until the verb has been dispatched
// - and a refusal, which is dispatched at the same moment, is enqueued to this
// client before the reply is. So the reply is a barrier: reaching it means the
// daemon has taken the verb and has not refused it.
//
// Without the barrier the alternative is writing the frame and hanging up, which
// puts the daemon's refusal - "unknown session", "that session has ended", "not
// reading its input" - on a connection nobody is reading, and tells the model
// "Sent." A manager that believes it delegated something is worse than one that
// knows it failed.
func TestAnActingCallAsksTheDaemonAQuestionBehindTheVerbAndWaitsForTheAnswer(t *testing.T) {
	d := startFakeDaemon(t, 0, oneAgentFleet(mcpPeter, "peter"))

	if out, isErr := toolCall(t, d.socket, "send_to_agent", map[string]any{
		"agent_id": mcpPeter, "message": "hello",
	}); isErr {
		t.Fatalf("send_to_agent failed: %s", out)
	}

	// The first status is requireLive's, on its own connection. What has to be
	// true is that the send is not the *last* thing this client said: a status
	// follows it, and the tool did not return until that status was answered.
	got := d.frames()
	sent := -1
	for i, f := range got {
		if f.Kind == rpc.FrameSend {
			sent = i
		}
	}
	if sent < 0 {
		t.Fatalf("no send frame reached the daemon at all: %+v", got)
	}
	asked := false
	for _, f := range got[sent+1:] {
		if f.Kind == rpc.FrameStatus {
			asked = true
		}
	}
	if !asked {
		t.Errorf("the send was the last thing this client said (%+v). Nothing acknowledges a send, so a client that writes one and hangs up has thrown away the only report of a refusal there is - and the model is told it delegated the work", kinds(got))
	}
}

// And the refusal the barrier exists to catch reaches the model.
func TestASendTheDaemonRefusesIsNotReportedAsSent(t *testing.T) {
	const why = "session " + mcpPeter + " is not reading its input: 64 messages already queued"
	d := listenAs(t, &fakeDaemon{status: oneAgentFleet(mcpPeter, "peter"), sendRefusal: why})

	text, isErr := toolCall(t, d.socket, "send_to_agent", map[string]any{
		"agent_id": mcpPeter, "message": "hello",
	})
	if !isErr {
		t.Fatalf("a refused send was reported as success: %q", text)
	}
	if !strings.Contains(text, why) {
		t.Errorf("the refusal did not reach the model: %q", text)
	}
}

// A refusal addressed to somebody else's session is not this call's answer.
//
// Every client on this daemon shares the fan-out, so another window's error
// frame arrives here too. Reading one as this send's refusal would report a
// failure that did not happen - and the manager would send the same message
// again.
func TestAnotherClientsFailureIsNotReadAsThisCallsRefusal(t *testing.T) {
	d := listenAs(t, &fakeDaemon{
		status:         oneAgentFleet(mcpPeter, "peter"),
		sendRefusal:    "unknown session " + mcpJohn,
		refusalAddress: mcpJohn,
	})

	if text, isErr := toolCall(t, d.socket, "send_to_agent", map[string]any{
		"agent_id": mcpPeter, "message": "hello",
	}); isErr {
		t.Errorf("an error frame naming another session was read as this send's refusal: %q", text)
	}
}

// Nothing here starts a daemon.
//
// connect() calls daemon.EnsureRunning, which *forks* one when nothing is
// listening - and that is right for a person typing `wake`, whose first run
// should get an agent. It is wrong here twice over: `wake mcp` is spawned by a
// manager session rather than by a person, so a fork would put a daemon on the
// machine nobody asked for; and the daemon it forked would hold no sessions, so
// every tool would answer confidently about an empty fleet that is not the one
// the manager is managing.
func TestTheMCPServerNeverStartsADaemon(t *testing.T) {
	socket := tempSocket(t)

	text, isErr := toolCall(t, socket, "list_agents", nil)
	if isErr {
		t.Fatalf("list_agents against no daemon failed rather than saying the fleet is empty: %s", text)
	}
	// Both halves of the fleet, because they dial through different code. The
	// reading half goes through daemon.Status, which answers from the disk when
	// nothing is listening; the acting half dials for itself, and it is the one
	// where reaching for EnsureRunning would look like an improvement - it is
	// what every other command in this package calls.
	if err := (socketFleet{socket: socket}).Send(t.Context(), mcpPeter, "hello"); err == nil {
		t.Error("a send against no daemon reported success")
	}
	if conn, err := daemon.Dial(socket); err == nil {
		_ = conn.Close()
		t.Error("something is listening on the socket after `wake mcp` ran against nothing: this verb forked a daemon, so every tool after it answers about an empty fleet that is not the one being managed")
	}
	if !strings.Contains(text, "No agents") {
		t.Errorf("list_agents against no daemon said %q, which is not an answer a model can act on", text)
	}
}

// An acting call against no daemon is refused rather than reported as done.
func TestActingWithNoDaemonListeningIsRefused(t *testing.T) {
	socket := tempSocket(t)
	text, isErr := toolCall(t, socket, "send_to_agent", map[string]any{
		"agent_id": mcpPeter, "message": "hello",
	})
	if !isErr {
		t.Fatalf("a send against no daemon reported success: %q", text)
	}
	if !strings.Contains(text, "list_agents") {
		t.Errorf("the refusal does not say what to do instead: %q", text)
	}
}

// A daemon that is bound and never accepting is the shape of one in graceful
// shutdown, and the acting path gives up on it rather than waiting forever.
//
// The dial *succeeds* - the kernel completes into the listen backlog - so
// nothing about the connection says anything is wrong. `wake mcp` is answering
// a model that is blocked on a tool result, so an unbounded wait is a manager
// stopped dead with no reason on any surface.
//
// Driven straight at the fleet rather than through the tool, because the tool
// asks for the roster first and that question has a bound of its own: this is
// about the *write's* bound, which is the one nothing else in this tree sets.
func TestAnActingCallGivesUpOnADaemonThatWillNotAnswer(t *testing.T) {
	socket := tempSocket(t)
	listenSilently(t, socket)
	shortActDeadline(t, 200*time.Millisecond)

	done := make(chan error, 1)
	go func() { done <- socketFleet{socket: socket}.Send(t.Context(), mcpPeter, "hello") }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a send to a daemon that never answered reported success")
		}
	// Ten times the bound rather than testTimeout, because what is waited for
	// here is a *non-event* - so this wait is what a mutation removing the
	// deadline costs every time somebody runs the battery, and fifteen seconds
	// of that is how a battery quietly shrinks to its fast half.
	case <-time.After(10 * actTimeout):
		t.Fatal("a send to a daemon that never answered has not returned inside ten times its own bound: a manager is blocked on this tool result with no reason on any surface")
	}
}

// A daemon that hangs up between the verb and the answer is not a success
// either. This is the reachable half of the wait above: an outgoing daemon
// closes its clients, and a client that read the close as "well, nothing said
// no" would report the work as delegated.
func TestADaemonThatHangsUpBeforeAnsweringIsNotReportedAsSent(t *testing.T) {
	d := listenAs(t, &fakeDaemon{
		status: oneAgentFleet(mcpPeter, "peter"),
		// One status answered - the roster the tool checks - and then this
		// daemon stops answering, which is what the acting call runs into.
		statusesBeforeHangUp: 1,
	})

	text, isErr := toolCall(t, d.socket, "send_to_agent", map[string]any{
		"agent_id": mcpPeter, "message": "hello",
	})
	if !isErr {
		t.Fatalf("a send the daemon never confirmed was reported as success: %q", text)
	}
}

// A spawn the daemon refuses is not reported to the manager as started.
//
// This is the acknowledgement act relies on, and it is the barrier the send and
// interrupt tests above assert with a fake - here against the *real* daemon,
// because what could break it is the real dispatch ordering. act writes a
// FrameStatus behind the spawn and reads the first status reply as "taken and
// not refused"; that holds only while the spawn is dispatched **synchronously**,
// so the refusal is enqueued to this client ahead of that reply. BUG-23 moved a
// *worktree* spawn off the dispatch goroutine because its `git worktree add` can
// block - and a spawn with no worktree, which is the only kind this tool sends,
// has to stay in line or a refused one comes back as `Started`.
//
// A non-absolute directory is refused by maySpawn before a name or a process, so
// nothing is started and the refusal is the daemon's own.
func TestASpawnTheDaemonRefusesIsNotReportedAsStarted(t *testing.T) {
	d := startRealDaemon(t)

	id, err := (socketFleet{socket: d.socket}).Spawn(t.Context(), "not/absolute")
	if err == nil {
		t.Fatalf("a spawn the daemon refuses was reported as started (id %q): act read the status "+
			"reply as 'taken' because the refusal was enqueued behind it, so the manager believes in "+
			"an agent that does not exist", id)
	}
	if !strings.Contains(err.Error(), "absolute") {
		t.Errorf("the refusal that reached the manager was %q, want the daemon's own reason", err)
	}
}

// kinds is the frame kinds a daemon received, for a failure message that says
// what the client actually did.
func kinds(frames []rpc.Frame) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, f.Kind)
	}
	return out
}

// A spawn reaches the socket as a FrameSpawn carrying an id this side minted
// and the directory the tool was given.
//
// The id is the half worth asserting. Wake originates identity - the daemon
// refuses a spawn frame that arrives without one - so a tool that let the
// daemon choose would be refused, and one that reused an existing id would be
// refused as already held. It is also what the tool answers with, so the
// manager can address the agent before the daemon has finished starting it.
func TestSpawnAgentPutsOneFrameOnTheSocketCarryingAFreshID(t *testing.T) {
	d := startFakeDaemon(t, 0, oneAgentFleet(mcpPeter, "peter"))

	out, isErr := toolCall(t, d.socket, "spawn_agent", map[string]any{"directory": "/repos/api"})
	if isErr {
		t.Fatalf("spawn_agent failed: %s", out)
	}

	got := d.lastOfKind(rpc.FrameSpawn)
	if got.Dir != "/repos/api" {
		t.Errorf("the daemon was sent %+v, want the directory the tool was given: an agent started somewhere else edits the wrong tree", got)
	}
	if _, err := uuid.Parse(got.SessionID); err != nil {
		t.Errorf("the spawn frame carries %q, which is not a UUID (%v). maySpawn refuses anything else", got.SessionID, err)
	}
	if got.SessionID == mcpPeter {
		t.Error("the spawn reused the id of an agent that already exists, which the daemon refuses as already held")
	}
	if !strings.Contains(out, got.SessionID) {
		t.Errorf("spawn_agent answered %q without the id it put on the wire: the manager addresses by id and has no other way to reach what it just started", out)
	}
}
