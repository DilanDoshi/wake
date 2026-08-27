// Attaching: getting a connection that a daemon is actually listening on,
// spawning the session, running the TUI, and saying what stayed behind.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// maxAttachAttempts bounds how many outgoing daemons this will wait out before
// giving up. One is the real case - a `wake` issued during a `wake stop` - and
// a second would mean somebody quit the daemon again while this was starting.
const maxAttachAttempts = 3

// helloPatience is how long a silent connection waits before explaining
// itself. Not a timeout: the wait below has no deadline by design. It is only
// the point at which staring at a blank terminal stops being reasonable.
const helloPatience = 400 * time.Millisecond

// waitingLine is what a caller sees during that wait.
//
// It describes what was observed rather than naming a cause, because the
// observation has two of them and they are indistinguishable from here. A
// daemon on its way out holds its listener bound for the whole of its
// shutdown - but so does one on its way *in*: Serve binds before it runs
// reapOrphans, which shells out to ps once per roster entry and can take a
// while on a machine with a stale roster. Blaming the first would be
// confidently wrong for a first-ever `wake`.
const waitingLine = "The daemon is not answering yet - it is either starting up or still stopping a previous fleet. Waiting…"

// detachHint is the two commands that reach a fleet with no window open.
const detachHint = "wake status · wake stop"

// attach opens a conversation with a new agent.
//
// requested is the name the caller asked for, or empty for "draw one". Either
// way the daemon decides: it is the only process that can see the whole fleet,
// so it is the only one that can promise no two live sessions share a name.
// What comes back on the spawn's confirmation is the name this conversation
// actually has, and that - not what was asked for - is what the DM is told.
func attach(socket, requested string, opts spawnOpts, out io.Writer) error {
	sessionID := uuid.NewString()
	// No confirmation line: a fresh agent is nobody's fork, and the only thing
	// openSession announces is that a fork is a snapshot. See announceFork.
	return openSession(socket, sessionID, func(conn net.Conn) error {
		return requestSpawn(conn, sessionID, requested, "", opts)
	}, nil, out)
}

// openSession asks the daemon for a session and opens the conversation it
// confirms.
//
// attach and forkSession differ in exactly one thing - the frame they write -
// and share everything else: the terminal handshake, the connection, the wait
// for a confirmation that has no deadline, and the frames read past on the way.
// Extracted rather than copied, because the parts that would drift are the
// three that are already load-bearing: render.Prime before Bubble Tea takes
// stdin, held.close on the refusal path, and drain retiring rpc.ReadFrames'
// goroutine.
//
// sessionID is the id of the session being *asked for*, and it is what the wait
// below is keyed on. Both callers mint it and both hand the same value to the
// frame they write, which is what makes a refusal reach the caller rather than
// hang - see forkSession.
func openSession(socket, sessionID string, request func(net.Conn) error, confirmed func(rpc.SessionStatus, *rpc.Status), out io.Writer) error {
	// Before Bubble Tea takes stdin, and before anything else can be slow.
	// Resolving the terminal's background colour is a blocking handshake with
	// the TTY - it can wait out a five-second timeout once Bubble Tea is
	// parsing the replies itself - and it happens under the process-global
	// render lock every session's drawing shares.
	render.Prime()

	held := &connection{}
	defer held.close()

	conn, stream, err := connect(socket, out)
	if err != nil {
		return err
	}
	held.replace(conn)

	if err := request(conn); err != nil {
		return err
	}

	// Waited for rather than assumed, for two reasons. The name is one: it is
	// assigned over there, so there is nothing to put in the DM's header until
	// the daemon has answered. The refusal is the other, and it is the half a
	// person notices: `wake new sydney` against a fleet that already has a
	// sydney, or `wake fork alex` while alex is mid-turn, fails here, on a
	// terminal, instead of opening an alt screen and putting the reason on a
	// notice row nobody was looking at yet.
	sess, fleet, spawned, err := awaitSpawn(stream, sessionID)
	if err != nil {
		held.close()
		drain(stream)
		return err
	}
	// After the wait and before the alt screen: whatever this is says its piece
	// on the notice row the first frame will draw. Nil for a plain spawn, which
	// has nothing to say - see announceFork for why `wake attach` may not
	// reuse it.
	if confirmed != nil {
		confirmed(sess, fleet)
	}
	// Every verb that opens a *conversation* ends here, so this is where the
	// room's default addressee is filled for each of them: `wake new`, `wake
	// attach`, `wake fork`, `wake import`. Bare `wake` fills it in
	// conversationOnly, both branches of it, since first run stopped coming
	// through here. `wake manager` is in neither place - it opens nothing, and
	// it is asking for this directly.
	ensureManager(conn, fleet)
	// The confirmation carried the whole fleet, not just the new session, so
	// the room opens with a roster rather than with an empty one that fills in
	// over the next thirty seconds. See conversation.
	return converse(socket, sess, fleet, conn, resume(stream, spawned), held, out)
}

