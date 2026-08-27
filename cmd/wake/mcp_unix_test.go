//go:build unix

// `wake mcp` against a real daemon and a real agent.
//
// Everything above this is the tool talking to a fake daemon, which proves what
// this package puts on the wire and nothing about what happens to it. This is
// the only place a manager's tool call reaches a process: a real socket, a real
// forked `claude`, and an assertion on the one thing only the far side can
// produce - the agent's own answer coming back.

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The loop the manager exists for, end to end: read the fleet, pick an id off
// it, send to that id, and the agent answers.
//
// The id is taken from list_agents' own output rather than from the spawn, which
// is the property the whole surface rests on: whatever that first column holds
// has to be something send_to_agent accepts and the daemon can route. A tool
// that printed a short id, or a name, passes every unit test in internal/mcp and
// fails here.
func TestAManagerReadsTheFleetAndSendsToAnAgentThatAnswers(t *testing.T) {
	withScriptedAgent(t, "")
	socket := serveInProcess(t)

	observer := observe(t, socket)
	id := spawnAgent(t, socket, "peter")

	listed, isErr := toolCall(t, socket, "list_agents", nil)
	if isErr {
		t.Fatalf("list_agents against a real daemon failed: %s", listed)
	}
	if !strings.Contains(listed, id) {
		t.Fatalf("list_agents over a real socket did not name the running agent:\n%s", listed)
	}

	const typed = "zebrafish"
	fromTheList := firstColumnID(t, listed)
	sent, isErr := toolCall(t, socket, "send_to_agent", map[string]any{
		"agent_id": fromTheList, "message": typed,
	})
	if isErr {
		t.Fatalf("send_to_agent was refused the id list_agents printed: %s", sent)
	}

	// The assertion only the far side can satisfy. App.submit's local echo does
	// not exist here - nothing is drawing anything - so this text exists only if
	// a real process read it off its own stdin and answered.
	observer.await(t, heardPrefix+typed)
}

// And an interrupt reaches the process too: a turn that is provably in flight is
// aborted, and the session is still there afterwards.
//
// The two halves are one test because either alone passes against a defect. An
// abort with no session left behind it is `stop` wearing the wrong name, which
// is the confusion rpc's four-verb rule exists to prevent - and it is exactly
// what a manager must not be able to do by accident.
func TestAManagersInterruptStopsATurnAndLeavesTheSessionThere(t *testing.T) {
	withScriptedAgent(t, scriptInterruptible)
	socket := serveInProcess(t)

	observer := observe(t, socket)
	id := spawnAgent(t, socket, "peter")

	if out, isErr := toolCall(t, socket, "send_to_agent", map[string]any{
		"agent_id": id, "message": "start something long",
	}); isErr {
		t.Fatalf("send_to_agent failed: %s", out)
	}
	// The turn is in flight before the key. An interrupt with no turn under it
	// is a harmless no-op that still produces a receipt, so a test that raced
	// the agent would pass having stopped nothing.
	observer.await(t, workingMarker)

	if out, isErr := toolCall(t, socket, "interrupt", map[string]any{"agent_id": id}); isErr {
		t.Fatalf("interrupt failed: %s", out)
	}
	observer.await(t, "[Request interrupted by user]")

	// Still there, and still offered as something to act on.
	listed, isErr := toolCall(t, socket, "list_agents", nil)
	if isErr {
		t.Fatalf("list_agents after an interrupt failed: %s", listed)
	}
	if !strings.Contains(listed, id) {
		t.Errorf("the session is gone after an interrupt. Stopping a turn and stopping a session are different verbs, and only one of them is on this surface:\n%s", listed)
	}
}

// observer is a plain client connection, kept open, that collects everything the
// daemon fans out.
//
// A raw connection rather than a TUI: what is being asserted is that a process
// on the far side of the daemon produced some text, and a model between the
// socket and the assertion is one more thing that can be the reason it did not.
type observer struct{ seen chan string }

func observe(t *testing.T, socket string) *observer {
	t.Helper()

	conn, err := daemon.Dial(socket)
	if err != nil {
		t.Fatalf("dial %s: %v", socket, err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	o := &observer{seen: make(chan string, 1024)}
	frames, errs := rpc.ReadFrames(conn)
	go func() {
		for f := range frames {
			if f.Event != nil {
				select {
				case o.seen <- f.Event.Text:
				default:
				}
			}
		}
		<-errs
	}()
	return o
}

// await blocks until some event carried the text, or the test's own bound
// expires.
func (o *observer) await(t *testing.T, want string) {
	t.Helper()

	deadline := time.After(testTimeout)
	var got []string
	for {
		select {
		case text := <-o.seen:
			if strings.Contains(text, want) {
				return
			}
			got = append(got, text)
		case <-deadline:
			t.Fatalf("waited %v for an event carrying %q; what arrived was %q", testTimeout, want, got)
		}
	}
}

// spawnAgent starts one session through the daemon and returns its id, waiting
// for the confirmation so nothing after it races the spawn.
func spawnAgent(t *testing.T, socket, name string) string {
	t.Helper()

	conn, err := daemon.Dial(socket)
	if err != nil {
		t.Fatalf("dial %s: %v", socket, err)
	}
	defer func() { _ = conn.Close() }()

	id := uuid.NewString()
	frames, errs := rpc.ReadFrames(conn)
	defer func() {
		_ = conn.Close()
		for range frames {
		}
		<-errs
	}()

	if err := rpc.WriteFrameTo(conn, rpc.Frame{
		Kind: rpc.FrameSpawn, SessionID: id, Text: name, Dir: t.TempDir(),
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}
	for f := range frames {
		if f.Kind == rpc.FrameError && f.SessionID == id {
			t.Fatalf("the daemon refused the spawn: %s", f.Text)
		}
		if _, ok := spawnedSession(f, id); ok {
			return id
		}
	}
	t.Fatal("the daemon never confirmed the spawn")
	return ""
}

// firstColumnID is the id off the first agent row of a list_agents result.
//
// Found by shape rather than by taking line one, because the result opens with
// the framing note that says whose words the rows are - and this test is about
// the round trip through the *id column*, not about where that note sits. The
// shape is `internal/mcp`'s own rule: an agent is addressed by a whole session
// UUID, so the first token of a row is 36 characters and nothing else is.
func firstColumnID(t *testing.T, listed string) string {
	t.Helper()

	const uuidLen = 36
	for _, line := range strings.Split(listed, "\n") {
		if id, _, ok := strings.Cut(line, " "); ok && len(id) == uuidLen {
			return id
		}
	}
	t.Fatalf("no line of this list_agents result opens with a session id:\n%s", listed)
	return ""
}
