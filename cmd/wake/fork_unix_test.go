//go:build unix

// The half of `wake fork` that needs a real daemon behind it: the frame this
// client builds, met by the dispatch that has to accept it, and the fleet a
// machine with no daemon left behind.
//
// # Why this file exists at all
//
// converse_unix_test.go's header states the charter and this is the same class:
// internal/daemon proves a fork frame reaches an agent, but it **hand-builds
// that frame**, and cmd/wake proves the client builds one, but only against a
// fake that reads three fields. A divergence between the frame the client
// writes and the frame the daemon's own tests write is therefore invisible to
// both packages at once - which is precisely the shape of the defect a
// recording spike already found here once: *bytes that were valid, correctly
// addressed and completely ineffective, with every test asserting their shape
// passing.*
//
// `wake fork` was the only starting verb whose bytes had been asserted for
// shape alone, and nothing anywhere exercised a **successful** fork from this
// side. This closes both.

package main

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A fork the client asks for, end to end, against the daemon that has to answer.
//
// # What it composes, and why it is not forkSession itself
//
// forkSession ends in converse, which builds a Bubble Tea program and takes the
// terminal - so the successful path cannot be driven from a test, which is the
// reason nothing had driven it. What runs here is exactly what openSession runs
// in front of that: forkParent to resolve, connect to dial, requestFork to
// write, awaitSpawn to wait. Every one of those is the shipped function, and
// TestTheIdForkWaitsOnIsTheOneItMintedAndNeverTheParents already pins statically
// that forkSession hands the same minted id to the last two - so the composition
// here is the composition forkSession performs, minus the terminal.
//
// # What only a real daemon can answer
//
// maySpawn, forkSource, the name claim, core's identity switch and an actual
// process. The fork's id appearing in a live process's argv is the assertion
// that reaches furthest: it is the reaper's own proof of identity, and it can
// only be true if the client's frame survived dispatch, if the daemon read the
// parent's directory rather than a Dir the client never sent, and if
// identityArgs emitted the triple around the id this client minted.
func TestAForkTheClientAsksForReachesARealAgent(t *testing.T) {
	f := startForkedFleet(t)

	// Resolved through the client's own matcher, against the daemon's own
	// report - not from f.sessionID directly, because "which session did they
	// mean" is half of what this verb does.
	parent, err := forkParent(f.socket, f.sessionID)
	if err != nil {
		t.Fatalf("forkParent against a real daemon: %v", err)
	}
	if parent.ID != f.sessionID {
		t.Fatalf("forkParent resolved %s, want the live session %s", parent.ID, f.sessionID)
	}

	conn, stream, err := connect(f.socket, io.Discard)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		for range stream.Frames {
		}
		<-stream.Errs
	})

	forkID := uuid.NewString()
	if err := requestFork(conn, forkID, parent.ID, "twin"); err != nil {
		t.Fatalf("requestFork: %v", err)
	}

	// The client's own wait, on the client's own id. A daemon that refused the
	// frame answers here with an error addressed to that same id, so a
	// divergence arrives as a failure rather than as a hang.
	fork, fleet, _, err := awaitSpawn(stream, forkID)
	if err != nil {
		t.Fatalf("the daemon refused the frame this client builds: %v. That is the whole reason "+
			"this test exists - internal/daemon's fork tests hand-build their frames, so nothing "+
			"else in the tree compares the two", err)
	}

	checkConfirmedFork(t, fork, fleet, parent.ID, forkID)

	// The furthest assertion: a real process group carrying the fork's UUID.
	awaitAgent(t, forkID)
}

// checkConfirmedFork holds everything the daemon's confirmation has to say about
// a fork the client asked for.
func checkConfirmedFork(t *testing.T, fork rpc.SessionStatus, fleet *rpc.Status, parentID, forkID string) {
	t.Helper()

	if fork.ID != forkID {
		t.Errorf("the fork came back as %s, want the id this client minted, %s: Wake originates "+
			"identity and the reaper needs that UUID in the argv", fork.ID, forkID)
	}
	if fork.ParentID != parentID {
		t.Errorf("the fork reports ParentID %q, want %q - nothing on Claude's wire carries this, "+
			"so a daemon that does not record it has thrown the relationship away", fork.ParentID, parentID)
	}
	if fork.Name != "twin" {
		t.Errorf("the fork is named %q, want the name the frame asked for", fork.Name)
	}
	if fork.State == rpc.StateEnded {
		t.Errorf("the fork ended immediately: %s", fork.Error)
	}
	// The confirmation carries the whole fleet, which is what lets the room open
	// with a roster rather than filling in over the next thirty seconds.
	if !holdsBoth(fleet, parentID, forkID) {
		t.Errorf("the confirmation does not carry both the parent and the fork: %+v", fleet)
	}
}

// holdsBoth reports whether a fleet report names two sessions.
func holdsBoth(st *rpc.Status, first, second string) bool {
	if st == nil {
		return false
	}
	found := map[string]bool{}
	for _, s := range st.Sessions {
		found[s.ID] = true
	}
	return found[first] && found[second]
}

// A machine whose daemon died is the case the orphan state actually reaches,
// and the sentence comes from resolveSession rather than from any rule of
// forkParent's own.
//
// This replaces a test that built `rpc.Status{Running: true}` with an orphaned
// row in it and asserted forkParent refused it. That status is a shape rpc's own
// type documentation calls impossible - orphaned rows come only from
// daemon.FleetOnDisk, which is what daemon.Status returns when the **dial
// fails**, so Running is false and resolveSession refuses before any state is
// read. The old test passed against a fabricated input and, worse, the guard
// over it had begun pinning the unreachable branch it covered. This drives the
// real path: a roster on disk, a live process, and no daemon at all.
func TestForkRefusesAFleetWhoseDaemonDied(t *testing.T) {
	socket := tempSocket(t)
	writeRoster(t, socket, idAlpha, startBlockedProcess(t, idAlpha))

	_, err := forkParent(socket, idAlpha)
	if err == nil {
		t.Fatal("forking an orphan was allowed: nothing is holding it, so there is no daemon to add the fork to")
	}
	if !strings.Contains(err.Error(), "nothing holding them") {
		t.Errorf("the refusal does not describe an orphaned fleet: %v", err)
	}
	if !strings.Contains(err.Error(), "wake status") {
		t.Errorf("the refusal is %q and does not say where to look", err)
	}
}
