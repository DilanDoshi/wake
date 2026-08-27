//go:build unix

// Taking the lock, where there is a kernel to take it from. See lock.go for
// why this is the primitive rather than a pid file or a probe on the socket.

package daemon

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
)

// lockfile is this process's claim on one Wake state directory.
type lockfile struct {
	path string

	// exclusive is whether the claim was actually granted. False means the
	// lock could not be taken *and* nobody else holds it - a filesystem with
	// no flock, a directory that cannot be written - which is not a reason to
	// refuse to run, but is a reason not to reap. Written once by takeLock,
	// before any goroutine reads it, so it needs no lock.
	exclusive bool

	// why exclusive is false, kept so the daemon can say what it gave up
	// rather than quietly declining to clean up after its predecessor.
	why error

	// mu guards file, info and withdrawn, which verify (the watchdog goroutine)
	// and release (Serve's shutdown defer) both touch. The wait that normally
	// orders those two is bounded, so a slow verify can overlap release - see
	// verify. It is never held across the stat/open/flock that re-establishes a
	// swept lock, only around the field reads and the swap.
	mu   sync.Mutex
	file *os.File
	// info identifies the inode the claim was granted on, so the liveness path
	// can tell the file it locked from a different one that later takes the
	// name. Nil when exclusive is false, or when the stat failed: there is then
	// nothing to defend and verify becomes a no-op. See verify.
	info os.FileInfo
	// withdrawn is set once this claim is being given up - by release, and by
	// verify when a successor has taken the path - so a verify still finishing
	// its I/O cannot resurrect a descriptor the daemon is closing, and a lost
	// lock stops being re-probed every tick.
	withdrawn bool
}

// takeLock claims the daemon lock for this process. It answers three ways and
// the middle one is the point:
//
//	another daemon holds it   -> errLockHeld; the caller must refuse to start
//	taken                     -> exclusive; the caller may reap
//	could not be taken at all  -> not exclusive; the caller must not reap
//
// The third is never folded into either of the others. Folded into the first,
// a machine whose state directory is on a filesystem without flock could never
// start a daemon; folded into the second, "I could not check" would authorise
// a SIGKILL, which is the whole failure this file exists to prevent.
func takeLock(path string) (*lockfile, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockPerm)
	if err != nil {
		return &lockfile{path: path, why: fmt.Errorf("open the daemon lock %s: %w", path, err)}, nil
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errLockHeld
		}
		return &lockfile{path: path, why: fmt.Errorf("lock %s: %w", path, err)}, nil
	}
	// Say on disk what this is. The file is left behind on purpose (see
	// release), so the one person who will ever meet it is somebody tidying
	// ~/.wake - and deleting it under a running daemon is the one way to defeat
	// the lock, because a successor would then lock a different inode and both
	// would believe they hold it. verify re-establishes it on the liveness tick,
	// which closes all but the window between the sweep and the next tick. A
	// failed write is not fatal: the lock is the flock, not the bytes.
	if err := explainLock(f); err != nil {
		logf("wake: could not describe the daemon lock in %s: %v", path, err)
	}
	// Record which inode the flock was granted on. The lock is the flock, not
	// this identity, so a stat that fails is logged and not fatal - it only
	// costs verify the ability to notice a later sweep.
	fi, err := f.Stat()
	if err != nil {
		logf("wake: could not record the daemon lock's inode in %s: %v", path, err)
	}
	return &lockfile{path: path, file: f, exclusive: true, info: fi}, nil
}

