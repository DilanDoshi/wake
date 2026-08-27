package ui

// The permission mode, from ⇧⇥ to the label.
//
// # The label moves on the receipt, never on the keystroke
//
// This is the whole design, and it is why the old indicator was deleted rather
// than fixed. A key that cycled a label reached no process, and it failed in the
// unsafe direction: the label that appeared to *tighten* permissions was the one
// that did nothing. So the key writes a frame, the daemon's answer is the
// confirmation, and modeOf changes when the answer arrives - ⌃F's pattern.
//
// It is not belt-and-braces. `manual` is accepted by the CLI and silently
// normalizes to `default`, so a label built on the string that was sent is wrong
// on a real position rather than in principle
// (docs/superpowers/notes/2026-08-12-permission-mode-findings.md §6). Wake does
// not send `manual` - the cycle spells that position the way the receipt does -
// but the wiring does not depend on that, which is the point.
//
// # Two observables, one fact
//
// The receipt is one. Every turn's `init` is the other, and it is the only one
// that can see a mode Wake did not ask for. Two cases need it and both are
// recorded:
//
//   - A mode set mid-session **does not survive a park and wake** (§8). The
//     woken process comes back in its spawn mode and nothing announces it.
//   - A permission allow carrying `updatedPermissions` changes the whole
//     session's mode from inside a single tool approval, with **no receipt at
//     all** (§5).
//
// Both would leave a believed mode that is not the real one - I7's own sentence.
// init costs nothing to read and corrects both, which is what makes the ruling
// on §8 "say it" rather than "persist it": there is no mode field in the park
// book, and the display cannot go stale for longer than a turn.
//
// # One agent's mode is one agent's
//
// There is no @all for this. Thirty agents' permission modes moved by one
// keystroke is the failure the per-agent card exists to prevent, arriving one
// level up. modes is keyed by session and ⇧⇥ acts on one target.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// modeFailed names the write that could not happen.
	modeFailed = "changing that agent's permission mode"

	// noModeTarget is ⇧⇥ with no agent in front of it. There is deliberately no
	// fleet-wide fallback: see this file's header.
	noModeTarget = "⇧⇥ changes one agent's permissions. " + noPaneAdvice

	// modeAsked is said from the room, for parkedFormat's reason: a DM names its
	// agent in its own header, but in the room the target is whoever the roster
	// cursor is on, and a key that changes what an agent may do somewhere the eye
	// is not resting has to say where.
	//
	// It names the mode as a request rather than a result - "asking for" - because
	// the receipt has not arrived. The label is what reports the answer.
	modeAsked = "asking %s%s for %s permissions…"

	// modeWouldRaceAnAsk is ⇧⇥ on an agent stopped dead on a permission request,
	// and it is the one state this refuses.
	//
	// What happens to an outstanding ask when the mode changes under it is
	// **unrecorded** - findings §9 item 3, alongside item 2, whether the request
	// is even accepted mid-turn. Neither has a byte behind it, and ⇧⇥ is a key an
	// operator can press at any moment, so this is reachable rather than
	// theoretical. This project does not design around unrecorded behaviour.
	//
	// It refuses rather than sending and finding out, because the failure would
	// land on the card in front of the operator: an ask that quietly changes or
	// disappears under a decision somebody is mid-way through making. ⌃C refuses
	// the same state for a neighbouring reason and names esc; this names the card,
	// because answering it is the thing that clears the way.
	modeWouldRaceAnAsk = "%s%s is stopped on a permission request. Answer it - or esc withdraws it - then ⇧⇥ changes its permissions."

	// modeRevertedFormat is said when a woken session comes back in the mode it
	// spawned in rather than the one it was moved to, which is always
	// (findings §8): a mode is a property of the process, and --resume does not
	// carry it.
	//
	// **This sentence is the whole of the ruling on that.** The alternative was a
	// mode field in the park book and a re-apply on the wake, and it was not
	// taken: parked.json holds the minimum that can do the job, and the argv half
	// of the re-apply rests on behaviour nobody has recorded (§9 item 1). Saying
	// it costs a row and rests on nothing.
	//
	// It names the mode rather than only the fact, because "its permissions
	// changed" leaves an operator to go and look at exactly the indicator that
	// was wrong a moment ago.
	modeRevertedFormat = "%s%s came back in %s permissions - a mode set while an agent is running does not survive a park. ⇧⇥ sets it again."

	// modeRefusedFormat carries the CLI's own words for a control request it
	// turned down, because they are the only account of why the key did nothing.
	// The reason is passed through rather than paraphrased: it names the modes
	// that would have been accepted.
	modeRefusedFormat = "%s%s refused that: %s"
)

