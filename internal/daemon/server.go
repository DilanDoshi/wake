// The server: the socket's accept loop, the client connections on it, and
// the dispatch from a frame to a verb. What a session is and how its
// liveness is judged is agent.go; how a frame reaches a client without a
// slow client freezing an agent is client.go.

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// quitGrace is how long a quitting daemon waits for stopped sessions to
// finish their in-flight turns before it kills what is left.
//
// It is a compromise between two things Wake owes the operator and cannot owe
// both of: an agent mid-Edit must be allowed to finish the edit, and the
// daemon must not still be alive minutes after `wake stop`. Exiting early is
// the worse failure of the two - it leaves process trees with nobody holding
// a handle - so the grace is generous and the ending is a group kill rather
// than a shrug.
// It is a var only so tests can compress it; nothing outside a test assigns it.
var quitGrace = defaultQuitGrace

const defaultQuitGrace = 30 * time.Second

// agentSettle is how often shutdown re-checks whether the sessions it
// stopped have actually ended.
const agentSettle = 20 * time.Millisecond

// shutdownWait bounds the last wait of all: everything this server started,
// after every session has ended or been killed. Short, because by this point
// nothing legitimate is still working.
const shutdownWait = 5 * time.Second

// server holds the fleet and the clients watching it.
type server struct {
	socket string
	roster *rosterFile

	// lock is this daemon's claim on the state directory, held by descriptor
	// and set by Serve before the accept loop starts. The liveness tick
	// re-verifies its inode, so a sweep of the lock file cannot silently hand a
	// successor an exclusive claim and its reaper the fleet - see lock_unix.go.
	lock *lockfile

	// parked is what this daemon can offer back. Beside roster rather than
	// inside it - see parkbook.go's header for why they are two files.
	parked *parkBook

	// names is which display names the live fleet is using. It has its own
	// lock rather than living under s.mu, because a name is reserved before
	// the agent it belongs to exists: the name becomes the child's --name, so
	// it is claimed before core.NewSession and the agent does not enter
	// s.agents until that process has started. See names.go.
	names *nameRegistry

	// mu guards the maps, debug files, quitting and taken, and nothing else. It
	// is never held across a write to a client, a write to an agent's stdin, or
	// any other operation that can block: the failure that has appeared three
	// times in this project is a lock held across a blocking call, and each
	// time the operation that most needed the lock was "kill this thing".
	//
	// Handing a frame to a client *is* done under this lock, and that is
	// safe only because client.enqueue is a non-blocking send by
	// construction. If it ever grows a blocking path, this comment is
	// wrong and the fleet is frozen.
	mu         sync.Mutex
	agents     map[string]*agent
	clients    map[*client]struct{}
	debugFiles map[string]string // session id to its normalized debug path
	quitting   bool

	// taken is set by takeAgents when shutdown snapshots the fleet. An agent
	// admitted after that snapshot enters a map nothing reads again - its
	// process outlives the grace, the kill and the roster clear - so register
	// and replaceParked refuse while it is set. Written and read under mu,
	// which is what makes the refusal one step with the snapshot.
	taken bool

	// admitMu makes the live count and taking a row one step - see admitLive.
	// A second mutex rather than mu because the count reads each agent's
	// snapshot, which takes a.mu, and mu is never held across one. What is
	// taken while holding it: mu, a.mu, and the park book's own mutex (admit
	// and admitRefusal read the book); nothing that holds any of those ever
	// takes admitMu, so the order cannot reverse.
	admitMu sync.Mutex

	// verb is why this daemon is ending, and it is only meaningful once
	// quitting is true: the zero value is quitNone, which is what a daemon
	// nobody asked to end is doing. Written once by beginQuit and read once by
	// shutdown; both go through the lock, because the writer is a client's
	// dispatch goroutine and the reader is Serve's.
	verb quitVerb

	// recent is the last few endings, kept so a status report can still
	// account for them.
	//
	// Announcing an ending once is not enough: that frame goes through the
	// same fan-out as every other, and a client that had fallen behind has
	// its frames dropped by design - so the one frame carrying *why* a
	// session ended is exactly as droppable as any other, and nothing it
	// asks afterwards can recover it. A row would vanish with no reason,
	// permanently. Bounded because it is a courtesy, not a log.
	recent []rpc.SessionStatus

	wg   sync.WaitGroup
	quit chan struct{}
	done chan struct{}
}

