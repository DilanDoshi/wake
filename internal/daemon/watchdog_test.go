package daemon

// The schedule that decides when a quiet agent is worth asking the OS about.
//
// psrate_test.go measures what the watchdog costs a machine and is the reason
// this exists; it cannot be the guard over it. That test drives a real daemon
// through a real socket with a wall clock, so it can say `fewer than one per
// tick` and cannot say *which* schedule produced that - a fixed 3-tick delay,
// a random one, or a backoff that resets at the wrong moment all measure the
// same over four seconds. The property is arithmetic, so it is asserted as
// arithmetic here: exact, instant, and with no daemon in it.
//
// Both halves are load-bearing and each kills a different plausible mutant:
//
//   - **It backs off.** Without the doubling the probe is a flat timer at
//     livenessInterval, which an idle fleet pays forever - the finding, with one
//     process per pass instead of thirty. Measured over a short window that
//     still looks cheap.
//   - **An event resets it.** Without that the backoff is a slower timer rather
//     than a suspicion: an agent that talks for an hour and then dies would be
//     asked about on whatever schedule its *first* silence had reached, so the
//     one case the probe exists for - a session that dies right after finishing
//     its last turn - is the case it would be slowest to notice.

import (
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// quietAgent is an agent with no process behind it, which is all the schedule
// needs: every function under test reads the clock it is handed and the two
// fields it owns.
//
// Every assertion below passes limit 0 to quietAndDue, so the agent counts as
// quiet whatever its lastEvent says. That keeps these tests about the schedule
// rather than about silenceLimit, which livenessIntervalFor already owns.
func quietAgent(t *testing.T) *agent {
	t.Helper()
	return newAgent(idAlpha, "alex", "", "", "", core.NewSession(core.Config{SessionID: idAlpha}), func() {})
}

// dueAt reports whether the agent's schedule says to ask at this instant.
func dueAt(a *agent, at time.Time) bool {
	_, due := a.quietAndDue(at, 0)
	return due
}

// TestAQuietAgentIsAskedAboutLessAndLessOftenUntilItSpeaks is the schedule,
// asserted through the two functions the watchdog actually calls.
//
// The gap is read back by *behaviour* rather than off the struct - due one
// instant before the boundary is a failure, due on it is the pass - so a
// rewrite that keeps the arithmetic and stores it differently is still held,
// and one that keeps the field and stops reading it is not.
func TestAQuietAgentIsAskedAboutLessAndLessOftenUntilItSpeaks(t *testing.T) {
	const (
		floor   = 30 * time.Second
		ceiling = 4 * time.Minute // floor << 3, so the cap lands on a gap the table names
	)

	a := quietAgent(t)
	now := time.Now()

	// A session that has just gone quiet has no schedule yet, and is asked
	// about at once. That is the detection latency the version before this had,
	// and it is the half the backoff must not cost: the hazard is highest right
	// after the last event.
	if !dueAt(a, now) {
		t.Fatal("a quiet agent with no probe behind it was not due: the first ask has to be prompt, " +
			"because an agent that dies right after its last turn is the case the probe exists for")
	}

	// Then twice as far out each time, up to the ceiling and never past it.
	want := []time.Duration{floor, 2 * floor, 4 * floor, ceiling, ceiling, ceiling}
	for i, gap := range want {
		a.noteProbed(now, floor, ceiling)
		if dueAt(a, now.Add(gap-time.Millisecond)) {
			t.Fatalf("probe %d: due %v after the last one, want not before %v: the schedule is not backing "+
				"off, so an idle fleet pays the same forever however long nothing happens", i+1, gap-time.Millisecond, gap)
		}
		if !dueAt(a, now.Add(gap)) {
			t.Fatalf("probe %d: not due %v after the last one, want due: the schedule has backed off past "+
				"the ceiling, so a dead session goes unnoticed for longer than %v", i+1, gap, ceiling)
		}
		now = now.Add(gap)
	}

	// And an event puts it back to the start. This is the whole difference
	// between a suspicion and a slower timer: the agent has spoken, so its
	// silence is a new one and the next one is worth asking about promptly.
	//
	// The deadline is pushed a full ceiling out first, and `now` left short of
	// it, so that being due below is *caused* by the reset. The loop above ends
	// with now exactly on the boundary, where a due check passes whether or not
	// anything was cleared - which is how the mutation that deletes the reset
	// entirely got past this assertion and had to be caught by the next one.
	a.noteProbed(now, floor, ceiling)
	if dueAt(a, now) {
		t.Fatal("the agent is due immediately after being probed, so nothing below is evidence about the reset")
	}
	a.observe(core.Event{Kind: core.KindTurnEnd, SessionID: idAlpha})
	if !dueAt(a, now) {
		t.Fatal("an agent that produced an event was not due to be probed: proof of life has to restart the " +
			"schedule, or a session that dies after an hour of work is asked about on the schedule its " +
			"first silence had reached")
	}
	a.noteProbed(now, floor, ceiling)
	if dueAt(a, now.Add(floor-time.Millisecond)) || !dueAt(a, now.Add(floor)) {
		t.Errorf("the first probe after an event was not %v out: the event cleared the deadline without "+
			"clearing the interval, so the backoff carries across a silence it does not belong to", floor)
	}
}

// The ceiling and the floor are derived from silenceLimit rather than picked,
// and this is the arithmetic that says so.
//
// It is a property over the two functions rather than a table of numbers,
// because a table restates the implementation and passes whatever it does. What
// it denies is a ceiling that stops meaning anything at either end: below its
// own floor it silently means "never back off", and unclamped it lets a silence
// limit somebody raises push a background check out to hours.
func TestTheProbeCeilingIsAlwaysAboveItsFloorAndNeverPastTheHardMax(t *testing.T) {
	for _, limit := range []time.Duration{
		time.Millisecond, 50 * time.Millisecond, time.Second, defaultSilenceLimit, time.Hour, 24 * time.Hour,
	} {
		floor, ceiling := livenessIntervalFor(limit), probeCeilingFor(limit)
		if ceiling < floor {
			t.Errorf("at a silence limit of %v the probe ceiling is %v and its floor is %v: a ceiling under "+
				"the floor means the schedule never backs off at all", limit, ceiling, floor)
		}
		if ceiling > probeCheckMax {
			t.Errorf("at a silence limit of %v the probe ceiling is %v, past the %v hard max: a session that "+
				"lost its process would go unreported for that long", limit, ceiling, probeCheckMax)
		}
	}
}
