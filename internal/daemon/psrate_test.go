//go:build unix

package daemon

// What the liveness watchdog costs a machine that is doing nothing.
//
// `Wake must be cheap to leave open` has one clause that names this exactly:
// *no process spawned on a timer that could be spawned on a suspicion*, and
// one that names the scale: *at this fleet size a per-agent cost on a ticker
// multiplies by 30*.
//
// This file used to measure the violation and pin it. probeQuietAgents spawned
// one `ps` per quiet agent per tick, gated on `quiet >= silenceLimit` - which
// is true of **every** agent in a fleet nobody is typing at, forever, because
// nothing memoised a probe and nothing took a living agent back out of the set.
// Measured at 250 spawns over 8 ticks at 30 agents: **86,400 a day**, and only
// an idle fleet paid at all.
//
// The claim is now the negation of that, and it takes two assertions because
// either one alone is satisfied by a wrong fix:
//
//   - **Flat in fleet size.** One `ps` answers the whole fleet, so 30 agents
//     cost what 1 agent costs. A fix that only made the per-agent probe cheaper
//     would still multiply.
//   - **Fewer than one per tick.** The schedule decays from each agent's last
//     event, so a fleet that stays quiet is asked less and less often. A fix
//     that only batched the probe would be flat *and* still spawn a process
//     every tick forever.
//
// And a floor under both, because the cheapest possible result is a watchdog
// that stopped running: at least one probe has to happen. What that probe is
// *for* is held by liveness_test.go, which is where the behaviour lives.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// probeSilence is how long a session must be quiet before the watchdog asks
// the OS about it, compressed for this test. Every other number here is
// derived from it through livenessInterval and probeCeilingFor, which is the
// same arithmetic production does - so nothing below is a threshold somebody
// typed.
const probeSilence = 2 * time.Second

// probeWindow is how long the count runs for once the fleet has gone quiet.
// Whole ticks, so the tick budget it is compared against is exact.
const probeWindow = 4 * time.Second

// TestTheLivenessWatchdogIsFlatInFleetSizeAndDecaysWhileNothingHappens is the
// measurement, and the guard over it.
//
// # What the pairing isolates
//
// Fleet size. Both arms run for the same window at the same tick interval with
// the same fake agents; the only thing that varies is how many sessions are
// alive. So the difference between them is what an agent costs the watchdog,
// and `the cost is not per agent` is read off the two totals rather than
// asserted about one number.
//
// # What it does not isolate
//
// Not the cost of a `ps`. The shim is a shell script that appends a line and
// execs the real thing, so what is counted is *how many* processes are
// spawned, never how long one takes. That is the right unit: the finding is a
// spawn rate, and a spawn rate is what the non-negotiable is written about.
//
// Not the daemon's whole idle cost either - the report rebuild, the broadcast
// and the roster write are separate paths with their own numbers.
func TestTheLivenessWatchdogIsFlatInFleetSizeAndDecaysWhileNothingHappens(t *testing.T) {
	total := map[int]int{}

	for _, agents := range []int{1, benchFleetSize} {
		t.Run(fmt.Sprintf("agents=%d", agents), func(t *testing.T) {
			shortSilence(t, probeSilence)
			// After shortSilence, never before it. livenessInterval reads
			// silenceLimit, and at the production limit it clamps to 30s -
			// so a tick budget computed in the parent is zero, and `probes >=
			// ticks` becomes a test that can only fail. It did.
			ticks := int(probeWindow / livenessInterval())
			log := countingPsOnPath(t)
			fakeClaudeOnPath(t, "")

			d := startDaemon(t)
			c := attach(t, d.socket)
			// Unnamed, for the soak lane's reason: a requested name that is
			// already held is refused, and every one of these is alive at once.
			for range agents {
				c.spawn(uuid.NewString(), "")
			}
			if n := len(live(c.status())); n != agents {
				t.Fatalf("%d of %d sessions are alive, so this would count probes for a fleet that is not there", n, agents)
			}

			// Quiet, and then long enough past silenceLimit that every agent is
			// due before the window opens. The fake agent says nothing unless it
			// is written to, so nothing here has to make it stop talking.
			time.Sleep(probeSilence + livenessInterval())

			before := psCount(t, log)
			time.Sleep(probeWindow)
			probes := psCount(t, log) - before
			total[agents] = probes

			t.Logf("%d agents, %d ticks of %v: %d ps spawns; at the production silence limit of %v "+
				"a fleet this size left alone costs about %d a day, against 86,400 before",
				agents, ticks, livenessInterval(), probes, defaultSilenceLimit, perDay())

			// The floor. A watchdog that stopped asking is the cheapest possible
			// result and would satisfy every other assertion here.
			if probes < 1 {
				t.Errorf("%d ps spawns over %d ticks: the watchdog never asked the OS about anything, "+
					"which is not a cheaper probe but an absent one - see "+
					"TestAnAgentThatDiesAfterItsLastTurnIsNotReportedIdleForever for what that loses", probes, ticks)
			}
			// The decay. One process per tick forever is what the schedule
			// replaced, and a fleet-wide probe on an undecayed ticker would be
			// flat in fleet size while still doing exactly that.
			if probes >= ticks {
				t.Errorf("%d ps spawns over %d ticks, want fewer than one per tick: the probe is still on a "+
					"flat timer, so an idle fleet pays the same forever no matter how long nothing happens",
					probes, ticks)
			}
		})
	}

	// The claim is that fleet size does not enter into it, and one arm cannot
	// carry that: a watchdog spawning one process per agent would satisfy the
	// per-arm bounds above at a fleet of one.
	if len(total) == 2 {
		if extra := total[benchFleetSize] - total[1]; extra > 2 {
			t.Errorf("a %d-agent fleet cost %d ps spawns against %d for a single agent (%d more), want no more "+
				"than 2 apart: the watchdog is still paying per agent, which is the cost the non-negotiable "+
				"names as multiplying by 30", benchFleetSize, total[benchFleetSize], total[1], extra)
		}
	}
}

