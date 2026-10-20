package ui

// The room's own /clear, and its reminder to use one.
//
// A bare /clear in the room empties the group chat in this window and touches
// nothing else: no agent, no conversation, no frame (owner's 2026-10-19 ruling).
// It is for when the room grows heavy - every line it holds costs memory and is
// re-wrapped on a resize - so the room says, briefly, each time it passes
// another roomReminderEvery messages. A re-opened window restores its history
// from the transcripts again; the clear is this window's.

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
)

const (
	// roomReminderEvery is how many messages the room holds between reminders.
	roomReminderEvery = 1000

	roomCleared    = "cleared the group chat — agents and their conversations are untouched"
	roomClearUsage = "/clear on its own clears the group chat; @name /clear clears one agent's conversation"

	// roomFullFmt takes the count in thousands.
	roomFullFmt = "the room holds %d,000 messages — /clear to lighten it"
)

func roomFullNotice(thousands int) string { return fmt.Sprintf(roomFullFmt, thousands) }

// clearRoom is the room's /clear. An argument is refused rather than guessed at:
// the room's clear takes none, and an agent is cleared by @name /clear.
func (a App) clearRoom(arg string) (App, tea.Cmd) {
	if arg != "" {
		notice.Report("%s", roomClearUsage)
		return a, nil
	}
	a = a.clearDraft()
	a = a.withRoom(a.room.cleared(clock()))
	a.sel, a.selecting = selection{}, false
	notice.Report("%s", roomCleared)
	return a, nil
}

// cleared is this room with nothing in it: the banner again, the reminder count
// back to nothing, and history older than at kept out when it is restored later.
// The view - size, focus, filter, draft - and ↑'s prompts stay.
func (r Room) cleared(at time.Time) Room {
	prompts := r.prompts()
	n := r.said.len()
	r.said = chunked[roomLine]{base: n, n: n}
	r.raw, r.hist, r.reclaimed, r.expanded, r.lineMoves = nil, 0, false, nil, nil
	r.clearedAt, r.reminded, r.earlier = at, 0, prompts[max(len(prompts)-promptWindow, 0):]
	r.tr = r.tr.replace(renderRoom(r, nil)).toBottom()
	return r
}

// remind says so once each time the room passes another roomReminderEvery
// messages. An ordinary notice, so it times out like every other.
func (r Room) remind() Room {
	if k := r.said.count() / roomReminderEvery; k > r.reminded {
		r.reminded = k
		notice.Report("%s", roomFullNotice(k))
	}
	return r
}

// sinceClear keeps restored lines written after the room was cleared.
func (r Room) sinceClear(lines []roomLine) []roomLine {
	if r.clearedAt.IsZero() {
		return lines
	}
	kept := make([]roomLine, 0, len(lines))
	for _, l := range lines {
		if l.ev.At.After(r.clearedAt) {
			kept = append(kept, l)
		}
	}
	return kept
}
