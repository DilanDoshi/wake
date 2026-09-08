package core

// DecodeTranscriptLine, against a recorded on-disk transcript.
//
// The file is claude's own, from this project's interrupt spike, scrubbed of
// signatures and truncated - not written by hand. A hand-written fixture agrees
// with whoever wrote it about the format, which is the one thing a decoder test
// must not do.

import (
	"encoding/json"
	"strings"
	"testing"
)

// Every user and assistant line in the recorded transcript decodes, and nothing
// decodes to KindUnknown.
//
// KindUnknown is the shape a second decoder would produce silently: the line is
// consumed, an event comes out, and the conversation is missing a turn nobody
// can see is missing.
func TestEveryRecordedTranscriptLineDecodesWithNoUnknowns(t *testing.T) {
	messages := 0
	for _, path := range transcriptFiles(t) {
		for i, line := range fixtureLines(t, path) {
			events, err := DecodeTranscriptLine([]byte(line))
			if err != nil {
				t.Fatalf("%s:%d: %v", path, i+1, err)
			}
			for _, ev := range events {
				if ev.Kind == KindUnknown {
					t.Errorf("%s:%d decoded to KindUnknown: %s", path, i+1, ev.Text)
				}
			}
			if len(events) > 0 {
				messages++
			}
		}
	}
	if messages == 0 {
		t.Fatal("no line in the recorded transcript produced an event: this test is asserting nothing")
	}
}

// The record types that exist only on disk produce nothing at all.
//
// They are not frames and have no place in a conversation: an attachment is a
// file claude read, a queue-operation is a keystroke, a custom-title is the
// name on a tab. Decoded rather than skipped they would each land in somebody's
// transcript as an unknown event.
func TestTheRecordsThatExistOnlyOnDiskAreDropped(t *testing.T) {
	dropped := 0
	for _, path := range transcriptFiles(t) {
		for i, line := range fixtureLines(t, path) {
			var f struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(line), &f); err != nil {
				t.Fatalf("%s:%d: %v", path, i+1, err)
			}
			if f.Type == "user" || f.Type == "assistant" {
				continue
			}
			events, err := DecodeTranscriptLine([]byte(line))
			if err != nil || len(events) != 0 {
				t.Errorf("%s:%d is a %q record and produced %d events (err=%v)", path, i+1, f.Type, len(events), err)
			}
			dropped++
		}
	}
	if dropped == 0 {
		t.Fatal("the recorded transcript has no on-disk-only records, so the filter is untested")
	}
}

// The conversation comes back with both sides of it and the tool calls between.
//
// Named kinds rather than a count: what a returning reader needs is their own
// turns, the agent's prose, and what it did - and a decoder that dropped one of
// the three would still produce a plausible number of events.
func TestATranscriptDecodesBothSidesAndTheToolCallsBetween(t *testing.T) {
	seen := map[EventKind]int{}
	for _, path := range transcriptFiles(t) {
		for _, line := range fixtureLines(t, path) {
			events, err := DecodeTranscriptLine([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			for _, ev := range events {
				seen[ev.Kind]++
			}
		}
	}
	for _, kind := range []EventKind{KindUserText, KindAssistantText, KindToolUse} {
		if seen[kind] == 0 {
			t.Errorf("the recorded transcript decoded no %v: %v", kind, seen)
		}
	}
}

// A subagent's line is not the conversation the operator was having.
func TestASidechainLineIsDropped(t *testing.T) {
	const line = `{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"a subagent talking"}]}}`
	events, err := DecodeTranscriptLine([]byte(line))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("a sidechain line produced %d events: a subagent's turn is not the operator's conversation", len(events))
	}

	// And the same line without the flag is kept, so the test above is about
	// the flag rather than about the shape.
	events, err = DecodeTranscriptLine([]byte(strings.Replace(line, `"isSidechain":true,`, "", 1)))
	if err != nil || len(events) == 0 {
		t.Errorf("the same line without isSidechain produced %d events (err=%v)", len(events), err)
	}
}

// A failed turn's synthetic frame is a type:"assistant" line on disk, so it must
// be dropped rather than restored as the agent saying "Not logged in · Please run
// /login" - the /resume that recovery drives re-reads exactly this. The disk key
// is camelCase (isApiErrorMessage) where the live stream is snake_case, recorded
// in testdata/transcript/api-error-auth.jsonl.
func TestAnAPIErrorTranscriptLineIsDropped(t *testing.T) {
	const line = `{"type":"assistant","isApiErrorMessage":true,"error":"authentication_failed","message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Not logged in · Please run /login"}]}}`
	events, err := DecodeTranscriptLine([]byte(line))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("an API-error line produced %d events: a failed turn's synthetic frame is not conversation content and must not restore as agent speech", len(events))
	}

	// The same line without the marker is ordinary assistant text and is kept, so
	// the drop is about the marker rather than the "<synthetic>" model.
	events, err = DecodeTranscriptLine([]byte(strings.Replace(line, `"isApiErrorMessage":true,`, "", 1)))
	if err != nil || len(events) == 0 {
		t.Errorf("the same line without isApiErrorMessage produced %d events (err=%v)", len(events), err)
	}
}

// A malformed line is an error rather than a silent drop.
func TestAMalformedTranscriptLineIsReported(t *testing.T) {
	if _, err := DecodeTranscriptLine([]byte("{not json")); err == nil {
		t.Error("a malformed line decoded without error: a transcript half-read is a conversation missing turns nobody can see are missing")
	}
}
