package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

// The picker opens on what the directory listing alone can say - which sessions
// exist, newest first, how old - and each session's content arrives as its
// transcript is read. These tests hold the stream to discover: the same
// sessions in the same order, and the same rows once they have all arrived.

// drain collects a stream's rows until it closes, failing the test if it does
// not close in time.
func drain(t *testing.T, filled <-chan FoundSession) []FoundSession {
	t.Helper()
	var rows []FoundSession
	timeout := time.After(10 * time.Second)
	for {
		select {
		case row, ok := <-filled:
			if !ok {
				return rows
			}
			rows = append(rows, row)
		case <-timeout:
			t.Fatal("the stream did not close")
		}
	}
}

func byPath(rows []FoundSession) []FoundSession {
	out := append([]FoundSession(nil), rows...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func TestTheListingTheStreamOpensOnIsDiscoverysOrderWithNoContentRead(t *testing.T) {
	tree := newTree(t)
	// A second session older than the rest, and one whose newest time ties.
	dup := t.TempDir()
	older := writeTranscript(t, tree.projects, slugOf(dup), "44444444-4444-4444-8444-444444444444", dup)
	touchOlder(t, older)
	opened := readCounter(t)

	want := mustDiscover(t, tree.projects)
	sessionScans.reset()
	opened.Store(0)
	listed, filled, err := discoverStream(context.Background(), tree.projects)
	if err != nil {
		t.Fatal(err)
	}
	drain(t, filled)

	if len(listed) != len(want) {
		t.Fatalf("the listing has %d sessions, discovery has %d", len(listed), len(want))
	}
	for i, row := range listed {
		w := want[i]
		if row.ID != w.ID || row.Path != w.Path || row.Slug != w.Slug || !row.Modified.Equal(w.Modified) {
			t.Errorf("listing row %d is %s (%s), discovery's is %s (%s)", i, row.ID, row.Modified, w.ID, w.Modified)
		}
		if row.Dir != "" || row.Preview != "" || row.Title != "" || row.Name != "" {
			t.Errorf("listing row %d carries content (%+v) though no transcript was read to make it", i, row)
		}
	}
	if got := opened.Load(); got != int64(len(want)) {
		t.Errorf("the stream read %d transcripts for %d sessions", got, len(want))
	}
}

func TestEverySessionTheStreamFillsIsTheRowDiscoveryGives(t *testing.T) {
	tree := newTree(t)
	sessionScans.reset()
	want := mustDiscover(t, tree.projects)
	sessionScans.reset()
	_, filled, err := discoverStream(context.Background(), tree.projects)
	if err != nil {
		t.Fatal(err)
	}
	got := drain(t, filled)
	if !reflect.DeepEqual(byPath(got), byPath(want)) {
		t.Errorf("the stream filled\n%+v\ndiscovery gives\n%+v", byPath(got), byPath(want))
	}
}

// The point of the listing: it is back before any transcript has been read.
func TestTheListingIsBackBeforeAnyTranscriptIsRead(t *testing.T) {
	tree := newTree(t)
	sessionScans.reset()
	release := make(chan struct{})
	prev := openTranscript
	openTranscript = func(path string) (io.ReadCloser, error) {
		<-release
		return prev(path)
	}
	t.Cleanup(func() { openTranscript = prev })

	type result struct {
		listed []FoundSession
		filled <-chan FoundSession
	}
	done := make(chan result, 1)
	go func() {
		listed, filled, _ := discoverStream(context.Background(), tree.projects)
		done <- result{listed, filled}
	}()
	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the listing waited on a transcript read")
	}
	if len(r.listed) != 3 {
		t.Fatalf("the listing has %d sessions, want 3", len(r.listed))
	}
	select {
	case row := <-r.filled:
		t.Fatalf("a row (%s) was filled while every read was held", row.ID)
	default:
	}
	close(release)
	if got := drain(t, r.filled); len(got) != 3 {
		t.Errorf("%d rows filled once the reads ran, want 3", len(got))
	}
}

func TestACancelledStreamStartsNoRead(t *testing.T) {
	tree := newTree(t)
	sessionScans.reset()
	opened := readCounter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	listed, filled, err := discoverStream(ctx, tree.projects)
	if err != nil || len(listed) != 3 {
		t.Fatalf("a cancelled stream still lists: %d sessions, err %v", len(listed), err)
	}
	if got := drain(t, filled); len(got) != 0 {
		t.Errorf("a cancelled stream filled %d rows", len(got))
	}
	if got := opened.Load(); got != 0 {
		t.Errorf("a cancelled stream read %d transcripts, want none", got)
	}
}

// What a cancelled stream never reached stays remembered: the listing, which is
// whole, says what is gone, not the reads.
func TestACancelledStreamDoesNotForgetTheTranscriptsItNeverReached(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, filled, _ := discoverStream(ctx, tree.projects)
	drain(t, filled)
	opened := readCounter(t)
	mustDiscover(t, tree.projects)
	if got := opened.Load(); got != 0 {
		t.Errorf("a discovery after a cancelled stream opened %d transcripts: the stream forgot them", got)
	}
}

func TestAStreamForgetsTheTranscriptsThatLeftTheListing(t *testing.T) {
	tree := newTree(t)
	mustDiscover(t, tree.projects)
	before := sessionScans.size()
	if err := os.Remove(tree.paths[0]); err != nil {
		t.Fatal(err)
	}
	_, filled, _ := discoverStream(context.Background(), tree.projects)
	drain(t, filled)
	if got := sessionScans.size(); got != before-1 {
		t.Errorf("the cache holds %d sessions after one left, want %d", got, before-1)
	}
}

func TestAStreamFailsWhereDiscoveryFails(t *testing.T) {
	if _, _, err := discoverStream(context.Background(), ""); err == nil {
		t.Error("a stream with no projects directory to look in gave no error")
	}
	listed, filled, err := discoverStream(context.Background(), t.TempDir()+"/missing")
	if err != nil || len(listed) != 0 {
		t.Errorf("a machine that never ran claude gave %d sessions, err %v", len(listed), err)
	}
	if got := drain(t, filled); len(got) != 0 {
		t.Errorf("a machine with nothing on it filled %d rows", len(got))
	}
}

// A stream owns goroutines, and a finished or cancelled one must give them back.
func TestAStreamLeavesNoGoroutineBehind(t *testing.T) {
	tree := newTree(t)
	sessionScans.reset()
	before := runtime.NumGoroutine()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(cancelIt bool) {
			defer wg.Done()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if cancelIt {
				cancel()
			}
			_, filled, err := discoverStream(ctx, tree.projects)
			if err != nil {
				t.Error(err)
				return
			}
			for range filled {
			}
		}(i%2 == 0)
	}
	wg.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("%d goroutines before the streams and %d after", before, after)
	}
}

