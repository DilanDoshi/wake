package daemon

// When a fleet is done, and removing one (BUG-21). Every test here works in a
// temporary root: nothing in this file may reach the operator's ~/.wake.

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fleetWith makes a named fleet's directory under root holding files, and
// returns the socket a daemon of it would bind.
func fleetWith(t *testing.T, root, name string, files map[string]string) string {
	t.Helper()
	dir := fleetPath(root, name)
	if err := os.MkdirAll(dir, stateDirPerm); err != nil {
		t.Fatal(err)
	}
	for file, body := range files {
		path := filepath.Join(dir, file)
		if err := os.MkdirAll(filepath.Dir(path), stateDirPerm); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, socketFileName)
}

// stoppedFiles is what a cleanly stopped fleet leaves today: the lock, the log
// and the manager's config.
func stoppedFiles() map[string]string {
	return map[string]string{lockFileName: "", logFileName: "a line\n", mcpConfigName: "{}"}
}

func with(files map[string]string, name, body string) map[string]string {
	files[name] = body
	return files
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// mustStay fails unless every file is still where fleetWith put it.
func mustStay(t *testing.T, socket string, files map[string]string) {
	t.Helper()
	for file := range files {
		if !exists(t, filepath.Join(filepath.Dir(socket), file)) {
			t.Errorf("%s was removed from a fleet that was kept", file)
		}
	}
}

// A stopped fleet with nothing parked, nothing crashed and nothing of anybody
// else's in it is removed, and its name is the next one a bare `wake` draws.
func TestADoneFleetIsRemovedAndFreesItsName(t *testing.T) {
	root := testRoot(t)
	socket := fleetWith(t, root, fleetPool[0], with(stoppedFiles(), parkBookName, "[]"))
	fleetWith(t, root, fleetPool[1], stoppedFiles())

	end, err := removeFleetIn(root, socket, true)
	if err != nil || !end.Removed || end.Name != fleetPool[0] {
		t.Fatalf("removeFleetIn = %+v, %v; want %s removed", end, err, fleetPool[0])
	}
	if exists(t, filepath.Dir(socket)) {
		t.Error("the fleet's directory is still there")
	}
	if got, err := nextFleetName(root); err != nil || got != fleetPool[0] {
		t.Errorf("nextFleetName = %q, %v; want the removed fleet's name %q back", got, err, fleetPool[0])
	}
	if !exists(t, fleetPath(root, fleetPool[1])) {
		t.Error("removing one fleet removed another")
	}
}

// A live daemon holds its fleet's lock, and the lock is what refuses: nothing
// in the directory is touched while it stands.
func TestAFleetIsNotRemovedWhileADaemonHoldsItsLock(t *testing.T) {
	root := testRoot(t)
	files := stoppedFiles()
	socket := fleetWith(t, root, "dev", files)
	held, err := takeLock(lockPath(socket))
	if err != nil || !held.exclusive {
		t.Fatalf("take the lock as a daemon would: %v", err)
	}
	t.Cleanup(func() { _ = held.release() })

	end, err := removeFleetIn(root, socket, true)
	if !errors.Is(err, ErrFleetRunning) || end.Removed {
		t.Fatalf("removeFleetIn under a held lock = %+v, %v; want ErrFleetRunning", end, err)
	}
	mustStay(t, socket, files)
}

// A parked session is resumable, so a fleet holding one is not done - but a stop
// forgets the parked half (owner's 2026-11-11 ruling), so its removal forgets
// them and names each one, the manager alone included.
func TestARemovalForgetsParkedSessionsAndNamesThem(t *testing.T) {
	for _, tc := range []struct {
		what, book string
		names      []string
	}{
		{"two agents", `[{"id":"a","name":"alex"},{"id":"b","name":"manager"}]`, []string{"alex", "manager"}},
		{"the manager alone", `[{"id":"m","name":"manager"}]`, []string{"manager"}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			root := testRoot(t)
			socket := fleetWith(t, root, "dev", with(stoppedFiles(), parkBookName, tc.book))
			if left := readFleetLeft(filepath.Dir(socket), true); left.Done() {
				t.Errorf("a fleet with %s parked reads as done", tc.what)
			}

			end, err := removeFleetIn(root, socket, true)
			if err != nil || !end.Removed || !slices.Equal(end.Forgot, tc.names) {
				t.Fatalf("removeFleetIn = %+v, %v; want removed, forgetting %v", end, err, tc.names)
			}
			if exists(t, filepath.Dir(socket)) {
				t.Error("the fleet's directory is still there")
			}
		})
	}
}

