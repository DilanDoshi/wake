package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
)

// The @name room view filter, and the two ways it is controlled.
//
// A lone @name in the composer resolves a *target* (Room.focus) whether or not
// the room narrows to it - retarget records it either way, because ⌃A has to be
// able to narrow on demand even when the default is off. Whether the room
// actually narrows is Room.narrowed, and effectiveFocus is the one thing the
// render paths read: focus while narrowed, "" otherwise.
//
// ⌃A flips narrowed for the current target (the override); /groupchat-filter
// flips narrowDefault, which is the state each newly resolved target starts in.
// The override is per target: WithFocus resets narrowed to narrowDefault on a
// target change and leaves it alone on the same target, so it survives typing
// and dies when the addressee changes or the draft clears.

// effectiveFocus is the id the render paths filter on: the resolved target while
// the room is narrowing to it, and "" (admit everything) otherwise.
func (r Room) effectiveFocus() string {
	if r.narrowed {
		return r.focus
	}
	return ""
}

// ToggleNarrow flips whether the room narrows to its resolved target - ⌃A's
// whole effect. A no-op on the render when there is no target (effectiveFocus is
// "" either way); App.toggleRoomFilter refuses that case with a notice rather
// than flipping a bit nothing reads.
func (r Room) ToggleNarrow() Room {
	prevEff := r.effectiveFocus()
	r.narrowed = !r.narrowed
	return r.rerenderOnFocusChange(prevEff)
}

// WithNarrowDefault sets whether a freshly resolved @name narrows by default.
// It does not re-render: the command that calls it is itself typed into the
// composer as a non-mention draft, so the room is already un-narrowed when the
// setting changes, and the next target resolution reads the new default.
func (r Room) WithNarrowDefault(on bool) Room {
	r.narrowDefault = on
	return r
}

// rerenderOnFocusChange re-derives the filtered subset and jumps to bottom when
// the effective focus moved, and returns the receiver untouched otherwise. A
// focus change is treated exactly like a width change (SetSize sets
// following=true): the read on entering or leaving a thread is its latest line.
// The caller clears any text selection, because the re-render renumbers the
// lines a selection is anchored to - a width change's own rule.
func (r Room) rerenderOnFocusChange(prevEff string) Room {
	if r.effectiveFocus() == prevEff {
		return r
	}
	lines := r.said.slice(r.said.first(), r.said.len())
	blocks := renderRoom(r, lines)
	first := r.said.first()
	r.said = chunked[roomLine]{base: first, n: first}.append(lines...)
	r.tr = r.tr.replace(blocks)
	r.tr = r.tr.toBottom()
	return r
}

const (
	// roomFilterDMOnly and roomFilterNoTarget are ⌃A's two refusals: it is a room
	// view control, so it does nothing from a DM pane and nothing in a room that
	// nothing has narrowed. Each names the way in rather than just saying no.
	roomFilterDMOnly   = "⌃A widens the group chat, which is a room view - press ⇥ to the room to use it"
	roomFilterNoTarget = "⌃A widens the group chat once a lone @name has narrowed it - address one agent first"

	// roomFilterOn and roomFilterOff are what /groupchat-filter reports, set or
	// asked. roomFilterUsage is the refusal for a word that is neither on nor off.
	roomFilterOn    = "the @name view filter is on: a lone @name narrows the group chat to that agent's thread (⌃A widens)"
	roomFilterOff   = "the @name view filter is off: a lone @name no longer narrows the group chat (⌃A narrows on demand)"
	roomFilterUsage = "/groupchat-filter takes on or off"
)

// toggleRoomFilter is ⌃A: widen the group chat when a lone @name has narrowed
// it, or re-narrow. Refused off the room and refused with no target, because it
// is a control over one surface's view and nothing to do anywhere else. It
// clears any room selection on the flip for rerenderOnFocusChange's reason.
func (a App) toggleRoomFilter() (tea.Model, tea.Cmd, bool) {
	if a.focus != "" {
		notice.Report("%s", roomFilterDMOnly)
		return a, nil, true
	}
	if a.room.focus == "" {
		notice.Report("%s", roomFilterNoTarget)
		return a, nil, true
	}
	prevEff := a.room.effectiveFocus()
	a.room = a.room.ToggleNarrow()
	return a.clearedSelOnFocusChange(prevEff), nil, true
}

// groupchatFilter is /groupchat-filter on|off: the default a lone @name narrows
// under. Bare reports the current state; anything but on or off is refused by
// name, /manager's own rule - a toggle firing under a word it did not read does
// the opposite of what was typed half the time.
func (a App) groupchatFilter(arg string) (App, tea.Cmd) {
	a = a.clearDraft()
	switch arg {
	case "on":
		a.room = a.room.WithNarrowDefault(true)
		notice.Report("%s", roomFilterOn)
	case "off":
		a.room = a.room.WithNarrowDefault(false)
		notice.Report("%s", roomFilterOff)
	case "":
		if a.room.narrowDefault {
			notice.Report("%s", roomFilterOn)
		} else {
			notice.Report("%s", roomFilterOff)
		}
	default:
		notice.Report("%s", roomFilterUsage)
	}
	return a, nil
}