func TestAStreamOfAReadThatFailsStillFillsTheRow(t *testing.T) {
	tree := newTree(t)
	sessionScans.reset()
	prev := openTranscript
	openTranscript = func(path string) (io.ReadCloser, error) {
		if path == tree.paths[1] {
			return nil, errors.New("the disk went away")
		}
		return prev(path)
	}
	t.Cleanup(func() { openTranscript = prev })
	_, filled, _ := discoverStream(context.Background(), tree.projects)
	got := drain(t, filled)
	if len(got) != 3 {
		t.Fatalf("%d rows filled, want 3: a transcript that would not open is still a row, as in discovery", len(got))
	}
}

// A client that walks away without reading a single row must not strand the
// readers: the channel holds every row, so they finish and the goroutines go.
func TestAStreamNobodyReadsStillFinishes(t *testing.T) {
	tree := newTree(t)
	sessionScans.reset()
	before := runtime.NumGoroutine()
	_, _, err := discoverStream(context.Background(), tree.projects)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("%d goroutines before the stream and %d after, with nobody reading it", before, after)
	}
}

// The picker draws the newest sessions first, so they are the first read: with
// every read held, the ones begun are exactly the newest discoverWorkers.
func TestTheNewestSessionsAreReadFirst(t *testing.T) {
	projects := t.TempDir()
	total := discoverWorkers + 4
	newest := map[string]bool{}
	for i := 0; i < total; i++ {
		dir := t.TempDir()
		id := fmt.Sprintf("%08x-5555-4555-8555-555555555555", i+1)
		p := writeTranscript(t, projects, slugOf(dir), id, dir)
		when := time.Now().Add(-time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, when, when); err != nil {
			t.Fatal(err)
		}
		if i < discoverWorkers {
			newest[p] = true
		}
	}
	sessionScans.reset()

	var mu sync.Mutex
	var begun []string
	release := make(chan struct{})
	prev := openTranscript
	openTranscript = func(path string) (io.ReadCloser, error) {
		mu.Lock()
		begun = append(begun, path)
		mu.Unlock()
		<-release
		return prev(path)
	}
	t.Cleanup(func() { openTranscript = prev })

	_, filled, err := discoverStream(context.Background(), projects)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(begun)
		mu.Unlock()
		if n >= discoverWorkers {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	got := append([]string(nil), begun...)
	mu.Unlock()
	close(release)
	drain(t, filled)
	for _, p := range got {
		if !newest[p] {
			t.Errorf("a read of %s began among the first %d, and it is not one of the newest", p, discoverWorkers)
		}
	}
}
