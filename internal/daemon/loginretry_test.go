package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// resumed stops an agent on a limit naming 6:50pm under acct-a and resumes it the
// way trigger does, so its next event is the resumed turn's.
func resumed(t *testing.T, trigger func(*testing.T, *server, *agent)) *agent {
	t.Helper()
	a := stoppedAgent(t, time.Date(2026, 11, 6, 14, 43, 0, 0, losAngeles(t)))
	trigger(t, fleetOf(t, a), a)
	written(t, a)
	return a
}

// written writes the resume the agent's input holds, as its input goroutine
// would, and ends the turn it starts as the agent's next event will.
func written(t *testing.T, a *agent) {
	t.Helper()
	select {
	case p := <-a.in:
		a.sendResume(*p.resume)
	default:
		t.Fatal("nothing resumed the agent")
	}
	a.observe(core.Event{Kind: core.KindTurnEnd})
}

// byLogin signs in another account and lets the login resume the agent; byReset
// lets its reset pass.
func byLogin(t *testing.T, s *server, _ *agent) {
	signIn(t, os.Getenv("CLAUDE_CONFIG_DIR"), "acct-b", "2")
	s.resumeOnLogin()
}

func byReset(_ *testing.T, s *server, a *agent) { s.resumeDue(a.limit.reset.Add(resetSlack)) }

// byNetwork makes the stop an unreachable API's and lets the network change.
func byNetwork(_ *testing.T, s *server, a *agent) {
	a.mu.Lock()
	a.limit = usageStop{at: a.limit.at, unreachable: true}
	a.mu.Unlock()
	s.resumeUnreachable(a.limit.at.Add(time.Second))
}

// noLogin is the turn a process with no login yet fails (api-error-auth.jsonl's words).
var noLogin = core.Event{Kind: core.KindAPIError, Notice: core.NoticeAPIError, Text: notLoggedIn}

// A login's resume that the new account's own limit answers - another reset -
// is a stop like any, under the account signed in now, with no retry; so is any
// other failure - an overload, an unreachable API - and the failure of a resume
// no login made, even the same limit again. A failure once the resumed turn went
// through, ended, failed otherwise, was interrupted or cleared is its own too.
func TestOnlyAFailureLikeTheOldLoginsIsRetried(t *testing.T) {
	la := losAngeles(t)
	after := time.Date(2026, 11, 6, 14, 44, 0, 0, la)
	overload := core.Event{Kind: core.KindAPIError, Notice: core.NoticeTurnFailed, Text: "API Error: 529 Overloaded"}
	unreachable := core.Event{Kind: core.KindAPIError, Notice: core.NoticeAPIUnreachable, Text: "API Error: Can't reach the API server"}
	cases := []struct {
		name    string
		trigger func(*testing.T, *server, *agent)
		before  func(*agent)
		ev      core.Event
	}{
		{"the new account's own limit", byLogin, nil, limitEvent("9:15pm")},
		{"the reset's resume, limited again", byReset, nil, limitEvent("6:50pm")},
		{"the reset's resume, not logged in", byReset, nil, noLogin},
		{"after the resumed turn went through", byLogin, func(a *agent) {
			a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "back at it"}, after)
		}, noLogin},
		{"after an interrupt", byLogin, (*agent).dropLimit, noLogin},
		{"after a /clear", byLogin, func(a *agent) { a.noteLimit(core.Event{Kind: core.KindSessionReset}, after) }, noLogin},
		{"after the turn ended with neither", byLogin, func(a *agent) { a.noteLimit(core.Event{Kind: core.KindTurnEnd}, after) }, noLogin},
		{"after an overload", byLogin, func(a *agent) { a.noteLimit(overload, after) }, noLogin},
		{"an overload", byLogin, nil, overload},
		{"an unreachable API", byLogin, nil, unreachable},
		{"the unreachable path's resume, not logged in", byNetwork, nil, noLogin},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := resumed(t, tc.trigger)
			if tc.before != nil {
				tc.before(a)
			}
			a.noteLimit(tc.ev, after)
			if !a.limit.retry.IsZero() || a.limit.tries != 0 {
				t.Errorf("armed a retry at %v (try %d), want none", a.limit.retry, a.limit.tries)
			}
			if tc.name == "the new account's own limit" && a.limit.account.ID != "acct-b" {
				t.Errorf("the stop went under %+v, want acct-b, signed in now", a.limit.account)
			}
		})
	}
}

// The old login's limit again is retried after loginRetries' first wait, keeping
// the reset it answered and the account signed in now; a fresh login after the
// retries ran out starts the count again.
func TestTheOldLoginsLimitArmsARetryAndAFreshLoginRestartsTheCount(t *testing.T) {
	la := losAngeles(t)
	after := time.Date(2026, 11, 6, 14, 44, 0, 0, la)
	a := resumed(t, byLogin)
	a.noteLimit(limitEvent("6:50pm"), after)
	want := usageStop{at: after, reset: time.Date(2026, 11, 6, 18, 50, 0, 0, la), retry: after.Add(loginRetries[0]), tries: 1}
	if got := a.limit; !got.retry.Equal(want.retry) || got.tries != 1 || !got.reset.Equal(want.reset) || got.account.ID != "acct-b" {
		t.Fatalf("stop %+v, want a retry at %v, try 1, the reset %v, under acct-b", got, want.retry, want.reset)
	}
	fleetOf(t, a).resumeDue(want.retry)
	written(t, a)
	second := want.retry.Add(time.Second)
	a.noteLimit(limitEvent("6:50pm"), second)
	if got := a.limit; got.tries != 2 || !got.retry.Equal(second.Add(loginRetries[1])) {
		t.Fatalf("after the retry failed: try %d, retry %v; want try 2 at %v", got.tries, got.retry, second.Add(loginRetries[1]))
	}

	a.mu.Lock()
	a.limit.retry, a.limit.tries = time.Time{}, len(loginRetries)+1
	a.mu.Unlock()
	signIn(t, os.Getenv("CLAUDE_CONFIG_DIR"), "acct-c", "3")
	fleetOf(t, a).resumeOnLogin()
	written(t, a)
	a.noteLimit(limitEvent("6:50pm"), after)
	if a.limit.tries != 1 || a.limit.retry.IsZero() {
		t.Errorf("after a fresh login the retry count reads %d, retry %v; want 1 and armed", a.limit.tries, a.limit.retry)
	}
}

