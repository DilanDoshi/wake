// The daemon lock: the one claim on disk that cannot go stale.
//
// # What it is for
//
// The reaper SIGKILLs process groups named by a file a previous daemon wrote.
// It is only allowed to do that when there is provably no daemon holding those
// agents, so everything turns on how "no daemon is running" is established.
//
// Holding the listener bound through shutdown (see stopAcceptingOnStop) makes
// the socket a *good* signal, and a second daemon started by mistake finds it
// and refuses. It is not a sound one to authorise a SIGKILL on, for a reason
// that is not about this program at all: the answer a probe gets when nobody
// is accepting is ECONNREFUSED once the listen backlog is full, and that is
// byte-for-byte the answer it gets from the debris of a crashed daemon. Same
// errno, opposite meaning. The backlog is a kernel resource - 128 on darwin,
// 4096 on modern Linux - that this process can neither observe nor reset, and
// its slots are consumed cumulatively and not returned while nobody accepts,
// so one polite client re-dialling every 100ms exhausts darwin's in about 13
// seconds, comfortably inside a 30-second grace. A liveness test whose
// negative answer is ambiguous is the wrong thing to hang a fleet-wide kill
// on, however unlikely the precondition.
//
// # Why a lock and not a pid file
//
// The property this needs is that the claim dies exactly when the process
// does, including on SIGKILL, when no cleanup runs at all. The kernel releases
// an flock on the last close of the file description, and process death closes
// everything - so a stale lock is not a state that exists. A pid file has the
// opposite property: it survives the SIGKILL, so reading one means asking "is
// that pid alive", which is ambiguous again under pid reuse and puts the
// original problem back one level down.
//
// It is also a possession rather than a queue, so it cannot be exhausted, and
// EWOULDBLOCK means "a live daemon holds this" and nothing else.
//
// # What it gates
//
// The reaper - the only thing here that sends a signal. Serve refuses to start
// at all while another daemon holds the lock, so the socket and the roster get
// the same protection as a consequence; but the ordering that matters is that
// the lock is taken before anything is removed from disk and before a single
// pid is read off the roster, so a bind race can no longer authorise a reap.
//
// A platform that cannot take the lock is not refused - it runs, and does not
// reap. That is the same answer reap_other.go gives to everything: unknown
// never kills anything.

package daemon

import (
	"errors"
	"path/filepath"
)

// lockFileName sits beside the socket and the roster, so a test with its own
// socket directory gets its own lock and never contends with the real one.
//
// Beside the roster rather than beside the socket by coincidence: the roster
// is what the reaper reads, and one lock per roster is the invariant worth
// having.
const lockFileName = "daemon.lock"

// lockPerm keeps the lock file as private as the directory holding it.
const lockPerm = 0o600

func lockPath(socket string) string {
	return filepath.Join(filepath.Dir(socket), lockFileName)
}

// errLockHeld is a live daemon saying so unambiguously. It is the one answer
// here that is a fact rather than an inference.
var errLockHeld = errors.New("another daemon holds the lock beside this socket")