func newServer(socket string) *server {
	return &server{
		socket:     socket,
		roster:     newRosterFile(rosterPath(socket)),
		parked:     newParkBook(parkBookPath(socket)),
		names:      newNameRegistry(),
		agents:     make(map[string]*agent),
		clients:    make(map[*client]struct{}),
		debugFiles: make(map[string]string),
		quit:       make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// start runs f on a tracked goroutine. Everything this package spawns goes
// through here, so shutdown's Wait is a complete account rather than a hope.
func (s *server) start(f func()) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		f()
	}()
}

// run accepts connections until the listener closes.
func (s *server) run(ctx context.Context, ln net.Listener) error {
	defer close(s.done)
	s.start(func() { s.stopAcceptingOnStop(ctx, ln) })
	s.start(func() { s.watchLiveness(ctx) })

	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.stopping(ctx) || errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrDeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("accept on %s: %w", s.socket, err)
		}
		c := newClient(conn)
		c.enqueue(rpc.Frame{Kind: rpc.FrameHello})
		s.addClient(c)
		if c.closed() {
			continue
		}
		s.start(func() { s.serveClient(ctx, c) })
	}
}

// stopAcceptingOnStop unblocks Accept when the daemon is asked to end, and
// retires with run either way so it cannot outlive the server it is watching.
//
// It sets a deadline rather than closing, and that is the whole of C1.
// Closing a net.UnixListener *unlinks the socket file*, and shutdown then
// takes up to quitGrace letting in-flight turns finish while the roster still
// names every one of those agents. In that window `wake` finds no socket,
// forks a second daemon, and its reaper reads that roster and SIGKILLs 15-30
// agents mid-Edit - reached by the most ordinary motion there is, "quit, then
// start again". Holding the binding closes it at the source: the second
// daemon's live probe connects, so it refuses to start at all, so the reaper
// never runs and there are never two daemons over one roster.
//
// A deadline is what makes that possible. There is no other way to unblock
// Accept on a unix listener, and the kernel keeps completing connections into
// the backlog while nobody accepts - which is exactly the answer a live probe
// needs. Serve closes the listener at the end, where the unlink belongs.
func (s *server) stopAcceptingOnStop(ctx context.Context, ln net.Listener) {
	select {
	case <-ctx.Done():
	case <-s.quit:
	case <-s.done:
	}
	// A listener that cannot express a deadline leaves closing as the only
	// way to end the accept loop, and with it the window above.
	type deadliner interface{ SetDeadline(time.Time) error }
	if dl, ok := ln.(deadliner); ok {
		_ = dl.SetDeadline(time.Now())
		return
	}
	logf("wake: this listener cannot stop accepting without closing; a restart during shutdown could reap a live fleet")
	_ = ln.Close()
}

// stopping reports whether this daemon is on its way out, by any of the three
// routes there are: the context cancelled, a client's quit, or the accept loop
// already finished.
//
// All three, and s.done was the one missing. run closes it and Serve then
// calls shutdown, which empties the agent map - so on the one path where
// neither of the other two is set (an accept error that is not a clean stop) a
// spawn dispatched afterwards would start a process shutdown had already
// walked past, with nothing left to stop it. watchLiveness selects on all
// three; the discrepancy was a bug waiting to become reachable.
func (s *server) stopping(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	select {
	case <-s.quit:
		return true
	case <-s.done:
		return true
	default:
		return false
	}
}

// quitVerb is why this daemon is ending, which decides what happens to the
// sessions it holds and to the ones it has already parked.
//
// Three values because only two endings are verbs. A signal and an empty
// daemon are both no-verb exits: neither may park a fleet nobody asked to park
// nor forget one somebody already did.
type quitVerb int

