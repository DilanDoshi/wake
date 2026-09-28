package daemon

// Keeping claude's own session name in step with Wake's (renamesync.go): the
// agent-level half, driven without a process.

import (
	"os"
	"path/filepath"
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
	if err := a.rename(r, to, false); err != nil {
		t.Fatalf("rename to %q: %v", to, err)
	}
}

// mustMirror is the UI's /rename mirror: a rename whose keystroke also sends
// claude its own /rename.
func mustMirror(t *testing.T, a *agent, r *nameRegistry, to string) {
	t.Helper()
	if err := a.rename(r, to, true); err != nil {
		t.Fatalf("mirrored rename to %q: %v", to, err)
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

// The operator's /rename passthrough renames claude itself, its reply shown -
// Wake sent no /rename of its own - and a /name afterwards to the name claude
// already took has nothing to tell it.
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

// A /name arriving mid-turn with an unmirrored /rename passthrough written
// behind it (the manager's, say): the earlier turn's end must not fire the want
// while that /rename is unanswered, its reply stays visible, and at the next
// idle the names agree.
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
	go func() { done <- a.rename(r, "bob", false) }()
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

// A /rename as claude writes it to disk (testdata/transcript/rename.jsonl) - the
// custom-title and agent-name lines, the caveat, the <command-name> envelope and
// the system/local_command entry holding the reply - restores as nothing, and
// core drops every line of it itself, so probeLine needs no /rename arm. The
// turn after it is kept, so the read is not simply empty.
func TestHistoryDropsTheRecordedOnDiskRename(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "rename.jsonl"))
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	recorded := strings.Split(strings.TrimSpace(string(data)), "\n")
	plantTranscript(t, histID, append(recorded, userLine("after"))...)
	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var got []string
	for _, ev := range events {
		got = append(got, ev.Text)
	}
	if want := []string{"after"}; !slices.Equal(got, want) {
		t.Fatalf("restored %q, want %q", got, want)
	}
}

// A mirrored rename sends nothing: claude's own /rename is right behind it, and
// its reply naming Wake's name settles the want. "busy" is a turn another
// window started ending between the mirror and its passthrough.
func TestAMirroredRenameWaitsForClaudesOwnReply(t *testing.T) {
	for _, tc := range []struct {
		name string
		busy bool
	}{{name: "idle"}, {name: "busy", busy: true}} {
		t.Run(tc.name, func(t *testing.T) {
			a, r := renamingAgent(t)
			if tc.busy {
				a.noteSent()
			}
			mustMirror(t, a, r, "bob")
			streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "a turn ends before the passthrough is applied"})
			a.probeIfWanted()
			if got := queuedRenames(a); len(got) != 0 {
				t.Fatalf("a mirrored rename queued %q before claude's own reply", got)
			}

			a.noteRenameSent("/rename bob")
			if !streamed(a, renamedEvent("bob")) {
				t.Fatal("the operator's own /rename reply was kept from clients")
			}
			streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: bob", LocalCommand: true})
			a.probeIfWanted()
			if got := queuedRenames(a); len(got) != 0 {
				t.Fatalf("claude took bob itself, and Wake queued %q", got)
			}
		})
	}
}

// A reply no rename probe claims releases a held want there and then, and
// settles it when claude took Wake's name - so no later reply, to a /rename
// Wake never mirrored, can fire it. What it pins is the release itself.
func TestAHeldWantIsSettledByTheReplyThatReleasesIt(t *testing.T) {
	a, r := renamingAgent(t)
	mustMirror(t, a, r, "bob")
	a.noteRenamed(renamedEvent("bob"))
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.renameHeld || a.probeWanted[renameProbe] {
		t.Fatalf("after claude named itself bob: held %v, wanted %v; want neither", a.renameHeld, a.probeWanted[renameProbe])
	}
}

// Wake hyphenated the name, claude took the spaced form: the reply releases the
// want and it fires once, bringing claude to Wake's name.
func TestAMirroredRenameClaudeTookDifferentlyIsSentOnce(t *testing.T) {
	a, r := renamingAgent(t)
	mustMirror(t, a, r, "foo-bar")
	a.noteSent()
	a.noteRenameSent("/rename foo bar")
	streamed(a, renamedEvent("foo bar"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: foo bar", LocalCommand: true})
	a.probeIfWanted()
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename foo-bar"}) {
		t.Fatalf("claude took \"foo bar\" and Wake queued %q, want one [/rename foo-bar]", got)
	}
}

// Another window's /name over a held want - between the mirror and its
// passthrough - moves its target and keeps it held, so the one /rename cat
// goes only after claude's reply to the passthrough.
func TestANameOverAHeldRenameWaitsForItsReply(t *testing.T) {
	a, r := renamingAgent(t)
	mustMirror(t, a, r, "bob")
	mustRename(t, a, r, "cat")
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("a /name over a held rename queued %q before claude's reply", got)
	}

	a.noteSent()
	a.noteRenameSent("/rename bob")
	streamed(a, renamedEvent("bob"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: bob", LocalCommand: true})
	a.probeIfWanted()
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename cat"}) {
		t.Fatalf("after claude's reply named bob, Wake queued %q, want one [/rename cat]", got)
	}
}

// A held want dies with a parked agent. Wake holds foo-bar and the passthrough
// asked "foo bar", so claude's reply leaves a /rename due - and the agent is
// idle, so nothing but its being gone stands in the way.
func TestAHeldWantOfAParkedAgentNeverFires(t *testing.T) {
	a, r := renamingAgent(t)
	mustMirror(t, a, r, "foo-bar")
	a.noteRenameSent("/rename foo bar")
	a.beginPark()
	a.finish(nil)
	a.markParked()
	a.noteRenamed(renamedEvent("foo bar"))
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("a parked agent's held rename fired: %q", got)
	}
}

