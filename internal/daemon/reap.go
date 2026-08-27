// The reaper: what a daemon does about the fleet its predecessor left running.
//
// Wake gives every agent its own process group, which is what makes detach
// mean anything - a ^C in the terminal no longer sweeps the fleet. The cost,
// written out in core/procgroup_unix.go and paid here, is that a daemon which
// is SIGKILLed or panics leaves 15-30 process trees alive with nobody holding
// a handle to any of them. They are not reachable through anything Go knows;
// the only way back is the group id the last daemon wrote down.
//
// # Why this is allowed to send SIGKILL to a pid it did not spawn
//
// It is not, unless it can prove the pid is still the agent that was recorded.
// A pid recorded hours ago may since have been recycled, and signalling a
// *group* on a stale id could take down a terminal job, a build, or the user's
// shell. So an entry is killed only when the live process still carries the
// session id Wake minted for it in its own argv - a UUID that exists nowhere
// else on the machine. An entry that cannot be verified is left alone and said
// out loud; nothing here kills on a guess.
//
// # What establishes that these agents have no daemon
//
// The lock, and only the lock. Serve calls this exactly once, before it
// accepts anything, and only when takeLock granted an exclusive claim on the
// state directory - which the kernel releases on process death including
// SIGKILL, so a daemon that is alive in any way at all still holds it and a
// daemon that is not cannot. lock.go carries the argument in full.
//
// It deliberately does not turn on the socket. Binding also fails while
// another daemon is live, and the listener is held bound through the whole of
// shutdown for that reason (stopAcceptingOnStop) - but a probe on a socket
// nobody is accepting on gets ECONNREFUSED once the listen backlog fills,
// which is the same errno as a socket file left by a crash. That ambiguity is
// tolerable when the consequence is unlinking a file and is not when the
// consequence is SIGKILL to 15-30 process groups mid-Edit.

package daemon

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
)

// probeTimeout bounds the process lookup. Daemon startup is on the critical
// path of `wake` doing anything at all, so a wedged lookup must not be able
// to hold it open.
//
// Expiring is a real event with a real answer, and inspect says why it has to
// be *unknown*: the ps this deadline kills comes back looking exactly like a
// pid that is not there.
// It is a var only so tests can compress it; nothing outside a test assigns it.
var probeTimeout = defaultProbeTimeout

const defaultProbeTimeout = 2 * time.Second

// reapOrphans kills what a previous daemon left behind, and keeps the records
// it could not finish.
//
// A record clears only once there is nothing left to do for it: its process is
// confirmed gone, the pid is something else now, or its group was signalled. A
// record whose process could not be asked about, or whose confirmed-live group
// would not die, is retained for the next daemon - clearing it regardless is how
// one transient ps failure turned 15-30 live trees permanently ownerless, with
// nothing on disk left to find them by (docs/notes/bugs.md BUG-28).
func (s *server) reapOrphans() {
	recs := loadRoster(s.roster.path)
	if len(recs) == 0 {
		return
	}
	logf("wake: %d sessions were recorded by a daemon that is no longer running", len(recs))

	var unfinished []record
	for _, rec := range recs {
		if s.reapRecord(rec) {
			unfinished = append(unfinished, rec)
		}
	}

	if err := s.roster.retain(unfinished); err != nil {
		logf("wake: could not rewrite the session roster: %v", err)
	}
}

// reapRecord signals one orphaned record's process group if it can still be
// shown to be that agent, and reports whether the record must be retained for a
// later daemon to retry. It is retained only when this could not finish the job:
// the process could not be asked about, or a confirmed-live group's kill failed.
// A record naming no group, an id Wake could not have minted, a confirmed-gone
// or recycled pid, and a group that was signalled all clear.
//
// One deadline per probe, the shape verifyAgent used to build for itself.
// FleetOnDisk shares a single deadline across its sweep because it is bare
// `wake`'s front door; a one-time startup reap of a dead predecessor's roster is
// not on that latency path, so it keeps the per-record budget.
func (s *server) reapRecord(rec record) (retain bool) {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()

	if rec.PID <= 0 {
		logf("wake: session %s recorded no process group, so nothing can be reaped for it", rec.ID)
		return false
	}
	if !mintedByWake(rec.ID) {
		logf("wake: %q in the roster is not an id Wake minted, so nothing recorded under it can be matched to a process and it will not be signalled", rec.ID)
		return false
	}

	switch probeAgent(ctx, rec.PID, rec.ID) {
	case reapUnknown:
		// The one shape that is not an answer: ps ran and could not say. The
		// process may well be a live agent, so the record is kept rather than
		// forgotten - a signal is still never sent on this.
		logf("wake: session %s (group %d) could not be checked, so it is kept for a later daemon to retry", rec.ID, rec.PID)
		return true
	case reapGone:
		logf("wake: session %s (group %d) could not be shown to be still running, so it is not signalled", rec.ID, rec.PID)
		return false
	default: // reapAlive
		// os.ErrProcessDone is the group having already gone between the probe
		// and the signal - the outcome that was asked for, not a failure, so it
		// clears like any other reaped record (killGroupError's own vocabulary).
		if err := core.KillGroup(rec.PID); err != nil && !errors.Is(err, os.ErrProcessDone) {
			logf("wake: could not reap session %s (group %d), so it is kept for a later daemon to retry: %v", rec.ID, rec.PID, err)
			return true
		}
		logf("wake: reaped session %s (group %d), left behind by a daemon that died", rec.ID, rec.PID)
		return false
	}
}

