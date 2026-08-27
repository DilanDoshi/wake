//go:build unix

// What the OS can and cannot prove about a process this daemon does not hold
// a handle to.
//
// Everything the reaper and the liveness probe decide rests on these answers,
// and both of them turn an answer into a SIGKILL - so the cases that matter
// most here are the ones where ps cannot answer at all. Unknown must never
// become "gone", and a check that cannot be exercised on the machine running
// it must say so rather than pass.

package daemon

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// Nothing that cannot be proved to be the recorded agent is ever signalled.
// Each case here is a way a roster entry can be wrong, and every one has to
// end in false - because the next thing the caller does with a true is SIGKILL
// a process group.
//
// It lives in the Unix file and uses a real group-leading process on purpose.
// The first version passed os.Getpid(), which under `go test` is *not* a group
// leader - so verifyAgent returned false at groupLeader and never reached the
// command-line check the case is named for. Deleting that check left it green.
func TestVerifyAgentRefusesEverythingItCannotProve(t *testing.T) {
	// A real process, leading its own group, whose command line carries no
	// session id at all: the shape of a recycled pid.
	stranger := startLingererInItsOwnGroup(t)

	for _, tc := range []struct {
		name      string
		pid       int
		sessionID string
	}{
		{"no pid", 0, idAlpha},
		{"init", 1, idAlpha},
		{"negative", -1, idAlpha},
		{"no session id", stranger.Process.Pid, ""},
		{"not a group leader", os.Getpid(), idAlpha},
		{"a group leader running something else", stranger.Process.Pid, idAlpha},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()
			if verifyAgent(ctx, tc.pid, tc.sessionID) {
				t.Errorf("verifyAgent(%d, %q) = true: this would SIGKILL a process group on no evidence", tc.pid, tc.sessionID)
			}
		})
	}

	// The last case must have reached the command-line check rather than
	// stopping at the group test, or it proves nothing about the thing it is
	// named for. Both halves are asserted: that the process leads its group,
	// and that ps actually produced a command line to compare against. On a
	// machine where ps cannot answer, the second is the one that fails - and
	// without it this test passes vacuously for a new reason.
	if !groupLeader(stranger.Process.Pid) {
		t.Fatal("the stranger does not lead its own group, so the case that matters never reached the command-line check")
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	p, err := inspect(ctx, stranger.Process.Pid)
	if err != nil {
		t.Fatalf("inspect the stranger: %v - ps cannot answer here, so the command-line check was never exercised", err)
	}
	if p.argv == "" {
		t.Fatal("ps returned an empty command line for the stranger, so the check that matters compared against nothing")
	}
}

// The other direction: every argv shape a live agent can carry must verify
// *true*, and the shapes are read off core rather than written down here.
//
// The negative half above is the one that keeps a SIGKILL honest. This half
// keeps the reaper *able to fire*, and it became load-bearing when wake shipped:
// a woken agent carries `--resume <id>` and no --session-id at all, and it
// verifies today only because verifyAgent ends in a bare strings.Contains of the
// id, which any marker contains. That is correct by accident. Tightening it to
// `--session-id <id>` is a reasonable-looking hardening - it is the marker rule
// core.SessionArgvMarkers itself establishes - and it would make every woken
// session unreapable after a daemon crash with nothing anywhere going red.
//
// Derived from SessionArgvMarkers, which is the producer: a third marker added
// there is covered here the day it is added rather than the day somebody
// remembers to. Each marker is a flag and its value, which is exactly the pair
// of argv words a lingerer needs.
func TestVerifyAgentAcceptsEveryArgvShapeALiveAgentCanCarry(t *testing.T) {
	markers := core.SessionArgvMarkers(idAlpha)
	if len(markers) == 0 {
		t.Fatal("core offers no argv markers for a session id, so this test walks nothing")
	}
	for _, marker := range markers {
		t.Run(marker, func(t *testing.T) {
			words := strings.Fields(marker)
			if len(words) < 2 {
				t.Fatalf("marker %q is not a flag and a value, so the lingerer below would carry something else", marker)
			}
			agent := startLingerer(t, words...)
			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()
			if !verifyAgent(ctx, agent.Process.Pid, idAlpha) {
				t.Errorf("verifyAgent said no about a process running as %v, which is one of the argv "+
					"shapes core builds for session %s. A reaper that cannot recognise a live agent "+
					"leaves it running with no daemon holding it, which is the whole failure reap.go exists for",
					words, idAlpha)
			}
		})
	}
}

