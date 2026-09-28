package daemon

// Keeping claude's own session name in step with Wake's (renamesync.go).

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// renamingAgent is effortAgent's live, unstarted agent, holding its name in a
// registry so a rename can be asked of it.
func renamingAgent(t *testing.T) (*agent, *nameRegistry) {
	t.Helper()
	r := newNameRegistry()
	if _, err := r.claim("sydney"); err != nil {
		t.Fatalf("claim sydney: %v", err)
	}
	return effortAgent(t), r
}

// queuedRenames drains a's stdin queue and returns every rename probe's line.
func queuedRenames(a *agent) []string {
	var out []string
	for {
		select {
		case p := <-a.in:
			if p.probe == renameProbe {
				if p.frame.Kind != rpc.FrameSend {
					out = append(out, "not a send: "+p.frame.Kind)
					continue
				}
				out = append(out, p.frame.Text)
			}
		default:
			return out
		}
	}
}

// renamedEvent is claude's /rename reply as it arrives, list-agents.jsonl:7.
func renamedEvent(name string) core.Event {
	return core.Event{Kind: core.KindAssistantText, Text: "Session renamed to: " + name}
}

// streamed is one event taken the way fanOut takes it: claude's name first,
// then the probe window. It reports whether a client would see the event.
func streamed(a *agent, ev core.Event) bool {
	a.noteRenamed(ev)
	suppress, _ := a.absorbProbe(ev)
	if !suppress {
		a.observe(ev)
	}
	return !suppress
}

func mustRename(t *testing.T, a *agent, r *nameRegistry, to string) {
	t.Helper()
	if err := a.rename(r, to); err != nil {
		t.Fatalf("rename to %q: %v", to, err)
	}
}

// A Wake rename of an idle agent tells claude at once, through the agent's own
// stdin queue - rename holds a.mu and writes to no process - and with the name
// the registry settled on, not the one asked for.
func TestAWakeRenameQueuesClaudesOwnRenameWithTheNameWakeHolds(t *testing.T) {
	a, r := renamingAgent(t)
	mustRename(t, a, r, "  Bob ")

	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename bob"}) {
		t.Fatalf("a rename of an idle agent queued %q, want exactly [/rename bob] - the registry's "+
			"normalised name, sent through the queue", got)
	}
}

// A rename while a turn is owed waits for that turn's end, and two renames in
// that time are one /rename of the later name.
func TestRenamesDuringATurnAreOneRenameOfTheLatestAtItsEnd(t *testing.T) {
	a, r := renamingAgent(t)
	a.noteSent()
	mustRename(t, a, r, "ann")
	mustRename(t, a, r, "zed")
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("a rename was queued into a turn in flight: %q", got)
	}

	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "done"})
	a.probeIfWanted()
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename zed"}) {
		t.Fatalf("the turn's end queued %q, want exactly one [/rename zed]", got)
	}
}

// The operator's /rename passthrough renames claude itself; its reply, seen
// first, leaves the mirrored Wake rename with nothing to tell - and the reply
// is not suppressed, since Wake sent no /rename of its own.
func TestTheOperatorsRenameRepliedFirstLeavesNothingToSend(t *testing.T) {
	a, r := renamingAgent(t)
	a.noteRenameSent("/rename bob")
	if !streamed(a, renamedEvent("bob")) {
		t.Fatal("the operator's own /rename reply was kept from clients")
	}
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: bob", LocalCommand: true})

	mustRename(t, a, r, "bob")
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("claude already calls itself bob, and Wake queued %q", got)
	}
	// The reply made claude's name known again, so the next rename is sent.
	mustRename(t, a, r, "cat")
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename cat"}) {
		t.Fatalf("a rename after the operator's reply queued %q, want [/rename cat]", got)
	}
}