// requestSpawn asks the daemon to start a session, saying where and under what
// name.
//
// Dir is not optional in practice. One daemon serves every repository on the
// machine, and without this the agent runs in the daemon's working directory -
// whichever repo the client that happened to fork it was in - which is also
// where claude persists the transcript, so a later --resume inherits the
// mistake. It is also where the session's label comes from: the daemon derives
// the branch from this directory, so a spawn that did not say where would be a
// session with no second half to its name.
//
// A UUID is not decoration either: the reaper identifies a process group by
// finding the session id in its argv, and maySpawn refuses anything that is not
// one for exactly that reason.
//
// Bounded, because this write is the one with nowhere to report from: it
// happens before tea.NewProgram, so a daemon that has stopped reading leaves
// `wake` parked on a blank terminal with nothing printed. See rpc.WriteFrameTo.
//
// role is empty for every agent and rpc.RoleManager for the one session that
// gets Wake's own tools. It is a parameter here rather than a second function
// beside this one because the two spawns differ in that field and in nothing
// else - the id, the directory and the wait are the same - and a second copy is
// where the directory argument stops being passed.
func requestSpawn(conn net.Conn, sessionID, requested, role string, opts spawnOpts) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("locate the working directory: %w", err)
	}
	if err := rpc.WriteFrameTo(conn, rpc.Frame{
		Kind:      rpc.FrameSpawn,
		SessionID: sessionID,
		Text:      requested,
		Dir:       dir,
		Role:      role,
		Effort:    opts.Effort,
		Model:     opts.Model,
		Worktree:  opts.Worktree,
		// Every field of spawnOpts has to appear here or the flag is parsed,
		// validated, printed in the usage text and then dropped with nothing
		// downstream able to tell. TestEverySpawnOptReachesTheSpawnFrame is
		// what holds that, derived from the struct rather than from this list.
		MaxBudgetUSD:  opts.MaxBudgetUSD,
		FallbackModel: opts.FallbackModel,
		AddDir:        opts.AddDir,
		Debug:         opts.Debug,
		DebugFile:     opts.DebugFile,
	}); err != nil {
		return fmt.Errorf("spawn a session: %w", err)
	}
	return nil
}

// awaitSpawn reads until the daemon confirms the session or refuses it - for a
// spawn and for a fork alike, which are confirmed with the same reply and
// refused with an error addressed to the same new id - and returns the whole
// fleet report that confirmed it plus whatever it read past on the way.
//
// # Why the whole report and not only this session
//
// Because the daemon's confirmation is s.fleet() - every session it holds, not
// only the one just started - so a bare `wake` already has the roster the room
// needs and asking for it again would be a second round trip for an answer
// already in hand. It would also cost this client the property that lets
// ui.App read both status kinds without telling them apart: it never writes
// FrameStatus, so a reply on its connection can only be its own spawn's
// confirmation.
//
// # Why the frames it read past come back
//
// Because they are this session's transcript. The daemon starts fanning a
// session's events out on its own goroutine and *then* enqueues the spawn's
// confirmation, so an agent that produced its first frame quickly can put an
// event ahead of the reply this is waiting for. Dropping it would lose the
// opening of the conversation - silently, and only sometimes, which is the
// worst shape a transcript bug can have.
//
// # Why it has no deadline
//
// The same reason waitForHello does not: both outcomes are events. A spawn
// either confirms or is refused, and a daemon that can do neither has hung up,
// which closes the stream. A timer here would have to guess how long forking a
// claude process takes on somebody else's machine.
func awaitSpawn(stream ui.Stream, sessionID string) (rpc.SessionStatus, *rpc.Status, []rpc.Frame, error) {
	var read []rpc.Frame
	for f := range stream.Frames {
		if f.Kind == rpc.FrameError && f.SessionID == sessionID {
			// Addressed to this session, so it is the answer to this spawn.
			// Another client's failure carries another client's id.
			return rpc.SessionStatus{}, nil, nil, errors.New(f.Text)
		}
		if s, ok := spawnedSession(f, sessionID); ok {
			return s, f.Status, read, nil
		}
		read = append(read, f)
	}
	if err := <-stream.Errs; err != nil {
		return rpc.SessionStatus{}, nil, nil, fmt.Errorf("reading from the daemon: %w", err)
	}
	return rpc.SessionStatus{}, nil, nil, errors.New("the daemon hung up without saying whether the session started")
}

