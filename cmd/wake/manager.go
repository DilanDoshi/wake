// `wake manager`: start the one session that can operate the fleet.
//
// # Why it does not open a conversation
//
// Every other spawn in this package ends in a TUI, because every other spawn is
// somebody starting a conversation. The manager is settled as a **service**: it
// does not read the room, it has tools and calls roll_up on demand, and the
// place you talk to it is the room's own composer - `@manager`, or an
// unaddressed message, which now goes to it by default. Opening a DM on it here
// would make the service a participant in the one command whose whole job is
// creating it, and it would put the operator in a pane whose composer sends
// unaddressed text to the session they are looking at.
//
// So this is `wake status`'s shape rather than `wake new`'s: it does one thing,
// prints one line and exits, and the fleet keeps the manager.
//
// # What it does not decide
//
// Nothing about what makes this session the manager. The role on the frame is
// the whole of what this command asks for; the tools, the socket they reach and
// the scope the session reads them under are the daemon's, derived from the name
// it gives the session. See internal/daemon/manager.go for why - the short
// version is that an MCP config names a command to execute, and a client that
// could choose one would be choosing the command line of the session holding
// tools that act on every agent on the machine.
//
// The effort and the model are not in that category and this command does carry
// them. They name no command to execute, they are checked before anything
// starts, and they survive a park in the book like every other session's - so
// they are ordinary spawn configuration that happens to also apply to the one
// session an operator cannot attach to and reconfigure by hand.

package main

import (
	"io"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// managerStarted is what the terminal says, and it claims exactly what the
// confirmation proves.
//
// **"Started", not "running", and the difference is the whole of what this
// command can know.** The daemon's reply proves a `claude` process was spawned
// under this id. It says nothing about whether `wake mcp` came up behind
// `--mcp-config` — that server is executed by *claude*, not by Wake, so a
// binary that moved or a client that rejected the handshake produces a manager
// whose tools are simply absent and which then reports in prose that it cannot
// see the fleet. `docs/notes/deferred.md` carries the costed close (a self-test
// at this point, running `wake mcp` against the same socket) and
// `live-testing.md` §13.1 is the gate until then.
//
// It names where to talk to it because the manager has no surface of its own:
// a person who started a service and was told nothing has no way to find out
// whether it worked, and `@manager` is not guessable from a command called
// `manager`. The first thing to ask it is the check this line cannot make.
const managerStarted = "The manager is started. `wake` opens the room, where @manager reaches it and an unaddressed message goes to it - ask it to list its agents to confirm its tools came up."

// startManager asks the daemon for the manager and reports what happened.
//
// It waits for the confirmation for `attach`'s reasons and one of its own. The
// refusal is the half that matters here: a second manager, or a daemon that
// could not write the MCP config, fails on a terminal instead of leaving the
// operator with a command that printed a success line over nothing. There is no
// alt screen for a notice row to hide in, so the wait is what makes the failure
// visible at all.
func startManager(socket string, opts spawnOpts, out io.Writer) error {
	sessionID := uuid.NewString()

	held := &connection{}
	defer held.close()

	conn, stream, err := connect(socket, out)
	if err != nil {
		return err
	}
	held.replace(conn)

	// No name is asked for. The daemon owns the one manager name and refuses it
	// to every other spawn, so there is nothing here for a person to choose and
	// nothing for this command to get wrong.
	if err := requestSpawn(conn, sessionID, "", rpc.RoleManager, opts); err != nil {
		return err
	}
	// The fleet report and the frames read past are both discarded, and that is
	// the difference from openSession: nothing here renders a transcript, so
	// there is nothing for them to seed. drain is still owed to rpc.ReadFrames'
	// goroutine on both paths.
	_, _, _, err = awaitSpawn(stream, sessionID)
	held.close()
	drain(stream)
	if err != nil {
		return err
	}
	return say(out, "%s", managerStarted)
}