// The mirrored Wake rename arriving first, mid-turn, with the operator's
// passthrough written behind it: the earlier turn's end must not fire the want
// while the operator's /rename is unanswered, its reply stays visible, and at
// the next idle the names agree.
func TestTheOperatorsRenameInFlightHoldsTheWantUntilItsReply(t *testing.T) {
	a, r := renamingAgent(t)
	a.noteSent()
	mustRename(t, a, r, "bob")
	a.noteRenameSent("/rename bob")

	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "the earlier turn"})
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("the want fired while the operator's /rename was unanswered: %q", got)
	}

	if !streamed(a, renamedEvent("bob")) {
		t.Fatal("the operator's own /rename reply was kept from clients")
	}
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: bob", LocalCommand: true})
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("claude already calls itself bob, and Wake queued %q", got)
	}
}

// A rename probe is decided again when it is written, because the queue can
// hold the operator's /rename ahead of it: then it waits for that reply, and
// when names already agree it writes nothing.
func TestAQueuedRenameIsDecidedAgainWhenItIsWritten(t *testing.T) {
	a, r := renamingAgent(t)
	mustRename(t, a, r, "bob")
	if got := queuedRenames(a); len(got) != 1 {
		t.Fatalf("the idle rename queued %q, want one probe", got)
	}

	// The operator's /rename bob, queued ahead of the probe, is written first.
	a.noteSent()
	a.noteRenameSent("/rename bob")
	if text := a.renameWrite(); text != "" {
		t.Fatalf("the probe wrote %q behind the operator's unanswered /rename", text)
	}

	streamed(a, renamedEvent("bob"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: bob", LocalCommand: true})
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("after the operator's reply named bob, Wake queued %q", got)
	}

	// And names that agree at the write send nothing, without re-arming.
	a.probeWanted[renameProbe] = false
	if text := a.renameWrite(); text != "" || a.probeWanted[renameProbe] {
		t.Fatalf("with the names agreeing the write gave %q (want re-armed: %v)", text, a.probeWanted[renameProbe])
	}
}

// A rename probe queued while idle, with an ordinary message queued ahead of
// it, is not written into that message's turn: sendProbe decides it again at
// the write, and the turn's end sends it.
func TestARenameProbeWaitsBehindATurnWrittenAheadOfIt(t *testing.T) {
	a, r := renamingAgent(t)
	mustRename(t, a, r, "bob")
	var p pending
	select {
	case p = <-a.in:
	default:
		t.Fatal("the idle rename queued nothing")
	}

	a.noteSent() // the message ahead of it was written and its turn is owed
	a.sendProbe(p)
	if a.pendingProbes[renameProbe] != 0 {
		t.Fatal("the rename probe opened a window inside a turn in flight")
	}

	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "done"})
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename bob"}) {
		t.Fatalf("the turn's end queued %q, want the deferred [/rename bob]", got)
	}
}

// Claude may answer with a variant of the name. Wake's reply is kept from
// clients, and nothing after it - an idle, a turn end, a probe - asks again:
// one /rename per Wake rename.
func TestAVariantNameIsNeverChasedIntoALoop(t *testing.T) {
	a, r := renamingAgent(t)
	mustRename(t, a, r, "bob")
	if got := queuedRenames(a); len(got) != 1 {
		t.Fatalf("the idle rename queued %q, want one probe", got)
	}
	if text := a.renameWrite(); text != "/rename bob" {
		t.Fatalf("the write gave %q, want /rename bob", text)
	}
	a.incProbe(renameProbe) // apply opens the window as it writes

	if streamed(a, renamedEvent("bob-2")) {
		t.Fatal("Wake's own /rename reply reached clients")
	}
	if streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: bob-2", LocalCommand: true}) {
		t.Fatal("Wake's own /rename turn end reached clients")
	}

	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "a later turn"})
	a.probeIfWanted()
	a.tryProbe()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("claude took bob-2 and Wake asked again: %q", got)
	}
	// Only a new Wake rename asks again - and it does, since claude is bob-2.
	mustRename(t, a, r, "bob")
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename bob"}) {
		t.Fatalf("a new rename to bob, with claude at bob-2, queued %q", got)
	}
}

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