// spawnedSession finds this session in a status *reply*.
//
// A reply and not a push, which is the distinction rpc.FrameStatusPush exists
// for: a push announcing some other client's session could be sitting in the
// socket already, and reading one as this spawn's confirmation would report a
// session that has not started - or, worse, report the wrong row's name.
func spawnedSession(f rpc.Frame, sessionID string) (rpc.SessionStatus, bool) {
	if f.Kind != rpc.FrameStatusReply || f.Status == nil {
		return rpc.SessionStatus{}, false
	}
	for _, s := range f.Status.Sessions {
		if s.ID == sessionID {
			return s, true
		}
	}
	return rpc.SessionStatus{}, false
}

// resume puts frames that were read past back in front of the stream.
//
// The goroutine exists only when something was read past, which is the rare
// half of a race, and it ends when the source does. It renders nothing and
// decides nothing - CLAUDE.md's rule is that nothing which *draws* may sit
// between the socket and the inbox ring, and this is a pass-through.
func resume(stream ui.Stream, held []rpc.Frame) ui.Stream {
	if len(held) == 0 {
		return stream
	}
	frames := make(chan rpc.Frame, len(held))
	go func() {
		defer close(frames)
		for _, f := range held {
			frames <- f
		}
		for f := range stream.Frames {
			frames <- f
		}
	}()
	return ui.Stream{Frames: frames, Errs: stream.Errs}
}

// drain empties a stream so the goroutine behind it can retire. rpc.ReadFrames
// has no cancellation: abandoning its channel parks that goroutine on a send
// for good, and closing the connection does not release one already parked.
func drain(stream ui.Stream) {
	for range stream.Frames {
	}
	<-stream.Errs
}

// reattach opens a conversation with an agent that is already running.
//
// # Why this verb exists
//
// Without it a hang-up was permanent. The TUI drains its connection on the
// goroutine that draws; the daemon hangs up on a client whose write blocks for
// five seconds; and `wake` has always minted a fresh UUID and spawned. So an
// ordinary drag of a terminal window could disconnect a live conversation and
// there was no way back into it - the agent kept working with nobody watching,
// which is the one state the whole architecture exists to make recoverable.
// internal/ui reattaches by itself when it can, and this is what a person types
// when it could not, or when they detached on purpose and want back in.
//
// It never spawns. That is the whole difference from attach and it is
// load-bearing: a reattach that spawned on a typo would make every mistyped id
// a new agent, and there is no verb yet that stops one.
func reattach(socket, sessionID string, out io.Writer) error {
	render.Prime()

	// Asked before anything is dialled, and asked of the fleet report rather
	// than of the connection: a client that attaches to a session id nothing
	// is holding gets a working socket and an empty conversation that never
	// says anything, which looks exactly like an idle agent.
	//
	// That report is also the room's seed. It was fetched to answer one
	// question about one session and it happens to describe every one of them,
	// so the alternative is asking the same daemon the same question twice.
	sess, fleet, err := liveSession(socket, sessionID)
	if err != nil {
		return err
	}

	held := &connection{}
	defer held.close()

	conn, stream, err := connect(socket, out)
	if err != nil {
		return err
	}
	held.replace(conn)

	// Said before the first frame is drawn, because the transcript starts
	// empty and an empty conversation with a working agent in it is the most
	// misleading thing this view can show.
	// displayName in both places, not sess.Name in one and displayName in the
	// other: an unnamed session was announced as "@<unnamed>" on the notice row
	// and drawn as a bare "@" in the DM header, which reads as two agents.
	// converse now derives its half from the same session, so the two cannot
	// drift apart by anyone editing one call site.
	notice.Report("%s", ui.TranscriptNotice(displayName(sess)))
	return converse(socket, sess, &fleet, conn, stream, held, out)
}

