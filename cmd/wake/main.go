// Command wake is the terminal client, and the daemon it forks.
//
// Eleven verbs, and the split between them is the product rather than a CLI
// convention:
//
//   - wake                  the front door: reopen the room over whatever
//     fleet there is, and start an agent when there is not one.
//   - wake new [name]       spawn: a new agent, with a name chosen rather
//     than drawn.
//   - wake attach <who>     the same conversation with an agent already
//     running, found by name or by session id.
//   - wake fork <who>       branch one: a new agent that inherits the named
//     session's conversation so far and then diverges. Its own id, its own
//     name, its own DM - and the parent's transcript is not touched.
//   - wake import [<id>]    adopt a session Wake never started: a transcript
//     from a `claude` somebody ran in a terminal. With no id it lists what is
//     on the machine, which is the picker.
//   - wake manager          start the manager: one session with tools over
//     the fleet, addressed as @manager from the room. It opens no
//     conversation of its own, because it is a service rather than a
//     participant.
//   - wake fleets           the named fleets on this machine.
//   - wake setup-terminal   make Shift+Enter send a newline in the composer,
//     by configuring the host terminal to send ESC CR for it - the sequence
//     Wake already reads as a newline. See internal/termsetup.
//   - wake daemon           serve: what EnsureRunning forks. Not a user
//     command.
//   - wake mcp              serve the manager's tools on stdin and stdout.
//     Spawned by a claude session through --mcp-config, so also not a user
//     command.
//   - wake status           what is running, including a fleet whose daemon
//     died.
//   - wake stop             end every session and the daemon, and wait for
//     it.
//
// Detach is not a verb here. ⌃O leaves the TUI and the fleet carries on,
// which is the property the whole architecture exists to provide, so it
// reaches the daemon as nothing at all - the client simply disconnects. It was
// ⌃C until park shipped: ⌃C detached because stopping was irreversible, and
// now that a park is recoverable it parks the focused agent instead. ⌃Q parks
// the whole fleet and closes Wake, which is the one exit that does reach the
// daemon.
//
// `wake attach` is the way back in for one agent, and it is the half of detach
// that was missing: a detached conversation, or one the daemon hung up on, was
// unreachable until it existed. **Bare `wake` used to be the only way back into
// the room, and it spawned to get there** - so seeing the fleet meant naming a
// person, and every `wake` typed to look at the room left a new agent nobody
// asked for. It branches now: a fleet gets the room and nothing else, and an
// empty machine gets first run, which still spawns - into that same room, since
// the agent is a roster row rather than a pane. See openroom.go.
//
// `wake new` is that spawn with a name on it, and it opens the conversation
// too - which is the whole of what it has that bare `wake` on an empty machine
// does not. It is a verb rather than a flag, and a verb rather than a bare
// `wake <name>`, because the dispatch below refuses an unrecognized word: a bare
// name would make `wake atach` a new agent instead of a typo, which is the one
// failure this command line is built to avoid.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/daemon"
)

// The subcommands. `daemon` is spelled by internal/daemon as well, which forks
// this binary with exactly that argument; the two must agree.
const (
	cmdDaemon = "daemon"
	cmdStatus = "status"

	// cmdFleets lists the named fleets. Zero arguments and no side effects: it
	// is the answer to "which of these did I start", which is the question a
	// second fleet creates the moment it exists.
	cmdFleets = "fleets"
	cmdStop   = "stop"
	cmdAttach = "attach"
	cmdNew    = "new"
	cmdFork   = "fork"

	// cmdImport adopts a transcript on this machine that Wake did not write.
	// With no argument it lists them, which is why it is the one verb whose
	// zero-argument form does something rather than being an arity error: the
	// listing is the picker, and a picker you have to already know the answer
	// to is not one.
	cmdImport = "import"

	// cmdManager starts the one session that can operate the fleet. It is a
	// user command, unlike the two below: `@manager` is what the room's
	// composer routes an unaddressed message to, and this is the only thing
	// that makes there be one.
	cmdManager = "manager"

	// cmdMCP serves the manager's tools on this process's own stdin and
	// stdout. Like `daemon` it is not a user command and is not in the usage
	// text below: it is spawned by a claude session through --mcp-config and
	// speaks JSON-RPC, so a person who runs it by hand gets a program that
	// appears to hang. It is in the verb list all the same, because the switch
	// refuses anything not in it - and a verb that is not a verb is a typo that
	// starts an agent.
	cmdMCP = "mcp"

	// cmdSetupTerminal configures the host terminal rather than the fleet -
	// the one verb here that dials no socket at all. See setupterminal.go.
	cmdSetupTerminal = "setup-terminal"
)

