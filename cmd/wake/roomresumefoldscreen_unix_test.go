//go:build unix

package main

import (
	"strings"
	"testing"
)

// On a real screen: an agent a limit keeps stopping as the account flips is
// resumed each time, and the room draws one line for it with a count that
// climbs - not a line per resume. The resume the agent answers is the last one
// the count takes, and the next resume after it is a line of its own.
func TestTheRoomFoldsAnAgentsRepeatedResumesIntoOneCountedLine(t *testing.T) {
	withScriptedAgent(t, scriptUsageLimitsHold)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	signInAs(t, dir, "acct-1")

	s := startWakeInAConversation(t, 160, 40)
	s.await("ready")
	name := s.agentName()
	s.send("ship it\r")
	s.await("Wake resumes it")
	s.send("\x17") // ⌃W: the group chat
	s.await("group chat")

	signInAs(t, dir, "acct-2")
	s.await("resumed after the usage limit")
	signInAs(t, dir, "acct-3")
	s.await("resumed after the usage limit ×2")
	signInAs(t, dir, "acct-4")
	s.await("resumed after the usage limit ×3")
	if n := strings.Count(s.text(), "resumed after the usage limit"); n != 1 {
		t.Errorf("the room should draw one resume line for three resumes, drew %d:\n%s", n, s.dump())
	}

	signInAs(t, dir, freeAccount) // the limit lifts: the agent answers this continue
	s.await(heardPrefix + "continue")
	s.await("resumed after the usage limit ×4")
	s.awaitGone("Wake resumes it")

	signInAs(t, dir, "acct-5") // spent again: the next resume is a line of its own
	s.send("@" + name + " again\r")
	s.await("Wake resumes it")
	signInAs(t, dir, "acct-6")
	s.awaitCount("resumed after the usage limit", 2)
}
