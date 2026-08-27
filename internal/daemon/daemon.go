// Package daemon supervises the fleet and serves it over a unix socket.
//
// Its entire reason to exist is that closing the TUI must not kill the
// agents: shut the laptop, reattach later, the work carried on. Everything
// here follows from that one requirement. The daemon owns the processes and
// never renders; a client renders and never touches a process.
//
// # What must never happen
//
// One stalled client must not be able to freeze the fleet. Backpressure does
// not stop at this package - core's event buffer blocks its pump when full,
// which stops draining claude's stdout, which freezes the agent mid-turn. So
// a slow client is never allowed to slow a fan-out: frames are handed to a
// client with a non-blocking send and *dropped* when its queue is full, with
// the gap reported to that client and to nobody else. A bound would only make
// the freeze take longer to arrive. See client.go.
//
// Nothing in this package holds a mutex across a write to a client, for the
// same reason: rpc.WriteFrame serializes writes process-wide, so a socket
// that has stopped draining parks whoever is inside it. Each client gets its
// own writer goroutine and every write gets a deadline, which is the only
// thing that bounds that park.
//
// # The four verbs
//
// Spec §5 names four endings and they are not interchangeable:
//
//   - park - FramePark. The process is terminated and the UUID kept, so
//     `--resume` brings the conversation back. FrameWake is the other half,
//     and parkbook.go is what makes a parked session outlive this daemon.
//   - detach - the TUI exits and every session keeps running. Reaches the
//     daemon as nothing at all: the client just disconnects.
//   - stop - FrameStop. Closes the agent's stdin and lets the in-flight turn
//     finish. It does not signal. An agent killed mid-Edit leaves a
//     half-written file, and Wake is not entitled to do that to a repo.
//   - quit - FrameQuit. Stops every session, then the daemon exits.
//
// FrameKill is the fifth thing and is deliberately not one of the four: it is
// for an agent that has already stopped behaving, and it is a separate verb
// so it cannot happen by accident.
//
// FrameParkAll is the sixth, and it is the only one that is about the *fleet*:
// ⌃Q parks every session and then exits, so the next start can offer them back.
// It is a separate kind from FrameQuit rather than a mode on it because the two
// disagree about the park book - quit forgets it, this fills it - and no
// default for a missing mode is safe in either direction. See server.quitVerb.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"time"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// SocketEnv names the socket explicitly, overriding the default path.
//
// It exists because EnsureRunning forks the daemon and then dials it, and the
// two processes must agree on the path even if they would not derive the same
// one - a HOME that differs between them is otherwise a hang with no
// explanation. The forked daemon reads it through SocketPath, so a caller
// that does the ordinary thing gets the agreement for free.
const SocketEnv = "WAKE_SOCKET"

// stateDirPerm keeps ~/.wake to its owner. The socket is a control channel
// for every agent on the machine; a group-writable one is a way to talk to
// somebody else's fleet.
const stateDirPerm = 0o700

// daemonSubcommand is the argv the forked background process is started with.
// cmd/wake dispatches on it (Task 10); it lives here because EnsureRunning is
// the only thing that produces it.
const daemonSubcommand = "daemon"

// startTimeout bounds how long EnsureRunning waits for a forked daemon to
// start listening, and startPoll how often it looks.
const (
	startTimeout = 5 * time.Second
	startPoll    = 25 * time.Millisecond
)

// liveProbeTimeout bounds the dial that decides whether a socket file has a
// daemon behind it or is the debris of a crashed one.
const liveProbeTimeout = 200 * time.Millisecond