// The description column is a column. `wake fork <who> [name]` is five
// characters wider than the widest verb before it, so every line moved rather
// than one line hanging off the end of a block somebody reads by scanning down
// the left of the descriptions.
// A var rather than a const because the flag block below derives what it says
// from core and from spawningVerbs. A hand-written copy of the levels here
// would be the second spelling core/effort.go's own guard exists to prevent,
// and a hand-written list of verbs would drift from the one that is enforced.
var usage = `usage:
  wake                    reopen the room, or start an agent if nothing is running
  wake new [name]         open a conversation with a new agent, with a name you choose
  wake attach <who>       open a conversation with one already running, by name or id
  wake fork <who> [name]  branch a conversation: a new agent with the same history so far
  wake import [<id>]      list the claude sessions on this machine, or adopt one
  wake manager            start the manager: the session that can see and operate the whole fleet
  wake status             what is running
  wake stop               stop every session and the daemon
  wake fleets             the named fleets on this machine
  wake setup-terminal     make Shift+Enter send a newline, by configuring your terminal

flags, anywhere:
  --fleet <name>          which fleet to talk to; several can run in one directory

flags, on the verbs that start a session (` + list(spawningVerbs) + `):
  --effort <level>        ` + list(core.EffortLevels) + `
  --model <model>         an alias such as ` + list(core.ModelAliases) + `, or a model's full name
  --worktree <name>       create a git worktree of this name and run the session in it
  --max-budget-usd <amt>  stop the session once it has spent this much, in dollars
  --fallback-model <m,m>  models to fail over to, in order, when the first is overloaded
  --add-dir <dir>         let this session's tools reach a directory outside its own; repeatable
  --debug-file <name>     write this session's debug log there, under the fleet's own debug directory
  --debug <categories>    narrow that log, as api,hooks or !1p,!file; needs --debug-file

flags, on wake setup-terminal:
  --yes, -y               skip the confirmation prompt
  --undo                  remove what wake setup-terminal added`

