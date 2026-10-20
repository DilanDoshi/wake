// Bare `wake`: the front door.
//
// It opens the room over whatever the daemon holds and spawns no agent of its
// own: the manager, which ensureManager starts when the fleet has none, is the
// only session it asks for (owner's 2026-10-16 ruling, replacing first run's
// pooled agent). `wake new` is the verb that means "an agent, and put me in it".

package main

import (
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/DilanDoshi/wake/internal/render"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// openRoom opens the room with no conversation beside it: bare `wake` is a
// request about the fleet, so there is no target to open.
//
// It shares connect, the held connection and converseModel with openSession;
// the flow that opens a DM is `wake new`, `wake fork` and `wake attach`.
func openRoom(socket string, out io.Writer) error {
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

	if err := requestFleet(conn); err != nil {
		held.close()
		drain(stream)
		return err
	}
	fleet, read, err := awaitFleet(stream)
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

// requestFleet asks the daemon what it is holding.
//
// **This is the seed, and it is asked for here rather than by the model** - for
// the same reason it always was, which is that the model does not exist yet. It
// wants only "is my session ended" out of a status report, and a snapshot can
// miss an ending but never invent one, so it folds a reply and a push the same
// way. This writes the frame in cmd/wake, before the model exists, and consumes
// the reply below; the model is handed the stream afterwards. Nothing on this
// path spawns before the reply, so there is no confirmation for it to be
// confused with: ensureManager's spawn is written only after it.
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
