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
// afterDaemon is a stop whose daemon has just unlinked its socket: it may still
// hold its lock a moment, so that is waited out, and it cleared the park book,
// so a session parked since keeps the fleet. With no daemon the stop forgets the
// parked ones itself. Exit 0 either way: the stop has happened, and a fleet that
// stays says why.
func endFleet(socket string, afterDaemon bool, out io.Writer) error {
	var wait time.Duration
	if afterDaemon {
		wait = releaseCeiling
	}
	deadline := time.Now().Add(wait)
	end, err := daemon.RemoveFleet(socket, !afterDaemon)
	for errors.Is(err, daemon.ErrFleetRunning) && time.Now().Before(deadline) {
		time.Sleep(releasePoll)
		end, err = daemon.RemoveFleet(socket, !afterDaemon)
	}
	if line := endLine(end, err); line != "" {
		return say(out, "%s", line)
	}
	return nil
}

// errLine is an error on one line: errors.Join puts each of its own on its own.
func errLine(err error) string { return strings.ReplaceAll(err.Error(), "\n", "; ") }

// endLine is what a removal came to, or nothing for a socket with no fleet
// directory of its own.
func endLine(end daemon.FleetEnd, err error) string {
	name, left := end.Name, end.Left
	forgot := fmt.Sprintf("%s parked (%s)", agents(len(end.Forgot)), strings.Join(end.Forgot, ", "))
	switch {
	case name == "":
		return ""
	case errors.Is(err, daemon.ErrFleetRunning):
		return fmt.Sprintf("Fleet %s stays: a daemon is running on it again.", name)
	case err != nil && len(end.Forgot) > 0:
		return fmt.Sprintf("Fleet %s stays: %s. Its park book was already removed, so it forgot %s.", name, errLine(err), forgot)
	case err != nil:
		return fmt.Sprintf("Fleet %s stays: %s.", name, errLine(err))
	case end.Removed && len(end.Forgot) > 0:
		return fmt.Sprintf("Forgot %s and removed fleet %s, its log with it; their transcripts stay on disk, "+
			"and the name is free for a new fleet.", forgot, name)
	case end.Removed:
		return fmt.Sprintf("Removed fleet %s, its log with it; the name is free for a new fleet.", name)
	}
	if why := keptBecause(name, left); why != "" {
		return fmt.Sprintf("Fleet %s stays%s%s", name, why, parkedToo(len(left.Parked)))
	}
	if len(left.Parked) > 0 {
		return fmt.Sprintf("Fleet %s stays: %s parked in it after the stop began, which it did not forget; `%s` forgets them.",
			name, agents(len(left.Parked)), rpc.FleetCommand(name, cmdStop))
	}
	return "" // a fleet that was not there
}

// keptBecause is what kept a fleet that holds something it cannot lose.
func keptBecause(name string, left daemon.FleetLeft) string {
	switch {
	case left.Err != nil:
		return fmt.Sprintf(", since what it holds could not be read: %s.", errLine(left.Err))
	case left.Crashed:
		return fmt.Sprintf(": its daemon crashed, and `%s` recovers what it left.", rpc.FleetCommand(name))
	case len(left.Kept) > 0:
		return fmt.Sprintf(": it holds %s, which Wake does not remove. Move that out, then `%s` removes the fleet.",
			strings.Join(left.Kept, ", "), rpc.FleetCommand(name, cmdStop))
	}
	return ""
}

// parkedToo says the parked sessions of a fleet that stays stayed with it.
func parkedToo(n int) string {
	switch n {
	case 0:
		return ""
	case 1:
		return " The 1 agent parked in it stays too."
	}
	return fmt.Sprintf(" The %s parked in it stay too.", agents(n))
}
