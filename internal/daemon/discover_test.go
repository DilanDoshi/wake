package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTranscript builds one <uuid>.jsonl under a project slug, with one frame
// per cwd given. It is deliberately not a fixture of recorded bytes: what is
// under test is the *relationship* between a file's location and the
// directories its lines name, and a recording pins one pair of those.
func writeTranscript(t *testing.T, projects, slug, id string, cwds ...string) string {
	t.Helper()
	dir := filepath.Join(projects, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	var b strings.Builder
	for _, c := range cwds {
		line, err := json.Marshal(map[string]any{"type": "user", "cwd": c, "sessionId": id})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	p := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func found(t *testing.T, sessions []FoundSession, id string) FoundSession {
	t.Helper()
	for _, s := range sessions {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("discovery did not report session %s; it reported %d sessions", id, len(sessions))
	return FoundSession{}
}

// A directory is proven when the transcript names it, its slug is the directory
// the transcript was found in, and it still exists. Three facts against each
// other, because no two of them are enough: the slug is lossy (many real paths
// share one), and a cwd is a property of a message rather than of a session.
func TestDiscoveryProvesADirectoryTheTranscriptNamesAndTheSlugAgreesWith(t *testing.T) {
	projects := t.TempDir()
	real := t.TempDir()

	id := "11111111-1111-4111-8111-111111111111"
	writeTranscript(t, projects, slugOf(real), id, real)

	sessions, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	got := found(t, sessions, id)
	if got.Dir != real {
		t.Errorf("discovery proved dir %q, want %q", got.Dir, real)
	}
}

// The 58-of-428 case from the 2026-08-12 findings, and the reason the first cwd
// is not simply read: a --worktree session's transcript lands under the
// directory it was *started* in while every frame names the worktree. Nothing
// in the file says which is which, so Wake says nothing.
func TestDiscoveryProvesNoDirectoryWhenNoNamedCwdSlugsToTheSlugItWasFoundUnder(t *testing.T) {
	projects := t.TempDir()
	started := t.TempDir()
	worktree := filepath.Join(started, "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	id := "22222222-2222-4222-8222-222222222222"
	// Found under the directory it started in; every line names the worktree.
	writeTranscript(t, projects, slugOf(started), id, worktree, worktree)

	sessions, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	got := found(t, sessions, id)
	if got.Dir != "" {
		t.Errorf("discovery proved dir %q for a transcript whose lines name only %q; "+
			"a cwd that does not slug to the directory holding the file is not evidence about where to run",
			got.Dir, worktree)
	}
}

// 93 of the 97 unproven transcripts on the recording machine. The conversation
// is still on disk and the directory it ran in is gone, so there is nowhere to
// run and the session is listed saying so rather than resumed into $PWD.
func TestDiscoveryProvesNoDirectoryWhenTheDirectoryIsGone(t *testing.T) {
	projects := t.TempDir()
	gone := filepath.Join(t.TempDir(), "deleted")

	id := "33333333-3333-4333-8333-333333333333"
	writeTranscript(t, projects, slugOf(gone), id, gone)

	sessions, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	got := found(t, sessions, id)
	if got.Dir != "" {
		t.Errorf("discovery proved dir %q, which does not exist", got.Dir)
	}
}

// The arm that never fired on 428 real transcripts, and the one that stands
// between a lossy function and a confident wrong answer. `a b` and `a.b` are
// one slug, and if a transcript names both there is no evidence which one the
// session ran in.
func TestDiscoveryProvesNoDirectoryWhenTwoRealDirectoriesShareTheSlug(t *testing.T) {
	projects := t.TempDir()
	base := t.TempDir()
	spaced := filepath.Join(base, "a b")
	dotted := filepath.Join(base, "a.b")
	for _, d := range []string{spaced, dotted} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	if slugOf(spaced) != slugOf(dotted) {
		t.Fatalf("the premise is gone: %q and %q no longer share a slug", spaced, dotted)
	}

	id := "44444444-4444-4444-8444-444444444444"
	writeTranscript(t, projects, slugOf(spaced), id, spaced, dotted)

	sessions, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	got := found(t, sessions, id)
	if got.Dir != "" {
		t.Errorf("discovery picked %q out of two real directories that share one slug; "+
			"a lossy function with two candidates has no answer", got.Dir)
	}
}

// The filename is the whole of where an id comes from - 428 of 428 on the
// recording machine are a canonical UUID - so anything else in that directory
// is not a session and must not become one. mintedByWake is the same predicate
// maySpawn and the reaper use.
func TestDiscoverySkipsAFileWhoseNameIsNotASessionId(t *testing.T) {
	projects := t.TempDir()
	real := t.TempDir()
	slug := slugOf(real)
	writeTranscript(t, projects, slug, "notes", real)
	writeTranscript(t, projects, slug, "11111111-1111-4111-8111-111111111111", real)

	sessions, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("discovery reported %d sessions, want 1: %+v", len(sessions), sessions)
	}
	if sessions[0].ID != "11111111-1111-4111-8111-111111111111" {
		t.Errorf("discovery reported %q", sessions[0].ID)
	}
}

// A projects directory that is not there is not an error: it is a machine that
// has never run claude, and the answer to "which sessions could be imported" is
// none. An error would make the verb fail on the one machine where the empty
// answer is the true one.
func TestDiscoveryOfAMissingProjectsDirectoryIsEmptyRatherThanAnError(t *testing.T) {
	sessions, err := discover(filepath.Join(t.TempDir(), "nothing-here"))
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("discovery reported %d sessions from a directory that does not exist", len(sessions))
	}
}

// M6: the existence check reads `err != nil || !fi.IsDir()`, and only the first
// half had a fixture - removing `|| !fi.IsDir()` survived
// TestDiscoveryProvesNoDirectoryWhenTheDirectoryIsGone, which deletes the path
// outright. A *file* where the directory used to be is the other half, and it
// is the shape a build artefact or a stray redirect produces.
func TestDiscoveryProvesNoDirectoryWhenAFileStandsWhereTheDirectoryShould(t *testing.T) {
	projects := t.TempDir()
	notADir := filepath.Join(t.TempDir(), "was-a-directory")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	id := "55555555-5555-4555-8555-555555555555"
	writeTranscript(t, projects, slugOf(notADir), id, notADir)

	sessions, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := found(t, sessions, id); got.Dir != "" {
		t.Errorf("discovery proved dir %q, which exists but is a file: claude locates a transcript by the "+
			"directory it was started in, and a session cannot be started in a file", got.Dir)
	}
}

// M4: a relative cwd never becomes a directory, which is maySpawn's rule
// arriving at the one door that bypasses maySpawn. It would be Stat'd and then
// run relative to the **daemon's** working directory, which is the $PWD failure
// this whole file exists to prevent.
func TestDiscoveryProvesNoDirectoryForARelativeCwd(t *testing.T) {
	projects := t.TempDir()
	rel := "relative-dir"
	if err := os.MkdirAll(filepath.Join(projects, rel), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	id := "66666666-6666-4666-8666-666666666666"
	writeTranscript(t, projects, slugOf(rel), id, rel)

	sessions, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if got := found(t, sessions, id); got.Dir != "" {
		t.Errorf("discovery proved dir %q, which is relative and would resolve against the daemon's own "+
			"working directory rather than anywhere the session ran", got.Dir)
	}
}
