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
