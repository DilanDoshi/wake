package ui

// The rate-limit warning is a timed pop-up above the composer, not a line in
// the scrollback.
//
// Claude emits a rate_limit_event once per process on the first turn that hits
// the API; a warning or exhausted status (core.NoticeRateLimited) is the one
// worth surfacing, and it is a fact about *now* - "you are close to your usage
// limit" - not a thing that happened in the conversation. So it goes to the
// notice row internal/notice already owns and clears itself after a few
// seconds, rather than standing forever in the transcript like a failure does.
//
// The tick is one-shot and armed only when a warning fires: an idle Wake, or a
// benign `allowed` heartbeat, schedules nothing. That is the same "no process
// on a timer" bound the heartbeat keeps (see beat.go), reached the same way -
// through a tea.Tick seam a test can compress.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
)

// rateLimitLinger is how long the warning stands before it clears itself. Long
// enough to read at a glance across a busy fleet, short enough that it is gone
// before it becomes chrome nobody reads.
const rateLimitLinger = 6 * time.Second

// rateLimitPrefix heads the warning. The status string is Claude's own value
// (not a wire word - see CLAUDE.md's note on Event.Text), so it is passed
// through beside it when there is one.
const rateLimitPrefix = "usage limit approaching"

// rateLimitTimer is the seam the clear is scheduled through, for beat.go's
// reason: a test that hand-delivers the message never runs the command, so it
// cannot otherwise see that no tick is armed for a benign heartbeat.
var rateLimitTimer = tea.Tick

// rateLimitState is the timed notice's whole state.
type rateLimitState struct {
	// gen rises with every warning shown. A clear tick carries the gen it was
	// armed for and no-ops unless it is still the newest, so two warnings close
	// together clear once, at the last one's deadline, rather than the first's
	// expiry clearing the second early.
	gen int

	// text is the exact notice set, so the clear removes that message through
	// notice.ClearIf and never a different failure that took the row meanwhile.
	text string

	// arm records that a warning fired this fold and a clear tick is owed. Read
	// and reset by armRateLimitClear, the way beat reads anyWorking - so a batch
	// of frames arms one tick, not one per frame.
	arm bool
}

// rateLimitClearMsg is a warning's linger elapsing.
type rateLimitClearMsg struct{ gen int }

// rateLimited is the whole of what a rate-limit event does now. A warning pops
// the timed notice; a benign heartbeat pops nothing. Either way the event never
// reaches the room, a DM or the fleet - it is a fact about quota, not
// conversation content, so observe routes it here instead of appending it.
func (a App) rateLimited(ev core.Event) App {
	if ev.Notice != core.NoticeRateLimited {
		return a
	}
	text := rateLimitPrefix
	if ev.Text != "" {
		text += " · " + ev.Text
	}
	notice.Report("%s", text)
	a.rl.gen++
	a.rl.text = text
	a.rl.arm = true
	return a
}

// armRateLimitClear schedules the linger, once, when a warning has just fired.
// Read after a fold the way beat is, and idempotent by the same means: arm is
// the flag a fold sets and this consumes.
func (a App) armRateLimitClear() (App, tea.Cmd) {
	if !a.rl.arm {
		return a, nil
	}
	a.rl.arm = false
	gen := a.rl.gen
	return a, rateLimitTimer(rateLimitLinger, func(time.Time) tea.Msg {
		return rateLimitClearMsg{gen: gen}
	})
}

// rateLimitCleared is the linger elapsing: the warning goes, unless a newer one
// has replaced it (a later gen) or a different failure has taken the notice row
// since (notice.ClearIf guards that).
func (a App) rateLimitCleared(m rateLimitClearMsg) App {
	if m.gen == a.rl.gen {
		notice.ClearIf(a.rl.text)
	}
	return a
}
