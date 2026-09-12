package core

import "testing"

// A successful compaction's boundary carries the real figures - context before
// and after, what it dropped, how long it took, and what triggered it - and the
// airlock surfaces them as a CompactSummary. This is the whole reason a
// completion line can show numbers rather than inventing a percentage.
func TestCompactBoundaryCarriesItsMetadata(t *testing.T) {
	line, n := findFixtureLine(t, "compaction.jsonl", `"compact_metadata"`)
	ev := onlyEvent(t, line, n)
	if ev.Notice != NoticeContextCompacted {
		t.Fatalf("compaction.jsonl:%d resolved to %q, want %q", n, ev.Notice, NoticeContextCompacted)
	}
	s := ev.Compaction
	if s == nil {
		t.Fatalf("compaction.jsonl:%d carried no CompactSummary", n)
	}
	if s.PreTokens != 50826 || s.PostTokens != 4477 || s.Dropped != 46349 || s.DurationMs != 16813 {
		t.Errorf("summary = %+v, want pre 50826 / post 4477 / dropped 46349 / dur 16813ms", *s)
	}
	if s.Trigger != "manual" {
		t.Errorf("trigger = %q, want manual", s.Trigger)
	}
}

// Every other frame carries no compaction summary, so a consumer can key on its
// presence - the boundary is the one frame that reports it.
func TestOnlyTheBoundaryCarriesACompactSummary(t *testing.T) {
	for _, marker := range []string{`"status":"compacting"`, `"compact_result":"success"`} {
		line, n := findFixtureLine(t, "compaction.jsonl", marker)
		if ev := onlyEvent(t, line, n); ev.Compaction != nil {
			t.Errorf("compaction.jsonl:%d (%s) carried a CompactSummary, want none", n, marker)
		}
	}
}