// maxSocketPath is the longest path a unix socket can be bound to.
//
// sun_path is a fixed array in the kernel's address struct - 104 bytes on
// darwin, 108 on Linux - so a path past it fails at bind with EINVAL, which
// surfaces as a bare "invalid argument". Worse, the bind happens in the forked
// daemon, whose stderr is /dev/null, so the client sees only "the daemon did
// not start listening" and has nothing to go on.
//
// The smaller of the two limits is used everywhere. It rejects a handful of
// paths between 103 and 107 bytes that Linux would have accepted, which is a
// deliberate trade: erring low costs a clear error on a path nobody chose by
// accident, and erring high costs the opaque failure this exists to prevent.
// The default ~/.wake/daemon.sock is nowhere near it, so this only ever meets
// someone who set $WAKE_SOCKET - exactly the person who will not guess EINVAL.
const maxSocketPath = 103

// stateDirName is the directory under $HOME that everything Wake keeps lives
// in, and socketFileName the socket inside one fleet's own directory.
const (
	stateDirName   = ".wake"
	socketFileName = "daemon.sock"
)

// SocketPath returns the default fleet's socket, creating its directory.
//
// Kept as the no-argument form because it is what every caller that does not
// care about fleets wants, and because $WAKE_SOCKET has to keep winning for
// EnsureRunning's sake. FleetSocketPath is the same thing with a name; see
// fleet.go for why a fleet is a directory.
func SocketPath() (string, error) { return FleetSocketPath(DefaultFleet) }

// checkSocketPath rejects a path no unix socket can be bound to, saying why.
func checkSocketPath(sock string) error {
	if len(sock) <= maxSocketPath {
		return nil
	}
	return fmt.Errorf("socket path is %d bytes and the limit is %d: %s\n"+
		"a unix socket path is a fixed-size field in the kernel, so this cannot be bound; set %s to something shorter",
		len(sock), maxSocketPath, sock, SocketEnv)
}

// Dial connects to a running daemon.
func Dial(socket string) (net.Conn, error) {
	return net.Dial("unix", socket)
}

// Serve listens on socket until ctx is cancelled, a client quits the daemon,
// or the last client leaves with no live session, then shuts down and returns.
//
// When it returns, nothing it started is still running: every goroutine is
// accounted for by the server's WaitGroup, which is what makes "the daemon
// exited" mean the fleet is not still out there. A daemon that returned while
// its agents ran would leave exactly the orphans the reaper below exists to
// clean up.
func Serve(ctx context.Context, socket string) error {
	leaseCtx, releaseLease, err := withTestParentLease(ctx)
	if releaseLease != nil {
		defer releaseLease()
	}
	if err != nil {
		return err
	}
	ctx = leaseCtx

	// The lock comes first, before the socket is touched and before a single
	// pid is read off the roster. It is the only claim on disk that cannot go
	// stale - the kernel drops it on process death including SIGKILL - so it
	// is the one thing the reaper is allowed to act on. See lock.go.
	//
	// Refusing here rather than after binding also means a daemon started by
	// mistake removes nothing and writes nothing: every destructive step is
	// downstream of holding this.
	lock, err := takeLock(lockPath(socket))
	switch {
	case errors.Is(err, errLockHeld):
		return fmt.Errorf("daemon already running on %s: %w", socket, err)
	case err != nil:
		// Distinguished rather than folded, so this stays true if takeLock
		// ever grows a third error. "Already running" is a claim about another
		// process, and only errLockHeld makes it.
		return fmt.Errorf("claim the daemon lock beside %s: %w", socket, err)
	}
	// Registered before every other defer, so it is released *last* - after
	// ln.Close() and after the belt-and-braces os.Remove below. Those two run
	// while the lock still stands, which is what stops the Remove from ever
	// being able to unlink a successor's socket file: there cannot be a
	// successor yet.
	defer func() {
		if err := lock.release(); err != nil {
			logf("wake: %v", err)
		}
	}()

	ln, err := listen(socket)
	if err != nil {
		return err
	}
	// Closing the listener unlinks the socket file, so it must happen *after*
	// shutdown has finished letting in-flight turns end - while the binding
	// stands, a second daemon started by mistake finds a live socket and
	// refuses, and clients keep finding a daemon on the path for the whole
	// grace. See stopAcceptingOnStop.
	//
	// Nothing runs between this and the lock's release, deliberately. There
	// used to be a belt-and-braces os.Remove(socket) here for a listener that
	// does not unlink; net.Listen's always does, listen() copes with a stale
	// file if one ever survived, and the only thing that Remove could still
	// reach was a *successor's* socket. Deleting it makes the EOF a client
	// sees on this listener the exact edge described on EnsureRunning.
	defer func() { _ = ln.Close() }()

	// Nothing this daemon logs may be able to park the daemon. If the caller
	// has not already put a sink behind log that cannot block, do it here:
	// the alternative is trusting cmd/wake to remember, and forgetting it
	// re-opens the process-wide wedge core/sessionlog.go describes.
	defer guardLog()()

	s := newServer(socket)
	// The liveness tick re-verifies this claim's inode (see watchLiveness), so
	// the daemon that holds the lock re-establishes it if the file is swept out
	// from under it - which is what stops a successor locking a fresh inode.
	s.lock = lock

	// Before accepting anything: an earlier daemon that was SIGKILLed left
	// its agents running in their own process groups with nobody holding a
	// handle. The session ids are on disk precisely so they are findable.
	//
	// Gated on the lock having actually been granted. "I could not take the
	// lock" is not "nobody else holds it", and the difference is 15-30
	// SIGKILLs.
	if lock.exclusive {
		s.reapOrphans()
	} else {
		logf("wake: %v, so anything a crashed daemon left running is left alone", lock.why)
	}

	// **Nothing is restored here, and that is the design.** The park book is
	// read on demand - fleet() carries it as Status.Parked and unpark launches
	// from a record - so a daemon that succeeds a ⌃Q comes up holding nothing.
	// Restoring put a roster row and an openable conversation in front of
	// somebody who had just quit the fleet, which is the opposite of what ⌃Q
	// means; /resume is how a session comes back, and it is a decision.

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	runErr := s.run(ctx, ln)
	return errors.Join(runErr, s.shutdown())
}