// What keeps a fleet keeps its park book too: nothing is forgotten from a fleet
// that stays.
func TestAKeptFleetForgetsNothing(t *testing.T) {
	root := testRoot(t)
	files := with(with(stoppedFiles(), parkBookName, `[{"id":"a","name":"alex"}]`), parkReadyName, "[]")
	socket := fleetWith(t, root, "dev", files)

	end, err := removeFleetIn(root, socket, true)
	if err != nil || end.Removed || len(end.Forgot) > 0 {
		t.Fatalf("removeFleetIn = %+v, %v; want a crashed fleet kept whole", end, err)
	}
	mustStay(t, socket, files)
}

// loadParkBook reads an unreadable book as empty, so a daemon still starts. The
// end of a fleet must not: a book nobody can read may hold somebody's sessions.
func TestAnUnreadableBookOrRosterIsNotEmpty(t *testing.T) {
	for _, file := range []string{parkBookName, rosterFileName} {
		t.Run(file, func(t *testing.T) {
			root := testRoot(t)
			files := with(stoppedFiles(), file, "{not json")
			socket := fleetWith(t, root, "dev", files)

			end, err := removeFleetIn(root, socket, true)
			if err != nil || end.Removed || end.Left.Err == nil {
				t.Fatalf("removeFleetIn = %+v, %v; want the fleet kept for an unreadable %s", end, err, file)
			}
			mustStay(t, socket, files)
		})
	}
}

// A daemon that died without stopping leaves roster rows or a park-ready file:
// the next daemon's reaper hands those sessions to the park book (BUG-53), so a
// crashed fleet is never removed.
func TestACrashedFleetIsNotRemoved(t *testing.T) {
	for file, body := range map[string]string{
		rosterFileName: `[{"id":"a","name":"alex","pid":1}]`,
		parkReadyName:  `[]`,
	} {
		t.Run(file, func(t *testing.T) {
			root := testRoot(t)
			files := with(stoppedFiles(), file, body)
			socket := fleetWith(t, root, "dev", files)

			end, err := removeFleetIn(root, socket, true)
			if err != nil || end.Removed || !end.Left.Crashed {
				t.Fatalf("removeFleetIn = %+v, %v; want the fleet kept as crashed", end, err)
			}
			mustStay(t, socket, files)
		})
	}
}

// An empty roster is what a daemon that wrote one and then lost every agent
// leaves; it holds nothing to hand over.
func TestAnEmptyRosterIsNotACrash(t *testing.T) {
	root := testRoot(t)
	socket := fleetWith(t, root, "dev", with(stoppedFiles(), rosterFileName, "[]"))

	if end, err := removeFleetIn(root, socket, true); err != nil || !end.Removed {
		t.Fatalf("removeFleetIn = %+v, %v; want removed", end, err)
	}
}

// Only Wake's own files are Wake's to remove. A debug log (debuglog.go: Wake
// never removes one) or a backup somebody made keeps the whole fleet, and
// nothing in it is removed - not even Wake's own files.
func TestAFleetHoldingWhatWakeDoesNotRemoveIsKept(t *testing.T) {
	root := testRoot(t)
	files := with(with(stoppedFiles(), filepath.Join(debugDirName, "alex.log"), "x"),
		parkBookName+".before-restore", "[]")
	socket := fleetWith(t, root, "dev", files)

	end, err := removeFleetIn(root, socket, true)
	if err != nil || end.Removed {
		t.Fatalf("removeFleetIn = %+v, %v; want the fleet kept", end, err)
	}
	if want := []string{debugDirName + "/", parkBookName + ".before-restore"}; !slices.Equal(end.Left.Kept, want) {
		t.Errorf("kept = %v, want %v", end.Left.Kept, want)
	}
	mustStay(t, socket, files)
}

