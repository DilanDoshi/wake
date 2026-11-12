package daemon

// The end of a fleet (BUG-21): when one is done, and removing it.
//
// A directory with no socket is still a fleet (fleet.go), so a fleet is done
// only when nothing in it can come back: no daemon, nothing parked, nothing a
// crash left for the reaper, and nothing of anybody else's. `wake stop` is the
// one caller - it is already the irreversible ending, and forgets the parked
// half - and it removes a fleet so its name is free again. Everything is decided and removed under the
// fleet's own daemon lock, which a live daemon holds and a starting one takes
// before it writes anything that could make the fleet not done.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// fleetFiles is every file Wake writes in a named fleet's directory, the lock
// last: until it goes, a daemon starting on the fleet finds it held and refuses.
// Anything else in the directory is somebody's, and keeps the fleet.
var fleetFiles = []string{
	socketFileName, logFileName, mcpConfigName, parkBookName, rosterFileName, parkReadyName, lockFileName,
}

// ErrFleetRunning is a fleet whose lock a daemon holds.
var ErrFleetRunning = errors.New("a daemon holds this fleet's lock")

// removeFleetFile removes one of a fleet's files. A var only so a test can see
// the order and make a file arrive meanwhile; nothing outside a test assigns it.
var removeFleetFile = os.Remove

// FleetLeft is what a fleet's directory holds with no daemon answering.
type FleetLeft struct {
	Parked  []string // the park book's sessions, by name
	Crashed bool     // roster rows or a park-ready file: its daemon died without stopping
	Kept    []string // entries Wake does not remove, a directory with a trailing slash
	Err     error    // a book or roster that could not be read, which is never an empty one
}

// Done is whether nothing in the fleet can come back.
func (l FleetLeft) Done() bool {
	return len(l.Parked) == 0 && !l.Crashed && len(l.Kept) == 0 && l.Err == nil
}

// readFleetLeft reads dir strictly: loadParkBook takes an unreadable book for an
// empty one so a daemon still starts, and here that would end somebody's
// sessions. own is whether dir is a named fleet's own, so its other entries are
// somebody's; the unnamed fleet's is ~/.wake, which holds every fleet.
func readFleetLeft(dir string, own bool) FleetLeft {
	var left FleetLeft
	book, err := readRecords[parkedRecord](filepath.Join(dir, parkBookName))
	for _, rec := range book {
		left.Parked = append(left.Parked, rec.Name)
	}
	roster, rerr := readRecords[record](filepath.Join(dir, rosterFileName))
	_, perr := os.Lstat(filepath.Join(dir, parkReadyName))
	left.Crashed = len(roster) > 0 || perr == nil
	if errors.Is(perr, fs.ErrNotExist) {
		perr = nil
	}
	left.Err = errors.Join(err, rerr, perr)
	if !own {
		return left
	}
	entries, err := os.ReadDir(dir)
	for _, e := range entries {
		switch {
		case slices.Contains(fleetFiles, e.Name()) && !e.IsDir():
		case e.IsDir():
			left.Kept = append(left.Kept, e.Name()+"/")
		default:
			left.Kept = append(left.Kept, e.Name())
		}
	}
	left.Err = errors.Join(left.Err, err)
	return left
}

// readRecords reads a JSON list a daemon wrote; a missing file holds none.
func readRecords[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []T
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return recs, nil
}

// FleetState is one fleet as `wake fleets` lists it: its daemon's report when
// one answers, Silent for one that took the dial and did not (a quit's grace, a
// wedge), and otherwise what its files hold.
type FleetState struct {
	Report *rpc.Status
	Silent bool
	Left   FleetLeft
}

// FleetStates is every named fleet's state. The fleets are asked together, so a
// daemon that takes the dial and never answers costs one statusTimeout for the
// whole listing rather than one each; a stopped one costs one failed dial and a
// read of its files, never FleetOnDisk's sweep. No lock is taken: holding one
// even for a moment could make a daemon starting on that fleet refuse.
func FleetStates(names []string) map[string]FleetState {
	var (
		mu     sync.Mutex
		wg     sync.WaitGroup
		states = map[string]FleetState{}
	)
	for _, name := range names {
		dir, err := fleetDir(name)
		if err != nil {
			states[name] = FleetState{Left: FleetLeft{Err: err}}
			continue
		}
		wg.Go(func() {
			var st FleetState
			report, running, err := runningStatus(filepath.Join(dir, socketFileName))
			switch {
			case running && err == nil:
				st.Report = &report
			case running:
				st.Silent = true
			default:
				st.Left = readFleetLeft(dir, name != LegacyFleet)
			}
			mu.Lock()
			states[name] = st
			mu.Unlock()
		})
	}
	wg.Wait()
	return states
}

