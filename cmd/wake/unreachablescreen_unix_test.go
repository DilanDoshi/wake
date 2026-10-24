//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// An agent the API stopped says Wake will resume it, and is resumed - drawn as
// Wake's line - once another agent's turn goes through: here the "ready" of an
// agent started after the network came back.
func TestAnAgentTheAPIStoppedResumesWhenAnotherTurnGoesThrough(t *testing.T) {
	withScriptedAgent(t, scriptUnreachable)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	offline := filepath.Join(t.TempDir(), "offline")
	if err := os.WriteFile(offline, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(offlineFileEnv, offline)

	s := startWakeInAConversation(t, 160, 40)
	s.await("ready")
	s.send("ship it\r")
	s.await("Wake resumes it when the connection is back")

	if err := os.Remove(offline); err != nil {
		t.Fatal(err)
	}
	s.send("\x17") // ⌃W, so the draft reaches the room
	s.await("group chat")
	s.send("/new robin\r")
	s.await("resumed after the connection came back")
}
