//go:build unix

// The completion menu at a real terminal: that it is drawn, and that its three
// keys survive everything between a keyboard and Wake.
//
// The keys are the reason this is here rather than only in internal/ui. This
// project has twice bound a chord that every guard in the tree agreed worked
// and no terminal ever delivered - ⇧↵, then all four ctrl+shift+arrows - so a
// key claimed in a switch is not evidence that pressing it does anything. ⌃N
// and ⌃P are \x0e and \x10, and this is the only thing that can see them
// arrive.

package main

import (
	"strings"
	"testing"
)

// The menu is drawn over a draft that begins a command, and ⇥ finishes it.
func TestTheCompletionMenuIsDrawnAndTabTakesIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	closeRoster(s)

	s.send("/resu")
	s.await("/resume")
	s.await(completionKeyHint)

	// The `x` is the sync point and half the assertion: the space between it
	// and the command can only have come from the accept, and awaiting the
	// whole draft is what makes the absence below a reading of a settled frame
	// rather than of one the keystroke had not reached yet.
	s.send("\tx")
	s.await("/resume x")
	if strings.Contains(s.text(), completionKeyHint) {
		t.Errorf("the menu is still up over a finished command:\n%s", s.dump())
	}
}

// ⌃N and ⌃P walk it, and ⇥ takes whatever the walk landed on.
//
// `/n` matches two of Wake's own commands, sorted `/name` then `/new`, so one
// ⌃N is the whole difference between the two words this can end up with - and
// `/name` disappearing from the screen is what says the walk moved.
func TestTheMenuWalksWithControlNAndControlP(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	closeRoster(s)

	s.send("/n")
	s.await("/name")
	s.await("/new")

	// Down one, back up, down one: the draft never moves, so a key that did
	// nothing would leave the cursor where it started and ⇥ would take `/name`.
	s.send("\x0e\x10\x0e")
	s.settle()
	s.send("\tx")
	s.await("/new x")

	if strings.Contains(s.text(), "/name") {
		t.Errorf("⇥ after ⌃N left `/name` on screen, so the walk did not move:\n%s", s.dump())
	}
}

// The menu is drawn directly above the composer, which is where Claude Code
// draws it and where the thing being completed actually is.
//
// It used to go up through App.paneChrome, beside the ask card and the picker,
// which pinned it above the *transcript* - measured at 100x30 the menu sat ~20
// rows above the word it was completing, so reading it meant looking away from
// the draft. Asserted as an offset from the composer's own top border rather
// than as a row number, because the row a pane starts at is a function of the
// grid and this claim is not.
func TestTheCompletionMenuSitsDirectlyAboveTheComposer(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	s.send("\x17") // ⌃W: back to the room, which bare `wake` opens a pane over
	s.await(roomBoxTitle)
	closeRoster(s)

	s.send("/resu")
	s.await("/resume")
	s.await(completionKeyHint)

	// The hint is the menu's last row - View appends it under the offers - so
	// the border of the box being typed into is the very next row down.
	menu, box := s.rowOf(completionKeyHint), s.rowOf(roomBoxTitle)
	if menu < 0 || box < 0 {
		t.Fatalf("menu row %d, composer row %d: one of them is not on screen\n%s", menu, box, s.dump())
	}
	if menu != box-1 {
		t.Errorf("the menu's last row is %d and the composer's border is %d, want the menu directly above it (%d rows away)\n%s",
			menu, box, box-menu, s.dump())
	}
}

// And the same claim in a conversation, which joins its rows in a different
// function: DM.View already puts the preview, the heartbeat and the dispatch
// list below the transcript for this reason, and the menu belongs with them.
func TestTheCompletionMenuSitsAboveAConversationsComposer(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	name := s.agentName()
	closeRoster(s)

	s.send("/resu")
	s.await(completionKeyHint)

	menu, box := s.rowOf(completionKeyHint), s.rowOf(agentPrefix+name)
	if menu < 0 || box < 0 {
		t.Fatalf("menu row %d, composer row %d: one of them is not on screen\n%s", menu, box, s.dump())
	}
	if menu != box-1 {
		t.Errorf("the menu's last row is %d and the conversation's composer border is %d, want the menu directly above it\n%s",
			menu, box, s.dump())
	}
}

// roomBoxTitle is the title the room's composer carries, which is what puts a
// findable string on the box's own top border. Spelled here because cmd/wake
// cannot import internal/ui's unexported constant; a conversation's box uses
// attach.go's agentPrefix beside the agent's name for the same purpose.
const roomBoxTitle = "group chat"

// completionKeyHint is a fragment of the line the menu draws about itself. A
// fragment rather than internal/ui's own constant, because cmd/wake cannot
// import an unexported one - and a whole-line match would fail on the width
// this terminal happens to have.
//
// The glyph is part of it and is not decoration: the bare word matched
// `.worktrees/autocomplete` in the status bar's own path, which is a branch
// name this very feature was written on. A fragment that can appear in a
// directory somebody is working in is not a fragment.
const completionKeyHint = "⇥ complete"