// FleetEnd is what removing a stopped fleet came to: Name is "" for a socket
// with no directory of its own, Forgot names the parked sessions the removal
// forgot, and Left says what kept a fleet that stays.
type FleetEnd struct {
	Name    string
	Removed bool
	Forgot  []string
	Left    FleetLeft
}

// RemoveFleet removes the named fleet at socket unless something in it can come
// back, and says what came of it. `wake stop` is the one caller. forgetParked is
// a stop that found no daemon: its parked sessions are forgotten with the fleet
// (owner's 2026-11-11 ruling), each one named. After a daemon's own stop, which
// cleared the book, a parked session is one parked since, and keeps the fleet.
func RemoveFleet(socket string, forgetParked bool) (FleetEnd, error) {
	root, err := StateRoot()
	if err != nil {
		return FleetEnd{}, err
	}
	return removeFleetIn(root, socket, forgetParked)
}

// removeFleetIn is RemoveFleet against a given root, for fleetDirIn's reason.
// The unnamed fleet (~/.wake itself) and a socket set by hand have no directory
// of their own, and are left alone before anything on disk is touched.
func removeFleetIn(root, socket string, forgetParked bool) (FleetEnd, error) {
	end := FleetEnd{Name: fleetNameOf(root, socket)}
	if end.Name == "" || end.Name == LegacyFleet {
		return FleetEnd{}, nil
	}
	name, err := forwardedName(root, end.Name)
	if err != nil {
		return end, err
	}
	end.Name = name
	dir := fleetPath(root, name)
	if at, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return end, nil
	} else if err != nil || !at.IsDir() {
		return end, errors.Join(err, fmt.Errorf("%s is not a fleet's directory", dir))
	}
	lock, err := lockFleet(dir)
	if err != nil {
		return end, err
	}
	defer func() { _ = lock.release() }()

	left := readFleetLeft(dir, true)
	if left.Crashed || len(left.Kept) > 0 || left.Err != nil || (len(left.Parked) > 0 && !forgetParked) {
		end.Left = left
		return end, nil
	}
	// Read now and removed after: a fleet that stays keeps its old names, and
	// once its directory is gone no link can be told to forward to it.
	forwards := forwardsHere(root, dir)
	err = removeFleetDir(dir)
	// A removal that fails after the book went has forgotten its sessions all
	// the same, so they are named either way.
	if _, gone := os.Lstat(filepath.Join(dir, parkBookName)); errors.Is(gone, fs.ErrNotExist) {
		end.Forgot = left.Parked
	}
	if err != nil {
		return end, err
	}
	removeForwards(forwards)
	end.Removed = true
	return end, nil
}

// lockFleet takes a stopped fleet's daemon lock: a held one is a daemon running,
// and one that cannot be taken at all proves none is not.
func lockFleet(dir string) (*lockfile, error) {
	lock, err := takeLock(filepath.Join(dir, lockFileName))
	switch {
	case errors.Is(err, errLockHeld):
		return nil, ErrFleetRunning
	case err != nil:
		return nil, err
	case !lock.exclusive:
		return nil, fmt.Errorf("the fleet's lock could not be taken, so nothing proves no daemon is running: %w", lock.why)
	}
	return lock, nil
}

// removeFleetDir removes Wake's files from a done fleet's directory, then the
// directory - never the tree, so anything that arrived meanwhile keeps it.
func removeFleetDir(dir string) error {
	for _, file := range fleetFiles {
		if err := removeFleetFile(filepath.Join(dir, file)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", file, err)
		}
	}
	if err := os.Remove(dir); err != nil {
		return fmt.Errorf("remove the fleet's directory, which something wrote to as it was removed: %w", err)
	}
	syncDir(filepath.Dir(dir), "fleets directory")
	return nil
}
