package daemon

// Keeping claude's own session name in step with Wake's, over a real process:
// the renamesync fake, driven through the daemon.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// apply marks claude's name unknown when it writes an operator's /rename, and
// only then - driven through a real process, since apply writes to one.
func TestWritingTheOperatorsRenameLeavesClaudesNameUnknown(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	ctx, cancel := context.WithCancel(context.Background())
	sess := core.NewSession(core.Config{SessionID: idAlpha, Name: "sydney"})
	if err := sess.Start(ctx); err != nil {
		cancel()
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		_ = sess.Stop()
		for range sess.Events() {
		}
		cancel()
	})
	a := newAgent(idAlpha, "sydney", "dev-1", "/repo/api", "", sess, cancel)
	claude := func() string { a.mu.Lock(); defer a.mu.Unlock(); return a.claudeName }

	a.apply(pending{frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "please /rename yourself"}})
	if got := claude(); got != "sydney" {
		t.Fatalf("an ordinary message moved claude's name to %q", got)
	}
	a.apply(pending{frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename bob"}})
	if got := claude(); got != "" {
		t.Fatalf("after writing the operator's /rename claude's name is %q, want unknown until its reply", got)
	}
}

// fakeRenameSync answers a bare /rename the way 2.1.283 does
// (list-agents.jsonl:7-8): the name as the assistant's text, then a local
// command result. "clash" takes a variant, as a colliding name does. "hold"
// keeps a turn open, buffering what arrives, until "release"; "renames?"
// reports every /rename received, and the --name it was started with.
func fakeRenameSync(sid string) int {
	var renames, held []string
	holding := false
	answer := func(line string) {
		switch name := argAskedIn(line, "/rename "); {
		case name != "":
			renames = append(renames, name)
			if name == "clash" {
				name += "-2"
			}
			emitText(sid, "Session renamed to: "+name)
			fmt.Printf(`{"type":"result","subtype":"success","is_error":false,"num_turns":0,"session_id":%q,"result":%q}`+"\n",
				sid, "Session renamed to: "+name)
		case strings.Contains(line, `"text":"renames?"`):
			emitText(sid, fmt.Sprintf("renames: %d %v name=%s", len(renames), renames, argValue(os.Args, "--name")))
			emitResult(sid)
		default:
			emitText(sid, "echo: "+line)
			emitResult(sid)
		}
	}
	for line := range stdinLines() {
		switch {
		case strings.Contains(line, `"text":"hold"`):
			holding = true
		case strings.Contains(line, `"text":"release"`):
			holding = false
			emitResult(sid) // the held turn ends, then what waited behind it runs
			for _, l := range held {
				answer(l)
			}
			held = nil
		case holding:
			held = append(held, line)
		default:
			answer(line)
		}
	}
	return 0
}

// askRenames asks the fake what it has been sent and returns its answer.
func askRenames(c *testClient, id string) string {
	c.t.Helper()
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: "renames?"})
	return c.await("the fake's count of renames", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.SessionID == id && f.Event != nil &&
			strings.HasPrefix(f.Event.Text, "renames: ")
	}).Event.Text
}

func renameTo(c *testClient, id, name string) {
	c.t.Helper()
	sendRename(c, rpc.Frame{Kind: rpc.FrameRename, SessionID: id, Text: name})
}

// mirrorTo is the UI's /rename mirror over the wire.
func mirrorTo(c *testClient, id, name string) {
	c.t.Helper()
	sendRename(c, rpc.Frame{Kind: rpc.FrameRename, SessionID: id, Text: name, SelfRenames: true})
}

func sendRename(c *testClient, f rpc.Frame) {
	c.t.Helper()
	id, name := f.SessionID, f.Text
	c.send(f)
	c.await("the rename to "+name+" published", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameStatusPush && f.Status != nil && sessionRow(*f.Status, id).Name == name
	})
}

// renameRepliesSeen counts the /rename replies that reached this client.
func renameRepliesSeen(c *testClient) int {
	n := 0
	for _, f := range c.seen {
		if f.Kind == rpc.FrameEvent && f.Event != nil && f.Event.Kind == core.KindAssistantText {
			if _, ok := core.RenamedFromReply(f.Event.Text); ok {
				n++
			}
		}
	}
	return n
}

// The whole round trip over a real process: a Wake rename reaches claude as one
// bare /rename, and its reply reaches no client.
func TestAWakeRenameTellsClaudeOnceAndItsReplyReachesNoClient(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	renameTo(c, idAlpha, "bob")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [bob]") {
		t.Fatalf("claude was sent %q, want exactly one /rename bob", got)
	}
	if n := renameRepliesSeen(c); n != 0 {
		t.Fatalf("%d /rename replies reached a client\nsaw: %s", n, c.transcript())
	}
}

// Over a real process: renames while a turn runs send nothing into it, and its
// end sends one /rename of the latest.
func TestRenamesDuringATurnReachClaudeAsOneRenameAfterIt(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hold"})
	renameTo(c, idAlpha, "ann")
	renameTo(c, idAlpha, "zed")
	// Asked inside the held turn, answered after it: anything written mid-turn
	// is ahead of the question.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "renames?"})
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "release"})
	during := c.await("the count asked mid-turn", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && strings.HasPrefix(f.Event.Text, "renames: ")
	}).Event.Text
	if !strings.HasPrefix(during, "renames: 0 ") {
		t.Fatalf("claude was sent a /rename during its turn: %q", during)
	}
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [zed]") {
		t.Fatalf("after the turn claude was sent %q, want one /rename zed", got)
	}
}

