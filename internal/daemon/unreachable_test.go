package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// unreachableEvent is the failed turn an API that never answered ends
// (testdata/stream/api-error-unreachable.jsonl).
func unreachableEvent() core.Event {
	return core.Event{Kind: core.KindAPIError, Notice: core.NoticeAPIUnreachable,
		Text: "API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)"}
}

// unreachableAgent is an idle agent an unreachable API stopped at hit.
func unreachableAgent(t *testing.T, hit time.Time) *agent {
	t.Helper()
	a := effortAgent(t)
	if !a.noteLimit(unreachableEvent(), hit) {
		t.Fatal("an unreachable API recorded no stop")
	}
	return a
}

// An unreachable API stops the agent with no reset to wait for, and the agent's
// own output ends the stop as it ends a usage limit's.
func TestAnUnreachableAPIRecordsAStopItsOwnOutputClears(t *testing.T) {
	a := unreachableAgent(t, time.Now())
	if !a.limit.unreachable || !a.limit.reset.IsZero() || a.limit.netOnly {
		t.Fatalf("stop %+v, want an unreachable one with no reset, open to any proof", a.limit)
	}
	a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "back at it"}, time.Now())
	if a.usageStopped() {
		t.Error("model output left the stop")
	}
}

// resumeWith runs one pass of the resume goroutine with the API last answering
// at answered and the network last changing at changed.
func resumeWith(s *server, answered, changed time.Time) {
	s.limits.mu.Lock()
	s.limits.answered = answered
	s.limits.mu.Unlock()
	s.resumeUnreachable(changed)
}

// nudged reports whether the resume goroutine has a wake waiting, taking it.
func nudged(s *server) bool {
	select {
	case <-s.limits.nudge:
		return true
	default:
		return false
	}
}

// Another agent's turn going through after the stop resumes it; one that went
// through before the stop proves nothing about the connection since.
func TestAnAnswerAfterTheStopResumesIt(t *testing.T) {
	hit := time.Now()
	a := unreachableAgent(t, hit)
	s := fleetOf(t, a)
	resumeWith(s, hit.Add(-time.Second), time.Time{})
	if n := queuedResumes(a); n != 0 {
		t.Fatalf("%d resumes on an answer from before the stop, want 0", n)
	}
	resumeWith(s, hit.Add(time.Second), time.Time{})
	if n := queuedResumes(a); n != 1 {
		t.Errorf("%d resumes on an answer after the stop, want 1", n)
	}
}

// The network coming back after the stop resumes it; a change from before it
// does not.
func TestANetworkChangeAfterTheStopResumesIt(t *testing.T) {
	hit := time.Now()
	a := unreachableAgent(t, hit)
	s := fleetOf(t, a)
	resumeWith(s, time.Time{}, hit.Add(-time.Second))
	if n := queuedResumes(a); n != 0 {
		t.Fatalf("%d resumes on a network change from before the stop, want 0", n)
	}
	resumeWith(s, time.Time{}, hit.Add(time.Second))
	if n := queuedResumes(a); n != 1 {
		t.Errorf("%d resumes on a network change after the stop, want 1", n)
	}
}

// An agent stopped again by its own resume, with no output of its own between,
// waits for the network: another agent's turns would otherwise resume it after
// every one of them. Output of its own reopens it to any proof.
func TestAStopRightAfterItsResumeWaitsForTheNetwork(t *testing.T) {
	a := unreachableAgent(t, time.Now())
	var ids []string
	a.sendResume(resumeCounter(stopAt(a), &ids))
	a.observe(core.Event{Kind: core.KindTurnEnd})
	hit := time.Now()
	a.noteLimit(unreachableEvent(), hit)
	if !a.limit.netOnly {
		t.Fatalf("a stop right after a resume: %+v, want it waiting on the network alone", a.limit)
	}
	s := fleetOf(t, a)
	resumeWith(s, hit.Add(time.Second), time.Time{})
	if n := queuedResumes(a); n != 0 {
		t.Fatalf("%d resumes on another agent's answer, want 0", n)
	}
	resumeWith(s, time.Time{}, hit.Add(time.Second))
	if n := queuedResumes(a); n != 1 {
		t.Errorf("%d resumes on the network coming back, want 1", n)
	}

	a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "back"}, time.Now())
	a.noteLimit(unreachableEvent(), time.Now())
	if a.limit.netOnly {
		t.Error("after output of its own, a new stop still waits on the network alone")
	}
}

