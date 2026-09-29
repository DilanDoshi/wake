package ui

// `@` searches the project's files: one bounded `git ls-files` per menu
// opening, off the draw goroutine, ranked per keystroke. A bare `@`, a path
// ending in a separator or starting with `/`, `~` or `.` still steps through
// directories, and so does a directory git does not answer for.

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// errNotARepository is git's answer outside a repository.
var errNotARepository = errors.New("fatal: not a git repository")

// notARepository is git over a directory no repository holds, which is every
// t.TempDir. TestMain installs it, so only the test of the shipped git runs one.
func notARepository(string) ([]byte, int, error) { return nil, 0, errNotARepository }

// lsOutput is names as `git ls-files -z` prints them, each ended by a NUL.
func lsOutput(names ...string) []byte {
	var out []byte
	for _, name := range names {
		out = append(append(out, name...), 0)
	}
	return out
}

// answering is a git that prints names.
func answering(names ...string) func(string) ([]byte, int, error) {
	out := lsOutput(names...)
	return func(string) ([]byte, int, error) { return out, 0, nil }
}

// withGit installs fake as the git behind the index for one test, and counts
// how many times it runs.
func withGit(t *testing.T, fake func(string) ([]byte, int, error)) *atomic.Int32 {
	t.Helper()
	runs := new(atomic.Int32)
	prev := lsFiles
	lsFiles = func(dir string) ([]byte, int, error) {
		runs.Add(1)
		return fake(dir)
	}
	t.Cleanup(func() { lsFiles = prev })
	return runs
}

// indexed is names read the way git's answer is.
func indexed(names ...string) []indexedPath { return parseIndex("", lsOutput(names...), 0).files }

// convOver is a conversation with an agent that works in dir: only a
// conversation searches.
func convOver(t *testing.T, dir string) App {
	t.Helper()
	fresh(t)
	return dmApp(nil, Stream{}, "s1", "alex").withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	)
}

// roomOver is a room whose one agent works in dir.
func roomOver(t *testing.T, dir string) App {
	t.Helper()
	fresh(t)
	return newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	)
}

// nested writes files at paths relative to dir, making their directories.
func nested(t *testing.T, dir string, rels ...string) {
	t.Helper()
	for _, rel := range rels {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir for %s: %v", rel, err)
		}
		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
}