// modeCycle is what ⇧⇥ walks, and it is Claude Code's own order rather than
// Wake's.
//
// This used to be three positions in a different order, refusing acceptEdits and
// dontAsk on the grounds that both loosen permissions and a key with no
// confirmation should not reach a fourth and fifth way of doing that. What
// changed is evidence: ⇧⇥ is the one key this build and Claude Code agree on,
// and Claude Code's `chat:cycleMode` was matched against 2.1.233 - the
// switch is in testdata/claude-mode-cycle.json and the note beside it. With
// bypassPermissions unavailable, which every Wake session is, it walks
// default → acceptEdits → plan → auto → default. Walking a different order
// under the same keystroke is the disagreement worth more than the ruling was.
//
// **dontAsk is still not a position, and now that is Claude Code's answer too.**
// Its switch has `case "dontAsk": return "default"` - an exit, not a stop on the
// way round. The old ruling and the shipped product reached that independently,
// which is the argument for keeping it.
//
// bypassPermissions is refused by the CLI outright for a session not launched
// with --dangerously-skip-permissions (§7), which nothing here passes, so it is
// unreachable by construction rather than by a check.
//
// The order costs half of what it replaced and the half kept is the one that
// matters. Every session spawns `auto`, so the first press is auto → default and
// still tightens - the direction the deleted indicator got wrong, and the one a
// key nobody has read the legend for is judged on. The *second* press,
// default → acceptEdits, loosens, where the old traversal tightened all the way
// round. That is the trade, and it is paid for a key that behaves the way an
// operator's other thirty windows behave.
var modeCycle = []string{
	core.PermissionModeDefault,
	core.PermissionModeAcceptEdits,
	core.PermissionModePlan,
	core.PermissionModeAuto,
}

// nextMode is the position after this one, wrapping.
//
// A mode not on the cycle - dontAsk, or a Claude that grew a seventh - lands on
// the first position rather than being left where it is, so the key always does
// something. Leaving it would be a key that silently stops working for exactly
// the sessions whose mode came from somewhere unexpected.
//
// With `default` first that fallback *is* Claude Code's switch rather than
// merely resembling it: its `case "dontAsk"` and its `default:` arm both return
// "default", and `auto` sits last here so wrapping gives the same answer its
// unlisted `auto` does. Every mode a Wake session can be in gets Claude Code's
// answer out of one list and no branch - see
// TestTheCycleIsClaudeCodesOwnCycle, which holds that against the recording.
func nextMode(current string) string {
	for i, mode := range modeCycle {
		if mode == current {
			return modeCycle[(i+1)%len(modeCycle)]
		}
	}
	return modeCycle[0]
}

// cycleMode asks for the next permission mode for the agent ⇧⇥ acts on.
//
// The target is interruptTarget's - the focused DM, the card on top, and
// otherwise the roster's selection. Reused rather than restated: esc, ⌃C and now
// ⇧⇥ are the keys that act on *one* agent from either pane, and one target rule
// in one place is what stops them disagreeing about which one that is. ⌃C parked
// an agent nobody chose once, which is why the rule exists.
func (a App) cycleMode() (tea.Model, tea.Cmd, bool) {
	id, ok := a.interruptTarget()
	if !ok {
		notice.Report("%s", noModeTarget)
		return a, nil, true
	}
	if a.endedAgent(id) || a.parkedAgent(id) {
		// No process to take the frame. Silent, the same trade interrupt and
		// park make: nothing was lost and the roster already says so.
		return a, nil, true
	}
	if a.blockedAgent(id) {
		notice.Report(modeWouldRaceAnAsk, agentPrefix, a.agentName(id))
		return a, nil, true
	}

	want := nextMode(a.cyclingFrom(id))
	if a.focus != id {
		notice.Report(modeAsked, agentPrefix, a.agentName(id), want)
	}
	return a.asked(id, want), a.write(modeFailed, rpc.Frame{Kind: rpc.FrameMode, SessionID: id, Mode: want}), true
}

