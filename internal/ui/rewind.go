package ui

// esc esc's second case: an idle agent's earlier prompts, offered so a rewind
// can be aimed rather than guessed.
//
// The daemon answers what a session's active-branch user prompts and their
// message uuids are (rpc.FrameRewindTargets / FrameRewindTargetsReply) -
// core.Event carries neither, since a rewind targets a transcript message's
// own uuid and nothing above the airlock has ever needed one before. So this
// asks, and folds the answer into a picker the way history.go asks and folds
// a conversation's past - the ask and the open are two different moments,
// because only the daemon can read the file the answer comes from.
//
// # Why this is not a Card
//
// Same reason picker.go gives for the /effort menu: Cards.Reconcile rebuilds
// its open set from every fleet report and drops what is absent, and this has
// no request id and appears in no report. It shares the drawing - optionRow
// and detailRow are cards_blocks.go's - and nothing else.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// rewindTargetsFailed and rewindFailed name the writes that could not happen,
// so the notice row says which action it was rather than only what the
// socket said about it. sendFailed's own pattern.
const (
	rewindTargetsFailed = "asking what this conversation could be rewound to"
	rewindFailed        = "rewinding that conversation"

	// rewindRefusedFormat carries the daemon's own account of a rewind that did
	// not happen. Its write already succeeded, so nothing else says a word -
	// mode.go's modeRefusedFormat, one control receipt over.
	rewindRefusedFormat = "%s%s refused that rewind: %s"
)

// RewindPicker is one session's earlier prompts, newest first - so the
// cursor's resting place, index 0, is the newest.
type RewindPicker struct {
	// Session is whose conversation this answers.
	Session string

	// Prompts is what to show, and UUIDs is each one's target_message_uuid -
	// core.Event carries neither, so this is the operator's only source for
	// one. Parallel slices rather than a struct slice, optionRow's own shape:
	// it walks a []string, the way Picker.Options already does.
	Prompts []string
	UUIDs   []string

	// LastSeen is the newest active user turn's uuid: the tip a FrameRewind's
	// RewindLastSeen wants, whichever prompt the cursor picks.
	LastSeen string

	Cursor int
}

// Open reports whether there is a rewind picker at all. The zero value is
// closed, Picker's own reason: it is what makes "no picker" App's default.
func (p RewindPicker) Open() bool { return p.Session != "" }

// rewindArmable is whether esc esc's second press means "open the rewind
// picker" rather than "interrupt again": a focused conversation, nothing
// typed, and its agent not owed a turn.
//
// Composer emptiness is what keeps this and clearsOnEscape mutually
// exclusive - see escape.go - so escArmed's second-press meaning never has to
// be decided by anything but which of the two is true when it is read. Read
// fresh on every press rather than cached, so a report landing between two
// presses is seen: an agent that started a turn in between must fall back to
// a genuine interrupt, not open a picker on stale idleness.
func (a App) rewindArmable() bool {
	if a.focus == "" || !a.composerEmpty() {
		return false
	}
	agent, ok := a.fleet.Agent(a.focus)
	return ok && !turnInFlight(agent.State)
}

// askRewindTargets asks the daemon what the focused session could be rewound
// to. It does not open the picker itself - the reply does, in
// rewindTargetsArrived, because the daemon reads a file this client cannot.
func (a App) askRewindTargets() (tea.Model, tea.Cmd, bool) {
	return a, a.write(rewindTargetsFailed, rpc.Frame{Kind: rpc.FrameRewindTargets, SessionID: a.focus}), true
}

// rewindTargetsArrived folds the daemon's answer into a picker.
//
// Dropped rather than opened over a conversation it no longer answers for.
// historyArrived's own staleness guard, stricter here because a stale reply
// does not just draw wrong - Enter on it sends a real FrameRewind for a
// session nobody is looking at. Three ways a reply outlives its ask, all
// caught by re-running rewindArmable, the same predicate that gated the ask
// in the first place: the operator moved the keys to another conversation,
// started typing into this one, or the agent started a turn while the
// daemon was reading the transcript. And dropped over an open config
// picker - picker.go's "one at a time" rule, extended to the second picker
// that can open here.
//
// An empty list is not a picker with nothing in it: there is nothing to
// choose, so this reports instead and leaves the model otherwise unchanged.
func (a App) rewindTargetsArrived(f rpc.Frame) App {
	if _, held := a.dms[f.SessionID]; !held {
		return a
	}
	if f.SessionID != a.focus || !a.rewindArmable() || a.picker.Open() {
		return a
	}
	if len(f.RewindTargets) == 0 {
		notice.Report("%s%s has nothing to rewind to yet", agentPrefix, a.agentName(f.SessionID))
		return a
	}
	n := len(f.RewindTargets)
	prompts := make([]string, n)
	uuids := make([]string, n)
	for i, t := range f.RewindTargets {
		// Reversed: the daemon answers oldest first, and the cursor's resting
		// place - index 0 - is meant to be the newest prompt.
		prompts[n-1-i] = t.Text
		uuids[n-1-i] = t.UUID
	}
	a.rewind = RewindPicker{
		Session:  f.SessionID,
		Prompts:  prompts,
		UUIDs:    uuids,
		LastSeen: f.RewindTargets[n-1].UUID,
	}
	return a
}

