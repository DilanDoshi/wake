// What the daemon writes down so a *later* daemon can offer a session back.
//
// # Why this is not roster.go, and why that file's argument still stands
//
// roster.go records live sessions for one reader - `wake status` on a machine
// whose daemon died - and its own header says why nothing there can become an
// agent again: loadRoster's two consumers are reapOrphans, which signals what
// it can verify and clears the records it finished, and FleetOnDisk, which
// assembles a report. That argument is about **live processes**, and it is
// untouched.
//
// A parked session is the opposite shape. It has no process, so:
//
//   - the reaper must never see it. A record in sessions.json names a pgid,
//     and a parked session's pgid is gone and may have been recycled - which
//     is the one thing reapOrphans is built never to signal on a guess.
//   - it carries no PID at all, and that absence is the type-level statement
//     of what a parked session is. record has one; this does not.
//   - it is read back **into s.agents**, which is precisely what roster.go
//     says nothing does. That is the contradiction, and it is resolved by
//     these being two files with two contracts rather than by widening one.
//
// It carries no ParentID either, and that is docs/notes/decisions.md's
// standing ruling rather than an omission: a persisted parent id outlives the
// parent's *name*, so a later daemon would report an edge to a session it
// holds nothing about - "a name is never an address" failing one level up. The
// durable copy exists and is not Wake's: a fork's transcript preserves the
// parent's per-message uuids across both recorded generations. A restored
// parked session therefore draws no `forked from` line, and that is honest.
//
// # What a record is allowed to assert, and what is re-proved
//
// Five fields, and only the first two are claims about the world. The id and
// the directory are what --resume needs to find a transcript; the name and the
// label are display; the timestamp is neither and is read by nobody yet.
//
// **A record asserts nothing about liveness.** It is written in completePark,
// which runs in retire - after core's Wait has returned - so the previous
// daemon did prove *its own* process was gone before writing this. That proof
// does not travel: a stray `claude --resume`, or a second daemon, can have
// started on the id in the meantime, and the file cannot know. So a restored
// row is a claim by a process that is no longer running, and unpark re-proves
// it through resumeSafe like any other wake. Nothing here shortens that path,
// and nothing may: two live processes on one id branch the transcript in place
// with no error on any wire.

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// parkBookName sits next to the socket, so a test with its own socket gets its
// own book and never touches the real one.
const parkBookName = "parked.json"

// parkBookPerm keeps it as private as the directory holding it: it names every
// session on the machine and the directories they ran in.
const parkBookPerm = 0o600

var errParkReservationHeld = errors.New("park record is already reserved by another wake")

func parkBookPath(socket string) string {
	return filepath.Join(filepath.Dir(socket), parkBookName)
}

