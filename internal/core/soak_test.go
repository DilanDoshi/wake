//go:build soak

// The soak: many fake sessions, replaying the recorded corpus, for as long as
// you ask. Spec §16 calls it "the one that matters at this scale - nothing
// else catches a goroutine leak that bites on hour three."
//
// It is behind the `soak` build tag so it never runs in the ordinary suite,
// and it is written to *find* things rather than to pass. Everything it
// measures is logged whether or not it fails, because a soak whose numbers
// you cannot see is the same green no-op it replaced: `make soak` reported
// "ok ... [no tests to run]" and exited 0 for the whole of Phase 1, and both
// Criticals the cross-cutting review found are exactly what it would have
// surfaced.
//
// What it exercises that nothing else does:
//
//   - whole session lifecycles, thousands of them. A leak that costs one
//     goroutine or one buffer per session is invisible in a test that spawns
//     three and shows up here in the first minute.
//   - the recorded corpus through the real pump, at concurrency, rather than
//     through DecodeLine on its own.
//   - event integrity under load: every event a fixture implies arrives, and
//     every one of them can name the session it came from.
//
// It never invokes a live claude - execCommand is swapped for the same
// re-exec helper the rest of the package uses (session_test.go), driving the
// `replay` script.

package core

import (
	"context"
	"flag"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	// Short by default so the soak is runnable in a review. The advertised
	// hour is `make soak SOAK_DURATION=1h`, and nothing about the test
	// changes between the two - only how many sessions it gets through.
	soakDuration = flag.Duration("soak.duration", 20*time.Second, "how long TestSoak keeps sessions churning")
	soakSessions = flag.Int("soak.sessions", 20, "how many sessions TestSoak runs concurrently")
)

// soakCycles is how many times one session replays its fixture before
// exiting. It trades spawn cost against event volume: at 1 the soak measures
// fork/exec, at 100 it stops churning sessions and only churns events.
const soakCycles = 3

// soakSustainedCycles is how many passes one *sustained* session replays -
// thousands of events without a restart, which is the only condition under
// which growth that accumulates inside a session becomes measurable. Sessions
// are identified by their id prefix, because argv is the only per-session
// channel the exec seam has.
const soakSustainedCycles = 400

const sustainedPrefix = "soak-sustained-"

// goroutinesPerSession bounds what one live session may account for while it
// runs: pump, closeOnCancel, awaitExit and logSink.run from this package,
// os/exec's stderr copier and its context watchdog, the soak's own worker, and
// slack. It is deliberately generous - the thing it has to catch is a leak that
// scales with *sessions completed* rather than with sessions live, and that
// one runs away by orders of magnitude.
const goroutinesPerSession = 12

// goroutineSlack absorbs the runtime's own workers plus whatever the test
// framework is doing, on top of the settled baseline.
const goroutineSlack = 32

// heapSlackBytes is how much the live heap may grow across the whole run.
// Live heap after a GC should be flat: events are consumed and dropped, and
// sessions are released as they end. A per-event leak of even a hundred bytes
// over a few hundred thousand events clears this by a wide margin.
const heapSlackBytes = 8 << 20

// growth bounds on the per-event measurements, comparing the last quarter of
// the run against the first. Both are ratios rather than absolutes so the
// speed of the machine drops out. Time is the looser of the two because it
// carries scheduler noise; allocations do not.
const (
	maxTimeGrowth = 8.0
	// Tighter than maxTimeGrowth because the sustained phase runs alone:
	// with no competing load the measurement is the pump's own cost, so a
	// real change in it is not hiding under scheduler noise.
	maxSustainedGrowth = 3.0
	maxAllocGrowth     = 4.0
	// Below this many events a quarter is noise, not a measurement.
	minEventsPerQuarter = 500
)

const quarters = 4

// sustainedShare is how much of the configured duration the quiet phase gets.
// The churn phase keeps the rest. A third is enough for several long sessions
// at any duration worth running, and leaves the leak detector the majority -
// leaks need session count, growth needs session length.
func sustainedShare(total time.Duration) time.Duration { return total / 3 }

