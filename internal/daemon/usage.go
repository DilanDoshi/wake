package daemon

// The fleet's session-usage reading: the plan's five-hour window, which is the
// signed-in account's, so one per daemon whichever agent's API response
// reported it. Held here so a window that attaches later is told it at once
// (rpc.Status.SessionUsage), and never cleared, so the bar under the roster
// stays once it has drawn (owner's 2026-10-24 picks).

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

// note takes the reading an event carries, the newest arrival winning, and
// reports whether what a status report says changed.
func (b *usageBook) note(ev core.Event) bool {
	u := ev.SessionUsage
	if u == nil {
		return false
	}
	next := rpc.SessionUsage{Percent: int(math.Round(u.Used * 100)), Resets: u.Resets, Warned: u.Warned, Limited: u.Limited}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.held && sameUsage(b.reading, next) {
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

// sameUsage compares two readings, the reset by instant rather than by ==.
func sameUsage(a, b rpc.SessionUsage) bool {
	return a.Percent == b.Percent && a.Resets.Equal(b.Resets) && a.Warned == b.Warned &&
		a.Limited == b.Limited && a.Lapsed == b.Lapsed
}