// A login or a reset proves nothing about the connection, and a usage limit is
// not resumed by the network or another agent's turn.
func TestEachStopIsResumedOnlyByItsOwnProof(t *testing.T) {
	cut := unreachableAgent(t, time.Now())
	limited := stoppedAgent(t, time.Now())
	limited.id = testSessionID("a8a8")
	s := fleetOf(t, cut, limited)

	for _, account := range []string{"acct-b", "acct-c"} {
		signIn(t, os.Getenv("CLAUDE_CONFIG_DIR"), account, account)
		s.resumeOnLogin()
	}
	s.resumeDue(time.Now().Add(48 * time.Hour))
	if n := queuedResumes(cut); n != 0 {
		t.Errorf("%d resumes of an unreachable stop on a login and a reset, want 0", n)
	}
	queuedResumes(limited)
	limited.queueResume(stopAt(limited), false)

	later := time.Now().Add(time.Second)
	resumeWith(s, later, later)
	if n := queuedResumes(limited); n != 0 {
		t.Errorf("%d resumes of a usage limit on an answer and a network change, want 0", n)
	}
}

// The resume of an unreachable stop carries the reconnect mark beside the
// resume mark, so a restore draws why it came; a usage limit's carries none.
func TestAReconnectResumeCarriesItsMark(t *testing.T) {
	for _, tc := range []struct {
		name  string
		agent func(*testing.T, time.Time) *agent
		want  bool
	}{
		{"unreachable", unreachableAgent, true},
		{"usage limit", stoppedAgent, false},
	} {
		a := tc.agent(t, time.Now())
		var ids []string
		a.sendResume(resumeCounter(stopAt(a), &ids))
		if len(ids) != 1 {
			t.Fatalf("%s: announced %d resumes, want 1", tc.name, len(ids))
		}
		_, flags, ok := rpc.MessageOf(ids[0])
		if !ok || flags&rpc.MessageResumed == 0 || (flags&rpc.MessageReconnected != 0) != tc.want {
			t.Errorf("%s: the resume's flags %08b, want resumed with reconnect %v", tc.name, flags, tc.want)
		}
	}
}

// Every turn the API accepts is recorded as proof, the newest kept, and it wakes
// the goroutine only while it runs and an answer could resume somebody.
func TestAnAnswerIsRecordedAndWakesTheGoroutineOnlyWhenItCould(t *testing.T) {
	s := newServer(tempSocket(t))
	at := time.Now()
	s.apiAnswered(at)
	s.apiAnswered(at.Add(-time.Second))
	if s.limits.answered != at || nudged(s) {
		t.Fatalf("answered %v, nudged %v; want the newest answer, and no wake with nothing running", s.limits.answered, !nudged(s))
	}
	s.limits.running = true
	s.apiAnswered(at)
	if nudged(s) {
		t.Error("an answer woke the goroutine with nothing an answer could resume")
	}
	s.limits.unanswered = true
	s.apiAnswered(at)
	if !nudged(s) {
		t.Error("an answer did not wake the goroutine an agent is waiting in")
	}
}

// An answer that lands while a pass is running, after the pass read the last
// one, is not lost: the pass wakes the goroutine again to read it.
func TestAnAnswerDuringAPassWakesTheGoroutineAgain(t *testing.T) {
	s := newServer(tempSocket(t))
	seen := time.Now()
	s.limits.answered = seen.Add(time.Second) // landed after the pass read seen
	s.armAnswers(seen, true)
	if !nudged(s) {
		t.Error("an answer that landed during the pass woke nothing")
	}
	s.armAnswers(seen, false)
	if nudged(s) {
		t.Error("a pass that left nobody waiting woke the goroutine again")
	}
	s.armAnswers(s.limits.answered, true)
	if nudged(s) {
		t.Error("a pass that saw the newest answer woke the goroutine again")
	}
}
