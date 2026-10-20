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
	if n, limit := s.liveCount(), s.agentCap(); n >= limit {
		c.enqueue(errorFrame(f.SessionID, capRefusal(n, limit)))
		return false
	}
	return true
}

// defaultLiveCap is how many sessions a fleet runs at once until the operator
// sets fewer with `--max-agents`: the ceiling itself (owner's 2026-10-20 ruling,
// raising the spec's 15-30). What it bounds is money and machine, so its case is
// a loop nobody is watching rather than a human who typed fifty commands.
const defaultLiveCap = rpc.MaxAgentsCeiling

// liveCap is that number, as a var so a test can reach the boundary without
// starting thirty processes. Written only before a daemon runs - see
// shortSilence, which is the same seam for the same reason.
var liveCap = defaultLiveCap

// capRefusal is what a spawn past the cap is told, and it names the two things
// that make room rather than only saying no.
func capRefusal(n, limit int) string {
	why := fmt.Sprintf("%d sessions are already running, which is this fleet's cap. ⌃C parks one and /resume brings it back, or `wake stop` ends the fleet", n)
	if limit < rpc.MaxAgentsCeiling {
		why += fmt.Sprintf("; `wake --fleet <this fleet> --max-agents <n>` raises the cap, up to %d", rpc.MaxAgentsCeiling)
	}
	return why
}

// agentCap is this fleet's cap now.
func (s *server) agentCap() int { return int(s.maxAgents.Load()) }

// setMaxAgents is FrameSetMaxAgents: the operator's cap for this fleet from now
// on. It stops nothing already running - the next spawn is measured against it.
// Under admitMu, so a spawn being admitted reads one cap or the other.
func (s *server) setMaxAgents(c *client, f rpc.Frame) {
	if f.MaxAgents < 1 || f.MaxAgents > rpc.MaxAgentsCeiling {
		c.enqueue(errorFrame("", fmt.Sprintf("--max-agents takes 1 to %d, not %d", rpc.MaxAgentsCeiling, f.MaxAgents)))
		return
	}
	s.admitMu.Lock()
	s.maxAgents.Store(int64(f.MaxAgents))
	s.admitMu.Unlock()
	s.pushStatus()
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
		if n, limit := s.liveCount(), s.agentCap(); n >= limit {
			return capRefusal(n, limit)
		}
	}
	if !s.admit(a, replaces, wake) {
		return s.admitRefusal(a.id, replaces, wake)
	}
	return ""
}

// liveSessions is a snapshot of every session that holds a process right now.
//
// Parked and ended are not among them: a parked session has no process, which
// is what parking *is*, and an ended one has none by definition. So the cap
// counts what is costing money and CPU rather than what is in the roster, and
// the fleet note names teammates from the same set.
func (s *server) liveSessions() []rpc.SessionStatus {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()

	live := make([]rpc.SessionStatus, 0, len(agents))
	for _, a := range agents {
		switch st := a.snapshot(); st.State {
		case rpc.StateParked, rpc.StateEnded:
		default:
			live = append(live, st)
		}
	}
	return live
}

// liveCount is how many sessions hold a process right now.
func (s *server) liveCount() int { return len(s.liveSessions()) }