// The ranking, as a table: the name before the directories above it, then the
// shorter row as drawn, a file before a directory, then the lexical one - and
// nothing that does not spell the query. An index holds every directory above
// its files, ranked by its last segment and drawn with its separator.
func TestASearchRanksTheFileNameFirstThenTheShorterPath(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		files, want []string
	}{
		{"a file name beats a directory-only match, even a shorter one", "comp",
			[]string{"comp/a.go", "src/deep/comp.go"}, []string{"comp/", "src/deep/comp.go", "comp/a.go"}},
		{"a prefix beats a substring beats a subsequence, whatever their lengths", "comp",
			[]string{"c_o_m_p.go", "xcompx.go", "compzzzzzzzz.go"}, []string{"compzzzzzzzz.go", "xcompx.go", "c_o_m_p.go"}},
		{"within a tier the shorter path, then the lexical one", "comp",
			[]string{"a/comp.go", "a/b/comp.go", "b/comp.go"}, []string{"a/comp.go", "b/comp.go", "a/b/comp.go"}},
		{"a path that does not spell the query is not offered", "comp",
			[]string{"readme.md", "pmoc.go"}, nil},
		{"matching ignores case in both", "COMP",
			[]string{"src/COMPLETION.go"}, []string{"src/COMPLETION.go"}},
		{"a hidden path waits for a query that reaches into one", "ci",
			[]string{".github/ci.yml", "x/.ci.go", "ci.go"}, []string{"ci.go"}},
		{"a query that reaches into a hidden path finds it", "x/.c",
			[]string{"x/.ci.go", "x/ci.go"}, []string{"x/.ci.go"}},
		// Every directory above a file, once: inner holds no file of its own.
		// é is C3 A9, and Ã© is C3 83 C2 A9: its bytes spell é, its runes do not.
		{"a subsequence is of runes, not bytes", "é",
			[]string{"Ã©.go", "café.go"}, []string{"café.go"}},
		// ls-files -o prints an untracked nested repository with a separator.
		{"a name ending in a separator is a directory, once", "lib",
			[]string{"a.go", "vendor/lib/"}, []string{"vendor/lib/"}},
		{"a directory ranks by its last segment", "inn",
			[]string{"inner/deep/buried.md", "inner/deep/other.md"},
			[]string{"inner/", "inner/deep/", "inner/deep/other.md", "inner/deep/buried.md"}},
		{"a hidden directory waits as a hidden file does", "work",
			[]string{".github/workflows/ci.yml", "workspace/a.go"}, []string{"workspace/", "workspace/a.go"}},
		// A file finishes the mention where a directory is one more step, so at the
		// same tier and drawn length the file goes first, whatever the bytes say.
		{"a file before a directory at the same tier and drawn length", "abc",
			[]string{"abcd/x.go", "abcde"}, []string{"abcde", "abcd/", "abcd/x.go"}},
		// Split at the last separator: the tail tiers against the name, and the
		// head is spelt through the directories - a path that cannot spell it is
		// not offered, whatever its name.
		{"a query with a separator tiers its tail against the name", "ui/comp",
			[]string{"internal/ui/completionpathmenu.go", "internal/ui/x/compare/zz.go",
				"internal/ui/compat/readme.md", "src/lib/completion.go"},
			[]string{"internal/ui/compat/", "internal/ui/x/compare/", "internal/ui/completionpathmenu.go",
				"internal/ui/x/compare/zz.go", "internal/ui/compat/readme.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, total := rankPaths(indexed(tc.files...), tc.query, completionRows)
			if !slices.Equal(got, tc.want) {
				t.Errorf("%q over %q ranked %q, want %q", tc.query, tc.files, got, tc.want)
			}
			if total != len(tc.want) {
				t.Errorf("%q over %q counted %d matches, want %d", tc.query, tc.files, total, len(tc.want))
			}
		})
	}
}

// Only the best k are kept, whatever order git printed them in, and every match
// is counted - which is what the menu's `more` says.
func TestASearchKeepsTheBestAndCountsEveryMatch(t *testing.T) {
	var files []string
	for i := 39; i >= 0; i-- {
		files = append(files, fmt.Sprintf("d%02d/comp%02d.go", i, i))
	}
	got, total := rankPaths(indexed(files...), "comp", 5)
	want := []string{"d00/comp00.go", "d01/comp01.go", "d02/comp02.go", "d03/comp03.go", "d04/comp04.go"}
	if !slices.Equal(got, want) || total != len(files) {
		t.Errorf("the best 5 of %d are %q of %d, want %q of %d", len(files), got, total, want, len(files))
	}
}

// Typed text searches the index; a path steps through directories. Each draft
// names an offer only its own mode makes, and one only the other mode would.
func TestTypedTextSearchesTheIndexAndAPathStepsThroughDirectories(t *testing.T) {
	dir := workdir(t, "top.md", "tmpfile.md", "compose.md", ".env", "~notes.md")
	nested(t, dir, "internal/inner.md", "internal/ignored.log", "ui/compact.md")
	// v1.2/tmx.go, x.envy/a.go, ~nox.go and p..q/00z.go spell the lead drafts
	// below, so a search would offer them where the listing does not.
	withGit(t, answering("top.md", "tmpfile.md", "internal/inner.md", "ui/completion.go", "zz/comp.go",
		"v1.2/tmx.go", "x.envy/a.go", "~nox.go", "p..q/00z.go"))
	sep := string(os.PathSeparator)
	for _, tc := range []struct{ draft, want, never string }{
		{"@", "@internal" + sep, "@ui/completion.go"},
		{"@internal" + sep, "@internal/ignored.log", "@ui/completion.go"},
		{"@.." + sep, "@.." + sep + filepath.Base(dir) + sep, "@ui/completion.go"},
		{"@/tmp", "@/tmpfile.md", "@tmpfile.md"},
		{"@comp", "@zz/comp.go", "@compose.md"},
		{"@ui/comp", "@ui/completion.go", "@ui/compact.md"},
		// A lead with no trailing separator still steps.
		{"@./tm", "@./tmpfile.md", "@v1.2/tmx.go"},
		{"@.en", "@.env", "@x.envy/"},
		{"@~no", "@~notes.md", "@~nox.go"},
		{"@.." + sep + filepath.Base(dir)[:2], "@.." + sep + filepath.Base(dir) + sep, "@p..q/00z.go"},
	} {
		got := convOver(t, dir).withDraft(tc.draft).completion.offers
		if !slices.Contains(got, tc.want) || slices.Contains(got, tc.never) {
			t.Errorf("%q offered %q, want %q and never %q", tc.draft, got, tc.want, tc.never)
		}
	}
}