// A want still pending when the agent is stopped or parked never fires into
// what is left of it.
func TestAStoppedOrParkedAgentsPendingRenameNeverFires(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(*agent)
	}{
		{name: "stopped", end: func(a *agent) { a.mu.Lock(); a.stopped = true; a.mu.Unlock() }},
		{name: "parked", end: func(a *agent) { a.beginPark(); a.finish(nil); a.markParked() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, r := renamingAgent(t)
			a.noteSent()
			mustRename(t, a, r, "bob")
			tc.end(a)
			streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "done"})
			a.probeIfWanted()
			if got := queuedRenames(a); len(got) != 0 {
				t.Fatalf("a %s agent's pending rename fired: %q", tc.name, got)
			}
		})
	}
}

// Every launch - a wake and a /resume included - passes the current Wake name
// as --name, so a launched agent starts with claude's name in step: renaming
// it to that name sends nothing, and to any other sends one.
func TestALaunchedAgentStartsWithClaudesNameInStep(t *testing.T) {
	r := newNameRegistry()
	if _, err := r.claim("bob"); err != nil {
		t.Fatalf("claim bob: %v", err)
	}
	a := newAgent(idAlpha, "bob", "dev-1", "/repo/api", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})

	mustRename(t, a, r, "bob")
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("an agent launched as bob was told it is bob: %q", got)
	}
	mustRename(t, a, r, "cat")
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename cat"}) {
		t.Fatalf("renaming it to cat queued %q, want [/rename cat]", got)
	}
}

// rename holds a.mu and writes to no process, so an agent whose queue is full
// is still renamed at once, its claude rename kept due for a later idle.
func TestARenameNeverWaitsOnAFullQueue(t *testing.T) {
	a, r := renamingAgent(t)
	for range agentQueue {
		a.in <- pending{frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: a.id, Text: "queued"}}
	}
	done := make(chan error, 1)
	go func() { done <- a.rename(r, "bob") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("rename: %v", err)
		}
	case <-time.After(testTimeout):
		t.Fatal("a rename waited on the agent's full stdin queue")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.probeWanted[renameProbe] || a.name != "bob" {
		t.Fatalf("after the rename: name %q, claude rename still due %v", a.name, a.probeWanted[renameProbe])
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
	c.send(rpc.Frame{Kind: rpc.FrameRename, SessionID: id, Text: name})
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

// Over a real process, the reply-first order: the operator's /rename bob
// renames claude and its reply is shown, so the mirrored Wake rename sends
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

// Restored history drops a /rename line and its reply, on their own shape -
// Wake's and the operator's alike, since the disk cannot tell them apart - and
// keeps a message that only mentions the command.
func TestHistoryDropsTheRenamePair(t *testing.T) {
	plantTranscript(t, histID,
		userLine("run the tests"),
		userLine("/rename bob"),
		assistantLine("Session renamed to: bob"),
		userLine("please /rename yourself when done"),
		assistantLine("done"),
	)
	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var got []string
	for _, ev := range events {
		got = append(got, ev.Text)
	}
	want := []string{"run the tests", "please /rename yourself when done", "done"}
	if !slices.Equal(got, want) {
		t.Fatalf("restored %q, want %q", got, want)
	}
}

// The on-disk form recorded for a /rename (at-menu findings §1a) - a caveat, the
// command envelope and a system/local_command entry - restores as nothing.
func TestHistoryDropsTheRecordedOnDiskRename(t *testing.T) {
	plantTranscript(t, histID,
		userLine("before"),
		`{"type":"user","isMeta":true,"isSidechain":false,"message":{"role":"user","content":"<local-command-caveat>Caveat: The messages below were generated by the user while running local commands.</local-command-caveat>"}}`,
		`{"type":"user","isSidechain":false,"message":{"role":"user","content":"<command-name>/rename</command-name>\n            <command-message>rename</command-message>\n            <command-args>bob</command-args>"}}`,
		`{"type":"system","subtype":"local_command","isSidechain":false,"content":"<local-command-stdout>Session renamed to: bob</local-command-stdout>"}`,
		userLine("after"),
	)
	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var got []string
	for _, ev := range events {
		got = append(got, ev.Text)
	}
	if want := []string{"before", "after"}; !slices.Equal(got, want) {
		t.Fatalf("restored %q, want %q", got, want)
	}
}