// listen binds the socket, clearing the debris of a crashed daemon first.
//
// The stale file is removed only after confirming nothing is listening on it,
// so a second daemon started by mistake fails rather than evicting the live
// one - unlinking a socket does not disturb the process bound to it, so the
// eviction would be silent and the fleet would be split between two daemons
// with one reachable.
//
// This is the second of two gates and no longer the one the reaper depends on.
// Its negative answer is ambiguous - ECONNREFUSED is both "nobody is there"
// and "the listen backlog is full" - which is fine for deciding whether to
// unlink a file and was never sound for deciding whether to SIGKILL a fleet.
// The lock Serve takes first is what answers that one.
func listen(socket string) (net.Listener, error) {
	if _, err := os.Stat(socket); err == nil {
		if c, derr := net.DialTimeout("unix", socket, liveProbeTimeout); derr == nil {
			_ = c.Close()
			return nil, fmt.Errorf("daemon already running on %s", socket)
		}
		if err := os.Remove(socket); err != nil {
			return nil, fmt.Errorf("remove stale socket %s: %w", socket, err)
		}
	}
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", socket, err)
	}
	return ln, nil
}

// EnsureRunning dials the daemon, forking one if nothing is listening.
//
// The fork is detached - its own session, no controlling terminal - because a
// daemon that stayed in the TUI's process group would die with the ^C that is
// supposed to mean "detach". That is the whole product in one flag.
//
// # A connection this returns can be silent for up to quitGrace + shutdownWait
//
// The ordinary motion "quit, then start again" now blocks, and the next caller
// should know why rather than rediscover it. A quitting daemon holds its
// listener bound for the whole of its shutdown - that is what stops a second
// daemon's reaper reaching a fleet still being stopped gently - and the kernel
// keeps completing connections into the listen backlog while nobody is
// accepting. So Dial succeeds throughout, this returns on the first success,
// and the caller then waits for a hello that no accept loop will ever send.
// Status has the same shape and degrades to an i/o timeout after
// statusTimeout.
//
// There is an exact discriminator, and it is better than a timer:
//
//   - run queues FrameHello before publishing the accepted client (server.go),
//     so a connection that was never accepted never sees one.
//     Silence on a fresh connection therefore means "accepted by the kernel,
//     not by a daemon" - it is not a slow daemon.
//   - Serve's defers run after shutdown() has returned. shutdown() writes the
//     park book (or clears it, on `wake stop`) *before* it closes its clients,
//     and its last act is s.roster.clear(). ln.Close() unlinks the socket and
//     delivers EOF to everyone in the backlog, and lock.release() is the only
//     thing after it.
//
// So the EOF on that connection is not merely a signal that something ended:
// it is exactly the edge at which starting a new daemon becomes safe, because
// by the time it arrives the roster is provably empty **and the park book is
// provably complete** - which is what the next daemon reads to know which
// sessions it can offer back - and the lock is released in the next statement.
// A client that waits for hello-or-EOF and re-dials on EOF cannot reap a live
// fleet, cannot hang, and cannot restore half a fleet.
//
// ⌃Q is what put real work in front of that edge: rpc.FrameParkAll parks N
// sessions on the way out, which takes as long as N stops take. It does not
// move the edge, because all of it happens inside shutdown() and shutdown()
// returns before any of these defers run - and the wait it lengthens is the
// same quitGrace + shutdownWait the paragraph above already bounds.
//
// Building that wait is cmd/wake's job (Task 10), not this function's - it is
// a policy about how long a person is willing to stare at a terminal, and it
// belongs where the terminal is.
//
// # What a forked daemon that refuses looks like from here
//
// Nothing. The fork's stdio is /dev/null and this lets go of the process, so a
// child that exited at takeLock because another daemon still holds the lock is
// indistinguishable from one that crashed: both surface as the timeout below.
// That is a decision rather than an oversight, and the reasoning is that both
// want the same response anyway - the lock being held means a daemon exists and
// is either serving (so Dial is about to succeed) or finishing (so it is about
// to release), and each resolves by continuing to poll, which is what this
// already does for startTimeout. The timeout message says so.
//
// What it costs is a *misleading* message in the one case that will not
// resolve: a daemon wedged past startTimeout, reported as "did not start
// listening" when one is very much there. Closing that means the child
// reporting its refusal - an exit status this process waits for, or a pipe it
// holds - which is a change to how the fork is launched rather than a line
// here. Recorded rather than done.
func EnsureRunning(ctx context.Context, socket string) (net.Conn, error) {
	if conn, err := Dial(socket); err == nil {
		return conn, nil
	}
	if err := fork(socket); err != nil {
		return nil, err
	}

	deadline := time.Now().Add(startTimeout)
	for time.Now().Before(deadline) {
		if conn, err := Dial(socket); err == nil {
			return conn, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("waiting for the daemon: %w", ctx.Err())
		case <-time.After(startPoll):
		}
	}
	return nil, fmt.Errorf("daemon did not start listening on %s within %v; if one is shutting down it holds the lock beside that socket until it has finished, and starting again after that works", socket, startTimeout)
}