// A search reads no directory: its rows are the index's, and it is git failing
// that starts the listing a search falls back to.
func TestASearchReadsNoDirectory(t *testing.T) {
	dir := workdir(t)
	nested(t, dir, "ui/compact.md")
	withGit(t, answering("ui/completion.go"))
	a := convOver(t, dir).withDraft("@")
	a, _ = a.withComposer(a.composer().WithDraft("@ui/comp")).recompleted().scanning()
	if out := a.completion.paths.out; out != "" {
		t.Errorf("a search over an answered index is reading %q", out)
	}
}

// A search offers a directory too, ranked by its last segment, and ⇥ on one
// steps into it: the draft then ends in a separator, which lists the directory.
func TestASearchOffersADirectoryAndTabStepsIntoIt(t *testing.T) {
	sep := string(os.PathSeparator)
	dir := workdir(t, "top.md")
	nested(t, dir, "inner/buried.md", "inner/notes.log")
	withGit(t, answering("top.md", "inner/buried.md"))
	a := convOver(t, dir).withDraft("@inn")
	if got := a.completion.offers; len(got) == 0 || got[0] != "@inner"+sep {
		t.Fatalf("`@inn` offered %q, want the directory first", got)
	}
	next, _ := pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	next = next.scanned()
	if got := next.composer().Value(); got != "@inner"+sep {
		t.Fatalf("⇥ on the directory left the draft %q, want %q", got, "@inner"+sep)
	}
	// notes.log is on disk and not in git's answer, so only the listing offers it.
	if got := next.completion.offers; !slices.Contains(got, "@inner/notes.log") {
		t.Errorf("stepping into the directory offered %q, want its listing", got)
	}
}

// A rebuild that moved neither the draft nor the index - a fleet report - reuses
// the ranking rather than running it again over the whole index; a keystroke
// ranks again.
func TestAReportReusesTheRankingAndAKeystrokeRanksAgain(t *testing.T) {
	withGit(t, answering("src/completion.go", "src/compare.go"))
	dir := workdir(t)
	a := convOver(t, dir).withDraft("@comp")
	ranked := a.completion.paths.rank.rows
	if len(ranked) == 0 {
		t.Fatalf("`@comp` ranked nothing over its index, so this asserts nothing: %q", a.completion.offers)
	}
	for range 2 {
		a = a.withRoster(rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle})
		if got := a.completion.paths.rank.rows; len(got) == 0 || &got[0] != &ranked[0] {
			t.Fatalf("a fleet report ranked the index again: %q", got)
		}
	}
	a = a.withDraft("l")
	if got := a.completion.paths.rank.rows; len(got) == 0 || &got[0] == &ranked[0] {
		t.Errorf("`@compl` reused the ranking for `@comp`: %q", got)
	}
	if got := a.completion.offers; slices.Contains(got, "@src/compare.go") {
		t.Errorf("`@compl` offered %q, which it does not spell", got)
	}
	// A closed menu holds no rank, and so no index behind it.
	if a = a.withDraft(" "); a.completion.paths.rank.index != nil {
		t.Error("a closed menu still holds the rank of the index it searched")
	}
}