const (
	// quitNone is context cancellation or the daemon becoming empty. Sessions
	// are stopped, the park book is left exactly as it is. It is the zero value
	// because nobody asked for either exit with a verb.
	quitNone quitVerb = iota

	// quitStop is FrameQuit, which is `wake stop`. Every session ends and the
	// park book is cleared: stop is the ending with no way back, and leaving
	// a parked fleet behind would give it one by accident.
	quitStop

	// quitPark is FrameParkAll, which is ⌃Q. Every session is parked on the
	// way out and written down, so the next start can offer them back.
	quitPark
)

// beginQuit is the quit verb: end the accept loop, and record why. The
// stopping happens in shutdown, which is the one path out of Serve and so the
// one place the ordering has to be right.
//
// The first verb wins. Two clients quitting at once is a race nobody can
// resolve into a third meaning, and the safe end is that a park does not
// become a stop - so whichever arrived first is what this daemon is doing.
func (s *server) beginQuit(v quitVerb) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quitting {
		return
	}
	s.quitting, s.verb = true, v
	close(s.quit)
}

// quitReason is the verb this daemon is ending for, or quitNone if nobody asked
// it to end at all.
func (s *server) quitReason() quitVerb {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verb
}

// shutdown ends every session, then every client, then waits for everything
// this server started.
//
// The order is the contract. Sessions are stopped the gentle way first -
// stdin closed, in-flight turn allowed to finish - because a daemon exit is
// not permission to corrupt a repo. Only what is still alive after the grace
// is killed, and it is killed by process group, because an agent is a tree.
//
// The quit verb decides two things here and nothing else does: whether each
// stop is labelled a park on the way in, and what happens to the park book on
// the way out. Both are read from one `verb`, taken once at the top, so a
// second client's quit landing mid-shutdown cannot make this daemon change its
// mind halfway through a fleet.
func (s *server) shutdown() error {
	verb := s.quitReason()
	agents := s.takeAgents()
	for _, a := range agents {
		if verb == quitPark {
			// Before the stop, never after: retire runs the moment the events
			// channel closes, which can be before this loop's next iteration.
			// It is set on unreachable ones too: OS-confirmed reclaim preserves
			// it below, while an ordinary grace-expiry kill clears it - see
			// bookParked. A session that is *already* parked is
			// refused by beginPark itself, which is what keeps markParked's
			// invariant true across this call.
			a.beginPark()
		}
		if a.reclaimingNow() {
			// The grace exists so an in-flight turn can finish. This agent
			// has no process left to finish one, so waiting is unnecessary.
			// ParkAll must preserve the label beginPark just installed: kill
			// clears it on a not-yet-retired agent. Reclaim's endProcess is
			// idempotent and lets retire/completePark decide the parked ending.
			if a.endForShutdown(verb == quitPark) {
				logf("wake: session %s has already lost its process; completing its reclaim as a park", a.id)
			} else {
				logf("wake: session %s has already lost its process; killing its group rather than waiting", a.id)
			}
			continue
		}
		if err := a.stop(); err != nil {
			logf("wake: stopping session %s: %v", a.id, err)
		}
	}
	remaining := waitForAgents(agents, quitGrace)
	for _, a := range remaining {
		logf("wake: session %s did not end within %v of being stopped; ending its process group", a.id, quitGrace)
		a.endForShutdown(verb == quitPark)
	}

	// The park book's two arms are in different places, and the asymmetry is
	// the rule rather than an accident: **an entry that must exist has to be
	// written before anybody can read the book, and an entry that must not
	// exist has to be removed after anything that could still write one.**
	//
	// This is the first half. Before closeClients, which is EnsureRunning's
	// edge from the inside: a client that sees this connection end - and, a
	// moment later, a `wake` that sees the EOF on the listener - reads the park
	// book next, so anything written after this point is written after somebody
	// has read it. Writing it early costs nothing, because completePark may
	// write the same records again from a fan-out goroutine afterwards and a
	// duplicate of an entry that is already there changes nothing.
	//
	// TestTheParkBookIsWrittenEarlyAndForgottenLate holds both orderings
	// statically, because a test that reads the book at an edge proves the end
	// state and not the order two statements ran in.
	switch verb {
	case quitPark:
		s.bookParked(agents)
	case quitStop, quitNone:
		// The forget is below, after the wait; a no-verb exit decides nothing.
		// Named rather than left to a default so a fourth verb has to come here
		// and say which half of this it belongs to.
	}

	s.closeClients()
	if !waitFor(&s.wg, shutdownWait) {
		// Bounded rather than a bare Wait, and the bound is not a
		// formality: an agent's input goroutine can be parked inside a
		// write to a stdin that a descendant outside the process group
		// still holds - core documents that as the one park neither Stop
		// nor a group kill reaches. An unbounded Wait there means Serve
		// never returns and the daemon never exits, which is strictly worse
		// than a goroutine that dies with the process a moment later.
		logf("wake: shutting down with goroutines still running after %v; they end with the process", shutdownWait)
	}

	// The second half, and it is here rather than beside the first because a
	// forget has the opposite constraint to a write. completePark runs on a
	// fan-out goroutine, after core's Wait returns, and *adds* an entry - so a
	// clear placed before that wait can be overtaken by a park that was still
	// finishing, and `wake stop` would leave behind exactly the one session the
	// operator ran it to be rid of. The wait above is what proves no such write
	// is still coming; it is bounded, so a shutdown that logs the line above has
	// not proved it - the same residual that bound already carries.
	//
	// It is still upstream of the EOF a client turns into "start a new daemon":
	// ln.Close() is one of Serve's defers and this returns first.
	switch verb {
	case quitStop:
		// `wake stop` is the deliberate ending and it ends the parked half
		// too. See parkBook.clear.
		if err := s.parked.clear(); err != nil {
			logf("wake: could not clear the park book: %v", err)
		}
	case quitPark, quitNone:
		// ⌃Q wrote the book above and a no-verb exit preserves it, but both
		// must finish any add/deletion update still dirty in memory. This
		// is after the tracked finalizer wait so retry writes the last held
		// generation, and before Serve releases the listener so a successor
		// cannot read the stale file first.
		if _, err := s.parked.retryPending(); err != nil {
			logf("wake: pending park-book update could not be retried during shutdown: %v", err)
		}
	}

	// Last, and only now: every agent this daemon owned is gone, so a
	// roster entry left behind would send the next daemon hunting for
	// process groups that no longer exist.
	//
	// The roster goes unconditionally and the park book does not, and the
	// asymmetry is the whole ruling. A roster entry outliving its process is a
	// pgid a later reaper would hunt for, so it is debris however the daemon
	// died. The book's entries name transcripts that are still on disk and
	// processes that are already gone, so they cost nothing to keep and
	// everything to lose - which is why the question above is "which verb"
	// rather than "did this daemon exit".
	return s.roster.clear()
}