// closeRewind takes the picker down with nothing sent.
func (a App) closeRewind() App {
	a.rewind = RewindPicker{}
	return a
}

// rewindRunning reports whether a picker's own session has started a
// turn since it opened - the CRITICAL hole an adversarial review found
// 2026-08-26: rewindArmable and rewindTargetsArrived both gate a picker's
// *opening* on idle, but nothing closed one back down again if that same
// session started running afterwards, e.g. the manager's autonomous
// send_to_agent, or any other status transition. reconcileRewind,
// rewindKey and confirmRewind each call this, in three different windows:
// the moment a report arrives with nobody touching a key, the moment a
// key arrives before that report has been folded, and the moment Enter is
// pressed regardless of which of the first two already ran.
func (a App) rewindRunning(sessionID string) bool {
	agent, ok := a.fleet.Agent(sessionID)
	return ok && turnInFlight(agent.State)
}

// reconcileRewind closes the picker the moment a fleet report shows its
// own session no longer idle, or ended - Cards.Reconcile's own reason
// (report.go), one picker over. Read after App.applyStatus has already
// folded the report into a.fleet, so this sees exactly what rewindKey and
// confirmRewind would see on the very next call - the belt to their
// braces for the common case, where the report and the key are two
// separate Update calls rather than one racing the other.
func (a App) reconcileRewind() App {
	if !a.rewind.Open() {
		return a
	}
	if a.endedAgent(a.rewind.Session) || a.rewindRunning(a.rewind.Session) {
		return a.closeRewind()
	}
	return a
}

// rewindKey is ↑↓ to move, ↵ to confirm and esc to cancel, and nothing else -
// pickerKey's own shape, read above App.key's switch for pickerKey's own
// reason: this is an interception above the switch legendEntries is checked
// against, not a case inside it, so it adds no legend entry.
//
// A key that is not one of those four goes to the composer, and the picker is
// dismissed in App.update rather than here - see the picker-close beside it.
//
// Scoped to the pane it was opened for - cardKey's own rule, a.cardOf(a.focus)
// and never Cards.Top: a picker left open on a conversation the operator has
// since tabbed away from must claim no keys at all, or the esc meant to
// interrupt whatever is now focused is swallowed as "close a picker nobody is
// looking at" instead of reaching the switch's own KeyEsc case.
func (a App) rewindKey(m tea.KeyMsg) (App, tea.Cmd, bool) {
	if !a.rewind.Open() || a.rewind.Session != a.focus {
		return a, nil, false
	}
	if a.rewindRunning(a.rewind.Session) {
		// reconcileRewind (report.go) closes the picker the instant a report
		// says this; this is the window before that report has been folded -
		// a key arriving just ahead of, or sharing a batch with, the status
		// push that would have closed it. Declining every key here, esc
		// included, is what lets it fall through to escape()/interrupt()
		// rather than the switch below swallowing the press. See CRITICAL,
		// adversarial review 2026-08-26.
		return a, nil, false
	}
	switch m.Type {
	case tea.KeyUp:
		return a.moveRewind(-1), nil, true
	case tea.KeyDown:
		return a.moveRewind(1), nil, true
	case tea.KeyEsc:
		return a.closeRewind(), nil, true
	case tea.KeyEnter:
		next, cmd := a.confirmRewind()
		return next, cmd, true
	}
	return a, nil, false
}

// moveRewind walks the list without wrapping, movePicker's own reason: a
// cursor that wraps makes the ends of a long list indistinguishable at a
// glance.
func (a App) moveRewind(by int) App {
	p := a.rewind
	p.Cursor = clamp(p.Cursor+by, 0, max(len(p.Prompts)-1, 0))
	a.rewind = p
	return a
}

