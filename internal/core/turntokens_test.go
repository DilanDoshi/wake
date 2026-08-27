package core

// What the turn in flight has produced, off the streaming wire.
//
// The only *complete* count is on the result frame that ends a turn, which is
// why every surface showed a session total: during turn N they reported turns
// 1…N−1. A `message_delta` carries the output tokens of the message it closes,
// so summing them across a turn is that turn's own figure, arriving as the turn
// runs rather than after it.
//
// Hand-written frames, for partial_test.go's reason and with its standing: the
// corpus was captured without --include-partial-messages, so nothing here is
// backed by a recording. Every shape partialEvent does not recognise yields no
// event at all, so a schema that has moved costs the count and never a turn.

import "testing"

func TestAMessageDeltaCarriesTheTurnsOutputTokens(t *testing.T) {
	evs := decodeOne(t, `{"type":"stream_event","session_id":"s1","parent_tool_use_id":null,"uuid":"u1","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":412}}}`)

	if len(evs) != 1 {
		t.Fatalf("a message_delta produced %d events, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Kind != KindTurnTokens {
		t.Errorf("kind is %v, want %v", ev.Kind, KindTurnTokens)
	}
	if ev.SessionID != "s1" {
		t.Errorf("session is %q, want s1", ev.SessionID)
	}
	if ev.Session == nil || ev.Session.TurnOutputTokens != 412 {
		t.Fatalf("the event carries %+v, want 412 turn output tokens", ev.Session)
	}
	// It is a count and nothing else. Text on it would reach a transcript as a
	// line nobody wrote.
	if ev.Text != "" {
		t.Errorf("the event carries text %q, want none", ev.Text)
	}
}

// A message begins, and the count on the deltas behind it starts again.
//
// The Anthropic streaming docs are explicit on both halves - "one or more
// message_delta events" and "the token counts shown in the usage field of the
// message_delta event are *cumulative*" - so a turn's figure is the sum over
// its **messages**, each message contributing its own last cumulative. Without
// a boundary there is nothing to sum over, and a build that added the deltas up
// reported 250 for a message that produced 150.
func TestAMessageStartIsTheBoundaryTheCountsAreMeasuredBetween(t *testing.T) {
	evs := decodeOne(t, `{"type":"stream_event","session_id":"s1","parent_tool_use_id":null,"uuid":"u1","event":{"type":"message_start","message":{"id":"msg_1","role":"assistant","usage":{"input_tokens":25,"output_tokens":1}}}}`)

	if len(evs) != 1 {
		t.Fatalf("a message_start produced %d events, want 1", len(evs))
	}
	if evs[0].Kind != KindMessageStart {
		t.Errorf("kind is %v, want %v", evs[0].Kind, KindMessageStart)
	}
	if evs[0].SessionID != "s1" {
		t.Errorf("session is %q, want s1", evs[0].SessionID)
	}
	// Its own usage is deliberately not read. output_tokens is 1 or 2 there -
	// the message has produced nothing yet - and taking it would add a token
	// per message to every turn.
	if evs[0].Session != nil {
		t.Errorf("a message_start carries facts %+v, want none: its usage describes a message that has not written anything", evs[0].Session)
	}
}

// A subagent's boundary is not the agent's, the same as its deltas.
func TestASubagentsMessageStartIsNotTheAgentsTurn(t *testing.T) {
	line := `{"type":"stream_event","session_id":"s1","parent_tool_use_id":"toolu_1","event":{"type":"message_start","message":{"id":"msg_1"}}}`
	if evs := decodeOne(t, line); len(evs) != 0 {
		t.Errorf("a subagent's message_start produced %d events: %+v", len(evs), evs)
	}
}

// Every shape this does not recognise is worth no event rather than a zero,
// because a zero is a figure and an absence is not.
func TestAStreamEventWithNoUsableCountProducesNothing(t *testing.T) {
	for _, tc := range []struct{ what, line string }{
		{"a message_delta with no usage", `{"type":"stream_event","session_id":"s1","event":{"type":"message_delta","delta":{"stop_reason":"end_turn"}}}`},
		{"a message_delta whose usage is empty", `{"type":"stream_event","session_id":"s1","event":{"type":"message_delta","usage":{}}}`},
		{"a ping", `{"type":"stream_event","session_id":"s1","event":{"type":"ping"}}`},
		{"a content_block_stop", `{"type":"stream_event","session_id":"s1","event":{"type":"content_block_stop","index":0}}`},
	} {
		if evs := decodeOne(t, tc.line); len(evs) != 0 {
			t.Errorf("%s produced %d events: %+v", tc.what, len(evs), evs)
		}
	}
}

// A subagent's tokens are dropped rather than attributed, which is the rule
// partialEvent already keeps for a subagent's text and fold keeps for its tool
// calls. The turn on screen is the agent's own.
func TestASubagentsMessageDeltaIsNotTheAgentsTurn(t *testing.T) {
	line := `{"type":"stream_event","session_id":"s1","parent_tool_use_id":"toolu_1","event":{"type":"message_delta","usage":{"output_tokens":900}}}`
	if evs := decodeOne(t, line); len(evs) != 0 {
		t.Errorf("a subagent's message_delta produced %d events: %+v", len(evs), evs)
	}
}

// And the text preview is untouched, which is what makes the branch above an
// addition rather than a change.
func TestATextDeltaStillProducesOnlyItsPreview(t *testing.T) {
	evs := decodeOne(t, `{"type":"stream_event","session_id":"s1","parent_tool_use_id":null,"uuid":"u1","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Fixed "}}}`)

	if len(evs) != 1 || evs[0].Kind != KindPartialText || evs[0].Text != "Fixed " {
		t.Fatalf("a text delta produced %+v, want one KindPartialText carrying its text", evs)
	}
	if evs[0].Session != nil {
		t.Errorf("a text delta carries session facts %+v, want none", evs[0].Session)
	}
}