// A ps that could not answer is not a ps that found nothing, and the exit
// status alone cannot tell them apart. There are three ways it happens and
// they are unrelated except in their consequence.
//
//   - It runs and refuses. busybox in a container, or any ps that is neither
//     procps nor BSD, rejects the flags this package asks for and exits
//     non-zero for *every* pid.
//   - This package kills it. inspect gives ps probeTimeout; exec.CommandContext
//     SIGKILLs it after that, and Cmd.Wait reports the signal rather than the
//     context - so it arrives as an ExitError with both streams empty, which is
//     byte-for-byte what a missing pid looks like. Only Exited() differs.
//   - It exits 0 and says nothing. No stock ps does that, which is the whole
//     reason it cannot be read as "there is no such process".
//
// Folded into "no such process", any of them declares a living fleet gone:
// every quiet agent marked unreachable, reported silent, and then killed by
// process group at shutdown instead of stopped gently. None needs a second
// daemon or a crash, and the middle one does not even need a wrong ps - a
// loaded machine or a laptop that suspended mid-probe is enough.
func TestAPsThatCannotAnswerIsUnknownRatherThanGone(t *testing.T) {
	for _, how := range []string{psRefuses, psHangs, psQuiet} {
		t.Run(how, func(t *testing.T) {
			shortProbeTimeout(t, 200*time.Millisecond)
			brokenPsOnPath(t, how)
			agent := startLingererInItsOwnGroup(t)

			ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
			defer cancel()
			_, err := inspect(ctx, agent.Process.Pid)
			if err == nil {
				t.Fatal("inspect succeeded through a ps that could not answer")
			}
			if errors.Is(err, errNoProcess) {
				t.Fatalf("inspect = %v: a ps that could not run the query was read as a pid that is not there", err)
			}

			// And the same of the fleet-wide lookup the watchdog actually uses.
			// It is a stricter statement than the one this replaced: agentGone
			// answered per pid, so "unknown" was one agent's answer, while a
			// listing that cannot be trusted has to fail the **whole pass** -
			// absence from a whole-machine listing is what "gone" means to
			// goneNow, so a partial or empty answer would declare every quiet
			// agent dead at once.
			got, gerr := goneNow(ctx, []watched{{id: idAlpha, pid: agent.Process.Pid}})
			if gerr == nil {
				t.Errorf("goneNow = %v, nil through a ps that could not answer: an unusable listing has to be a refusal, "+
					"because every agent missing from it reads as gone", got)
			}
			if got[idAlpha] {
				t.Errorf("goneNow said session %s was gone through a ps that could not answer: unknown never declares "+
					"anything dead, and this answer ends in a SIGKILL to its group at shutdown", idAlpha)
			}
		})
	}
}

// The same thing at the level it is felt. The agent is alive, quiet and
// perfectly healthy; only ps is broken.
//
// The harm is not a slow probe. Reading an unusable answer as "gone" sends it
// through lostProcess, which cancels the live process group immediately.
func TestABrokenPsDoesNotTurnAQuietAgentIntoAKill(t *testing.T) {
	for _, how := range []string{psRefuses, psHangs, psQuiet} {
		t.Run(how, func(t *testing.T) {
			shortSilence(t, 150*time.Millisecond)
			shortProbeTimeout(t, 100*time.Millisecond)
			fakeClaudeOnPath(t, "") // stays alive on stdin, says nothing more
			brokenPsOnPath(t, how)
			d := startDaemon(t)
			c := attach(t, d.socket)

			c.spawn(idAlpha, "sydney")
			c.awaitEvent(idAlpha, "ready")

			// Several probe ticks, every one of them asking a ps that cannot
			// answer.
			c.stayedIn(idAlpha, rpc.StateIdle, 6*silenceLimit,
				"the agent is alive and quiet and only ps is broken; unknown must never become OS proof")
		})
	}
}
