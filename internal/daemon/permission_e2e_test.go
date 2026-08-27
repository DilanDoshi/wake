// Answering a permission ask, judged by what the agent did about it.
//
// # Why this file exists next to lifecycle_test.go's permission test
//
// That test asserts an answer *arrives*: the fake greps the line for a
// behavior and says which one it saw, and the daemon's routing is proved by the
// echo coming back attributed to the right session. Everything about it is
// correct and none of it is evidence that a tool ran.
//
// The gap is not theoretical. A recording spike found core.EncodeAllow emitting
// well-formed JSON - correct envelope, right request_id - that a real `claude`
// read and answered with "The user did not answer the questions", ending the
// turn subtype "success". Every unit test in the tree passed, because every one
// of them asserted on the shape of what Wake wrote. A test asserting the shape
// of what Wake writes proves nothing about whether it arrived.
//
// So these three tests assert on the far side's own state, reached through the
// whole stack - a real daemon, a real socket, a real client, a real process -
// and never on a frame Wake produced:
//
//   - an allow leaves a file on disk that only the tool running creates
//   - a deny leaves the reason the operator gave, decoded out of the nesting
//     the CLI reads rather than grepped out of the line
//   - an allow addressed to one of two blocked agents runs that one's tool and
//     leaves the other stopped dead, still waiting
//
// fakeTool in main_test.go is the other half, and its header carries why it
// decodes structurally instead of matching a substring.

package daemon

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// blockedOnTool spawns an agent that asks permission for a Write, and returns
// once the daemon reports it stopped dead waiting for an answer.
//
// It hands back the directory the tool works in, so a caller can assert on what
// is and is not there. Every caller needs the same three barriers - the ask
// reached this client, it was attributed to the right session, and the daemon
// is reporting blocked - and a test that skipped any of them could answer a
// question nobody had asked yet.
func blockedOnTool(t *testing.T, c *testClient, sessionID, name string) string {
	t.Helper()

	dir := os.Getenv(fakeToolDirEnv)
	c.spawn(sessionID, name)

	ask := c.await("the permission request for "+sessionID, func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == sessionID &&
			f.Event != nil && f.Event.Kind == core.KindPermissionRequest
	})
	// A can_use_tool request carries no session_id on Claude's wire, so the
	// stamp core.Session.attribute puts on it from the pipe it arrived on is
	// the only thing that makes an answer routable. Checked here because every
	// test below depends on it and none of them would say so on failing.
	if ask.Event.SessionID != sessionID {
		t.Fatalf("the ask for %s arrived attributed to %q: an answer cannot be routed back to an agent the ask does not name",
			sessionID, ask.Event.SessionID)
	}
	if ask.Event.RequestID != askRequestID {
		t.Fatalf("ask RequestID = %q, want %q - the only correlator an answer has", ask.Event.RequestID, askRequestID)
	}
	c.pollState(sessionID, rpc.StateBlocked)

	// The precondition the whole file rests on. Without it "the file is there
	// afterwards" also passes against a fake that wrote it at startup, which is
	// the shape of a check whose negative answer has never been seen.
	mustNotExist(t, toolRanPath(dir, sessionID),
		"the tool had already run before anybody answered, so nothing below observes an allow")
	return dir
}

// withToolAgent puts the tool-running fake on PATH and gives it somewhere to
// work. The directory is per call - a fixed path under os.TempDir() is what let
// one mutation run poison every clean run after it.
func withToolAgent(t *testing.T) {
	t.Helper()
	fakeClaudeOnPath(t, "tool")
	t.Setenv(fakeToolDirEnv, t.TempDir())
}

