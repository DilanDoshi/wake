// The liveness watchdog: how often it looks, when a quiet agent is worth
// asking the OS about, and the pass that asks.
//
// Split out of agent.go, which owns what a session's state *is*, and out of
// spawn.go, which owns starting one; this owns the clock the watchdog reads and
// what it does when it fires. The move was forced by the 800-line hard max and is
// the same one argv.go made out of session.go - a file whose whole subject this
// is, rather than a policy sitting inside the state machine it happens to time.
//
// Two rates, and the difference between them is the whole of the liveness fix.
// The **tick** is how often the watchdog wakes at all, and it is flat: it costs
// nothing, because a pass that finds nobody due spawns no process. The **probe
// schedule** is how often any one agent is asked about, and it decays.
//
// # Why the schedule decays rather than repeating
//
// The probe exists for one case: an agent that dies *right after* finishing its
// last turn. It owes nothing, so no silence timer fires; nobody writes to it, so
// no failed write proves anything; and core cannot help, because its pump is
// parked in Scan with Err() nil and Events() open. Asking the OS is the only
// route to the answer.
//
// The gate that used to decide *when* to ask read elapsed quiet and nothing
// else - which is permanently true of exactly that population, since an idle
// agent produces no events and its quiet time only grows. So every idle agent
// was asked about on every tick forever, and only an idle fleet paid: 86,400
// process spawns a day at 30 agents. A suspicion that admits 100% of the
// population in the state the rule is written about is a timer.
//
// What makes the decay a suspicion rather than a slower timer is the reset.
// Entering the state being detected requires the process to *exit*, so the
// hazard is concentrated near the last event and thins out behind it - and any
// event is proof of life, which starts a fresh silence that is worth asking
// about promptly again. A flat tick asserts the hazard is uniform for the
// following nine hours, which is the assumption that made this expensive and
// wrong at the same time.

package daemon

import (
	"context"
	"fmt"
	"time"
)

// inputCheckMin and inputCheckMax bound how often the liveness watchdog
// looks, derived from silenceLimit so a test that compresses the limit gets a
// proportionate tick without a second knob.
const (
	livenessCheckMin = 50 * time.Millisecond
	livenessCheckMax = 30 * time.Second
)

func livenessInterval() time.Duration { return livenessIntervalFor(silenceLimit) }

// livenessIntervalFor is that arithmetic as a pure function of a limit.
//
// Split out so the production rate can be asked for by a test that has
// compressed silenceLimit, without writing to a var a running daemon's
// watchdog is reading - which is a data race and was one: probeQuietAgents
// reads silenceLimit through suspects on every tick.
func livenessIntervalFor(limit time.Duration) time.Duration {
	return min(max(limit/4, livenessCheckMin), livenessCheckMax)
}

// probeBackoffFactor and probeCheckMax bound how far the per-agent probe
// schedule may back off from livenessInterval, which is where it starts.
//
// The factor is against silenceLimit rather than against the tick, so the
// ceiling means something in the units the policy is written in: silenceLimit
// is this project's own statement of how long a session may say nothing before
// the silence means something, and six of those is the point past which asking
// again sooner buys nothing. A probe that is late costs a *report* being stale -
// a dead-but-idle agent reads `idle` rather than `silent` for a while longer -
// and never a wrong answer, because every other route to noticing is unchanged
// and sharper: an operator who writes to the agent finds out on the failed
// write, immediately, whatever this schedule says.
//
// probeCheckMax is the absolute ceiling for the same reason livenessCheckMax
// is: a silence limit somebody raises must not push a background check out to
// hours.
const (
	probeBackoffFactor = 6
	probeCheckMax      = 30 * time.Minute
)

func probeCeiling() time.Duration { return probeCeilingFor(silenceLimit) }

// probeCeilingFor is that arithmetic as a pure function of a limit, split out
// for livenessIntervalFor's reason: a test that has compressed silenceLimit
// needs the production number without writing to the var a running daemon's
// watchdog is reading.
//
// The floor is livenessIntervalFor(limit) rather than a constant of its own,
// because that is where the schedule starts and a ceiling below its own floor
// would silently mean "never back off".
func probeCeilingFor(limit time.Duration) time.Duration {
	return min(max(limit*probeBackoffFactor, livenessIntervalFor(limit)), probeCheckMax)
}

// quietAndDue reports whether this agent has been quiet long enough to be
// worth asking the OS about, and whether its own schedule says to ask now.
//
// Two answers rather than one, and the split is what keeps the pass flat in
// fleet size. One `ps` answers the whole fleet for the price of one agent, so a
// pass triggered by *any* agent coming due refreshes *every* quiet agent for
// free. Returning only "due" would make the caller ask about that one agent,
// and a fleet whose agents went quiet at different times would then run one
// pass per agent - the per-agent multiplication back again as pass frequency
// rather than as processes per pass.
//
// A session that has ended, is parked (which is an ended one), or is already
// being reclaimed is neither. A write-reported unreachable session stays in:
// EPIPE is not OS proof, and the watchdog is the path that can supply it.
func (a *agent) quietAndDue(now time.Time, limit time.Duration) (quiet, due bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ended || a.reclaiming {
		return false, false
	}
	if now.Sub(a.lastEvent) < limit {
		return false, false
	}
	return true, !now.Before(a.probeAfter)
}