// apply marks claude's name unknown before it writes the operator's /rename,
// so the reply - which can reach fanOut before apply returns - lands after the
// mark and is never overwritten by it. Driven with a write that fails: only a
// mark made before the write leaves the name unknown, which is fail-safe.
func TestTheOperatorsRenameIsMarkedBeforeItsWrite(t *testing.T) {
	a, _ := renamingAgent(t) // an unstarted session, so the write fails
	a.apply(pending{from: newClient(nil), frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: a.id, Text: "/rename bob"}})
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.claudeName != "" {
		t.Fatalf("after the operator's /rename was attempted claude's name is %q, want unknown", a.claudeName)
	}
}

// A rename probe's own reply never releases a held want (review O9, in the
// ordering the UI now produces): Wake's /rename cat is in flight when the
// operator's /rename foo bar mirror and passthrough arrive together. The
// probe's reply releases nothing; the passthrough's reply ("foo bar", the ask,
// not Wake's foo-bar) does, and one /rename foo-bar follows.
func TestAProbesOwnReplyDoesNotReleaseAHeldWant(t *testing.T) {
	a, r := renamingAgent(t)
	mustRename(t, a, r, "cat")
	if got := queuedRenames(a); len(got) != 1 {
		t.Fatalf("the idle /name queued %q, want one probe", got)
	}
	if text := a.renameWrite(); text != "/rename cat" {
		t.Fatalf("the probe wrote %q", text)
	}
	a.incProbe(renameProbe) // written, its reply not yet read

	mustMirror(t, a, r, "foo-bar")
	a.noteSent()
	a.noteRenameSent("/rename foo bar")
	streamed(a, renamedEvent("cat"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: cat", LocalCommand: true})
	streamed(a, renamedEvent("foo bar"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: foo bar", LocalCommand: true})
	a.probeIfWanted()
	if got := queuedRenames(a); !slices.Equal(got, []string{"/rename foo-bar"}) {
		t.Fatalf("after the passthrough's reply Wake queued %q, want one [/rename foo-bar]", got)
	}
}

// R1 in the ordering the UI now produces: /name cat on an idle agent fires its
// /rename cat while the operator's /rename bob waits in type-ahead; then the
// mirror and passthrough for bob arrive together, and claude answers bob-2.
// Wake ends bob and claude bob-2, a variant claude chose, and nothing chases it.
func TestANameThenAQueuedRenameAnsweredWithAVariantIsNotChased(t *testing.T) {
	a, r := renamingAgent(t)
	mustRename(t, a, r, "cat")
	if got := queuedRenames(a); len(got) != 1 {
		t.Fatalf("the idle /name queued %q, want one probe", got)
	}
	if text := a.renameWrite(); text != "/rename cat" {
		t.Fatalf("the probe wrote %q", text)
	}
	a.incProbe(renameProbe)
	streamed(a, renamedEvent("cat"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: cat", LocalCommand: true})

	mustMirror(t, a, r, "bob")
	a.noteSent()
	a.noteRenameSent("/rename bob")
	streamed(a, renamedEvent("bob-2"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: bob-2", LocalCommand: true})
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("claude chose bob-2 for the operator's /rename bob, and Wake queued %q", got)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.name != "bob" || a.claudeName != "bob-2" {
		t.Fatalf("Wake is %q and claude %q, want bob and the variant bob-2", a.name, a.claudeName)
	}
}

// A variant claude chose for the operator's own /rename is never chased: the
// reply differs from what the passthrough asked, so the release settles.
func TestAVariantForTheOperatorsRenameIsNeverChased(t *testing.T) {
	a, r := renamingAgent(t)
	mustMirror(t, a, r, "bob")
	a.noteSent()
	a.noteRenameSent("/rename bob")
	streamed(a, renamedEvent("bob-2"))
	streamed(a, core.Event{Kind: core.KindTurnEnd, Text: "Session renamed to: bob-2", LocalCommand: true})
	a.probeIfWanted()
	if got := queuedRenames(a); len(got) != 0 {
		t.Fatalf("claude chose bob-2 for the operator's /rename bob, and Wake queued %q", got)
	}
}
