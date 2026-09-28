package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A usage limit recovers on its own when the quota resets, and a dead login
// recovers the moment a fresh process can read a working one. These tests hold
// both: a limit never parks or asks for /reauth, and a session parked for a
// failing API wakes itself on proof the API answers again.

// usageLimitFrame is one KindAPIError the airlock decoded as a usage limit.
func usageLimitFrame(sessionID string) rpc.Frame {
	return rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: sessionID,
		Event: &core.Event{Kind: core.KindAPIError, SessionID: sessionID, Notice: core.NoticeUsageLimit,
			Text: "You've hit your session limit · resets 9:50pm (America/Los_Angeles)"},
	}
}

// healthyTurn is a real model turn on the stream.
func healthyTurn(sessionID string) rpc.Frame {
	return rpc.Frame{
		Kind:      rpc.FrameEvent,
		SessionID: sessionID,
		Event:     &core.Event{Kind: core.KindAssistantText, SessionID: sessionID, Text: "back"},
	}
}

// twoAgents is alex (s1) and bea (s2), both live, on a recorder.
func twoAgents(t *testing.T) App {
	t.Helper()
	fresh(t)
	return dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex", "bea").withSize(200, 40)
}

// reportStates folds a fleet report with each named session in the state given.
func reportStates(a App, states map[string]string) App {
	st := rpc.Status{Running: true}
	for _, id := range []string{"s1", "s2"} {
		if state, ok := states[id]; ok {
			st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: id, Name: map[string]string{"s1": "alex", "s2": "bea"}[id], State: state})
		}
	}
	return a.applyStatus(&st)
}

func TestAUsageLimitNeitherMarksForReauthNorParks(t *testing.T) {
	a := twoAgents(t)
	for range authRetryParkAttempt + 1 {
		a, _ = a.apply(usageLimitFrame("s1")).settle()
	}
	if _, marked := a.authFailed["s1"]; marked {
		t.Error("a usage limit marked the session for /reauth; a restart cannot lift a quota")
	}
	if _, parking := a.parking["s1"]; parking {
		t.Error("a usage limit auto-parked the session; the next message after the reset should just work")
	}
	if n, _ := notice.Latest(); strings.Contains(n.Text, reauthVerb) {
		t.Errorf("the usage-limit notice sends the operator to /reauth: %q", n.Text)
	}
	pin := a.pinnedNotice()
	if !strings.Contains(pin, "resets 9:50pm") || strings.Contains(pin, reauthVerb) || strings.Contains(pin, resumeVerb) {
		t.Errorf("the usage-limit pin should name the reset and no recovery command: %q", pin)
	}
}

func TestAUsageLimitUnpinsOnTheFirstTurnAfterTheReset(t *testing.T) {
	a := twoAgents(t).apply(usageLimitFrame("s1"))
	if a.pinnedNotice() == "" {
		t.Fatal("the usage limit pinned nothing")
	}
	if pin := a.apply(healthyTurn("s1")).pinnedNotice(); pin != "" {
		t.Errorf("the turn after the reset left the limit pinned: %q", pin)
	}
}

// apiParkedApp is alex auto-parked after authRetryParkAttempt 401s, the park
// confirmed by a report, and bea still live.
func apiParkedApp(t *testing.T) App {
	t.Helper()
	a := twoAgents(t)
	for range authRetryParkAttempt {
		a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	}
	if _, parking := a.parking["s1"]; !parking {
		t.Fatal("alex was not auto-parked at the threshold")
	}
	return reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
}

func TestAnAutoParkedSessionWakesOnAnotherAgentsHealthyTurn(t *testing.T) {
	a := apiParkedApp(t)
	if pin := a.pinnedNotice(); !strings.Contains(pin, "/login") {
		t.Errorf("the parked failure's pin should say the login wakes it: %q", pin)
	}
	a, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered()
	if got := kindsFor(sentFrames(t, a, cmd), rpc.FrameWake); len(got) != 1 || got[0] != "s1" {
		t.Fatalf("bea's healthy turn woke %v, want [s1]", got)
	}
	if _, waking := a.waking["s1"]; !waking {
		t.Error("the auto-wake was not awaited, so no 'has been resumed' notice follows")
	}
	if _, cmd := a.autoWakeRecovered(); cmd != nil {
		t.Error("a second settle woke alex again")
	}
}