// A stop resumes on its own at the sooner of its retry and its reset past the
// slack, and never with neither.
func TestAStopIsDueAtTheSoonerOfItsRetryAndItsReset(t *testing.T) {
	now := time.Now()
	soon, late := now.Add(time.Minute), now.Add(time.Hour)
	cases := []struct {
		name string
		stop usageStop
		want time.Time
	}{
		{"neither", usageStop{at: now}, time.Time{}},
		{"a retry", usageStop{retry: soon}, soon},
		{"a reset", usageStop{reset: late}, late.Add(resetSlack)},
		{"a retry before the reset", usageStop{retry: soon, reset: late}, soon},
		{"a reset before the retry", usageStop{retry: late, reset: soon}, soon.Add(resetSlack)},
	}
	for _, tc := range cases {
		if got := tc.stop.due(); !got.Equal(tc.want) {
			t.Errorf("%s: due %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A turn Wake did not send - the operator's, a peer's - that meets the old login
// while a retry waits leaves the retry standing; another reset is the new
// account's own limit, a stop like any.
func TestATurnWakeDidNotSendLeavesTheRetryStanding(t *testing.T) {
	la := losAngeles(t)
	after := time.Date(2026, 11, 6, 14, 44, 0, 0, la)
	a := resumed(t, byLogin)
	a.noteLimit(limitEvent("6:50pm"), after)
	armed := a.limit
	for _, ev := range []core.Event{limitEvent("6:50pm"), noLogin} {
		if a.noteLimit(ev, after.Add(5*time.Second)); a.limit != armed {
			t.Fatalf("%q from a turn Wake did not send left %+v, want the armed stop %+v", ev.Text, a.limit, armed)
		}
	}
	a.noteLimit(limitEvent("9:15pm"), after.Add(10*time.Second))
	if a.limit.tries != 0 || !a.limit.retry.IsZero() {
		t.Errorf("another reset kept the retry (try %d at %v), want a plain stop", a.limit.tries, a.limit.retry)
	}
}

// A reset already past when the resumed turn fails is not kept: it would resume
// the agent at once, as a reset's resume, which is never retried.
func TestARetriedStopDropsAResetAlreadyPast(t *testing.T) {
	la := losAngeles(t)
	a := resumed(t, byLogin)
	a.noteLimit(noLogin, time.Date(2026, 11, 6, 19, 0, 0, 0, la))
	if !a.limit.reset.IsZero() || a.limit.retry.IsZero() {
		t.Errorf("stop %+v, want a retry and no reset", a.limit)
	}
}

// A stop whose retry and reset have both passed is resumed as the retry it is,
// so a failure of that turn is still the login's.
func TestADueRetryIsResumedAsALoginsEvenPastTheReset(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	a.mu.Lock()
	a.limit.retry, a.limit.tries, a.limit.reset = time.Now().Add(-time.Second), 1, time.Now().Add(-2*resetSlack)
	a.mu.Unlock()
	fleetOf(t, a).resumeDue(time.Now())
	if p := <-a.in; p.resume == nil || !p.resume.login.made || p.resume.login.tries != 1 {
		t.Errorf("queued %+v, want the login's retry 1", p.resume)
	}
}

// The liveness tick wakes the resume goroutine once a stop's due time has
// passed - a retry's own, with no slack - and not before.
func TestTheLivenessTickWakesTheGoroutineAtARetry(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	retry := time.Now().Add(time.Hour)
	a.mu.Lock()
	a.limit.retry, a.limit.tries, a.limit.reset = retry, 1, time.Time{}
	a.mu.Unlock()
	s := fleetOf(t, a)
	t.Cleanup(func() { close(s.quit); s.wg.Wait() })
	running := func() bool { s.limits.mu.Lock(); defer s.limits.mu.Unlock(); return s.limits.running }
	if s.nudgeLimitsDue(retry.Add(-time.Second)); running() {
		t.Fatal("the tick woke the goroutine a second before the retry")
	}
	if s.nudgeLimitsDue(retry); !running() {
		t.Error("the tick left the goroutine asleep at the retry")
	}
}

// The waits outlast what was seen: the first past the ten seconds a process held
// the old login on 2026-11-06, each longer than the last, the last under the six
// minutes after which a hand-sent continue worked.
func TestTheLoginRetriesOutwaitTheHoldsSeen(t *testing.T) {
	if loginRetries[0] <= 10*time.Second {
		t.Errorf("the first retry waits %v, not past the 10s hold seen", loginRetries[0])
	}
	for i := 1; i < len(loginRetries); i++ {
		if loginRetries[i] <= loginRetries[i-1] {
			t.Errorf("retry %d waits %v, no longer than the %v before it", i+1, loginRetries[i], loginRetries[i-1])
		}
	}
	if last := loginRetries[len(loginRetries)-1]; last >= 6*time.Minute {
		t.Errorf("the last retry waits %v, not under the six minutes a hand-sent continue took", last)
	}
}
