//go:build unix

// /wake-name on a real screen, against a named fleet under a scratch HOME: the
// room says the new name, and the machine lists the fleet by it.

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
)

func TestWakeNameRenamesTheFleetOnARealScreen(t *testing.T) {
	withScriptedAgent(t, "")
	home, err := os.MkdirTemp("", "wakeh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	// A scratch socket where a fleet called atlas keeps its own, so the daemon
	// reads it as that fleet; an agent's WAKE_SOCKET still names it afterwards.
	old := filepath.Join(home, ".wake", "fleets", "atlas", "daemon.sock")
	t.Setenv("WAKE_SOCKET", old)

	s := startWake(t, 200, 40)
	s.await("group chat")
	s.send("/wake-name dev\r")
	s.await("fleet atlas is now dev")

	if names, err := daemon.Fleets(); err != nil || !slices.Equal(names, []string{"dev"}) {
		t.Errorf("`wake fleets` lists %v (%v), want only dev", names, err)
	}
	st, err := daemon.Status(old)
	if err != nil || st.Fleet != "dev" {
		t.Errorf("the old socket reached a daemon calling itself %q (%v), want dev", st.Fleet, err)
	}
}
