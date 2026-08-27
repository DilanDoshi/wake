package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
)

// projectsTree builds a claude projects directory this test owns, and points
// discovery at it.
func projectsTree(t *testing.T) string {
	t.Helper()
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	return projects
}

// slugFor is this package's own copy of the rule internal/daemon applies, and
// it is **not** a parallel implementation for one reason: nothing reads its
// answer except the code that puts a fixture on disk, and every fixture built
// with it has its premise checked by transcript() through daemon.Discoverable -
// the real thing. If the two ever disagree, every test here fails saying so
// rather than passing over a tree discovery cannot see.
//
// The alternative was exporting daemon.slugOf, and that is worse: the function
// is lossy, its whole safety property is that production may only ever *compare*
// with it, and an exported one is an invitation to build a path.
func slugFor(dir string) string {
	var b strings.Builder
	for _, r := range dir {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// transcript writes one session under the slug of dir, naming cwd on every
// line.
func transcript(t *testing.T, projects, slugSource, id string, cwds ...string) {
	t.Helper()
	dir := filepath.Join(projects, slugFor(slugSource))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var b strings.Builder
	for _, c := range cwds {
		line, err := json.Marshal(map[string]any{"type": "user", "cwd": c, "lastPrompt": "what is the plan"})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// The premise, read back through the real discovery rather than trusted.
	// A slug rule that drifted would otherwise leave every test here asserting
	// over a tree nothing can see, which reads as a pass in most of them.
	found, err := daemon.Discoverable()
	if err != nil {
		t.Fatalf("read the fixture back: %v", err)
	}
	for _, f := range found {
		if f.ID == id {
			return
		}
	}
	t.Fatalf("discovery does not see the transcript this test just wrote at %s: the slug rule here and the one "+
		"in internal/daemon have diverged, so nothing below is testing what it says it is", dir)
}

const (
	importA = "a1111111-1111-4111-8111-111111111111"
	importB = "a2222222-2222-4222-8222-222222222222"
)

// Bare `wake import` is the picker, and it answers with **no daemon anywhere**.
//
// The socket handed in does not exist and nothing starts one. That is the
// property rather than a convenience: this is the verb somebody runs on the
// first day, before there is a fleet, and a picker that needed a daemon would
// ask them to build the thing they came to populate.
func TestBareImportListsWithoutStartingAnything(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, importA, real)

	socket := filepath.Join(t.TempDir(), "nothing.sock")
	var out bytes.Buffer
	if err := run([]string{"import"}, &out); err != nil {
		t.Fatalf("wake import: %v", err)
	}
	if _, err := os.Stat(socket); err == nil {
		t.Error("`wake import` created a socket: listing what is on disk must dial nothing and start nothing")
	}
	got := out.String()
	if !strings.Contains(got, shortID(importA)) {
		t.Errorf("the listing does not name session %s:\n%s", shortID(importA), got)
	}
	if !strings.Contains(got, real) {
		t.Errorf("the listing does not say where session %s ran:\n%s", shortID(importA), got)
	}
}

// A session with no provable directory is **listed** and its row says why.
//
// Both halves matter. Hiding it would lose a conversation that exists; offering
// it with a blank column would make the refusal arrive only after somebody
// chose.
func TestTheListingShowsASessionWithNoProvableDirectoryAndSaysWhy(t *testing.T) {
	projects := projectsTree(t)
	started := t.TempDir()
	worktree := filepath.Join(started, "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	transcript(t, projects, started, importB, worktree)

	var out bytes.Buffer
	if err := printImportable(&out); err != nil {
		t.Fatalf("printImportable: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, shortID(importB)) {
		t.Fatalf("a session with no provable directory was left out of the listing entirely:\n%s", got)
	}
	if !strings.Contains(got, "no directory can be proven") {
		t.Errorf("the row for %s does not say why it has no directory:\n%s", shortID(importB), got)
	}
}

// The listing says, in its own text, the thing 2026-08-09 findings §5 says this
// flow owes: a session somebody still has open must be closed there first.
//
// It is on the listing rather than only on the refusal because the refusal
// arrives after a choice, and this is the surface where the choice is made.
func TestTheListingSaysToCloseAnOpenSessionFirst(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, importA, real)

	var out bytes.Buffer
	if err := printImportable(&out); err != nil {
		t.Fatalf("printImportable: %v", err)
	}
	if !strings.Contains(out.String(), "close it there first") {
		t.Errorf("the picker never says that a session still open in a terminal cannot be imported. "+
			"Wake cannot detect that state (2026-08-12 findings §5), so this sentence is the whole of the "+
			"warning anybody gets:\n%s", out.String())
	}
}

// An empty machine is an empty answer rather than an error.
func TestTheListingOnAMachineWithNoSessionsSaysSo(t *testing.T) {
	projectsTree(t)
	var out bytes.Buffer
	if err := printImportable(&out); err != nil {
		t.Fatalf("printImportable: %v", err)
	}
	if !strings.Contains(out.String(), "no claude sessions") {
		t.Errorf("an empty machine got %q", out.String())
	}
}

// Exact id, unique prefix, ambiguous prefix, no match - matchSession's four
// answers over a different corpus, and the ambiguous one names the candidates
// rather than picking.
func TestImportResolvesAnIdTheWayTheRestOfThisCommandDoes(t *testing.T) {
	projects := projectsTree(t)
	real := t.TempDir()
	transcript(t, projects, real, importA, real)
	transcript(t, projects, real, importB, real)

	t.Run("exact", func(t *testing.T) {
		got, err := importTarget(importA)
		if err != nil || got.ID != importA {
			t.Fatalf("importTarget(%s) = %+v, %v", importA, got, err)
		}
	})
	t.Run("unique prefix", func(t *testing.T) {
		got, err := importTarget("a1111")
		if err != nil || got.ID != importA {
			t.Fatalf("importTarget(a1111) = %+v, %v", got, err)
		}
	})
	t.Run("ambiguous prefix names the candidates", func(t *testing.T) {
		_, err := importTarget("a")
		if err == nil {
			t.Fatal("a prefix matching two sessions resolved to one of them: this command refuses rather than guessing")
		}
		for _, want := range []string{shortID(importA), shortID(importB)} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal is %q and does not name candidate %s", err, want)
			}
		}
	})
	t.Run("no match", func(t *testing.T) {
		_, err := importTarget("zzzz")
		if err == nil {
			t.Fatal("a prefix matching nothing resolved to something")
		}
		if !strings.Contains(err.Error(), "wake import") {
			t.Errorf("the refusal is %q and does not say how to see what there is", err)
		}
	})
}

// The sentence an import opens with, and both of its claims.
//
// The second is the one nothing else in this build says: Wake cannot tell
// whether the original is still open, so the operator is told that the two
// carry on separately. A sentence that only said "this is a snapshot" would be
// a fork's sentence, and an import is not only a fork - its source may have a
// person typing into it right now.
func TestTheImportSentenceSaysItIsACopyAndThatTheOriginalIsUntouched(t *testing.T) {
	got := importedNotice("sydney", importA)
	for _, want := range []string{"@sydney", "copy", shortID(importA), "does not write to the original", "neither sees the other"} {
		if !strings.Contains(got, want) {
			t.Errorf("the import notice is %q and does not contain %q", got, want)
		}
	}
	// **The qualifier, asserted rather than assumed.** 2026-08-10 findings §5
	// measured an *idle* parent and §12 declines the mid-turn case, which is
	// the import's central scenario. An unqualified "the original is untouched"
	// is this project designing around unrecorded behaviour - and it was pinned
	// by this test, so the assertion had a guard defending it.
	if !strings.Contains(got, "measured on an idle session") {
		t.Errorf("the import notice is %q and states the no-write claim without the condition it was measured "+
			"under. The import's central case is a session somebody is using right now - the mid-turn case - "+
			"which 2026-08-10 findings §12 explicitly does not cover and which the import path cannot guard, "+
			"because Wake holds no state about a stranger's session", got)
	}
	if strings.Contains(got, "\n") {
		t.Errorf("the import notice is %q and contains a newline: notice.Report draws one row, and lipgloss "+
			"truncates each line while preserving the line count, so a second line makes the frame a row too tall", got)
	}
}

// `wake import` takes at most one id, and **zero is the listing rather than an
// arity error** - the one verb here for which "you did not say which" has a
// better answer than a refusal.
func TestImportArity(t *testing.T) {
	projectsTree(t)
	var out bytes.Buffer
	if err := run([]string{"import"}, &out); err != nil {
		t.Errorf("`wake import` with no argument is the picker, and it errored: %v", err)
	}
	if err := run([]string{"import", "one", "two"}, &out); err == nil {
		t.Error("`wake import one two` was accepted")
	}
}

// **M2: a refusal the disk already decided must not fork a daemon.**
//
// This verb's own rule is that the id is resolved against the disk before
// anything is dialled, because connect() calls EnsureRunning and would answer
// "there is no such transcript" by starting a daemon that was never going to
// know. The no-directory refusal is equally disk-decidable - importTarget
// already returned a FoundSession whose Dir is "" - and it is 97 of 428 rows on
// the recording machine, so roughly a quarter of every pick was spending that
// property.
func TestImportingASessionWithNoProvableDirectoryStartsNoDaemon(t *testing.T) {
	projects := projectsTree(t)
	started := t.TempDir()
	worktree := filepath.Join(started, "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	transcript(t, projects, started, importB, worktree)

	// tempSocket rather than t.TempDir: a unix socket path is a fixed-size
	// kernel field, and SocketPath refuses an over-long one before dispatch
	// ever runs - so the refusal under test would never be reached.
	socket := tempSocket(t)
	t.Setenv(daemon.SocketEnv, socket)

	err := run([]string{"import", importB}, io.Discard)
	if err == nil {
		t.Fatal("an import with no provable directory was accepted by the client")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("the refusal is %q and does not say the problem is the directory", err)
	}
	if _, statErr := os.Stat(socket); statErr == nil {
		t.Error("the refusal forked a daemon. This one is decidable from the disk, and `wake import` dials " +
			"nothing precisely so that somebody running it before they have a fleet does not acquire one by asking")
	}
}
