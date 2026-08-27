package core

// When a transcript line happened.
//
// The room is a fold over many sessions, so restoring it means interleaving
// several transcripts - and interleaving needs a clock. The on-disk record
// carries one; the stream does not carry one at all, which is why this is
// stamped by DecodeTranscriptLine and nowhere else.

import (
	"testing"
	"time"
)

// Every recorded line that produces an event produces one that knows when it
// happened. A stamp on some of them would interleave a room in an order that
// is only mostly right, which is worse than an order that is obviously wrong.
func TestEveryRecordedTranscriptEventIsStamped(t *testing.T) {
	stamped := 0
	for _, path := range transcriptFiles(t) {
		for i, line := range fixtureLines(t, path) {
			events, err := DecodeTranscriptLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d: %v", path, i+1, err)
			}
			for _, ev := range events {
				if ev.At.IsZero() {
					t.Errorf("%s:%d decoded to a %v with no time on it", path, i+1, ev.Kind)
					continue
				}
				stamped++
			}
		}
	}
	if stamped == 0 {
		t.Fatal("no recorded transcript line produced a stamped event: this test is asserting nothing")
	}
}

// The stamp is the record's own time and not the moment it was read.
func TestATranscriptLineCarriesItsOwnTime(t *testing.T) {
	const line = `{"type":"assistant","timestamp":"2026-08-09T06:04:08.727Z","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`
	events, err := DecodeTranscriptLine([]byte(line))
	if err != nil || len(events) != 1 {
		t.Fatalf("decode: %d events, err=%v", len(events), err)
	}
	want := time.Date(2026, 8, 9, 6, 4, 8, 727_000_000, time.UTC)
	if !events[0].At.Equal(want) {
		t.Errorf("the event is stamped %v, want the record's own %v", events[0].At, want)
	}
}

// A line the clock cannot be read from still decodes. A transcript is a file
// somebody else writes, and losing a turn over a field the conversation does
// not depend on is the wrong trade - the room draws it in the order it has.
func TestALineWithNoReadableTimeStillDecodes(t *testing.T) {
	for _, tc := range []struct{ what, line string }{
		{"no timestamp at all", `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`},
		{"a timestamp nothing can parse", `{"type":"assistant","timestamp":"half past ten","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`},
	} {
		events, err := DecodeTranscriptLine([]byte(tc.line))
		if err != nil {
			t.Errorf("%s: decode failed: %v", tc.what, err)
			continue
		}
		if len(events) != 1 {
			t.Errorf("%s: produced %d events, want the turn to survive", tc.what, len(events))
			continue
		}
		if !events[0].At.IsZero() {
			t.Errorf("%s: invented a time (%v)", tc.what, events[0].At)
		}
	}
}

// The stream carries no time, and a stamp on a live event would be a clock
// nothing set. It is also the discriminator the room's merge runs on: a live
// line is the one with no time, and it is never re-ordered.
func TestAStreamLineIsNotStamped(t *testing.T) {
	events, err := DecodeLine([]byte(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"hello"}]}}`))
	if err != nil || len(events) != 1 {
		t.Fatalf("decode: %d events, err=%v", len(events), err)
	}
	if !events[0].At.IsZero() {
		t.Errorf("a stream event is stamped %v; nothing on that wire says when anything happened", events[0].At)
	}
}