// reapProbe is what asking the OS about an orphaned pid can establish: that it
// is still the recorded agent, that it is not - gone, or the pid is something
// else now - or that the question could not be answered at all. The last is the
// zero value, so a path that fails to decide keeps the safe direction: the
// reaper retains, never signalling and never forgetting on "I could not ask".
type reapProbe int

const (
	reapUnknown reapProbe = iota // could not ask; retain and retry
	reapAlive                    // still the recorded agent; signal it
	reapGone                     // gone, or the pid is something else now; clear
)

// probeAgent is verifyAgent's three-valued core, separating a confirmed answer
// from "could not ask" - the distinction the reaper needs and a bool cannot
// carry. Every false-returning shape verifyAgent folds together is reapGone here
// - a bad id, a non-leader, a missing pid, a mismatched argv - and only a ps
// that ran and could not answer is reapUnknown. groupLeader is a bare syscall
// that cannot be "unable to answer", so its false is gone or recycled, never
// unknown.
func probeAgent(ctx context.Context, pid int, sessionID string) reapProbe {
	if pid <= 1 || !mintedByWake(sessionID) {
		return reapGone
	}
	if !groupLeader(pid) {
		return reapGone
	}
	p, err := inspect(ctx, pid)
	if err != nil {
		if errors.Is(err, errNoProcess) {
			return reapGone
		}
		logf("wake: cannot inspect process %d, so it will not be signalled: %v", pid, err)
		return reapUnknown
	}
	if strings.Contains(p.argv, sessionID) {
		return reapAlive
	}
	return reapGone
}

// verifyAgent reports whether pid is still the agent recorded under
// sessionID, which is the question that has to be answered yes before
// anything here sends a signal.
//
// Three independent facts have to hold, and none is sufficient alone.
//
// The id must be one Wake minted. maySpawn refuses anything else at the
// boundary, but this does not read ids from that boundary - it reads them from
// a file on disk, which outlives the process, the build that wrote it and any
// later build that relaxes the rule. A roster written before that check
// existed, or hand-edited, still says "s1"; and a substring match for "s1"
// against every group-leading process's argv is somebody's shell job, matched
// and then SIGKILLed. The invariant the match relies on has to be re-checked
// where the signal is, not merely where it was established.
//
// The process must still lead its own group, because that is what makes
// signalling -pid reach a group Wake created rather than one it merely joined.
//
// And its command line must still carry the session id, which is the part that
// survives pid reuse: a UUID passed as --session-id exists nowhere else on the
// machine, so a recycled pid running something else cannot match it.
//
// Anything unknown is false. A lookup that fails, a platform with no way to
// ask, an empty answer - all of them mean "do not signal this".
//
// The deadline comes from the caller rather than being built here, so successive
// probes can share one budget: FleetOnDisk holds a single ctx over its whole
// sweep (see fleetProbeBudget), while reapOrphans gives each record its own
// probeTimeout ctx, which is the shape this used to build for itself.
func verifyAgent(ctx context.Context, pid int, sessionID string) bool {
	return probeAgent(ctx, pid, sessionID) == reapAlive
}

// The question agentGone used to answer - *has this agent's process ended even
// though the session has not?* - now lives in livefleet.go as goneNow, and the
// move is the whole of the liveness fix rather than a tidy-up. It was one
// `ps -p <pid>` per quiet agent per tick, which is the reaper's shape: one
// lookup, one pid, no fleet. That is right for a caller reading pids off a dead
// daemon's roster and wrong for one holding thirty of its own children, where
// it came to 86,400 process spawns a day. goneNow asks once for all of them and
// keeps every property agentGone had; livefleet.go's header carries the
// comparison.
//
// mintedByWake reports whether an id is one Wake could have assigned.
//
// It is the same check maySpawn makes, deliberately duplicated rather than
// assumed: maySpawn establishes the invariant for ids arriving on the wire,
// and this enforces it for ids arriving off disk. They are different sources
// and only one of them is trusted.
func mintedByWake(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	_, err := uuid.Parse(sessionID)
	return err == nil
}

// alive reports whether this record's process is still the agent it names. It
// is what `wake status` uses to tell an orphaned fleet from a roster nobody
// cleaned up. Its one caller, FleetOnDisk, passes the ctx that bounds the whole
// sweep, so a hung probe cannot hold the front door open past the budget.
func (r record) alive(ctx context.Context) bool {
	return verifyAgent(ctx, r.PID, r.ID)
}
