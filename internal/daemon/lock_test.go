//go:build unix

// The daemon lock, and the reap it is there to refuse.
//
// Unix-only for the same reason reap_unix_test.go is: the observation is
// whether a specific process is still alive after a second daemon started, and
// that is what "did the reaper get it wrong" means.

package daemon

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// The state no probe on the socket can read correctly: a daemon is alive and
// holding a fleet, and there is nothing listening on the socket path.
//
// That is not a contrived state. It is a daemon between binding and its first
// Accept, a daemon whose listen backlog is full while it sits out a shutdown
// grace, or any future arrangement in which the socket and the fleet stop
// being the same fact. The lock is the fact.
//
// The agent here is a real process leading its own group whose command line
// carries the session id, so verifyAgent says yes about it - which means a
// reaper that ran at all would SIGKILL it. Nothing else in this test would
// stop it: no socket exists, so listen's live probe has nothing to find.
func TestADaemonWillNotStartOrReapWhileAnotherHoldsTheLock(t *testing.T) {
	sock := tempSocket(t)
	agent := startLingererCarrying(t, idAlpha)

	// A roster the way a daemon that is still running wrote it.
	writeRoster(t, rosterPath(sock), []record{{
		ID:      idAlpha,
		Name:    "sydney",
		PID:     agent.Process.Pid,
		Started: time.Now().Add(-time.Hour),
	}})

	held, err := takeLock(lockPath(sock))
	if err != nil {
		t.Fatalf("take the lock: %v", err)
	}
	if !held.exclusive {
		t.Fatalf("the lock was not granted (%v), so this test never entered the state it is about", held.why)
	}
	t.Cleanup(func() {
		if err := held.release(); err != nil {
			t.Errorf("release: %v", err)
		}
	})

	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("something is at %s: the point of this test is that the socket is no evidence", sock)
	}

	// Bounded, so a Serve that wrongly starts fails this test instead of
	// blocking in its accept loop until the package timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	serveErr := Serve(ctx, sock)

	// The harm first, because it is the harm. By the time Serve has returned,
	// a reaper that ran did its killing at startup.
	select {
	case <-agent.died:
		t.Fatalf("a second daemon reaped a live agent (%v) while its daemon was still holding the lock: at 15-30 sessions that is the whole fleet, mid-Edit", agent.why)
	default:
	}

	if serveErr == nil {
		t.Fatal("Serve returned nil while another daemon held the lock: two daemons are now writing one roster")
	}
	if !strings.Contains(serveErr.Error(), "already running") {
		t.Errorf("Serve = %v, want it to say a daemon is already running", serveErr)
	}
	// A daemon that refuses must leave no trace. Everything destructive in
	// Serve is downstream of the lock, so a roster emptied here would mean the
	// order is wrong even though nothing was signalled.
	if recs := loadRoster(rosterPath(sock)); len(recs) != 1 {
		t.Errorf("roster after a refused start = %+v, want the live daemon's entry untouched", recs)
	}
}

// The one way to defeat the lock, named in lock_unix.go: delete the file under
// a running daemon. The flock lives on behind the descriptor, but daemon.lock
// no longer points at the inode it was taken on - so a successor's takeLock
// creates and locks a *different* inode, comes up believing it holds the lock,
// and its reaper SIGKILLs this daemon's fleet.
//
// It is not a contrived deletion. CLAUDE.md tells developers to put the socket,
// and so the lock beside it, under $(mktemp -d) - which is exactly where /tmp's
// periodic sweep runs, on a schedule, deleting files it finds untouched.
//
// The defence is on the incumbent: it holds the lock by descriptor and
// re-verifies the inode on its liveness tick, re-establishing the file when it
// has vanished. So after a sweep and one re-check, a second start must still
// refuse - and, crucially, must not reap. The lingerer here is a stand-in for a
// live agent: it leads its own group and carries the session id in its argv, so
// verifyAgent says yes about it and a reaper that ran would kill it.
func TestASweptLockIsReestablishedSoASecondDaemonWillNotReap(t *testing.T) {
	sock := tempSocket(t)
	agent := startLingererCarrying(t, idAlpha)

	// A roster the way a daemon that is still running wrote it.
	writeRoster(t, rosterPath(sock), []record{{
		ID:      idAlpha,
		Name:    "sydney",
		PID:     agent.Process.Pid,
		Started: time.Now().Add(-time.Hour),
	}})

	held, err := takeLock(lockPath(sock))
	if err != nil {
		t.Fatalf("take the lock: %v", err)
	}
	if !held.exclusive {
		t.Fatalf("the lock was not granted (%v), so this test never entered the state it is about", held.why)
	}
	t.Cleanup(func() {
		if err := held.release(); err != nil {
			t.Errorf("release: %v", err)
		}
	})

	// The periodic sweep deletes the lock file while the daemon that holds it
	// keeps running. The inode survives behind held's descriptor; only its name
	// is gone.
	if err := os.Remove(lockPath(sock)); err != nil {
		t.Fatalf("simulate the temp reaper deleting the lock: %v", err)
	}

	// The liveness tick re-verifies the inode and re-establishes the claim on a
	// fresh file. Without this, the sweep has defeated the lock.
	held.verify()

	// The second start, exactly as the existing lock test does it. No socket
	// exists, so listen's live probe finds nothing; the re-established lock is
	// the only thing that can refuse this daemon, and refusing is what keeps its
	// reaper from ever running.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	serveErr := Serve(ctx, sock)

	// The harm first, because it is the harm.
	select {
	case <-agent.died:
		t.Fatalf("a second daemon reaped a live agent (%v) after the lock file was swept from under the daemon holding it: a successor that locks a fresh inode believes it holds the lock, and at 15-30 sessions that is the whole fleet, mid-Edit", agent.why)
	default:
	}

	if serveErr == nil {
		t.Fatal("Serve returned nil after the swept lock was re-established: two daemons are now writing one roster")
	}
	if !strings.Contains(serveErr.Error(), "already running") {
		t.Errorf("Serve = %v, want it to say a daemon is already running", serveErr)
	}
	if recs := loadRoster(rosterPath(sock)); len(recs) != 1 {
		t.Errorf("roster after a refused start = %+v, want the live daemon's entry untouched", recs)
	}
}

