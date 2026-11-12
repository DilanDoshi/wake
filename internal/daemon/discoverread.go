package daemon

import (
	"context"
	"os"
	"sort"
	"sync"
)

// statted is a transcript with the stat its row and its cache entry are keyed
// on. row is the listing's row: what the stat alone says.
type statted struct {
	job  transcriptFile
	info os.FileInfo
	row  FoundSession
}

// statSessions stats the transcripts, fanned across discoverWorkers, and returns
// the regular ones newest first: a file that is not a regular one is no session.
func statSessions(jobs []transcriptFile) []statted {
	got := make([]statted, len(jobs))
	ok := make([]bool, len(jobs))
	forEach(context.Background(), len(jobs), func(i int) {
		if info, isReg := regularTranscript(jobs[i].path); isReg {
			j := jobs[i]
			got[i] = statted{job: j, info: info, row: FoundSession{
				ID: j.id, Slug: j.slug, Path: j.path, Modified: info.ModTime(),
			}}
			ok[i] = true
		}
	})
	files := make([]statted, 0, len(jobs))
	for i := range got {
		if ok[i] {
			files = append(files, got[i])
		}
	}
	sort.Slice(files, func(i, j int) bool { return newestFirst(files[i].row, files[j].row) })
	return files
}

// readSessions reads each transcript in order, fanned across discoverWorkers,
// and hands its row and index to emit, which may run on any goroutine. It stops
// starting reads once ctx is done; those begun finish.
func readSessions(ctx context.Context, files []statted, emit func(i int, row FoundSession)) {
	forEach(ctx, len(files), func(i int) { emit(i, foundSession(files[i].job, files[i].info)) })
}

// forEach runs fn(0..n-1) on at most discoverWorkers goroutines, starting them in
// order, and returns when every one it started is done. Hundreds of open files
// at once buy nothing past saturating the disk and the cores.
func forEach(ctx context.Context, n int, fn func(i int)) {
	sem := make(chan struct{}, discoverWorkers)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		sem <- struct{}{}
		if ctx.Err() != nil {
			<-sem
			break
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}

// foundSession is one transcript's row: what reading it found, from the cache
// while the file is as it was, and the directory proved against the disk now.
func foundSession(j transcriptFile, info os.FileInfo) FoundSession {
	read := sessionRead(j, info)
	return FoundSession{
		ID:       j.id,
		Dir:      verifiedDir(j.slug, read.cwds),
		Slug:     j.slug,
		Path:     j.path,
		Modified: info.ModTime(),
		Preview:  read.preview,
		Title:    read.title,
		Name:     read.recorded,
	}
}

// newestFirst is the order every listing keeps: newest first, then by id, then by
// path. It is a total order (a path is unique), so a sort over the whole machine
// and one over a session's copies agree on which copy comes first.
func newestFirst(a, b FoundSession) bool {
	if !a.Modified.Equal(b.Modified) {
		return a.Modified.After(b.Modified)
	}
	if a.ID != b.ID {
		return a.ID < b.ID
	}
	return a.Path < b.Path
}
