// May this spawn happen at all: the boundary check a spawn frame passes and
// the live cap it is counted against. Split from spawn.go at the seam
// deferred.md named while that file sat at 790 of the 800-line hard max -
// this is one subject, and taking the row (launch, admit, register) is the
// other.

package daemon

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// maySpawn reports whether this spawn is allowed, telling the client why not.
//
// This is the daemon's boundary check, and one of the two things it validates
// is load-bearing far away from here: the reaper decides whether to SIGKILL a
// process group by looking for the session id in a live process's command
// line, and justifies that on the id being a UUID that exists nowhere else on
// the machine. Nothing checked it. A short or ordinary id - "s1", "build" -
// makes that match any group-leading process whose argv happens to contain
// it, which at reaping time is somebody's shell job.
//
// Established here for ids arriving on the wire. It is *also* checked in
// verifyAgent, because the reaper's ids come off a file on disk rather than
// through this door, and a persisted artifact outlives whichever build wrote
// it. Same predicate, two boundaries.
func (s *server) maySpawn(ctx context.Context, c *client, f rpc.Frame) bool {
	if s.stopping(ctx) {
		c.enqueue(errorFrame(f.SessionID, "the daemon is shutting down"))
		return false
	}
	if f.SessionID == "" {
		// Wake originates identity. Without an id there is no --session-id
		// on the command line, which costs both things the id exists for:
		// the transcript cannot be resumed and the process cannot be found
		// again by a reaper. Refused rather than silently spawned.
		c.enqueue(errorFrame("", "a session needs a Wake-assigned id"))
		return false
	}
	if !mintedByWake(f.SessionID) {
		c.enqueue(errorFrame(f.SessionID, "a session id must be a UUID, got "+strconv.Quote(f.SessionID)))
		return false
	}
	if s.holds(f.SessionID) {
		// Identity before display. register below is still the authoritative
		// check and still has to be - it is the only one atomic with taking the
		// id - but it happens after a name has been claimed and a process
		// started, so without this a client that re-sent its own spawn would be
		// told its *name* was taken. It is: by itself. The id is the true
		// refusal and it is the one worth reading.
		c.enqueue(errorFrame(f.SessionID, "session "+f.SessionID+" already exists"))
		return false
	}
	if f.Dir != "" && !filepath.IsAbs(f.Dir) {
		// A relative directory would resolve against the daemon's own
		// working directory, which is the confusion this field exists to
		// end rather than one to inherit.
		c.enqueue(errorFrame(f.SessionID, "a session directory must be absolute, got "+f.Dir))
		return false
	}
	if n := s.liveCount(); n >= liveCap {
		c.enqueue(errorFrame(f.SessionID, capRefusal(n)))
		return false
	}
	return true
}

// defaultLiveCap is how many sessions may run at once.
//
// The number is the product's own ceiling - spec §1 is "15-30 agents" - rather
// than a measurement, because what it bounds is money and machine rather than
// anything Wake can measure from here. A human at 30 agents has typed thirty
// commands and knows; the case this exists for is a loop nobody is watching.
const defaultLiveCap = 30

// liveCap is that number, as a var so a test can reach the boundary without
// starting thirty processes. Written only before a daemon runs - see
// shortSilence, which is the same seam for the same reason.
var liveCap = defaultLiveCap

// capRefusal is what a spawn past the cap is told, and it names the two things
// that make room rather than only saying no.
func capRefusal(n int) string {
	return fmt.Sprintf("%d sessions are already running, which is the cap. ⌃C parks one and /resume brings it back, or `wake stop` ends the fleet", n)
}

// admitLive is the cap and the row as one step. maySpawn counts at the
// boundary - early enough that a spawn past the cap costs no name and no
// worktree - but that count and the admit are separated by both, so two
// spawns racing can each count 29 and each be admitted: every check green and
// one more process than the cap. This is the same predicate restated at the
// last door, atomic with taking the row, which is configRefusal's own pattern.
//
// A wake is exempt from the count and not from the row -
// TestAWakeIsNotRefusedByTheCap's ruling: the cap bounds *new* sessions, and
// a conversation the operator already owns is not new. A wake's admit still
// happens under the mutex, so its row is in the fleet before any concurrent
// spawn counts.
//
// It answers why the launch may not proceed, or "" once the row is taken.
func (s *server) admitLive(a, replaces *agent, wake bool) string {
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	if s.refusesAdmission() {
		return "the daemon is shutting down"
	}
	if !wake {
		if n := s.liveCount(); n >= liveCap {
			return capRefusal(n)
		}
	}
	if !s.admit(a, replaces, wake) {
		return s.admitRefusal(a.id, replaces, wake)
	}
	return ""
}

// liveCount is how many sessions hold a process right now.
//
// Parked and ended are not among them: a parked session has no process, which
// is what parking *is*, and an ended one has none by definition. So the cap
// counts what is costing money and CPU rather than what is in the roster.
func (s *server) liveCount() int {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()

	n := 0
	for _, a := range agents {
		switch a.snapshot().State {
		case rpc.StateParked, rpc.StateEnded:
		default:
			n++
		}
	}
	return n
}