// verify runs on every liveness tick, so its common case - the file it locked
// is still the file at the path - must cost nothing and disturb nothing. If it
// re-created the file each tick it would churn the inode a successor is meant to
// find, and the lock would flicker free for the width of each re-create.
func TestVerifyLeavesAHealthyLockUntouched(t *testing.T) {
	sock := tempSocket(t)

	held, err := takeLock(lockPath(sock))
	if err != nil || !held.exclusive {
		t.Fatalf("take the lock: %v (%v)", err, held.why)
	}
	t.Cleanup(func() { _ = held.release() })

	before, err := os.Stat(lockPath(sock))
	if err != nil {
		t.Fatalf("stat the lock: %v", err)
	}

	held.verify()

	after, err := os.Stat(lockPath(sock))
	if err != nil {
		t.Fatalf("stat the lock after verify: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Error("verify replaced the inode of a lock file that was never swept: every tick would churn the file a successor is meant to find")
	}
	// And it is still held: a takeLock must still be refused.
	if _, err := takeLock(lockPath(sock)); err == nil {
		t.Error("the lock was free after verify ran against a healthy lock")
	}
}

// The window verify cannot close: a successor slips in between the sweep and the
// incumbent's next tick, creating and locking a replacement inode. verify then
// finds a file at the path that it does not hold. It must leave the successor's
// inode alone - clobbering it would create a third file nobody holds and make
// the split brain worse - and must not adopt it as its own claim.
func TestVerifyDoesNotClobberASuccessorThatAlreadyReplacedTheLock(t *testing.T) {
	sock := tempSocket(t)

	held, err := takeLock(lockPath(sock))
	if err != nil || !held.exclusive {
		t.Fatalf("take the lock: %v (%v)", err, held.why)
	}
	t.Cleanup(func() { _ = held.release() })

	// The sweep, then a successor that locks a fresh inode before the incumbent
	// re-checks - the one ordering the incumbent's re-establish cannot prevent.
	if err := os.Remove(lockPath(sock)); err != nil {
		t.Fatalf("simulate the temp reaper: %v", err)
	}
	succ, err := takeLock(lockPath(sock))
	if err != nil || !succ.exclusive {
		t.Fatalf("the successor did not take the lock on a fresh inode: %v (%v)", err, succ.why)
	}
	t.Cleanup(func() { _ = succ.release() })
	successor, err := os.Stat(lockPath(sock))
	if err != nil {
		t.Fatalf("stat the successor's lock: %v", err)
	}

	held.verify()

	// The successor's inode is still the file at the path: verify neither
	// clobbered it with a third inode nor unlinked it.
	now, err := os.Stat(lockPath(sock))
	if err != nil {
		t.Fatalf("stat the lock after the incumbent's verify: %v", err)
	}
	if !os.SameFile(successor, now) {
		t.Error("verify clobbered a successor's replacement lock: the incumbent cannot reclaim a lock a live successor already holds, and remaking it a third time only deepens the split brain")
	}
	// And the incumbent did not adopt the successor's inode as its own claim.
	if held.info != nil && os.SameFile(held.info, now) {
		t.Error("the incumbent adopted the successor's inode: it would then believe it holds a lock a different daemon actually holds")
	}
}

// verify runs on the watchdog goroutine and release in Serve's shutdown defer.
// The WaitGroup wait that normally orders them is bounded, so a verify stuck on
// slow I/O can overlap release - the two must not race on l.file/l.info, and a
// verify that finishes after release must not resurrect the descriptor release
// just closed. Run under -race; the sweep first, so verify takes its mutating
// re-establish path rather than the read-only fast one.
func TestVerifyAndReleaseDoNotRace(t *testing.T) {
	sock := tempSocket(t)

	held, err := takeLock(lockPath(sock))
	if err != nil || !held.exclusive {
		t.Fatalf("take the lock: %v (%v)", err, held.why)
	}
	if err := os.Remove(lockPath(sock)); err != nil {
		t.Fatalf("simulate the temp reaper: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); held.verify() }()
	go func() { defer wg.Done(); _ = held.release() }()
	wg.Wait()

	// However the two interleaved, the claim is withdrawn and no descriptor was
	// left holding an flock: a fresh takeLock is granted rather than blocked by a
	// resurrected one.
	held.verify() // a no-op now, and it must not race the finished release either
	second, err := takeLock(lockPath(sock))
	if err != nil {
		t.Fatalf("the lock was not free after a concurrent verify and release: %v - a descriptor survived that should have been closed", err)
	}
	if err := second.release(); err != nil {
		t.Errorf("release the second lock: %v", err)
	}
}

// The end-to-end wiring: a real daemon, its lock swept, must re-establish the
// file on its own liveness tick - which is what proves s.lock.verify() is
// actually called on the tick and s.lock is set before the accept loop. None of
// the unit tests above exercises that path; a regression that dropped the call
// or moved the assignment would pass every one of them.
//
// It asserts on the file rather than a second takeLock, deliberately: a probing
// takeLock would itself lock a fresh inode and become the very successor this
// defends against. The swept inode lives on behind the daemon's descriptor, so
// a re-created one cannot reuse it - a file back at the path on a *different*
// inode is the daemon's own work and nothing else's.
func TestARunningDaemonReestablishesASweptLock(t *testing.T) {
	shortSilence(t, 150*time.Millisecond) // livenessInterval clamps to ~50ms ticks
	d := startDaemon(t)

	before, err := os.Stat(lockPath(d.socket))
	if err != nil {
		t.Fatalf("the running daemon has no lock file: %v", err)
	}

	// The periodic sweep deletes the lock out from under the live daemon.
	if err := os.Remove(lockPath(d.socket)); err != nil {
		t.Fatalf("simulate the temp reaper: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		after, serr := os.Stat(lockPath(d.socket))
		if serr == nil && !os.SameFile(before, after) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the daemon did not re-establish its lock file after it was swept: a successor would now lock a fresh inode and its reaper would SIGKILL this daemon's fleet")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// And it describes itself again, the way takeLock's explainLock does.
	said, err := os.ReadFile(lockPath(d.socket))
	if err != nil || !strings.Contains(string(said), "flock") {
		t.Fatalf("the re-established lock file does not describe itself: %v\n%s", err, said)
	}
}

// The lock is a possession and possessions are returned. A daemon that exited
// cleanly must not leave the next one unable to start - which is the failure
// mode a pid file has and the reason this is not one.
func TestTheLockIsAvailableAgainOnceTheDaemonHasGone(t *testing.T) {
	d := startDaemon(t)

	if _, err := takeLock(lockPath(d.socket)); err == nil {
		t.Fatal("the lock was free while a daemon was serving on that socket")
	}

	// It says on disk what it is. The file outlives every daemon by design, so
	// the one person who will ever meet it is somebody tidying ~/.wake - and
	// deleting it under a running daemon is the single way to defeat the lock.
	said, err := os.ReadFile(lockPath(d.socket))
	if err != nil {
		t.Fatalf("read the lock file: %v", err)
	}
	for _, want := range []string{"flock", "Do not delete it while a daemon is running"} {
		if !strings.Contains(string(said), want) {
			t.Errorf("the lock file does not mention %q, so the file beside the socket explains nothing to whoever finds it:\n%s", want, said)
		}
	}

	d.cancel()
	d.waitForExit(t)

	after, err := takeLock(lockPath(d.socket))
	if err != nil {
		t.Fatalf("take the lock after the daemon exited: %v", err)
	}
	if !after.exclusive {
		t.Fatalf("the lock was not granted after the daemon exited: %v", after.why)
	}
	if err := after.release(); err != nil {
		t.Errorf("release: %v", err)
	}
}
