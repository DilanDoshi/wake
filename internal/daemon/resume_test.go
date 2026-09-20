package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The happy path: a transcript with a proven directory is resumable, and the
// directory and id are the proven ones — resumeSource decides nothing else.
func TestAResumeTakesTheDirectoryDiscoveryProved(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "aaaaaaaa-1111-4111-8111-111111111111"
	writeTranscript(t, projects, slugOf(real), id, real)

	src, err := s.resumeSource(id)
	if err != nil {
		t.Fatalf("resumeSource: %v", err)
	}
	if src.Dir != real {
		t.Errorf("resume would run in %q, want the proven directory %q", src.Dir, real)
	}
	if src.ID != id {
		t.Errorf("resume source is %q, want %q", src.ID, id)
	}
}

// A transcript whose directory cannot be proven is refused, and the refusal is
// about the directory — the same failure importSource guards, for the same
// reason: claude locates a transcript by where it started, so a resume with no
// directory opens an empty session under a live-looking header.
func TestAResumeIsRefusedWhenNoDirectoryCanBeProven(t *testing.T) {
	s, projects := importServer(t)
	started := t.TempDir()
	worktree := filepath.Join(started, "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	id := "bbbbbbbb-2222-4222-8222-222222222222"
	writeTranscript(t, projects, slugOf(started), id, worktree)

	src, err := s.resumeSource(id)
	if err == nil {
		t.Fatalf("resumeSource allowed a resume of %+v with no provable directory", src)
	}
	for _, want := range []string{"directory", "started in"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is %q and does not contain %q", err, want)
		}
	}
}

// A session id with no transcript anywhere is refused, and the refusal says the
// transcript is missing rather than that the directory is unknown.
func TestAResumeIsRefusedWhenThereIsNoTranscript(t *testing.T) {
	s, _ := importServer(t)
	_, err := s.resumeSource("cccccccc-3333-4333-8333-333333333333")
	if err == nil || !strings.Contains(err.Error(), "no transcript") {
		t.Errorf("the refusal is %v, which does not say the transcript is missing", err)
	}
}

// The reversal, asserted behaviourally: with `ps` off PATH, resumeSafe cannot
// check and so **refuses** — importSource returns that refusal, and resumeSource
// still allows the same source. The one thing that differs between the two calls
// is whether resumeSafe is asked; emptying PATH is what makes "asked" observable.
//
// This is the whole of "same as Claude Code, no guard" in one assertion: the
// resume proceeds on a source no guard could clear.
func TestResumeDoesNotAskResumeSafe(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "abcd0000-1111-4111-8111-111111111111"
	writeTranscript(t, projects, slugOf(real), id, real)

	// Take `ps` away, so resumeSafe's own idsInUse errors and resumeSafe refuses.
	t.Setenv("PATH", "")

	// Premise: importSource refuses here, because it asks resumeSafe and cannot.
	if _, err := s.importSource(id); err == nil {
		t.Fatal("premise broken: importSource must refuse when resumeSafe cannot check")
	}
	// The reversal: resumeSource allows it, because it never asks.
	src, err := s.resumeSource(id)
	if err != nil {
		t.Fatalf("resumeSource refused %v: the reversal is that it does not ask resumeSafe, so a source no guard could clear still resumes", err)
	}
	if src.ID != id {
		t.Errorf("resume source is %q, want %q", src.ID, id)
	}
}
