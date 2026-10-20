//go:build unix

// Bare `wake` on a real screen: what the front door actually draws.
//
// openroom_test.go asserts on the frames the daemon received. It cannot see
// which pane has the terminal or what the roster holds once the alt screen is
// up, because it arranges never to reach the Bubble Tea program.

package main

import (
	"os"
	"slices"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/daemon"
)

// A fresh fleet opens the room with the manager and nobody else (owner's
// 2026-10-16 ruling): no pooled agent beside it, and no pane but the room.
//
// Above dmTakeoverColumns, so a second pane would be drawn rather than hidden.
func TestBareWakeOpensTheRoomWithOnlyTheManager(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 200, 40)
	s.await("group chat")
	// Waited for, so the assertions below are made over a fleet that has
	// started what it is going to start rather than over the first frame.
	for deadline := time.Now().Add(screenTimeout); !slices.ContainsFunc(s.rosterNames(), core.IsManager); {
		if time.Now().After(deadline) {
			t.Fatalf("bare `wake` drew no manager on the roster.\n%s", s.dump())
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.settle()

	if got := paneTitles(s); len(got) != 1 || got[0] != "group chat" {
		t.Fatalf("bare `wake` drew panes %v, want the room alone.\n%s", got, s.dump())
	}
	if names := agentsOnRoster(s); len(names) != 0 {
		t.Fatalf("bare `wake` started %v beside the manager, want nobody.\n%s", names, s.dump())
	}
	// The daemon's word as well as the screen's: a row the roster had not drawn
	// yet would pass the check above.
	st, err := daemon.Status(os.Getenv("WAKE_SOCKET"))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(st.Sessions) != 1 || !core.IsManager(st.Sessions[0].Name) {
		t.Fatalf("the daemon holds %+v, want the manager alone", st.Sessions)
	}
}