// waitFor waits on wg, reporting whether everything retired within d.
func waitFor(wg *sync.WaitGroup, d time.Duration) bool {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// takeAgents empties the map and returns what was in it, so shutdown works on
// a stable set with no lock held.
func (s *server) takeAgents() []*agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	// In the same locked step as the snapshot: a register that runs after this
	// lock releases sees the flag, so no agent can enter the emptied map.
	s.taken = true
	out := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		out = append(out, a)
	}
	s.agents = make(map[string]*agent)
	return out
}

// waitForAgents returns the agents that had not ended within grace.
func waitForAgents(agents []*agent, grace time.Duration) []*agent {
	deadline := time.Now().Add(grace)
	for {
		var alive []*agent
		for _, a := range agents {
			if !a.finished() {
				alive = append(alive, a)
			}
		}
		if len(alive) == 0 || !time.Now().Before(deadline) {
			return alive
		}
		time.Sleep(agentSettle)
	}
}

func (s *server) closeClients() {
	s.mu.Lock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.clients = make(map[*client]struct{})
	s.mu.Unlock()

	for _, c := range clients {
		c.close()
	}
}

// serveClient runs one connection: a hello, then frames until it hangs up.
func (s *server) serveClient(ctx context.Context, c *client) {
	s.start(c.write)
	defer s.dropClient(c)

	frames, errs := rpc.ReadFrames(c.conn)
	// Ranged to completion, never returned out of. rpc.ReadFrames has no
	// cancellation: its channels close only when the reader is drained, so
	// an early return parks that goroutine on a send for good. Ending this
	// loop early means closing the connection, which is what c.close does.
	for f := range frames {
		s.dispatch(ctx, c, f)
	}
	// A client that vanished is the ordinary case and reads as a clean EOF.
	// A connection this daemon closed under its own reader is an error by
	// the time rpc sees it, and saying so on every shutdown would bury the
	// one case worth reading: a client that stopped speaking the protocol.
	if err := <-errs; err != nil && !c.closed() {
		logf("wake: client on %s: %v", s.socket, err)
	}
}