// A search git has nothing for falls back to the listing for that text: an
// index git answered empty, and a query whose directories git never indexed -
// build output, an ignored tree - stepped into and then narrowed.
func TestASearchGitHasNothingForListsInstead(t *testing.T) {
	t.Run("an empty index", func(t *testing.T) {
		withGit(t, answering())
		a := convOver(t, workdir(t, "readme.md", "rebase.md")).withDraft("@re")
		if got, want := a.completion.offers, []string{"@readme.md", "@rebase.md"}; !slices.Equal(got, want) {
			t.Errorf("`@re` over an empty index offered %q, want the listing's %q", got, want)
		}
	})
	t.Run("an unindexed directory", func(t *testing.T) {
		dir := workdir(t, "top.md")
		nested(t, dir, "src/a.go", "build/out.txt", "build/other.bin")
		// rebuild/foo.go spells `build/o`, so only the head rule lists here.
		withGit(t, answering("top.md", "src/a.go", "rebuild/foo.go"))
		a := convOver(t, dir).withDraft("@build/o")
		if got, want := a.completion.offers, []string{"@build/other.bin", "@build/out.txt"}; !slices.Equal(got, want) {
			t.Errorf("`@build/o` offered %q, want the listing's %q", got, want)
		}
	})
	// A head naming no directory at the root lists nothing, so the search stands.
	t.Run("a head that lists nothing", func(t *testing.T) {
		withGit(t, answering("internal/ui/completion.go"))
		a := convOver(t, workdir(t)).withDraft("@ui/comp")
		if got := a.completion.offers; !slices.Contains(got, "@internal/ui/completion.go") {
			t.Errorf("`@ui/comp` offered %q, want the search's nested file", got)
		}
	})
}

// Only a conversation searches. The room's `@` addresses the fleet, so it keeps
// the one-directory listing and runs no git at all.
func TestTheRoomListsAndRunsNoGit(t *testing.T) {
	runs := withGit(t, answering("zz/comp.go"))
	a := roomOver(t, workdir(t, "compose.md")).withDraft("@comp")
	if n := runs.Load(); n != 0 {
		t.Errorf("a room mention ran git %d times, want none", n)
	}
	if got := a.completion.offers; !slices.Contains(got, "@compose.md") || slices.Contains(got, "@zz/comp.go") {
		t.Errorf("the room's `@comp` offered %q, want the listing's @compose.md and no search", got)
	}
}

// Directories are bounded as entries and as bytes: derived ones at most
// indexMaxFiles of them and indexMaxBytes of path - a deep, narrow tree or one
// very long name must not multiply the index the draw goroutine ranks. They are
// not counted into `more`.
func TestDerivedDirectoriesAreBoundedInCountAndBytes(t *testing.T) {
	dirStats := func(idx fileIndex) (n, size int) {
		for _, f := range idx.files {
			if f.dir {
				n, size = n+1, size+len(f.path)
			}
		}
		return n, size
	}
	var wide []string
	for i := range 20_000 {
		wide = append(wide, fmt.Sprintf("p%05d/q/r/f", i)) // three directories each
	}
	idx := parseIndex("", lsOutput(wide...), 0)
	if n, _ := dirStats(idx); n != indexMaxFiles || idx.left != 0 {
		t.Errorf("60,000 directories derived %d with %d left, want the cap's %d and none counted", n, idx.left, indexMaxFiles)
	}
	deep := strings.Repeat("aaaaaaaaaa/", 3000) + "f" // 3000 directories, ~50MB of their paths
	done := make(chan fileIndex, 1)
	go func() { done <- parseIndex("", lsOutput(deep), 0) }()
	select {
	case idx := <-done:
		if n, size := dirStats(idx); size > indexMaxBytes || n == 3000 {
			t.Errorf("one deep name derived %d directories of %d bytes, want at most %d bytes", n, size, indexMaxBytes)
		}
	case <-time.After(bangTestLimit):
		t.Fatal("deriving one deep name's directories did not end")
	}
}

