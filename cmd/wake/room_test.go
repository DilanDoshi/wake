package main

// What the room opens with.
//
// The seed is the whole of this file's subject: the daemon's spawn confirmation
// carries every session it holds, so a client that passed it on gets a roster
// in its first frame and a client that did not gets an empty one until the next
// state change - which at an idle fleet is not a second, it is however long it
// takes somebody to do something.

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// fleetOf is a status report naming agents, the shape a spawn is confirmed
// with.
func fleetOf(names ...string) *rpc.Status {
	st := &rpc.Status{Running: true}
	for i, name := range names {
		st.Sessions = append(st.Sessions, rpc.SessionStatus{
			ID:    string(rune('a'+i)) + "-session",
			Name:  name,
			Dir:   "/repos/" + name,
			State: rpc.StateIdle,
		})
	}
	return st
}

// drawRoom drives the model the way Bubble Tea would: a size, then the keys.
//
// Named for what it does rather than for the command, because `openRoom` is now
// a function in this package - bare `wake` itself - and a test helper that
// shadows the thing under test is a build failure at best and a test asserting
// about the wrong function at worst.
func drawRoom(t *testing.T, app ui.App, keys ...tea.KeyMsg) string {
	t.Helper()
	var m tea.Model = app
	m, _ = m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m.View()
}

// The room opens knowing who is there. Nothing has been pushed to this model -
// it has no connection at all - so every name in the frame came from the report
// the client already held.
//
// Asserted through ⌃R because opening a DM closes the right sidebar, which is
// what `wake` does on the way in: the room beside you is doing the awareness
// job, and the sidebar is a key away.
func TestTheRoomOpensWithTheFleetTheClientAlreadyHad(t *testing.T) {
	held := &connection{}
	t.Cleanup(held.close)

	seed := fleetOf("sydney", "john", "marco")
	app := conversation(tempSocket(t), seed.Sessions[0], seed, nil, ui.Stream{}, held)

	view := drawRoom(t, app, tea.KeyMsg{Type: tea.KeyCtrlR})
	for _, name := range []string{"sydney", "john", "marco"} {
		if !strings.Contains(view, name) {
			t.Errorf("the room opened without %q, so the roster fills in on the next state change rather than immediately - which at an idle fleet is not a second:\n%s", name, view)
		}
	}
}

// And without a seed it does not, which is what makes the test above measure
// the seed rather than something else the model draws.
func TestWithoutASeedTheRoomOpensEmpty(t *testing.T) {
	held := &connection{}
	t.Cleanup(held.close)

	sess := rpc.SessionStatus{ID: "a-session", Name: "sydney", State: rpc.StateIdle}
	app := conversation(tempSocket(t), sess, nil, nil, ui.Stream{}, held)

	view := drawRoom(t, app, tea.KeyMsg{Type: tea.KeyCtrlR})
	for _, name := range []string{"john", "marco"} {
		if strings.Contains(view, name) {
			t.Errorf("an unseeded room knows about %q:\n%s", name, view)
		}
	}
	// The attached agent is still there: it comes from the session, not the
	// seed, so an empty roster is not an empty window.
	if !strings.Contains(view, "sydney") {
		t.Errorf("the conversation this client asked for is missing:\n%s", view)
	}
}

// --- the room bare `wake` opens -------------------------------------------

