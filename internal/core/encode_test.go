// Encoding tests: the outbound half of the airlock - a user turn, and the
// control_response that answers a permission request.
//
// None of these can be a golden. Wake writes these frames and never reads
// them, so control_response appears zero times in the recorded corpus and
// there is nothing to diff against. They assert instead against the shapes
// protocol.go's encoding section marks as probe-derived, and - for the user
// frame, which Wake also decodes - against a round trip through DecodeLine.
//
// This file, protocol_test.go and fixtures_test.go are the airlock's own
// tests, so together they are the only files besides protocol.go that may
// name Claude's frame types - and only ever to prove protocol.go decodes
// and encodes them.

package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestEncodeUserMessage(t *testing.T) {
	got, err := EncodeUserMessage("hello world", nil)
	if err != nil {
		t.Fatalf("EncodeUserMessage: %v", err)
	}
	if n := len(got); n == 0 || got[n-1] != '\n' {
		t.Fatal("encoded line must be newline-terminated")
	}

	var f struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(got[:len(got)-1], &f); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v", err)
	}
	if f.Type != "user" || f.Message.Role != "user" {
		t.Errorf("type/role = %q/%q, want user/user", f.Type, f.Message.Role)
	}
	if len(f.Message.Content) != 1 || f.Message.Content[0].Text != "hello world" {
		t.Errorf("content = %+v, want one text block with the message", f.Message.Content)
	}
}

func TestEncodeUserMessageEscapesNewlinesAndQuotes(t *testing.T) {
	got, err := EncodeUserMessage("line one\nline \"two\"", nil)
	if err != nil {
		t.Fatalf("EncodeUserMessage: %v", err)
	}
	if n := bytes.Count(got, []byte("\n")); n != 1 {
		t.Fatalf("got %d newlines, want 1 (the terminator)", n)
	}
}

// Counting newlines proves the frame survives; it does not prove the text
// does. encoding/json also escapes angle brackets and ampersands by
// default, so a prompt quoting <local-command-stdout> leaves here looking
// nothing like itself and still has to arrive as itself.
func TestEncodeUserMessagePreservesAwkwardTextThroughDecode(t *testing.T) {
	const msg = "line one\nline \"two\" <local-command-stdout> & \\ done"

	line, err := EncodeUserMessage(msg, nil)
	if err != nil {
		t.Fatalf("EncodeUserMessage: %v", err)
	}
	evs, err := DecodeLine(bytes.TrimSuffix(line, []byte("\n")))
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1 - escaping split the frame", len(evs))
	}
	if evs[0].Text != msg {
		t.Errorf("Text = %q, want %q", evs[0].Text, msg)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	const msg = "check the snapshot tests"
	line, err := EncodeUserMessage(msg, nil)
	if err != nil {
		t.Fatalf("EncodeUserMessage: %v", err)
	}
	evs, err := DecodeLine(bytes.TrimSuffix(line, []byte("\n")))
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Text != msg {
		t.Fatalf("round trip lost the message: %+v", evs)
	}
	if evs[0].Kind != KindUserText {
		t.Errorf("Kind = %q, want %q - our own turn is not agent speech", evs[0].Kind, KindUserText)
	}
}

// decodeUserContent pulls the content array out of an encoded user frame so
// the image tests can assert block order and shape.
func decodeUserContent(t *testing.T, line []byte) []map[string]any {
	t.Helper()
	var f struct {
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(bytes.TrimSuffix(line, []byte("\n")), &f); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v", err)
	}
	return f.Message.Content
}

// An image goes first and the text block last: Claude derives the prompt from
// the final block, so a message ending on an image routes wrong.
func TestEncodeUserMessagePutsImagesBeforeText(t *testing.T) {
	img := ImageBlock{MediaType: "image/png", Data: "aGVsbG8="}
	line, err := EncodeUserMessage("what is this?", []ImageBlock{img})
	if err != nil {
		t.Fatalf("EncodeUserMessage: %v", err)
	}
	content := decodeUserContent(t, line)
	if len(content) != 2 {
		t.Fatalf("got %d blocks, want image then text", len(content))
	}
	if content[0]["type"] != "image" {
		t.Errorf("first block type = %v, want image (Claude reads the prompt off the last block)", content[0]["type"])
	}
	if content[1]["type"] != "text" || content[1]["text"] != "what is this?" {
		t.Errorf("last block = %v, want the text", content[1])
	}
	src, ok := content[0]["source"].(map[string]any)
	if !ok {
		t.Fatalf("image block has no source object: %v", content[0])
	}
	if src["type"] != "base64" || src["media_type"] != "image/png" || src["data"] != "aGVsbG8=" {
		t.Errorf("source = %v, want a base64 png carrying the bytes verbatim", src)
	}
}

