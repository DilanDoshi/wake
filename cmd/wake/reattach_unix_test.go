//go:build unix

// The half of `wake attach` that needs a real daemon and a real agent behind
// it: joining a session this process did not spawn, over a connection opened
// after the first one was gone.
//
// This is C1's scenario end to end. Everything above it - the drain that no
// longer stops when the view is busy, the write that no longer parks without a
// deadline - reduces how often a client is hung up on. None of it makes a
// hang-up recoverable. This does, and nothing else in the tree asserts the
// daemon-side property it rests on: that a client which did not spawn a session
// can still drive it and still hear it.

package main

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// And a fleet whose daemon died is the third answer, not the second: those
// agents are still on the machine and `wake status` is where they are dealt
// with.
func TestAttachSaysSoWhenTheDaemonDiedLeavingAgentsBehind(t *testing.T) {
	socket := tempSocket(t)
	writeRoster(t, socket, idAlpha, startBlockedProcess(t, idAlpha))

	_, _, err := liveSession(socket, idAlpha)
	if err == nil {
		t.Fatal("attaching to an orphan was allowed")
	}
	if !strings.Contains(err.Error(), "nothing holding them") {
		t.Errorf("the refusal does not describe an orphaned fleet: %v", err)
	}
}

// The scenario C1 describes, from the far side of the disconnect.
//
// A conversation is open on a live agent; the client goes away - dragged
// window, hung-up daemon, ⌃O, it does not matter which, because the daemon sees
// the same thing in every case. Then a *different* client asks for that session
// by id and must get the same conversation: the daemon takes its messages and
// broadcasts that session's events to it.
//
// Mutation check: making liveSession return its argument without asking the
// daemon leaves this failing at "the daemon refused a message from a client
// that did not spawn the session".
func TestAttachJoinsASessionItDidNotSpawn(t *testing.T) {
	f := startForkedFleet(t)

	// The disconnect. As far as the daemon is concerned this is every way a
	// client can vanish, including the one C1 is about.
	f.detach()

	sess, _, err := liveSession(f.socket, f.sessionID)
	if err != nil {
		t.Fatalf("the session was not attachable after its client went away: %v", err)
	}
	if sess.ID != f.sessionID {
		t.Fatalf("attached to session %s, want %s", sess.ID, f.sessionID)
	}
	if sess.State == rpc.StateEnded {
		t.Fatalf("the session ended when its client disconnected, which is the whole thing that must not happen")
	}

	conn, stream, err := connect(f.socket, io.Discard)
	if err != nil {
		t.Fatalf("reattaching: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		for range stream.Frames {
		}
		<-stream.Errs
	})

	// A prefix, because that is what `wake status` gives a person to copy.
	if _, _, err := liveSession(f.socket, f.sessionID[:8]); err != nil {
		t.Errorf("a prefix of a live session id did not resolve: %v", err)
	}

	// The new client drives the session. The fake agent reads one line from
	// stdin and exits, so the message landing is observable from outside: the
	// session ends. A daemon that refused the send - "unknown session", or
	// because this client is not the one that spawned it - answers with an
	// error frame instead, and the session stays up.
	if err := rpc.WriteFrameTo(conn, rpc.Frame{Kind: rpc.FrameSend, SessionID: f.sessionID, Text: "still there?"}); err != nil {
		t.Fatalf("sending on the reattached connection: %v", err)
	}
	awaitEnding(t, stream, f.sessionID)
}

