package daemon

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Discovery remembers what it read from each transcript, so a second open reads
// only what changed. These tests count the transcripts opened rather than time
// the walk: a count does not flake on a loaded machine, and it is the thing the
// cache exists to change.

// readCounter counts the transcripts discovery opens for the rest of the test.
func readCounter(t *testing.T) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	prev := openTranscript
	openTranscript = func(path string) (io.ReadCloser, error) {
		n.Add(1)
		return prev(path)
	}
	t.Cleanup(func() { openTranscript = prev })
	return &n
}

// size is how many transcripts the cache holds.
func (c *scanCache) size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.entries)
}

// reset forgets everything, so a benchmark can start cold.
func (c *scanCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

// aTree is three sessions with a real directory each.
type aTree struct {
	projects string
	dirs     [3]string
	ids      [3]string
	paths    [3]string
}

func newTree(t *testing.T) aTree {
	t.Helper()
	tree := aTree{
		projects: t.TempDir(),
		ids: [3]string{
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
			"33333333-3333-4333-8333-333333333333",
		},
	}
	for i := range tree.dirs {
		tree.dirs[i] = t.TempDir()
		tree.paths[i] = writeTranscript(t, tree.projects, slugOf(tree.dirs[i]), tree.ids[i], tree.dirs[i])
	}
	return tree
}

func mustDiscover(t *testing.T, projects string) []FoundSession {
	t.Helper()
	rows, err := discover(projects)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return rows
}

func TestASecondDiscoveryOpensNoTranscriptThatHasNotChanged(t *testing.T) {
	tree := newTree(t)
	opened := readCounter(t)

	first := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 3 {
		t.Fatalf("the first discovery opened %d transcripts, want 3", got)
	}
	second := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 3 {
		t.Errorf("the second discovery opened %d more transcripts, want none: nothing changed", got-3)
	}
	want, err := discoverDecoded(tree.projects)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Errorf("the rows are not the decoding walk's:\nfirst  %+v\nsecond %+v\nwant   %+v", first, second, want)
	}
}

