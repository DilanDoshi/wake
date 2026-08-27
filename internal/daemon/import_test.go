package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// importServer is a server with a projects tree it owns and no listener.
//
// importSource starts no process and dials nothing - it reads the disk, the
// agent map and one `ps` - so a bare server is the whole world it needs. That
// is the point: the refusals below are decided before anything is started, and
// a harness that started a daemon would be asserting over a wider world than
// the function has.
func importServer(t *testing.T) (*server, string) {
	t.Helper()
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	return newServer(filepath.Join(t.TempDir(), "wake.sock")), projects
}

func refuses(t *testing.T, s *server, id string) string {
	t.Helper()
	src, err := s.importSource(id)
	if err == nil {
		t.Fatalf("importSource(%s) allowed an import of %+v, and this case must be refused", id, src)
	}
	return err.Error()
}

// The happy path, and the only thing it is allowed to say: a transcript with a
// proven directory is importable, and the directory is the proven one.
func TestAnImportTakesTheDirectoryDiscoveryProved(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "aaaaaaaa-1111-4111-8111-111111111111"
	writeTranscript(t, projects, slugOf(real), id, real)

	src, err := s.importSource(id)
	if err != nil {
		t.Fatalf("importSource: %v", err)
	}
	if src.Dir != real {
		t.Errorf("import would run in %q, want the proven directory %q", src.Dir, real)
	}
	if src.ID != id {
		t.Errorf("import source is %q, want %q", src.ID, id)
	}
}

