package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// What reading a transcript found is remembered, so opening /resume a second
// time reads only the transcripts that changed since the first.
//
// A transcript is append-only and claude writes it whole-line at a time, so
// (path, size, mtime) says whether anything was written: a file's entry is used
// while all three are as they were, and replaced the first time they are not.
// That is per change, never per open, and nothing here runs on a timer, polls,
// or reads a transcript before somebody asks for the listing.
//
// **It remembers what the file said, never what the disk said of it.** The
// directories a transcript names are asked again on every open (verifiedDir):
// a project deleted since the last open must be refused, one made since must be
// proved, and neither is a fact about the transcript.
//
// **The size and time are taken before the read** (the stat discover already
// made), so a line written while - or just after - a read is a change to the
// next open rather than a stamp that claims the read saw it.
//
// The cache lives in the process that asks, which is the `wake` window for the
// picker and `/adopt` and the daemon for a confirmed pick: a daemon-held copy
// would help neither the picker, which does not dial, nor the eighteen fleets
// that would each hold one.

// maxCachedText bounds the text a remembered session holds beyond its preview
// and title: its name as written and the directories its transcript names, none
// of which is bounded by claude. A session over it is read on every open, as it
// was before the cache, and holds nothing.
const maxCachedText = 1 << 10

// entryBudget is the most one remembered session retains: a preview and a title,
// each at most previewBytes runes of at most utf8.UTFMax bytes (oneLine's bound),
// and maxCachedText. The cache holds at most one entry per transcript in the
// listing, since every discovery drops those that left it, so its memory is the
// number of transcripts times this.
const entryBudget = 2*previewBytes*utf8.UTFMax + maxCachedText

// sessionScans is the process's memory of what each transcript held.
var sessionScans scanCache

type scanCache struct {
	mu      sync.Mutex
	entries map[string]cachedScan
}

type cachedScan struct {
	size  int64
	mtime time.Time
	read  transcriptRead
}

// heldText is the text of a session claude does not bound: its name as written
// and the directories its transcript names.
func heldText(r transcriptRead) int {
	n := len(r.recorded)
	for _, c := range r.cwds {
		n += len(c)
	}
	return n
}

// retainedBytes is the text a remembered session holds.
func retainedBytes(r transcriptRead) int { return heldText(r) + len(r.preview) + len(r.title) }

func overBudget(r transcriptRead) bool { return heldText(r) > maxCachedText }

// get is what was read of the file at path while it still has this size and
// time.
func (c *scanCache) get(path string, info os.FileInfo) (transcriptRead, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[path]
	if !ok || e.size != info.Size() || !e.mtime.Equal(info.ModTime()) {
		return transcriptRead{}, false
	}
	return e.read, true
}

// put remembers what was read of the file info describes, under the slug it was
// found in. Only the directories that could ever prove it are kept (the others
// cannot change verifiedDir's answer), and a session over its budget is not kept.
func (c *scanCache) put(path string, info os.FileInfo, slug string, r transcriptRead) {
	kept := r
	kept.cwds = nil
	for _, cwd := range r.cwds {
		if fitsSlug(slug, cwd) {
			kept.cwds = append(kept.cwds, cwd)
		}
	}
	if overBudget(kept) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cachedScan)
	}
	c.entries[path] = cachedScan{size: info.Size(), mtime: info.ModTime(), read: kept}
}

// retain forgets every remembered transcript under root that is not in live: a
// session whose file is gone leaves the cache with the listing. Another root's
// entries are not this listing's to judge.
func (c *scanCache) retain(root string, live map[string]struct{}) {
	prefix := filepath.Clean(root) + string(filepath.Separator)
	c.mu.Lock()
	defer c.mu.Unlock()
	for path := range c.entries {
		if _, ok := live[path]; !ok && strings.HasPrefix(path, prefix) {
			delete(c.entries, path)
		}
	}
}

// sessionRead is what reading one transcript found, from the cache while the
// file is as it was. A read that did not finish is returned but not remembered.
func sessionRead(j transcriptFile, info os.FileInfo) transcriptRead {
	if r, hit := sessionScans.get(j.path, info); hit {
		return r
	}
	r, err := readTranscript(j.path)
	if err == nil {
		sessionScans.put(j.path, info, j.slug, r)
	}
	return r
}
