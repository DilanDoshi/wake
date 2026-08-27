// Bare `wake`: the front door.
//
// # Why this is a branch rather than a one-liner
//
// Two cases, and the second is why. **A fleet exists** - open the room on it, no
// target, nothing spawned. **No fleet exists** - this is first run, and it still
// spawns, because making a new user type two commands to get an agent is the
// thing nobody forgives.
//
// **First run is not the rare half, and this comment used to say it was.** With
// no `--fleet` and no `$WAKE_SOCKET`, a bare `wake` makes a *new* fleet
// (`makesNewFleet`), and a new fleet's socket has nothing on it - so the
// "machine that has never run one" branch is what the front door takes every
// time somebody types the front door's name. The other half is reached through
// `--fleet`, `$WAKE_SOCKET`, or a fleet this same directory already had.
//
// **Both halves end in the same room, and the branch decides only whether a
// session is asked for on the way.** First run used to go through `attach`,
// which spawns *and* opens that agent's conversation beside the room - so the
// one surface bare `wake` is a request about was half a terminal wide on the
// only path a new user takes, and below dmTakeoverColumns not drawn at all. The
// agent is still started; it arrives as a roster row, which is what it is for
// every other client of that daemon anyway.
//
// # Why the branch is taken before anything is dialled
//
// connect() calls daemon.EnsureRunning, which *forks a daemon* when nothing is
// listening - so deciding after dialling would mean the fleet question is asked
// of a daemon that has just been created and has never held anything.
// daemon.Status is the right question and it is cheap: it dials, and on a
// machine with no daemon the dial fails at once and it reads the on-disk answer
// instead.
//
// **And the on-disk answer is why this works for a fleet parked by ⌃Q.** That
// fleet has no daemon at all - ⌃Q parks and then exits - so "is a daemon
// running" is the wrong question and would send every restore down the first-run
// path, spawning a fresh agent beside twenty parked ones. daemon.FleetOnDisk reads the
// park book, so the parked fleet is visible with nothing running.
//
// **The ordering has an observable consequence, and this comment used to deny
// it.** It shipped saying the reversing mutation *"survives the suite"* -
// reasoned from restoreParked running before the accept loop, so a forked daemon
// would report the same rows. That reasoning holds only when the dial's outcome
// is "no daemon". The outcome that matters is the other one: a daemon in
// graceful shutdown holds its listener, connect() waits that out with **no
// deadline by design**, and a dial-first version therefore cannot decide
// anything at all until somebody else's shutdown finishes - while daemon.Status
// bounds itself at statusTimeout and fleetToReopen answers from the disk in
// three seconds. Asking first is what makes bare `wake` decidable inside ⌃Q's
// own window, and
// TestBareWakeFindsTheParkedFleetWhileTheDaemonIsStillShuttingDown kills the
// mutation on the deadline rather than on the answer.

package main

