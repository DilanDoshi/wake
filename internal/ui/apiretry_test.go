package ui

// While claude retries a request the API never answers - the network gone, a
// host that does not resolve, an overload - the operator is told so where the
// turn is drawn, in place of a working timer that would tick for three minutes.
// The recording is testdata/stream/api-error-unreachable.jsonl: ten retries, then
// the failure. Everything here is driven by the retry frames and the heartbeat
// ticker a working agent already runs; nothing is scheduled.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// retryingInto replays the recording's retries into s1, a working agent of a,
// and stops before the turn fails, returning the app mid-storm.
func retryingInto(t *testing.T, a App) App {
	t.Helper()
	a = reportStates(a, map[string]string{"s1": rpc.StateWorking, "s2": rpc.StateIdle})
	before := 0
	for _, line := range fixtureLines(t, "api-error-unreachable.jsonl") {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, ev := range evs {
			if ev.Kind == core.KindAPIError {
				if before == 0 {
					t.Fatal("the recording's turn failed before any retry")
				}
				return a
			}
			before++
			ev.SessionID = "s1"
			a = a.apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
		}
	}
	t.Fatal("the recording holds no failed turn")
	return a
}

// retryFrame is one retry of s1's request: which try failed, out of ten, with
// the pause before the next and the status the API answered, if any.
func retryFrame(attempt, status int, delay time.Duration) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindSystem, SessionID: "s1", Notice: core.NoticeAPIRetrying,
		Retry: &core.APIRetry{Attempt: attempt, Max: 10, Delay: delay, Status: status},
	}}
}

// holdClock pins the heartbeat's clock at t for the test, so a countdown reads
// off a known now rather than the wall.
func holdClock(t *testing.T, at time.Time) {
	t.Helper()
	clock = func() time.Time { return at }
	t.Cleanup(func() { clock = time.Now })
}

// The conversation of an agent whose request claude is retrying says so above
// its composer, in the row the working line takes - the cause, that it is
// retrying, and which try this is - rather than a pool word over a timer.
func TestAConversationSaysItsAPIIsBeingRetried(t *testing.T) {
	a := retryingInto(t, twoAgents(t))
	pane := stripANSI(a.dmPane("s1", 120, 40))
	for _, want := range []string{retryGlyph + " " + unreachableCause + metaSep + retryingWord, "attempt 10 of 10"} {
		if !strings.Contains(pane, want) {
			t.Errorf("ten retries in, the conversation lacks %q:\n%s", want, pane)
		}
	}
}

// The room's working line and the roster row say it too, so a supervisor sees
// it without opening the conversation. Read off the whole frame, as drawn.
func TestTheRoomSaysAnAgentsAPIIsBeingRetried(t *testing.T) {
	view := shown(retryingInto(t, roomWithHistoryAsked(t)))
	if want := "@alex " + retryGlyph + " " + unreachableCause + metaSep + "retrying 10/10" + roomFor; !strings.Contains(view, want) {
		t.Errorf("ten retries in, the room's working line lacks %q:\n%s", want, view)
	}
	if !strings.Contains(view, retryGlyph+" alex") {
		t.Errorf("ten retries in, alex's roster row keeps its working glyph:\n%s", view)
	}
	if !strings.Contains(view, "○ bea") {
		t.Errorf("bea's roster row changed for alex's retry:\n%s", view)
	}
}

// The line counts the pause down to the next try off the heartbeat's clock,
// rounding up so the last fraction reads 1s and never 0s, then says the try is
// under way. The turn's own age stays beside the count, as the working line's does.
func TestTheRetryLineCountsDownThePauseThenWaits(t *testing.T) {
	forceTrueColour(t)
	start := clock()
	holdClock(t, start)
	r := apiRetry{attempt: 3, max: 10, delay: 2486 * time.Millisecond, since: start}
	if got, want := stripANSI(retryLine(r, 72*time.Second, 80)), "⚠ API unreachable · retrying in 3s (attempt 3 of 10 · 1m 12s)"; got != want {
		t.Errorf("mid-pause the line reads %q, want %q", got, want)
	}
	holdClock(t, start.Add(2*time.Second))
	if got, want := stripANSI(retryLine(r, 74*time.Second, 80)), "⚠ API unreachable · retrying in 1s (attempt 3 of 10 · 1m 14s)"; got != want {
		t.Errorf("at the pause's last half-second the line reads %q, want %q", got, want)
	}
	holdClock(t, start.Add(3*time.Second))
	if got, want := stripANSI(retryLine(r, 75*time.Second, 80)), "⚠ API unreachable · retrying… (attempt 3 of 10 · 1m 15s)"; got != want {
		t.Errorf("past the pause the line reads %q, want %q", got, want)
	}
}

