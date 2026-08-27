package ui

// Leaving takes ⌃O and then ↵, and the arm is on screen the whole way.
//
// # Why this key needs a mechanism at all
//
// ⌃O is `app:toggleTranscript` in Claude Code - the key its own truncation
// notice advertises under any tool result it cut. Wake binds it to detach. So
// the one reflex a Claude Code operator brings to this window that costs
// something is the reflex to read the rest of a tool result: the window closes
// with the fleet still running. keymap_test.go holds the whole collision set
// and which ones are accepted without a mechanism; this is the only one whose
// result is not on screen to be undone.
//
// **And the way back stopped being the obvious command when fleets landed.** A
// bare `wake` makes a *new* fleet rather than reopening one (cmd/wake/main.go);
// `wake --fleet <name>` is what comes back, and the line the client prints on
// its way out names neither - `detachHint` is `wake status · wake stop`. The
// window that closed is the one that was showing which fleet this was.
//
// # Why the confirm is ↵ and not a second ⌃O
//
// It shipped as a second ⌃O and that is the version this replaces, because a
// same-key confirm fires on exactly the reflex it exists to catch. There is no
// key release, no timing and no distinct signal in a KeyMsg, so terminal auto
// repeat is indistinguishable from intent - and so is the ordinary human reply
// to a key that appeared to do nothing, which is to press it again. **Measured
// rather than assumed**: TestTwoCtrlOsInOneReadArriveAsTwoPresses shows two
// ⌃O sharing a single read arriving as two plain messages, where ⎋⎋ collapses
// into one `alt+esc` (escprobe_test.go). Nothing downstream can tell them apart
// because nothing upstream kept the difference.
//
// ↵ is this tree's confirm already: a card's a/d are settled by ↵, for the same
// reason and with the same disarm. It cannot be produced by repeating ⌃O, and
// at an empty composer it was doing nothing more than opening the picked
// conversation.
//
// # Why a second ⌃O cancels rather than re-arming
//
// So that a run of auto repeat of any length lands somewhere harmless: odd
// leaves an arm that is drawn and confirms nothing, even leaves the model
// exactly where it started. Re-arming would be honest too, but then ⌃O's own
// legend label while armed could only say `detach`, which is false - ⌃O does
// not detach at that moment, ↵ does.
//
// # Why the arm is drawn, and why that is the whole defence
//
// App.disarmed is reached from key and mouse paths only. A stream frame, a
// heartbeat, a resize, a geometry settle and a reattach all leave the arm
// standing, so it can outlive any amount of fleet activity. The first version
// of this had one tell - a notice - and internal/notice is a single
// most-recent-message slot that an ending, a dropped frame or a mode correction
// takes within seconds. An arm with no account of itself and an unbounded life
// is a detach nobody saw coming.
//
// **Broadening the disarm to those messages was considered and is worse.** On a
// thirty-agent fleet a frame lands between the two presses constantly, so a
// stream-cancelled arm would make detaching fail at random - and worse, would
// make ↵ mean *send* on the press the operator aimed at *detach*, decided by
// socket timing. The card arm survives non-input messages for exactly this
// reason and is safe because it stays drawn; this now does the same. legend.go
// swaps `↵ send` to `↵ detach` and `⌃O detach` to `⌃O cancel` for as long as it
// lives, in every pane, and ↵ is the entry that survives every truncation.
//
// The notice is kept beside it and demoted: the legend carries the *state*,
// which is what has to persist, and the notice carries the *explanation*, which
// only has to arrive once.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/notice"
)

// detachArmPrefix opens the arm's notice and is what tells it from any other
// message in the slot.
const detachArmPrefix = "↵ to detach"

// detachArm is what the first ⌃O says. It names the key that finishes the
// detach, the key that takes it back, and ⌃E - which expands a truncated tool
// result, and is what the press was probably aimed at.
const detachArm = detachArmPrefix + ": this window closes and the fleet keeps working. " +
	"⌃O again cancels. ⌃E expands a tool result."

// armDetach takes ⌃O: it arms when nothing is, and cancels when something is.
//
// armed is passed rather than read off the model, because App.key has already
// disarmed by the time this runs - that disarm is what makes "every other input
// takes the arm back" true without a call site per key.
func (a App) armDetach(armed bool) (tea.Model, tea.Cmd, bool) {
	if armed {
		// Already taken back by the disarm above. Silent: the legend going
		// back to `↵ send` is the whole report, and a notice here would be a
		// row spent saying nothing happened.
		return a, nil, true
	}
	notice.Report("%s", detachArm)
	a.detachArmed = true
	return a, nil, true
}