// TestSoak churns sessions for the configured duration and then asks the
// three questions a soak exists to ask: did anything leak, did anything grow,
// and did every event arrive intact.
func TestSoak(t *testing.T) {
	fixtures := soakFixtures(t)
	perPass := eventsPerPass(t, fixtures)

	// Taken before anything is spawned, and taken in a way a previous test's
	// unwinding cannot inflate - see settledGoroutines.
	baseline := settledGoroutines(2*time.Second, 50*time.Millisecond)
	heapBefore := liveHeap()

	orig := execCommand
	execCommand = soakExec
	t.Cleanup(func() { execCommand = orig })

	res := &soakResult{
		start:         time.Now(),
		deadline:      time.Now().Add(*soakDuration - sustainedShare(*soakDuration)),
		churnWant:     scaled(perPass, soakCycles),
		sustainedWant: scaled(perPass, soakSustainedCycles),
	}
	peak := watchGoroutines(res)

	// Phase one: churn, at concurrency. This is the leak detector, and it
	// wants the machine busy.
	var wg sync.WaitGroup
	for w := range *soakSessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			soakWorker(w, fixtures, res)
		}()
	}
	wg.Wait()

	// Phase two: one long session at a time, deliberately alone. This is the
	// growth detector, and it wants the machine quiet - which is why it is a
	// phase rather than another lane.
	//
	// Measured: run alongside the churn lane, an event's inter-arrival time
	// is ~155us, of which ~14us is the pump and the rest is twenty other
	// processes competing for the CPU. A quadratic that made the pump ten
	// times slower moved that number by 1.23x and passed. Alone, the noise
	// floor is the pump itself and the same quadratic shows up as the ratio
	// it actually is.
	sustainedWorker(fixtures, res, time.Now().Add(sustainedShare(*soakDuration)))
	close(peak)

	heapAfter := liveHeap()
	report(t, res, len(fixtures), baseline, heapBefore, heapAfter)

	// Order matters: the leak checks come last, because they are the ones
	// worth reading a stack dump for and the ones above explain what the
	// session was doing when it leaked.
	assertIntegrity(t, res)
	assertNoGrowth(t, res)
	assertGoroutinesReturned(t, res, baseline)
	assertHeapReturned(t, heapBefore, heapAfter, res.events)
}

// --- the workload ----------------------------------------------------------

// soakWorker runs one session after another until the deadline. Sessions are
// serial within a worker and concurrent across them, so the fleet stays at
// *soakSessions live while thousands of them come and go.
func soakWorker(worker int, fixtures []string, res *soakResult) {
	for round := 0; time.Now().Before(res.deadline); round++ {
		fixture := fixtures[(worker+round)%len(fixtures)]
		runSoakSession(fmt.Sprintf("soak-%02d-%d", worker, round), fixture, res.churnWant[fixture], res)
	}
}

// sustainedWorker runs one long session at a time and measures what an event
// costs against how many events that session has already seen.
//
// This lane exists because the churn lane cannot answer that question, and an
// earlier draft of this file shipped without it and said it could. Verified by
// mutation: a transcript accumulated per session and rebuilt on every event -
// the exact shape of the DM.Append quadratic the cross-cutting review measured
// at 11.3ms per append - passes the churn lane untouched. Churn sessions see
// ~85 events each and then start over from nothing, so quadratic work over 85
// items never grows large enough to measure, and the per-quarter comparison is
// across sessions rather than within one.
//
// A sustained session sees soakSustainedCycles passes of its fixture instead,
// which is thousands of events without a restart. Cost is bucketed by *event
// index* within the session, so the comparison is "the ten-thousandth event
// against the first thousand" - which is the question "does the product get
// slower the longer you use it", asked in the only place it can be answered.
func sustainedWorker(fixtures []string, res *soakResult, deadline time.Time) {
	for round := 0; time.Now().Before(deadline); round++ {
		fixture := fixtures[round%len(fixtures)]
		runSustainedSession(fmt.Sprintf("soak-sustained-%d", round), fixture, res)
	}
}

