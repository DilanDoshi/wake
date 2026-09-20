package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
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

// End to end over a real socket and a real fake claude: a resume is **in place**
// (the same id it resumed, not a fork to a new one) and has **no parent** - a
// self-referential ParentID would read as a fork, so the room's history would
// never backfill and the DM header would say "forked from" its own name.
// unparkRecord passes "" for exactly this reason; resumeSession must too.
func TestAResumedSessionIsInPlaceAndHasNoParent(t *testing.T) {
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	fakeClaudeOnPath(t, "")
	real := t.TempDir()
	id := "abcd0000-1111-4111-8111-111111111111"
	writeTranscript(t, projects, slugOf(real), id, real)

	d := startDaemon(t)
	c := attach(t, d.socket)

	c.send(rpc.Frame{Kind: rpc.FrameResume, SessionID: id})
	var got rpc.SessionStatus
	f := c.await("the daemon's answer to a resume of "+id, func(f rpc.Frame) bool {
		if f.Kind == rpc.FrameError && f.SessionID == id {
			return true
		}
		if f.Kind != rpc.FrameStatusReply || f.Status == nil {
			return false
		}
		for _, s := range f.Status.Sessions {
			if s.ID == id && s.State != rpc.StateEnded {
				got = s
				return true
			}
		}
		return false
	})
	if f.Kind == rpc.FrameError {
		t.Fatalf("the daemon refused this resume: %s", f.Text)
	}
	if got.ID != id {
		t.Errorf("the resumed session is %q, want the same id %q - a resume is in place, not a fork to a new id", got.ID, id)
	}
	if got.ParentID != "" {
		t.Errorf("a resumed session reports ParentID %q, want empty: it is not a fork, and a self-referential "+
			"parent would make isFork true - the room's history would never backfill and the DM header would "+
			"read 'forked from' its own name", got.ParentID)
	}
}

// A resume reuses the id, so a session already **live** in this fleet must not
// be resumed as a stranger - that would put a second process on its id. The
// picker drops a live row, so this is the daemon's own backstop for a stale
// snapshot or a racing client.
func TestResumeRefusesASessionAlreadyInTheFleet(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "dddddddd-4444-4444-8444-444444444444"
	writeTranscript(t, projects, slugOf(real), id, real)
	if !s.register(liveAgent(id, "alex", real)) {
		t.Fatalf("could not put session %s in the fleet", id)
	}
	_, err := s.resumeSource(id)
	if err == nil || !strings.Contains(err.Error(), "already in this fleet") {
		t.Errorf("resumeSource of a live session gave %v, want a refusal naming that it is already here", err)
	}
}

// A session the **park book** lists (a cross-restart record, not in s.agents) is
// refused too - resuming it in place would put a live process under an id the
// book reports parked, breaking the Parked/Sessions disjointness. This is the
// path admit's own `parked && !wake` guard defends, which FrameResume's wake
// bypass would otherwise skip with none of unparkRecord's reservation.
func TestResumeRefusesABookParkedSession(t *testing.T) {
	s, projects := importServer(t)
	real := t.TempDir()
	id := "eeee1111-4444-4444-8444-444444444444"
	writeTranscript(t, projects, slugOf(real), id, real)
	if err := s.parked.add(parkedRecord{ID: id, Name: "iris", Label: "feat/x", Dir: real, Parked: time.Now()}); err != nil {
		t.Fatalf("seed a park-book record: %v", err)
	}
	_, err := s.resumeSource(id)
	if err == nil || !strings.Contains(err.Error(), "parked in this fleet") {
		t.Errorf("resumeSource of a book-parked session gave %v, want a refusal naming that it is parked", err)
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