// converse runs the TUI over an established connection and says what stayed
// behind.
//
// It takes the session rather than an id and a name, and that is a guard rather
// than tidiness: the name a conversation is drawn under is the one the *daemon*
// assigned, never the one the caller asked for. Bare `wake` asks for nothing at
// all, so a call site holding a separate name string is one edit away from
// putting an empty string in the DM's header - which is the "@ with nothing
// after it" that reads as a second agent. There is no name to pass here, so
// there is no wrong name to pass.
//
// The dialer it wires up is the reattach path the model reaches for when its
// connection ends. It is a function rather than something internal/ui does for
// itself because dialing is this package's job: connect() owns the hello-or-EOF
// discrimination, daemon.Status owns "is that session still there", and a
// second implementation of either inside a view is the parallel implementation
// this project forbids.
func converse(socket string, sess rpc.SessionStatus, seed *rpc.Status, conn net.Conn, stream ui.Stream, held *connection, out io.Writer) error {
	return converseModel(socket, conversation(socket, sess, seed, conn, stream, held), out)
}

// converseRoom is the same over the room alone, which is bare `wake`.
func converseRoom(socket string, seed *rpc.Status, conn net.Conn, stream ui.Stream, held *connection, out io.Writer) error {
	return converseModel(socket, conversationRoom(socket, seed, conn, stream, held), out)
}

// converseModel runs one model over an established connection and says what
// stayed behind.
//
// The two callers above differ in the model and in nothing else, which is why
// this is an extraction rather than a second function beside the first: the exit
// line - including ⌃Q's, which is the one that does not ask a daemon - is
// written once. A room whose ⌃Q printed a detach line would be the parallel
// implementation this project forbids, arriving as a copy of four statements.
// **The kill switch is armed before the program exists and disarmed after it
// ends**, because every exit Wake has is a key the Update loop reads and the
// loop is exactly what can stop answering. See killswitch.go. Arming it takes
// raw mode off Bubble Tea - initInput only claims it for a reader that is
// itself a terminal, and the reader it gets here is a pipe - so the restore is
// deferred here, on the one path every conversation leaves through.
func converseModel(socket string, model ui.App, out io.Writer) error {
	kill, err := armKillSwitch()
	if err != nil {
		return err
	}
	defer kill.restore()
	kill.watchSignals()

	term := &guardedOutput{File: os.Stdout}
	opts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithOutput(term)}
	if kill != nil {
		// Only when there is a terminal to read. With input piped there is
		// nobody pressing keys, and the default reader is what it always was.
		opts = append(opts, tea.WithInput(kill.Input()))
	}
	p := tea.NewProgram(model.WithOutput(term), opts...)
	final, err := p.Run()
	if err != nil {
		return err
	}
	return say(out, "%s", leavingLine(final, socket))
}

// leavingLine is what `wake` prints on its way out, and there are two of them
// because there are two ways to leave.
//
// **⌃Q is the one exit that does not ask.** The daemon it would ask is the one
// it just told to shut down, so the dial lands in that daemon's listen backlog
// and burns the whole status timeout for an i/o error - and the line an
// operator gets after the key whose entire point was counting is "Detached, but
// could not count what is still running". The model already counted, before it
// wrote the frame, which is why ui.App.ParkedFleet exists at all.
//
// **So the number is what was asked for, and the sentence says so.** The daemon
// gives each agent the quit grace and kills whatever has not ended by the end of
// it; a killed session is never a parked one, so it is dropped from the book,
// and a turn running a build routinely outlasts the grace. This process is
// leaving and cannot learn which - that is the whole reason it is not asking -
// so it may not print an outcome as an accomplished fact. "Parking N" and
// "offers back whatever finished in time" are both the hedge, and the
// alternative was printing no number at all, which loses the one thing this
// client does know.
//
// Every other way out asks, because a background process nobody can see is a
// liability - 20 sessions is about 16GB with no window open - and the count has
// to be what is running *now* rather than what was running when the last frame
// was drawn. ⌃O, a hang-up and a crashed daemon are all that case.
//
// Split out of converse so the decision is reachable without a terminal: the
// program above needs a TTY and this does not.
func leavingLine(final tea.Model, socket string) string {
	if app, ok := final.(ui.App); ok {
		if n, parked, err := app.ParkedFleet(); parked {
			return parkAllLine(n, err)
		}
	}
	return detachLine(daemon.Status(socket))
}

