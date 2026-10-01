package ui

import (
	"os"
	"path/filepath"
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

// The turn that proves the reset takes the limit's timed notice with its pin, so
// the row does not say "send again once it resets" for the linger after it has.
// A notice reported since is someone else's and stays.
func TestTheTurnAfterTheResetTakesTheLimitsNoticeWithIt(t *testing.T) {
	a := twoAgents(t).apply(usageLimitFrame("s1"))
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.Text, "resets 9:50pm") {
		t.Fatalf("the usage limit reported %q, %v", n.Text, ok)
	}
	a.apply(healthyTurn("s1"))
	if n, ok := notice.Latest(); ok {
		t.Errorf("the turn after the reset left the limit's notice up: %q", n.Text)
	}

	a = twoAgents(t).apply(usageLimitFrame("s1"))
	notice.Report("copied %d chars to clipboard", 5)
	a.apply(healthyTurn("s1"))
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.Text, "copied 5 chars") {
		t.Errorf("the turn after the reset cleared a newer notice: %q, %v", n.Text, ok)
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
	if pin := a.pinnedNotice(); !strings.Contains(pin, apiAwaitTail) {
		t.Errorf("the parked failure's pin should say it wakes itself: %q", pin)
	}
	if n, _ := notice.Latest(); !strings.Contains(n.Text, apiAwaitTail) || strings.Contains(n.Text, resumeVerb) {
		t.Errorf("the confirmed park should say it wakes itself, not send the operator to /resume: %q", n.Text)
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

// Proof has to follow the confirmed park, not only the ask: a turn that lands
// while the park is in flight predates the report the wake answers.
func TestOnlyProofAfterTheConfirmedParkWakesIt(t *testing.T) {
	a := twoAgents(t)
	for range authRetryParkAttempt {
		a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	}
	a, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered()
	if cmd != nil {
		t.Fatal("a session whose park is not confirmed was woken")
	}
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	if _, cmd := a.autoWakeRecovered(); cmd != nil {
		t.Error("a turn from before the confirmed park woke the session")
	}
	if _, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered(); cmd == nil {
		t.Error("a turn after the confirmed park did not wake it")
	}
}

func TestAHandParkedSessionIsNeverAutoWoken(t *testing.T) {
	a := twoAgents(t).awaitingPark("s1")
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	if _, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered(); cmd != nil {
		t.Error("a session the operator parked woke itself")
	}
}

// A session brought back by hand (or by another window), then parked by hand, is
// no longer owed a wake - read off the reports alone, with no settle between.
func TestASessionResumedThenParkedByHandIsNotWoken(t *testing.T) {
	a := reportStates(apiParkedApp(t), map[string]string{"s1": rpc.StateIdle, "s2": rpc.StateIdle})
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

// Claude answers /context or /compact itself with a "<synthetic>" frame and no
// inference, so it works on a dead login: never proof, neither for a parked
// session nor for the one that answered.
func TestAClaudeLocalReplyIsNotProof(t *testing.T) {
	blob, err := os.ReadFile(filepath.Join("..", "..", "testdata", "stream", "slash-commands.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	a := apiParkedApp(t).markAuthFailed("s2")
	replies := 0
	for _, line := range strings.Split(string(blob), "\n") {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			continue
		}
		for _, ev := range evs {
			if ev.Kind == core.KindAssistantText && ev.LocalCommand {
				replies++
				ev.SessionID = "s2"
				a = a.apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &ev})
			}
		}
	}
	if replies == 0 {
		t.Fatal("the fixture holds no local-command reply")
	}
	if _, cmd := a.autoWakeRecovered(); cmd != nil {
		t.Error("a local-command reply woke a session parked for a dead login")
	}
	if _, marked := a.authFailed["s2"]; !marked {
		t.Error("a local-command reply cleared its own session's auth-failed mark")
	}
}

// A live process's token working does not prove a new one can read the login,
// so a session that fails again after a wake on a turn's word waits for /login.
func TestASessionThatFailsAgainAfterAWakeWaitsForLogin(t *testing.T) {
	a := apiParkedApp(t)
	a, _ = a.apply(healthyTurn("s2")).autoWakeRecovered()
	a = reportStates(a, map[string]string{"s1": rpc.StateIdle, "s2": rpc.StateIdle})
	for range authRetryParkAttempt {
		a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	}
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	if _, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered(); cmd != nil {
		t.Fatal("a second turn's word woke a session its first wake did not heal")
	}
	if pin := a.pinnedNotice(); !strings.Contains(pin, apiLoginTail) {
		t.Errorf("the pin should say only /login wakes it now: %q", pin)
	}
	m, cmd := a.Update(authResultMsg{ID: "s1", Text: `{"loggedIn": true}`})
	if _, waking := m.(App).waking["s1"]; !waking || cmd == nil {
		t.Error("a signed-in /login did not wake it")
	}
}

// The daemon reports a park a moment before it takes the park's wake. A refusal
// in that moment keeps the proof and retries once the next report shows the park
// again; a second refusal gives up, so a wake the daemon never takes cannot
// retry on every report.
func TestARefusedAutoWakeRetriesOnceOnTheNextReport(t *testing.T) {
	refuse := func(a App) App {
		return a.apply(rpc.Frame{Kind: rpc.FrameError, SessionID: "s1", Text: "session s1 is not parked, so there is nothing to bring back"})
	}
	parked := map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle}
	a, _ := apiParkedApp(t).apply(healthyTurn("s2")).autoWakeRecovered()
	a = refuse(a)
	if _, waking := a.waking["s1"]; waking {
		t.Error("the refused wake is still awaited, so its arrival notice can never come")
	}
	if _, cmd := a.autoWakeRecovered(); cmd != nil {
		t.Fatal("the refusal itself retried the wake, before any report")
	}
	a, cmd := reportStates(a, parked).autoWakeRecovered()
	if got := kindsFor(sentFrames(t, a, cmd), rpc.FrameWake); len(got) != 1 {
		t.Fatalf("the next parked report did not retry on the proof already seen: %v", got)
	}
	a = refuse(a)
	a = reportStates(a, parked)
	if _, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered(); cmd != nil {
		t.Error("a second refusal did not give up")
	}
	if pin := a.pinnedNotice(); !strings.Contains(pin, resumeVerb) {
		t.Errorf("after giving up the pin should send the operator to /resume: %q", pin)
	}
}

// A turn that goes straight to a tool call is the API answering too: it wakes a
// login-parked session without waiting for prose.
func TestAToolCallIsProof(t *testing.T) {
	tool := core.Event{Kind: core.KindToolUse, SessionID: "s2", Tool: &core.ToolCall{Name: "Bash"}}
	a := apiParkedApp(t).apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &tool})
	if _, cmd := a.autoWakeRecovered(); cmd == nil {
		t.Error("a tool call from another agent did not count as the API answering")
	}
}