// cyclingFrom is the position the next press moves off: what this client last
// asked for and has not been answered about, and otherwise what it believes.
//
// The two are different for as long as a round trip takes, and ⇧⇥ is a key that
// gets mashed. Cycling from the belief alone makes two presses inside one round
// trip both read the same starting point and ask for the same thing, so the
// second press does nothing an operator can see.
//
// **This does not move the label and is not allowed to.** modeOf is still
// written only by a receipt or an init. What this holds is the client's own
// intent, which is a different question from what the session is in - and it
// stops being asked the moment the daemon answers.
func (a App) cyclingFrom(sessionID string) string {
	if asked, ok := a.asking[sessionID]; ok {
		return asked
	}
	return a.modeOf(sessionID)
}

// asked records the mode this client just requested, so a second press cycles
// past it rather than back onto it.
func (a App) asked(sessionID, mode string) App {
	next := make(map[string]string, len(a.asking)+1)
	for id, held := range a.asking {
		next[id] = held
	}
	next[sessionID] = mode
	a.asking = next
	return a
}

// answered forgets an unanswered request, because the daemon has now answered
// it. Called for anything carrying a mode - a receipt, or an init that reports
// one - since either settles what the session is really in, which is the
// question the pending request was asking.
func (a App) answered(sessionID string) App {
	if _, ok := a.asking[sessionID]; !ok {
		return a
	}
	next := make(map[string]string, len(a.asking))
	for id, held := range a.asking {
		if id != sessionID {
			next[id] = held
		}
	}
	a.asking = next
	return a
}

// modeOf is the permission mode this client believes a session is in.
//
// The spawn mode until something says otherwise, which is honest for exactly as
// long as it takes the session's first init to arrive. It is what every session
// starts in, and a session that has said nothing yet has not contradicted it.
func (a App) modeOf(sessionID string) string {
	if mode, ok := a.modes[sessionID]; ok {
		return mode
	}
	return spawnedMode
}

// notedMode records what a receipt or an init reported.
//
// A new map rather than a write into the old one: App is copied by value on
// every Update, and mutating a shared map would let one frame's belief appear in
// a model built before it arrived.
func (a App) notedMode(sessionID, mode string) App {
	if sessionID == "" || mode == "" {
		return a
	}
	// Flattened once, here, because two surfaces draw this value now - the
	// legend's tail and the conversation's status bar - and it is the agent's
	// own: it arrives on a receipt or an init, and nothing between the CLI and
	// this map constrains it to the four words the cycle walks. A newline in it
	// draws a legend row nobody counted, which is the pane-taller-than-its-box
	// failure chromeHeight exists for, and a control sequence rewrites whatever
	// the terminal has drawn so far. Sanitising at the writer rather than at the
	// two readers is what makes the bar and the legend agree by construction
	// instead of by both remembering to.
	mode = oneRow(mode)
	if held, ok := a.modes[sessionID]; ok && held == mode {
		// Every turn's init reports a mode, so this is the common call and it
		// has nothing to record. Without the guard it copies a.modes and then
		// the whole dms map through refreshedBar, once per turn per session,
		// to store what is already there.
		return a
	}
	next := make(map[string]string, len(a.modes)+1)
	for id, held := range a.modes {
		next[id] = held
	}
	next[sessionID] = mode
	a.modes = next
	// The bar names the mode too, and its cache lives on the stored DM - so a
	// belief that moved here without the stored conversation being told leaves
	// the key current and the cache stale, and the pane re-renders from that
	// mismatch on every frame. That is BUG-5 through the event path: ⇧⇥ travels
	// as a receipt rather than as a fleet report, so App.refreshedAgents never
	// sees it. Measured at 20 draws over 20 frames after one receipt.
	return a.refreshedBar(sessionID)
}

