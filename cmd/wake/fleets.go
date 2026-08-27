package main

// `--fleet`, and the verb that lists what it can name.
//
// A fleet is a directory with a socket in it (internal/daemon/fleet.go), so
// everything here is about turning a word on the command line into one - and
// about refusing the words that are not names.

import (
	"fmt"
	"io"
	"strings"

	"github.com/DilanDoshi/wake/internal/daemon"
)

// fleetFlagName is the flag, spelled once.
const fleetFlagName = "--fleet"

// noFleetsYet is what `wake fleets` says on a machine with none.
//
// It names how to make one, because "none" is a complete answer to the question
// asked and a useless one to the person asking it: somebody typing this verb has
// either just made a fleet or is about to.
const noFleetsYet = "no named fleets. `wake --fleet <name>` starts one; a bare `wake` is the unnamed fleet, " +
	"which is separate from all of them"

// fleetFlag takes `--fleet <name>` off the arguments, returning the rest.
//
// Hand-parsed rather than through spawnFlags, and the two are deliberately not
// merged: that one is for flags that configure *a session being started*, and
// refuses itself on any other verb. This one is legal everywhere, because it
// chooses which daemon the verb is addressed to - `wake stop --fleet x` is a
// sentence and `wake stop --effort max` is not.
//
// Only the first occurrence is meaningful and a second is an error rather than
// a silent last-wins: two fleets on one command line is somebody who has lost
// track of which one they meant.
func fleetFlag(args []string) (rest []string, fleet string, err error) {
	rest = make([]string, 0, len(args))
	seen := false
	for i := 0; i < len(args); i++ {
		if args[i] != fleetFlagName {
			rest = append(rest, args[i])
			continue
		}
		if seen {
			return nil, "", fmt.Errorf("%s was given twice: a command is addressed to one fleet", fleetFlagName)
		}
		if i+1 >= len(args) {
			return nil, "", fmt.Errorf("%s needs a name: which fleet to talk to", fleetFlagName)
		}
		i++
		fleet, seen = args[i], true
		if strings.HasPrefix(fleet, "-") {
			// Almost certainly the next flag, which means the name was left
			// out. Taking it as a name would create a fleet called `--effort`.
			return nil, "", fmt.Errorf("%s was given %q, which is a flag rather than a fleet name",
				fleetFlagName, fleet)
		}
	}
	return rest, fleet, nil
}

// printFleets lists the named fleets.
//
// Names only, and no status: asking each one what it is running means dialling
// every socket on the machine, and `wake status --fleet <name>` is the question
// that already answers that for one. A list that took a second per stopped
// fleet would be a list nobody waits for.
func printFleets(out io.Writer) error {
	names, err := daemon.Fleets()
	if err != nil {
		return err
	}
	// One checked write rather than a line at a time, which is printStatus's
	// shape: a listing half-written to a closed pipe is worse than one that
	// says it could not be written.
	_, err = io.WriteString(out, formatFleets(names))
	return err
}

// formatFleets is the listing, separated from the write so a test reads a
// string rather than driving an io.Writer.
func formatFleets(names []string) string {
	if len(names) == 0 {
		return noFleetsYet + "\n"
	}
	return strings.Join(names, "\n") + "\n"
}

// newFleetLine is what a fresh fleet says about itself before the room opens.
//
// **It is the only way back.** Bare `wake` no longer reopens what you had, so a
// fleet whose name was never shown is one somebody can find again only by
// running `wake fleets` and guessing which row is theirs. It is printed before
// the alt screen for the reason announceFork's line was: after it, the terminal
// belongs to Bubble Tea and anything written is drawn over.
const newFleetLine = "fleet %s. `wake --fleet %s` comes back to it; `wake fleets` lists them all.\n"

// openNewFleet makes a fleet nobody named and opens it.
func openNewFleet(out io.Writer) error {
	socket, name, err := daemon.NewFleetSocketPath()
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, newFleetLine, name, name); err != nil {
		return err
	}
	return openRoom(socket, out)
}

// makesNewFleet is whether this invocation starts a fleet nobody named.
//
// A function rather than a condition inline in run, so the guard over it reads
// the same expression the binary does. It is three clauses and each one is a
// different mistake:
//
//   - **a verb is never a new fleet.** `wake status` that made one would report
//     on an empty fleet, every time.
//   - **a named fleet is the one named.** That is what `--fleet` is for.
//   - **$WAKE_SOCKET is that socket.** It names one exact path, so a bare `wake`
//     under it has always meant the fleet there - and making one somewhere else
//     puts the client on a different socket from the daemon whoever set that
//     variable is running. It is how `make run` works, how the screen suite
//     drives the real binary, and how CLAUDE.md's scratch-socket rule keeps a
//     test off the owner's own fleet. The pty suite is what caught it: nine
//     tests timed out talking to a daemon that was never on their socket.
func makesNewFleet(args []string, fleet, socket string) bool {
	return len(args) == 0 && fleet == daemon.DefaultFleet && socket == ""
}
