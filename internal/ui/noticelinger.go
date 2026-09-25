package ui

// The notice row is a moment, not a record. Every notice clears itself after a
// linger scaled to how much of it the row can show, so `renaming @alex…` does
// not stand for an hour on a quiet fleet. What must outlive a moment - an agent
// stopped on an API failure - is pinned under it (pinnedNotice) and drawn
// whenever no fresher notice is up.
//
// The tick is one-shot and armed only when a fresh report appears: an idle
// Wake schedules nothing, which is beat.go's "no process on a timer" bound.

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	// noticeMinLinger is the shortest a notice stands: long enough to read at a
	// glance across a busy fleet.
	noticeMinLinger = 10 * time.Second

	// noticePerCell is the reading time one drawn cell buys, so a row wider
	// than a hundred cells stands longer than the floor.
	noticePerCell = 100 * time.Millisecond
)

// noticeTimer is the seam the expiry is scheduled through, for beat.go's
// reason: a test compresses it and sees what was armed.
var noticeTimer = tea.Tick

// noticeExpiredMsg is one report's linger elapsing.
type noticeExpiredMsg struct{ seq uint64 }

// noticeState is what the notice row owes: which report a linger is already
// armed for, and the API failures pinned until their sessions recover.
type noticeState struct {
	armed uint64
	// stuck is session id → the API's message, copy-on-write like authFailed.
	// Unlike that mark, /reauth does not clear it: a parked session is not back.
	stuck map[string]string
}

// armNoticeLinger times the newest report, once. Run after every message, so a
// notice reported anywhere - a fold, a draw, a goroutine, main before Run - is
// armed by the next message, and a repeat (a new seq) restarts the clock.
func (a App) armNoticeLinger() (App, tea.Cmd) {
	n, ok := notice.Latest()
	if !ok || n.Seq == a.notices.armed {
		return a, nil
	}
	a.notices.armed = n.Seq
	return a, noticeTimer(a.noticeLinger(n), func(time.Time) tea.Msg {
		return noticeExpiredMsg{seq: n.Seq}
	})
}

// noticeLinger is how long n stands: the cells the row can draw of it at
// noticePerCell, never under noticeMinLinger. Text the row truncates buys
// nothing - nobody can read it.
func (a App) noticeLinger(n notice.Notice) time.Duration {
	cells := min(lipgloss.Width(oneLine(noticePrefix+n.String())), a.noticeWidth())
	return max(noticeMinLinger, time.Duration(cells)*noticePerCell)
}

// noticeExpired clears the report the linger was armed for, unless a newer
// one has taken the row since (notice.ClearIf guards that).
func (a App) noticeExpired(m noticeExpiredMsg) App {
	notice.ClearIf(m.seq)
	return a
}
