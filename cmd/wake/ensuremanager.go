// Making sure the fleet has a manager, on the way into the room.
//
// # Why the room starts one at all
//
// Spec §12 gives the manager "a permanent seat in every group", and until this
// the build had no way to seat it: `wake manager` at a shell was the only thing
// that produced one, so the room's own composer refused every unaddressed draft
// and pointed the operator *out* of the room to fix it. A front door that has to
// be repaired from outside is not a front door. Every path into the TUI ends
// here now, so the seat is filled by the time anybody can type into it.
//
// # Why cmd/wake writes the frame and internal/ui does not
//
// This is requestFleet's slot and its argument, one frame over: the write
// happens before tea.NewProgram, on the connection this process has just
// finished a handshake on, so there is no draw goroutine anywhere near it and
// no reply for a later frame to be confused with. internal/ui owns the
// *decision* - ui.ManagerFrames is the same function `/manager` uses, so the
// default and the command cannot disagree about what a fleet with no manager
// needs - and this owns the socket.
//
// # What it does not do
//
// It does not wait. `wake manager` waits for its confirmation because a person
// typed a command and is owed an answer on a terminal; nobody typed this, and
// the room is the answer - the manager appears as a roster row when the daemon
// announces it. It also does not unpark on its own beyond the ruling in
// ui.ManagerFrames: a manager parked with `/manager` comes back on the next
// `wake`, because "parked deliberately" and "parked by ⌃Q" are the same record
// on disk and parkedRecord carries no reason field to tell them apart.
//
// A refusal is left visible rather than swallowed. The two that can arrive are
// another window having started one inside the round trip, and a fleet already
// at daemon.liveCap - both true, both rare, and a client that hid them would be
// hiding the reason the room has no default addressee.

package main

import (
	"net"
	"os"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// managerAskFailed is what the notice row says when the ask could not be
// written at all.
//
// It names the consequence rather than only the error, because the symptom an
// operator meets is not "a write failed" - it is a room that refuses the first
// thing they type into it with no manager to blame.
const managerAskFailed = "wake could not ask for a manager, so the room has no default addressee: %v"

// ensureManager asks the daemon for a manager when the fleet is not running one.
//
// The directory is this process's own, which is what `wake manager` sends and
// what `/new` defaults to, so all three agree about where a session nobody named
// a directory for runs.
//
// It reports rather than returns an error: every caller is about to open an alt
// screen, so notice is the channel a failure has to arrive on, and none of them
// should refuse to open a room because the manager could not be asked for.
func ensureManager(conn net.Conn, fleet *rpc.Status) {
	dir, err := os.Getwd()
	if err != nil {
		notice.Report(managerAskFailed, err)
		return
	}
	for _, f := range ui.ManagerFrames(fleet, dir, uuid.NewString()) {
		if err := rpc.WriteFrameTo(conn, f); err != nil {
			notice.Report(managerAskFailed, err)
			return
		}
	}
}
