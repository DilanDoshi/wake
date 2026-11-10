//go:build unix

// A team's verbs on a real screen: /park @backend, /resume backend, and
// /quit @backend through its confirm, typed into the real binary through a pty
// and checked against the daemon's own report. internal/ui holds the rules in
// process; this is the journey a person takes.

package main

import (
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// awaitTeamStates polls the daemon until each named session reads as want says,
// failing with the screen and the last report.
func awaitTeamStates(t *testing.T, s *screen, want map[string]func(rpc.SessionStatus) bool, what string) {
	t.Helper()
	deadline := time.Now().Add(screenTimeout)
	var last rpc.Status
	for time.Now().Before(deadline) {
		st, err := daemon.Status(os.Getenv("WAKE_SOCKET"))
		if err == nil {
			last = st
			if teamStatesHold(st, want) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s: the daemon reports %+v\n%s", what, last.Sessions, s.dump())
}

func teamStatesHold(st rpc.Status, want map[string]func(rpc.SessionStatus) bool) bool {
	for name, ok := range want {
		at := slices.IndexFunc(st.Sessions, func(ss rpc.SessionStatus) bool { return ss.Name == name })
		row := rpc.SessionStatus{State: rpc.StateEnded} // a session no longer reported is gone
		if at >= 0 {
			row = st.Sessions[at]
		}
		if !ok(row) {
			return false
		}
	}
	return true
}

func isParked(ss rpc.SessionStatus) bool  { return ss.State == rpc.StateParked }
func isRunning(ss rpc.SessionStatus) bool { return ss.State != rpc.StateParked && ss.State != rpc.StateEnded }
func isEnded(ss rpc.SessionStatus) bool   { return ss.State == rpc.StateEnded }

func TestATeamIsParkedResumedAndQuitOnTheRealScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 34)
	teamedFleet(t, s) // w1, w2 on backend; w3 on frontend

	s.send("/park @backend\r")
	s.await("@backend is parked")
	awaitTeamStates(t, s, map[string]func(rpc.SessionStatus) bool{
		"w1": isParked, "w2": isParked, "w3": isRunning,
	}, "after /park @backend")
	t.Logf("SCREENSHOT the backend team parked:\n%s", s.dump())

	s.send("/resume backend\r")
	s.await("bringing @backend back")
	awaitTeamStates(t, s, map[string]func(rpc.SessionStatus) bool{
		"w1": isRunning, "w2": isRunning, "w3": isRunning,
	}, "after /resume backend")
	s.settle()

	// The confirm opens on never mind, so ↵ alone stops nobody.
	s.send("/quit @backend\r")
	s.await("never mind")
	s.await("for good")
	t.Logf("SCREENSHOT the quit confirm:\n%s", s.dump())
	s.send("\r")
	s.awaitGone("never mind")
	s.settle()
	awaitTeamStates(t, s, map[string]func(rpc.SessionStatus) bool{
		"w1": isRunning, "w2": isRunning,
	}, "after ↵ on never mind")

	s.send("/quit @backend\r")
	s.await("never mind")
	s.send("\x1b[B") // ↓ onto the quit row
	s.settle()
	s.send("\r")
	awaitTeamStates(t, s, map[string]func(rpc.SessionStatus) bool{
		"w1": isEnded, "w2": isEnded, "w3": isRunning,
	}, "after ↓ ↵ on the quit confirm")
	// The room's history still names w1 and w2, so the roster's own rows are read.
	quit := func() bool {
		names := s.rosterNames()
		return !slices.Contains(names, "w1") && !slices.Contains(names, "w2") && slices.Contains(names, "w3")
	}
	for deadline := time.Now().Add(screenTimeout); !quit() && time.Now().Before(deadline); {
		s.settle()
	}
	if !quit() {
		t.Errorf("the roster draws %v after the team quit, want w3 and neither w1 nor w2\n%s", s.rosterNames(), s.dump())
	}
	if strings.Contains(s.text(), "never mind") {
		t.Errorf("the confirm is still drawn after quitting\n%s", s.dump())
	}
}