// forgotModes drops every belief this client holds, for the two events that
// mean "something may have happened here that I did not see": a reported frame
// gap, and a reattach.
//
// A receipt is an ordinary frame, so the daemon drops one for a client whose
// queue is full exactly as it drops anything else, and a reattached window was
// not there at all. Keeping a belief across either is keeping one nothing can
// vouch for - and the direction it fails in is the unsafe one, because a stale
// `plan` beside an agent now running `auto` tells an operator their agent will
// ask before it edits.
//
// What is left is spawnedMode, and that is a claim rather than an absence -
// deliberately, because it is the **loosest** of the three. Being wrong that way
// means Wake says an agent acts freely while it is really planning, which costs
// somebody a surprise; being wrong the other way is what this whole feature
// exists to stop. The next turn's init replaces the guess with the fact.
// The unanswered requests go with them. A request whose answer may have been in
// the gap is one nothing will ever settle, and cycling from it forever would
// pin the key to a position the session never reached.
func (a App) forgotModes() App {
	a.modes, a.asking = nil, nil
	return a
}

// observedMode folds either observable into the belief.
//
// One function for both because they are one fact, and neither is preferred: the
// receipt is the answer to a question this client asked, and the init is what the
// session says about itself a turn later. Whichever arrives last is the newest
// thing known, which is what makes a stale belief self-correcting.
//
// Every other event kind leaves PermissionMode empty, so this is a read rather
// than a branch on kind.
// A refused control request is reported here and nowhere else. The daemon's
// write succeeded, so nothing sends an error frame, and the receipt is drawn by
// no view - which would leave a key that did nothing with no account of itself.
// The CLI's own reason is the whole content: it names the modes it would have
// accepted, or says the session was not launched dangerously enough.
func (a App) observedMode(sessionID string, ev core.Event) App {
	if ev.Kind == core.KindControlReceipt && ev.Control != nil && ev.Control.Error != "" {
		notice.Report(modeRefusedFormat, agentPrefix, a.agentName(sessionID), ev.Control.Error)
		return a.answered(sessionID)
	}
	if ev.PermissionMode == "" {
		return a
	}
	return a.answered(sessionID).notedMode(sessionID, ev.PermissionMode)
}

// modeReverted resets a woken session's mode to the one it spawned in, and says
// so when that is news.
//
// A woken process is a new process, and a mode does not survive one: the wake
// emits a bare `--resume <id>` and the mode reverts to the spawn flag
// (findings §8). So the belief is corrected here rather than waiting for the
// session's first init - the correction is *known* at this point, and holding a
// mode this client already knows to be wrong until a turn happens is the same
// stale label with a shorter life.
//
// Silent when nothing moved, which is the common case. A session that spawned
// `auto` and was never cycled comes back `auto`, and a sentence about that is a
// notice row spent saying nothing happened.
//
// That test reads the belief, so it depends on this running *before* the woken
// session's first init overwrites it - an init reporting the spawn mode would
// make the revert look like nothing having happened. It does, by construction
// rather than by luck: daemon.launch enqueues the fleet report on the client's
// queue immediately after starting the process, microseconds before the child
// can have produced a byte, so the report this is folding always precedes the
// init. If launch ever announces later than the first event it forwards, this
// goes quiet and the label stays correct - the benign direction, but the
// sentence is the point.
func (a App) modeReverted(sessionID, name string) App {
	if a.modeOf(sessionID) == spawnedMode {
		return a
	}
	notice.Report(modeRevertedFormat, agentPrefix, name, spawnedMode)
	return a.notedMode(sessionID, spawnedMode)
}

// hintMode is the mode the legend under the room's composer names.
//
// It is the mode of whichever agent ⇧⇥ would act on, and that is the rule rather
// than a convenience: a label naming one agent's mode beside a key that changes
// another's is the lie this feature exists to delete, wearing a new hat. With no
// target there is nothing true to say about a specific agent, so it falls back to
// what every session starts in.
func (a App) hintMode() string {
	if id, ok := a.interruptTarget(); ok {
		return a.modeOf(id)
	}
	return spawnedMode
}
