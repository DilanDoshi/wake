package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// limitEvent is the failed turn a usage limit ends, naming its reset.
func limitEvent(reset string) core.Event {
	return core.Event{Kind: core.KindAPIError, Notice: core.NoticeUsageLimit,
		Text: "You've hit your session limit · resets " + reset + " (America/Los_Angeles)"}
}

// stoppedAgent is an idle agent a limit stopped at hit, signed in as acct-a.
func stoppedAgent(t *testing.T, hit time.Time) *agent {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	signIn(t, dir, "acct-a", "1")
	a := effortAgent(t)
	if !a.noteLimit(limitEvent("6:50pm"), hit) {
		t.Fatal("a usage limit recorded no stop")
	}
	return a
}

func losAngeles(t *testing.T) *time.Location {
	t.Helper()
	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}
	return la
}

// A limit records when it hit, the reset it named and who was signed in; a
// reset that is not after the hit is stale and arms nothing, since it would
// fire at once and stop again. Model output clears the stop; a local command's
// reply, which ran no inference, and a subagent's own limit do not touch it.
func TestALimitRecordsItsStopAndModelOutputClearsIt(t *testing.T) {
	la := losAngeles(t)
	hit := time.Date(2026, 10, 17, 14, 43, 25, 0, la)
	a := stoppedAgent(t, hit)
	if want := time.Date(2026, 10, 17, 18, 50, 0, 0, la); !a.limit.reset.Equal(want) || a.limit.account.ID != "acct-a" {
		t.Fatalf("stop %+v, want a reset at %v under acct-a", a.limit, want)
	}

	a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "Set model to Opus", LocalCommand: true}, hit)
	if !a.usageStopped() {
		t.Error("a local command's reply cleared the stop")
	}
	sub := limitEvent("6:50pm")
	sub.Subagent = &core.Subagent{}
	if a.noteLimit(sub, hit) {
		t.Error("a subagent's limit recorded a stop of the agent's own")
	}
	a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "back at it"}, hit)
	if a.usageStopped() {
		t.Error("model output left the stop")
	}

	if !a.noteLimit(limitEvent("2:43pm"), hit) || !a.limit.reset.IsZero() {
		t.Errorf("a reset in the hit's own minute armed %v, want no timer", a.limit.reset)
	}
}

// resumeCounter is a resume for the stop that hit at, counting announcements.
func resumeCounter(at time.Time, ids *[]string) resumeAsk {
	return resumeAsk{at: at, announce: func(id string) { *ids = append(*ids, id) }}
}

// stopAt is when this agent's stop hit.
func stopAt(a *agent) time.Time {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.limit.at
}

// A send never clears the stop: its turn is in flight, so a resume behind it
// waits, and a local command - /model, /context - runs no turn at all, so the
// resume still comes after it.
func TestATurnInFlightHoldsTheResumeAndALocalCommandLeavesTheStop(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	at := stopAt(a)
	a.noteSent()
	var ids []string
	a.sendResume(resumeCounter(at, &ids))
	if len(ids) != 0 || !a.usageStopped() {
		t.Fatalf("a resume behind a send in flight: announced %d, stopped %v; want 0, true", len(ids), a.usageStopped())
	}
	a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "Set model to Opus", LocalCommand: true}, time.Now())
	a.observe(core.Event{Kind: core.KindTurnEnd})
	a.sendResume(resumeCounter(at, &ids))
	if len(ids) != 1 {
		t.Errorf("after a local command's turn the resume announced %d times, want 1", len(ids))
	}
}

// A subagent's output is not the agent's own turn going through - its request
// may have begun before the limit - and a /clear abandons the conversation the
// resume would have continued.
func TestOnlyTheAgentsOwnOutputOrAClearEndsTheStop(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "subagent words", Subagent: &core.Subagent{}}, time.Now())
	if !a.usageStopped() {
		t.Error("a subagent's output cleared the agent's stop")
	}
	a.noteLimit(core.Event{Kind: core.KindSessionReset}, time.Now())
	if a.usageStopped() {
		t.Error("a /clear left a stop that would resume a conversation nobody has any more")
	}
}