func (s *server) addClient(c *client) {
	s.mu.Lock()
	if s.quitting {
		s.mu.Unlock()
		c.close()
		return
	}
	s.clients[c] = struct{}{}
	s.mu.Unlock()
}

func (s *server) dropClient(c *client) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
	c.close()
	s.reconsiderEmptyExit()
}

// reconsiderEmptyExit commits an idle daemon to quitting when no agent needs
// it. Admission and the decision share admitMu; client attach and the
// commitment share mu, so neither can enter after the decision wins.
func (s *server) reconsiderEmptyExit() {
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	published, err := s.parked.retryPending()
	if err != nil {
		logf("wake: pending park-book update could not be retried: %v", err)
	} else {
		for id, rec := range published {
			if a, held := s.agent(id); held {
				a.markParkDurable(rec)
			}
		}
	}
	if s.needsSupervision() {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quitting || len(s.clients) != 0 {
		return
	}
	s.quitting, s.verb = true, quitNone
	close(s.quit)
}

// dispatch routes one client frame to one verb.
//
// Nothing here may block. Send, AllowTool, AnswerQuestion and DenyTool all
// write to an agent's stdin, which parks for good against a child that stopped
// draining it - and this goroutine is the one carrying that client's *kill*
// frame. So they are handed to the agent's own input goroutine and this
// returns immediately.
//
// Stop goes through that same queue, and the ordering is the reason. Stop
// means "finish what I have given you, then end", so it has to arrive behind
// the messages already queued for that agent - applied here it would overtake
// them, and a client that typed one last thing and then hit stop would watch
// its message be refused by the session it just closed. That happened; this
// comment is the fix.
//
// Park goes through the queue for both of Stop's reasons and they are the same
// reasons. It is a write to the agent's stdin - it *is* a stop, with a label
// on it - so it has to be serialized with the frames already queued, and it
// means "finish what I have given you, then park", so it has to land behind
// them. A park applied here would overtake the last message somebody typed.
//
// Interrupt goes through the queue for the first of those reasons and not the
// second. It is a write to the agent's stdin like every other frame here, so
// it has to be serialized with them - two frames interleaved on stdin is a
// line claude cannot parse, and an unparseable line kills the process it was
// meant to interrupt. It is emphatically not the wedged-agent verb: an agent
// that has stopped reading stdin cannot be interrupted at all, by anyone, and
// kill is still what an operator reaches for then.
//
// Wake does not go through the queue, and the reason is the mirror of park's.
// That queue is one agent's stdin writes, in the order they were given, and a
// parked session has no stdin to write to - its process is gone and its input
// goroutine retired with it, so a wake queued there would be a frame nothing
// ever applies. There is also nothing for it to be serialized behind: park is
// the last write a session takes, and everything after it is this.
//
// Kill does not go through the queue, and must not: it is the verb for an
// agent that has stopped reading its stdin, so queueing it behind the write
// that is stuck would make the wedged case the one case kill cannot reach.
// The cost is stated where it lands - a stop queued behind a parked write is
// late, and kill is what an operator reaches for then.
//
// ParkAll is not addressed to any agent, so it is not on any queue: it records
// the verb and lets the accept loop end, and shutdown does the parking on
// Serve's own goroutine. It therefore inherits Quit's ordering rather than
// Park's - shutdown stops every session directly, so a message still queued for
// an agent can lose its race with that agent's stdin closing. That is what
// ending a fleet has always cost here and ⌃Q does not change it; the frame that
// waits for what somebody typed is FramePark, one session at a time.
func (s *server) dispatch(ctx context.Context, c *client, f rpc.Frame) {
	switch f.Kind {
	case rpc.FrameSpawn:
		// A worktree spawn is the one verb here that can block: sessionDir shells
		// out to `git worktree add`, bounded at gitTimeout and, past a
		// backgrounding post-checkout hook, at worktree.go's WaitDelay. dispatch
		// is serial per connection, so run in line it would hold this client's
		// kill, interrupt and answer frames behind it - the FrameHistory rule, and
		// the runaway agent an operator is trying to stop keeps running. So it runs
		// on its own goroutine (s.start, so shutdown's Wait accounts for it; the
		// Add cannot race a Wait at zero, since dispatch runs on a tracked one).
		// A spawn with no worktree cannot block - sessionDir returns before it
		// touches git - and stays in line, which keeps the synchronous ordering
		// cmd/wake's `act` piggybacks on: its refusal is enqueued during this
		// dispatch, ahead of any FrameStatus written behind the spawn, so a refused
		// spawn is never read back as one that was taken.
		if f.Worktree != "" {
			s.start(func() { s.spawn(ctx, c, f) })
		} else {
			s.spawn(ctx, c, f)
		}
	case rpc.FrameFork:
		s.fork(ctx, c, f)
	case rpc.FrameImport:
		s.importSession(ctx, c, f)
	case rpc.FrameSend, rpc.FrameAllow, rpc.FrameAnswer, rpc.FrameDeny, rpc.FrameInterrupt, rpc.FrameMode, rpc.FrameRewind, rpc.FrameStop, rpc.FramePark:
		s.submit(c, f)
	case rpc.FrameWake:
		s.unpark(ctx, c, f)
	case rpc.FrameRename:
		s.renameSession(c, f)
	case rpc.FrameLabel:
		s.relabelSession(c, f)
	case rpc.FrameKill:
		s.withAgent(c, f, func(a *agent) error { a.kill(); return nil })
	case rpc.FrameQuit:
		s.beginQuit(quitStop)
	case rpc.FrameParkAll:
		s.beginQuit(quitPark)
	case rpc.FrameStatus:
		c.enqueue(s.statusReply())
	case rpc.FrameHistory:
		// On its own goroutine: dispatch is serial per connection and this
		// reads a file off disk - measured at 740ms for a 50MB transcript -
		// during which that client's kill, interrupt and answer frames are
		// undispatched. The runaway agent an operator is trying to stop would
		// keep running while the daemon read somebody else's conversation.
		// client.enqueue is a non-blocking send and is already called from
		// every session's fan-out goroutine.
		//
		// Through s.start rather than a bare go, so shutdown's Wait accounts
		// for it. The Add cannot race a Wait at zero: dispatch itself runs on
		// a tracked goroutine, so the counter is above zero here.
		s.start(func() { s.sendHistory(c, f.SessionID) })
	case rpc.FrameRoomHistory:
		// The room's ask, on its own goroutine for the reason above and
		// answered under its own kind for rpc.FrameRoomHistory's.
		s.start(func() { s.sendRoomHistory(c, f.SessionID) })
	case rpc.FrameRewindTargets:
		// The same file read as FrameHistory, on its own goroutine for the
		// same reason, answered under its own kind for FrameRewindTargets'.
		s.start(func() { s.sendRewindTargets(c, f.SessionID) })
	default:
		// Unrecognized rather than guessed at. The whole reason stop, kill
		// and quit are separate kinds is that no default is safe.
		c.enqueue(errorFrame(f.SessionID, fmt.Sprintf("unknown frame kind %q", f.Kind)))
	}
}

// withAgent runs an ending verb against a named session, reporting an unknown
// name rather than silently doing nothing.
func (s *server) withAgent(c *client, f rpc.Frame, do func(*agent) error) {
	a, ok := s.agent(f.SessionID)
	if !ok {
		c.enqueue(errorFrame(f.SessionID, "unknown session "+f.SessionID))
		return
	}
	if err := do(a); err != nil {
		c.enqueue(errorFrame(f.SessionID, err.Error()))
	}
}

// holds reports whether this daemon is already running a session under an id.
func (s *server) holds(id string) bool {
	_, ok := s.agent(id)
	return ok
}

func (s *server) agent(id string) (*agent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[id]
	return a, ok
}

// submit hands a stdin-bound frame to the agent that owns it.
func (s *server) submit(c *client, f rpc.Frame) {
	s.withAgent(c, f, func(a *agent) error { return a.submit(c, f) })
}

// broadcast hands one frame to every attached client.
//
// A client that cannot keep up loses frames; it never slows this down. The
// lock is held only across non-blocking sends - see the note on server.mu.
func (s *server) broadcast(f rpc.Frame) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.clients {
		c.enqueue(f)
	}
}

