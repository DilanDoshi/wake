// What a daemon that dies without parking anything still leaves its successor:
// one park book row per live agent, so the reaper can hand a crashed fleet to
// the park book instead of forgetting it (docs/notes/bugs.md BUG-53).
//
// It is a third file because the other two cannot hold it. sessions.json names
// a pgid and is the reaper's to clear (roster.go: nothing turns a roster record
// back into an agent), and parked.json is for sessions with no process
// (decisions.md, 2026-08-10: the park book is a second file, not a wider
// roster). This one carries a parkedRecord - so no pid, ever - for a session
// that still has a process, and nothing reads it except the reaper.
//
// **The roster stays the proof.** A record here is handed over only when the
// reaper finishes the roster row of the same session, so one that outlived its
// row - a session that ended or parked in the instant before a crash - is
// dropped, never offered back.

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// parkReadyName sits next to the socket, like the roster and the park book.
const parkReadyName = "parkready.json"

// parkReadyPerm is the park book's: it names every agent and where it ran.
const parkReadyPerm = 0o600

func parkReadyPath(socket string) string {
	return filepath.Join(filepath.Dir(socket), parkReadyName)
}

// readyRecord is one live session's row: the spawn id the roster names it by,
// and the park book record a wake of it would read. The record is flattened, so
// the file is the park book's own shape plus one key.
type readyRecord struct {
	Session string `json:"session"`
	parkedRecord
}

// sameWakeInputs reports whether two records ask a wake for the same thing. The
// timestamp is not an input, and is the one field that differs on every build.
func sameWakeInputs(a, b parkedRecord) bool {
	a.Parked, b.Parked = time.Time{}, time.Time{}
	return a == b
}

// parkReady is the file's in-memory twin, rewritten whole through a temp file
// and a rename like the roster and the park book beside it.
type parkReady struct {
	path string

	mu    sync.Mutex
	held  map[string]parkedRecord
	dirty bool // the last write failed, so an unchanged record still owes the disk
}

// newParkReady starts empty whatever is on disk: a starting daemon holds no live
// sessions, and the file a dead one left is read by the reaper alone.
func newParkReady(path string) *parkReady {
	return &parkReady{path: path, held: make(map[string]parkedRecord)}
}

// set records what a wake of session would need, writing only when that changed.
func (p *parkReady) set(session string, rec parkedRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if old, held := p.held[session]; held && !p.dirty && sameWakeInputs(old, rec) {
		return nil
	}
	p.held[session] = rec
	return p.writeLocked()
}

func (p *parkReady) remove(session string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, held := p.held[session]; !held {
		return nil
	}
	delete(p.held, session)
	return p.writeLocked()
}

// clear removes the file: the last thing an orderly ending writes, so a file
// found at startup always means the last daemon died.
func (p *parkReady) clear() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clearLocked()
}

func (p *parkReady) clearLocked() error {
	p.held, p.dirty = make(map[string]parkedRecord), false
	if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove park-ready file %s: %w", p.path, err)
	}
	return nil
}

// retain rewrites the file to exactly kept - the rows the reaper could not
// finish, which a later daemon must try again for. Like the roster's retain it
// runs at startup before this daemon spawns anything, and nothing keeps an empty
// set.
func (p *parkReady) retain(kept map[string]parkedRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(kept) == 0 {
		return p.clearLocked()
	}
	p.held = make(map[string]parkedRecord, len(kept))
	for session, rec := range kept {
		p.held[session] = rec
	}
	return p.writeLocked()
}

func (p *parkReady) writeLocked() error {
	rows := make([]readyRecord, 0, len(p.held))
	for session, rec := range p.held {
		rows = append(rows, readyRecord{Session: session, parkedRecord: rec})
	}
	data, err := json.Marshal(rows)
	if err == nil {
		err = writeFileAtomically(p.path, "park-ready file", data, parkReadyPerm)
	}
	p.dirty = err != nil
	if err != nil {
		return fmt.Errorf("write park-ready file: %w", err)
	}
	return nil
}

// loadParkReady reads what the last daemon left, keyed by the spawn id its
// roster row names. A missing file is the ordinary case, and an unreadable one
// is reported and read as empty, because the alternative is refusing to start.
func loadParkReady(path string) map[string]parkedRecord {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("wake: cannot read the park-ready file %s: %v", path, err)
		}
		return nil
	}
	var rows []readyRecord
	if err := json.Unmarshal(data, &rows); err != nil {
		logf("wake: park-ready file %s is unreadable, so a crashed fleet cannot be offered back from it: %v", path, err)
		return nil
	}
	out := make(map[string]parkedRecord, len(rows))
	for _, row := range rows {
		out[row.Session] = row.parkedRecord
	}
	return out
}

// runningRecord is an agent's park book row while its process still runs, and
// false once it has ended or parked, or between a /clear's reset and its
// successor's first frame, when the conversation it writes is not yet known.
func (a *agent) runningRecord() (parkedRecord, bool) {
	a.mu.Lock()
	running := !a.ended && !a.parked && !a.midClearLocked()
	a.mu.Unlock()
	if !running {
		return parkedRecord{}, false
	}
	return recordFor(a), true
}

// noteReady writes one agent's row down. A failure is not fatal to the session:
// it only means a crash would lose what the operator set on it.
func (s *server) noteReady(a *agent) {
	rec, running := a.runningRecord()
	if !running {
		return
	}
	if err := s.ready.set(a.id, rec); err != nil {
		logf("wake: session %s is not recorded for a crash, so its configuration would be lost with its daemon: %v", a.id, err)
	}
}

// syncReady offers every running agent's row to the file, which writes only
// what changed. It never removes: a row leaves with its roster row, and an
// agent absent from s.agents mid-shutdown is still running until it ends.
func (s *server) syncReady() {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()
	for _, a := range agents {
		if rec, running := a.runningRecord(); running {
			if err := s.ready.set(a.id, rec); err != nil {
				logf("wake: the park-ready file could not be updated, so a crash would lose what was set since: %v", err)
				return
			}
		}
	}
}

// forgetReady drops an agent's row beside its roster row.
func (s *server) forgetReady(id string) {
	if err := s.ready.remove(id); err != nil {
		logf("wake: session %s could not be removed from the park-ready file: %v", id, err)
	}
}