// A status the API answered with is named rather than guessed at: an overload
// is `API error 529`, not "unreachable".
func TestTheRetryLineNamesAStatusTheAPIAnswered(t *testing.T) {
	forceTrueColour(t)
	r := apiRetry{attempt: 1, max: 10, status: 529, since: clock().Add(-time.Minute)}
	if got, want := stripANSI(retryLine(r, 5*time.Second, 80)), "⚠ API error 529 · retrying… (attempt 1 of 10 · 5s)"; got != want {
		t.Errorf("a 529 retry reads %q, want %q", got, want)
	}
	if got, want := stripANSI(roomRetryLine(r, 5*time.Second, 80)), "⚠ API error 529 · retrying 1/10 for 5s"; got != want {
		t.Errorf("the room's 529 retry reads %q, want %q", got, want)
	}
}

// The parenthesised count is dropped whole before the warning is cut, as the
// working line drops its meta: the cause is what says why the turn is stalled.
func TestTheRetryLineDropsItsCountBeforeItsWarning(t *testing.T) {
	forceTrueColour(t)
	r := apiRetry{attempt: 3, max: 10, since: clock().Add(-time.Minute)}
	got := stripANSI(retryLine(r, time.Minute, 34))
	if got != "⚠ API unreachable · retrying…" {
		t.Errorf("a narrow line reads %q, want the warning alone", got)
	}
	if lipgloss.Width(got) > 34 {
		t.Errorf("a narrow line ran to %d cells", lipgloss.Width(got))
	}
	if retryLine(r, time.Minute, 0) != "" {
		t.Error("a zero width drew something")
	}
}

// The retry clears on proof the request is over: the API answered - a streamed
// start or token, prose, a tool call - or the turn ended, failed for good (the
// unreachable pin takes over), or the conversation was cleared.
func TestARetryClearsWhenTheAPIAnswersOrTheTurnIsOver(t *testing.T) {
	for name, ev := range map[string]core.Event{
		"a streamed start":  {Kind: core.KindMessageStart},
		"a streamed token":  {Kind: core.KindPartialText, Text: "h"},
		"the turn's tokens": {Kind: core.KindTurnTokens},
		"prose":             {Kind: core.KindAssistantText, Text: "hi"},
		"thinking":          {Kind: core.KindThinking, Text: "hm"},
		"a tool call":       {Kind: core.KindToolUse, Tool: &core.ToolCall{ID: "t1", Name: "Bash"}},
		"the turn's end":    {Kind: core.KindTurnEnd},
		"the final failure": *unreachableFrame("s1").Event,
		"a /clear":          {Kind: core.KindSessionReset},
	} {
		a := retryingInto(t, twoAgents(t))
		ev.SessionID = "s1"
		a = a.apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
		if _, held := a.retrying["s1"]; held {
			t.Errorf("%s left the retry standing", name)
		}
	}
	if pin := retryingInto(t, twoAgents(t)).apply(unreachableFrame("s1")).pinnedNotice(); !strings.Contains(pin, "ENOTFOUND") {
		t.Errorf("the final failure did not pin as before: %q", pin)
	}
}

// What is not the API's answer leaves it: a receipt, a queued message's
// lifecycle, a replayed line, a quota reading, a system frame of another kind,
// and a background subagent's output, which rides the parent's session while the
// parent's own request still waits.
func TestARetryOutlivesWhatIsNotTheAPIsAnswer(t *testing.T) {
	sub := &core.Subagent{Dispatch: "t9", Type: "Explore"}
	for name, ev := range map[string]core.Event{
		"a control receipt":      {Kind: core.KindControlReceipt, RequestID: "r1"},
		"a message's lifecycle":  {Kind: core.KindMessageState, MessageID: "m1"},
		"a replayed line":        {Kind: core.KindUserText, Text: "@alex go", Echoed: true},
		"a quota reading":        {Kind: core.KindRateLimit},
		"another system frame":   {Kind: core.KindSystem},
		"a subagent's prose":     {Kind: core.KindAssistantText, Text: "found it", Subagent: sub},
		"a subagent's tool call": {Kind: core.KindToolUse, Tool: &core.ToolCall{ID: "t2", Name: "Read"}, Subagent: sub},
		"a subagent's thinking":  {Kind: core.KindThinking, Text: "hm", Subagent: sub},
	} {
		a := retryingInto(t, twoAgents(t))
		ev.SessionID = "s1"
		a = a.apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
		if _, held := a.retrying["s1"]; !held {
			t.Errorf("%s cleared the retry, as if the API had answered", name)
		}
	}
}