func errorFrame(sessionID, text string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameError, SessionID: sessionID, Text: text}
}

// fleet is the whole fleet as one Status, and the sessions that recently left
// it - so a client learns how one ended rather than watching a row vanish, and
// can still learn it after the announcement it missed.
//
// The live agents and the remembered endings are read under one lock, which is
// what makes the two halves consistent with each other: register and retire
// each move an id between them in a single locked step, so no id is ever in
// both and none is ever in neither.
func (s *server) fleet() rpc.Status {
	st := rpc.Status{Running: true, PID: os.Getpid(), Socket: s.socket}

	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	st.Sessions = append(st.Sessions, s.recent...)
	s.mu.Unlock()

	for _, a := range agents {
		st.Sessions = append(st.Sessions, a.snapshot())
	}
	sortSessions(st.Sessions)

	// The park book, on its own list. Read outside s.mu because it has its own
	// lock and holds no agent - and reported by a *running* daemon rather than
	// only by FleetOnDisk, because that is what makes /resume work in a room
	// that has been open since before anything was parked.
	st.Parked = parkedStatuses(s.parked.records())
	sortSessions(st.Parked)
	return st
}

// statusReply answers a request. It is never broadcast: a client waiting for
// the answer to its own question must not be handed an announcement that was
// already in flight when it asked. See rpc.FrameStatusPush.
func (s *server) statusReply() rpc.Frame {
	st := s.fleet()
	return rpc.Frame{Kind: rpc.FrameStatusReply, Status: &st}
}