// noteProbed records that the OS has just been asked about this agent, and
// pushes the next ask further out.
//
// It is called for an agent the pass found alive **and** for one the pass could
// not establish anything about, deliberately. A machine whose ps cannot answer
// would otherwise retry every tick forever, which is the flat timer this
// replaced arriving through the error path - and a ps that could not answer
// this pass is no more likely to answer the next one.
//
// The floor and ceiling are passed in rather than read here, so the pass reads
// silenceLimit once instead of once per agent, and so the schedule can be
// tested as arithmetic rather than through a running daemon.
func (a *agent) noteProbed(now time.Time, floor, ceiling time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.probeEvery == 0 {
		a.probeEvery = floor
	} else {
		a.probeEvery = min(a.probeEvery*2, ceiling)
	}
	a.probeAfter = now.Add(a.probeEvery)
}

// watchLiveness announces a session whose state changed without being asked.
//
// Without it, "this agent has said nothing for ten minutes while owing a turn
// end" is only visible to whoever thinks to run wake status. The whole point
// of separating silent from idle is that somebody finds out.
func (s *server) watchLiveness(ctx context.Context) {
	tick := time.NewTicker(livenessInterval())
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.quit:
			return
		case <-s.done:
			return
		case <-tick.C:
			// One stat in the common case, re-establishing the lock only on the
			// rare tick after its file was swept - the flat tick already wakes
			// here and spawns nothing, so this adds no process on a timer.
			s.lock.verify()
			s.probeQuietAgents(ctx)
			if s.statesChanged() {
				s.broadcast(s.statusPush())
			}
		}
	}
}

// probeQuietAgents asks the OS about agents that have gone quiet.
//
// It closes the hole in the policy that the event stream cannot: an agent that
// died after finishing its last turn owes nothing, so the silence timer never
// fires, and nobody writes to it, so a failed write never proves anything. It
// reads idle forever. The daemon already holds both halves of the answer - the
// process group core recorded at spawn, and a way to ask the OS about it - and
// this is where they meet.
//
// # It spawns nothing unless something is due, and one process when it is
//
// The version before this ran one `ps` per quiet agent per tick, gated on
// `quiet >= silenceLimit` and nothing else. That gate is permanently true of
// exactly the population this exists for - an idle agent produces no events, so
// its quiet time only grows, and nothing memoised a probe or took a living
// agent back out of the set. So an idle fleet was re-asked every tick forever:
// 86,400 process spawns a day at 30 agents, measured, and *only* an idle fleet
// paid at all. CLAUDE.md's non-negotiable names both halves of that - *no
// process spawned on a timer that could be spawned on a suspicion*, and *a
// per-agent cost on a ticker multiplies by 30* - and a suspicion that admits
// 100% of the population in the state the rule is written about is a timer.
//
// Two changes, one for each half. The ask is **one ps for the whole fleet**
// (goneNow), which is liveid_unix.go's ruling applied to this question and is
// why agentGone was replaced rather than kept beside it. And each agent carries
// a schedule that **decays from its own last event and is reset by one**, so a
// fleet that stays quiet is asked less and less often while an agent that
// speaks again is asked promptly - which is the shape of the hazard this
// exists for, since dying "right after finishing its last turn" is not a risk
// that stays uniform for the following nine hours.
//
// An answer of "cannot tell" changes nothing, and it is the whole pass rather
// than one agent: a listing that cannot be trusted must not read as a fleet
// that is gone. The ask happens with no lock held because it shells out to ps.
//
// It gives up the moment the daemon is asked to end. That check is now worth
// less than it was - there is one lookup bounded by probeTimeout where there
// used to be thirty, so this can no longer hold the goroutine past
// shutdownWait - but whether a quiet agent is also gone stops being worth
// knowing at that point either way.
func (s *server) probeQuietAgents(ctx context.Context) {
	now := time.Now()
	quiet, due := s.suspects(now)
	if !due || s.stopping(ctx) {
		return
	}

	fleet := make([]watched, 0, len(quiet))
	for _, a := range quiet {
		fleet = append(fleet, watched{id: a.id, pid: a.sess.Pgid()})
	}
	gone, err := goneNow(ctx, fleet)
	if err != nil {
		// Said once, not per agent: it is one question and one failure. The
		// schedule still advances below, because a ps that could not answer
		// this pass is no more likely to answer the next one and retrying every
		// tick is the timer this replaced.
		logf("wake: could not ask the OS about %d quiet sessions: %v", len(quiet), err)
	}
	for _, a := range quiet {
		if gone[a.id] {
			a.lostProcess(fmt.Errorf("the agent's process is gone, but the session has not ended: something it spawned is holding its output open"))
			continue
		}
		a.noteProbed(now, livenessInterval(), probeCeiling())
	}
}

// suspects are the agents worth asking the OS about - quiet for longer than the
// silence limit, still running, and not already known to be unreachable - and
// whether any of their schedules says to ask now.
//
// Every quiet agent is returned, not only the due ones, because one ps answers
// all of them for the price of one. See agent.quietAndDue for why splitting the
// two answers is what keeps the pass flat in fleet size.
func (s *server) suspects(now time.Time) (quiet []*agent, due bool) {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()

	limit := silenceLimit
	for _, a := range agents {
		isQuiet, isDue := a.quietAndDue(now, limit)
		if !isQuiet {
			continue
		}
		quiet = append(quiet, a)
		due = due || isDue
	}
	return quiet, due
}

// statesChanged reports whether any agent has moved since the last
// announcement, and records the new states as announced.
func (s *server) statesChanged() bool {
	s.mu.Lock()
	agents := make([]*agent, 0, len(s.agents))
	for _, a := range s.agents {
		agents = append(agents, a)
	}
	s.mu.Unlock()

	changed := false
	for _, a := range agents {
		// Every agent, not just up to the first change: this call is what
		// records the announced state, so short-circuiting would leave the
		// rest to re-announce one tick later.
		if a.changed() {
			changed = true
		}
	}
	return changed
}