// Past the cap the index keeps the first names and counts the rest, and the
// menu's `more` says so beside what the rows left out.
func TestAnIndexPastTheCapKeepsTheFirstAndCountsTheRest(t *testing.T) {
	const over = 7
	names := make([]string, indexMaxFiles+over)
	for i := range names {
		names[i] = fmt.Sprintf("f%06d.go", i)
	}
	withGit(t, answering(names...))
	c := convOver(t, workdir(t)).withDraft("@f0").completion

	if idx := c.paths.index; len(idx.files) != indexMaxFiles || idx.left != over {
		t.Fatalf("an index of %d names kept %d and counted %d, want %d and %d",
			len(names), len(idx.files), idx.left, indexMaxFiles, over)
	}
	if want := indexMaxFiles + over - len(c.offers); c.more != want {
		t.Errorf("the menu says %d more, want %d: every indexed match it did not draw and the %d never indexed",
			c.more, want, over)
	}
}

// A git that does not answer - no repository, or a failure - gives today's
// listing, exactly, and is not run again on the next keystroke.
func TestAGitThatDoesNotAnswerFallsBackToTheListingOnce(t *testing.T) {
	for name, fake := range map[string]func(string) ([]byte, int, error){
		"no repository": notARepository,
		// What a failing git printed is no answer, even where it would match.
		"a failing git": func(string) ([]byte, int, error) { return lsOutput("reghost.md"), 0, errors.New("exit status 1") },
	} {
		t.Run(name, func(t *testing.T) {
			runs := withGit(t, fake)
			a := convOver(t, workdir(t, "readme.md", "rebase.md")).withDraft("@re")
			if got, want := a.completion.offers, []string{"@readme.md", "@rebase.md"}; !slices.Equal(got, want) {
				t.Errorf("`@re` offered %q, want the listing's %q", got, want)
			}
			a = a.withDraft("a")
			if got, want := a.completion.offers, []string{"@readme.md"}; !slices.Equal(got, want) {
				t.Errorf("`@rea` offered %q, want the listing's %q", got, want)
			}
			if n := runs.Load(); n != 1 {
				t.Errorf("git ran %d times over one opening, want once: a failure is recorded on the menu", n)
			}
		})
	}
}

// A slow git never holds the keys. Until it answers, a search offers the names
// and no paths - not even the listing that has landed - and a later keystroke
// starts no second git beside it.
func TestASlowGitNeverHoldsTheKeysAndRunsOncePerOpening(t *testing.T) {
	release, started := make(chan struct{}), make(chan struct{}, 4)
	runs := withGit(t, func(string) ([]byte, int, error) {
		started <- struct{}{}
		<-release
		return lsOutput("jar.md"), 0, nil
	})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free) // a failure below must not park the git it started
	dir := workdir(t, "jar.md")
	a := peerFleet(t, dir)

	type typedOut struct {
		a    App
		cmds []tea.Cmd
	}
	typed := make(chan typedOut, 1)
	go func() {
		var m tea.Model = a
		var cmds []tea.Cmd
		for _, k := range runes("@ja") {
			var cmd tea.Cmd
			m, cmd = m.Update(k)
			cmds = append(cmds, cmd)
		}
		typed <- typedOut{m.(App), cmds}
	}()
	var held typedOut
	select {
	case held = <-typed:
	case <-time.After(bangTestLimit):
		t.Fatal("typing `@ja` over a git that has not answered did not return: the keys waited for it")
	}
	if n := runs.Load(); n != 0 {
		t.Fatalf("Update ran git %d times itself: it belongs to a tea.Cmd", n)
	}
	next, _ := held.a.Update(scanPaths(dir)())
	if got, want := next.(App).completion.offers, []string{"@jane"}; !slices.Equal(got, want) {
		t.Errorf("`@ja` before git answered offered %q, want the names alone %q", got, want)
	}

	var wg sync.WaitGroup
	for _, cmd := range held.cmds {
		wg.Go(func() { drainBatch(cmd) })
	}
	select {
	case <-started:
	case <-time.After(bangTestLimit):
		t.Fatal("no git started from the keystrokes' commands")
	}
	free()
	wg.Wait()
	if n := runs.Load(); n != 1 {
		t.Errorf("three keystrokes into one opening ran git %d times, want once", n)
	}
}

