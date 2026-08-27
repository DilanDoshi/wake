// What a core.Event loses, or does not lose, crossing the socket. These
// are the fields the layers above depend on and the ones easiest to drop
// silently: RequestID is the only correlator an answer to a permission ask
// has, and Echoed carries the replayed/synthetic mark the App needs for labelling -
// see event.go, which is explicit that nothing may key suppression or
// de-duplication on it. Raw is the one field that deliberately does not
// cross, and that is pinned here too.

package rpc

import (
	"encoding/json"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestEventFieldsSurviveTheWire(t *testing.T) {
	want := &core.Event{
		Kind:      core.KindToolResult,
		SessionID: "s-42",
		RequestID: "req-7",
		Echoed:    true,
		Text:      "ok",
		Tool: &core.ToolCall{
			ID:      "toolu_1",
			Name:    "Bash",
			Input:   map[string]any{"command": "ls", "timeout": 30.0},
			IsError: true,
		},
	}

	got := roundTrip(t, Frame{Kind: FrameEvent, SessionID: "s-42", Event: want}).Event
	if got == nil {
		t.Fatal("event dropped on the wire")
	}
	if got.Kind != want.Kind || got.SessionID != want.SessionID || got.Text != want.Text {
		t.Errorf("event = %+v, want %+v", got, want)
	}
	// RequestID is the only correlator an answer to a permission ask has,
	// and Echoed is the frame's replayed/synthetic mark. Both are easy to drop silently
	// - one is a bool that is absent when false, the other is empty on
	// most kinds - so both are asserted by name rather than by struct
	// comparison.
	if !got.Echoed {
		t.Error("Echoed = false, want true")
	}
	if got.RequestID != want.RequestID {
		t.Errorf("RequestID = %q, want %q", got.RequestID, want.RequestID)
	}
	if got.Tool == nil {
		t.Fatal("Tool dropped on the wire")
	}
	if got.Tool.ID != "toolu_1" || got.Tool.Name != "Bash" || !got.Tool.IsError {
		t.Errorf("Tool = %+v, want %+v", got.Tool, want.Tool)
	}
	if got.Tool.Input["command"] != "ls" || got.Tool.Input["timeout"] != 30.0 {
		t.Errorf("Tool.Input = %#v, want %#v", got.Tool.Input, want.Tool.Input)
	}
}

func TestRawStaysOnTheDaemonSide(t *testing.T) {
	// Raw is 85% of the serialized event stream and no client is allowed to
	// parse it, so core.Event tags it json:"-". The saving is worth nothing
	// if someone restores the tag, so the omission is pinned rather than
	// merely assumed - and the fields beside it are checked in the same
	// breath to prove the event itself still crossed.
	raw := json.RawMessage(`{"type":"assistant","session_id":"s1"}`)
	got := roundTrip(t, Frame{Kind: FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind:      core.KindAssistantText,
		SessionID: "s1",
		Text:      "hello",
		Raw:       raw,
	}}).Event
	if got == nil {
		t.Fatal("event dropped on the wire")
	}
	if got.Text != "hello" {
		t.Fatalf("Text = %q, want %q", got.Text, "hello")
	}
	if got.Raw != nil {
		t.Errorf("Raw = %s, want nil - it must not reach a client", got.Raw)
	}
}

func TestPermissionRequestCarriesBothOfItsCorrelators(t *testing.T) {
	// This replaces TestPermissionRequestCarriesOnlyItsRequestID, which
	// asserted SessionID == "" and described that as correct. It was true
	// of the wire and false of the Event: a can_use_tool control_request
	// carries no session_id in Claude's JSON, but core.Session stamps the
	// pipe it arrived on before the event is ever handed out, because the
	// pipe is the only evidence of which agent is blocked. The old test
	// pinned an unroutable event as the contract.
	//
	// The two ids are not interchangeable and both have to survive.
	// SessionID says which agent is blocked - it is what a roster, a view
	// and the attention model route on. RequestID says which ask is being
	// answered, and is the only thing that correlates an answer: an answer
	// addressed by session alone cannot tell two outstanding asks apart.
	//
	// GUARD, NOT EVIDENCE. This package copies a *core.Event through
	// encoding/json and reads no field of it, so this passes against the
	// unfixed core just as it passes against the fixed one. What it defends
	// is the next change to Event's tags or to Frame - the proof that the
	// attribution happens at all is
	// core.TestPermissionRequestIsAttributedToTheSessionItArrivedOn.
	want := &core.Event{
		Kind:      core.KindPermissionRequest,
		SessionID: "s-42",
		RequestID: "req-abc",
		Tool:      &core.ToolCall{ID: "toolu_9", Name: "Write"},
	}

	got := roundTrip(t, Frame{Kind: FrameEvent, SessionID: "s-42", Event: want}).Event
	if got == nil {
		t.Fatal("event dropped on the wire")
	}
	if got.RequestID != "req-abc" {
		t.Errorf("RequestID = %q, want %q - nothing could correlate the answer", got.RequestID, "req-abc")
	}
	if got.SessionID != "s-42" {
		t.Errorf("SessionID = %q, want %q - nothing could name the blocked agent", got.SessionID, "s-42")
	}
	if got.Kind != core.KindPermissionRequest {
		t.Errorf("Kind = %q, want %q", got.Kind, core.KindPermissionRequest)
	}
}

func TestEchoedFalseSurvivesAsFalse(t *testing.T) {
	// Echoed is omitempty, so false is absent from the JSON. Absent must
	// decode as false rather than as "unknown".
	got := roundTrip(t, Frame{Kind: FrameEvent, Event: &core.Event{
		Kind:   core.KindUserText,
		Echoed: false,
	}}).Event
	if got == nil {
		t.Fatal("event dropped on the wire")
	}
	if got.Echoed {
		t.Error("Echoed = true, want false")
	}
}

func TestFrameWithoutAnEventDecodesToNil(t *testing.T) {
	// A send frame carries no event. It must arrive as a nil pointer, not
	// a zero-valued Event that a consumer would mistake for a real one.
	got := roundTrip(t, Frame{Kind: FrameSend, SessionID: "s1", Text: "hi"})
	if got.Event != nil {
		t.Fatalf("Event = %+v, want nil", got.Event)
	}
	if got.Text != "hi" || got.SessionID != "s1" {
		t.Errorf("frame = %+v, want text hi on s1", got)
	}
}
