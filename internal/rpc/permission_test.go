// Answering a permission request over the socket.
//
// The ask reaches a client as an ordinary event frame (event_test.go). This
// file is the other direction: the answer, which is the only client-to-daemon
// verb whose payload differs by which answer it is. An allow carries the input
// the tool will actually receive; a deny carries prose that reaches the model
// verbatim. Neither shape is meaningful on the other, which is why they are
// two kinds rather than one kind with a behavior field.

package rpc

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestAllowFrameCarriesItsCorrelatorsAndUpdatedInput(t *testing.T) {
	// SessionID routes the answer to a process; RequestID says which ask it
	// answers. A daemon holding one session with two outstanding asks needs
	// both, and neither substitutes for the other.
	//
	// UpdatedInput is a tool-input object, so it is the second place in this
	// package - after core.ToolCall.Input - where arbitrary JSON crosses.
	// Nested maps, slices and numbers are the shapes an Edit or a Bash call
	// actually carries, and comparing them with != panics at runtime, so the
	// comparison here is a deep one.
	want := map[string]any{
		"file_path": "/tmp/probe-note.txt",
		"edits": []any{
			map[string]any{"old_string": "a", "new_string": "b"},
		},
		// float64, not int: a JSON number decodes to float64, and a daemon
		// that re-encodes this for claude re-encodes what came back. Written
		// as the type it arrives as rather than the type a caller typed.
		"timeout": float64(30),
	}

	got := roundTrip(t, Frame{
		Kind:         FrameAllow,
		SessionID:    "s-42",
		RequestID:    "req-abc",
		UpdatedInput: want,
	})

	if got.Kind != FrameAllow {
		t.Errorf("Kind = %q, want %q", got.Kind, FrameAllow)
	}
	if got.SessionID != "s-42" {
		t.Errorf("SessionID = %q, want s-42 - the answer cannot reach a process without it", got.SessionID)
	}
	if got.RequestID != "req-abc" {
		t.Errorf("RequestID = %q, want req-abc - nothing correlates the answer without it", got.RequestID)
	}
	if !reflect.DeepEqual(got.UpdatedInput, want) {
		t.Errorf("UpdatedInput = %#v, want %#v", got.UpdatedInput, want)
	}
}

func TestAllowWithNoUpdatedInputPutsNoKeyOnTheWire(t *testing.T) {
	// "Run the tool exactly as it asked" and "run it with an empty input"
	// are different instructions, and only one of them is safe: the findings
	// note records that updatedInput was only ever echoed back unchanged and
	// says in so many words that passing {} was not tested.
	//
	// omitempty collapses both nil and an empty map to an absent key, so this
	// package can never transmit the untested shape. That is protective and
	// it is also easy to undo by accident, so it is pinned: absent means
	// core.EncodeAllow omits the key, which is the recorded-safe behavior.
	var buf bytes.Buffer
	for _, in := range []map[string]any{nil, {}} {
		buf.Reset()
		f := Frame{Kind: FrameAllow, SessionID: "s1", RequestID: "req-1", UpdatedInput: in}
		if err := WriteFrame(&buf, f); err != nil {
			t.Fatalf("WriteFrame(%#v): %v", in, err)
		}
		if strings.Contains(buf.String(), "updated_input") {
			t.Errorf("frame = %s, want no updated_input key for %#v", strings.TrimSpace(buf.String()), in)
		}
	}

	got := roundTrip(t, Frame{Kind: FrameAllow, SessionID: "s1", RequestID: "req-1"})
	if got.UpdatedInput != nil {
		t.Errorf("UpdatedInput = %#v, want nil - an absent key must not decode to an empty map", got.UpdatedInput)
	}
}

func TestDenyFrameCarriesItsReasonVerbatim(t *testing.T) {
	// The reason is not a log line. It reaches the model as the tool result,
	// prefixed with "Error: ", so every byte of it is read by the agent that
	// was refused - newlines, quotes and non-ASCII included. A transport that
	// mangled it would change what the agent is told.
	const reason = "No: that path is outside the worktree.\nTry \"./probe-note.txt\" — relative to the repo root."

	got := roundTrip(t, Frame{
		Kind:      FrameDeny,
		SessionID: "s-42",
		RequestID: "req-abc",
		Reason:    reason,
	})

	if got.Kind != FrameDeny {
		t.Errorf("Kind = %q, want %q", got.Kind, FrameDeny)
	}
	if got.SessionID != "s-42" || got.RequestID != "req-abc" {
		t.Errorf("frame = %+v, want req-abc on s-42", got)
	}
	if got.Reason != reason {
		t.Errorf("Reason = %q, want %q", got.Reason, reason)
	}
}