// perDay is what the watchdog's schedule comes to over a day at the production
// silence limit, for a fleet that is left alone and never speaks again.
//
// Derived from the constants rather than restated, so a change to
// defaultSilenceLimit or the backoff moves the number in the log instead of
// leaving a stale one in a comment. Through the …For helpers rather than by
// assigning silenceLimit and putting it back: a daemon is running while this is
// called and its watchdog reads that var on every tick, so the obvious version
// is a data race - and it was, caught by `make test`'s first lane.
//
// It counts the ramp exactly and then fills the rest of the day at the ceiling.
// No fleet size appears in it, which is the whole finding: one ps answers the
// fleet, so the figure is the same at 1 agent and at 30.
func perDay() int {
	ceiling := probeCeilingFor(defaultSilenceLimit)
	probes, elapsed := 0, time.Duration(0)
	for every := livenessIntervalFor(defaultSilenceLimit); elapsed < 24*time.Hour; every = min(every*2, ceiling) {
		probes++
		elapsed += every
	}
	return probes
}

// countingPsOnPath puts a ps(1) at the front of PATH that records every
// invocation and then execs the real one, and returns the log's path.
//
// # Why this is not brokenPsOnPath
//
// That one shadows ps with *this test binary*, which TestMain dispatches on
// argv[0]. It cannot pass an invocation through - answering correctly would
// mean reimplementing ps - and it costs a whole Go runtime start per call,
// which for a *rate* measurement is the measurement.
//
// The pass-through matters for a second reason. The probe reads the argv it
// gets back and looks for each session id in it; a shim that answered anything
// else would have every agent declared gone on the first pass,
// noteUnreachable would fire, and the fleet would stop being probed at all -
// which is the opposite of the behaviour being measured, and would show up
// here as a cheaper result.
func countingPsOnPath(t *testing.T) string {
	t.Helper()

	real, err := realPs()
	if err != nil {
		t.Skipf("no ps to pass through to: %v", err)
	}

	dir := t.TempDir()
	log := filepath.Join(dir, "ps.log")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexec %q \"$@\"\n", log, real)
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(script), 0o700); err != nil {
		t.Fatalf("write counting ps: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// realPs is the ps this shim defers to. Looked up by absolute path rather than
// through PATH, because PATH is what the shim is on.
func realPs() (string, error) {
	for _, p := range []string{"/bin/ps", "/usr/bin/ps"} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("neither /bin/ps nor /usr/bin/ps exists")
}

// psCount is how many times ps has been run, counted from the shim's own log.
//
// **Every invocation, whatever its shape.** The version before this counted
// only `-p <pid>` lines, to separate the watchdog's per-agent lookup from
// idsInUse's `-Aww`. That distinction was load-bearing while the watchdog had a
// shape of its own to be told apart; now that it asks one fleet-wide question,
// a filter on the old shape would count zero and read as the cheapest possible
// result - the exact vacuity this file's floor exists to catch, arriving
// through the harness instead of the code.
//
// Nothing else runs ps inside the window either way: reap's lookups happen at
// startup and shutdown, and idsInUse runs on an operator's keystroke, which no
// arm here makes. Counting everything is therefore both simpler and stricter,
// because it cannot miss a probe that changed shape again.
func psCount(t *testing.T, log string) int {
	t.Helper()

	data, err := os.ReadFile(log)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read ps log: %v", err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}