// A report that shows the agent no longer working - parked, ended, idle after
// an interrupt whose turn end a gap ate - forgets its retry.
func TestARetryIsForgottenWhenTheAgentStopsWorking(t *testing.T) {
	for _, state := range []string{rpc.StateParked, rpc.StateEnded, rpc.StateIdle} {
		a := reportStates(retryingInto(t, twoAgents(t)), map[string]string{"s1": state, "s2": rpc.StateIdle})
		if _, held := a.retrying["s1"]; held {
			t.Errorf("a report showing s1 %s left its retry standing", state)
		}
	}
	a := reportStates(retryingInto(t, twoAgents(t)), map[string]string{"s1": rpc.StateWorking, "s2": rpc.StateIdle})
	if _, held := a.retrying["s1"]; !held {
		t.Error("a report showing s1 still working forgot its retry")
	}
}

// A frame gap or a reattach may have eaten the answer that would have cleared
// a retry, so both forget every one as they forget the other turn beliefs; the
// next retry frame restores it.
func TestAGapOrAReattachForgetsEveryRetry(t *testing.T) {
	for name, forget := range map[string]func(App) App{
		"a gap":      func(a App) App { return a.notedGap(3) },
		"a reattach": func(a App) App { m, _ := a.reattached(reattachedMsg{}); return m.(App) },
	} {
		a := forget(retryingInto(t, twoAgents(t)))
		if len(a.retrying) != 0 {
			t.Errorf("%s left a retry standing: %+v", name, a.retrying)
		}
		if a = a.apply(retryFrame(4, 0, time.Second)); a.retrying["s1"].attempt != 4 {
			t.Errorf("after %s the next retry frame did not restore the line: %+v", name, a.retrying)
		}
	}
}

// Both lines stay within any width, the head cut before the count is dropped:
// a row wider than its pane is the alt-screen scroll DM.chrome exists for.
func TestTheRetryLinesStayWithinEveryWidth(t *testing.T) {
	forceTrueColour(t)
	r := apiRetry{attempt: 3, max: 10, delay: 30 * time.Second, since: clock()}
	for _, width := range []int{1, 5, 10, 20, 28, 40} {
		for name, line := range map[string]string{"conversation": retryLine(r, time.Minute, width), "room": roomRetryLine(r, time.Minute, width)} {
			if got := lipgloss.Width(line); got > width || got == 0 {
				t.Errorf("the %s line at width %d drew %d cells: %q", name, width, got, stripANSI(line))
			}
		}
	}
}

// Each retry frame replaces the last: the line always says the newest count
// and pause, and the storm never grows the record.
func TestEachRetryReplacesTheLast(t *testing.T) {
	a := twoAgents(t)
	a = reportStates(a, map[string]string{"s1": rpc.StateWorking, "s2": rpc.StateIdle})
	a = a.apply(retryFrame(1, 0, time.Second)).apply(retryFrame(2, 0, 2*time.Second))
	if got := a.retrying["s1"]; got.attempt != 2 || got.delay != 2*time.Second || len(a.retrying) != 1 {
		t.Errorf("after two retries the record holds %+v (%d sessions), want the second alone", got, len(a.retrying))
	}
}