// The loop: an agent's tool call blocks, the ask reaches the operator, the
// operator allows, and the tool runs.
//
// The last clause is the one nothing else in this tree asserts. FrameAllow is
// unit-tested, the routing is tested, and the encoding is tested - and the
// defect that produced this file passed all three while the tool never ran.
//
// Mutation check: deleting the a.sess.AllowTool call from agent.apply's
// rpc.FrameAllow case leaves this failing at "the operator allowed the tool and
// it never ran". Nesting EncodeAllow's verdict one level shallower - the exact
// shape of the live defect, still valid JSON carrying the right request_id -
// fails at the same line, where a substring check would not have noticed.
func TestAnAllowActuallyRunsTheTool(t *testing.T) {
	withToolAgent(t)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := blockedOnTool(t, c, idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameAllow, SessionID: idAlpha, RequestID: askRequestID})

	awaitFile(t, toolRanPath(dir, idAlpha),
		"the operator allowed the tool and it never ran")
	// And the agent is no longer stopped: an allow that ran the tool but left
	// the daemon believing the ask outstanding would show a live agent as
	// blocked forever.
	c.pollState(idAlpha, rpc.StateIdle)
}

// The other verdict, and the one with something to say.
//
// core.EncodeDeny's reason is echoed to the model verbatim as the tool result,
// which makes it the one channel Wake has for telling an agent why - not just
// that - it was refused. defaultDenyReason was written with great care about
// what the model reads, and until now nothing checked the model reads anything
// at all.
//
// Mutation check: dropping the Message field from encode.go's outPermDecision
// leaves this failing at "the model was told nothing about why" for both rows.
// Deleting defaultDenyReason's fallback - the case an operator reaches by
// hitting enter on an empty box - leaves the blank row failing at the same
// line while the row with a reason still passes, which is the whole reason the
// table has two rows.
func TestADenyTellsTheModelWhyItWasRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reason string
	}{
		// What an operator types.
		{name: "a reason the operator gave", reason: "that file is generated; edit the template instead"},
		// And what an operator in a hurry gives, which is the case the
		// substitute exists for.
		{name: "no reason at all", reason: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withToolAgent(t)
			d := startDaemon(t)
			c := attach(t, d.socket)

			dir := blockedOnTool(t, c, idAlpha, "sydney")

			c.send(rpc.Frame{Kind: rpc.FrameDeny, SessionID: idAlpha, RequestID: askRequestID, Reason: tc.reason})

			got := readFileWithin(t, toolDeniedPath(dir, idAlpha),
				"the model was told nothing about why the tool was refused")
			if got == "" {
				t.Fatalf("the tool result the model receives is empty: an unexplained refusal teaches the agent it was blocked and nothing about what to do instead, and the likeliest next move is the identical call again")
			}
			// Derived from the encoder rather than copied out of it. A literal
			// here would be this project's most-repeated failure - a test
			// restating a fact the code already declares - and it would pass
			// against an encoder that had stopped sending the field at all,
			// because the literal would still match itself.
			if want := denyMessageOnTheWire(t, tc.reason); got != want {
				t.Errorf("the model received %q as the tool result, and core.EncodeDeny wrote %q: the reason did not survive the trip", got, want)
			}
			// A refusal that ran the tool anyway is the failure that costs
			// somebody their repository, and it is not covered by any assertion
			// about text.
			mustNotExist(t, toolRanPath(dir, idAlpha),
				"the tool ran despite being denied")
		})
	}
}

// denyMessageOnTheWire is what core.EncodeDeny actually puts in front of the
// model for a given reason.
//
// It reads the encoder's own output rather than naming its constants, so a
// blank reason's substitute is whatever the airlock decided it is. That keeps
// the assertion about the property under test - the text reaches the model
// intact - rather than about the wording, which belongs to encode.go and is
// tested there.
func denyMessageOnTheWire(t *testing.T, reason string) string {
	t.Helper()

	line, err := core.EncodeDeny(askRequestID, reason)
	if err != nil {
		t.Fatalf("EncodeDeny(%q): %v", reason, err)
	}
	var ans answeredPermission
	if err := json.Unmarshal(line, &ans); err != nil {
		t.Fatalf("the encoder's own frame does not decode as a permission answer: %v: %s", err, line)
	}
	return ans.Response.Response.Message
}

