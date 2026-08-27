// What the daemon writes down so a *later* daemon can clean up after it.
//
// Wake owns almost no state on purpose - claude persists every transcript
// itself - and this file is the exception the spec asks for by name: "the
// session UUIDs are on disk precisely so that is recoverable". Recoverable
// from what: a daemon that was SIGKILLed. Its agents lead their own process
// groups, so they survive it, and nothing else in the system holds a handle
// to them.
//
// It is not a roster in the product sense (names, groups, layout - that is
// registry.go's job later). It is the minimum needed to find a process again:
// which session, which group, and enough to prove the group is still that
// session's and not a recycled pid.

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// rosterFileName sits next to the socket, so a test with its own socket gets
// its own roster and never touches the real one.
const rosterFileName = "sessions.json"

// rosterPerm keeps the roster as private as the directory holding it: it
// names every agent on the machine and the groups they lead.
const rosterPerm = 0o600

func rosterPath(socket string) string {
	return filepath.Join(filepath.Dir(socket), rosterFileName)
}

// record is one live session as the next daemon will find it.
//
// Name and Label are display rather than identity, and they are here for one
// reader: `wake status` on a machine whose daemon died, which assembles its
// report from this file. Nothing is ever found by them - reaping matches the
// session UUID in a live process's argv, and that is what makes signalling a
// process group Wake did not spawn defensible.
//
// Neither is read back to decide *allocation*, and the reason is structural
// rather than a matter of timing: **nothing turns a record back into an agent.**
// loadRoster has exactly two consumers - reapOrphans, which signals what it can
// verify and clears the records it finished (retaining what it could not, for a
// later daemon), and FleetOnDisk, which assembles a report for a client - so a
// starting daemon holds no live sessions whatever this file says,
// and no name can be required to survive a restart. (reapOrphans is itself
// gated on the exclusive lock, so it does not always run; that changes which
// processes are killed, never whether this daemon holds any.)
//
// That is why adding a name pool did not change what this file means: Label is
// one more optional key on a shape that already carried a name, and a roster
// written by an older build decodes without it.
type record struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Label string `json:"label,omitempty"`

	// PID is the process group the agent leads, which is also its pid
	// because Wake makes each agent a group leader. Zero where the platform
	// has no process groups, and a zero is never signalled.
	PID int `json:"pid"`

	Started time.Time `json:"started"`
}

// rosterFile is the on-disk record, rewritten whole on every change.
//
// Whole rather than appended because it is tiny (30 sessions is under 4KB) and
// because a partial line in an append log is exactly the corruption that would
// hand a reaper a garbage pid. Rewriting through a temp file and a rename
// means a reader either sees the previous roster or the next one.
type rosterFile struct {
	path string

	mu   sync.Mutex
	live map[string]record
}

func newRosterFile(path string) *rosterFile {
	return &rosterFile{path: path, live: make(map[string]record)}
}

func (r *rosterFile) add(rec record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live[rec.ID] = rec
	return r.writeLocked()
}

func (r *rosterFile) remove(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, id)
	return r.writeLocked()
}

// clear removes the roster entirely. A daemon that exits having stopped its
// sessions leaves nothing behind to hunt for, and an entry that outlived its
// process sends the next reaper looking for a group that may since have been
// recycled.
//
// The file goes rather than being emptied, because "no roster" and "a roster
// of nothing" should not be two states that mean the same thing - loadRoster
// already treats a missing file as the ordinary case, and this is the last
// thing a daemon writes to disk, so its absence is a legible marker that one
// shut down properly.
func (r *rosterFile) clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.clearLocked()
}

func (r *rosterFile) clearLocked() error {
	r.live = make(map[string]record)
	if err := os.Remove(r.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove roster %s: %w", r.path, err)
	}
	return nil
}

// retain rewrites the roster to exactly recs - the records a reaper could not
// finish, which a later daemon must try again for. It runs at startup before
// this daemon spawns anything, so recs seed r.live and are re-persisted
// alongside its own agents rather than clobbered by the first add. An empty set
// removes the file, the same legible "nothing left to hunt" marker clear leaves.
func (r *rosterFile) retain(recs []record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(recs) == 0 {
		return r.clearLocked()
	}
	r.live = make(map[string]record, len(recs))
	for _, rec := range recs {
		r.live[rec.ID] = rec
	}
	return r.writeLocked()
}

func (r *rosterFile) writeLocked() error {
	recs := make([]record, 0, len(r.live))
	for _, rec := range r.live {
		recs = append(recs, rec)
	}
	data, err := json.Marshal(recs)
	if err != nil {
		return fmt.Errorf("encode roster: %w", err)
	}

	return writeFileAtomically(r.path, "roster", data, rosterPerm)
}

// loadRoster reads what the last daemon left. A missing file is the ordinary
// case - no daemon has run here - and an unreadable one is reported and
// treated as empty, because the alternative is refusing to start.
func loadRoster(path string) []record {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("wake: cannot read the session roster %s: %v", path, err)
		}
		return nil
	}
	var recs []record
	if err := json.Unmarshal(data, &recs); err != nil {
		logf("wake: session roster %s is unreadable, so anything it recorded is unreachable: %v", path, err)
		return nil
	}
	return recs
}
