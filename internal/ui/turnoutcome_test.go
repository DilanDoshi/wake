package ui

// A turn the API failed is not done. The failure reaches the UI only as a
// core.KindAPIError, which observe routes to the notice row before the fleet's
// fold, and the result behind it reads idle like any turn's - so the ✔, the DM's
// `✻ … done` line and the strip's `N done` drew a finished turn over a failed one
// (bugs.md BUG-35, the done-line residual). All three read turnDone, which reads
// the agent's failed bit.
//
// The failures are recordings: api-error-auth.jsonl (a dead login),
// api-retry-auth.jsonl (two 401 retries, then the give-up) and
// api-error-unreachable.jsonl (ten retries, then server_error). No stream
// recording of a usage limit exists (deferred.md), so it and a rejected request
// are the recorded auth frame with the "error" kind swapped, as
// core/usagelimit_test.go builds them.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// recordedTurn is a turn's stream and, for a failed one, the notice its
// failure decodes to - held so a row cannot drift onto another path unseen.
type recordedTurn struct {
	lines []string
	want  core.Notice
}

func failedTurns(t *testing.T) map[string]recordedTurn {
	t.Helper()
	auth := fixtureLines(t, "api-error-auth.jsonl")
	return map[string]recordedTurn{
		"a dead login":              {auth, core.NoticeAPIError},
		"a 401 retried then failed": {fixtureLines(t, "api-retry-auth.jsonl"), core.NoticeAPIError},
		"an unreachable API":        {fixtureLines(t, "api-error-unreachable.jsonl"), core.NoticeAPIUnreachable},
		"a usage limit":             {withErrorKind(t, auth, "rate_limit"), core.NoticeUsageLimit},
		"a rejected request":        {withErrorKind(t, auth, "invalid_request"), core.NoticeTurnFailed},
	}
}

// withErrorKind is the recorded failed turn with its failed frame's "error"
// kind swapped for another.
func withErrorKind(t *testing.T, lines []string, kind string) []string {
	t.Helper()
	var frame map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &frame); err != nil || frame["is_api_error_message"] != true {
		t.Fatalf("the auth recording no longer opens with its failed frame (%v)", err)
	}
	frame["error"] = kind
	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := slices.Clone(lines)
	out[0] = string(b)
	return out
}

func eventsOf(t *testing.T, lines []string) []core.Event {
	t.Helper()
	var out []core.Event
	for _, line := range lines {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		out = append(out, evs...)
	}
	return out
}

// failsAs is the premise of a row: the turn's own failure decodes to the
// notice the row names.
func failsAs(t *testing.T, name string, turn recordedTurn) {
	t.Helper()
	for _, ev := range eventsOf(t, turn.lines) {
		if ev.Kind == core.KindAPIError && ev.Notice == turn.want && ev.Subagent == nil {
			return
		}
	}
	t.Fatalf("%s: the recording holds no failure of the agent's own decoding to %q", name, turn.want)
}

// firstTurn is a recording up to and including its first turn's result.
func firstTurn(t *testing.T, name string) []string {
	t.Helper()
	lines := fixtureLines(t, name)
	for i, line := range lines {
		for _, ev := range eventsOf(t, []string{line}) {
			if ev.Kind == core.KindTurnEnd {
				return lines[:i+1]
			}
		}
	}
	t.Fatalf("%s holds no turn end", name)
	return nil
}

// turnOf is one turn of alex's (s1), watched from its start: the report into
// working, the recording's events, and the idle report its result is followed by.
func turnOf(t *testing.T, a App, lines []string) App {
	t.Helper()
	a = reportStates(a, map[string]string{"s1": rpc.StateWorking, "s2": rpc.StateIdle})
	for _, ev := range eventsOf(t, lines) {
		ev.SessionID = "s1"
		a = a.apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
	}
	return reportStates(a, map[string]string{"s1": rpc.StateIdle, "s2": rpc.StateIdle})
}

// doneSaid is what each surface says of alex - the fleet's answer, the roster's
// ✔, the strip's `N done` and the DM's line - in that order.
func doneSaid(a App) []bool {
	ag, _ := a.fleet.Agent("s1")
	return []bool{
		a.fleet.done("s1"),
		strings.Contains(rosterOf(a.fleet), turnDoneGlyph+" alex"),
		strings.Contains(stripANSI(awarenessStrip([]Agent{ag}, a.fleet.RunningTasks, "", 200)), "done"),
		strings.Contains(stripANSI(a.dmFor("s1").heartbeat()), strings.TrimSpace(doneMeta)),
	}
}

var doneSurfaces = []string{"Fleet.done", "the roster's ✔", "the strip's N done", "the DM's done line"}

// wantDone holds every surface to one answer. A "not done" is checked against a
// turn whose end was captured, so it is the failure alone that withholds the ✔ -
// not a done line nobody minted.
func wantDone(t *testing.T, name string, a App, want bool) {
	t.Helper()
	if ag, _ := a.fleet.Agent("s1"); !want && ag.doneAt.IsZero() {
		t.Fatalf("%s: the turn's end was never captured, so not done proves nothing", name)
	}
	for i, said := range doneSaid(a) {
		if said != want {
			t.Errorf("%s: %s says done = %v, want %v", name, doneSurfaces[i], said, want)
		}
	}
}

// Mutation check: drop turnDone's failed gate, or the observe call that sets it,
// and every surface reads done again.
func TestAFailedTurnIsNotDone(t *testing.T) {
	for name, turn := range failedTurns(t) {
		failsAs(t, name, turn)
		wantDone(t, name, turnOf(t, twoAgents(t), turn.lines), false)
	}
}