// The wake is derived after the fold, like the park, so a healthy turn on the
// stream reaches it without a test calling autoWakeRecovered.
func TestTheAutoWakeIsDerivedAfterTheFold(t *testing.T) {
	a := apiParkedApp(t)
	m, _ := a.Update(frameMsg{Frame: healthyTurn("s2")})
	if _, waking := m.(App).waking["s1"]; !waking {
		t.Error("a healthy turn on the stream did not wake the auto-parked session")
	}
}

// Proof has to come after the park: without that, a session that keeps failing
// while another agent works would be parked and woken in a loop.
func TestNoProofSinceTheParkWakesNothing(t *testing.T) {
	a := twoAgents(t).apply(healthyTurn("s2"))
	for range authRetryParkAttempt {
		a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	}
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	if _, cmd := a.autoWakeRecovered(); cmd != nil {
		t.Error("a turn from before the park woke the session")
	}
}

// Proof that arrives while the park is still in flight is kept, not spent: the
// session wakes once the park lands.
func TestProofDuringTheParkWakesItOnceTheParkLands(t *testing.T) {
	a := twoAgents(t)
	for range authRetryParkAttempt {
		a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	}
	a, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered()
	if cmd != nil {
		t.Fatal("a session whose park is not confirmed was woken")
	}
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	if _, cmd := a.autoWakeRecovered(); cmd == nil {
		t.Error("the proof that came during the park was lost")
	}
}

func TestAHandParkedSessionIsNeverAutoWoken(t *testing.T) {
	a := twoAgents(t).awaitingPark("s1")
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	if _, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered(); cmd != nil {
		t.Error("a session the operator parked woke itself")
	}
}

// A session brought back by hand (or by another window) is no longer owed a wake.
func TestAResumedSessionIsForgotten(t *testing.T) {
	a := reportStates(apiParkedApp(t), map[string]string{"s1": rpc.StateIdle, "s2": rpc.StateIdle})
	a, _ = a.apply(healthyTurn("s2")).autoWakeRecovered()
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	if _, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered(); cmd != nil {
		t.Error("a later park woke a session whose API park was already over")
	}
}

func TestReauthParkedSessionsWakeOnceTheLoginWorks(t *testing.T) {
	a := twoAgents(t).markAuthFailed("s1")
	a, _ = a.reauth("")
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	a, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered()
	if got := kindsFor(sentFrames(t, a, cmd), rpc.FrameWake); len(got) != 1 || got[0] != "s1" {
		t.Errorf("a /reauth-parked session did not wake on a healthy turn: %v", got)
	}
}

// With every agent parked nothing can prove the login, so /login is the proof:
// signed in wakes them, signed out does not.
func TestASignedInLoginCheckWakesTheParkedSessions(t *testing.T) {
	for _, tc := range []struct {
		out  string
		wake bool
	}{
		{`{"loggedIn": true, "authMethod": "claude.ai"}`, true},
		{`{"loggedIn": false}`, false},
		{"command not found", false},
	} {
		a := apiParkedApp(t)
		m, cmd := a.Update(authResultMsg{ID: "s1", Text: tc.out})
		if _, waking := m.(App).waking["s1"]; waking != tc.wake || (cmd != nil) != tc.wake {
			t.Errorf("/login answered %q: woke = %v, want %v", tc.out, waking, tc.wake)
		}
	}
}

// A /resume already on its way is the wake; a second would be refused as
// "not parked" once the first lands.
func TestASessionAlreadyAskedToWakeIsNotWokenTwice(t *testing.T) {
	a := apiParkedApp(t).awaitingWake("s1")
	if _, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered(); cmd != nil {
		t.Error("the auto-wake repeated a wake this window had already asked for")
	}
}