import (
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// openRoom is bare `wake`, and it has three answers rather than two.
//
// A **live fleet** reopens the room over it. **Nothing at all** is first run and
// still spawns, because a new user's first command has to produce an agent.
//
// A **park book and nothing running** - what ⌃Q leaves - opens the room and
// starts nothing. That is the case this branch grew for: the daemon used to
// restore those records into the fleet, so quitting and coming back handed back
// the roster and the conversations somebody had just quit. Now they are
// addressable and invisible, and `/resume <name>` is what brings one back.
//
// It must not fall through to first run, which is why the park book is asked
// about at all: spawning a fresh agent beside five parked ones is the failure
// hasFleet was written to prevent, and moving parked off Status.Sessions took
// hasFleet's answer away with it.
//
// What the branch produces is a bool and not a flow, which is what keeps the
// three answers down to two behaviours: every one of them opens the room, and
// only the third asks for a session first.
func openRoom(socket string, out io.Writer) error {
	firstRun := !reopensRoom(fleetToReopen(socket))
	return conversationOnly(socket, firstRun, out)
}

// reopensRoom is whether bare `wake` opens a room or spawns, and it is split out
// for hasFleet's reason: the rest of openRoom opens a terminal, so this is the
// only part of the branch a test can reach.
//
// A park book counts even though it is not a fleet. Those records draw no row
// and hold no process, so the room over them is empty - but it must still be a
// *room*, because the alternative is first run spawning a fresh agent beside
// five sessions somebody parked deliberately.
func reopensRoom(st rpc.Status) bool { return hasFleet(st) || len(st.Parked) > 0 }

// fleetToReopen is the report bare `wake` branches on: what a daemon says, or -
// when the daemon that would say is not answering - what is on disk.
//
// # Why an error may not be read as "nothing is running"
//
// Because the error Status actually produces in normal use is **a daemon that is
// there and will not speak**, and that is precisely the state ⌃Q leaves behind.
// It parks the fleet and then holds its listener for the whole quit grace, so
// the `wake` its own exit line invites - *"`wake` reopens the room and offers
// back whatever finished in time"* - lands in a backlog nothing accepts from,
// waits out statusTimeout and gets an error with a zero report. A zero report
// has no sessions, so reading it as an answer takes the first-run path and
// spawns a twenty-first agent beside twenty parked ones, with no offer. That is
// the failure hasFleet exists to prevent, arriving through a timeout instead of
// through the branch.
//
// # Why the disk is the right answer and not a guess
//
// It is the answer Status itself gives when it cannot reach a daemon, and this
// is the same question one shape further out. shutdown writes the park book
// *before* it closes its clients, so the book is complete for the whole of that
// window rather than half-written - which is `EnsureRunning`'s edge read from
// the inside, and the reason ⌃Q's ordering is a property rather than a detail.
//
// **And the disk is what separates ⌃Q from `wake stop`.** Both leave a daemon
// that will not answer. Only ⌃Q leaves a book, because the quit verb clears it -
// so the same clause sends a parked fleet to the room and a stopped one to first
// run, which is what each of them means.
//
// # What this costs when it is wrong
//
// Nothing that lasts. A stale book names sessions the daemon `connect` forks
// will restore anyway - restoreParked reads the same file - so the room opens on
// exactly the rows the report will carry. The one thing it must not do is start
// anything, and it does not: this decides a branch and dials nothing.
//
// Split out of openRoom because it is the whole of the decision and the rest of
// that function opens a terminal: this is the only part of the branch a test can
// reach, and the one configuration that matters takes three seconds to build.
func fleetToReopen(socket string) rpc.Status {
	st, err := daemon.Status(socket)
	if err != nil {
		return daemon.FleetOnDisk(socket)
	}
	return st
}

// hasFleet reports whether there is a room to reopen.
//
// The question it is really answering is **"will the daemon this command is
// about to be connected to be holding anything"**, and that is what decides each
// state rather than "is this row interesting". Three answers, and each is about
// what survives into that daemon:
//
//   - **Parked counts.** restoreParked reads the book into s.agents before the
//     accept loop, so a parked row is there in the first report the room is
//     handed. It is also the whole reason this branch cannot be "is a daemon
//     running": a fleet parked by ⌃Q has none.
//
//   - **Ended does not.** A status report carries recent endings so a client can
//     learn how one died, and a fleet of nothing but endings is a fleet of
//     nothing.
//
//   - **Orphaned does not**, and that is the verdict worth arguing. This is the
//     one call site in cmd/wake where rpc.StateOrphaned can actually arrive:
//     every other reader is behind resolveSession, which refuses a report whose
//     Running is false, and daemon.FleetOnDisk is the only writer of the state. An
//     orphan is a live process a dead daemon left behind - and Serve runs
//     reapOrphans *before* restoreParked and before it accepts anything, so the
//     daemon connect() is about to fork ends exactly those processes on its way
//     up. Counting them would open the room on rows that are being killed as it
//     draws, which is the empty room the first-run case exists to prevent,
//     arriving on the one machine that is already in trouble. `wake status` is
//     where an orphan is reported, and detachLine already says so.
//
//     **The reaping has an exception and the verdict survives it.** reapOrphans
//     is gated on `lock.exclusive`: a daemon that could not take the flock logs
//     *"anything a crashed daemon left running is left alone"* and the orphans
//     live. The reason above is then false and the answer is still right, for the
//     second reason rather than the first - a room drawn over processes nobody is
//     holding is a room whose rows nothing can send to, whereas first run ends
//     with a working agent. Neither branch saves those processes; only one of
//     them produces something to type into.
//
// The verdict is per state and the domain is derived from both producers -
// agent.stateLocked and daemon.FleetOnDisk - in openroomguard_test.go, so a seventh
// reachable state is a build failure until somebody rules on it.
func hasFleet(st rpc.Status) bool {
	for _, s := range st.Sessions {
		if s.State != rpc.StateEnded && s.State != rpc.StateOrphaned {
			return true
		}
	}
	return false
}

// conversationOnly opens the room with no conversation beside it, which is
// every bare `wake`.
//
// spawn is first run, and the only thing it changes is which frame is written
// below - not what opens. It shares connect, the held connection and
// converseModel with openSession and differs in the DM: that flow is `wake
// new`, `wake fork` and `wake attach`, where one agent is what was asked for and
// its conversation is the pane that should have the cursor. Here there is no
// target, because bare `wake` is a request about the fleet.
func conversationOnly(socket string, spawn bool, out io.Writer) error {
	// Before Bubble Tea takes stdin: resolving the terminal's background colour
	// is a blocking handshake with the tty, under the process-global render
	// lock. openSession's own first line, for the same reason.
	render.Prime()

	held := &connection{}
	defer held.close()

	conn, stream, err := connect(socket, out)
	if err != nil {
		return err
	}
	held.replace(conn)

	fleet, read, err := seedRoom(conn, stream, spawn)
	if err != nil {
		held.close()
		drain(stream)
		return err
	}
	// The room's default addressee, filled before anything can be typed at it.
	// See ensuremanager.go for why the frame is written here rather than by the
	// model that is about to be built over this same connection.
	ensureManager(conn, fleet)

	return converseRoom(socket, fleet, conn, resume(stream, read), held, out)
}

// seedRoom is the report the room opens over: what the daemon is already
// holding, or - on a machine with nothing at all - the fleet its first agent
// makes.
//
// # Why first run needs no second question
//
// Because the daemon confirms a spawn with s.fleet(), which is every session it
// holds rather than the one just started, so awaitSpawn already hands back the
// report requestFleet would have asked for. Asking anyway would be a round trip
// for an answer in hand, and it would put a second reply in flight against a
// model that reads both status kinds the same way.
//
// Both arms return the frames they read past, for awaitSpawn's reason: they are
// the fleet's transcript, and dropping them loses the opening of whatever was
// being said - silently, and only sometimes.
//
// The spawn goes through requestSpawn rather than a second spelling of that
// frame, so first run cannot drift from `wake new` in the two fields that are
// not optional: the UUID the reaper proves a process group by, and the directory
// claude persists the transcript to.
func seedRoom(conn net.Conn, stream ui.Stream, spawn bool) (*rpc.Status, []rpc.Frame, error) {
	if !spawn {
		if err := requestFleet(conn); err != nil {
			return nil, nil, err
		}
		return awaitFleet(stream)
	}
	sessionID := uuid.NewString()
	if err := requestSpawn(conn, sessionID, "", "", spawnOpts{}); err != nil {
		return nil, nil, err
	}
	// The session itself is dropped deliberately: first run opens no
	// conversation, so the one thing that would be done with it is the DM this
	// path exists not to open.
	_, fleet, read, err := awaitSpawn(stream, sessionID)
	return fleet, read, err
}

// requestFleet asks the daemon what it is holding.
//
// **This is the seed, and it is asked for here rather than by the model** - for
// the same reason it always was, which is that the model does not exist yet. It
// wants only "is my session ended" out of a status report, and a snapshot can
// miss an ending but never invent one, so it folds a reply and a push the same
// way. This writes the frame in cmd/wake, before the model exists, and consumes
// the reply below; the model is handed the stream afterwards. Nothing on this
// path spawns, so there is no confirmation for a later reply to be confused
// with - and first run, which does spawn, is seedRoom's *other* arm and never
// reaches this one, so the two questions are never both in flight.
//
// **ui.App does now write one FrameStatus** - ⌃Q's, behind the FrameParkAll,
// where the reply is the daemon's acknowledgement that it took the verb - and
// that does not weaken the seed. It is the last frame that model ever writes, so
// no reply it asks for can arrive before this one; ui.parkAllTaken is the one
// place the two kinds are told apart, and it is reached only after ⌃Q.
//
// Bounded, because this write happens before tea.NewProgram: a daemon that has
// stopped reading would leave `wake` parked on a blank terminal with nothing
// printed. See rpc.WriteFrameTo.
func requestFleet(conn net.Conn) error {
	if err := rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameStatus}); err != nil {
		return fmt.Errorf("ask what is running: %w", err)
	}
	return nil
}

// awaitFleet reads until the daemon answers, returning the report and whatever
// it read past on the way.
//
// A **reply** and never a push, which is what rpc.FrameStatusPush exists for: a
// push announcing some other client's state change can be sitting in the socket
// already, and reading one as this question's answer would seed the room from a
// report assembled before it was asked.
//
// The frames it read past come back for awaitSpawn's reason - they are the
// fleet's transcript, and dropping them would lose the opening of whatever was
// being said, silently and only sometimes.
//
// No deadline, for waitForHello's reason: both outcomes are events. The daemon
// answers, or it hangs up and the stream closes.
func awaitFleet(stream ui.Stream) (*rpc.Status, []rpc.Frame, error) {
	var read []rpc.Frame
	for f := range stream.Frames {
		if f.Kind == rpc.FrameStatusReply && f.Status != nil {
			return f.Status, read, nil
		}
		read = append(read, f)
	}
	if err := <-stream.Errs; err != nil {
		return nil, nil, fmt.Errorf("reading from the daemon: %w", err)
	}
	return nil, nil, errors.New("the daemon hung up without saying what it is holding")
}
