package core

import (
	"path/filepath"
	"testing"
	"time"
)

// sessionReadings is every five-hour reading a recorded stream carries, in order.
func sessionReadings(t *testing.T, fixture string) []*SessionUsage {
	t.Helper()
	var out []*SessionUsage
	for _, line := range fixtureLines(t, filepath.Join("..", "..", "testdata", "stream", fixture)) {
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s: %v", fixture, err)
		}
		for _, ev := range evs {
			if ev.Kind == KindRateLimit {
				out = append(out, ev.SessionUsage)
			}
		}
	}
	return out
}

// The plan's five-hour window rides every rate_limit_event from 2.1.246 on, as
// unifiedWindows.five_hour; a five-hour warning is the top-level status naming
// that window (question-multiselect.jsonl, 2.1.302, at 90% then 91%).
func TestARateLimitEventCarriesTheSessionWindow(t *testing.T) {
	resets := time.Unix(1792362600, 0)
	want := []SessionUsage{
		{Used: 0.9, Resets: resets, Warned: true},
		{Used: 0.91, Resets: resets, Warned: true},
	}
	got := sessionReadings(t, "question-multiselect.jsonl")
	if len(got) != len(want) {
		t.Fatalf("got %d readings, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] == nil || !got[i].Resets.Equal(w.Resets) || got[i].Used != w.Used || got[i].Warned != w.Warned || got[i].Limited {
			t.Errorf("reading %d = %+v, want %+v", i, got[i], w)
		}
	}
}

// A window that rolls over mid-session reads zero against the next reset
// (midturn-two.jsonl, 2.1.288: 54% then 0%).
func TestAWindowThatResetsMidSessionReadsZeroAgainstTheNextReset(t *testing.T) {
	got := sessionReadings(t, "midturn-two.jsonl")
	if len(got) != 2 || got[0] == nil || got[1] == nil {
		t.Fatalf("got %+v, want two readings", got)
	}
	if got[0].Used != 0.54 || !got[0].Resets.Equal(time.Unix(1791003600, 0)) {
		t.Errorf("first reading = %+v, want 54%% resetting at 1791003600", got[0])
	}
	if got[1].Used != 0 || !got[1].Resets.Equal(time.Unix(1791021600, 0)) {
		t.Errorf("second reading = %+v, want 0%% resetting at 1791021600", got[1])
	}
	if got[0].Warned || got[1].Warned || got[0].Limited || got[1].Limited {
		t.Errorf("an allowed window read as warned or limited: %+v %+v", got[0], got[1])
	}
}

// A weekly warning is the top-level status naming seven_day, so it says nothing
// about the session window beside it (rewind-conversation.jsonl, 2.1.246).
func TestAWeeklyWarningDoesNotWarnTheSessionWindow(t *testing.T) {
	got := sessionReadings(t, "rewind-conversation.jsonl")
	if len(got) != 1 || got[0] == nil {
		t.Fatalf("got %+v, want one reading", got)
	}
	if got[0].Used != 0.38 || got[0].Warned || got[0].Limited {
		t.Errorf("reading = %+v, want 38%% and no warning: the warning was the week's", got[0])
	}
}

// Claude before 2.1.246 names a reset but no figure (basic-turn.jsonl, 2.1.226),
// and a frame with no rate_limit_info names neither: no reading either way.
func TestAFrameWithNoSessionFigureIsNoReading(t *testing.T) {
	got := sessionReadings(t, "basic-turn.jsonl")
	if len(got) != 1 || got[0] != nil {
		t.Errorf("an older claude's rate_limit_event decoded as %+v, want no reading", got)
	}
	for _, line := range []string{
		`{"type":"rate_limit_event","session_id":"s1"}`,
		`{"type":"rate_limit_event","session_id":"s1","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"resetsAt":1792883400}}}}`,
		`{"type":"rate_limit_event","session_id":"s1","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":0.4}}}}`,
	} {
		if ev := onlyEvent(t, line, 0); ev.SessionUsage != nil {
			t.Errorf("%s decoded as %+v, want no reading", line, ev.SessionUsage)
		}
	}
}

// Past a warning is the limit itself. No recording has the status Claude sends
// there, so anything on the five-hour window that is neither allowed nor a
// warning reads as limited.
func TestAStatusPastAWarningIsTheLimit(t *testing.T) {
	line := `{"type":"rate_limit_event","session_id":"s1","rate_limit_info":{"status":"rejected","rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"resetsAt":1792883400,"utilization":1}}}}`
	got := onlyEvent(t, line, 0).SessionUsage
	if got == nil || !got.Limited || got.Warned || got.Used != 1 {
		t.Errorf("reading = %+v, want 100%% and limited", got)
	}
}

// A window of an unexpected shape costs the reading, never the frame: the
// status and its notice still arrive, and the five-hour figure survives a
// sibling window nobody reads.
func TestAnUnexpectedWindowShapeKeepsTheFrame(t *testing.T) {
	bad := `{"type":"rate_limit_event","session_id":"s1","rate_limit_info":{"status":"allowed_warning","rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":"n/a","resetsAt":1792883400}}}}`
	ev := onlyEvent(t, bad, 0)
	if ev.Kind != KindRateLimit || ev.Notice != NoticeRateLimited || ev.SessionUsage != nil {
		t.Errorf("a five-hour window of the wrong shape decoded as %q/%q with %+v, want the frame and its notice, no reading", ev.Kind, ev.Notice, ev.SessionUsage)
	}
	sibling := `{"type":"rate_limit_event","session_id":"s1","rate_limit_info":{"status":"allowed","rateLimitType":"five_hour","unifiedWindows":{"five_hour":{"utilization":0.4,"resetsAt":1792883400},"seven_day":{"utilization":"n/a"}}}}`
	if got := onlyEvent(t, sibling, 0).SessionUsage; got == nil || got.Used != 0.4 {
		t.Errorf("a sibling window of the wrong shape cost the five-hour reading: %+v", got)
	}
}
