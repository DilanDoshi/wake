package daemon

import (
	"os"
	"path/filepath"
	"sync"
	"unicode/utf8"
)

// sessionScans remembers what reading each transcript found, so a second open
// reads only what changed. An entry is used while the file is the same file with
// the same size and mtime - the stat is taken before the read, so a line written
// mid-read is a change to the next open - and holds what the file said, never the
// directory it names: that is re-proved on every open (verifiedDir). It lives in
// the process that asks (the window for the picker, the daemon for a pick).
var sessionScans scanCache

// entryBudget is the most one kept session retains: a preview and a title, each at
// most previewBytes runes of utf8.UTFMax bytes (oneLine's bound), plus
// maxCachedText for the name and directories claude does not bound. A session
// over it is not kept (read every open, as before). Every listing drops the
// entries that left it, so the cache holds at most one entry per transcript.
const (
	maxCachedText = 1 << 10
	entryBudget   = 2*previewBytes*utf8.UTFMax + maxCachedText
)

type scanCache struct {
	mu      sync.Mutex
	entries map[string]cachedScan
}

type cachedScan struct {
	file os.FileInfo // the stat the read was keyed on
	read transcriptRead
}

// retainedBytes is the text a kept session holds.
func retainedBytes(r transcriptRead) int {
	n := len(r.recorded) + len(r.preview) + len(r.title)
	for _, c := range r.cwds {
		n += len(c)
	}
	return n
}

// get is what was read of the file at path while it is still that file, at that
// size and time.
func (c *scanCache) get(path string, info os.FileInfo) (transcriptRead, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[path]
	if !ok || !os.SameFile(e.file, info) || e.file.Size() != info.Size() || !e.file.ModTime().Equal(info.ModTime()) {
		return transcriptRead{}, false
	}
	return e.read, true
}

// put keeps what was read of the file info describes, under the slug it was found
// in. Only the directories that could ever prove it are kept, and a session over
// its budget is not kept.
func (c *scanCache) put(path string, info os.FileInfo, slug string, r transcriptRead) {
	kept := r
	kept.cwds = nil
	for _, cwd := range r.cwds {
		if fitsSlug(slug, cwd) {
			kept.cwds = append(kept.cwds, cwd)
		}
	}
	if retainedBytes(kept) > entryBudget {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cachedScan)
	}
	c.entries[path] = cachedScan{file: info, read: kept}
}

// retain forgets every kept transcript under root that is not in the listing jobs
// came from. Another root's entries are not this listing's to judge; a path is
// root/slug/id.jsonl, so its root is two directories up.
func (c *scanCache) retain(root string, jobs []transcriptFile) {
	live := make(map[string]struct{}, len(jobs))
	for _, j := range jobs {
		live[j.path] = struct{}{}
	}
	root = filepath.Clean(root)
	c.mu.Lock()
	defer c.mu.Unlock()
	for path := range c.entries {
		if _, ok := live[path]; !ok && filepath.Dir(filepath.Dir(path)) == root {
			delete(c.entries, path)
		}
	}
}

// sessionRead is what reading one transcript found, from the cache while the
// file is as it was. A read that did not finish is returned but not kept.
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
