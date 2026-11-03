package daemon

// A fork's conversation pane opens on the conversation it was taken from. claude
// writes a fork's transcript only on the fork's first turn (seen live on 2.1.311),
// so until then the pane's read finds no file of the fork's own.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func projectsDir(t *testing.T) string {
	t.Helper()
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	dir := filepath.Join(projects, "-repo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func paneHistory(c *testClient, id string) string {
	c.t.Helper()
	return strings.Join(historyTexts(c, rpc.FrameHistory, rpc.FrameHistoryReply, id), "|")
}

// ⌃F and `wake fork` write the same FrameFork.
func TestAForkWithNoTurnYetShowsTheConversationAsItWasForked(t *testing.T) {
	fakeClaudeOnPath(t, "")
	dir := projectsDir(t)
	d := startDaemon(t)
	c := attach(t, d.socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)

	asked := recordLine("user", "u-1", "", "the parent was asked")
	answered := recordLine("assistant", "u-2", "u-1", "the parent answered")
	writeTranscriptLines(t, dir, idAlpha, asked, answered)
	forkOf(c, idAlpha, idGamma, "")
	c.pollState(idGamma, rpc.StateIdle)
	// The parent goes on after the fork, and none of that reaches the fork.
	writeTranscriptLines(t, dir, idAlpha, asked, answered, recordLine("user", "u-3", "u-2", "the parent went on"))

	want := "the parent was asked|the parent answered"
	if got := paneHistory(c, idGamma); got != want {
		t.Errorf("a fork with no turn yet has conversation history %q, want the parent's as it was forked %q", got, want)
	}
	// The room draws that conversation under the parent, so the fork has no turns
	// of its own there yet.
	if got := historyTexts(c, rpc.FrameRoomHistory, rpc.FrameRoomHistoryReply, idGamma); len(got) != 0 {
		t.Errorf("a fork with no turn yet has room history %q, want none", got)
	}

	// A wake inside this daemon keeps the snapshot, as it keeps the lineage.
	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idGamma})
	c.awaitState(idGamma, rpc.StateParked)
	c.send(rpc.Frame{Kind: rpc.FrameWake, SessionID: idGamma})
	c.pollState(idGamma, rpc.StateIdle)
	if got := paneHistory(c, idGamma); got != want {
		t.Errorf("after a park and a wake a fork with no turn yet has conversation history %q, want %q", got, want)
	}

	// Its first turn writes its own transcript, which is what is read from then on.
	writeTranscriptLines(t, dir, idGamma, asked, answered, recordLine("user", "u-4", "u-2", "the fork was asked"))
	want = "the parent was asked|the parent answered|the fork was asked"
	if got := paneHistory(c, idGamma); got != want {
		t.Errorf("once it has taken a turn the fork's conversation history is %q, want its own transcript %q", got, want)
	}
}

// A parent that /cleared is forked from the conversation it is writing, so that
// is the transcript the fork's pane shows - and a fork that /clears before its
// first turn has nothing to show.
func TestAForkShowsTheConversationItCopiedAcrossAClearOnEitherSide(t *testing.T) {
	fakeClaudeOnPath(t, "argv")
	dir := projectsDir(t)
	d := startDaemon(t)
	c := attach(t, d.socket)
	spawnFor(c, idAlpha, "alex", t.TempDir())
	c.pollState(idAlpha, rpc.StateIdle)
	conv := clearSession(c, idAlpha)
	c.pollState(idAlpha, rpc.StateIdle)

	// Shorter than the conversation after the clear, so reading it at the wrong
	// length cuts that one short.
	writeTranscriptLines(t, dir, idAlpha, recordLine("user", "u-0", "", "gone"))
	writeTranscriptLines(t, dir, conv,
		recordLine("user", "u-1", "", "asked after the clear"),
		recordLine("assistant", "u-2", "u-1", "answered after the clear"))
	forkOf(c, idAlpha, idGamma, "")
	c.pollState(idGamma, rpc.StateIdle)

	want := "asked after the clear|answered after the clear"
	if got := paneHistory(c, idGamma); got != want {
		t.Errorf("a fork of a cleared parent has conversation history %q, want the conversation the parent was writing %q", got, want)
	}

	clearSession(c, idGamma)
	c.pollState(idGamma, rpc.StateIdle)
	if got := paneHistory(c, idGamma); got != "" {
		t.Errorf("a fork cleared before its first turn has conversation history %q, want none: the model no longer holds it", got)
	}
}

// Between a /clear's reset and its successor's first frame the conversation is
// not known yet, and what the fork inherited is gone either way.
func TestAForkInTheGapAfterAClearHasNoHistory(t *testing.T) {
	dir := projectsDir(t)
	source := "eeeeeeee-7777-4777-8777-777777777777"
	writeTranscriptLines(t, dir, source, recordLine("user", "u-1", "", "inherited"))
	s := newServer(filepath.Join(t.TempDir(), "s"))
	a := &agent{id: idGamma, name: "juno", forkFrom: source, forkBytes: transcriptSize(source)}
	s.agents[idGamma] = a
	if got, err := s.history(idGamma, true); err != nil || len(got) != 1 {
		t.Fatalf("before the /clear the fork's history is %v (%v), want the one inherited record", got, err)
	}

	a.observe(core.Event{Kind: core.KindSessionReset, SessionID: idGamma})
	if got, err := s.history(idGamma, true); err != nil || len(got) != 0 {
		t.Errorf("in the gap after a /clear the fork's history is %v (%v), want none", got, err)
	}
}

// `wake import` and /adopt launch the same way a fork does.
func TestAnImportWithNoTurnYetShowsTheConversationItWasImportedFrom(t *testing.T) {
	fakeClaudeOnPath(t, "")
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	real := t.TempDir()
	dir := filepath.Join(projects, slugOf(real))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := "eeeeeeee-6666-4666-8666-666666666666"
	// The cwd is what discovery proves the import's directory by.
	inDir := func(line string) string { return strings.Replace(line, "{", `{"cwd":"`+real+`",`, 1) }
	asked := inDir(recordLine("user", "u-1", "", "asked by hand"))
	answered := inDir(recordLine("assistant", "u-2", "u-1", "answered by hand"))
	writeTranscriptLines(t, dir, source, asked, answered)

	d := startDaemon(t)
	c := attach(t, d.socket)
	c.send(rpc.Frame{Kind: rpc.FrameImport, SessionID: idGamma, ParentID: source})
	c.pollState(idGamma, rpc.StateIdle)
	writeTranscriptLines(t, dir, source, asked, answered, inDir(recordLine("user", "u-3", "u-2", "asked by hand again")))

	want := "asked by hand|answered by hand"
	if got := paneHistory(c, idGamma); got != want {
		t.Errorf("an import with no turn yet has conversation history %q, want the conversation as it was imported %q", got, want)
	}
}
