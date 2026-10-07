//go:build unix

// BUG-46 through the real binary: the operator types a first brief to a
// just-spawned agent in the room, ends it with a /command, and the menu offers
// the agent's skills although it has taken no turn and sent no init.

package main

import "testing"

func TestAFreshAgentsSkillsCompleteInTheRoomBeforeItsFirstTurn(t *testing.T) {
	withScriptedAgent(t, scriptHandshakes)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 120, 32) // the room, with one fresh agent on its roster
	s.await(handshakeKnown)
	s.settle()
	names := agentsOnRoster(s)
	if len(names) != 1 {
		t.Fatalf("want the one agent bare wake spawned, got %v.\n%s", names, s.dump())
	}

	s.send("@" + names[0] + " read the failing test\n")
	s.send("and find out why it only fails on CI\n")
	s.send("then, when it is green, run /compl")
	s.await("/" + handshakeSkill)
	s.await(completionKeyHint)

	// The `x` is the sync point: ⇥ replaces only the token being typed.
	s.send("\tx")
	s.await("run /" + handshakeSkill + " x")
}
