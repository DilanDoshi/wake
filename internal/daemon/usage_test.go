package daemon

// The fleet's session-usage reading: the book's own rules, then over real
// processes - the usage fake reports whatever window a turn names.

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fakeUsage answers a turn "usage <used> <resets unix> [warning]" with the
// rate_limit_event an API response carries, then a reply.
func fakeUsage(sid string) int {
	for line := range stdinLines() {
		if strings.Contains(line, `"type":"control_request"`) {
			fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q}}`+"\n", controlRequestID(line))
			continue
		}
		var used, resets, status string
		if i := strings.Index(line, `usage `); i >= 0 {
			f := strings.Fields(strings.SplitN(line[i:], `"`, 2)[0])
			used, resets, status = f[1], f[2], "allowed"
			if len(f) > 3 {
				status = "allowed_warning"
			}
		}
		if used != "" {
			fmt.Printf(`{"type":"rate_limit_event","session_id":%q,"rate_limit_info":{"status":%q,"rateLimitType":"five_hour","resetsAt":%s,"unifiedWindows":{"five_hour":{"utilization":%s,"resetsAt":%s}}}}`+"\n",
				sid, status, resets, used, resets)
		}
		emitText(sid, "noted")
		emitResult(sid)
	}
	return 0
}

func reading(used float64, resets time.Time, warned bool) core.Event {
	return core.Event{Kind: core.KindRateLimit, SessionUsage: &core.SessionUsage{Used: used, Resets: resets, Warned: warned}}
}

// No report names a reading until an API response has carried one, and an
// event with none - another kind, or a rate_limit_event from older claude -
// changes nothing.
func TestTheBookReportsNothingUntilAReading(t *testing.T) {
	var b usageBook
	if b.note(core.Event{Kind: core.KindRateLimit}, time.Now()) || b.note(core.Event{Kind: core.KindAssistantText}, time.Now()) {
		t.Error("an event with no reading changed the report")
	}
	if got := b.report(); got != nil {
		t.Errorf("report = %+v before any reading, want none", got)
	}
}

// The newest reading wins, rounded to a whole percent, and a reading that says
// what the report already says changes nothing - thirty agents at 47% push once.
func TestANoteChangesTheReportOnlyWhenTheReadingMoves(t *testing.T) {
	var b usageBook
	resets := time.Unix(1792883400, 0)
	for _, step := range []struct {
		ev      core.Event
		changed bool
		percent int
	}{
		{reading(0.47, resets, false), true, 47},
		{reading(0.47, resets, false), false, 47},
		{reading(0.91, resets, true), true, 91},
		{reading(0.91, resets, false), true, 91},
		{reading(0, resets.Add(5*time.Hour), false), true, 0},
	} {
		if got := b.note(step.ev, resets.Add(-time.Hour)); got != step.changed {
			t.Errorf("note(%+v) = %v, want %v", *step.ev.SessionUsage, got, step.changed)
		}
		got := b.report()
		if got == nil || got.Percent != step.percent || !got.Resets.Equal(step.ev.SessionUsage.Resets) || got.Warned != step.ev.SessionUsage.Warned {
			t.Errorf("after %+v the report is %+v", *step.ev.SessionUsage, got)
		}
	}
}

// Each agent's events reach the book on its own goroutine, so two readings that
// left the API a moment apart can land out of order. A window's use only grows,
// so a lower figure for the same reset is the older one; and a reading for a
// window whose reset has passed is older than one for a window still running -
// while another account's live window, resetting sooner, is not.
func TestAReadingThatArrivesBehindANewerOneIsKeptOut(t *testing.T) {
	resets := time.Unix(1792883400, 0)
	before := resets.Add(-time.Hour)

	var b usageBook
	b.note(reading(0.48, resets, false), before)
	if b.note(reading(0.47, resets, false), before) || b.report().Percent != 48 {
		t.Errorf("a lower figure for the same window replaced the newer one: %+v", b.report())
	}

	next := resets.Add(5 * time.Hour)
	b.note(reading(0.02, next, false), resets.Add(time.Second))
	if b.note(reading(0.98, resets, false), resets.Add(time.Second)) || !b.report().Resets.Equal(next) {
		t.Errorf("the window that just reset replaced the one that followed it: %+v", b.report())
	}

	sooner := next.Add(-time.Hour)
	if !b.note(reading(0.30, sooner, false), resets.Add(time.Minute)) || !b.report().Resets.Equal(sooner) {
		t.Errorf("another account's live window, resetting sooner, was kept out: %+v", b.report())
	}
}