// A resume answers the stop it was queued for: one that finds a newer stop -
// the turn ahead of it hit the limit again - writes nothing.
func TestAQueuedResumeNeverAnswersANewerStop(t *testing.T) {
	first := time.Now().Add(-time.Minute)
	a := stoppedAgent(t, first)
	a.noteLimit(limitEvent("6:50pm"), time.Now())
	var ids []string
	a.sendResume(resumeCounter(first, &ids))
	if len(ids) != 0 || !a.usageStopped() {
		t.Errorf("a resume for an older stop: announced %d, stopped %v; want 0, true", len(ids), a.usageStopped())
	}
}

// An agent busy when its resume comes up declines it, and stays stopped with
// nothing queued, so a later trigger can resume it.
func TestABusyAgentDeclinesTheResumeAndCanTakeALaterOne(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	a.mu.Lock()
	a.owed, a.limit.queued = true, true
	a.mu.Unlock()
	var ids []string
	a.sendResume(resumeCounter(stopAt(a), &ids))
	if len(ids) != 0 || !a.usageStopped() || a.limit.queued {
		t.Errorf("a busy agent: announced %d, stopped %v, queued %v; want 0, true, false", len(ids), a.usageStopped(), a.limit.queued)
	}
}

// A ready agent is resumed once, under a uuid carrying the resume mark, and a
// second trigger finds nothing left to resume.
func TestAReadyAgentIsResumedOnceUnderTheResumeMark(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	at := stopAt(a)
	var ids []string
	a.sendResume(resumeCounter(at, &ids))
	a.sendResume(resumeCounter(at, &ids))
	if len(ids) != 1 {
		t.Fatalf("announced %d resumes, want 1", len(ids))
	}
	if _, flags, ok := rpc.MessageOf(ids[0]); !ok || flags&rpc.MessageResumed == 0 {
		t.Errorf("the resume went out as %q, which carries no resume mark", ids[0])
	}
	if a.usageStopped() {
		t.Error("a resumed agent still reads stopped")
	}
}

// queuedResumes drains an agent's input and counts the resumes in it.
func queuedResumes(a *agent) int {
	n := 0
	for {
		select {
		case p := <-a.in:
			if p.resume != nil {
				n++
			}
		default:
			return n
		}
	}
}

// fleetOf is a server holding these agents under their ids.
func fleetOf(t *testing.T, agents ...*agent) *server {
	t.Helper()
	s := newServer(tempSocket(t))
	for _, a := range agents {
		s.agents[a.id] = a
	}
	return s
}

// A login resumes the agents stopped under another account and no other; one
// stopped before the account could be read takes the one signed in now.
func TestALoginResumesOnlyAgentsStoppedUnderAnotherAccount(t *testing.T) {
	other := stoppedAgent(t, time.Now())
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	same, unknown := effortAgent(t), effortAgent(t)
	same.id, unknown.id = testSessionID("f1f1"), testSessionID("f2f2")
	signIn(t, dir, "acct-b", "2")
	same.noteLimit(limitEvent("6:50pm"), time.Now())
	unknown.noteLimit(limitEvent("6:50pm"), time.Now())
	unknown.limit.account = core.Account{}
	s := fleetOf(t, other, same, unknown)

	s.resumeOnLogin()
	if n := queuedResumes(other); n != 1 {
		t.Errorf("the agent stopped under acct-a got %d resumes after a login to acct-b, want 1", n)
	}
	if n := queuedResumes(same) + queuedResumes(unknown); n != 0 {
		t.Errorf("%d resumes went to agents stopped under the account signed in now, want 0", n)
	}
	if unknown.limit.account.ID != "acct-b" {
		t.Errorf("an agent stopped with no account read took %+v, want acct-b", unknown.limit.account)
	}
}

// Nothing resumes before the reset plus its slack, and everything due does after.
func TestNoResumeBeforeTheResetAndItsSlack(t *testing.T) {
	la := losAngeles(t)
	a := stoppedAgent(t, time.Date(2026, 10, 17, 14, 43, 25, 0, la))
	s := fleetOf(t, a)
	due := a.limit.reset.Add(resetSlack)
	s.resumeDue(due.Add(-time.Second))
	if n := queuedResumes(a); n != 0 {
		t.Fatalf("%d resumes a second before the reset's slack ran out, want 0", n)
	}
	a.queueResume(a.limit.at, false)
	s.resumeDue(due)
	if n := queuedResumes(a); n != 1 {
		t.Errorf("%d resumes once the reset and its slack had passed, want 1", n)
	}
}