// Bare `wake` opens the room over the whole fleet and no conversation beside
// it, because there is no target: it is a request about the fleet rather than
// about one agent.
//
// The roster half is the same claim the seed test above makes and is asserted
// again here rather than assumed, because this model is built by a different
// function - `conversationRoom` does not call WithOpenDM at all, and a model
// that forgot the seed with it would open on an empty room with nothing to say
// so.
func TestTheRoomBareWakeOpensHasTheFleetAndNoConversation(t *testing.T) {
	held := &connection{}
	t.Cleanup(held.close)

	seed := fleetOf("sydney", "john", "marco")
	app := conversationRoom(tempSocket(t), seed, nil, ui.Stream{}, held)

	view := drawRoom(t, app)
	for _, name := range []string{"sydney", "john", "marco"} {
		if !strings.Contains(view, name) {
			t.Errorf("the room bare `wake` opens does not know about %q:\n%s", name, view)
		}
	}
	// ⌃W closes the focused conversation. With the keys in the room there is
	// nothing for it to close, and it says so rather than doing nothing - which
	// is how "no conversation opened" is visible from outside a model that does
	// not expose its panes. A frame that gained a *conversation* instead would
	// carry a second composer, not this line.
	shut := drawRoom(t, app, tea.KeyMsg{Type: tea.KeyCtrlW})
	if !strings.Contains(shut, "the room stays open") {
		t.Errorf("⌃W did not report that the room is what it was pressed in, so bare `wake` opened a conversation beside the room. "+
			"There is no target - naming a person to see the room is the thing this replaces:\n%s", shut)
	}
}

// And the way back is the room's, not a session's.
//
// redial asks liveSession about an id and App.sessionID is empty here, so the
// session dialer would refuse every reattach with a message about a session
// nobody asked for. This one asks whether there is a daemon at all - and refuses
// with a reason rather than letting connect() fork a fresh daemon and come back
// with an empty room that looks like a fleet that ended.
func TestTheRoomsWayBackRefusesADaemonThatIsGoneRatherThanStartingOne(t *testing.T) {
	socket := tempSocket(t)
	held := &connection{}
	t.Cleanup(held.close)
	// Registered before the call, because the failure this is watching for is a
	// redialRoom that *forks* one - and a detached daemon left behind while
	// tempSocket deletes its directory is unreachable by any wake command for
	// the rest of the machine's uptime.
	t.Cleanup(func() { _ = stopFleet(socket, io.Discard) })

	_, _, _, _, err := redialRoom(socket, held)
	if err == nil {
		t.Fatal("redialRoom reported success against a socket with no daemon behind it, which means " +
			"it forked one: the room would come back empty and read as a fleet that had ended")
	}
	if !strings.Contains(err.Error(), "no daemon") {
		t.Errorf("redialRoom failed with %q, want it to name the missing daemon", err)
	}
}

// And it does get back onto a daemon that is there.
//
// The negative above passes against a redialRoom that always failed, which is
// the shape of a reattach that never works - and a hang-up is exactly when
// nobody is watching the return code.
func TestTheRoomGetsBackOntoADaemonThatIsStillThere(t *testing.T) {
	d := startFakeDaemon(t, 0, rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: idAlpha, Name: "alex", State: rpc.StateIdle},
	}})
	held := &connection{}
	t.Cleanup(held.close)

	conn, stream, sess, fleet, err := redialRoom(d.socket, held)
	if err != nil {
		t.Fatalf("redialRoom against a live daemon: %v", err)
	}
	if conn == nil {
		t.Fatal("redialRoom returned no connection")
	}
	// The whole fleet comes back for the model to fold. The room draws every
	// agent, so this is BUG-11's worst case: without a report an agent that
	// blocked during the outage is on no surface. It is daemon.Status's read,
	// not a FrameStatus this client wrote.
	if fleet == nil || len(fleet.Sessions) != 1 || fleet.Sessions[0].ID != idAlpha {
		t.Errorf("redialRoom did not hand back the fleet it fetched (%+v); the room has no report to reconcile the roster and the cards against", fleet)
	}
	// Closed before it is drained, and in that order: rpc.ReadFrames has no
	// cancellation, so draining a connection nothing has hung up on waits for
	// the daemon rather than for the frames.
	t.Cleanup(func() {
		_ = conn.Close()
		drain(stream)
	})
	// A zero session, deliberately: ui.reattachedText renders that through its
	// default arm, which says what a room can honestly say. There is no one
	// agent here whose state is the thing to report.
	if sess.ID != "" {
		t.Errorf("redialRoom reattached as session %q. The room is not a conversation with anybody, "+
			"and naming one would put a state on the notice row that belongs to a session this "+
			"client never asked about", sess.ID)
	}
}