// Over a real process: the operator's /rename bob renames claude and its reply
// is shown, so a /name afterwards to the name claude already took sends
// nothing.
func TestTheOperatorsRenameAnsweredFirstIsTheOnlyRename(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename bob"})
	c.awaitEvent(idAlpha, "Session renamed to: bob")
	renameTo(c, idAlpha, "bob")
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [bob]") {
		t.Fatalf("claude was sent %q, want only the operator's /rename bob", got)
	}
}

// Over a real process, the mirror-first order: the Wake rename arrives while a
// turn runs and the operator's /rename is written behind it. The turn's end
// does not send a second /rename, and the operator's reply is shown.
func TestTheMirroredRenameArrivingFirstSendsNoSecondRename(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hold"})
	renameTo(c, idAlpha, "bob")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename bob"})
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "release"})
	c.awaitEvent(idAlpha, "Session renamed to: bob")
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [bob]") {
		t.Fatalf("claude was sent %q, want only the operator's /rename bob", got)
	}
	if n := renameRepliesSeen(c); n != 1 {
		t.Fatalf("%d /rename replies reached a client, want the operator's one\nsaw: %s", n, c.transcript())
	}
}

// Over a real process: claude answers a variant, and later turns never ask
// again.
func TestAVariantNameIsAskedForOnce(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	renameTo(c, idAlpha, "clash")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hello"})
	c.awaitEvent(idAlpha, "echo: ")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [clash]") {
		t.Fatalf("claude took clash-2 and was sent %q, want the one /rename clash", got)
	}
}

// Over a real process, the concern this fixes: the UI's mirror arrives while
// the agent works, its passthrough held in type-ahead until the turn ends. The
// turn's end sends nothing, the passthrough renames claude, and its reply is
// the only one a client sees.
func TestAMirrorAheadOfItsHeldBackPassthroughSendsNothing(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hold"})
	mirrorTo(c, idAlpha, "bob")
	// Answered after the held turn ends, so anything the end queued is ahead of
	// the passthrough below.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "renames?"})
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "release"})
	c.await("the count after the turn", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && strings.HasPrefix(f.Event.Text, "renames: ")
	})
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename bob"})
	c.awaitEvent(idAlpha, "Session renamed to: bob")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [bob]") {
		t.Fatalf("claude was sent %q, want only the operator's /rename bob", got)
	}
	if n := renameRepliesSeen(c); n != 1 {
		t.Fatalf("%d /rename replies reached a client, want the operator's one\nsaw: %s", n, c.transcript())
	}
}

// The same with the agent idle: the mirror lands first and sends nothing.
func TestAMirrorOfAnIdleAgentSendsNothing(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	mirrorTo(c, idAlpha, "bob")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename bob"})
	c.awaitEvent(idAlpha, "Session renamed to: bob")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [bob]") {
		t.Fatalf("claude was sent %q, want only the operator's /rename bob", got)
	}
}

// Over a real process: Wake holds foo-bar, claude took "foo bar", and one
// /rename foo-bar brings claude to Wake's name.
func TestAHyphenatedMirrorIsSentOnceInWakesForm(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	mirrorTo(c, idAlpha, "foo-bar")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename foo bar"})
	c.awaitEvent(idAlpha, "Session renamed to: foo bar")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 2 [foo bar foo-bar]") {
		t.Fatalf("claude was sent %q, want the operator's /rename foo bar and one /rename foo-bar", got)
	}
}

// Over a real process: a mirrored bob, then /name cat, both before claude's
// reply to bob. Wake sends one /rename cat, after that reply.
func TestANameBeforeTheMirroredReplyIsTheOneRenameSent(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hold"})
	mirrorTo(c, idAlpha, "bob")
	renameTo(c, idAlpha, "cat")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename bob"})
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "release"})
	c.awaitEvent(idAlpha, "Session renamed to: bob")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 2 [bob cat]") {
		t.Fatalf("claude was sent %q, want the operator's /rename bob, then one /rename cat", got)
	}
}

// The gap fix round 1 left, over a real process: /rename bob while the agent
// works (its passthrough held in type-ahead), then /name cat, then the turn
// ends and the passthrough flushes. Nothing goes before claude's reply to bob;
// after it, one /rename cat, and both end as cat.
func TestANameWhileTheMirroredPassthroughWaitsEndsBothAsTheName(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "hold"})
	mirrorTo(c, idAlpha, "bob")
	renameTo(c, idAlpha, "cat")
	// Answered after the held turn ends, so anything that end queued is ahead
	// of the flushed passthrough.
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "renames?"})
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "release"})
	after := c.await("the count after the turn", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameEvent && f.Event != nil && strings.HasPrefix(f.Event.Text, "renames: ")
	}).Event.Text
	if !strings.HasPrefix(after, "renames: 0 ") {
		t.Fatalf("the turn's end sent claude a /rename before the passthrough: %q", after)
	}

	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename bob"})
	c.awaitEvent(idAlpha, "Session renamed to: bob")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 2 [bob cat]") {
		t.Fatalf("claude was sent %q, want the operator's /rename bob and then one /rename cat", got)
	}
	if name := sessionRow(c.status(), idAlpha).Name; name != "cat" {
		t.Fatalf("Wake calls the agent %q, want cat", name)
	}
}

// Over a real process: the operator's /rename clash is answered clash-2, a
// name claude chose, so Wake sends nothing after it.
func TestAVariantForTheOperatorsRenameIsNotChasedOverAProcess(t *testing.T) {
	fakeClaudeOnPath(t, "renamesync")
	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "sydney")

	mirrorTo(c, idAlpha, "clash")
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: idAlpha, Text: "/rename clash"})
	c.awaitEvent(idAlpha, "Session renamed to: clash-2")
	askRenames(c, idAlpha)
	if got := askRenames(c, idAlpha); !strings.HasPrefix(got, "renames: 1 [clash]") {
		t.Fatalf("claude was sent %q, want only the operator's /rename clash", got)
	}
}
