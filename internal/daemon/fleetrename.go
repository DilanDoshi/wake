package daemon

// `/wake-name`: renaming a running fleet.
//
// A fleet is its directory under ~/.wake/fleets (fleet.go), so a rename moves the
// directory - and leaves a link at the old name, because every process this daemon
// started holds a path under it: an agent's WAKE_SOCKET, the manager's tools in
// mcp.json, claude's debug file, and this daemon's own roster, park book and lock.
// Through the link all of them keep reaching the same files. Only what the fleet
// answers to moves (fleetHome): the report's socket and name, and the manager's
// claude name while it is still the fleet's own.
//
// The link lasts as long as this daemon, which outlives everything it started that
// holds the old path; Serve drops it at exit while it still holds the lock
// (dropForwards). A crash leaves it until the fleet's next clean exit, forwarding
// meanwhile. decisions.md, 2026-10-24.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// unnamedFleetKept refuses the fleet at ~/.wake itself: its files sit beside
	// fleets/, so there is no directory of its own to move or to link.
	unnamedFleetKept = "this is the unnamed fleet, whose files are ~/" + stateDirName + " itself rather than a " +
		"directory of their own, so it has no name to change. `wake --fleet <name>` starts a named one"

	// handSetFleetKept refuses a socket set by hand outside ~/.wake's layout: it is
	// no fleet's directory.
	handSetFleetKept = "this fleet's socket was set by hand with " + SocketEnv + " (%s), so it names no fleet to rename"
)

// fleetHome is what the fleet answers to: the socket it was started on until a
// rename moves it. One rename at a time, and readers never wait on one.
type fleetHome struct {
	moving sync.Mutex
	moved  atomic.Pointer[string]
}

// fleetSocket is the socket this fleet answers to now. The daemon's own files stay
// on s.socket, reached through the link a rename leaves there.
func (s *server) fleetSocket() string {
	if moved := s.home.moved.Load(); moved != nil {
		return *moved
	}
	return s.socket
}

// fleetNameOf is what the fleet on socket is called: a named fleet's name,
// LegacyFleet for the one at root, and "" for a socket outside that layout,
// which names no fleet.
func fleetNameOf(root, socket string) string {
	dir, err := filepath.Abs(filepath.Dir(socket))
	if err != nil {
		dir = filepath.Dir(socket)
	}
	switch {
	case dir == root:
		return LegacyFleet
	case filepath.Base(socket) == socketFileName && filepath.Dir(dir) == filepath.Join(root, fleetsDirName) &&
		checkFleetName(filepath.Base(dir)) == nil:
		return filepath.Base(dir)
	}
	return ""
}

// renameFleet is FrameRenameFleet. The asker hears a refusal; every window hears
// the rename on the report that follows it.
func (s *server) renameFleet(c *client, f rpc.Frame) {
	if err := s.moveFleetTo(strings.TrimSpace(f.Text)); err != nil {
		c.enqueue(errorFrame("", err.Error()))
		return
	}
	s.pushStatus()
}

// moveFleetTo renames this fleet to `to`, and the manager with it while it is
// still called for the fleet.
func (s *server) moveFleetTo(to string) error {
	if s.refusesAdmission() {
		return errors.New("the fleet is shutting down")
	}
	root, err := StateRoot()
	if err != nil {
		return err
	}
	s.home.moving.Lock()
	defer s.home.moving.Unlock()
	cur := s.fleetSocket()
	from := fleetNameOf(root, cur)
	switch from {
	case LegacyFleet:
		return errors.New(unnamedFleetKept)
	case "":
		return fmt.Errorf(handSetFleetKept, cur)
	}
	// A daemon started under an old name moves the directory, not that link.
	if from, err = forwardedName(root, from); err != nil {
		return err
	}
	if err := moveFleet(root, from, to); err != nil {
		return err
	}
	moved := filepath.Join(fleetPath(root, to), socketFileName)
	s.home.moved.Store(&moved)
	if mgr, ok := s.managerAgent(); ok {
		mgr.followFleet(core.ManagerPrefix+nameShaped(from), core.ManagerPrefix+nameShaped(to))
	}
	return nil
}

func fleetPath(root, name string) string { return filepath.Join(root, fleetsDirName, name) }

// linkFleet makes an old name's link. A var only so a test can make it fail;
// nothing outside a test assigns it.
var linkFleet = os.Symlink

// moveFleet renames fleet from to `to` under root and leaves a link at from. A
// link that cannot be made undoes the move, and an old name of this fleet's
// own taken back is linked again: every process holding either path keeps it.
func moveFleet(root, from, to string) error {
	if err := checkFleetName(to); err != nil {
		return err
	}
	src, dst := fleetPath(root, from), fleetPath(root, to)
	if err := checkSocketPath(filepath.Join(dst, socketFileName)); err != nil {
		return err
	}
	tookBack, err := clearFor(src, dst, to)
	if err != nil {
		return err
	}
	putBack := func() error {
		if !tookBack {
			return nil
		}
		return linkFleet(from, dst)
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("rename the fleet's directory: %w", errors.Join(err, putBack()))
	}
	if err := linkFleet(to, src); err != nil {
		undo := os.Rename(dst, src)
		if undo == nil {
			undo = putBack()
		}
		return fmt.Errorf("leave a link at %s, so the fleet stays %s: %w", src, from, errors.Join(err, undo))
	}
	return nil
}