// awaitEnding waits for the daemon to report a session gone, failing on the
// refusal that means the message never reached the agent.
func awaitEnding(t *testing.T, stream ui.Stream, sessionID string) {
	t.Helper()

	deadline := time.After(testTimeout)
	for {
		select {
		case fr, ok := <-stream.Frames:
			if !ok {
				t.Fatal("the daemon hung up on the reattached client")
			}
			if fr.Kind == rpc.FrameError && (fr.SessionID == sessionID || fr.SessionID == "") {
				t.Fatalf("the daemon refused a message from a client that did not spawn the session: %s", fr.Text)
			}
			if fr.Status == nil {
				continue
			}
			for _, s := range fr.Status.Sessions {
				if s.ID == sessionID && s.State == rpc.StateEnded {
					return
				}
			}
		case err := <-stream.Errs:
			t.Fatalf("reading on the reattached connection: %v", err)
		case <-deadline:
			t.Fatalf("the message sent from the reattached client never reached the agent within %v", testTimeout)
		}
	}
}

// The dialer the TUI reaches for when its connection ends, exercised without a
// terminal. It is the half of converse that is wiring rather than a program,
// which is why it is a function of its own.
//
// Mutation check: dialing before asking the fleet - which is the ordering
// mistake that matters, because connect() forks a daemon when nothing is
// listening - leaves TestAttachRefusesWhenNoDaemonIsRunning green and this one
// failing at "the previous connection was left open".
func TestTheDialerReplacesTheConnectionItWasHoldingOnTo(t *testing.T) {
	f := startForkedFleet(t)
	f.detach()

	first, stream, err := connect(f.socket, io.Discard)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	held := &connection{}
	held.replace(first)
	t.Cleanup(held.close)
	go func() {
		for range stream.Frames {
		}
	}()

	// Built as converse builds it, so the wiring is exercised, and then the
	// dialer is called directly - a tea.Cmd's goroutine is the only thing that
	// ever calls it, and there is no terminal here to run one.
	_ = conversation(f.socket, rpc.SessionStatus{ID: f.sessionID, Name: "alex"}, nil, first, stream, held)
	conn, next, sess, fleet, err := redial(f.socket, f.sessionID, held)
	if err != nil {
		t.Fatalf("the dialer failed against a live session: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		for range next.Frames {
		}
		<-next.Errs
	})
	if sess.ID != f.sessionID {
		t.Errorf("the dialer attached to %s, want %s", sess.ID, f.sessionID)
	}
	// The whole fleet comes back rather than being discarded, so the model can
	// fold it and mint the card for an ask raised during the outage. It is the
	// same daemon.Status read liveSession already did, not a FrameStatus.
	if fleet == nil || len(fleet.Sessions) == 0 {
		t.Error("the dialer discarded the fleet it fetched; reattach then has no report to reconcile the cards against")
	}
	if conn == first {
		t.Error("the dialer handed back the connection that had already ended")
	}

	// The one that hung up is closed rather than leaked. A window that
	// reattaches a few times over a day would otherwise hold a file descriptor
	// and a reader goroutine per outage.
	if _, err := first.Write([]byte("x")); err == nil {
		t.Error("the previous connection was left open")
	}
}

// And a dialer whose session has gone reports that rather than opening a
// connection to a conversation that cannot exist.
func TestTheDialerRefusesASessionThatIsGone(t *testing.T) {
	f := startForkedFleet(t)
	f.detach()

	held := &connection{}
	t.Cleanup(held.close)
	gone := uuid.NewString()
	_ = conversation(f.socket, rpc.SessionStatus{ID: gone, Name: "alex"}, nil, nil, ui.Stream{}, held)

	if _, _, _, _, err := redial(f.socket, gone, held); err == nil {
		t.Fatal("the dialer opened a connection for a session nothing is holding")
	}
}

// close is reached by a deferred cleanup and by a replace, so it has to be
// idempotent: a double close that panicked would take the process out on the
// way to printing the detach line.
func TestClosingTheHeldConnectionTwiceIsHarmless(t *testing.T) {
	held := &connection{}
	held.close()

	mine, theirs := net.Pipe()
	t.Cleanup(func() { _ = theirs.Close() })
	held.replace(mine)
	held.close()
	held.close()

	if _, err := mine.Write([]byte("x")); err == nil {
		t.Error("close left the connection open")
	}
}
