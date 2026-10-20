//go:build unix

package main

import "testing"

// On a real screen: a turn the usage limit stops pins a notice saying Wake will
// resume it, and signing in to another account in some other session does - a
// Wake line in the conversation, then the agent answering the "continue".
func TestAnAgentALimitStoppedResumesWhenYouSignInElsewhere(t *testing.T) {
	withScriptedAgent(t, scriptUsageLimits)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	signInAs(t, dir, limitedAccount)

	s := startWakeInAConversation(t, 160, 40)
	s.await("ready")
	s.send("ship it\r")
	s.await("Wake resumes it")

	signInAs(t, dir, "acct-b")
	s.await("↻ resumed after the usage limit")
	s.await(heardPrefix + "continue")
}