// fork starts the background daemon and lets go of it.
func fork(socket string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate wake binary: %w", err)
	}
	cmd := exec.Command(exe, daemonSubcommand)
	// nil is /dev/null. The daemon outlives this terminal, so writing to
	// its stdout would eventually be a write to a closed pty; what it has
	// to say goes to its own log (see OpenLog).
	cmd.Stdout, cmd.Stderr, cmd.Stdin = nil, nil, nil
	cmd.Env = append(os.Environ(), SocketEnv+"="+socket)
	leaseFile, err := passTestParentLease(cmd)
	if err != nil {
		return err
	}
	if leaseFile != nil {
		defer func() { _ = leaseFile.Close() }()
	}
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	// Wait reaps an early refusal. The parked goroutine does not keep this
	// client alive, and the detached daemon still outlives it.
	//
	// notice rather than logf: fork runs in the *client*, which installs
	// neither guardLog nor OpenLog, so the standard logger there writes
	// straight to the stderr the alt screen is drawn on.
	go func() {
		err := cmd.Wait()
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			notice.Report("wake: wait for forked daemon: %v", err)
		}
	}()
	return nil
}

// Status reports what is running. It answers three different questions with
// one call, and the third is the one that is easy to leave out: a daemon that
// answers, a machine with no daemon, and a machine whose daemon died leaving
// its agents behind. The last is not "nothing is running" - it is 15-30
// processes nobody is holding.
//
// **There is a fourth shape and it arrives as an error rather than as an
// answer**: a daemon that is *there* and will not speak, which is what a
// graceful shutdown looks like from outside. It keeps its listener bound for the
// whole quit grace, so the kernel completes the dial below into its backlog and
// nothing ever replies. That is a real error and it is reported as one - a
// caller that must not guess, like `wake stop`, has to know it could not ask -
// but a caller that only wants the best available answer should read the disk,
// which is FleetOnDisk, and is what this returns above when the dial fails.
//
// It returns on rpc.FrameStatusReply and never on rpc.FrameStatusPush, which
// is the whole reason those are two kinds. The daemon announces a state change
// to every attached client the moment it happens, so a push can be sitting in
// the socket between the hello and this function's request - and returning it
// would answer the question with a report assembled before it was asked.
func Status(socket string) (rpc.Status, error) {
	conn, err := Dial(socket)
	if err != nil {
		return FleetOnDisk(socket), nil
	}
	if err := conn.SetDeadline(time.Now().Add(statusTimeout)); err != nil {
		_ = conn.Close()
		return rpc.Status{}, fmt.Errorf("set status deadline: %w", err)
	}
	if err := rpc.WriteFrame(conn, rpc.Frame{Kind: rpc.FrameStatus}); err != nil {
		_ = conn.Close()
		return rpc.Status{}, fmt.Errorf("ask for status: %w", err)
	}

	frames, errs := rpc.ReadFrames(conn)
	// Closed and drained to completion on every path out of here, including
	// the early return below: abandoning frames while the reader still has
	// data parks that goroutine on a send forever, and closing the
	// connection does not release one already parked there.
	defer func() {
		_ = conn.Close()
		for range frames {
		}
		<-errs
	}()
	for f := range frames {
		if f.Kind == rpc.FrameStatusReply && f.Status != nil {
			return *f.Status, nil
		}
	}
	if err := <-errs; err != nil {
		return rpc.Status{}, fmt.Errorf("read status: %w", err)
	}
	return rpc.Status{}, errors.New("daemon closed the connection without answering")
}