// Two agents, both stopped dead on their own ask, and one answer.
//
// This is what core.Session.attribute exists for. A permission request carries
// no session_id on Claude's wire, and the CLI numbers requests per process - so
// two agents' first asks are genuinely both called req-1, and the correlator
// alone cannot tell them apart. The only thing that can is the pipe the ask
// arrived on, which is the stamp attribute puts there.
//
// It is the mistake a fleet makes first and the most expensive one it can make:
// the operator reads one agent's request, approves it, and a different agent's
// tool runs against a repository nobody was looking at.
//
// Mutation check: deleting the `if ev.SessionID == ""` guard in
// core.Session.attribute is not enough to fail this - it changes which id an
// already-correct stamp uses. Removing the stamp entirely leaves the ask
// unattributed and this fails in blockedOnTool at "an answer cannot be routed
// back to an agent the ask does not name". Routing the answer by request id
// instead of by session - `s.agentByRequest(f.RequestID)` over the whole fleet,
// which is the plausible shortcut - leaves it failing at "beta's tool ran".
func TestAnAllowRunsTheToolOfOnlyTheSessionItNames(t *testing.T) {
	withToolAgent(t)
	d := startDaemon(t)
	c := attach(t, d.socket)

	dir := blockedOnTool(t, c, idAlpha, "sydney")
	if other := blockedOnTool(t, c, idBeta, "alex"); other != dir {
		t.Fatalf("the two agents were given different working directories (%q, %q): they are supposed to be distinguished by session id alone", dir, other)
	}

	c.send(rpc.Frame{Kind: rpc.FrameAllow, SessionID: idAlpha, RequestID: askRequestID})

	awaitFile(t, toolRanPath(dir, idAlpha), "the allowed agent's tool never ran")
	// Waited out rather than checked instantly. The two agents are independent
	// processes, so "beta has not run its tool yet" is true for a while
	// whatever the daemon does; the question is whether it is still true once
	// alpha's has demonstrably finished, and settleAfterCrossTalk is that gap.
	time.Sleep(settleAfterCrossTalk)
	mustNotExist(t, toolRanPath(dir, idBeta),
		"an answer addressed to one agent ran a different agent's tool")

	// And beta is not merely un-run: it is still stopped, waiting for the
	// answer nobody has given it. Without this the test would also pass against
	// a daemon that had somehow retired beta's ask without acting on it, which
	// leaves an operator holding a prompt whose answer goes nowhere.
	c.pollState(idBeta, rpc.StateBlocked)
	// Alpha, meanwhile, moved - so this compared two agents in different
	// states rather than two that were both simply untouched.
	c.pollState(idAlpha, rpc.StateIdle)
}

// settleAfterCrossTalk is how long the unanswered agent is given to
// misbehave before it is declared untouched. It follows a barrier that has
// already fired - the answered agent's tool has run - so it is the width of the
// window after the event, not a guess at when the event happens.
const settleAfterCrossTalk = 300 * time.Millisecond

// awaitFile waits for a path to appear and fails with why it mattered.
func awaitFile(t *testing.T, path, why string) {
	t.Helper()
	readFileWithin(t, path, why)
}

// readFileWithin waits for a path to appear and returns what is in it.
//
// Polled rather than waited on a frame, because the tool's side effect is
// deliberately outside every channel Wake owns: a barrier taken from Wake's own
// stream would be the thing under test standing surety for itself.
func readFileWithin(t *testing.T, path, why string) string {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for {
		body, err := os.ReadFile(path)
		if err == nil {
			return string(body)
		}
		if !os.IsNotExist(err) {
			t.Fatalf("read %s: %v", path, err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: nothing appeared at %s within %v", why, path, testTimeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// mustNotExist fails if a path is there, naming what its presence would mean.
func mustNotExist(t *testing.T, path, why string) {
	t.Helper()

	switch _, err := os.Stat(path); {
	case err == nil:
		t.Fatalf("%s: %s exists", why, path)
	case !os.IsNotExist(err):
		t.Fatalf("stat %s: %v", path, err)
	}
}