func TestAGrownTranscriptIsReadAgainAndTheRestAreNot(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	opened := readCounter(t)

	f, err := os.OpenFile(tree.paths[1], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"last-prompt","lastPrompt":"what now"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	rows := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 1 {
		t.Errorf("discovery opened %d transcripts after one grew, want 1", got)
	}
	if got := found(t, rows, tree.ids[1]).Preview; got != "what now" {
		t.Errorf("the grown transcript's preview is %q, want the new prompt", got)
	}
}

func TestATranscriptWhoseModifiedTimeMovedIsReadAgain(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	opened := readCounter(t)

	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(tree.paths[0], later, later); err != nil {
		t.Fatal(err)
	}
	mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 1 {
		t.Errorf("discovery opened %d transcripts after one was touched, want 1", got)
	}
}

func TestATranscriptThatChangedSizeAtTheSameModifiedTimeIsReadAgain(t *testing.T) {
	tree := newTree(t)
	before, err := os.Stat(tree.paths[2])
	if err != nil {
		t.Fatal(err)
	}
	mustDiscover(t, tree.projects)
	opened := readCounter(t)

	body := `{"cwd":"` + tree.dirs[2] + `","customTitle":"renamed"}` + "\n"
	if err := os.WriteFile(tree.paths[2], []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tree.paths[2], before.ModTime(), before.ModTime()); err != nil {
		t.Fatal(err)
	}
	rows := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 1 {
		t.Errorf("discovery opened %d transcripts, want 1: the size moved though the time did not", got)
	}
	if got := found(t, rows, tree.ids[2]).Name; got != "renamed" {
		t.Errorf("the name is %q, want the rewritten one", got)
	}
}

// What is remembered is what the file said, never what the disk said of the
// directories it names: those are asked again on every open.
func TestADirectoryDeletedSinceTheLastOpenIsStillRefusedOnAWarmOpen(t *testing.T) {
	tree := newTree(t)
	if got := found(t, mustDiscover(t, tree.projects), tree.ids[0]).Dir; got != tree.dirs[0] {
		t.Fatalf("the first open proved %q, want %q", got, tree.dirs[0])
	}
	opened := readCounter(t)

	if err := os.RemoveAll(tree.dirs[0]); err != nil {
		t.Fatal(err)
	}
	rows := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 0 {
		t.Errorf("a warm open read %d transcripts, want none", got)
	}
	if got := found(t, rows, tree.ids[0]).Dir; got != "" {
		t.Errorf("a warm open still proves %q after the directory was deleted", got)
	}
}

func TestADirectoryCreatedSinceTheLastOpenIsProvedOnAWarmOpen(t *testing.T) {
	projects := t.TempDir()
	later := filepath.Join(t.TempDir(), "later")
	id := "44444444-4444-4444-8444-444444444444"
	writeTranscript(t, projects, slugOf(later), id, later)
	if got := found(t, mustDiscover(t, projects), id).Dir; got != "" {
		t.Fatalf("a directory that does not exist was proved as %q", got)
	}
	opened := readCounter(t)

	if err := os.Mkdir(later, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := found(t, mustDiscover(t, projects), id).Dir; got != later {
		t.Errorf("the directory made since is proved as %q, want %q", got, later)
	}
	if got := opened.Load(); got != 0 {
		t.Errorf("a warm open read %d transcripts, want none", got)
	}
}

func TestAReadThatFailsIsNotRemembered(t *testing.T) {
	tree := newTree(t)
	prev := openTranscript
	openTranscript = func(path string) (io.ReadCloser, error) {
		if path == tree.paths[1] {
			return nil, errors.New("the disk went away")
		}
		return prev(path)
	}
	failed := mustDiscover(t, tree.projects)
	openTranscript = prev
	if got := found(t, failed, tree.ids[1]).Dir; got != "" {
		t.Fatalf("a transcript that would not open proved %q", got)
	}

	opened := readCounter(t)
	rows := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 1 {
		t.Errorf("discovery opened %d transcripts, want only the one whose read had failed", got)
	}
	if got := found(t, rows, tree.ids[1]).Dir; got != tree.dirs[1] {
		t.Errorf("the transcript is proved as %q once it reads, want %q", got, tree.dirs[1])
	}
}

// The file's size and time are taken before it is read, so a line written the
// moment a read ends is never mistaken for one the read saw.
func TestALineWrittenAsAReadEndsIsPickedUpByTheNextOpen(t *testing.T) {
	tree := newTree(t)
	prev := openTranscript
	openTranscript = func(path string) (io.ReadCloser, error) {
		r, err := prev(path)
		if err != nil || path != tree.paths[0] {
			return r, err
		}
		return closeHook{r, func() {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = f.Close() }()
			_, _ = f.WriteString(`{"type":"last-prompt","lastPrompt":"after the read"}` + "\n")
		}}, nil
	}
	mustDiscover(t, tree.projects)
	openTranscript = prev

	rows := mustDiscover(t, tree.projects)
	if got := found(t, rows, tree.ids[0]).Preview; got != "after the read" {
		t.Errorf("the preview is %q: a line written as the read ended was never read, and the file was remembered as whole", got)
	}
}

type closeHook struct {
	io.ReadCloser
	after func()
}

func (c closeHook) Close() error {
	err := c.ReadCloser.Close()
	c.after()
	return err
}

func TestATranscriptThatLeftTheListingLeavesTheCache(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	body, err := os.ReadFile(tree.paths[1])
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(tree.paths[1])
	if err := os.Remove(tree.paths[1]); err != nil {
		t.Fatal(err)
	}
	mustDiscover(t, tree.projects)

	// Put back byte for byte, time included: only a forgotten entry reads again.
	if err := os.WriteFile(tree.paths[1], body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tree.paths[1], info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	opened := readCounter(t)
	mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 1 {
		t.Errorf("discovery opened %d transcripts, want the 1 that had left the listing: the cache kept it", got)
	}
}

func TestDiscoveringOneTreeDoesNotForgetAnother(t *testing.T) {
	a, b := newTree(t), newTree(t)
	mustDiscover(t, a.projects)
	mustDiscover(t, b.projects)
	opened := readCounter(t)

	mustDiscover(t, a.projects)
	mustDiscover(t, b.projects)
	if got := opened.Load(); got != 0 {
		t.Errorf("discovery opened %d transcripts, want none: each tree was already remembered", got)
	}
}

// A session whose name and directories outweigh the budget is read on every open,
// as it was before the cache, and holds nothing in it.
func TestASessionOverTheBudgetIsReadEveryTimeAndNotKept(t *testing.T) {
	tree := newTree(t)
	body := `{"cwd":"` + tree.dirs[1] + `","customTitle":"` + strings.Repeat("n", entryBudget+1) + `"}` + "\n"
	if err := os.WriteFile(tree.paths[1], []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustDiscover(t, tree.projects)
	opened := readCounter(t)
	rows := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 1 {
		t.Errorf("discovery opened %d transcripts, want only the over-budget one", got)
	}
	if got := len(found(t, rows, tree.ids[1]).Name); got != entryBudget+1 {
		t.Errorf("the long name came back %d bytes long, want %d: it is still the whole name", got, entryBudget+1)
	}
}

// The budget is held through a real read: a prompt of any length is cut to
// previewBytes runes by the reader, so a session with a huge prompt is still
// kept, and nothing the cache keeps outweighs entryBudget.
func TestNoSessionTheCacheKeepsOutweighsTheBudget(t *testing.T) {
	tree := newTree(t)
	huge := strings.Repeat("😀", 20_000)
	body := `{"cwd":"` + tree.dirs[1] + `","customTitle":"named","lastPrompt":"` + huge + `"}` + "\n"
	if err := os.WriteFile(tree.paths[1], []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionScans.reset()
	mustDiscover(t, tree.projects)
	opened := readCounter(t)
	mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 0 {
		t.Errorf("a warm open read %d transcripts: a session with a huge prompt was not kept, so its preview was not bounded", got)
	}
	sessionScans.mu.Lock()
	defer sessionScans.mu.Unlock()
	for path, e := range sessionScans.entries {
		if got := retainedBytes(e.read); got > entryBudget {
			t.Errorf("%s is kept at %d bytes, over the %d budget", path, got, entryBudget)
		}
	}
}

func TestTheCacheHoldsNoMoreSessionsThanTheListing(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	if got := sessionScans.size(); got < 3 {
		t.Fatalf("the cache holds %d sessions after reading 3", got)
	}
	before := sessionScans.size()
	if err := os.Remove(tree.paths[0]); err != nil {
		t.Fatal(err)
	}
	mustDiscover(t, tree.projects)
	if got := sessionScans.size(); got != before-1 {
		t.Errorf("the cache holds %d sessions after one left, want %d", got, before-1)
	}
}

func TestDiscoveriesRunningTogetherAgree(t *testing.T) {
	tree := newTree(t)
	want, err := discoverDecoded(tree.projects)
	if err != nil {
		t.Fatal(err)
	}
	sessionScans.reset()
	var wg sync.WaitGroup
	results := make([][]FoundSession, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], _ = discover(tree.projects)
		}()
	}
	wg.Wait()
	for i, got := range results {
		if !reflect.DeepEqual(got, want) {
			t.Errorf("concurrent discovery %d gave %+v, want %+v", i, got, want)
		}
	}
}

// A transcript can name hundreds of directories (every subagent and worktree it
// ran in); only those that slug to where it was found can ever prove it, so only
// those are held, and a session that names many is still kept.
func TestASessionThatNamesManyDirectoriesIsKeptForTheOnesThatCouldProveIt(t *testing.T) {
	tree := newTree(t)
	var b strings.Builder
	for i := 0; i < 400; i++ {
		b.WriteString(`{"cwd":"/elsewhere/worktree-` + strings.Repeat("w", 20) + string(rune('a'+i%26)) + string(rune('a'+i/26%26)) + `"}` + "\n")
	}
	b.WriteString(`{"cwd":"` + tree.dirs[1] + `"}` + "\n")
	if err := os.WriteFile(tree.paths[1], []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	mustDiscover(t, tree.projects)
	opened := readCounter(t)
	rows := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 0 {
		t.Errorf("a warm open read %d transcripts, want none: the session names directories that could never prove it, and they are not held", got)
	}
	if got := found(t, rows, tree.ids[1]).Dir; got != tree.dirs[1] {
		t.Errorf("the session is proved as %q, want %q", got, tree.dirs[1])
	}
}

// A transcript put back by a restore or a copy that keeps dates is a new file of
// the same size and time. It is not the file that was read.
func TestAFileReplacedWithTheSameSizeAndTimeIsReadAgain(t *testing.T) {
	tree := newTree(t)
	body := []byte(`{"cwd":"` + tree.dirs[0] + `","customTitle":"AAAA"}` + "\n")
	if err := os.WriteFile(tree.paths[0], body, 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tree.paths[0])
	if err != nil {
		t.Fatal(err)
	}
	mustDiscover(t, tree.projects)
	opened := readCounter(t)

	replacement := tree.paths[0] + ".new"
	if err := os.WriteFile(replacement, []byte(strings.Replace(string(body), "AAAA", "BBBB", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(replacement, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, tree.paths[0]); err != nil {
		t.Fatal(err)
	}
	rows := mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 1 {
		t.Errorf("discovery opened %d transcripts, want the replaced one", got)
	}
	if got := found(t, rows, tree.ids[0]).Name; got != "BBBB" {
		t.Errorf("the name is %q, want the replacement's", got)
	}
}

// A projects root of "." leaves no separator to match a path on, so what leaves
// the listing must still leave the cache.
func TestTheCacheForgetsATranscriptUnderARelativeRootToo(t *testing.T) {
	tree := newTree(t)
	t.Chdir(tree.projects)
	if _, err := discover("."); err != nil {
		t.Fatal(err)
	}
	before := sessionScans.size()
	entries, _ := os.ReadDir(".")
	var gone string
	for _, e := range entries {
		if e.IsDir() {
			gone = filepath.Join(e.Name(), tree.ids[0]+".jsonl")
			if _, err := os.Stat(gone); err == nil {
				break
			}
		}
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := discover("."); err != nil {
		t.Fatal(err)
	}
	if got := sessionScans.size(); got != before-1 {
		t.Errorf("the cache holds %d sessions after one left a relative root, want %d", got, before-1)
	}
}