// An index is held while its menu stays open and forgotten when it closes, so
// the next opening asks git again - and an answer that lands after its menu
// closed is not held for the next one.
func TestAnOpeningRunsGitOnceAndAClosedMenuForgetsIt(t *testing.T) {
	runs := withGit(t, answering("readme.md", "src/reader.go"))
	a := convOver(t, workdir(t)).withDraft("@r").withDraft("ea")
	if got := a.completion.offers; !slices.Contains(got, "@readme.md") || !slices.Contains(got, "@src/reader.go") {
		t.Errorf("`@rea` offered %q, want both indexed files", got)
	}
	if n := runs.Load(); n != 1 {
		t.Fatalf("one opening ran git %d times, want once", n)
	}
	_ = a.withDraft(" @re")
	if n := runs.Load(); n != 2 {
		t.Errorf("a menu closed and opened again ran git %d times in all, want twice", n)
	}

	// Typed without answering, so the menu closes before its git lands.
	var m tea.Model = convOver(t, workdir(t))
	for _, k := range runes("@r ") {
		m, _ = m.Update(k)
	}
	closed := m.(App)
	late, _ := closed.Update(indexPaths(closed.completion.paths.indexing)())
	before := runs.Load()
	_ = late.(App).withDraft("@r")
	if n := runs.Load() - before; n != 1 {
		t.Errorf("the next opening ran git %d times, want once: an answer that outlived its menu was held for it", n)
	}
}

// A git's answer nothing is waiting on is dropped, as a directory read's is.
func TestAnIndexNothingWaitsOnIsDropped(t *testing.T) {
	withGit(t, answering("readme.md"))
	dir := workdir(t)
	a := convOver(t, dir).withDraft("@re")
	late := fileIndex{dir: dir, files: indexed("late-report.md")}
	next, _ := a.Update(pathScanMsg{dir: dir, index: &late})
	if got := next.(App).completion.offers; slices.Contains(got, "@late-report.md") {
		t.Errorf("a git answer nobody asked for landed in the menu: %q", got)
	}
}

// A name is external bytes (BUG-9), a row is one line and one mention, and a
// merge's stages print a name once each - so git's answer is contained, holds
// no word break but the space, and is deduped.
func TestAnIndexedNameIsContainedOneLineAndOnce(t *testing.T) {
	idx := parseIndex("", lsOutput("a\x1b[2Jb.go", "two\nlines.go", "tab\tbed.go", "dup.go", "dup.go", "has space.md"), 0)
	got := make([]string, 0, len(idx.files))
	for _, f := range idx.files {
		got = append(got, f.path)
	}
	if want := []string{"a [2Jb.go", "dup.go", "has space.md"}; !slices.Equal(got, want) {
		t.Errorf("git's answer read as %q, want %q", got, want)
	}
}

// The byte cap cuts through a name, and that tail is counted, never offered.
func TestANameTheByteCapCutIsCountedNotOffered(t *testing.T) {
	idx := parseIndex("", []byte("a.go\x00b.g"), 1)
	if len(idx.files) != 1 || idx.files[0].path != "a.go" || idx.left != 1 {
		t.Errorf("a cut answer read as %+v, want a.go alone and one left out", idx)
	}
}