// parkedRecord is one session with no process behind it.
//
// Exactly the fields a wake needs and the room needs to draw one: the id
// --resume is given, the directory it has to run in, and the two display
// halves. Nothing else, and no PID - see the header.
type parkedRecord struct {
	ID     string    `json:"id"`
	Name   string    `json:"name,omitempty"`
	Label  string    `json:"label,omitempty"`
	Dir    string    `json:"dir,omitempty"`
	Parked time.Time `json:"parked"`

	// Effort is the reasoning level this session was started at, and it is the
	// one piece of *configuration* in a record that otherwise holds only
	// identity and location. It is here because a level the operator chose and
	// a wake silently dropped is a downgrade with nothing on screen saying so -
	// and unlike the manager's config it cannot be derived from anything, since
	// nothing about a name or a directory implies an effort.
	//
	// Empty means Wake chose none, which is what it means everywhere else: the
	// flag is left off and claude applies its own default.
	Effort string `json:"effort,omitempty"`

	// Model is what this session runs as, here for Effort's reason: a model the
	// operator chose and a wake silently dropped is a downgrade with nothing on
	// screen saying so, and nothing about a name or a directory implies one.
	//
	// Unlike Effort it is not narrowed on the way back out. Effort has two
	// vocabularies and only one of them may reach an argv; a model has no
	// knowable set at all, so a row is carried as written or not at all.
	Model string `json:"model,omitempty"`

	// MaxBudgetUSD is the ceiling this session was started under and
	// FallbackModel is the chain it fails over to. Here for Effort's reason and
	// then some: there is no runtime command for either, so the record is the
	// *only* thing that can carry one across a park, and a cap a wake dropped
	// would make ⌃Q the way to uncap a fleet.
	//
	// Carried as written, like Model. Both were checked before the session that
	// wrote this row was started, and re-narrowing here would refuse a row this
	// build's own spawn path admitted.
	MaxBudgetUSD  string `json:"max_budget_usd,omitempty"`
	FallbackModel string `json:"fallback_model,omitempty"`

	// Color is the identity hue this session was set to, here for the display
	// halves' reason rather than the configuration ones: a colour an operator
	// chose is operator intent, and a wake that dropped it would bring the session
	// back grey with nothing saying why. Unlike a rename it may be rewritten here,
	// because it is only ever written *by the park* - a running session's /color
	// is refused once it is parked (see color.go), so the record is the one writer.
	Color string `json:"color,omitempty"`
}

// parkBook is the on-disk record, rewritten whole through a temp file and a
// rename, so a reader sees the previous book or the next one and never half of
// either. Same shape as rosterFile, deliberately: this file is read by a
// process that is starting while another may be finishing.
type parkBook struct {
	path string

	mu   sync.Mutex
	held map[string]parkedRecord

	// reserved is process-local ownership while one wake attempts a launch.
	// The exact held/on-disk record stays durable; status and a second wake hide
	// it until success commits deletion or failure releases ownership.
	reserved map[string]parkedRecord

	// pending is an exact generation whose atomic add or deletion rewrite
	// failed. A dirty addition remains in held; a deletion does not. Either
	// keeps the daemon supervising until an eventful whole-book retry makes disk
	// match held. A newer add under the same id supersedes the older generation.
	pending map[string]parkedRecord
}

// newParkBook seeds itself from disk, which is the one place it differs from
// newRosterFile and the difference is the whole point. A starting daemon holds
// no live sessions whatever sessions.json says, so the roster starts empty; it
// holds every session parked.json names, so this does not.
func newParkBook(path string) *parkBook {
	held := make(map[string]parkedRecord)
	for _, rec := range loadParkBook(path) {
		held[rec.ID] = rec
	}
	return &parkBook{
		path: path, held: held,
		reserved: make(map[string]parkedRecord),
		pending:  make(map[string]parkedRecord),
	}
}

func (p *parkBook) add(rec parkedRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held[rec.ID] = rec
	if err := p.writeLocked(); err != nil {
		p.pending[rec.ID] = rec
		return err
	}
	// A successfully published newer generation is now the durable truth for
	// this id, so an older failed deletion has been superseded.
	delete(p.pending, rec.ID)
	return nil
}

func (p *parkBook) reserve(id string) (parkedRecord, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, reserved := p.reserved[id]; reserved {
		return parkedRecord{}, false, errParkReservationHeld
	}
	rec, held := p.held[id]
	if !held {
		return parkedRecord{}, false, nil
	}
	p.reserved[id] = rec
	return rec, true, nil
}

// reserveExact takes ownership only of the generation the caller already
// validated. A record can be replaced between wakeRecord and this call by a
// newer completePark; launching that unvalidated generation would apply the
// old row's safety checks and configuration to different durable state.
func (p *parkBook) reserveExact(expected parkedRecord) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, reserved := p.reserved[expected.ID]; reserved {
		return false, errParkReservationHeld
	}
	rec, held := p.held[expected.ID]
	if !held || rec != expected {
		return false, nil
	}
	p.reserved[expected.ID] = expected
	return true, nil
}

