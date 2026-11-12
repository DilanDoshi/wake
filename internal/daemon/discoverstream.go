package daemon

import "context"

// DiscoverStream is discovery for a picker that must open at once: every session
// on this machine from the directory listing alone - id, project, age, newest
// first, none of it read from a transcript - and a channel on which each
// session's row arrives complete as its transcript is read.
//
// The listing is the order discover gives and the rows are discover's rows, so a
// picker opened on the first and filled from the second shows what discover
// would have after the same wait. The channel is closed once every session has
// arrived, or once ctx is cancelled and the reads already begun have finished;
// it holds every row, so a client that walks away strands no reader. Newest
// sessions are read first, since they are what the picker draws first.
func DiscoverStream(ctx context.Context) ([]FoundSession, <-chan FoundSession, error) {
	return discoverStream(ctx, ProjectsDir())
}

func discoverStream(ctx context.Context, projects string) ([]FoundSession, <-chan FoundSession, error) {
	jobs, err := listTranscripts(projects)
	if err != nil {
		return nil, nil, err
	}
	files := statSessions(jobs)
	listed := make([]FoundSession, len(files))
	for i, f := range files {
		listed[i] = f.row
	}
	// The listing is whole, so it can say what is gone however much is read.
	sessionScans.retain(projects, jobs)

	filled := make(chan FoundSession, len(files))
	go func() {
		defer close(filled)
		readSessions(ctx, files, func(_ int, row FoundSession) { filled <- row })
	}()
	return listed, filled, nil
}
