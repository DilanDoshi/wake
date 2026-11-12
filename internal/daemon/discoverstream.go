package daemon

import (
	"context"
	"os"
	"sort"
	"sync"
)

// DiscoverStream is discovery for a picker that must open at once: every session
// on this machine from the directory listing alone - id, project, age, newest
// first, none of it read from a transcript - and a channel on which each
// session's row arrives complete as its transcript is read.
//
// The listing is the order discover gives and the rows are discover's rows, so a
// picker opened on the first and filled from the second shows what discover
// would have after the same wait, and in the order it would have. The channel is
// closed once every session has arrived, or once ctx is cancelled and the reads
// already begun have finished; it never blocks the readers, so a client that
// walks away leaks nothing. Newest sessions are read first, since they are what
// the picker draws first.
func DiscoverStream(ctx context.Context) ([]FoundSession, <-chan FoundSession, error) {
	return discoverStream(ctx, ProjectsDir())
}

// statted is a transcript with the stat its row and its cache entry are keyed on.
type statted struct {
	job  transcriptFile
	info os.FileInfo
	row  FoundSession // the listing's row: what the stat alone says
}

func discoverStream(ctx context.Context, projects string) ([]FoundSession, <-chan FoundSession, error) {
	jobs, err := listTranscripts(projects)
	if err != nil {
		return nil, nil, err
	}
	files := make([]statted, 0, len(jobs))
	for _, j := range jobs {
		if info, ok := regularTranscript(j.path); ok {
			files = append(files, statted{job: j, info: info, row: FoundSession{
				ID: j.id, Slug: j.slug, Path: j.path, Modified: info.ModTime(),
			}})
		}
	}
	sort.Slice(files, func(i, j int) bool { return newestFirst(files[i].row, files[j].row) })

	listed := make([]FoundSession, len(files))
	live := make(map[string]struct{}, len(files))
	for i, f := range files {
		listed[i] = f.row
		live[f.row.Path] = struct{}{}
	}
	// The listing is whole, so it can say what is gone however much is read.
	sessionScans.retain(projects, live)

	filled := make(chan FoundSession, len(files))
	go readSessions(ctx, files, filled)
	return listed, filled, nil
}

// readSessions reads each transcript in order, fanned across discoverWorkers,
// and sends its row. out holds every row, so a send never waits on a reader.
func readSessions(ctx context.Context, files []statted, out chan<- FoundSession) {
	defer close(out)
	sem := make(chan struct{}, discoverWorkers)
	var wg sync.WaitGroup
	for _, f := range files {
		sem <- struct{}{}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(f statted) {
			defer wg.Done()
			defer func() { <-sem }()
			out <- foundSession(f.job, f.info)
		}(f)
	}
	wg.Wait()
}