func (p *parkBook) release(id string) {
	p.mu.Lock()
	delete(p.reserved, id)
	p.mu.Unlock()
}

// commit is the only durable deletion: it runs after a process successfully
// starts. It deletes only the exact generation this reservation owns; a newer
// completePark record under the same id remains durable. If the atomic write
// fails, the exact deleted generation remains hidden in pending and keeps this
// daemon alive until an eventful retry makes disk match held.
func (p *parkBook) commit(expected parkedRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	reserved, owns := p.reserved[expected.ID]
	if !owns || reserved != expected {
		return nil
	}
	delete(p.reserved, expected.ID)
	held, exists := p.held[expected.ID]
	if !exists || held != expected {
		return nil
	}
	delete(p.held, expected.ID)
	p.pending[expected.ID] = expected
	if err := p.writeLocked(); err != nil {
		return err
	}
	delete(p.pending, expected.ID)
	return nil
}

// retryPending makes disk match the currently held generations. Rewriting the
// whole book is generation-safe: a newer R2 in held replaces an old pending R
// rather than being removed with it. Called only from lifecycle events; there
// is deliberately no timer or polling loop.
func (p *parkBook) retryPending() (map[string]parkedRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.pending) == 0 {
		return nil, nil
	}
	published := make(map[string]parkedRecord, len(p.held))
	for id, rec := range p.held {
		published[id] = rec
	}
	if err := p.writeLocked(); err != nil {
		return nil, err
	}
	p.pending = make(map[string]parkedRecord)
	return published, nil
}

func (p *parkBook) hasPending() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.pending) != 0
}

func (p *parkBook) isDurable(expected parkedRecord) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, held := p.held[expected.ID]
	_, dirty := p.pending[expected.ID]
	return held && rec == expected && !dirty
}

// clear forgets every parked session. The quit verb is the one caller: spec §5's
// stop is the deliberate ending, and a stop that left twenty sessions for the
// next `wake` to offer back would make the one irreversible verb reversible by
// accident. The transcripts stay on disk; what goes is Wake's memory of them.
func (p *parkBook) clear() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.held = make(map[string]parkedRecord)
	p.reserved = make(map[string]parkedRecord)
	p.pending = make(map[string]parkedRecord)
	if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove park book %s: %w", p.path, err)
	}
	return nil
}

func (p *parkBook) records() []parkedRecord {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]parkedRecord, 0, len(p.held))
	for id, rec := range p.held {
		if _, reserved := p.reserved[id]; reserved {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// protectedIDs are records already durable or temporarily reserved by a wake.
// Shutdown must not reconstruct either: the exact record is still held, and a
// successful wake cannot pass admission after quit commits.
func (p *parkBook) protectedIDs() map[string]bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]bool, len(p.held)+len(p.reserved))
	for id := range p.held {
		out[id] = true
	}
	for id := range p.reserved {
		out[id] = true
	}
	return out
}

// record is one entry by id, for the resume that has no live agent to look up.
func (p *parkBook) record(id string) (parkedRecord, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	rec, ok := p.held[id]
	return rec, ok
}

// wakeRecord is the visible record for a wake. Identity fences use record and
// still see a reservation; a second wake gets the in-progress refusal instead.
func (p *parkBook) wakeRecord(id string) (parkedRecord, bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, reserved := p.reserved[id]; reserved {
		return parkedRecord{}, false, errParkReservationHeld
	}
	rec, ok := p.held[id]
	return rec, ok, nil
}