// A full input queue leaves the resume queued, so a reset already past cannot
// spin the goroutine retrying it; a send of the operator's ends the wait.
func TestAFullInputQueueLeavesTheResumeQueued(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	for len(a.in) < cap(a.in) {
		a.in <- pending{}
	}
	s := fleetOf(t, a)
	s.resume(a, a.limit.at, "a test")
	if !a.limit.queued {
		t.Error("a resume that found the input full is ready again, so a past reset retries at once")
	}
	if due, _ := s.nextReset(); !due.IsZero() {
		t.Errorf("an agent whose resume could not be queued still arms the timer at %v", due)
	}
}

// The resume goroutine ends once nothing is stopped and starts again on the
// next stop; the liveness tick starts it for a reset slept through.
func TestTheResumeGoroutineRunsOnlyWhileAnAgentIsStopped(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	s := fleetOf(t, a)
	running := func() bool { s.limits.mu.Lock(); defer s.limits.mu.Unlock(); return s.limits.running }
	awaitRunning := func(want bool, why string) {
		t.Helper()
		for deadline := time.Now().Add(testTimeout); running() != want; time.Sleep(10 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("the resume goroutine running = %v %s, want %v", !want, why, want)
			}
		}
	}
	t.Cleanup(func() { close(s.quit); s.wg.Wait() })

	s.limitHit()
	awaitRunning(true, "with an agent stopped")
	a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "back"}, time.Now())
	s.limitHit()
	awaitRunning(false, "after the stop cleared")

	a.noteLimit(limitEvent("6:50pm"), time.Now())
	a.mu.Lock()
	a.limit.reset = time.Now().Add(-2 * resetSlack)
	a.mu.Unlock()
	s.nudgeLimitsDue(time.Now())
	awaitRunning(true, "after the liveness tick found a reset slept through")
}

// Only a live agent counts as stopped: a parked or ended one has no process to
// resume and would keep the resume goroutine running for nothing. A busy one
// counts, and is not ready.
func TestOnlyALiveAgentCountsAsStopped(t *testing.T) {
	s := newServer(tempSocket(t))
	now := time.Now()
	ids := map[string]func(*agent){
		testSessionID("e1e1"): func(*agent) {},
		testSessionID("e2e2"): func(a *agent) { a.ended, a.parked = true, true },
		testSessionID("e3e3"): func(a *agent) { a.parking = true },
		testSessionID("e4e4"): func(a *agent) { a.owed = true },
	}
	for id, shape := range ids {
		a := stoppedAgent(t, now)
		a.id = id
		shape(a)
		s.agents[id] = a
	}
	held := map[string]bool{}
	for a, stop := range s.limited() {
		held[a.id] = stop.ready
	}
	want := map[string]bool{testSessionID("e1e1"): true, testSessionID("e4e4"): false}
	if len(held) != len(want) || held[testSessionID("e1e1")] != true || held[testSessionID("e4e4")] != false {
		t.Errorf("stopped agents (id → ready) %v, want %v", held, want)
	}
}

// The daemon's "continue" comes back off disk under the uuid it stamped, so a
// reopened conversation or room draws it as Wake's - and an operator who typed
// "continue" themselves still reads as the operator. The line is the recorded
// room-stamped one (testdata/transcript/room-stamped-uuid.jsonl), re-stamped.
func TestARestoredResumeIsMarkedAsWakes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "transcript", "room-stamped-uuid.jsonl"))
	if err != nil {
		t.Fatalf("read the fixture: %v", err)
	}
	stamped := func(id string, parent any) string {
		var rec map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec); err != nil {
			t.Fatal(err)
		}
		rec["uuid"] = id
		rec["parentUuid"] = parent
		rec["message"] = map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": resumeText}}}
		out, _ := json.Marshal(rec)
		return string(out)
	}
	resumed := rpc.NewMessageSend(rpc.MessageResumed).ID()
	plantTranscript(t, histID, stamped(resumed, nil), stamped(uuid.NewString(), resumed))
	events, err := History(histID)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	var notices []core.Notice
	for _, ev := range events {
		if ev.Kind == core.KindUserText {
			notices = append(notices, ev.Notice)
		}
	}
	if want := []core.Notice{core.NoticeAutoResumed, ""}; !slices.Equal(notices, want) {
		t.Errorf("restored the two \"continue\"s with notices %q, want %q", notices, want)
	}
}