// An image with no text still sends: the text block is simply absent.
func TestEncodeUserMessageImageOnly(t *testing.T) {
	line, err := EncodeUserMessage("", []ImageBlock{{MediaType: "image/jpeg", Data: "eHg="}})
	if err != nil {
		t.Fatalf("EncodeUserMessage: %v", err)
	}
	content := decodeUserContent(t, line)
	if len(content) != 1 || content[0]["type"] != "image" {
		t.Fatalf("got %v, want exactly one image block", content)
	}
}

// Neither text nor an image is refused rather than sent as an empty content
// array, which Claude silently drops.
func TestEncodeUserMessageRefusesEmpty(t *testing.T) {
	if _, err := EncodeUserMessage("", nil); !errors.Is(err, ErrNotWritten) {
		t.Fatalf("err = %v, want ErrNotWritten - an empty content array is dropped on the floor", err)
	}
}

// The encoded shape matches the recorded fixture Wake would write.
func TestEncodeUserMessageMatchesRecordedInput(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/input/image-block.stdin.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// The fixture is pretty-printed for readability; the wire is compact. Both
	// carry the same fields in the same order, so compacting is all it takes to
	// compare bytes.
	var want bytes.Buffer
	if err := json.Compact(&want, raw); err != nil {
		t.Fatalf("compact fixture: %v", err)
	}
	var wf struct {
		Message struct {
			Content []map[string]any `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(want.Bytes(), &wf); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}
	// Reconstruct the same message from the fixture's own blocks so the test
	// pins block order and source shape rather than the exact 292x57 PNG bytes.
	var text string
	var imgs []ImageBlock
	for _, b := range wf.Message.Content {
		switch b["type"] {
		case "text":
			text, _ = b["text"].(string)
		case "image":
			src, _ := b["source"].(map[string]any)
			mt, _ := src["media_type"].(string)
			data, _ := src["data"].(string)
			imgs = append(imgs, ImageBlock{MediaType: mt, Data: data})
		}
	}
	got, err := EncodeUserMessage(text, imgs)
	if err != nil {
		t.Fatalf("EncodeUserMessage: %v", err)
	}
	if !bytes.Equal(bytes.TrimSuffix(got, []byte("\n")), want.Bytes()) {
		t.Errorf("encoded frame does not match the recorded input fixture\n got: %s\nwant: %s", got, want.Bytes())
	}
}

// A decoded image block off a transcript is a user turn carrying a placeholder,
// not an unknown frame rendering as nothing.
func TestDecodeImageBlockYieldsPlaceholder(t *testing.T) {
	const line = `{"type":"user","message":{"role":"user","content":[` +
		`{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGk="}}]}}`
	evs, err := DecodeLine([]byte(line))
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Kind != KindUserText {
		t.Errorf("Kind = %q, want %q - an image is content on a user turn", evs[0].Kind, KindUserText)
	}
	if evs[0].Text != ImagePlaceholder {
		t.Errorf("Text = %q, want %q - the bytes cannot be drawn, so a placeholder stands in", evs[0].Text, ImagePlaceholder)
	}
}

func TestEncodeDenyCarriesRequestIDAndReason(t *testing.T) {
	got, err := EncodeDeny("req-1", "not this time")
	if err != nil {
		t.Fatalf("EncodeDeny: %v", err)
	}
	if n := bytes.Count(got, []byte("\n")); n != 1 {
		t.Fatalf("got %d newlines, want 1 (the terminator)", n)
	}

	var f struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Response  struct {
				Behavior string `json:"behavior"`
				Message  string `json:"message"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(bytes.TrimSuffix(got, []byte("\n")), &f); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v", err)
	}
	if f.Type != "control_response" {
		t.Errorf("type = %q, want control_response", f.Type)
	}
	if f.Response.RequestID != "req-1" {
		t.Errorf("request_id = %q, want req-1 - the only correlator there is", f.Response.RequestID)
	}
	if f.Response.Response.Behavior != "deny" {
		t.Errorf("behavior = %q, want deny", f.Response.Response.Behavior)
	}
	if f.Response.Response.Message != "not this time" {
		t.Errorf("message = %q, want the reason - it reaches the model verbatim", f.Response.Response.Message)
	}
}

func TestEncodeAllowOmitsUpdatedInputWhenNil(t *testing.T) {
	got, err := EncodeAllow("req-1", nil)
	if err != nil {
		t.Fatalf("EncodeAllow: %v", err)
	}
	if bytes.Contains(got, []byte("updatedInput")) {
		t.Errorf("nil input must be omitted, got %s", got)
	}
	if !bytes.Contains(got, []byte(`"behavior":"allow"`)) {
		t.Errorf("missing allow behavior: %s", got)
	}
}

// updatedInput is the input the tool actually receives, and the probe only
// ever echoed request.input back unchanged - sending {} instead was never
// tested. So the map has to arrive intact, key for key.
func TestEncodeAllowCarriesUpdatedInput(t *testing.T) {
	input := map[string]any{"file_path": "/tmp/probe-note.txt", "content": "ok"}

	got, err := EncodeAllow("req-2", input)
	if err != nil {
		t.Fatalf("EncodeAllow: %v", err)
	}
	var f struct {
		Response struct {
			Response struct {
				UpdatedInput map[string]any `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(bytes.TrimSuffix(got, []byte("\n")), &f); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v", err)
	}
	sent := f.Response.Response.UpdatedInput
	if len(sent) != len(input) {
		t.Fatalf("updatedInput = %+v, want %+v", sent, input)
	}
	for k, want := range input {
		if sent[k] != want {
			t.Errorf("updatedInput[%q] = %v, want %v", k, sent[k], want)
		}
	}
}

// A control frame keeps its subtype and request id one level down, under
// "response" - the same trap the decoder dodges from the other direction.
// Answering at the top level would be answering a protocol nobody speaks,
// and no inbound fixture exists to notice it in.
func TestEncodeControlResponseNestsSubtypeAndRequestID(t *testing.T) {
	allow, err := EncodeAllow("req-3", nil)
	if err != nil {
		t.Fatalf("EncodeAllow: %v", err)
	}
	deny, err := EncodeDeny("req-3", "no")
	if err != nil {
		t.Fatalf("EncodeDeny: %v", err)
	}

	for _, tc := range []struct {
		name     string
		line     []byte
		behavior string
		absent   string
	}{
		{"allow", allow, "allow", "message"},
		{"deny", deny, "deny", "updatedInput"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var top map[string]any
			if err := json.Unmarshal(bytes.TrimSuffix(tc.line, []byte("\n")), &top); err != nil {
				t.Fatalf("encoded line is not valid JSON: %v", err)
			}
			for _, k := range []string{"subtype", "request_id"} {
				if _, ok := top[k]; ok {
					t.Errorf("%q is at the top level; it belongs under response", k)
				}
			}
			body, ok := top["response"].(map[string]any)
			if !ok {
				t.Fatalf("response = %T, want an object", top["response"])
			}
			if body["subtype"] != "success" {
				t.Errorf("response.subtype = %v, want success", body["subtype"])
			}
			if body["request_id"] != "req-3" {
				t.Errorf("response.request_id = %v, want req-3", body["request_id"])
			}
			decision, ok := body["response"].(map[string]any)
			if !ok {
				t.Fatalf("response.response = %T, want an object", body["response"])
			}
			if decision["behavior"] != tc.behavior {
				t.Errorf("behavior = %v, want %v", decision["behavior"], tc.behavior)
			}
			if _, ok := decision[tc.absent]; ok {
				t.Errorf("%q must not ride on a %s", tc.absent, tc.behavior)
			}
		})
	}
}

func TestEncodeControlResponseRejectsAnEmptyRequestID(t *testing.T) {
	if _, err := EncodeAllow("", nil); err == nil {
		t.Fatal("want error for an empty request id, got nil")
	}
}

func TestEncodeDenyRejectsAnEmptyRequestID(t *testing.T) {
	if _, err := EncodeDeny("", "no"); err == nil {
		t.Fatal("want error for an empty request id, got nil")
	}
}

// A blank reason is the one denial that must not go out as written. An
// empty one is dropped by omitempty; a whitespace-only one is worse, since
// it survives omitempty and reaches the model as "Error:    ". Either way
// the agent learns it was refused but not what to do instead, and the
// likeliest next move from an unexplained refusal is to retry the identical
// call - live-locking the one path in the protocol that blocks the process.
// Unlike an empty request id, the frame is well-formed and does unblock the
// process, so refusing to build it would leave the caller holding a blocked
// process and nothing to send it. It gets a default instead.
//
// Whitespace is not a hypothetical: the likeliest caller is a UI text field
// where the operator hits space and then enter.
func TestEncodeDenyWithoutAReasonStillExplainsItself(t *testing.T) {
	if strings.TrimSpace(defaultDenyReason) == "" {
		t.Fatal("the default reason is the only thing keeping message on the wire")
	}

	for _, reason := range []string{"", " ", "   ", "\t\n "} {
		t.Run(strconv.Quote(reason), func(t *testing.T) {
			got, err := EncodeDeny("req-5", reason)
			if err != nil {
				t.Fatalf("EncodeDeny: %v", err)
			}
			var f struct {
				Response struct {
					Response struct {
						Behavior string `json:"behavior"`
						Message  string `json:"message"`
					} `json:"response"`
				} `json:"response"`
			}
			if err := json.Unmarshal(bytes.TrimSuffix(got, []byte("\n")), &f); err != nil {
				t.Fatalf("encoded line is not valid JSON: %v", err)
			}
			if f.Response.Response.Behavior != "deny" {
				t.Errorf("behavior = %q, want deny", f.Response.Response.Behavior)
			}
			if f.Response.Response.Message != defaultDenyReason {
				t.Errorf("message = %q, want the default reason %q", f.Response.Response.Message, defaultDenyReason)
			}
		})
	}
}

// The trim decides only whether a reason is blank. A reason that has any
// content reaches the model exactly as the operator wrote it, surrounding
// space and all - "echoed verbatim" is the whole reason this field is worth
// writing carefully.
func TestEncodeDenyDoesNotTrimARealReason(t *testing.T) {
	const reason = "  no: that path is shared with the release branch  "

	got, err := EncodeDeny("req-6", reason)
	if err != nil {
		t.Fatalf("EncodeDeny: %v", err)
	}
	var f struct {
		Response struct {
			Response struct {
				Message string `json:"message"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(bytes.TrimSuffix(got, []byte("\n")), &f); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v", err)
	}
	if f.Response.Response.Message != reason {
		t.Errorf("message = %q, want %q unchanged", f.Response.Response.Message, reason)
	}
}

// updatedInput crosses into the airlock from outside - a human editing a
// tool's arguments before approving them. A value encoding/json cannot
// render has to surface as an error here, never as a half-written line on
// the stdin of a process blocked waiting for exactly one answer.
func TestEncodeAllowReportsUnencodableInput(t *testing.T) {
	got, err := EncodeAllow("req-4", map[string]any{"ch": make(chan int)})
	if err == nil {
		t.Fatal("want an error for an unencodable updatedInput, got nil")
	}
	if got != nil {
		t.Errorf("got %q alongside the error, want no line at all", got)
	}
}

// --- EncodeInterrupt ---------------------------------------------------
//
// The outbound half of a control_request rather than a control_response:
// Wake initiating rather than answering. request_id sits on the envelope
// here, the same place the inbound can_use_tool ask carries it and the
// opposite of where a control_response nests it one level under "response".

// TestEncodeInterruptEnvelopeShape pins the exact bytes against the recorded
// wire shape (docs/superpowers/notes/2026-08-08-interrupt-findings.md §2).
// The request id is not arbitrary: it is the one this exact interrupt's
// receipt echoes back at testdata/stream/interrupt-mid-tool.jsonl:50, so a
// mismatch here would mean the encoder and the recording have drifted.
//
// This compares against a hand-written literal, not a struct round trip -
// round-tripping through the same struct on both ends cannot catch
// request_id landing one level off, since a symmetrically wrong nesting
// would decode back out again just fine.
func TestEncodeInterruptEnvelopeShape(t *testing.T) {
	got, err := EncodeInterrupt("1fc6c7cc-7865-4242-a251-e76e78a7fc15", false)
	if err != nil {
		t.Fatalf("EncodeInterrupt: %v", err)
	}
	want := `{"type":"control_request","request_id":"1fc6c7cc-7865-4242-a251-e76e78a7fc15","request":{"subtype":"interrupt"}}` + "\n"
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// TestEncodeInterruptWithCancelQueuedEnvelopeShape is the cancel_queued
// sibling, pinned against the request id its own receipt echoes back at
// testdata/stream/interrupt-cancel-queued.jsonl:14.
func TestEncodeInterruptWithCancelQueuedEnvelopeShape(t *testing.T) {
	got, err := EncodeInterrupt("369900e4-8ebf-45c0-9406-cb447c161499", true)
	if err != nil {
		t.Fatalf("EncodeInterrupt: %v", err)
	}
	want := `{"type":"control_request","request_id":"369900e4-8ebf-45c0-9406-cb447c161499","request":{"subtype":"interrupt","cancel_queued":true}}` + "\n"
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// TestEncodeInterruptRequestIDIsOnEnvelopeNotNested targets the one-level-off
// bug class directly, independent of the exact-bytes tests above: request_id
// must be a sibling of "request", never a key inside it.
func TestEncodeInterruptRequestIDIsOnEnvelopeNotNested(t *testing.T) {
	got, err := EncodeInterrupt("req-interrupt-1", false)
	if err != nil {
		t.Fatalf("EncodeInterrupt: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(bytes.TrimSuffix(got, []byte("\n")), &top); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v", err)
	}
	if top["request_id"] != "req-interrupt-1" {
		t.Errorf("top-level request_id = %v, want req-interrupt-1", top["request_id"])
	}
	body, ok := top["request"].(map[string]any)
	if !ok {
		t.Fatalf("request = %T, want an object", top["request"])
	}
	if _, ok := body["request_id"]; ok {
		t.Error(`"request_id" is nested inside "request"; it belongs on the envelope`)
	}
	if body["subtype"] != "interrupt" {
		t.Errorf("request.subtype = %v, want interrupt", body["subtype"])
	}
}

// TestEncodeInterruptCancelQueuedAbsentWhenNotAsked is half of the
// present/absent pair omitempty is supposed to buy: a plain interrupt must
// not carry cancel_queued:false. ControlResult's doc comment explains why the
// receipt's own cancelled key tracks presence rather than truth; an
// always-present false on the way out would erase the distinction the
// receipt goes out of its way to preserve on the way back.
func TestEncodeInterruptCancelQueuedAbsentWhenNotAsked(t *testing.T) {
	got, err := EncodeInterrupt("req-interrupt-2", false)
	if err != nil {
		t.Fatalf("EncodeInterrupt: %v", err)
	}
	if bytes.Contains(got, []byte("cancel_queued")) {
		t.Errorf("cancel_queued must be absent, not false, when not asked for: %s", got)
	}
}

// TestEncodeInterruptCancelQueuedPresentAndTrueWhenAsked is the other half:
// asking for it must put the literal true on the wire, nested beside
// subtype.
func TestEncodeInterruptCancelQueuedPresentAndTrueWhenAsked(t *testing.T) {
	got, err := EncodeInterrupt("req-interrupt-3", true)
	if err != nil {
		t.Fatalf("EncodeInterrupt: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(bytes.TrimSuffix(got, []byte("\n")), &top); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v", err)
	}
	body, ok := top["request"].(map[string]any)
	if !ok {
		t.Fatalf("request = %T, want an object", top["request"])
	}
	cq, ok := body["cancel_queued"]
	if !ok {
		t.Fatal("cancel_queued is absent, want present and true")
	}
	if cq != true {
		t.Errorf("cancel_queued = %v, want true", cq)
	}
}

// TestEncodeInterruptRejectsAnEmptyRequestID proves the chosen behaviour for
// the trap in interrupt-no-request-id.jsonl: the CLI accepts a blank
// request_id and aborts the turn anyway, but the receipt then carries no
// request_id either, so it cannot be matched back to the interrupt that
// caused it. This refuses to build the frame at all rather than send one
// whose receipt would be unattributable - the same non-empty check
// encodeControlResponse already makes for the answer direction.
func TestEncodeInterruptRejectsAnEmptyRequestID(t *testing.T) {
	got, err := EncodeInterrupt("", false)
	if err == nil {
		t.Fatal("want error for an empty request id, got nil")
	}
	if got != nil {
		t.Errorf("got %q alongside the error, want no line at all", got)
	}
}

// --- set_permission_mode -----------------------------------------------------
//
// The shape is transcribed by hand from Claude Code 2.1.228
// (docs/superpowers/notes/2026-08-12-permission-mode-findings.md §2) and
// confirmed by the recordings §4 drove with it. ultraplan is deliberately not
// a field: it is in the binary's request shape, was never sent and never
// recorded, so it is a guess rather than a feature.

// TestEncodeSetModeEnvelopeShape pins the exact bytes, against the request the
// spike's driver wrote at findings §4 - the one whose receipt came back
// {"mode":"acceptEdits"} at mode-set.jsonl:29.
func TestEncodeSetModeEnvelopeShape(t *testing.T) {
	got, err := EncodeSetMode("ee9cab7f-1d3a-4b2b-9c5e-1f0a2b3c4d5e", "acceptEdits")
	if err != nil {
		t.Fatalf("EncodeSetMode: %v", err)
	}
	want := `{"type":"control_request","request_id":"ee9cab7f-1d3a-4b2b-9c5e-1f0a2b3c4d5e","request":{"subtype":"set_permission_mode","mode":"acceptEdits"}}` + "\n"
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

// TestEncodeSetModeRequestIDIsOnEnvelopeNotNested is EncodeInterrupt's
// one-level-off guard for the second control_request Wake sends. A
// control_request nests once; only the response nests twice.
func TestEncodeSetModeRequestIDIsOnEnvelopeNotNested(t *testing.T) {
	got, err := EncodeSetMode("req-mode-1", "plan")
	if err != nil {
		t.Fatalf("EncodeSetMode: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(bytes.TrimSuffix(got, []byte("\n")), &top); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v", err)
	}
	if top["request_id"] != "req-mode-1" {
		t.Errorf("top-level request_id = %v, want req-mode-1", top["request_id"])
	}
	body, ok := top["request"].(map[string]any)
	if !ok {
		t.Fatalf("request = %T, want an object", top["request"])
	}
	if _, ok := body["request_id"]; ok {
		t.Error(`"request_id" is nested inside "request"; it belongs on the envelope`)
	}
	if body["subtype"] != "set_permission_mode" {
		t.Errorf("request.subtype = %v, want set_permission_mode", body["subtype"])
	}
	if body["mode"] != "plan" {
		t.Errorf("request.mode = %v, want plan", body["mode"])
	}
}

// TestEncodeSetModeCarriesNoUltraplan holds the line the findings note draws:
// ultraplan is in the binary's request shape and in no recording, so Wake does
// not send it.
func TestEncodeSetModeCarriesNoUltraplan(t *testing.T) {
	got, err := EncodeSetMode("req-mode-2", "auto")
	if err != nil {
		t.Fatalf("EncodeSetMode: %v", err)
	}
	if bytes.Contains(got, []byte("ultraplan")) {
		t.Errorf("got %s, want no ultraplan key", got)
	}
}

// TestEncodeSetModeRejectsAnEmptyRequestID is EncodeInterrupt's rule for the
// same reason: a receipt naming no request cannot be attributed across 15-30
// sessions, and this receipt is the only authority on what the mode became.
func TestEncodeSetModeRejectsAnEmptyRequestID(t *testing.T) {
	got, err := EncodeSetMode("", "plan")
	if err == nil {
		t.Fatal("want error for an empty request id, got nil")
	}
	if got != nil {
		t.Errorf("got %q alongside the error, want no line at all", got)
	}
}

// TestEncodeSetModeRejectsAnEmptyMode refuses the frame rather than letting an
// empty mode reach the CLI, which would answer with an error receipt naming a
// mode nobody asked for. An empty mode would have to mean either "leave it" or
// "reset it", and both readings are wrong - the same argument that makes
// rpc.FrameMode a kind rather than a field on an existing frame.
func TestEncodeSetModeRejectsAnEmptyMode(t *testing.T) {
	got, err := EncodeSetMode("req-mode-3", "")
	if err == nil {
		t.Fatal("want error for an empty mode, got nil")
	}
	if got != nil {
		t.Errorf("got %q alongside the error, want no line at all", got)
	}
}
