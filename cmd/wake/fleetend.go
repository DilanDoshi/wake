package main

// The end of a stopped fleet: `wake stop` removes a named fleet once it is down
// and done, so its name is free again (BUG-21; daemon/fleetend.go decides).

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// endFleet removes the fleet at socket if it is done and says what came of it.
// wait covers a daemon that has just unlinked its socket and not yet let go of
// its lock; with no daemon to wait for it is zero. Exit 0 either way: the stop
// has happened, and a fleet that stays says why.
func endFleet(socket string, wait time.Duration, out io.Writer) error {
	deadline := time.Now().Add(wait)
	end, err := daemon.RemoveFleet(socket)
	for errors.Is(err, daemon.ErrFleetRunning) && time.Now().Before(deadline) {
		time.Sleep(releasePoll)
		end, err = daemon.RemoveFleet(socket)
	}
	if line := endLine(end, err); line != "" {
		return say(out, "%s", line)
	}
	return nil
}

// endLine is what a removal came to, or nothing for a socket with no fleet
// directory of its own.
func endLine(end daemon.FleetEnd, err error) string {
	name, left := end.Name, end.Left
	switch {
	case name == "":
		return ""
	case errors.Is(err, daemon.ErrFleetRunning):
		return fmt.Sprintf("Fleet %s stays: a daemon is running on it again.", name)
	case err != nil:
		return fmt.Sprintf("Fleet %s stays: %v.", name, err)
	case end.Removed && len(end.Forgot) > 0:
		return fmt.Sprintf("Forgot %s parked (%s) and removed fleet %s, its log with it; their transcripts stay on disk, "+
			"and the name is free for a new fleet.", agents(len(end.Forgot)), strings.Join(end.Forgot, ", "), name)
	case end.Removed:
		return fmt.Sprintf("Removed fleet %s, its log with it; the name is free for a new fleet.", name)
	case left.Err != nil:
		return fmt.Sprintf("Fleet %s stays, since what it holds could not be read: %v.", name, left.Err)
	case left.Crashed:
		return fmt.Sprintf("Fleet %s stays: its daemon crashed, and `%s` recovers what it left.", name, rpc.FleetCommand(name))
	case len(left.Kept) > 0:
		return fmt.Sprintf("Fleet %s stays: it holds %s, which Wake does not remove. Move that out, then `%s` removes the fleet.",
			name, strings.Join(left.Kept, ", "), rpc.FleetCommand(name, cmdStop))
	}
	return "" // a fleet that was not there
}
