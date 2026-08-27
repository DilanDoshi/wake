package core

// What --include-partial-messages puts on the wire, and the one thing Wake
// reads out of it.
//
// There is no fixture behind these: the corpus was recorded without the flag,
// so every frame here is hand-written from claude 2.1.233's own zod schema.
// See wireStreamEvent for the schema and for what that
// source is worth.

import (
	"slices"
	"testing"
)

func TestATextDeltaIsTheOnlyThingAStreamEventProduces(t *testing.T) {
	evs := decodeOne(t, `{"type":"stream_event","session_id":"s1","parent_tool_use_id":null,"uuid":"u1","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Fixed "}}}`)
	if len(evs) != 1 {
		t.Fatalf("a text delta decoded to %d events, want 1: %v", len(evs), kinds(evs))
	}
	if evs[0].Kind != KindPartialText {
		t.Errorf("Kind = %q, want %q", evs[0].Kind, KindPartialText)
	}
	if evs[0].Text != "Fixed " {
		t.Errorf("Text = %q, want the delta's own text: the event carries the tokens that arrived, never the block so far", evs[0].Text)
	}
	if evs[0].SessionID != "s1" {
		t.Errorf("SessionID = %q, want s1: a partial has to reach the conversation it belongs to", evs[0].SessionID)
	}
}

// Every shape that is neither a text delta nor a token count is dropped, and
// that is the design rather than an omission: the completed block arrives as an
// ordinary assistant frame a moment later and is what the transcript renders.
// Anything decoded here would be a second account of the same words.
//
// message_start and message_delta left this list on 2026-08-16 and are read
// for the *count* rather than for their content - the words they describe still
// arrive complete behind them, so the sentence above is unchanged. See
// turntokens_test.go and KindTurnTokens.
func TestEveryStreamEventThatDrawsNothingDecodesToNothing(t *testing.T) {
	for _, c := range []struct {
		name string
		line string
	}{
		{"content_block_start", `{"type":"stream_event","session_id":"s1","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`},
		{"content_block_stop", `{"type":"stream_event","session_id":"s1","event":{"type":"content_block_stop","index":0}}`},
		{"a message_delta with no count on it", `{"type":"stream_event","session_id":"s1","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"}}}`},
		{"message_stop", `{"type":"stream_event","session_id":"s1","event":{"type":"message_stop"}}`},
		{"ping", `{"type":"stream_event","session_id":"s1","event":{"type":"ping"}}`},
		{"a tool call's arguments", `{"type":"stream_event","session_id":"s1","event":{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}}`},
		{"thinking", `{"type":"stream_event","session_id":"s1","event":{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}}`},
		{"a frame carrying no event at all", `{"type":"stream_event","session_id":"s1"}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if evs := decodeOne(t, c.line); len(evs) != 0 {
				t.Errorf("decoded to %v, want nothing: this frame draws no line, and what it describes is already carried by the completed block", kinds(evs))
			}
		})
	}
}

// A delta with no text is not a partial. It costs a copy through the whole
// fan-out and moves nothing on any screen, and at a token a frame across
// thirty agents the empty ones are the cheapest thing to refuse.
func TestAnEmptyTextDeltaIsNotAnEvent(t *testing.T) {
	if evs := decodeOne(t, `{"type":"stream_event","session_id":"s1","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}}`); len(evs) != 0 {
		t.Errorf("an empty text delta decoded to %v, want nothing", kinds(evs))
	}
}

// A subagent's tokens are dropped outright rather than attributed. A preview
// is one running text with no speaker on it, and three concurrent streams
// interleaving into it would be the monologue dm_blocks.go needed a gutter and
// a per-block header to prevent - on a surface that is replaced within the
// turn. The subagent's completed blocks still arrive, with that attribution.
func TestASubagentsTokensAreNotPreviewed(t *testing.T) {
	if evs := decodeOne(t, `{"type":"stream_event","session_id":"s1","parent_tool_use_id":"toolu_1","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}`); len(evs) != 0 {
		t.Errorf("a forwarded subagent's token decoded to %v, want nothing", kinds(evs))
	}
}

// A partial carries no Raw, alone among everything the airlock decodes: it is
// one token, and pinning the line it arrived on holds the whole stream alive at
// a token a frame across thirty agents.
func TestAPartialPinsNoneOfTheStreamItArrivedOn(t *testing.T) {
	ev := decodeOne(t, `{"type":"stream_event","session_id":"s1","parent_tool_use_id":null,"event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}}`)[0]
	if ev.Raw != nil {
		t.Errorf("Raw is set on a partial: %s", ev.Raw)
	}
	if ev.Subagent != nil {
		t.Errorf("Subagent = %+v, want nil: a previewed token is the agent's own by construction", ev.Subagent)
	}
}

// The flag and the two it depends on travel together, and that is claude's
// rule rather than Wake's taste. The 2.1.233 bundle carries the refusal in
// so many words:
//
//	Error: --include-partial-messages requires --print and --output-format=stream-json.
//
// buildArgs emits all three unconditionally today, so this is a guard against
// a future argv that makes one of them conditional and turns every spawn into
// a startup error - which, like the identity shapes argv.go refuses, exits 1
// with nothing on stdout.
func TestThePartialFlagIsEmittedBesideTheTwoItRequires(t *testing.T) {
	args, err := NewSession(Config{SessionID: "11111111-1111-4111-8111-111111111111"}).buildArgs()
	if err != nil {
		t.Fatalf("buildArgs: %v", err)
	}
	for _, want := range []string{"--include-partial-messages", "--print", "stream-json"} {
		if !slices.Contains(args, want) {
			t.Errorf("buildArgs emits %v, which does not carry %q: claude refuses --include-partial-messages without --print and --output-format=stream-json", args, want)
		}
	}
}

func decodeOne(t *testing.T, line string) []Event {
	t.Helper()
	evs, err := DecodeLine([]byte(line))
	if err != nil {
		t.Fatalf("decode %s: %v", line, err)
	}
	return evs
}