// The API answering the agent's next turn is the failure over. The healthy
// recording's quota heartbeat (a rate_limit_event) comes after its reply, so
// this also holds that the heartbeat is no failure.
//
// Mutation check: stop model output clearing the bit, or count a
// rate_limit_event as a failure, and no surface reads done.
func TestAHealthyTurnAfterAFailureIsDoneAgain(t *testing.T) {
	healthy := fixtureLines(t, "basic-turn.jsonl")
	for name, turn := range failedTurns(t) {
		a := turnOf(t, turnOf(t, twoAgents(t), turn.lines), healthy)
		wantDone(t, name+", then a healthy turn", a, true)
	}
}

// An interrupt is the operator's ending, not a failure. The recording's first
// turn is cut at its result, so the refused tool's error result and the
// interrupt are the last of it, with no reply after them.
//
// Mutation check: count an error tool result as a failure and this goes red.
func TestAnInterruptedTurnIsStillDone(t *testing.T) {
	interrupted := firstTurn(t, "interrupt-mid-tool.jsonl")
	wantDone(t, "an interrupted turn", turnOf(t, twoAgents(t), interrupted), true)
}

// A turn interrupted before the API's first block redoes nothing a failure
// left undone, and it lifts the failure's pin no more than the bit: the two
// read the same evidence, so the row never draws ✔ beside a standing pin.
//
// Mutation check: clear the bit on an interrupt and this goes red.
func TestAnInterruptAfterAFailureLeavesItStanding(t *testing.T) {
	interrupted := firstTurn(t, "interrupt-mid-generation.jsonl")
	a := turnOf(t, turnOf(t, twoAgents(t), fixtureLines(t, "api-error-unreachable.jsonl")), interrupted)
	wantDone(t, "a failure, then an interrupt", a, false)
	if a.pinnedNotice() == "" {
		t.Error("the interrupt lifted the failure's pin; the bit and the pin must read the same evidence")
	}
}

// A denial is the operator's answer, not a failure: the turn ends success with
// the tool refused and the model replying to the refusal.
func TestADeniedTurnIsStillDone(t *testing.T) {
	denied := fixtureLines(t, "permission-deny-response.jsonl")
	refused := false
	for _, ev := range eventsOf(t, denied) {
		refused = refused || (ev.Kind == core.KindToolResult && ev.Tool != nil && ev.Tool.IsError)
	}
	if !refused {
		t.Fatal("the recording no longer holds a refused tool")
	}
	wantDone(t, "a denied turn", turnOf(t, twoAgents(t), denied), true)
}

// A local command's reply (/status's here) ran no inference, so it proves
// nothing about the API: the failed turn still stands.
func TestALocalCommandAfterAFailureIsNotDone(t *testing.T) {
	local := fixtureLines(t, "bare-status.jsonl")
	answered := false
	for _, ev := range eventsOf(t, local) {
		answered = answered || (ev.Kind == core.KindAssistantText && ev.LocalCommand)
	}
	if !answered {
		t.Fatal("the recording no longer holds a local command's reply")
	}
	a := turnOf(t, turnOf(t, twoAgents(t), fixtureLines(t, "api-error-auth.jsonl")), local)
	wantDone(t, "a failure, then /status", a, false)
}

// A park and a gap forget the failure where they forget doneAt: a woken process
// has failed nothing, and a gap leaves no belief this client can confirm. A
// local command's turn follows each, so only the forgetting can bring the ✔ back.
//
// Mutation check: leave the bit out of WithStatus's park forgetting, or out of
// ForgetTurns, and its row stays not done.
func TestAParkOrAGapForgetsAFailure(t *testing.T) {
	local := fixtureLines(t, "bare-status.jsonl")
	for name, between := range map[string]func(App) App{
		"parked and woken": func(a App) App {
			a = reportStates(a, map[string]string{"s1": rpc.StateParked, "s2": rpc.StateIdle})
			return reportStates(a, map[string]string{"s1": rpc.StateIdle, "s2": rpc.StateIdle})
		},
		"a frame gap": func(a App) App { return a.notedGap(1) },
	} {
		a := between(turnOf(t, twoAgents(t), fixtureLines(t, "api-error-auth.jsonl")))
		wantDone(t, name+", then /status", turnOf(t, a, local), true)
	}
}

// A subagent's frames are not its parent's turn: its failure does not fail the
// parent's, and its answer does not lift the parent's own failure.
//
// Mutation check: drop withTurnOutcome's subagent gate and both rows go red.
func TestASubagentNeitherFailsNorRecoversItsParentsTurn(t *testing.T) {
	auth := fixtureLines(t, "api-error-auth.jsonl")
	var frame map[string]any
	if err := json.Unmarshal([]byte(auth[0]), &frame); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	frame["parent_tool_use_id"] = "toolu_sub"
	b, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	subFailed := string(b)
	if evs := eventsOf(t, []string{subFailed}); len(evs) != 1 || evs[0].Kind != core.KindAPIError || evs[0].Subagent == nil {
		t.Fatalf("the subagent's failure decoded as %+v", evs)
	}

	healthy := fixtureLines(t, "basic-turn.jsonl")
	withSub := append(slices.Clone(healthy[:len(healthy)-1]), subFailed, healthy[len(healthy)-1])
	wantDone(t, "a subagent failing in a healthy turn", turnOf(t, twoAgents(t), withSub), true)

	a := turnOf(t, twoAgents(t), auth)
	subSaid := core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "found it", Subagent: &core.Subagent{Dispatch: "toolu_sub"}}
	a = a.apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &subSaid})
	wantDone(t, "a subagent answering after its parent failed", a, false)
}
