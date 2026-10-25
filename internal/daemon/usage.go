package daemon

// The fleet's session-usage reading: the plan's five-hour window, which is the
// signed-in account's, so one per daemon whichever agent's API response
// reported it. Held here so a window that attaches later is told it at once
// (rpc.Status.SessionUsage), and never cleared, so the bar under the roster
// stays once it has drawn (decisions.md, 2026-10-24).

import (
	"math"
	"sync"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// usageBook is the newest reading. Its own lock: fanOut notes under no other,
// and the liveness tick lapses it.
type usageBook struct {
	mu      sync.Mutex
	reading rpc.SessionUsage
	held    bool
}

// note takes the reading an event carries, arriving at now, and reports whether
// what a status report says changed.
func (b *usageBook) note(ev core.Event, now time.Time) bool {
	u := ev.SessionUsage
	if u == nil {
		return false
	}
	// The reset in UTC and off a Unix time, so == compares the instant; and a
	// reading whose reset has passed arrives lapsed, so a late copy of one the
	// tick already lapsed changes nothing.
	next := rpc.SessionUsage{Percent: int(math.Round(u.Used * 100)), Resets: u.Resets.UTC(), Warned: u.Warned, Limited: u.Limited,
		Lapsed: !now.Before(u.Resets)}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.held && (b.reading == next || older(next, b.reading, now)) {
		return false
	}
	b.reading, b.held = next, true
	return true
}

// lapse marks the reading stale once its reset has passed, reporting that once.
// now is the wall clock (limitClock), so a reset slept through is caught on the
// first tick after waking.
func (b *usageBook) lapse(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.held || b.reading.Lapsed || now.Before(b.reading.Resets) {
		return false
	}
	lapsed := b.reading
	lapsed.Lapsed = true
	b.reading = lapsed
	return true
}

// report is the reading for a status report, nil until there is one.
func (b *usageBook) report() *rpc.SessionUsage {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.held {
		return nil
	}
	u := b.reading
	return &u
}

// older reports a reading that left the API before the held one: each agent's
// events arrive on its own goroutine, so two a moment apart can land out of
// order. A window's use only grows, so a lower figure for the same reset is
// older, and so is a window whose reset has passed beside one still running.
// Another account's live window, resetting sooner, is not. "Passed" is this
// machine's clock, which the stream gives nothing better than (deferred.md).
func older(next, held rpc.SessionUsage, now time.Time) bool {
	if next.Resets.Equal(held.Resets) {
		return next.Percent < held.Percent
	}
	return next.Resets.Before(held.Resets) && !now.Before(next.Resets)
}
