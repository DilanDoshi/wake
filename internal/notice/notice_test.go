package notice

import (
	"strings"
	"sync"
	"testing"
)

// fresh empties the sink for one test. Every test in this package needs it:
// the sink is process-global on purpose, so tests are the only thing that has
// to be able to start from empty.
func fresh(t *testing.T) {
	t.Helper()
	Reset()
	t.Cleanup(Reset)
}

func TestNothingReportedIsNotANotice(t *testing.T) {
	fresh(t)

	if n, ok := Latest(); ok {
		t.Errorf("Latest() = %v, true on an empty sink; want no notice", n)
	}
}

func TestReportSurfacesTheMessage(t *testing.T) {
	fresh(t)

	Report("rendering markdown failed: %v", "bad style")

	n, ok := Latest()
	if !ok {
		t.Fatal("Latest() reported nothing after a Report")
	}
	if n.Text != "rendering markdown failed: bad style" {
		t.Errorf("Text = %q", n.Text)
	}
	if n.Count != 1 {
		t.Errorf("Count = %d, want 1", n.Count)
	}
}

// The whole reason this package exists rather than a log call: a draw loop
// failing on every frame across 30 sessions must cost one line, not thirty
// thousand.
func TestARepeatedFailureIsOneNoticeWithACount(t *testing.T) {
	fresh(t)

	for range 500 {
		Report("the same failure")
	}

	n, ok := Latest()
	if !ok {
		t.Fatal("Latest() reported nothing")
	}
	if n.Count != 500 {
		t.Errorf("Count = %d, want 500", n.Count)
	}
	if got := n.String(); !strings.Contains(got, "×500") {
		t.Errorf("String() = %q, want it to admit 500 repeats", got)
	}
}

func TestASingleFailureDoesNotShowACount(t *testing.T) {
	fresh(t)

	Report("once")

	n, _ := Latest()
	if got := n.String(); got != "once" {
		t.Errorf("String() = %q, want %q with no count", got, "once")
	}
}

func TestLatestIsTheNewestDistinctFailure(t *testing.T) {
	fresh(t)

	Report("first")
	Report("second")

	n, _ := Latest()
	if n.Text != "second" {
		t.Errorf("Text = %q, want the newest report", n.Text)
	}
	if Count("first") != 1 {
		t.Errorf("the earlier failure lost its count: %d", Count("first"))
	}
}

// Reading must not consume. View runs on every frame, and a notice that
// vanished after one frame would be invisible in practice.
func TestLatestDoesNotClearTheNotice(t *testing.T) {
	fresh(t)

	Report("still here")
	for range 3 {
		if _, ok := Latest(); !ok {
			t.Fatal("Latest() stopped reporting a failure that was never cleared")
		}
	}
}

// The text can carry a session id or a path, so the set of distinct messages
// is not small and fixed. Unbounded, this would be a leak for the life of the
// process.
func TestTheSinkIsBounded(t *testing.T) {
	fresh(t)

	for i := range maxDistinct * 4 {
		Report("failure number %d", i)
	}

	mu.Lock()
	size := len(counts)
	mu.Unlock()
	if size > maxDistinct {
		t.Errorf("the sink holds %d distinct messages, want at most %d", size, maxDistinct)
	}
	if _, ok := Latest(); !ok {
		t.Error("the newest failure was lost when the sink was bounded")
	}
}

// A timed notice clears itself, but only while it is still the one showing:
// its own expiry must not wipe a fresher report that took the slot - including
// a repeat of the same text, which restarts the clock.
func TestClearIfOnlyClearsTheReportItWasArmedFor(t *testing.T) {
	fresh(t)

	Report("renaming @alex…")
	first, _ := Latest()
	ClearIf(first.Seq)
	if n, ok := Latest(); ok {
		t.Errorf("ClearIf left %q showing; want the slot empty", n.Text)
	}

	Report("usage limit approaching")
	stale, _ := Latest()
	Report("the daemon hung up")
	ClearIf(stale.Seq)
	if n, ok := Latest(); !ok || n.Text != "the daemon hung up" {
		t.Errorf("ClearIf wiped a fresher notice: Latest = %q, %v", n.Text, ok)
	}

	Report("forking @alex…")
	once, _ := Latest()
	Report("forking @alex…")
	ClearIf(once.Seq)
	if n, ok := Latest(); !ok || n.Count != 2 {
		t.Errorf("the first report's expiry cleared its repeat: Latest = %v, %v", n, ok)
	}
}

// Every report takes a new sequence number, a repeat included, so a repeat is
// a fresh report to whoever times the row.
func TestEveryReportTakesANewSeq(t *testing.T) {
	fresh(t)

	Report("copied 3 chars")
	a, _ := Latest()
	Report("copied 3 chars")
	b, _ := Latest()
	if a.Seq == 0 || b.Seq <= a.Seq {
		t.Errorf("seqs %d then %d; want non-zero and rising", a.Seq, b.Seq)
	}
}

// A message reported again after a clear starts its count over rather than
// carrying a stale one.
func TestClearIfForgetsTheCount(t *testing.T) {
	fresh(t)

	Report("close to the limit")
	Report("close to the limit")
	n, _ := Latest()
	ClearIf(n.Seq)

	Report("close to the limit")
	if got := Count("close to the limit"); got != 1 {
		t.Errorf("Count after a clear = %d, want 1", got)
	}
}

func TestReportIsSafeForConcurrentUse(t *testing.T) {
	fresh(t)

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				Report("goroutine %d", i)
			}
		}()
	}
	wg.Wait()

	for i := range 8 {
		if got := Count("goroutine " + string(rune('0'+i))); got != 100 {
			t.Errorf("goroutine %d reported %d times, want 100", i, got)
		}
	}
}