// verify re-checks that the file this claim was granted on is still the file at
// the lock's path, and re-establishes the claim when it is not.
//
// The one way to defeat the lock is to delete daemon.lock under a running
// daemon: the flock lives on behind this descriptor, but the name no longer
// points at the inode it was taken on, so a successor's takeLock creates and
// locks a *different* inode, comes up believing it holds the lock, and its
// reaper SIGKILLs this daemon's fleet. It is not a contrived deletion - the
// socket, and so the lock beside it, lives under a $(mktemp -d) that /tmp's
// periodic sweep runs against, on a schedule.
//
// Re-creating and re-locking the file closes it: a successor then finds a held
// lock and refuses. This runs on the liveness tick, which already wakes on a
// flat schedule and spawns nothing - so it is one stat in the common case, and
// the open only on the rare tick after a sweep.
//
// The mutex is taken only around the field reads and the final swap, never
// across the stat/open/flock: release holds no lock while it closes a
// descriptor, and neither may this. The withdrawn re-check after the I/O is
// what stops a verify that raced a release from resurrecting the claim.
func (l *lockfile) verify() {
	l.mu.Lock()
	skip := l.withdrawn || !l.exclusive || l.file == nil || l.info == nil
	info := l.info
	l.mu.Unlock()
	if skip {
		return // nothing was granted, or the claim is being given up
	}
	if cur, err := os.Stat(l.path); err == nil && os.SameFile(info, cur) {
		return // the file we locked is still the file at the path
	}
	f, fi, err := reopenLock(l.path)
	if errors.Is(err, errLockHeld) {
		// A successor created and locked a new inode between the sweep and this
		// tick. Both daemons believe they hold the lock; this one already ran
		// its reaper at startup, so nothing here can undo a kill - but the split
		// brain is real. Withdraw so the lost claim is not re-probed every tick.
		l.mu.Lock()
		l.withdrawn = true
		l.mu.Unlock()
		logf("wake: the daemon lock %s was deleted and a successor now holds its replacement, so two daemons may be running", l.path)
		return
	}
	if err != nil {
		// A transient failure to reopen (a permission, a full disk); keep the
		// old descriptor and try again next tick rather than giving up.
		logf("wake: the daemon lock %s was deleted and could not be re-established: %v", l.path, err)
		return
	}
	l.mu.Lock()
	if l.withdrawn {
		l.mu.Unlock()
		_ = f.Close() // released while we re-established; do not resurrect the claim
		return
	}
	old := l.file
	l.file, l.info = f, fi
	l.mu.Unlock()
	_ = old.Close() // drop the flock on the swept, now-nameless inode
	logf("wake: the daemon lock %s was deleted under a running daemon and has been re-established", l.path)
}

// reopenLock re-creates and re-locks the file at path after a sweep, returning
// the new descriptor and its inode identity. errLockHeld means a successor
// already holds the replacement; any other error is a transient failure to
// reopen. Does no locking of its own - the caller owns the fields.
func reopenLock(path string) (*os.File, os.FileInfo, error) {
	// O_CREATE remakes a gone file; the flock then tells the two cases apart -
	// granted on a fresh inode this daemon now owns, refused on one a successor
	// already locked.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, lockPerm)
	if err != nil {
		return nil, nil, fmt.Errorf("reopen %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, nil, errLockHeld
		}
		return nil, nil, fmt.Errorf("re-lock %s: %w", path, err)
	}
	if err := explainLock(f); err != nil {
		logf("wake: could not describe the re-established daemon lock in %s: %v", path, err)
	}
	fi, err := f.Stat()
	if err != nil {
		// The lock is the flock, not this identity - but with no inode to
		// compare, verify can no longer notice a *later* sweep of this file.
		logf("wake: could not record the re-established daemon lock's inode in %s, so it is no longer watched for a later sweep: %v", path, err)
	}
	return f, fi, nil
}

// lockContents is what the file says to whoever opens it.
const lockContents = `This file is Wake's daemon lock. It is held with flock(2), which the kernel
releases when the daemon exits - including on SIGKILL - so it never goes stale
and never needs cleaning up.

Do not delete it while a daemon is running. A daemon that starts after the file
is replaced locks a different inode, so two daemons would each believe they
hold it, and the second one's reaper would SIGKILL the first one's agents.
Removing it while nothing is running is harmless; so is leaving it.
`

// explainLock writes that text, replacing whatever was there.
func explainLock(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate: %w", err)
	}
	if _, err := f.WriteAt([]byte(lockContents), 0); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	return nil
}

// release drops the claim.
//
// Closing the descriptor is the whole of it: that is what releases an flock,
// and it is also what the kernel does on process death, which is why the two
// paths cannot disagree.
//
// The file is deliberately left on disk. Unlinking it here would be a race
// with a successor that has already opened the same path and is about to lock
// an inode with no name - two daemons each holding "the" lock on different
// inodes, which is no lock at all. An empty 0600 file next to the socket costs
// nothing.
func (l *lockfile) release() error {
	// withdrawn under the same lock verify takes, so a verify still finishing
	// its re-establish sees the claim is gone and closes its own descriptor
	// rather than storing it back into l.file. The close is outside the lock,
	// for the reason server.mu's comment gives: a lock is never held across it.
	l.mu.Lock()
	f := l.file
	l.file = nil
	l.withdrawn = true
	l.mu.Unlock()
	if f == nil {
		return nil
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("release the daemon lock %s: %w", l.path, err)
	}
	return nil
}
