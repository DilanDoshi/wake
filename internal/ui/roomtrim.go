package ui

// How the room keeps itself small: when it holds roomRetentionEvents messages it
// drops the oldest roomTrimEvents at once, so it swings between the two and never
// holds the cap (owner's 2026-11-23 ruling). A message is one line of the room's
// canonical store, said.count() - a line a focus hides included - so the cap is
// the room's own, never a channel's. The window's view only: no agent, no
// conversation and no transcript is touched, and a window opened later restores
// what its transcripts hold.

import (
	"fmt"

	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	// roomRetentionEvents is what the room may hold before it trims: every line
	// is re-wrapped on a resize and kept in memory (the DM's bound is its own).
	roomRetentionEvents = 3_000

	// roomTrimEvents is the oldest chunk dropped at once, so a trim is felt once
	// per chunk appended rather than by every message at the cap.
	roomTrimEvents = 1_000

	roomTrimmedFmt = "cleared the oldest %s group-chat messages — agents untouched"
)

// roomTrimCount is how many of the oldest messages a room holding held sheds:
// none under the cap, then whole chunks until it is under again. Live, the room
// reaches the cap one message at a time, so it is exactly one chunk; a restore
// that lands among the live lines can hold more, and sheds by the same rule.
func roomTrimCount(held int) int {
	if held < roomRetentionEvents {
		return 0
	}
	return ((held-roomRetentionEvents)/roomTrimEvents + 1) * roomTrimEvents
}

// roomTrimNotice is the notice for n messages trimmed.
func roomTrimNotice(n int) string { return fmt.Sprintf(roomTrimmedFmt, thousands(n)) }

// announceTrim says so once for every message trimmed since it last did, in an
// ordinary notice that times out like every other. App.withRoom asks only while
// the room holds the keys, so a trim that happens while a conversation does is
// said as the room takes them back (App.refocus), the thousands added together.
func (r Room) announceTrim() Room {
	if r.trimmed > 0 {
		notice.Report("%s", roomTrimNotice(r.trimmed))
		r.trimmed = 0
	}
	return r
}