// confirmRewind sends the cursored prompt as the rewind target.
//
// Only the send: the receipt - re-reading the transcript and prefilling the
// draft with the rewound turn's own words - is noteRewind's, below, off the
// core.KindRewindReceipt a FrameRewind's control_response resolves to. The
// picker closes unconditionally, whether or not the write itself succeeds -
// confirmPicker's own reasoning about a picker after a choice is made.
//
// endedAgent is interrupt()'s own guard: a picker can outlive the agent it
// was opened for, and a rewind aimed at a process nothing is left to hand it
// to is a write worth skipping rather than sending. rewindRunning is the
// same guard rewindKey reads before Enter ever reaches here - kept here too,
// belt and braces, so Enter reached any other way still refuses to write a
// live FrameRewind at a session that is mid-turn.
func (a App) confirmRewind() (App, tea.Cmd) {
	p := a.rewind
	if p.Cursor < 0 || p.Cursor >= len(p.UUIDs) || a.endedAgent(p.Session) || a.rewindRunning(p.Session) {
		return a.closeRewind(), nil
	}
	f := rpc.Frame{Kind: rpc.FrameRewind, SessionID: p.Session, RewindTarget: p.UUIDs[p.Cursor], RewindLastSeen: p.LastSeen}
	a = a.closeRewind()
	return a, a.write(rewindFailed, f)
}

// noteRewind folds a KindRewindReceipt: Claude's answer to the FrameRewind
// confirmRewind sent. The airlock classifies it beside KindControlReceipt for
// drawing purposes - never a transcript event, see fleet_test.go's roomCases
// and dm_blocks.go's default case - so this is the only place its payload is
// read, from App.observe.
//
// A refusal changes nothing on this client: the daemon's write already
// succeeded, so the transcript is exactly what it was and the draft is
// untouched, and notice.Report is the only account of it.
//
// A success means Claude has already rewritten the transcript on disk - the
// receipt follows the marker landing, never a race this client has to wait
// out - so the rewound conversation is re-read tree-aware regardless of which
// pane has the focus: an operator who tabbed away still finds the dead branch
// gone next time they look. Only the prefill is focus-gated, because handing
// the rewound prompt to a composer nobody is reading is a draft that appears
// to have typed itself into the wrong conversation.
func (a App) noteRewind(sessionID string, ev core.Event) App {
	if ev.Kind != core.KindRewindReceipt || ev.Rewind == nil {
		return a
	}
	if !ev.Rewind.Rewound {
		notice.Report(rewindRefusedFormat, agentPrefix, a.agentName(sessionID), ev.Rewind.Error)
		return a
	}
	if _, held := a.dms[sessionID]; held {
		// Emptied first, then asked again: askHistory refuses a session already
		// in askedHistory, and forgetHistoryAsk is what makes it askable. The
		// daemon's tree-aware FrameHistoryReply is the one and only thing that
		// repopulates it - see resetDM.
		a = a.resetDM(sessionID).forgetHistoryAsk(sessionID).askHistory(sessionID)
	}
	if a.focus == sessionID {
		a = a.withComposer(a.composer().WithDraft(ev.Rewind.PrefillText))
	}
	return a
}

// resetDM empties one conversation's transcript back to nothing, so the
// history ask noteRewind issues next repopulates it from the daemon's reply
// alone rather than merging onto what streamed before the rewind.
//
// events and marks both index into the same sequence, so both go: Before
// shifts marks by what it prepends, which is only correct when they were
// indices into the *live* half of a merge - and after a rewind there is no
// live half to be indices into, since the whole conversation is being
// replaced. Before already resets the checklist and the run tally themselves
// and re-renders unconditionally, so nothing else here needs clearing by hand.
//
// A no-op for a conversation this client never opened: a receipt for a DM with
// no pane to re-read into, which is what "held" is asking. A later open reads
// it fresh off disk, correctly, the ordinary path any new conversation takes.
func (a App) resetDM(id string) App {
	dm, held := a.dms[id]
	if !held {
		return a
	}
	next := *dm
	next.events, next.marks = chunked[core.Event]{}, nil
	return a.withDM(id, next)
}

// View draws it, through the same rows a card and a Picker draw. agent is the
// display name for the header, resolved by the caller: a session's name can
// change while a picker has been open, and this holds no cache to go stale.
func (p RewindPicker) View(width int, agent string) string {
	if !p.Open() {
		return ""
	}
	rows := make([]string, 0, len(p.Prompts)+1)
	rows = append(rows, detailRow(fmt.Sprintf("rewind %s%s to…", agentPrefix, agent), width))
	for i, prompt := range p.Prompts {
		rows = append(rows, optionRow(prompt, width, i == p.Cursor, false, AccentStyle))
	}
	return strings.Join(rows, "\n")
}