// The 97-of-428 case. A transcript whose directory cannot be proven is refused
// **and the refusal is about the directory**, because that is the thing the
// operator can act on: open the session where it lives.
//
// This is the refusal that matters most, because the alternative to it is not
// an error - it is an import that runs in the daemon's own directory, finds no
// transcript there, and opens an empty conversation under a live-looking
// header.
func TestAnImportIsRefusedWhenNoDirectoryCanBeProven(t *testing.T) {
	s, projects := importServer(t)
	started := t.TempDir()
	worktree := filepath.Join(started, "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := "bbbbbbbb-2222-4222-8222-222222222222"
	writeTranscript(t, projects, slugOf(started), id, worktree)

	why := refuses(t, s, id)
	for _, want := range []string{"directory", "started in"} {
		if !strings.Contains(why, want) {
			t.Errorf("the refusal is %q and does not contain %q: an operator who cannot tell that this is about *where* to run has nothing to act on", why, want)
		}
	}
}

// A session id with no transcript anywhere. Distinct from the case above, and
// the refusal says so: there is nothing to import rather than nowhere to run
// it.
func TestAnImportIsRefusedWhenThereIsNoTranscript(t *testing.T) {
	s, _ := importServer(t)
	why := refuses(t, s, "cccccccc-3333-4333-8333-333333333333")
	if !strings.Contains(why, "no transcript") {
		t.Errorf("the refusal is %q, which does not say the transcript is missing", why)
	}
}

// An id that is not a UUID never reaches the disk. The same predicate maySpawn
// and the reaper apply, for the reaper's reason: it proves a process group by
// finding a session id in an argv, and a short or ordinary string matches
// somebody's shell job.
func TestAnImportIsRefusedForAnIdThatIsNotASessionId(t *testing.T) {
	s, _ := importServer(t)
	why := refuses(t, s, "build")
	if !strings.Contains(why, "UUID") {
		t.Errorf("the refusal is %q, which does not say why the id is not one", why)
	}
}

// An import of a session already in this fleet would put the same conversation
// under two names, with the operator's next message going to whichever one they
// happened to have open. Refused, and the refusal names both things they might
// have meant - the same shape `wake attach` uses on a parked session.
func TestAnImportIsRefusedWhenTheSessionIsAlreadyInTheFleet(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "dddddddd-4444-4444-8444-444444444444"
	writeTranscript(t, projects, slugOf(real), id, real)

	if !s.register(liveAgent(id, "alex", real)) {
		t.Fatalf("could not put session %s in the fleet", id)
	}

	why := refuses(t, s, id)
	if !strings.Contains(why, "already in this fleet") {
		t.Errorf("the refusal is %q, which does not say the session is already here", why)
	}
	for _, verb := range []string{"wake attach", "wake fork"} {
		if !strings.Contains(why, verb) {
			t.Errorf("the refusal is %q and does not offer %q: an operator told 'no' with no next step tries the same thing again", why, verb)
		}
	}
}

// liveAgent is an agent with an **unstarted** core.Session behind it, which is
// parkedAgent's own trick: Pgid() reports 0 and no goroutine exists, so a bare
// server can hold it and s.fleet() can snapshot it. A nil Session panics there,
// which is how this helper came to exist.
func liveAgent(id, name, dir string) *agent {
	return newAgent(id, name, "label", dir, "", core.NewSession(core.Config{SessionID: id}), func() {})
}

// **I3: a parked source is pointed at the verb that works.**
//
// completePark deliberately leaves a parked session's id in s.agents so `holds`
// refuses a respawn under it, which means a parked session reaches the
// already-here arm looking like any other. It is not: `wake attach` refuses a
// parked session, so the general sentence hands the operator a verb that will
// itself refuse and never names the one thing that works.
//
// That is the defect CLAUDE.md records repairing on `wake attach`'s own
// refusal, arriving at a new door, so this asserts the repaired shape rather
// than only that some sentence came back.
func TestAnImportOfAParkedSessionLeadsWithTheVerbThatBringsItBack(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "eeee0000-4444-4444-8444-444444444444"
	writeTranscript(t, projects, slugOf(real), id, real)

	a := liveAgent(id, "sydney", real)
	a.finish(nil)
	a.markParked()
	if !s.register(a) {
		t.Fatalf("could not put session %s in the fleet", id)
	}
	// The premise, through the producer rather than the flag: if this is not
	// reported parked, the refusal below is the ordinary one and this test is
	// asserting nothing.
	if row := s.fleet().Sessions; len(row) != 1 || row[0].State != rpc.StateParked {
		t.Fatalf("the fixture reports %+v, want one session in %q", row, rpc.StateParked)
	}

	why := refuses(t, s, id)
	if !strings.Contains(why, "/resume sydney") {
		t.Errorf("the refusal is %q and does not name `/resume sydney`, the one thing that brings a parked "+
			"session back. CLAUDE.md records this exact defect being repaired on `wake attach`'s refusal: an "+
			"operator pointed at `wake fork` gets a second session, a second name, and the original still parked", why)
	}
	if !strings.Contains(why, "refuses a parked session") {
		t.Errorf("the refusal is %q and does not say why `wake attach` is not the answer, so an operator who "+
			"knows that verb will try it and be refused again", why)
	}
}

// **I2: an import poisons resumeSafe about its own source, forever.**
//
// An imported agent runs as `--resume <src> --fork-session --session-id <new>`
// for its whole life, and core.SessionArgvMarkers matches a flag and its value -
// so idsInUse(src) matches Wake's own agent from the moment one import
// succeeds. resumeSafe's sentence is then false in every clause, and its last
// one - "close it there first" - instructs a destructive action against the
// only session the operator can find to close.
//
// This drives the refusal from the fleet side rather than from a real process,
// because what is being asserted is which *sentence* comes back when Wake holds
// an agent whose parent is the source. The unix test beside it covers the
// process half.
func TestASecondImportOfAnAlreadyImportedSourceDoesNotTellAnybodyToCloseATerminal(t *testing.T) {
	s, _ := importServer(t)
	real := t.TempDir()
	sourceID := "abcd0000-1111-4111-8111-111111111111"

	imported := newAgent("dcba0000-2222-4222-8222-222222222222", "sydney", "label", real, sourceID,
		core.NewSession(core.Config{SessionID: "dcba0000-2222-4222-8222-222222222222"}), func() {})
	if !s.register(imported) {
		t.Fatal("could not put the imported agent in the fleet")
	}

	err := s.explainHeldSource(sourceID, errors.New("a process is still running under session X; close it there first"))
	if err == nil {
		t.Fatal("the refusal was dropped: an import is still refused here, the sentence is what changes")
	}
	if strings.Contains(err.Error(), "close it there first") {
		t.Errorf("the refusal is %q. The process Wake can see is its own imported agent, and the only session "+
			"an operator could find to close is the real terminal one - which may be mid-turn and which "+
			"`wake status` does not even list, because the fleet carries the import's id and not the source's", err)
	}
	for _, want := range []string{"already been imported", "@sydney", "wake attach sydney"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is %q and does not contain %q", err, want)
		}
	}
}

// The other half of the same rule, and the reason explainHeldSource is a
// narrowing rather than a suppression: with no imported agent in the fleet, the
// original refusal is returned untouched.
func TestAHeldSourceWithNoImportBehindItKeepsResumeSafesOwnSentence(t *testing.T) {
	s, _ := importServer(t)
	original := errors.New("a process is still running under session X; close it there first")
	if got := s.explainHeldSource("abcd0000-1111-4111-8111-111111111111", original); got != original {
		t.Errorf("explainHeldSource rewrote %q to %q with nothing in the fleet to explain it: the sentence may "+
			"only change when Wake can prove the process it saw is its own", original, got)
	}
}

// An empty source id is a client bug, not an operator one, and the sentence
// still points at the thing a person would type next.
func TestAnImportWithNoSourceNamesTheListing(t *testing.T) {
	s, _ := importServer(t)
	why := refuses(t, s, "")
	if !strings.Contains(why, "wake import") {
		t.Errorf("the refusal is %q, which does not say how to find out what could be imported", why)
	}
}
