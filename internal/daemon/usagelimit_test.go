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
	if !a.limited() {
		t.Error("a local command's reply cleared the stop")
	}
	sub := limitEvent("6:50pm")
	sub.Subagent = &core.Subagent{}
	if a.noteLimit(sub, hit) {
		t.Error("a subagent's limit recorded a stop of the agent's own")
	}
	a.noteLimit(core.Event{Kind: core.KindAssistantText, Text: "back at it"}, hit)
	if a.limited() {
		t.Error("model output left the stop")
	}

	if !a.noteLimit(limitEvent("2:43pm"), hit) || !a.limit.reset.IsZero() {
		t.Errorf("a reset in the hit's own minute armed %v, want no timer", a.limit.reset)
	}
}

// resumeCounter is a pending resume that counts its announcements.
func resumeCounter(ids *[]string) pending {
	return pending{frame: rpc.Frame{Kind: rpc.FrameSend, Text: resumeText}, resumed: func(id string) { *ids = append(*ids, id) }}
}

// A send of anyone's - the operator's, another window's, the manager's - is
// the resume, so one queued behind it writes nothing.
func TestASendAheadOfTheResumeCancelsIt(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	a.noteSent()
	var ids []string
	a.sendResume(resumeCounter(&ids))
	if len(ids) != 0 || a.limited() {
		t.Errorf("after a send, the resume announced %d times and the agent reads stopped %v", len(ids), a.limited())
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
	a.sendResume(resumeCounter(&ids))
	if len(ids) != 0 || !a.limited() || a.limit.queued {
		t.Errorf("a busy agent: announced %d, stopped %v, queued %v; want 0, true, false", len(ids), a.limited(), a.limit.queued)
	}
}

// A ready agent is resumed once, under a uuid carrying the resume mark, and a
// second trigger finds nothing left to resume.
func TestAReadyAgentIsResumedOnceUnderTheResumeMark(t *testing.T) {
	a := stoppedAgent(t, time.Now())
	var ids []string
	a.sendResume(resumeCounter(&ids))
	a.sendResume(resumeCounter(&ids))
	if len(ids) != 1 {
		t.Fatalf("announced %d resumes, want 1", len(ids))
	}
	if _, flags, ok := rpc.MessageOf(ids[0]); !ok || flags&rpc.MessageResumed == 0 {
		t.Errorf("the resume went out as %q, which carries no resume mark", ids[0])
	}
	if a.limited() {
		t.Error("a resumed agent still reads stopped")
	}
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