func TestDenyWithNoReasonStaysEmptyRatherThanInventingOne(t *testing.T) {
	// core.EncodeDeny substitutes defaultDenyReason for a blank one, and it
	// is the only layer that can: it is the only one that knows the wire
	// field is omitempty and that its contents reach the model verbatim.
	//
	// So the transport must hand a blank reason through as blank. Filling one
	// in here would put a second, competing default in a package that is
	// meant to be a dumb pipe, and the two would drift.
	got := roundTrip(t, Frame{Kind: FrameDeny, SessionID: "s1", RequestID: "req-1", Reason: "   "})
	if got.Reason != "   " {
		t.Errorf("Reason = %q, want the blank reason untouched - the substitution belongs to core", got.Reason)
	}
}

func TestTwoAsksOnOneSessionAreAnsweredIndependently(t *testing.T) {
	// The reason correlation is by request id and not by session id. One
	// blocked agent can have more than one ask outstanding, and an answer
	// addressed by session alone could not say which one it answers - it
	// would allow whichever the daemon happened to pick.
	var buf bytes.Buffer
	answers := []Frame{
		{Kind: FrameAllow, SessionID: "s1", RequestID: "req-1"},
		{Kind: FrameDeny, SessionID: "s1", RequestID: "req-2", Reason: "not that file"},
	}
	for _, f := range answers {
		if err := WriteFrame(&buf, f); err != nil {
			t.Fatalf("WriteFrame(%+v): %v", f, err)
		}
	}

	got := readAll(t, bytes.NewReader(buf.Bytes()))
	if len(got) != 2 {
		t.Fatalf("got %d frames, want 2", len(got))
	}
	if got[0].Kind != FrameAllow || got[0].RequestID != "req-1" {
		t.Errorf("first answer = %+v, want an allow for req-1", got[0])
	}
	if got[1].Kind != FrameDeny || got[1].RequestID != "req-2" || got[1].Reason != "not that file" {
		t.Errorf("second answer = %+v, want a deny for req-2", got[1])
	}
}

func TestWriteFrameRefusesUpdatedInputItCannotEncode(t *testing.T) {
	// The mirror of TestWriteFrameRefusesAnEventItCannotEncode. UpdatedInput
	// is the second map[string]any in this package that arrives from outside,
	// so it is the second way a caller can hand the transport something JSON
	// cannot render. It must break loudly with nothing written, rather than
	// emit a partial frame that desyncs the connection for every other
	// session on it.
	var buf bytes.Buffer
	err := WriteFrame(&buf, Frame{
		Kind:         FrameAllow,
		SessionID:    "s1",
		RequestID:    "req-1",
		UpdatedInput: map[string]any{"ch": make(chan int)},
	})
	if err == nil {
		t.Fatal("want an error for an updated input that cannot be encoded")
	}
	if !strings.Contains(err.Error(), "marshal frame") {
		t.Errorf("err = %v, want it to name the marshal step", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes for a frame that failed to encode, want 0", buf.Len())
	}
}

func TestAnswerFramesCarryNoEventAndNoText(t *testing.T) {
	// An answer is not an event and carries no prose beyond its reason. The
	// fields exist on the same flat struct, so this pins that the common
	// answer stays small: four keys on an allow, five on a deny, across a
	// fleet where every ask blocks an agent until one is sent.
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameAllow, SessionID: "s1", RequestID: "req-1"}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	const want = `{"kind":"allow","session_id":"s1","request_id":"req-1"}` + "\n"
	if got := buf.String(); got != want {
		t.Errorf("frame = %q, want %q", got, want)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &keys); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, absent := range []string{"event", "text", "reason", "updated_input"} {
		if _, ok := keys[absent]; ok {
			t.Errorf("answer frame carries a %q key it has no use for", absent)
		}
	}
}

// The ask and its answer are two frames and one exchange, so the shapes are
// checked against each other here rather than only in isolation: whatever a
// client reads off the event is what it has to put on the answer.
func TestAnAskCanBeAnsweredFromTheFieldsItArrivedWith(t *testing.T) {
	ask := roundTrip(t, Frame{
		Kind:      FrameEvent,
		SessionID: "s-42",
		Event: &core.Event{
			Kind:      core.KindPermissionRequest,
			SessionID: "s-42",
			RequestID: "req-abc",
			Tool:      &core.ToolCall{ID: "toolu_9", Name: "Write", Input: map[string]any{"file_path": "note.txt"}},
		},
	})
	if ask.Event == nil {
		t.Fatal("ask dropped on the wire")
	}

	// Built from the ask alone. If this needs anything the ask did not
	// carry, a client cannot answer what it was shown.
	answer := roundTrip(t, Frame{
		Kind:         FrameAllow,
		SessionID:    ask.Event.SessionID,
		RequestID:    ask.Event.RequestID,
		UpdatedInput: ask.Event.Tool.Input,
	})
	if answer.SessionID != "s-42" || answer.RequestID != "req-abc" {
		t.Errorf("answer = %+v, want req-abc on s-42", answer)
	}
	if !reflect.DeepEqual(answer.UpdatedInput, map[string]any{"file_path": "note.txt"}) {
		t.Errorf("UpdatedInput = %#v, want the input the ask carried", answer.UpdatedInput)
	}
}
