//go:build unix

// A card scrolls with its pane, on a real screen.
//
// internal/ui holds the geometry; this is the one place the wheel is an actual
// mouse report read off a tty by the real binary, and the rows are what vt10x
// drew from the escape codes that came out.

package main

import "testing"

const (
	wheelUpReport   = "\x1b[<64;20;3M" // SGR wheel up, over the pane's transcript
	wheelDownReport = "\x1b[<65;20;3M"
	cardCutLine     = "keys return when the whole card is on screen"
)

func TestTheWheelScrollsTheWholePaneWhenACardIsUp(t *testing.T) {
	withScriptedAgent(t, scriptAsks)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.send("write the file\r")
	s.await("[d]eny")

	// Too short for the card and the composer together: the composer scrolls out
	// of the way and the whole card, keys and all, is what is left on screen.
	s.resize(100, 8)
	s.settle() // the repaint after a resize is not the frame to read
	s.await("[d]eny")

	// Down brings the page to its foot: the composer, and the card's last row,
	// which no longer advertises keys that are not read.
	s.send(wheelDownReport)
	s.await(cardCutLine)
	s.awaitGone("[d]eny")

	// Up takes the card and the composer back down with the transcript coming in
	// above them: two notches, one at a time, reach the prompt the card answers.
	for range 2 {
		s.send(wheelUpReport)
		s.settle()
	}
	s.await("write the file")
	s.awaitGone(cardCutLine)

	// A key goes to the composer, so the page is brought to it.
	s.send("x")
	s.await("> x")
}
