//go:build unix

// The half of `wake import` that needs a real daemon behind it.
//
// fork_unix_test.go's header states the charter and this is the same class, one
// verb later: internal/daemon proves an import frame reaches an agent but
// **hand-builds that frame**, and cmd/wake proves the client builds one but only
// against its own resolution. A divergence between the two is invisible to both
// packages at once — which is the shape of a defect this project has already
// found once here: bytes that were valid, correctly addressed and completely
// ineffective, with every test asserting their shape passing.
//
// It matters more for import than it did for fork, because import is the one
// verb whose frame deliberately carries **no Dir**. The daemon supplies the
// directory from its own discovery, so "the client sent nothing and the daemon
// filled it in" is a two-package property and this is the only place both
// packages are in the room.

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// An import the client asks for, end to end, against the daemon that answers.
//
// What runs here is exactly what openSession runs in front of the terminal:
// importTarget to resolve, connect to dial, requestImport to write, awaitSpawn
// to wait. Every one is the shipped function.
//
// **The furthest assertion is the argv**, and for import it carries the most.
// A live process whose command line holds the imported session's UUID can only
// exist if the client's frame survived dispatch, if the daemon read the
// directory out of its **own** discovery rather than one this client never
// sent, and if core's identity switch emitted the fork triple around it.
func TestAnImportTheClientAsksForReachesARealAgent(t *testing.T) {
	// **Before the fleet starts, and that ordering is the point.** The daemon is
	// a separate process that EnsureRunning forks, so it reads its own
	// ProjectsDir from its own environment - the first draft of this test set
	// WAKE_PROJECTS afterwards and the daemon went looking in the real
	// ~/.claude/projects, refusing a transcript the client could see perfectly
	// well. That is a two-package disagreement about which tree is being talked
	// about, and it is exactly the class this file exists to catch.
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	ran := t.TempDir()
	sourceID := uuid.NewString()
	transcript(t, projects, ran, sourceID, ran)

	f := startForkedFleet(t)

	// Resolved through the client's own lookup rather than from sourceID
	// directly, because "which session did they mean" is half of what the verb
	// does. A prefix, which is the form somebody actually types.
	src, err := importTarget(sourceID[:8])
	if err != nil {
		t.Fatalf("importTarget against the real tree: %v", err)
	}
	if src.ID != sourceID || src.Dir != ran {
		t.Fatalf("importTarget resolved %+v, want session %s in %s", src, sourceID, ran)
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

	importID := uuid.NewString()
	if err := requestImport(conn, importID, src.ID); err != nil {
		t.Fatalf("requestImport: %v", err)
	}

	// The client's own wait, on the id it minted. A daemon that refused the
	// frame answers here addressed to that same id, so a divergence arrives as
	// a failure rather than as a hang - which is the shape a wrong id produces
	// and neither wait in this package has a deadline.
	sess, fleet, _, err := awaitSpawn(stream, importID)
	if err != nil {
		t.Fatalf("the daemon refused the frame this client builds: %v. That is the whole reason this "+
			"test exists - internal/daemon's import tests hand-build their frames, so nothing else in "+
			"the tree compares the two", err)
	}

	if sess.ID != importID {
		t.Errorf("the import came back as %s, want the id this client minted, %s: Wake originates "+
			"identity and the reaper needs that UUID in the argv", sess.ID, importID)
	}
	if sess.ParentID != sourceID {
		t.Errorf("the imported session reports ParentID %q, want the transcript it came from, %q. "+
			"Nothing on claude's wire says a session has an ancestor, so a daemon that does not record "+
			"this has thrown away the only account of where the conversation came from", sess.ParentID, sourceID)
	}
	// **The property this file exists for.** The client sent no directory; the
	// daemon supplied the one discovery proved. A daemon that fell back to its
	// own working directory would start a session that finds no transcript,
	// inherits nothing, and looks exactly like a working agent.
	if sess.Dir != ran {
		t.Errorf("the imported session runs in %q, want %q - the directory discovery proved. "+
			"The frame carries no Dir on purpose, so this field is the daemon's own answer and the "+
			"only place the two packages' idea of it can be compared", sess.Dir, ran)
	}
	if sess.State == rpc.StateEnded {
		t.Errorf("the imported session ended immediately: %s", sess.Error)
	}
	if fleet == nil || !holdsBoth(fleet, f.sessionID, importID) {
		t.Errorf("the confirmation does not carry both the existing session and the import: %+v", fleet)
	}

	awaitAgent(t, importID)
}

// The refusal an operator is most likely to meet, through a real daemon: a
// transcript that exists with no directory anybody can prove.
//
// Driven through the socket rather than against importSource directly, because
// what is being checked is that the refusal **arrives** - addressed to the
// client's own id, on the connection it is waiting on. A daemon that refused to
// the source's id instead would leave `wake import` on a blank terminal
// forever, which is indistinguishable from a daemon that is thinking.
func TestAnImportRefusedByTheDaemonIsAddressedToTheIdTheClientIsWaitingOn(t *testing.T) {
	// Before the fleet, for the reason above: the daemon reads its own
	// environment.
	projects := t.TempDir()
	t.Setenv("WAKE_PROJECTS", projects)
	started := t.TempDir()
	worktree := filepath.Join(started, "wt")
	if err := os.MkdirAll(worktree, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sourceID := uuid.NewString()
	transcript(t, projects, started, sourceID, worktree)

	f := startForkedFleet(t)

	// The premise, through the real discovery: this session has no provable
	// directory. Without it a refusal below could be about anything.
	found, err := daemon.Discoverable()
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	var src daemon.FoundSession
	for _, s := range found {
		if s.ID == sourceID {
			src = s
		}
	}
	if src.ID == "" || src.Dir != "" {
		t.Fatalf("the fixture is not the one this test needs: %+v has a directory, so the refusal "+
			"below would be about something else", src)
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

	importID := uuid.NewString()
	if err := requestImport(conn, importID, sourceID); err != nil {
		t.Fatalf("requestImport: %v", err)
	}
	_, _, _, err = awaitSpawn(stream, importID)
	if err == nil {
		t.Fatal("the daemon started a session for a transcript with no provable directory. " +
			"claude locates a transcript by the directory it was started in, so that is an empty " +
			"conversation under a live-looking header")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("the refusal is %q and does not say the problem is the directory", err)
	}
}
