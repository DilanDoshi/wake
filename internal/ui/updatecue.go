package ui

// Telling a room left open for days that a newer wake is out. The check itself is
// cmd/wake's - the release host and the cache under ~/.wake - and this holds when
// to ask and what the answer was: on the first frame, then on a keystroke once
// updateRecheckEvery has passed. Never on a timer, so a room nobody types in does
// nothing. A newer release, once known, ends the asking and is named on the strip
// (upgradeMarked) for the life of the process - upgrading and restarting stay
// the operator's.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// UpdateCheck asks whether a newer wake is out: its version, or "" for none and
// for a check that failed. It blocks on the network, so it only runs as a
// tea.Cmd, and it gives its own once-a-day notice.
type UpdateCheck func() string

// updateRecheckEvery is how long a keystroke waits to ask again. An ask is one
// read of the cache, which still keeps the network to once a day.
const updateRecheckEvery = time.Hour

// updateCue is the room's side of the check.
type updateCue struct {
	check  UpdateCheck
	asked  time.Time
	asking bool
	newer  string
}

// updateCheckedMsg is a check's answer.
type updateCheckedMsg struct{ newer string }

// WithUpdateCheck gives the room its check; nil (WAKE_NO_UPDATE_CHECK) never asks.
func (a App) WithUpdateCheck(c UpdateCheck) App { a.upgrade.check = c; return a }

// dueUpdateCheck takes a check's answer ("" is nothing newer, or a check that
// failed), and is the check as a Cmd when msg should set one off: the first
// message, then a keystroke an hour on, never while one is out and never once a
// newer release is known - no later answer changes what to do. Typed on keys,
// so the composer's blink ticks cost no clock read.
func (a App) dueUpdateCheck(msg tea.Msg) (App, tea.Cmd) {
	if m, ok := msg.(updateCheckedMsg); ok {
		a.upgrade.asking, a.upgrade.newer = false, m.newer
		return a, nil
	}
	u := a.upgrade
	if u.check == nil || u.asking || u.newer != "" {
		return a, nil
	}
	if !u.asked.IsZero() {
		if _, key := msg.(tea.KeyMsg); !key || clock().Sub(u.asked) < updateRecheckEvery {
			return a, nil
		}
	}
	// Wall time: darwin's monotonic clock stops while a laptop sleeps.
	a.upgrade.asked, a.upgrade.asking = clock().Round(0), true
	return a, func() tea.Msg { return updateCheckedMsg{newer: u.check()} }
}
