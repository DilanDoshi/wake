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

	roomFullFmt = "the room holds %s messages — /clear to lighten it"
)

// roomFullNotice is the reminder at k reminders' worth of messages.
func roomFullNotice(k int) string { return fmt.Sprintf(roomFullFmt, thousands(k*roomReminderEvery)) }

// clearRoom is the room's /clear. An argument is refused rather than guessed at:
// the room's clear takes none, and an agent is cleared by @name /clear.
func (a App) clearRoom(arg string) (App, tea.Cmd) {
	if arg != "" {
		notice.Report("%s", roomClearUsage)
		return a, nil
	}
	a = a.clearDraft().heldAsked()
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
	// No prefix: a room past its cap marks the history it reclaimed, and a
	// cleared room has reclaimed nothing.
	r.tr = r.tr.replaceFrom(renderRoom(r, nil), 0, "").toBottom()
	return r
}

// heldAsked marks every session the room holds lines for as already asked, the
// way askRoomHistory refuses one with lines on screen: a park and wake after the
// clear would otherwise ask, and restore a turn whose live echo is still here.
func (a App) heldAsked() App {
	next := roomAsk{at: make(map[string]askedAt, len(a.roomAsk.at)), pending: append([]string(nil), a.roomAsk.pending...)}
	for k, v := range a.roomAsk.at {
		next.at[k] = v
	}
	for _, l := range a.room.said.slice(a.room.said.first(), a.room.said.len()) {
		if _, asked := next.at[l.ev.SessionID]; l.ev.SessionID != "" && !asked {
			next.at[l.ev.SessionID] = askedAt{}
		}
	}
	a.roomAsk = next
	return a
}

// remind says so once each time the room passes another roomReminderEvery
// messages. An ordinary notice, so it times out like every other; App.withRoom
// asks only while the room holds the keys, since /clear typed in a conversation
// clears that agent's.
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