// A park write that never landed leaves its wait behind; the operator's own
// park after it replaces the API's, and is never undone.
func TestAHandParkAfterAStaleAutoParkIsNotWoken(t *testing.T) {
	a := twoAgents(t)
	for range authRetryParkAttempt {
		a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	}
	a = reportStates(a, map[string]string{"s1": rpc.StateIdle, "s2": rpc.StateIdle})
	a, _, _ = a.parkTarget("s1", "alex")
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	if _, cmd := a.apply(healthyTurn("s2")).autoWakeRecovered(); cmd != nil {
		t.Error("the operator's park was undone by an auto-wake")
	}
}

// The API only reports a usage limit to a login it knows: an earlier 401's mark
// and count go, so a later 401 does not park one short of the threshold.
func TestAUsageLimitClearsAnEarlierLoginMark(t *testing.T) {
	a := twoAgents(t)
	for range authRetryParkAttempt - 1 {
		a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	}
	a, _ = a.apply(usageLimitFrame("s1")).settle()
	if _, marked := a.authFailed["s1"]; marked {
		t.Error("the usage limit left the 401 mark, so /reauth would park a session whose login works")
	}
	a, _ = a.apply(apiErrorFrame("s1", "Failed to authenticate. API Error: 401")).settle()
	if _, parking := a.parking["s1"]; parking {
		t.Error("a single 401 after the usage limit parked the session on the old count")
	}
}

// A new process does not lift a quota, so a park and a resume leave the limit
// pinned; only a turn that goes through unpins it.
func TestAUsageLimitPinSurvivesAParkAndResume(t *testing.T) {
	a := twoAgents(t).apply(usageLimitFrame("s1"))
	a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
	a = reportStates(a, map[string]string{"s1": rpc.StateIdle, "s2": rpc.StateIdle})
	if pin := a.pinnedNotice(); !strings.Contains(pin, "resets 9:50pm") {
		t.Errorf("a resume unpinned a usage limit it cannot lift: %q", pin)
	}
}