// statusTimeout bounds the whole exchange. `wake status` is the command
// someone runs *because* something is wrong, so it must not be the second
// thing that hangs.
const statusTimeout = 3 * time.Second

// fleetProbeBudget bounds FleetOnDisk's whole liveness sweep, the way
// statusTimeout bounds the live path. FleetOnDisk walks the roster running one
// ps per record, and a broken ps can hang each for probeTimeout - so N records
// with no deadline over the loop is N×probeTimeout serial, on bare `wake`'s
// front door when the daemon is gone. One shared deadline makes the total a
// single probe's cost instead. A probe that overruns it drops its record, which
// is the safe direction for a status view - FleetOnDisk asserts identity, never
// liveness - so it is set to statusTimeout: the dead-daemon path gets the same
// ceiling the live one has, generous enough that only an already-broken ps
// reaches it.
//
// It is a var only so tests can compress it; nothing outside a test assigns it.
var fleetProbeBudget = statusTimeout

// FleetOnDisk reports the fleet as the files beside the socket describe it: the
// live trees a dead daemon left behind, read off the roster, and the sessions it
// parked, read off the park book.
//
// The two lists answer opposite questions and both belong here. An orphan is a
// process nobody is holding; a parked session is a transcript nobody can find
// the id for, which is the state this file's second half now exists to end.
//
// They are disjoint by construction - completePark removes the roster entry as
// it writes the book - and if they ever overlap the roster row wins, because a
// process that is verifiably alive is not parked. The overlap is reachable only
// through a failed roster write, which completePark logs and carries on from.
//
// # Why it is exported, and what that is not permission for
//
// Status returns it when the dial fails, and **a caller that got an error out of
// Status has the same question**: a daemon in graceful shutdown holds its
// listener for the whole quit grace, so the dial lands in a backlog nothing is
// accepting from and the answer never comes. Bare `wake` asks it there, because
// the alternative is reading a timeout as "nothing is running" and spawning a
// fresh agent beside a fleet somebody just parked.
//
// It asserts **identity and location and nothing about liveness**, exactly as
// parkbook.go's header says a restored row does - the roster half is filtered by
// which pids still exist, and the park book half is a claim by a process that is
// no longer running. Nothing may resume, signal or count anything as *running*
// from this; the daemon re-proves what it needs through resumeSafe.
//
// The roster half is filtered under **one deadline over the whole sweep**
// (fleetProbeBudget), not one per record - because this is bare `wake`'s front
// door, and one ps per record with no ceiling is N×probeTimeout serial when a ps
// hangs. A record whose probe overruns the budget is **dropped**: an
// under-report, which is the safe direction for a view that already asserts
// identity and never liveness - a missing row, never a signal sent - and set
// generously (statusTimeout) so only an already-broken ps reaches it. A drop
// that came from the budget rather than a dead process sets **ProbeIncomplete**,
// the fail-closed signal a consumer asserting a negative - `wake stop`'s reading
// of runningCount as "nothing is running" - must respect, so an under-count
// cannot be mistaken for an answer.
func FleetOnDisk(socket string) rpc.Status {
	st := rpc.Status{Socket: socket}
	seen := map[string]bool{}
	// One deadline over the whole sweep, not one per record: each r.alive is a ps
	// a broken one can hang for probeTimeout, and successive probes share what is
	// left of this budget rather than each getting probeTimeout afresh. Breaking
	// when it is spent drops the records not yet reached - the safe direction for
	// a report that asserts identity, never liveness. See fleetProbeBudget.
	ctx, cancel := context.WithTimeout(context.Background(), fleetProbeBudget)
	defer cancel()
	for _, r := range loadRoster(rosterPath(socket)) {
		if ctx.Err() != nil {
			// The budget is spent with records still unprobed. They are dropped,
			// so say the sweep is not authoritative rather than let a caller read
			// the short roster as "nothing is running". See ProbeIncomplete.
			st.ProbeIncomplete = true
			break
		}
		if !r.alive(ctx) {
			// A genuinely dead process answers false with the budget intact and is
			// skipped as ever. A false that coincides with a spent budget is a probe
			// the deadline cut short - that record is unverified, so the sweep is not
			// authoritative and stops here. The one case ctx.Err() cannot separate is
			// a record answering dead in the instant the budget expires; it errs to
			// incomplete, which is fail-safe (a spurious "could not confirm", never a
			// false-down). A race-free split needs inspect's dead-vs-aborted outcome
			// up through the reaper's verifyAgent, out of this change's scope.
			if ctx.Err() != nil {
				st.ProbeIncomplete = true
				break
			}
			continue
		}
		seen[r.ID] = true
		st.Sessions = append(st.Sessions, rpc.SessionStatus{
			ID: r.ID, Name: r.Name, Label: r.Label, PID: r.PID, State: rpc.StateOrphaned,
		})
	}
	// On Parked rather than Sessions, and no PID: every reader of the list above
	// treats a pgid as something to go and look at, and a parked session's is
	// gone. The separation is what lets bare `wake` tell "a fleet to reopen"
	// from "a book to resume out of".
	for _, ps := range parkedStatuses(loadParkBook(parkBookPath(socket))) {
		if seen[ps.ID] {
			continue
		}
		st.Parked = append(st.Parked, ps)
	}
	return st
}
