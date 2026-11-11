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
// one answers, else what its files hold.
type FleetState struct {
	Report *rpc.Status
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
			if report, running, err := runningStatus(filepath.Join(dir, socketFileName)); running && err == nil {
				st.Report = &report
			} else {
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
// back other than its parked sessions, which it forgets: `wake stop` is the one
// caller, and a stop forgets the parked half (owner's 2026-11-11 ruling, the
// same as a live stop's).
func RemoveFleet(socket string) (FleetEnd, error) {
	root, err := StateRoot()
	if err != nil {
		return FleetEnd{}, err
	}
	return removeFleetIn(root, socket)
}

// removeFleetIn is RemoveFleet against a given root, for fleetDirIn's reason.
// The unnamed fleet (~/.wake itself) and a socket set by hand have no directory
// of their own, and are left alone before anything on disk is touched.
func removeFleetIn(root, socket string) (FleetEnd, error) {
	name := fleetNameOf(root, socket)
	if name == "" || name == LegacyFleet {
		return FleetEnd{}, nil
	}
	name, err := forwardedName(root, name)
	if err != nil {
		return FleetEnd{}, err
	}
	end := FleetEnd{Name: name}
	dir := fleetPath(root, name)
	if at, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return end, nil
	} else if err != nil || !at.IsDir() {
		return end, errors.Join(err, fmt.Errorf("%s is not a fleet's directory", dir))
	}
	lock, err := takeLock(filepath.Join(dir, lockFileName))
	if errors.Is(err, errLockHeld) {
		return end, ErrFleetRunning
	}
	if err != nil {
		return end, err
	}
	if !lock.exclusive {
		return end, fmt.Errorf("the fleet's lock could not be taken, so nothing proves no daemon is running: %w", lock.why)
	}
	defer func() { _ = lock.release() }()

	left := readFleetLeft(dir, true)
	if left.Crashed || len(left.Kept) > 0 || left.Err != nil {
		end.Left = left
		return end, nil
	}
	dropForwards(root, dir)
	if err := removeFleetDir(dir); err != nil {
		return end, err
	}
	end.Removed, end.Forgot = true, left.Parked
	return end, nil
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