// parkAllLine is ⌃Q's exit line, and there are two of them because the ask has
// two outcomes and only one of them may make a promise.
//
// **This is the surface a failed park lands on, and it is the only one there
// is.** The notice row is inside an alt screen that is torn down a frame later,
// so a refusal drawn there is a refusal nobody reads; this line is printed to
// the terminal the operator is left looking at. It is also why ui.App waits for
// the daemon's answer before it quits rather than reporting on the keypress -
// without the wait there is nothing for this to say, which is the whole of
// outstanding bug 3.
//
// The failure sentence claims nothing about the fleet. Of the four ways the ask
// goes unconfirmed only one - no connection at all - knows that nothing was
// parked; a write that failed may have failed after a partial write, and a
// daemon that went quiet may have taken the verb and died with the book half
// written. So it reports what is *not* known and names the two verbs that
// answer it, which is `wake stop`'s own rule: never claim more than you can see.
func parkAllLine(n int, err error) string {
	if err != nil {
		return fmt.Sprintf("Could not confirm that ⌃Q parked the fleet (%s): %v. "+
			"`wake status` says what is still running; `wake` reopens the room.", agents(n), err)
	}
	return fmt.Sprintf("Parking %s. `wake` reopens the room and offers back whatever finished in time.", agents(n))
}

// redial is one reattach attempt: is the session still there, and can this
// process get a connection the daemon is provably listening on.
//
// The order is the point. connect() calls daemon.EnsureRunning, which *forks a
// daemon* when nothing is listening - so dialing first would answer "the daemon
// died" by starting a fresh one that has never heard of this session, and
// reattaching would appear to succeed into a conversation that cannot exist.
// Asking the fleet first makes that case an error with a reason.
//
// The whole fleet comes back, not just this session: liveSession already
// fetched it, and the model folds it so an ask that arrived during the outage
// comes back as a card - Cards.Reconcile needs a report, and nothing else pushes
// one after a reattach. It is this process's own daemon.Status read, so it adds
// no FrameStatus writer to ui.App. See ui.App.reattached.
func redial(socket, sessionID string, held *connection) (net.Conn, ui.Stream, rpc.SessionStatus, *rpc.Status, error) {
	sess, fleet, err := liveSession(socket, sessionID)
	if err != nil {
		return nil, ui.Stream{}, rpc.SessionStatus{}, nil, err
	}
	// io.Discard rather than the terminal: Bubble Tea owns the screen while
	// this runs, and connect's "waiting…" line would be written straight onto
	// the alt screen's canvas. The model has its own notice row for saying so.
	conn, stream, err := connect(socket, io.Discard)
	if err != nil {
		return nil, ui.Stream{}, rpc.SessionStatus{}, nil, err
	}
	held.replace(conn)
	return conn, stream, sess, &fleet, nil
}

// connection is the socket this client is currently on.
//
// It exists because a reattach replaces one and both ends have to be closed by
// somebody: the one that hung up, so its file descriptor and its reader
// goroutine go, and the last one, when the program exits. The model cannot own
// that - it is handed connections and never opens them - and a bare variable
// cannot either, because it is written from a tea.Cmd's goroutine and read by
// the deferred close on the main one.
type connection struct {
	mu   sync.Mutex
	conn net.Conn
}

// replace closes whatever was held and takes the new one.
func (c *connection) replace(conn net.Conn) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = conn
}

func (c *connection) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// agentPrefix is how an agent is named, matching the DM header. ui spells it
// too and the two may not import each other; if they ever disagree, the header
// is right.
const agentPrefix = "@"

