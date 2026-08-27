//go:build soak

// What the soak measures, and what it refuses to accept: the assertions, the
// two goroutine questions, the growth checks, and the report that prints
// every number whether or not anything failed.
//
// Split from soak_test.go, which owns the workload. The division is that this
// file never spawns anything - it only reads the counters the workload filled
// in - which keeps the thresholds and their justifications in one place.

package core

import (
	"fmt"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// --- the assertions --------------------------------------------------------

func assertIntegrity(t *testing.T, res *soakResult) {
	t.Helper()
	got := res.snapshot()

	if got.sessions == 0 || got.events == 0 {
		t.Fatalf("%d sessions and %d events - the soak proved nothing at all", got.sessions, got.events)
	}
	if got.unattributed > 0 {
		t.Errorf("%d of %d events left core with no session attribution: unroutable at any fleet size, and a permission ask among them is an agent blocked on stdin that no view can name",
			got.unattributed, got.events)
	}
	if got.misattributed > 0 {
		t.Errorf("%d permission requests carried the wrong session id", got.misattributed)
	}
	for _, f := range got.failures {
		t.Errorf("session failed: %s", f)
	}
}

func assertNoGrowth(t *testing.T, res *soakResult) {
	t.Helper()
	got := res.snapshot()

	first, last := 0, quarters-1
	if got.winEvents[first] < minEventsPerQuarter || got.winEvents[last] < minEventsPerQuarter {
		t.Logf("growth check skipped: %d and %d events in the first and last quarters, want %d in each - run it for longer",
			got.winEvents[first], got.winEvents[last], minEventsPerQuarter)
		return
	}

	firstNs := float64(got.winDrain[first]) / float64(got.winEvents[first])
	lastNs := float64(got.winDrain[last]) / float64(got.winEvents[last])
	if lastNs > firstNs*maxTimeGrowth {
		t.Errorf("time per event grew %.1fx across the run (%.0fns to %.0fns): something is doing work proportional to what it has already seen",
			lastNs/firstNs, firstNs, lastNs)
	}

	assertNoSustainedGrowth(t, got)

	firstAlloc, ok1 := allocsPerEvent(got, first)
	lastAlloc, ok2 := allocsPerEvent(got, last)
	if ok1 && ok2 && lastAlloc > firstAlloc*maxAllocGrowth {
		t.Errorf("allocations per event grew %.1fx across the run (%.1f to %.1f)", lastAlloc/firstAlloc, firstAlloc, lastAlloc)
	}
}

// assertNoSustainedGrowth is the one that would have caught a C2-shaped
// quadratic: the cost of an event measured against how many events the same
// session has already delivered.
//
// The churn lane's version of this comparison is across sessions and cannot
// see it - proven by mutation, see sustainedWorker. This one compares the last
// quarter of a long session's events against its first, so work proportional
// to history shows up as the ratio it is.
func assertNoSustainedGrowth(t *testing.T, got soakStats) {
	t.Helper()
	first, last := 0, quarters-1
	if got.sustEvents[first] < minEventsPerQuarter || got.sustEvents[last] < minEventsPerQuarter {
		t.Logf("sustained growth check skipped: %d and %d events in the first and last quarters of the long sessions, want %d in each - run it for longer",
			got.sustEvents[first], got.sustEvents[last], minEventsPerQuarter)
		return
	}
	firstNs := float64(got.sustNanos[first]) / float64(got.sustEvents[first])
	lastNs := float64(got.sustNanos[last]) / float64(got.sustEvents[last])
	if lastNs > firstNs*maxSustainedGrowth {
		t.Errorf("inside one session, an event cost %.1fx more at the end than at the start (%.0fns to %.0fns): something is doing work proportional to the history it has already seen, so the product gets slower the longer it runs",
			lastNs/firstNs, firstNs, lastNs)
	}
}

// allocsPerEvent differences the malloc counter across one window. The first
// window has no predecessor to difference against, so it starts from the
// second - which is also the better baseline, since the first quarter carries
// every one-off allocation the run makes on its way up to speed.
func allocsPerEvent(got soakStats, q int) (float64, bool) {
	if q == 0 || got.winEvents[q] == 0 || got.winMallocs[q] < got.winMallocs[q-1] {
		return 0, false
	}
	return float64(got.winMallocs[q]-got.winMallocs[q-1]) / float64(got.winEvents[q]), true
}

// assertGoroutinesReturned asks the two questions separately, because they
// fail for different reasons and only one of them is diagnosable after the
// fact.
//
// The peak is the live question: a leak of one goroutine per session pushes
// the count past anything the concurrency can explain long before the run
// ends, and it says so while the stacks would still have been readable.
//
// The return to baseline is the settled question, and it is polled rather
// than sampled: os/exec's copy goroutine and its context watchdog both retire
// a moment after Wait returns, so the last few sessions are still unwinding
// when the workers stop.
func assertGoroutinesReturned(t *testing.T, res *soakResult, baseline int) {
	t.Helper()
	got := res.snapshot()

	if bound := goroutineBound(baseline); got.peak > bound {
		t.Errorf("goroutines peaked at %d against a bound of %d (baseline %d + %d live sessions): a leak that scales with sessions completed rather than sessions live",
			got.peak, bound, baseline, *soakSessions)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= baseline {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			buf = buf[:runtime.Stack(buf, true)]
			t.Errorf("%d goroutines still running after %d sessions ended, want <= %d - something a session started never returned\n%s",
				n, got.sessions, baseline, buf)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertHeapReturned(t *testing.T, before, after uint64, events int) {
	t.Helper()
	if after <= before+heapSlackBytes {
		return
	}
	grew := after - before
	perEvent := float64(grew) / float64(max(events, 1))
	t.Errorf("live heap grew %s across %d events (%.1f bytes per event) - after two GCs, so it is retained rather than in flight",
		humanBytes(grew), events, perEvent)
}

// --- measurement -----------------------------------------------------------

// settledGoroutines returns a baseline that a previous test's unwinding cannot
// inflate.
//
// runtime.NumGoroutine() sampled once at the top of a test counts whatever is
// still retiring from the test before it - os/exec's copy goroutine and its
// context watchdog both unwind a moment after Wait returns - and a baseline
// that is too high hides a leak of exactly that size. That is the known
// weakness in waitForGoroutines, which samples once and compares against it.
//
// The error only ever runs one way, so this takes the *minimum* over a settle
// window rather than the first sample or the last: a straggler retiring during
// the window drags the number down toward the truth, and nothing drags it up.
// A baseline that is too *low* costs a false failure, which is the direction a
// leak detector should fail in.
func settledGoroutines(window, interval time.Duration) int {
	lowest := runtime.NumGoroutine()
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		time.Sleep(interval)
		lowest = min(lowest, runtime.NumGoroutine())
	}
	return lowest
}

// liveHeap is what is still reachable, not what has been allocated. Twice,
// because the first GC can leave a finalizer-reachable object alive into the
// second.
func liveHeap() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func mallocCount() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Mallocs
}

// --- fixtures --------------------------------------------------------------

// soakFixtures is the recorded corpus, as absolute paths: the helper runs with
// its own working directory and cannot resolve a relative one.
func soakFixtures(t *testing.T) []string {
	t.Helper()
	var abs []string
	for _, f := range fixtureFiles(t) {
		p, err := filepath.Abs(f)
		if err != nil {
			t.Fatalf("resolve fixture %s: %v", f, err)
		}
		abs = append(abs, p)
	}
	return abs
}

// eventsPerPass is how many events one replay of a fixture must deliver.
// Computed through DecodeLine exactly as the pump computes it - including that
// an undecodable line still emits one event - so a mismatch at run time is
// events lost or duplicated in transit, never a disagreement about what the
// fixture contains.
func eventsPerPass(t *testing.T, fixtures []string) map[string]int {
	t.Helper()
	want := make(map[string]int, len(fixtures))
	for _, path := range fixtures {
		n := 0
		for _, line := range fixtureLines(t, path) {
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				n++ // emit's error path: one KindUnknown
				continue
			}
			n += len(evs)
		}
		if n == 0 {
			t.Fatalf("%s produced no events, so a session replaying it proves nothing", path)
		}
		want[path] = n
	}
	return want
}

// scaled multiplies a per-pass count by however many passes a lane replays.
func scaled(perPass map[string]int, cycles int) map[string]int {
	out := make(map[string]int, len(perPass))
	for k, v := range perPass {
		out[k] = v * cycles
	}
	return out
}

// --- reporting -------------------------------------------------------------

// report prints every number the run produced, pass or fail. The measurements
// are the deliverable: a soak that only says "ok" is the one this replaced.
func report(t *testing.T, res *soakResult, fixtures, baseline int, heapBefore, heapAfter uint64) {
	t.Helper()
	got := res.snapshot()
	elapsed := time.Since(res.start)

	t.Logf("soak: %s, %d concurrent, %d fixtures x%d cycles", *soakDuration, *soakSessions, fixtures, soakCycles)
	t.Logf("  sessions   %d (%.0f/s)", got.sessions, float64(got.sessions)/elapsed.Seconds())
	t.Logf("  events     %d (%.0f/s), %d unattributed, %d misattributed, %d unknown kinds",
		got.events, float64(got.events)/elapsed.Seconds(), got.unattributed, got.misattributed, got.unknown)
	t.Logf("  goroutines baseline %d, peak %d, now %d (bound %d)",
		baseline, got.peak, runtime.NumGoroutine(), goroutineBound(baseline))
	t.Logf("  live heap  %s -> %s", humanBytes(heapBefore), humanBytes(heapAfter))
	for q := range quarters {
		line := fmt.Sprintf("  quarter %d  %d events", q+1, got.winEvents[q])
		if got.winEvents[q] > 0 {
			// One session's own clock, so it carries the inflation of
			// running *soakSessions of them at once. Comparable between
			// quarters, which is all the growth check asks of it; not a
			// throughput figure - that is the events/s line above.
			line += fmt.Sprintf(", %.0f ns/event in-session", float64(got.winDrain[q])/float64(got.winEvents[q]))
		}
		if got.sessionsIn(q) > 0 {
			line += fmt.Sprintf(", %.1fms spawn", float64(got.winSpawn[q])/float64(got.sessionsIn(q))/1e6)
		}
		if a, ok := allocsPerEvent(got, q); ok {
			line += fmt.Sprintf(", %.1f allocs/event", a)
		}
		t.Log(line)
	}

	t.Logf("  sustained  %d sessions of ~%d events (%d passes each)", got.sustSessions,
		got.sustEvents[0]+got.sustEvents[1]+got.sustEvents[2]+got.sustEvents[3], soakSustainedCycles)
	for q := range quarters {
		if got.sustEvents[q] == 0 {
			continue
		}
		t.Logf("    events %d-%d%%  %.0f ns/event", q*100/quarters, (q+1)*100/quarters,
			float64(got.sustNanos[q])/float64(got.sustEvents[q]))
	}
}

func goroutineBound(baseline int) int {
	return baseline + *soakSessions*goroutinesPerSession + goroutineSlack
}

func humanBytes(n uint64) string {
	const unit = 1 << 10
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for n/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGT"[exp])
}