// clearFor makes dst free for the fleet at src, or says why it is not: another
// fleet's directory or old name is never taken, and one of this fleet's own old
// names is taken back - an undo - by removing its link, which it reports.
func clearFor(src, dst, to string) (tookBack bool, err error) {
	at, err := os.Lstat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	link := at.Mode()&fs.ModeSymlink != 0
	here, err := os.Stat(src)
	if err != nil {
		return false, err
	}
	there, err := os.Stat(dst)
	switch {
	case err == nil && os.SameFile(here, there) && link:
		return true, os.Remove(dst)
	case err == nil && os.SameFile(here, there):
		return false, fmt.Errorf("this fleet is already called %s", to)
	case link && errors.Is(err, fs.ErrNotExist):
		return false, forwardsNowhere(to, dst)
	case link:
		return false, fmt.Errorf("%s forwards to another fleet until that fleet's daemon stops", to)
	}
	return false, fmt.Errorf("a fleet called %s already exists; `wake fleets` lists them", to)
}

// forwardsNowhere refuses a link to nothing rather than removing it: it is
// another rename's link for the width of its move, or one whose fleet was
// removed by hand, and only the second is the operator's to clear.
func forwardsNowhere(name, path string) error {
	return fmt.Errorf("fleet %s forwards to a fleet that is not there; if nothing is renaming it, remove %s", name, path)
}

// forwardedName is the fleet an old name forwards to, through every link a rename
// left, so `wake --fleet <old>` - and any daemon it starts - lands on the fleet's
// real name. A link to nothing is refused, never removed (forwardsNowhere).
func forwardedName(root, name string) (string, error) {
	dir := fleetPath(root, name)
	if at, err := os.Lstat(dir); err != nil || at.Mode()&fs.ModeSymlink == 0 {
		return name, nil
	}
	real, err := filepath.EvalSymlinks(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return "", forwardsNowhere(name, dir)
	}
	if err != nil {
		return "", fmt.Errorf("follow fleet %s: %w", name, err)
	}
	fleets, err := filepath.EvalSymlinks(filepath.Join(root, fleetsDirName))
	if err != nil {
		return "", err
	}
	if to := filepath.Base(real); filepath.Dir(real) == fleets && checkFleetName(to) == nil {
		return to, nil
	}
	return "", fmt.Errorf("fleet %s is a link to %s, which is not a fleet", name, real)
}

// dropForwards removes every old name that forwards to the fleet at dir, and
// nobody else's. They are all read before any goes, so a chain is not broken
// into a link to nothing halfway through.
func dropForwards(root, dir string) {
	home, err := os.Stat(dir)
	if err != nil {
		return
	}
	entries, err := os.ReadDir(filepath.Join(root, fleetsDirName))
	if err != nil {
		return
	}
	var ours []string
	for _, e := range entries {
		path := fleetPath(root, e.Name())
		if fi, err := os.Stat(path); e.Type()&fs.ModeSymlink != 0 && err == nil && os.SameFile(home, fi) {
			ours = append(ours, path)
		}
	}
	for _, path := range ours {
		if err := os.Remove(path); err != nil {
			logf("wake: remove the old fleet name %s: %v", path, err)
		}
	}
}

// dropForwards is the daemon's own exit: the old names of a named fleet go with
// the last process that could need them. Nil-safe for Serve, whose defer is
// registered before there is a server.
func (s *server) dropForwards() {
	if s == nil {
		return
	}
	root, err := StateRoot()
	if err != nil {
		return
	}
	if name := fleetNameOf(root, s.fleetSocket()); name != "" && name != LegacyFleet {
		dropForwards(root, filepath.Dir(s.socket))
	}
}

// followFleet moves the manager's claude name from the fleet's old one to its new
// one, and only from it: any other is the operator's part, which wins. A live
// manager is told at idle with the one /rename any rename sends; a parked one has
// no process for the probe to reach (queueProbeLocked), so only the name it wakes
// under moves.
func (a *agent) followFleet(from, to string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.claudeWant != from {
		return
	}
	a.claudeWant = to
	a.probeWanted[renameProbe] = true
	a.tryProbeLocked(renameProbe)
}

// recordOf is recordFor as this fleet books it: a manager still under the
// fleet's own name is booked under none, so it wakes under whatever the fleet is
// called by then. Under the rename's lock (home, then agent: moveFleetTo's
// order), so the name and the fleet's default are read from one side of it.
func (s *server) recordOf(a *agent) parkedRecord {
	s.home.moving.Lock()
	defer s.home.moving.Unlock()
	rec := recordFor(a)
	if rec.ClaudeName == s.managerClaudeDefault() {
		rec.ClaudeName = ""
	}
	return rec
}