func runSustainedSession(id, fixture string, res *soakResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	s := NewSession(Config{SessionID: id, Name: fixture, PermissionMode: "manual"})
	if err := s.Start(ctx); err != nil {
		res.record(sessionOutcome{failure: fmt.Sprintf("%s: Start: %v", id, err)})
		return
	}

	var out sessionOutcome
	var events [quarters]int
	var nanos [quarters]int64
	// Read once, not per event: the bucket widths have to be fixed in
	// advance, because bucketing against the running count would put every
	// event in the last bucket.
	want := max(res.sustainedWant[fixture], 1)
	prev := time.Now()
	for ev := range s.Events() {
		now := time.Now()
		q := min(out.events*quarters/want, quarters-1)
		events[q]++
		nanos[q] += now.Sub(prev).Nanoseconds()
		prev = now

		out.events++
		switch {
		case ev.SessionID == "":
			out.unattributed++
		case ev.Kind == KindPermissionRequest && ev.SessionID != id:
			out.misattributed++
		}
		if ev.Kind == KindUnknown {
			out.unknown++
		}
	}
	if err := s.Err(); err != nil {
		out.failure = fmt.Sprintf("%s (%s): Err: %v", id, filepath.Base(fixture), err)
	}
	if out.events != want {
		out.failure = fmt.Sprintf("%s (%s): %d events, want %d", id, filepath.Base(fixture), out.events, want)
	}
	res.recordSustained(out, events, nanos)
}

// runSoakSession is one whole lifecycle: spawn, drain to the close, reap.
//
// Everything it learns is folded into fixed counters before it returns.
// Keeping a record per session would grow the heap with the run and turn the
// leak check into a measurement of the test's own bookkeeping.
func runSoakSession(id, fixture string, want int, res *soakResult) {
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	s := NewSession(Config{SessionID: id, Name: fixture, PermissionMode: "manual"})
	if err := s.Start(ctx); err != nil {
		res.record(sessionOutcome{failure: fmt.Sprintf("%s: Start: %v", id, err)})
		return
	}

	// Two clocks, because one of them would lie. A session's wall time is
	// almost entirely fork/exec of the test binary - measured at ~26ms
	// against ~85 events - so dividing it by events reports a per-event cost
	// that is 95% spawn and dilutes any real per-event growth by the same
	// factor. draining starts at the first event, which is the earliest
	// moment the process is actually streaming.
	var out sessionOutcome
	var draining time.Time
	for ev := range s.Events() {
		if out.events == 0 {
			draining = time.Now()
			out.spawnNanos = draining.Sub(started).Nanoseconds()
		}
		out.events++
		switch {
		case ev.SessionID == "":
			// The finding this assertion exists for: a control_request
			// carries no session_id on the wire, so an event that leaves
			// core without one is unroutable - and the permission ask is
			// the highest-priority attention trigger there is.
			out.unattributed++
		case ev.Kind == KindPermissionRequest && ev.SessionID != id:
			out.misattributed++
		}
		if ev.Kind == KindUnknown {
			out.unknown++
		}
	}
	if !draining.IsZero() {
		out.drainNanos = time.Since(draining).Nanoseconds()
	}
	if err := s.Err(); err != nil {
		out.failure = fmt.Sprintf("%s (%s): Err: %v", id, filepath.Base(fixture), err)
	}
	// Nothing here cancels or stops early, so every event a fixture implies
	// must arrive. A short count is dropped events; a long one is duplicated.
	if out.events != want {
		out.failure = fmt.Sprintf("%s (%s): %d events, want %d", id, filepath.Base(fixture), out.events, want)
	}
	res.record(out)
}

// soakExec is the package's fake-process helper, told which fixture to
// replay. The path travels in this command's own environment rather than the
// process's, because twenty sessions replay twenty different files and
// os.Setenv is process-wide; it arrives via Config.Name, which is the one
// per-session string that reaches argv.
func soakExec(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := fakeExec(ctx, name, args...)
	cmd.Env = append(cmd.Env,
		"WAKE_HELPER_SCRIPT=replay",
		"WAKE_HELPER_FIXTURE="+argValue(args, "--name"),
		fmt.Sprintf("WAKE_HELPER_CYCLES=%d", cyclesFor(argValue(args, "--session-id"))),
	)
	return cmd
}