// parkedStatuses is a whole park book as the wire reports it, minus the entries
// nothing could ever bring back.
//
// **An id Wake did not mint is dropped rather than reported.** Nothing can be
// resumed under one - the reaper's only proof that a process group belongs to a
// session is that id appearing in an argv, which an ordinary word matches by
// accident - so a row for it offers a session that cannot come back, and
// `/resume` would refuse it after somebody typed the name. One unusable entry
// costs only itself; the rest of the book is reported.
//
// **And a record with no transcript on disk is dropped too**, through the very
// lookup a wake would use to reach one - transcriptPath, never a slug rebuilt
// here. mintedByWake only proves the id is well formed; a book routinely holds
// ids that were parked before their first turn, or whose transcript has since
// been deleted, and a wake of one of those opens an empty conversation under a
// live id (2026-08-23: 11 of 33 records on one machine, every manager among
// them). The two readings are indistinguishable and resolve the same way - there
// is nothing to bring back, so it is not offered. Pruning the record itself is a
// separate decision the book does not make here.
//
// The check is a stat per record and fleet() calls this on every status
// broadcast, so it is filesystem work off a live fleet's state changes - but
// zero when nothing is parked (the loop never runs) and bounded by the park
// book otherwise, which is a handful of rows in ordinary use. Batching it into
// one ProjectsDir scan was declined: a second reader of that directory beside
// transcriptPath is the parallel implementation the non-negotiables forbid, and
// transcriptPath is the lookup this must agree with exactly.
func parkedStatuses(recs []parkedRecord) []rpc.SessionStatus {
	var out []rpc.SessionStatus
	for _, rec := range recs {
		if !mintedByWake(rec.ID) {
			logf("wake: %q in the park book is not an id Wake minted, so nothing can be resumed under it", rec.ID)
			continue
		}
		// No transcript means nothing to resume - a wake would open an empty
		// conversation under a live id - so the record is dropped. Silently:
		// this is routine (a session parked before its first turn is the
		// ordinary case), and fleet() runs it on every status broadcast, so a
		// line per stale record here would be a line per record per fleet
		// state-change. mintedByWake above logs because that one *is* an anomaly.
		if _, ok := transcriptPath(rec.ID); !ok {
			continue
		}
		out = append(out, parkedStatus(rec))
	}
	return out
}

// parkedStatus is a park book entry as the wire reports it.
//
// One converter for both producers - the live daemon's fleet() and the on-disk
// FleetOnDisk - because a row that differed between them would make bare `wake`
// branch one way against a running daemon and the other way against its book.
// The state is StateParked and there is no PID: nothing is holding it.
//
// QuietMS is measured from when the session **parked**, not from when anything
// started. Starting a daemon does not make a parked session recent, and this is
// the number `wake status` prints beside a row somebody is deciding what to do
// with. A record that does not say when it parked reports nothing rather than
// two thousand years: the zero time means a book an older build wrote or one
// somebody edited, and the subtraction from 1970 is worse than saying nothing.
func parkedStatus(rec parkedRecord) rpc.SessionStatus {
	st := rpc.SessionStatus{
		ID:     rec.ID,
		Name:   rec.Name,
		Label:  rec.Label,
		Color:  rec.Color,
		Dir:    rec.Dir,
		Effort: rec.Effort,
		State:  rpc.StateParked,
	}
	if !rec.Parked.IsZero() {
		st.QuietMS = time.Since(rec.Parked).Milliseconds()
	}
	return st
}

func (p *parkBook) writeLocked() error {
	recs := make([]parkedRecord, 0, len(p.held))
	for _, rec := range p.held {
		recs = append(recs, rec)
	}
	data, err := json.Marshal(recs)
	if err != nil {
		return fmt.Errorf("encode park book: %w", err)
	}
	return writeFileAtomically(p.path, "park book", data, parkBookPerm)
}

// loadParkBook reads what the last daemon left. A missing file is the ordinary
// case - nothing has ever been parked here - and an unreadable one is reported
// and treated as empty, because refusing to start would cost the live fleet
// too.
func loadParkBook(path string) []parkedRecord {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("wake: cannot read the park book %s: %v", path, err)
		}
		return nil
	}
	var recs []parkedRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		logf("wake: park book %s is unreadable, so anything parked in it cannot be offered back: %v", path, err)
		return nil
	}
	return recs
}