// A lister that floods is cut at the byte cap without a deadlock: the writer
// claims every write, so the child is drained to its end rather than blocked on
// a full pipe until the deadline, and memory stops at the cap.
func TestAFloodingListerIsCutAtTheByteCapWithoutADeadlock(t *testing.T) {
	const name = "abcdefg" // with its NUL, eight bytes
	flood := 3 * indexMaxBytes
	type result struct {
		out     []byte
		dropped int
		err     error
	}
	done := make(chan result, 1)
	go func() {
		script := fmt.Sprintf("yes %s | tr '\\n' '\\000' | head -c %d", name, flood)
		out, dropped, err := runCapped(time.Minute, nil, "/bin/sh", "-c", script)
		done <- result{out, dropped, err}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("the flood failed: %v", r.err)
		}
		if len(r.out) != indexMaxBytes {
			t.Errorf("kept %d bytes of %d, want the cap's %d", len(r.out), flood, indexMaxBytes)
		}
		if want := (flood - indexMaxBytes) / (len(name) + 1); r.dropped != want {
			t.Errorf("counted %d names dropped, want %d", r.dropped, want)
		}
	case <-time.After(bangTestLimit):
		t.Fatalf("a %d-byte flood did not end within %v: the capped writer stopped draining the child", flood, bangTestLimit)
	}
}

// Past its deadline the lister is killed with everything it started: a group
// kill ends at once, where a kill of one pid waits out the WaitDelay on a pipe
// the child it left still holds.
func TestAListerPastItsDeadlineIsKilledWithWhatItStarted(t *testing.T) {
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, _, err := runCapped(100*time.Millisecond, nil, "/bin/sh", "-c", "sleep 30 & wait")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a lister killed at its deadline reported success")
		}
		if elapsed := time.Since(start); elapsed >= bangWaitDelay {
			t.Errorf("a 100ms deadline took %v: only the shell was killed, and the wait ended at the WaitDelay", elapsed)
		}
	case <-time.After(bangTestLimit):
		t.Fatalf("a lister past its deadline was still running after %v", bangTestLimit)
	}
}

// A lister that exits while something it left holds its output is let go at the
// WaitDelay, and that is a failure: the answer may be incomplete.
func TestAListerThatLeavesItsOutputHeldIsLetGo(t *testing.T) {
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, _, err := runCapped(time.Minute, nil, "/bin/sh", "-c", "(sleep 5 &) ; exit 0")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, exec.ErrWaitDelay) {
			t.Errorf("err = %v, want exec.ErrWaitDelay", err)
		}
		if elapsed := time.Since(start); elapsed >= 2*bangWaitDelay {
			t.Errorf("took %v, want about the %v WaitDelay", elapsed, bangWaitDelay)
		}
	case <-time.After(bangTestLimit):
		t.Fatal("a lister whose output a background job held was waited on without a bound")
	}
}