func main() {
	// Before any verb dispatch: a wake binary re-exec'd as an agent's supervisor
	// carries WAKE_AGENT_LAUNCHER in its environment and --wake-agent-launcher as
	// its first argument, and it must become that supervisor rather than be read
	// as an unknown verb. The daemon is the only thing that starts one; see
	// internal/daemon's launch. Direct and non-unix spawns never take this path.
	if core.AgentLauncherRequested() {
		if err := core.RunAgentLauncher(); err != nil {
			fmt.Fprintln(os.Stderr, "wake:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "wake:", err)
		os.Exit(1)
	}
}

// run dispatches one invocation. It takes its arguments and its output rather
// than reading os.Args and writing to os.Stdout so the commands that print
// something are testable without a subprocess.
func run(args []string, out io.Writer) error {
	// The fleet comes off before anything else, including the verb: it decides
	// *which daemon* every verb below is about, so a flag read after dispatch
	// would be a flag half the paths had already ignored. It is also the one
	// flag that is legal on every verb rather than only on the spawning ones -
	// `wake status --fleet x` is exactly as meaningful as `wake --fleet x`.
	args, fleet, err := fleetFlag(args)
	if err != nil {
		return err
	}
	// A bare `wake` with no fleet named starts a **new** one, which is the whole
	// of the difference between this and every verb below it.
	//
	// The model is claude's: running the command again gives you another one,
	// and coming back to a particular one is a thing you ask for by name. The
	// cost is real and was chosen rather than overlooked - the obvious command
	// stops being the way back, so `wake --fleet <name>` and `wake fleets` are
	// the way back and openNewFleet prints the name it just made.
	//
	// Only for a bare `wake`: `wake status` and `wake new` with no fleet still
	// mean the unnamed one, because a verb that made a fleet to report on it
	// would report on an empty fleet every time.
	//
	// **Not when $WAKE_SOCKET is set**, and that is not a detail: that variable
	// names one exact socket, so a bare `wake` under it has always meant "the
	// fleet on this socket" and making a new one somewhere else instead points
	// the client at a different daemon from the one the caller set up. It is how
	// `make run` works, how the screen tests drive the real binary, and how the
	// scratch-socket rule in CLAUDE.md keeps a test off the owner's own fleet.
	// The pty suite caught this: nine tests timed out talking to a daemon that
	// was never on the socket they had started one on.
	if makesNewFleet(args, fleet, os.Getenv(daemon.SocketEnv)) {
		return openNewFleet(out)
	}

	// wake setup-terminal touches no fleet at all - handled here, before
	// daemon.FleetSocketPath below, because that call creates the fleet's
	// state directory (mkdir -p ~/.wake or a named fleet's own directory)
	// as a side effect of resolving a path this verb has no use for. Every
	// other verb needs that path; this is the one that would otherwise
	// leave a directory behind for a fleet that was never started.
	if len(args) > 0 && args[0] == cmdSetupTerminal {
		if _, err := setupTerminalFlags(args[1:]); err != nil {
			return err
		}
		return runSetupTerminal(args[1:], os.Stdin, out)
	}

	socket, err := daemon.FleetSocketPath(fleet)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return openRoom(socket, out)
	}

	// Flags come off before anything looks at the words, so a verb's arity is
	// still counted in names rather than in flags. See spawnflags.go.
	args, opts, err := spawnFlags(args)
	if err != nil {
		return err
	}
	if len(args) == 0 {
		// Every word was a flag. `wake --effort max` is a plausible typo for
		// `wake new --effort max`, and it used to index an empty slice and dump
		// a stack trace where every other malformed invocation here returns a
		// sentence. It is the one case spawnFlags cannot refuse for itself:
		// there is no verb left to name.
		return fmt.Errorf("%s are for %s, and no verb was given\n\n%s",
			list(flagNames()), list(spawningVerbs), usage)
	}

	// The verb is checked before the arity, so a typo is reported as a typo.
	// The other way round, `wake bogus arg` complains that "bogus" takes no
	// arguments, which quietly asserts that bogus is a command.
	switch args[0] {
	case cmdDaemon, cmdStatus, cmdStop, cmdAttach, cmdNew, cmdFork, cmdImport, cmdMCP, cmdManager, cmdFleets:
	default:
		// Not attached-with-extra-words. An unrecognized verb that silently
		// spawned a session would make every typo a new agent.
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
	if err := checkArity(args); err != nil {
		return err
	}

	switch args[0] {
	case cmdDaemon:
		return serveDaemon(socket)
	case cmdMCP:
		return serveMCP(socket, os.Stdin, os.Stdout)
	case cmdFleets:
		return printFleets(out)
	case cmdStatus:
		return printStatus(socket, out)
	case cmdStop:
		return stopFleet(socket, out)
	case cmdAttach:
		return reattach(socket, args[1], out)
	case cmdNew:
		return attach(socket, chosenName(args), opts, out)
	case cmdFork:
		return forkSession(socket, args[1], forkName(args), out)
	case cmdImport:
		if len(args) == 1 {
			return printImportable(out)
		}
		return importSession(socket, args[1], out)
	case cmdManager:
		return startManager(socket, opts, out)
	default:
		// Unreachable: the switch above admits nothing else. Stated rather
		// than routed, because a `default` that runs a verb turns "I added a
		// command and forgot this switch" into that command silently stopping
		// the fleet.
		return fmt.Errorf("unhandled command %q\n\n%s", args[0], usage)
	}
}

// checkArity rejects an invocation with the wrong number of words in it.
//
// Two verbs take an argument and they are deliberately different about it.
//
// `wake attach` requires one. With no id it could plausibly mean "attach to the
// only one running", and that guess is wrong the moment there are two - which
// is the ordinary case for a product whose whole premise is 15-30 of them. It
// asks instead, and the answer lists what there is.
//
// `wake new` takes at most one, because there *is* a right answer for the
// missing half: the daemon draws a name. Bare `wake new` is bare `wake`, which
// is what makes the two verbs one thing with an optional name rather than two
// ways to spawn.
func checkArity(args []string) error {
	switch args[0] {
	case cmdAttach:
		if len(args) != 2 {
			return fmt.Errorf("`wake attach` takes one session id or name\n\n%s", usage)
		}
	case cmdNew:
		if len(args) > 2 {
			return fmt.Errorf("`wake new` takes at most one name\n\n%s", usage)
		}
	case cmdFork:
		if len(args) < 2 || len(args) > 3 {
			return fmt.Errorf("`wake fork` takes one session id or name, and optionally a name for the fork\n\n%s", usage)
		}
	case cmdImport:
		// At most one, and the zero case is the listing rather than an error -
		// the only verb here for which "you did not say which" has a better
		// answer than a refusal, because the answer *is* the question they
		// were about to ask. No name argument, unlike `wake fork`: an import
		// already has more to say on its first line than a name would add, and
		// the daemon draws one from the pool.
		if len(args) > 2 {
			return fmt.Errorf("`wake import` takes at most one session id\n\n%s", usage)
		}
	default:
		if len(args) > 1 {
			return fmt.Errorf("%q takes no arguments\n\n%s", args[0], usage)
		}
	}
	return nil
}

// chosenName is the name `wake new` was given, or nothing.
//
// Nothing is not an error here - checkArity has already established that this
// is `wake new` with at most one word after it - and it means the same as bare
// `wake`: the daemon draws a name from the pool.
func chosenName(args []string) string {
	if len(args) < 2 {
		return ""
	}
	return args[1]
}

// serveDaemon is the forked background process.
//
// It opens the log first, because everything after this point is a process
// with no terminal: without a file behind the standard logger, what the daemon
// has to say goes to the /dev/null EnsureRunning gave it. Serve installs its
// own non-blocking sink over whatever is there and does not need a second one.
//
// SIGINT is handled as well as SIGTERM even though this process has no
// controlling terminal - a daemon started by hand from a shell, which is how
// it is debugged, still gets one.
func serveDaemon(socket string) (err error) {
	logFile, err := daemon.OpenLog(socket)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, logFile.Close()) }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return daemon.Serve(ctx, socket)
}

// agents is a count with the right noun on it.
func agents(n int) string {
	if n == 1 {
		return "1 agent"
	}
	return fmt.Sprintf("%d agents", n)
}

// trimmed is a line with no trailing blanks, for the messages assembled from
// optional halves.
func trimmed(s string) string { return strings.TrimRight(s, " ") }

// say writes one line.
//
// The error is returned rather than dropped: these lines are the whole output
// of two of the four verbs, and a `wake stop` whose confirmation went nowhere
// has told nobody anything. The one caller that deliberately ignores it says
// why at the call site.
func say(out io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(out, format+"\n", args...)
	return err
}