// The unnamed fleet's files are ~/.wake itself, and a socket set by hand names
// no fleet: neither has a directory of its own to remove.
func TestTheUnnamedFleetAndAHandSetSocketAreNeverRemoved(t *testing.T) {
	root := testRoot(t)
	if err := os.MkdirAll(root, stateDirPerm); err != nil {
		t.Fatal(err)
	}
	handSet := filepath.Join(t.TempDir(), "s.sock")
	for _, socket := range []string{filepath.Join(root, socketFileName), handSet} {
		for _, file := range []string{lockFileName, logFileName} {
			if err := os.WriteFile(filepath.Join(filepath.Dir(socket), file), nil, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		end, err := removeFleetIn(root, socket, true)
		if err != nil || end.Removed || end.Name != "" {
			t.Errorf("removeFleetIn(%s) = %+v, %v; want nothing done", socket, end, err)
		}
		if !exists(t, filepath.Join(filepath.Dir(socket), logFileName)) {
			t.Errorf("removeFleetIn(%s) removed a file from a directory that is no fleet's own", socket)
		}
	}
}

// A fleet reached by an old name is removed under its own, and every name that
// forwards to it goes too, so none is left forwarding to nothing; another
// fleet's old name stays.
func TestRemovingAFleetDropsItsOldNamesAndNoOneElses(t *testing.T) {
	root := testRoot(t)
	fleetWith(t, root, "dev", stoppedFiles())
	fleetWith(t, root, "web", stoppedFiles())
	for old, to := range map[string]string{"atlas": "dev", "basin": "web"} {
		if err := os.Symlink(to, fleetPath(root, old)); err != nil {
			t.Fatal(err)
		}
	}

	end, err := removeFleetIn(root, filepath.Join(fleetPath(root, "atlas"), socketFileName), true)
	if err != nil || !end.Removed || end.Name != "dev" {
		t.Fatalf("removeFleetIn through atlas = %+v, %v; want dev removed", end, err)
	}
	for _, gone := range []string{"dev", "atlas"} {
		if exists(t, fleetPath(root, gone)) {
			t.Errorf("%s is still there", gone)
		}
	}
	for _, kept := range []string{"web", "basin"} {
		if !exists(t, fleetPath(root, kept)) {
			t.Errorf("%s, another fleet's, was removed", kept)
		}
	}
}

// Removal is file by file, never the whole tree: anything that appears while it
// runs - a starting daemon's log, opened before its lock - keeps the directory,
// that file and the fleet's old names.
func TestRemovalNeverRemovesWhatAppearsWhileItRuns(t *testing.T) {
	root := testRoot(t)
	socket := fleetWith(t, root, "dev", with(stoppedFiles(), parkBookName, `[{"id":"a","name":"alex"}]`))
	if err := os.Symlink("dev", fleetPath(root, "atlas")); err != nil {
		t.Fatal(err)
	}
	late := filepath.Join(filepath.Dir(socket), "arrived")
	removeFleetFile = func(path string) error {
		if filepath.Base(path) == lockFileName {
			if err := os.WriteFile(late, nil, 0o600); err != nil {
				return err
			}
		}
		return os.Remove(path)
	}
	t.Cleanup(func() { removeFleetFile = os.Remove })

	end, err := removeFleetIn(root, socket, true)
	if err == nil || end.Removed {
		t.Fatalf("removeFleetIn = %+v, %v; want the directory kept and said", end, err)
	}
	if !exists(t, late) {
		t.Error("a file that arrived during the removal was removed with it")
	}
	if !exists(t, fleetPath(root, "atlas")) {
		t.Error("the fleet stayed but its old name was removed")
	}
	// The book went before the directory's removal failed, so what it held is
	// forgotten all the same, and named.
	if !slices.Equal(end.Forgot, []string{"alex"}) {
		t.Errorf("forgot = %v, want alex named: the book is gone though the fleet stays", end.Forgot)
	}
}

// After a daemon's own stop the book is empty, so a session in it was parked
// since - by a daemon started meanwhile - and the stop never named it: the
// fleet stays, book and all.
func TestAfterADaemonsStopAParkedSessionKeepsTheFleet(t *testing.T) {
	root := testRoot(t)
	files := with(stoppedFiles(), parkBookName, `[{"id":"a","name":"alex"}]`)
	socket := fleetWith(t, root, "dev", files)

	end, err := removeFleetIn(root, socket, false)
	if err != nil || end.Removed || len(end.Forgot) > 0 || !slices.Equal(end.Left.Parked, []string{"alex"}) {
		t.Fatalf("removeFleetIn = %+v, %v; want the fleet kept with alex parked", end, err)
	}
	mustStay(t, socket, files)
}

// A lock that cannot be taken at all proves no daemon is not running, so
// nothing is removed.
func TestALockThatCannotBeTakenRemovesNothing(t *testing.T) {
	root := testRoot(t)
	files := stoppedFiles()
	socket := fleetWith(t, root, "dev", files)
	if err := os.Chmod(lockPath(socket), 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockPath(socket), 0o600) })

	if end, err := removeFleetIn(root, socket, true); err == nil || end.Removed {
		t.Fatalf("removeFleetIn = %+v, %v; want a refusal", end, err)
	}
	mustStay(t, socket, files)
}