// A 401 retry keeps its own path - the login's failure, told and marked for
// /reauth - and draws no retry line: a new process is what recovers it, not a
// wait. api-retry-auth.jsonl.
func TestA401RetryDrawsNoRetryLine(t *testing.T) {
	a := twoAgents(t)
	a = reportStates(a, map[string]string{"s1": rpc.StateWorking, "s2": rpc.StateIdle})
	for _, line := range fixtureLines(t, "api-retry-auth.jsonl") {
		evs, err := core.DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, ev := range evs {
			if ev.Kind != core.KindAPIError {
				continue
			}
			ev.SessionID = "s1"
			a = a.apply(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
		}
	}
	if len(a.retrying) != 0 {
		t.Errorf("a 401 retry was held as retrying: %+v", a.retrying)
	}
	if _, marked := a.authFailed["s1"]; !marked {
		t.Error("the 401 retry no longer marks the session for /reauth")
	}
	if n, _ := notice.Latest(); !strings.Contains(n.Text, "401") {
		t.Errorf("the 401 retry's notice is gone: %q", n.Text)
	}
	if pane := stripANSI(a.dmPane("s1", 120, 40)); strings.Contains(pane, retryGlyph+" ") {
		t.Errorf("a 401 retry drew a retry line:\n%s", pane)
	}
}

// A retry notice is the line above the composer and nothing else: no block in
// the transcript, for the compaction notices' reason - the standing line already
// says it, and ten rows of it would be the storm drawn twice.
func TestARetryNoticeLeavesNoTranscriptBlock(t *testing.T) {
	forceTrueColour(t)
	d := NewDM("s1", "alex").SetSize(80, 24)
	if b := d.renderEvent(*retryFrame(3, 0, time.Second).Event); strings.TrimSpace(b.text) != "" {
		t.Errorf("a retry notice drew a transcript block %q, want none", b.text)
	}
}

// The retry line takes the working line's row - the same one row, so a pane
// sized for a working agent draws exactly its rows with a retry up.
func TestTheRetryLineTakesTheWorkingLinesRow(t *testing.T) {
	forceTrueColour(t)
	const w, h = 100, 29
	d := NewDM("s1", "alex").SetSize(w, h)
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking, startedAt: clock()}
	d = d.WithRetrying(apiRetry{attempt: 2, max: 10, delay: time.Second, since: clock()})
	if got := lipgloss.Height(d.View(w, h)); got != h {
		t.Errorf("pane drew %d rows into %d with a retry up", got, h)
	}
	if beat := stripANSI(d.heartbeat()); !strings.Contains(beat, unreachableCause) {
		t.Errorf("the heartbeat row reads %q, want the retry line", beat)
	}
	if !d.hasBeat() {
		t.Error("hasBeat is false with a retry up; the row is unbudgeted")
	}
}

// The roster's glyph waits for a row the daemon reads as working, as the DM and
// the room do: a turn it reads as idle (a peer's message, a background task's
// note) is pruned on the next report, so a glyph drawn meanwhile would flicker.
func TestTheRosterGlyphWaitsForAWorkingRow(t *testing.T) {
	a := twoAgents(t).apply(retryFrame(2, 0, time.Second))
	if _, held := a.retrying["s1"]; !held {
		t.Fatal("the retry was not held for the idle agent")
	}
	if view := shown(a); strings.Contains(view, retryGlyph+" alex") || !strings.Contains(view, "○ alex") {
		t.Errorf("an idle agent's row drew the retry glyph:\n%s", view)
	}
	a = reportStates(a, map[string]string{"s1": rpc.StateWorking, "s2": rpc.StateIdle}).apply(retryFrame(3, 0, time.Second))
	if view := shown(a); !strings.Contains(view, retryGlyph+" alex") {
		t.Errorf("a working agent's row kept its glyph through a retry:\n%s", view)
	}
}

// The room names the oldest turn, as ever; when that turn is being retried the
// row says so in place of its word, and the count of the rest still follows.
func TestTheRoomLineNamesTheOldestTurnsRetry(t *testing.T) {
	forceTrueColour(t)
	agents := []Agent{working("s1", "noah", 72*time.Second, 0), working("s2", "mia", time.Second, 0)}
	retrying := map[string]apiRetry{"s1": {attempt: 3, max: 10, since: clock().Add(-time.Minute)}}
	got := stripANSI(roomWorkingLine(agents, retrying, 120))
	if want := "@noah ⚠ API unreachable · retrying 3/10 for 1m 12s · +1 more working"; got != want {
		t.Errorf("the room line reads %q, want %q", got, want)
	}
	if got := stripANSI(roomWorkingLine(agents, map[string]apiRetry{"s2": retrying["s1"]}, 120)); strings.Contains(got, retryGlyph) {
		t.Errorf("a younger turn's retry took the oldest's row: %q", got)
	}
}
