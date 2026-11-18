//go:build unix

// A `!cmd` sent from the room on a real screen: its message on one row, what it
// printed folded under it, and ⌃E opening the rest. internal/ui proves the fold
// and the read-back in process; this is the half a person sees.

package main

import (
	"fmt"
	"strings"
	"testing"
)

// roomBangRows is a made-up command printing six rows, row-0 first.
const roomBangRows = "printf 'row-%d\\n' 0 1 2 3 4 5"

func TestARoomBangFoldsItsOutputAndCtrlEOpensIt(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 36)
	s.await("ready")
	name := s.agentName()
	s.send("\x17") // ⌃W
	s.await("group chat")

	s.send("@" + name + " !" + roomBangRows + "\r")
	s.await("+3 lines")
	if row := s.lines()[s.rowOf("row-0")]; strings.Contains(row, "!printf") {
		t.Fatalf("the message and its output share a row: %q\n%s", row, s.dump())
	}
	if strings.Contains(s.text(), "row-3") {
		t.Fatalf("the output is not folded to three rows:\n%s", s.dump())
	}
	t.Logf("SCREENSHOT folded:\n%s", s.dump())

	s.send("\x05") // ⌃E
	s.await("row-5")
	if strings.Contains(s.text(), "to expand") {
		t.Fatalf("the pointer is still drawn after ⌃E:\n%s", s.dump())
	}
	t.Logf("SCREENSHOT opened:\n%s", s.dump())

	s.send("\x05")
	s.awaitGone("row-5")
}

func TestAFoldedRoomBangStillShowsItsExitStatus(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 36)
	s.await("ready")
	name := s.agentName()
	s.send("\x17") // ⌃W
	s.await("group chat")

	s.send("@" + name + " !" + roomBangRows + "; exit 3\r")
	s.await(fmt.Sprintf("… exited %d", 3))
	if strings.Contains(s.text(), "row-5") || !strings.Contains(s.text(), "+3 lines") {
		t.Fatalf("a failing bang is not folded:\n%s", s.dump())
	}
}
