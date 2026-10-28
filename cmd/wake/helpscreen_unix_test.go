//go:build unix

package main

// /wake-help through the real binary: typed into a conversation, scrolled with
// the page key, narrowed by typing, and closed with esc, from real bytes through
// a real terminal. The unit tests decide what each row says and which key does
// what; this is the only thing that puts the view's pane, its keys and the shell
// section `wake help` prints through a real pty.

import (
	"testing"
)

func TestWakeHelpOpensScrollsFiltersAndClosesOnARealScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 160, 44)
	s.await("ready")

	s.send("/wake-help\r")
	s.await("› wake-help")
	s.await("park the agent in front of you") // the ⌃C line, from the legend

	// The page key moves the body: the first row leaves, and the shell section
	// `wake help` prints is at the bottom of it.
	s.send("\x1b[6~")
	s.awaitGone("send the draft; on an empty one")
	for range 12 {
		s.send("\x1b[6~")
	}
	s.await("wake setup-terminal")
	for range 12 {
		s.send("\x1b[5~")
	}
	s.await("send the draft; on an empty one")

	// Typing narrows it, and the filter is on screen as it is typed.
	s.send("detach")
	s.await("filter: detach")
	s.awaitGone("park the agent in front of you")
	s.await("arm a detach")

	// esc is the way out, and the conversation is under it.
	s.send("\x1b")
	s.awaitGone("› wake-help")
	s.await("ready")
}