// statusPush is the same report sent unasked. It is never a reply.
func (s *server) statusPush() rpc.Frame {
	st := s.fleet()
	return rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st}
}

// recentEndings is how many endings a status report carries after the fact.
// Enough that a client which blinked during a fleet-wide stop can still
// account for every session, small enough to be a courtesy rather than a log.
const recentEndings = 32

// rememberLocked records an ending. The caller holds s.mu, and that is the
// contract for the same reason forgetLocked's is: leaving s.agents and
// entering s.recent have to be one step, or the two are briefly both false and
// a status reply reports a session that is neither running nor ended. See
// retire.
func (s *server) rememberLocked(ended rpc.SessionStatus) {
	s.recent = appendRecent(s.recent, ended)
}

// forgetLocked drops any ending for an id, so a respawned session is not
// reported twice - once alive and once dead - in the same status.
//
// The caller holds s.mu, and that is the contract rather than a convenience:
// forgetting the ending and taking the id have to be one step, or a retire
// landing between them reinstates the ending the spawn just cleared. See
// register.
func (s *server) forgetLocked(id string) {
	s.recent = slices.DeleteFunc(s.recent, func(r rpc.SessionStatus) bool { return r.ID == id })
}

// appendRecent adds an ending, replacing any earlier one for the same session
// and dropping the oldest once the bound is reached. It returns a new slice
// rather than editing in place.
func appendRecent(recent []rpc.SessionStatus, ended rpc.SessionStatus) []rpc.SessionStatus {
	out := make([]rpc.SessionStatus, 0, len(recent)+1)
	for _, r := range recent {
		if r.ID != ended.ID {
			out = append(out, r)
		}
	}
	out = append(out, ended)
	if len(out) > recentEndings {
		out = out[len(out)-recentEndings:]
	}
	return out
}