// The reset passing makes the figure stale: the report says so once, and the
// next reading takes it back.
func TestTheResetLapsesTheReadingOnceUntilTheNext(t *testing.T) {
	var b usageBook
	resets := time.Unix(1792883400, 0)
	if b.lapse(resets.Add(time.Hour)) {
		t.Error("a book with no reading lapsed")
	}
	b.note(reading(0.47, resets, false), resets.Add(-time.Hour))
	if b.lapse(resets.Add(-time.Second)) {
		t.Error("the reading lapsed before its reset")
	}
	if !b.lapse(resets) || !b.report().Lapsed {
		t.Fatalf("the reading did not lapse at its reset: %+v", b.report())
	}
	if b.lapse(resets.Add(time.Minute)) {
		t.Error("the lapse was reported twice")
	}
	if !b.note(reading(0.02, resets.Add(5*time.Hour), false), resets.Add(time.Minute)) || b.report().Lapsed {
		t.Errorf("a new reading left the lapse standing: %+v", b.report())
	}
}

// awaitUsage waits for a pushed report whose reading satisfies ok.
func (c *testClient) awaitUsage(what string, ok func(rpc.SessionUsage) bool) {
	c.t.Helper()
	c.await(what, func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameStatusPush && f.Status != nil && f.Status.SessionUsage != nil && ok(*f.Status.SessionUsage)
	})
}

// A reading off any agent's API response is pushed to the window watching, and
// a window that attaches afterwards is told it at once.
func TestAReadingIsPushedAndAWindowThatAttachesLaterIsToldIt(t *testing.T) {
	fakeClaudeOnPath(t, "usage")
	d := startDaemon(t)
	c := attach(t, d.socket)
	id := testSessionID("e5e5")
	c.spawn(id, "sydney")
	if got := c.status().SessionUsage; got != nil {
		t.Fatalf("the report named a reading before any API response: %+v", got)
	}

	resets := time.Now().Add(3 * time.Hour).Truncate(time.Second)
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: "usage 0.47 " + strconv.FormatInt(resets.Unix(), 10)})
	c.awaitUsage("the reading pushed", func(u rpc.SessionUsage) bool { return u.Percent == 47 && u.Resets.Equal(resets) && !u.Lapsed })

	late := attach(t, d.socket)
	if got := late.status().SessionUsage; got == nil || got.Percent != 47 {
		t.Errorf("a window attaching after the reading was told %+v, want 47%%", got)
	}
}

// The liveness tick the daemon already runs is what notices a reset passing:
// a reading whose reset is behind it is pushed lapsed, with no timer of its own.
func TestTheLivenessTickLapsesAReadingWhoseResetHasPassed(t *testing.T) {
	shortSilence(t, time.Second)
	fakeClaudeOnPath(t, "usage")
	d := startDaemon(t)
	c := attach(t, d.socket)
	id := testSessionID("f6f6")
	c.spawn(id, "sydney")

	past := time.Now().Add(-time.Minute).Unix()
	c.send(rpc.Frame{Kind: rpc.FrameSend, SessionID: id, Text: "usage 0.62 " + strconv.FormatInt(past, 10)})
	c.awaitUsage("the reading lapsed by the tick", func(u rpc.SessionUsage) bool { return u.Percent == 62 && u.Lapsed })
}
