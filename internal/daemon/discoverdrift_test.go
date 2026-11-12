//go:build drift

// Discovery's scanner held to the decoder it replaced, over this machine's own
// transcripts: every claude version that wrote one, not the few the suite
// generates. `make drift` runs it. Not a gate - it reads data no other machine
// has - and it prints counts and a session id, never a line of a transcript.

package daemon

import (
	"os"
	"reflect"
	"sync"
	"testing"
)

func TestDriftDiscovery(t *testing.T) {
	projects := ProjectsDir()
	entries, err := os.ReadDir(projects)
	if err != nil {
		t.Skipf("no transcripts on this machine to hold the scanner to: %v", err)
	}
	jobs := transcriptFiles(projects, entries)

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		sem     = make(chan struct{}, discoverWorkers)
		rows    []FoundSession
		fieldsD []string
	)
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j transcriptFile) {
			defer wg.Done()
			defer func() { <-sem }()
			want := readTranscriptDecoded(j.path)
			got, _ := readTranscript(j.path)
			row, ok := foundDecoded(j)
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(want, got) {
				fieldsD = append(fieldsD, j.id)
			}
			if ok {
				rows = append(rows, row)
			}
		}(j)
	}
	wg.Wait()

	for _, id := range fieldsD {
		t.Errorf("session %s reads differently with the scanner than with the decoder", id)
	}
	t.Logf("%d of %d transcripts read identically by the scanner and the decoder", len(jobs)-len(fieldsD), len(jobs))

	newestFirstDecoded(rows)
	got, err := discover(projects)
	if err != nil {
		t.Fatal(err)
	}
	// A session being written between the two walks is a different file by the
	// second, and says nothing about the scanner: it is counted and set aside.
	// Keyed by path, since one id can sit under two project slugs.
	want := map[string]FoundSession{}
	for _, r := range rows {
		want[r.Path] = r
	}
	var moved int
	var gotSame, wantSame []string
	for _, g := range got {
		w, there := want[g.Path]
		switch {
		case !there || !w.Modified.Equal(g.Modified):
			moved++
		case !reflect.DeepEqual(w, g):
			t.Errorf("session %s: the scanner's row differs from the decoder's (dir %t preview %t title %t name %t)",
				g.ID, w.Dir == g.Dir, w.Preview == g.Preview, w.Title == g.Title, w.Name == g.Name)
		default:
			gotSame = append(gotSame, g.Path)
		}
	}
	for _, r := range rows {
		if g := lookup(got, r.Path); g != nil && g.Modified.Equal(r.Modified) {
			wantSame = append(wantSame, r.Path)
		}
	}
	if !reflect.DeepEqual(gotSame, wantSame) {
		t.Errorf("the rows are the same but not in the same order")
	}
	t.Logf("%d sessions, %d with a proven directory; %d rows equal and in order, %d written to mid-run and set aside",
		len(got), withDir(got), len(gotSame), moved)
}

func lookup(rows []FoundSession, path string) *FoundSession {
	for i := range rows {
		if rows[i].Path == path {
			return &rows[i]
		}
	}
	return nil
}

func withDir(rows []FoundSession) int {
	n := 0
	for _, r := range rows {
		if r.Dir != "" {
			n++
		}
	}
	return n
}
