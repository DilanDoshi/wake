package daemon

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

// touchOlder sets a file's modified time an hour back.
func touchOlder(t *testing.T, path string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

// Confirming a pick names one session, so it reads that session's transcript and
// no other: the picker has already walked the machine once, and the daemon
// walking it again before the agent appears was a second wait after the first.

func TestAResumeOpensOnlyTheTranscriptItNames(t *testing.T) {
	s, projects := importServer(t)
	var ids [4]string
	for i := range ids {
		ids[i] = strings.Repeat(string(rune('a'+i)), 8) + "-1111-4111-8111-111111111111"
		dir := t.TempDir()
		writeTranscript(t, projects, slugOf(dir), ids[i], dir)
	}
	opened := readCounter(t)

	src, err := s.resumeSource(ids[2])
	if err != nil {
		t.Fatalf("resumeSource: %v", err)
	}
	if src.ID != ids[2] || src.Dir == "" {
		t.Errorf("the resume source is %+v, want session %s with its proven directory", src, ids[2])
	}
	if got := opened.Load(); got != 1 {
		t.Errorf("confirming one session opened %d transcripts, want only its own", got)
	}
}

func TestAnImportOpensOnlyTheTranscriptItNames(t *testing.T) {
	s, projects := importServer(t)
	var ids [4]string
	for i := range ids {
		ids[i] = strings.Repeat(string(rune('a'+i)), 8) + "-2222-4222-8222-222222222222"
		dir := t.TempDir()
		writeTranscript(t, projects, slugOf(dir), ids[i], dir)
	}
	opened := readCounter(t)

	src, err := s.importSource(ids[1])
	if err != nil {
		t.Fatalf("importSource: %v", err)
	}
	if src.ID != ids[1] || src.Dir == "" {
		t.Errorf("the import source is %+v, want session %s with its proven directory", src, ids[1])
	}
	if got := opened.Load(); got != 1 {
		t.Errorf("confirming one session opened %d transcripts, want only its own", got)
	}
}

// What the one-session lookup answers is what the listing's would have: the same
// row, the newest where one id sits under two slugs, and the same two ways of
// finding nothing.
func TestTheSessionAPickNamesIsTheRowTheListingGives(t *testing.T) {
	tree := newTree(t)
	dup := t.TempDir()
	// The same session under a second slug, older: the newer row must win.
	other := writeTranscript(t, tree.projects, slugOf(dup), tree.ids[0], tree.dirs[0])
	touchOlder(t, other)

	want, err := discoverDecoded(tree.projects)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range append(tree.ids[:], "99999999-9999-4999-8999-999999999999") {
		var expect *FoundSession
		for i := range want {
			if want[i].ID == id {
				expect = &want[i]
				break
			}
		}
		got, ok, err := discoverID(tree.projects, id)
		if err != nil {
			t.Fatalf("discoverID(%s): %v", id, err)
		}
		switch {
		case expect == nil && ok:
			t.Errorf("discoverID(%s) found %+v, the listing has no such session", id, got)
		case expect != nil && (!ok || !reflect.DeepEqual(*expect, got)):
			t.Errorf("discoverID(%s) = %+v (found %t), the listing's row is %+v", id, got, ok, *expect)
		}
	}
}

func TestTheOneSessionLookupFailsWhereTheListingFails(t *testing.T) {
	if _, _, err := discoverID("", "99999999-9999-4999-8999-999999999999"); err == nil {
		t.Error("a lookup with no projects directory to look in gave no error")
	}
	if _, ok, err := discoverID(t.TempDir()+"/missing", "99999999-9999-4999-8999-999999999999"); ok || err != nil {
		t.Errorf("a machine that never ran claude gave found=%t err=%v, want neither", ok, err)
	}
}

// A file is the session its name says, spelled as it is spelled: a filesystem
// that folds case must not turn an upper-case transcript into a match for a
// lower-case id.
func TestAPickIsTheSessionWhoseFileNameSpellsItExactly(t *testing.T) {
	projects := t.TempDir()
	dir := t.TempDir()
	upper := "AAAAAAAA-1111-4111-8111-111111111111"
	writeTranscript(t, projects, slugOf(dir), upper, dir)
	if _, ok, _ := discoverID(projects, strings.ToLower(upper)); ok {
		t.Errorf("a lower-case id matched the transcript named %s", upper)
	}
	if _, ok, _ := discoverID(projects, upper); !ok {
		t.Errorf("the id %s did not match its own transcript", upper)
	}
}

// Looking one up is not a listing, so it must not tell the cache which
// transcripts are gone: the rest are still there.
func TestLookingOneSessionUpLeavesTheRestRemembered(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	if _, ok, err := discoverID(tree.projects, tree.ids[1]); !ok || err != nil {
		t.Fatalf("discoverID: found=%t err=%v", ok, err)
	}
	opened := readCounter(t)
	mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 0 {
		t.Errorf("a listing after one lookup opened %d transcripts, want none: the lookup forgot them", got)
	}
}

func TestALookupAfterAListingReadsNothing(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	opened := readCounter(t)
	if _, ok, _ := discoverID(tree.projects, tree.ids[2]); !ok {
		t.Fatal("not found")
	}
	if got := opened.Load(); got != 0 {
		t.Errorf("a lookup of a remembered session opened %d transcripts, want none", got)
	}
}

// Two copies of one session with the same modified time are the same row to a
// comparison that stops at the id, and an unstable sort over the whole machine
// puts them in a different order than a sort over the two of them. The listing's
// order ends on the path, so a pick resumes the copy the picker showed.
func TestAPickIsTheCopyTheListingShowedWhenTwoCopiesShareATimestamp(t *testing.T) {
	for others := 10; others <= 40; others++ {
		projects := t.TempDir()
		same := time.Now().Add(-time.Hour)
		put := func(id string) {
			dir := t.TempDir()
			p := writeTranscript(t, projects, slugOf(dir), id, dir)
			if err := os.Chtimes(p, same, same); err != nil {
				t.Fatal(err)
			}
		}
		const twice = "55555555-5555-4555-8555-555555555555"
		put(twice)
		put(twice)
		for i := 0; i < others; i++ {
			put(fmt.Sprintf("%08x-6666-4666-8666-666666666666", i+1))
		}

		rows, err := discover(projects)
		if err != nil {
			t.Fatal(err)
		}
		var first FoundSession
		for _, r := range rows {
			if r.ID == twice {
				first = r
				break
			}
		}
		got, ok, err := discoverID(projects, twice)
		if err != nil || !ok {
			t.Fatalf("discoverID: found=%t err=%v", ok, err)
		}
		if got.Path != first.Path {
			t.Fatalf("with %d other sessions the listing shows the copy under %s and a pick resumes the one under %s",
				others, first.Slug, got.Slug)
		}
	}
}

// A lookup has the whole listing in hand, so what it finds gone is gone from the
// cache too: the daemon only ever looks one up, and would otherwise keep an entry
// for every session it was ever asked about.
func TestALookupForgetsTheTranscriptsThatLeftTheListing(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	before := sessionScans.size()
	if err := os.Remove(tree.paths[0]); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := discoverID(tree.projects, tree.ids[1]); !ok {
		t.Fatal("not found")
	}
	if got := sessionScans.size(); got != before-1 {
		t.Errorf("the cache holds %d sessions after a lookup found one gone, want %d", got, before-1)
	}
}

// Where one id sits under two projects only the newer copy is the row, so only it
// is read: the other can be gigabytes the operator would wait on for nothing.
func TestALookupOfASessionInTwoProjectsReadsOnlyTheNewerCopy(t *testing.T) {
	tree := newTree(t)
	older := writeTranscript(t, tree.projects, slugOf(t.TempDir()), tree.ids[0], tree.dirs[0])
	touchOlder(t, older)
	sessionScans.reset()
	opened := readCounter(t)

	got, ok, err := discoverID(tree.projects, tree.ids[0])
	if err != nil || !ok {
		t.Fatalf("discoverID: found=%t err=%v", ok, err)
	}
	if got.Path != tree.paths[0] {
		t.Errorf("the lookup returned %s, want the newer copy %s", got.Path, tree.paths[0])
	}
	if n := opened.Load(); n != 1 {
		t.Errorf("the lookup opened %d transcripts, want only the newer copy", n)
	}
}