func cyclesFor(sessionID string) int {
	if strings.HasPrefix(sessionID, sustainedPrefix) {
		return soakSustainedCycles
	}
	return soakCycles
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// --- what the workers report ----------------------------------------------

type sessionOutcome struct {
	events        int
	unattributed  int
	misattributed int
	unknown       int
	spawnNanos    int64
	drainNanos    int64
	failure       string
}

// soakStats is every counter the run accumulates. Fixed size on purpose: it
// must not grow with the number of sessions, or the bookkeeping becomes the
// leak the run is looking for. Split from its mutex so a snapshot is a plain
// copyable value.
type soakStats struct {
	sessions      int
	events        int
	unattributed  int
	misattributed int
	unknown       int
	peak          int
	failures      []string

	// Per-quarter windows, for the growth checks. Bucketed by when a session
	// finished rather than by count, so a run that slows down still fills
	// four comparable windows.
	// Sustained lane: cost bucketed by event index within one long session,
	// which is where growth-with-history shows up. Kept apart from the churn
	// windows above, which are bucketed by wall-clock time across sessions.
	sustSessions int
	sustEvents   [quarters]int
	sustNanos    [quarters]int64

	winSessions [quarters]int
	winEvents   [quarters]int
	winDrain    [quarters]int64
	winSpawn    [quarters]int64
	winMallocs  [quarters]uint64
}

type soakResult struct {
	start    time.Time
	deadline time.Time

	// Immutable after construction, so no lock guards them.
	churnWant     map[string]int
	sustainedWant map[string]int

	mu sync.Mutex
	soakStats
}

// maxFailures bounds what the run keeps. The first handful say what is wrong;
// ten thousand of them say the same thing and cost a gigabyte.
const maxFailures = 10

func (r *soakResult) record(out sessionOutcome) {
	q := r.quarter(time.Now())
	mallocs := mallocCount()

	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions++
	r.events += out.events
	r.unattributed += out.unattributed
	r.misattributed += out.misattributed
	r.unknown += out.unknown
	r.winSessions[q]++
	r.winEvents[q] += out.events
	r.winDrain[q] += out.drainNanos
	r.winSpawn[q] += out.spawnNanos
	// Sampled at the window's end and differenced later: a per-session
	// reading would count the test's own bookkeeping between sessions.
	r.winMallocs[q] = mallocs
	if out.failure != "" && len(r.failures) < maxFailures {
		r.failures = append(r.failures, out.failure)
	}
}

// sessionsIn is how many sessions finished in one window. Spawn time is
// recorded once per session and events many times, so the two figures in the
// report need different denominators and dividing spawn by events would
// report a per-event spawn cost that means nothing.
func (s soakStats) sessionsIn(q int) int { return s.winSessions[q] }

// recordSustained folds a long session's integrity counters into the same
// totals as the churn lane - every event is worth checking wherever it came
// from - while keeping its timing in the by-index buckets, which are the only
// place growth-with-history is legible.
func (r *soakResult) recordSustained(out sessionOutcome, events [quarters]int, nanos [quarters]int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sustSessions++
	r.events += out.events
	r.unattributed += out.unattributed
	r.misattributed += out.misattributed
	r.unknown += out.unknown
	if out.failure != "" && len(r.failures) < maxFailures {
		r.failures = append(r.failures, out.failure)
	}
	for q := range quarters {
		r.sustEvents[q] += events[q]
		r.sustNanos[q] += nanos[q]
	}
}

func (r *soakResult) quarter(now time.Time) int {
	total := r.deadline.Sub(r.start)
	if total <= 0 {
		return 0
	}
	q := int(now.Sub(r.start) * quarters / total)
	return min(max(q, 0), quarters-1)
}

func (r *soakResult) snapshot() soakStats {
	r.mu.Lock()
	defer r.mu.Unlock()
	got := r.soakStats
	got.failures = append([]string(nil), r.failures...)
	return got
}

// watchGoroutines samples the live count while the run is in flight and keeps
// the peak. The end-of-run count says whether everything unwound; this says
// whether the fleet ever grew past what its concurrency can explain, which is
// what a per-session leak looks like *while* it is happening.
func watchGoroutines(res *soakResult) chan struct{} {
	stop := make(chan struct{})
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				n := runtime.NumGoroutine()
				res.mu.Lock()
				res.peak = max(res.peak, n)
				res.mu.Unlock()
			}
		}
	}()
	return stop
}
