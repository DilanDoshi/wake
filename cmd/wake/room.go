// The room, from this side of the socket: the model converse runs, and the
// fleet report it opens with.
//
// Split from attach.go, which owns getting a connection and running a program
// over it. This file is wiring and can be asserted on without a terminal; that
// one opens an alt screen and takes stdin, and keeping the dialer inside it is
// how the reattach path would go untested.

package main

import (
	"errors"
	"fmt"
	"io"
	"net"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// conversation is the model converse runs: the room over the whole fleet, one
// agent's DM open beside it, and the way back onto a connection that hung up.
//
// # Why the seed is passed rather than fetched
//
// Because this client already holds it. The spawn's confirmation is a
// FrameStatusReply carrying s.fleet() - *every* session, not just the new one -
// so bare `wake` gets a full roster for free, and the reattach path fetched one
// through liveSession on the way in to answer "is that session still there".
// Asking again here would be a second round trip for an answer already in hand,
// and it would put a reply in flight against a model that reads both status
// kinds the same way. The one FrameStatus ui.App writes is ⌃Q's, behind the
// FrameParkAll and after everything here is long over, so a reply arriving on
// this connection while the room is opening can only be its own spawn's
// confirmation.
//
// Nil is legitimate and means "wait for the first push" - the room fills in as
// the daemon announces state changes, which is what it did before the seed
// existed.
//
// # Why the DM opens at all
//
// Because `wake` and `wake attach` are both requests about one agent. The room
// is beside that conversation rather than instead of it: §8's amendment makes
// them peers, and the pane you were asking for is the one that should have the
// cursor in it.
func conversation(socket string, sess rpc.SessionStatus, seed *rpc.Status, conn net.Conn, stream ui.Stream, held *connection) ui.App {
	return ui.NewRoomApp(conn, stream, seed).
		WithOpenDM(sess.ID, displayName(sess)).
		WithSessions(machineSessions{}).
		WithDialer(func() (net.Conn, ui.Stream, rpc.SessionStatus, *rpc.Status, error) {
			return redial(socket, sess.ID, held)
		})
}

// conversationRoom is the model bare `wake` runs: the room over the whole fleet,
// nothing beside it, and the way back onto a connection that hung up.
//
// It differs from conversation in the two things bare `wake` does not have. No
// DM opens, because there is no target - and WithOpenDM("") already returns the
// model untouched, so this simply does not call it. And the dialer reattaches to
// the *room* rather than to a session, because App.sessionID is empty here:
// redial asks liveSession about an id, and there is no id to ask about.
func conversationRoom(socket string, seed *rpc.Status, conn net.Conn, stream ui.Stream, held *connection) ui.App {
	return ui.NewRoomApp(conn, stream, seed).
		WithSessions(machineSessions{}).
		WithDialer(func() (net.Conn, ui.Stream, rpc.SessionStatus, *rpc.Status, error) {
			return redialRoom(socket, held)
		})
}

// redialRoom is redial with no session to check.
//
// The ordering argument is redial's and it still holds: connect() forks a daemon
// when nothing is listening, so dialling first would answer "the daemon died" by
// starting a fresh one - and the room would come back empty and look like a
// fleet that had ended. Asking the fleet first makes that an error with a
// reason.
//
// It returns the whole fleet st for the model to fold, which the room is where
// it matters most: the room draws every agent, so an agent that blocked during
// the outage is invisible on this surface until a report reconciles the roster,
// the awareness strip and its card - and after a reattach nothing else pushes
// one. It is this process's own daemon.Status read, not a FrameStatus ui.App
// writes. See ui.App.reattached.
//
// It returns a zero SessionStatus, which ui.reattachedText renders through its
// default arm as *"reattached; what it said meanwhile is not in the conversation
// above"*. That is the honest line for a room - there is no one agent whose
// state is the thing to report.
//
// **That sentence was a claim this comment made and the code did not keep**,
// found in review: `reattachedText` interpolated `@` and a name into every arm,
// so a room got `"reattached to @; …"` - the bare handle this package's own
// `reattach` records as reading like a second agent. The room arm is
// `ui.App.reattachTarget` now and this quote is checkable;
// `TestARoomThatHangsUpNamesNobodyRatherThanABareHandle` is what holds it.
func redialRoom(socket string, held *connection) (net.Conn, ui.Stream, rpc.SessionStatus, *rpc.Status, error) {
	st, err := daemon.Status(socket)
	if err != nil {
		return nil, ui.Stream{}, rpc.SessionStatus{}, nil, fmt.Errorf("ask what is running: %w", err)
	}
	if !st.Running {
		return nil, ui.Stream{}, rpc.SessionStatus{}, nil, errors.New("no daemon is running, so there is no room to get back to")
	}
	// io.Discard rather than the terminal: Bubble Tea owns the screen while this
	// runs, and connect's "waiting…" line would be written straight onto the alt
	// screen's canvas.
	conn, stream, err := connect(socket, io.Discard)
	if err != nil {
		return nil, ui.Stream{}, rpc.SessionStatus{}, nil, err
	}
	held.replace(conn)
	return conn, stream, rpc.SessionStatus{}, &st, nil
}
