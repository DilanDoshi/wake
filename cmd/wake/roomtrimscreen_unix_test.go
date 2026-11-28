//go:build unix

package main

import (
	"strings"
	"testing"
)

// On a real screen: an agent that fills the group chat past three thousand
// messages makes the room drop its oldest thousand and say so, and the oldest
// messages are gone from the top of it, behind the reclaimed-history line. The
// newest is still the one the room is following.
func TestTheRoomDropsItsOldestThousandAtThreeThousandMessages(t *testing.T) {
	withScriptedAgent(t, scriptChatters)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 160, 40)
	s.await("ready")
	name := s.agentName()
	s.send("\x17") // ⌃W: the group chat
	s.await("group chat")

	s.send("@" + name + " talk\r")
	s.await("cleared the oldest 1,000 group-chat messages")
	s.await(chatterText(chatterCount)) // the room kept following the newest line
	s.settle()
	if n := strings.Count(s.text(), "cleared the oldest"); n != 1 {
		t.Errorf("the room said so %d times for one trim:\n%s", n, s.dump())
	}
	if strings.Contains(s.text(), "dropped") {
		t.Fatalf("the window fell behind, so the room did not see every message:\n%s", s.dump())
	}

	s.send(strings.Repeat("\x1b[5~", 600)) // ⇞ to the top: a press is under a page, and the room is some 6,000 rows
	s.await("older room history reclaimed")
	if strings.Contains(s.text(), chatterText(1)) {
		t.Errorf("the oldest message is still in the room after its chunk was dropped:\n%s", s.dump())
	}
}