// connect returns a connection a daemon is provably listening on, and the
// single reader over it.
//
// # Why a successful dial is not enough
//
// A daemon in graceful shutdown keeps its listener bound and stops accepting.
// That is deliberate: unlinking the socket earlier is what let a second
// daemon's reaper SIGKILL a fleet the first one was politely waiting for. But
// the kernel completes connections into the listen backlog throughout, so
// net.Dial succeeds against a daemon that will never read a byte - and
// daemon.EnsureRunning returns the first success. Spawning into that gives an
// empty conversation that sits there until the old daemon exits.
//
// serveClient enqueues rpc.FrameHello as its first act on every accepted
// connection, so the handshake is the discriminator: no hello means "never
// accepted", not "slow". The other outcome is EOF, which arrives when the
// outgoing daemon closes its listener - after its roster is provably empty,
// which is exactly the edge at which starting a fresh daemon is safe.
//
// # Why this waits on the connection it holds instead of re-dialling
//
// Because a retry loop written the obvious way SIGKILLs the fleet. Measured on
// darwin: 128 dials succeed against a bound-but-never-accepting listener, and
// closing a pending connection does not give its backlog slot back. A polite
// 100ms poll therefore exhausts the backlog in about 12.8 seconds - inside the
// 30-second grace - and the next dial gets ECONNREFUSED, which is precisely
// what the daemon's own listen() reads as a crashed daemon's stale socket. It
// unlinks, binds, and reaps the live fleet.
//
// One connection, waited on. It costs one slot and needs no timeout guess.
func connect(socket string, out io.Writer) (net.Conn, ui.Stream, error) {
	var last error
	for attempt := 1; attempt <= maxAttachAttempts; attempt++ {
		conn, err := daemon.EnsureRunning(context.Background(), socket)
		if err != nil {
			// Retried rather than returned, and this is the case that needs
			// it: the outgoing daemon releases its lock *after* closing its
			// listener, so a fork started in that sliver is refused and
			// EnsureRunning reports that nothing began listening. Every other
			// failure here is immediate and permanent, so retrying one costs
			// nothing but the same answer again.
			last = err
			continue
		}

		frames, errs := rpc.ReadFrames(conn)
		attached, err := waitForHello(frames, errs, out)
		switch {
		case err != nil:
			_ = conn.Close()
			return nil, ui.Stream{}, err
		case attached:
			return conn, ui.Stream{Frames: frames, Errs: errs}, nil
		}

		// EOF with no hello: that daemon has finished shutting down and
		// unlinked its socket, so the next EnsureRunning forks a fresh one.
		_ = conn.Close()
		last = fmt.Errorf("a daemon on %s shut down while attaching to it", socket)
	}
	return nil, ui.Stream{}, fmt.Errorf("could not attach after %d attempts: %w", maxAttachAttempts, last)
}

// waitForHello reads until the daemon says hello or the connection ends.
//
// It has no deadline, and that is the design rather than an omission: the two
// outcomes are both events, not silences. A daemon that accepted this
// connection has already queued the hello, and a daemon that did not will
// deliver EOF when it closes its listener. A timer here would have to guess a
// bound on somebody else's shutdown, and guessing short is the failure that
// reaps a live fleet.
func waitForHello(frames <-chan rpc.Frame, errs <-chan error, out io.Writer) (bool, error) {
	patience := time.NewTimer(helloPatience)
	defer patience.Stop()

	for {
		select {
		case f, ok := <-frames:
			if !ok {
				if err := <-errs; err != nil {
					return false, fmt.Errorf("reading from the daemon: %w", err)
				}
				return false, nil
			}
			if f.Kind == rpc.FrameHello {
				return true, nil
			}
			// Nothing of this client's can precede its own hello - the
			// session it is about to ask for does not exist yet - so anything
			// here belongs to another client's session and is not ours to
			// render.
		case <-patience.C:
			// Ignored deliberately: an explanation that could not be printed
			// is not a reason to abandon an attach that is otherwise fine.
			_ = say(out, "%s", waitingLine)
		}
	}
}

// detachLine is what wake prints on the way out. It takes daemon.Status's two
// results directly so the call site reads as one statement.
func detachLine(st rpc.Status, err error) string {
	switch {
	case err != nil:
		return fmt.Sprintf("Detached, but could not count what is still running: %v", err)
	case !st.Running && len(st.Sessions) > 0:
		return fmt.Sprintf("Detached. The daemon is gone and left %s running with nothing holding them.    wake status",
			agents(len(st.Sessions)))
	case !st.Running:
		return "Detached. The daemon is no longer running."
	case runningCount(st) == 0:
		return "Detached. No agents are running.    " + detachHint
	default:
		return fmt.Sprintf("Detached. %s still running.    %s", agents(runningCount(st)), detachHint)
	}
}

// runningCount is how many sessions are still alive. A status report also
// carries recent endings - that is what makes an ending recoverable by a
// client that missed the announcement - so this is a filter and not a length.
//
// A parked session is not one of them, and this is the count `wake stop` is
// built on: counted here, a fleet of parked agents reports as still up with
// nothing running in it, which is precisely the claim `wake stop` exists to be
// able to deny. A parked session is not lost by leaving it out either - it is
// in `wake status` with its own state beside it.
func runningCount(st rpc.Status) int {
	n := 0
	for _, s := range st.Sessions {
		if s.State != rpc.StateEnded && s.State != rpc.StateParked {
			n++
		}
	}
	return n
}