// The shipped git, once, over a real repository: tracked and untracked files,
// never an ignored one, and a name with a space intact.
func TestTheShippedGitListsTrackedAndUntrackedFilesButNotIgnoredOnes(t *testing.T) {
	if _, err := exec.LookPath(gitBinary); err != nil {
		t.Skipf("no git on PATH: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := workdir(t, "tracked.go", "untracked.md", "has space.md", "ignored.log")
	nested(t, dir, "sub/nested.go")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.log\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", "tracked.go")
	withGit(t, shippedLsFiles)

	a := convOver(t, dir).withDraft("@nest")
	var got []string
	for _, f := range a.completion.paths.index.files {
		if !f.dir {
			got = append(got, f.path)
		}
	}
	slices.Sort(got)
	if want := []string{".gitignore", "has space.md", "sub/nested.go", "tracked.go", "untracked.md"}; !slices.Equal(got, want) {
		t.Errorf("git indexed %q, want %q", got, want)
	}
	if !slices.Contains(a.completion.offers, "@sub/nested.go") {
		t.Errorf("`@nest` offered %q, want the nested file", a.completion.offers)
	}
}

// Git's location variables in Wake's own environment - a hook, a wrapper - would
// point `-C dir` at another repository, so the shipped git runs without them.
// Each one set here reroutes ls-files on its own: the other repository's index,
// its work tree, or its info/exclude.
func TestTheShippedGitIndexesItsOwnDirectoryWhateverGitsVariablesSay(t *testing.T) {
	if _, err := exec.LookPath(gitBinary); err != nil {
		t.Skipf("no git on PATH: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	mine, other := workdir(t, "mine.go", "loose.md"), workdir(t, "decoy.go")
	gitIn(t, mine, "init", "-q")
	gitIn(t, mine, "add", "mine.go")
	gitIn(t, other, "init", "-q")
	gitIn(t, other, "add", "decoy.go")
	otherGit := filepath.Join(other, ".git")
	if err := os.WriteFile(filepath.Join(otherGit, "info", "exclude"), []byte("loose.md\n"), 0o600); err != nil {
		t.Fatalf("write the other repository's exclude: %v", err)
	}
	t.Setenv("GIT_DIR", otherGit)
	t.Setenv("GIT_WORK_TREE", other)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(otherGit, "index"))
	t.Setenv("GIT_COMMON_DIR", otherGit)

	out, dropped, err := shippedLsFiles(mine)
	if err != nil {
		t.Fatalf("the shipped git over %s failed: %v", mine, err)
	}
	var got []string
	for _, f := range parseIndex(mine, out, dropped).files {
		got = append(got, f.path)
	}
	slices.Sort(got)
	if want := []string{"loose.md", "mine.go"}; !slices.Equal(got, want) {
		t.Errorf("with git's variables naming another repository, the index of %s is %q, want %q", mine, got, want)
	}
}

// A repository's own config must not run code as the operator: an agent can
// write its .git/config, and core.fsmonitor names a program git runs on a
// read. The shipped git overrides it on its command line.
func TestTheShippedGitRunsNoProgramTheRepositoryNames(t *testing.T) {
	if _, err := exec.LookPath(gitBinary); err != nil {
		t.Skipf("no git on PATH: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := workdir(t, "tracked.go")
	marker := filepath.Join(t.TempDir(), "ran")
	hook := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700); err != nil {
		t.Fatalf("write the hook: %v", err)
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", "tracked.go")
	gitIn(t, dir, "config", "core.fsmonitor", hook)

	out, dropped, err := shippedLsFiles(dir)
	if err != nil {
		t.Fatalf("the shipped git failed: %v", err)
	}
	if got := parseIndex(dir, out, dropped).files; len(got) != 1 || got[0].path != "tracked.go" {
		t.Errorf("the index is %+v, want tracked.go", got)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("indexing ran the program the repository's core.fsmonitor names")
	}
}

// gitIn runs one git command in dir for a test's fixture.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command(gitBinary, append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// BenchmarkRankPathsAtTheBounds is a search over the worst index the bounds
// admit: full-length names at the byte cap's ratio (160 bytes a name, 50,000 of
// them), each with directories of its own, so derived directories hit their cap.
func BenchmarkRankPathsAtTheBounds(b *testing.B) {
	pad := strings.Repeat("b", 70)
	names := make([]string, indexMaxFiles)
	for i := range names {
		names[i] = fmt.Sprintf("d%05d/%s/%s/Completion%05d.go", i, strings.Repeat("a", 60), pad, i)
	}
	idx := parseIndex("", lsOutput(names...), 0)
	b.Logf("%d entries", len(idx.files))
	for _, q := range []string{"c", "comp", "ui/comp", "zzz"} {
		b.Run(q, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				rankPaths(idx.files, q, completionRows)
			}
		})
	}
}

// BenchmarkRankPaths is a search over a full index, on the goroutine that draws.
func BenchmarkRankPaths(b *testing.B) {
	words := []string{"completion", "Config", "server", "handler", "README", "main", "index"}
	names := make([]string, indexMaxFiles)
	for i := range names {
		names[i] = fmt.Sprintf("internal/pkg%02d/sub%03d/%s%05d.go", i%50, i%700, words[i%len(words)], i)
	}
	index := parseIndex("", lsOutput(names...), 0).files
	for _, q := range []string{"c", "comp", "ui/comp", "zzz"} {
		b.Run(q, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				rankPaths(index, q, completionRows)
			}
		})
	}
}