// The lock is the last of Wake's files to go: until then a daemon starting on
// the fleet finds it held and refuses.
func TestTheLockIsTheLastFileRemoved(t *testing.T) {
	root := testRoot(t)
	socket := fleetWith(t, root, "dev", with(with(stoppedFiles(), parkBookName, "[]"), rosterFileName, "[]"))
	var order []string
	removeFleetFile = func(path string) error {
		order = append(order, filepath.Base(path))
		return os.Remove(path)
	}
	t.Cleanup(func() { removeFleetFile = os.Remove })

	if end, err := removeFleetIn(root, socket, true); err != nil || !end.Removed {
		t.Fatalf("removeFleetIn = %+v, %v", end, err)
	}
	if len(order) == 0 || order[len(order)-1] != lockFileName {
		t.Errorf("removed in the order %v; the lock must be last", order)
	}
}

// A fleet that is not there is nothing to remove, and not an error: it is what
// `wake stop --fleet typo` reaches.
func TestAFleetThatIsNotThereIsNothingToRemove(t *testing.T) {
	root := testRoot(t)
	end, err := removeFleetIn(root, filepath.Join(fleetPath(root, "typo"), socketFileName), true)
	if err != nil || end.Removed {
		t.Errorf("removeFleetIn on no fleet = %+v, %v", end, err)
	}
	if exists(t, fleetPath(root, "typo")) {
		t.Error("asking to remove a fleet that was not there made it")
	}
}

// What the listing reads off a stopped fleet's files is what the removal
// decides on: one reading, so the two cannot disagree.
func TestAStoppedFleetsFilesSayWhatItIs(t *testing.T) {
	root := testRoot(t)
	for _, tc := range []struct {
		what  string
		files map[string]string
		check func(FleetLeft) bool
	}{
		{"done", stoppedFiles(), FleetLeft.Done},
		{"parked", with(stoppedFiles(), parkBookName, `[{"id":"m","name":"manager"}]`),
			func(l FleetLeft) bool { return !l.Done() && slices.Equal(l.Parked, []string{"manager"}) }},
		{"crashed", with(stoppedFiles(), parkReadyName, "[]"),
			func(l FleetLeft) bool { return !l.Done() && l.Crashed }},
		{"keeping a log", with(stoppedFiles(), filepath.Join(debugDirName, "a.log"), ""),
			func(l FleetLeft) bool { return !l.Done() && strings.Join(l.Kept, ",") == debugDirName+"/" }},
		{"unreadable", with(stoppedFiles(), parkBookName, "nope"),
			func(l FleetLeft) bool { return !l.Done() && l.Err != nil }},
	} {
		t.Run(tc.what, func(t *testing.T) {
			socket := fleetWith(t, root, strings.ReplaceAll(tc.what, " ", "-"), tc.files)
			if left := readFleetLeft(filepath.Dir(socket), true); !tc.check(left) {
				t.Errorf("readFleetLeft = %+v", left)
			}
		})
	}
}

// Resolving a named fleet's socket makes nothing, so a command refused after it,
// or one that only looks, leaves no directory behind (`wake --fleet typo
// bogus`). A bare `wake` still makes the one it drew: that is the name's
// reservation.
func TestResolvingAFleetMakesNoDirectoryButABareWakeReservesItsName(t *testing.T) {
	t.Setenv("HOME", tempHome(t))
	t.Setenv(SocketEnv, "")

	sock, err := FleetSocketPath("typo")
	if err != nil {
		t.Fatalf("FleetSocketPath: %v", err)
	}
	if exists(t, filepath.Dir(sock)) {
		t.Error("resolving fleet typo made its directory")
	}
	drawn, name, err := NewFleetSocketPath()
	if err != nil {
		t.Fatalf("NewFleetSocketPath: %v", err)
	}
	if !exists(t, filepath.Dir(drawn)) || name != fleetPool[0] {
		t.Errorf("a bare wake drew %q at %s without reserving it", name, drawn)
	}
}

// An old name that forwards to nothing is said, under that name, and nothing is
// removed: a link to nothing is the operator's to clear (forwardsNowhere).
func TestAForwardToNothingIsSaid(t *testing.T) {
	root := testRoot(t)
	if err := os.MkdirAll(filepath.Join(root, fleetsDirName), stateDirPerm); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("gone", fleetPath(root, "atlas")); err != nil {
		t.Fatal(err)
	}
	end, err := removeFleetIn(root, filepath.Join(fleetPath(root, "atlas"), socketFileName), true)
	if err == nil || end.Name != "atlas" || end.Removed {
		t.Errorf("removeFleetIn through a link to nothing = %+v, %v; want atlas named and refused", end, err)
	}
	if !exists(t, fleetPath(root, "atlas")) {
		t.Error("the link to nothing was removed")
	}
}
