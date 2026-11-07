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

// A login's resume that the new account's own limit answers - another reset -
// is a stop like any, under the account signed in now, with no retry; so is the
// failure of a resume no login made, even the same limit again, and a later
// failure once the resumed turn went through or the operator interrupted it.
func TestOnlyAFailureLikeTheOldLoginsIsRetried(t *testing.T) {
	la := losAngeles(t)
	after := time.Date(2026, 11, 6, 14, 44, 0, 0, la)
	noLogin := core.Event{Kind: core.KindAPIError, Notice: core.NoticeAPIError, Text: notLoggedIn}
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
