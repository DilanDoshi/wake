// `wake fork <who> [name]`: branching a conversation from the shell.
//
// The TUI's fork action is the useful one; this is the half that is scriptable
// and the half a test can drive without a terminal. Both write the same frame
// to the same verb, and the state gate that decides whether a fork is allowed
// lives behind both, in the daemon - it is the only process that can see
// whether a parent is mid-turn.

package main

import (
	"fmt"
	"io"
	"net"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// forkSession branches an existing conversation and opens the fork.
//
// The parent is resolved before anything is dialled, for the reason reattach
// does it in that order: connect() forks a daemon when nothing is listening, so
// dialling first would answer "there is no such session" by starting a fresh
// daemon that has never heard of it.
//
// It never resumes the parent. `wake fork` and `wake attach` are the two ways
// to reach an existing conversation and they are different verbs on purpose:
// one continues it and one branches it, and the parent's transcript file is
// untouched by the second (2026-08-10 findings §5).
//
// **The id it waits on is the fork's, never the parent's**, and that is the
// sharpest thing in this file. awaitSpawn matches the daemon's refusal on
// `f.SessionID == sessionID` and has no deadline by design; the daemon
// addresses every one of its nine fork refusals to the fork's own id. Waiting
// on the parent's would leave `wake fork` parked on a blank terminal forever
// with nothing printed, which looks exactly like a daemon that is thinking.
func forkSession(socket, who, name string, out io.Writer) error {
	parent, err := forkParent(socket, who)
	if err != nil {
		return err
	}
	sessionID := uuid.NewString()
	return openSession(socket, sessionID, func(conn net.Conn) error {
		return requestFork(conn, sessionID, parent.ID, name)
	}, announceFork, out)
}

// announceFork says, on the notice row of the conversation this verb is about
// to open, that a fork is a snapshot.
//
// **Both fork surfaces say it or neither does.** ⌃F says it from
// ui.startArrived; this is the other half, and the sentence itself is
// ui.SnapshotNotice so there is one spelling rather than two - the claim has
// already been written into artefacts more times than it has been implemented,
// and a second copy of the words is how that happens again.
//
// Said on the *confirmation* and not on the request, for the reason ⌃F waits: a
// refused fork must not be told it is a snapshot of anything. openSession calls
// this only after awaitSpawn has returned a session.
//
// `wake attach` deliberately passes nothing here. The sentence says the fork has
// the parent's conversation *as of now*, which is true at the moment a fork is
// taken and false every time afterwards - so attaching to a fork days later
// would be told something untrue in the one direction that costs work.
func announceFork(sess rpc.SessionStatus, fleet *rpc.Status) {
	notice.Report("%s", ui.SnapshotNotice(displayName(sess), forkParentName(sess, fleet)))
}

// forkParentName is the parent's name while that name still points at one
// session, and "" otherwise - sessionNames' rule, which is the listing's, which
// is the DM header's.
//
// The fallbacks differ and that is deliberate: `wake status` prints the parent's
// short id, because that column prints ids and `wake attach` resolves them. A
// sentence about what @sydney *does next* has no such column, so it drops the
// name and keeps the claim.
func forkParentName(sess rpc.SessionStatus, fleet *rpc.Status) string {
	if sess.ParentID == "" || fleet == nil {
		return ""
	}
	return sessionNames(*fleet)[sess.ParentID]
}

// requestFork asks the daemon to start one session as a fork of another.
//
// It carries no Dir, and that is a decision rather than an omission: a fork
// runs where its parent ran, because claude derives the project slug - and so
// the transcript path - from the working directory, and resuming or forking
// from a different one is completely unrecorded. The daemon reads the parent's
// own directory and ignores anything a client says about it.
//
// Bounded, because this write is the one with nowhere to report from: it
// happens before tea.NewProgram, so a daemon that has stopped reading leaves
// `wake` parked on a blank terminal with nothing printed. See rpc.WriteFrameTo.
func requestFork(conn net.Conn, sessionID, parentID, requested string) error {
	if err := rpc.WriteFrameTo(conn, rpc.Frame{
		Kind:      rpc.FrameFork,
		SessionID: sessionID,
		ParentID:  parentID,
		Text:      requested,
	}); err != nil {
		return fmt.Errorf("fork a session: %w", err)
	}
	return nil
}

// forkName is the name `wake fork <who> [name]` was given for the fork, or
// nothing - which means the daemon draws one from the pool, exactly as bare
// `wake` does.
func forkName(args []string) string {
	if len(args) < 3 {
		return ""
	}
	return args[2]
}
