package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// /reauth parks the sessions a shared-login expiry knocked out, so /resume can
// bring them back on a fresh login without killing the fleet. These tests hold
// the recovery: it parks exactly the marked live sessions, clears the mark it is
// acting on, and guides rather than acts when nothing is marked.

func TestReauthParksAMarkedSession(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.markAuthFailed("s1")

	next, cmd := a.reauth("")

	if _, held := next.authFailed["s1"]; held {
		t.Error("s1 stayed marked authFailed after /reauth handled it")
	}
	if _, parking := next.parking["s1"]; !parking {
		t.Error("s1 was not awaited as parking, so parkArrived cannot confirm it")
	}
	f := sentFrame(t, next, cmd)
	if f.Kind != rpc.FramePark || f.SessionID != "s1" {
		t.Errorf("/reauth wrote %q for %q, want a FramePark for s1", f.Kind, f.SessionID)
	}
}

// A mark can outlive its session (it ended, or another window parked it). reauth
// parks only the ones with a live process; parking a gone one is a refusal the
// daemon logs for nobody.
func TestReauthParksOnlyLiveMarkedSessions(t *testing.T) {
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
	a = a.markAuthFailed("s1").markAuthFailed("gone")

	live := a.authFailedLive()

	if len(live) != 1 || live[0] != "s1" {
		t.Errorf("authFailedLive = %v, want only the live session s1", live)
	}
}

// With nothing marked, /reauth writes no frame and points at the login and
// /resume steps rather than acting.
func TestReauthWithNoMarkedSessionsGuides(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")

	next, cmd := a.reauth("")

	if cmd != nil {
		t.Error("reauth with nothing marked should write no frame")
	}
	n, ok := notice.Latest()
	if !ok || !strings.Contains(n.Text, "auth-error state") {
		t.Fatalf("no guidance notice: %q %v", n.Text, ok)
	}
	_ = next
}

// The router takes /reauth as Wake's own rather than passing it to the agent.
func TestReauthIsAWakeCommand(t *testing.T) {
	a := sizedApp(t, nil, nil, "s1")

	_, _, handled := a.slash("/reauth")

	if !handled {
		t.Error("the router did not take /reauth, so it went to the agent as a message")
	}
}
