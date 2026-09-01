// Unit-level decoding tests: one wire shape per test, built from literals
// so each failure names exactly one decision. The golden pass over the
// recorded corpus lives in fixtures_test.go, and the outbound half of the
// airlock is tested in encode_test.go.
//
// Split at 1153 lines against this project's 800-line hard max - the limit
// that split the airlock itself and then fixtures_test.go, and it applies to
// tests too. The subagent dimension went to protocol_subagent_test.go and
// Claude's resolved vocabulary to protocol_vocabulary_test.go, by subject
// rather than by size; this file keeps the frame-by-frame decode tests, the
// degradation rulings, and the shared helpers at the bottom.
//
// This file, protocol_subagent_test.go, protocol_vocabulary_test.go,
// fixtures_test.go, fixtures_subagent_test.go, fixtures_helpers_test.go,
// encode_test.go and airlock_test.go are the airlock's own tests, so together
// they are the only files besides the airlock itself that may name Claude's
// frame types - and only ever to prove it decodes and encodes them.
// session_test.go and interrupt_test.go are the further exceptions, for the
// same narrow reason: their fake processes have to speak the wire to prove
// session.go never does.

package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeAssistantText(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"text","text":"hello"}]},"parent_tool_use_id":null,"request_id":"req_1","session_id":"s1","uuid":"u1","timestamp":"2026-08-09T00:00:00.000Z"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("got %d events, want 1", len(evs))
	}
	if evs[0].Kind != KindAssistantText {
		t.Errorf("Kind = %q, want %q", evs[0].Kind, KindAssistantText)
	}
	if evs[0].Text != "hello" {
		t.Errorf("Text = %q, want %q", evs[0].Text, "hello")
	}
	if evs[0].SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", evs[0].SessionID, "s1")
	}
	// An assistant frame's request_id is the Anthropic API request, not a
	// control-protocol correlator. Leaking it would break the permission flow.
	if evs[0].RequestID != "" {
		t.Errorf("RequestID = %q, want empty on an assistant frame", evs[0].RequestID)
	}
}

// An assistant frame carries the real output-token count for its message in
// message.usage.output_tokens; the decoded assistant-text event carries it, so
// a surface can report how much a response ran to rather than counting words.
func TestAssistantTextCarriesItsOutputTokens(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"id":"m","role":"assistant","usage":{"output_tokens":210},"content":[{"type":"text","text":"a long report"}]},"session_id":"s1"}`)
	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindAssistantText {
		t.Fatalf("got %d events, want one assistant_text", len(evs))
	}
	if evs[0].OutputTokens != 210 {
		t.Errorf("OutputTokens = %d, want 210", evs[0].OutputTokens)
	}
}

// output_tokens is the whole message's count, so it attaches to exactly one
// text block, never once per block - two text blocks must not each claim the
// message total.
func TestOutputTokensAttachToOneTextBlockNotEvery(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"id":"m","role":"assistant","usage":{"output_tokens":50},"content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]},"session_id":"s1"}`)
	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	var total int
	for _, e := range evs {
		total += e.OutputTokens
	}
	if total != 50 {
		t.Errorf("summed OutputTokens across blocks = %d, want 50 (attached once)", total)
	}
}

// A malformed usage on an assistant frame must not cost the prose beside it:
// usage is decoded separately, so a bad one leaves OutputTokens 0 and the text
// intact rather than collapsing the whole frame to KindUnknown - the
// all-on-nothing hazard messageEvents decodes content block-by-block to avoid.
func TestAMalformedUsageDoesNotDiscardTheProse(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"role":"assistant","usage":"broken","content":[{"type":"text","text":"still here"}]},"session_id":"s1"}`)
	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindAssistantText {
		t.Fatalf("got %d events, want one assistant_text - a bad usage collapsed the prose", len(evs))
	}
	if evs[0].Text != "still here" {
		t.Errorf("Text = %q, want %q", evs[0].Text, "still here")
	}
	if evs[0].OutputTokens != 0 {
		t.Errorf("OutputTokens = %d, want 0 on a malformed usage", evs[0].OutputTokens)
	}
}

func TestDecodeThinkingBlockIgnoresSignature(t *testing.T) {
	line := []byte(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"weighing it","signature":"EssMCokBCBAYAipA"}]}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindThinking {
		t.Fatalf("got %+v, want one KindThinking", evs)
	}
	if evs[0].Text != "weighing it" {
		t.Errorf("Text = %q, want %q", evs[0].Text, "weighing it")
	}
}

func TestDecodeToolUse(t *testing.T) {
	line := []byte(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"},"caller":{"type":"direct"}}]}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindToolUse {
		t.Fatalf("got %+v, want one KindToolUse", evs)
	}
	if evs[0].Tool == nil {
		t.Fatal("Tool is nil")
	}
	if evs[0].Tool.Name != "Bash" {
		t.Errorf("Tool.Name = %q, want %q", evs[0].Tool.Name, "Bash")
	}
	if got := evs[0].Tool.Input["command"]; got != "ls" {
		t.Errorf("Tool.Input[command] = %v, want %q", got, "ls")
	}
}

// Divergence 3: tool_result.content is a string, not a nested block array.
// Decoding it as raw JSON leaves the quotes in the transcript.
func TestDecodeToolResultContentIsAPlainString(t *testing.T) {
	line := []byte(`{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"wake-spike-ok","is_error":false}]}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindToolResult {
		t.Fatalf("got %+v, want one KindToolResult", evs)
	}
	if evs[0].Text != "wake-spike-ok" {
		t.Errorf("Text = %q, want %q (unquoted)", evs[0].Text, "wake-spike-ok")
	}
	if evs[0].Tool == nil || evs[0].Tool.ID != "toolu_1" {
		t.Fatalf("tool_use_id not carried: %+v", evs[0].Tool)
	}
	if evs[0].Tool.IsError {
		t.Error("IsError = true, want false")
	}
}

func TestDecodeToolResultCarriesIsError(t *testing.T) {
	line := []byte(`{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"spike: denied by probe","is_error":true}]}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if evs[0].Tool == nil || !evs[0].Tool.IsError {
		t.Fatalf("IsError not carried: %+v", evs[0].Tool)
	}
}

func TestDecodeMultipleBlocksInOneFrame(t *testing.T) {
	line := []byte(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"running it"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	if evs[0].Kind != KindAssistantText || evs[1].Kind != KindToolUse {
		t.Errorf("kinds = %q,%q; want %q,%q", evs[0].Kind, evs[1].Kind, KindAssistantText, KindToolUse)
	}
}

// Divergence 1: message is a bare string on system/permission_denied.
// A struct-typed Message field loses this frame entirely.
func TestDecodeSystemFrameWithStringMessage(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"permission_denied","tool_name":"Write","tool_use_id":"toolu_1","message":"Claude requested permissions to write, but you haven't granted it yet.","uuid":"u1","session_id":"s1"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("a string message must not fail the frame: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindSystem {
		t.Fatalf("got %+v, want one KindSystem", evs)
	}
	if evs[0].Text != "permission_denied" {
		t.Errorf("Text = %q, want the subtype %q", evs[0].Text, "permission_denied")
	}
}

// Divergence 2: message.content is a bare string on compaction summaries
// and <local-command-stdout> frames.
func TestDecodeUserFrameWithStringContent(t *testing.T) {
	line := []byte(`{"type":"user","message":{"role":"user","content":"<local-command-stdout>Compacted </local-command-stdout>"},"isReplay":true,"parent_tool_use_id":null,"session_id":"s1","uuid":"u1","timestamp":"2026-08-09T00:00:00.000Z"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("a string content must not fail the frame: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindUserText {
		t.Fatalf("got %+v, want one KindUserText", evs)
	}
	if !strings.Contains(evs[0].Text, "Compacted") {
		t.Errorf("Text = %q, want the string content", evs[0].Text)
	}
}

// The permission request. It blocks the process until answered, is the only
// frame with no session_id - so RequestID is the only correlator - and has
// no top-level subtype either: the subtype is nested inside "request". A
// decoder reading the shared top-level Subtype field here sees "" and
// demotes the most important frame Wake decodes to an unrecognized control
// frame. This literal deliberately has no top-level subtype.
func TestDecodeControlRequestIsAPermissionRequest(t *testing.T) {
	line := []byte(`{"type":"control_request","request_id":"a059b289","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"/tmp/probe-note.txt","content":"ok"},"description":"probe-note.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_1"}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindPermissionRequest {
		t.Fatalf("got %+v, want one KindPermissionRequest", evs)
	}
	if evs[0].RequestID != "a059b289" {
		t.Errorf("RequestID = %q, want %q", evs[0].RequestID, "a059b289")
	}
	if evs[0].SessionID != "" {
		t.Errorf("SessionID = %q, want empty - the frame carries none", evs[0].SessionID)
	}
	if evs[0].Tool == nil || evs[0].Tool.Name != "Write" || evs[0].Tool.ID != "toolu_1" {
		t.Fatalf("tool not carried: %+v", evs[0].Tool)
	}
	if got := evs[0].Tool.Input["file_path"]; got != "/tmp/probe-note.txt" {
		t.Errorf("Input[file_path] = %v, want the probe path", got)
	}
}

func TestDecodeUnansweredControlSubtypeKeepsItsRequestID(t *testing.T) {
	// Not can_use_tool, but still blocking. Phase 1 cannot answer it; the
	// correlator has to survive anyway so Phase 2 can.
	line := []byte(`{"type":"control_request","request_id":"r2","request":{"subtype":"some_future_control"}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if evs[0].Kind != KindUnknown {
		t.Errorf("Kind = %q, want %q", evs[0].Kind, KindUnknown)
	}
	if evs[0].RequestID != "r2" {
		t.Errorf("RequestID = %q, want %q", evs[0].RequestID, "r2")
	}
}

// The receipt for a control_request Wake sent. Its subtype AND its
// request_id are both nested inside "response", and there is a second
// nesting under that for the payload. The frame carries no session_id, so a
// decoder that reads the top level gets nothing at all: no correlator, no
// subtype, and an interrupt that is never acknowledged.
func TestDecodeControlResponseIsAReceipt(t *testing.T) {
	line := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"369900e4","response":{"still_queued":[],"cancelled":["c07ca05a"]}}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindControlReceipt {
		t.Fatalf("got %+v, want one KindControlReceipt", evs)
	}
	if evs[0].RequestID != "369900e4" {
		t.Errorf("RequestID = %q, want the nested %q", evs[0].RequestID, "369900e4")
	}
	if evs[0].SessionID != "" {
		t.Errorf("SessionID = %q, want empty - the frame carries none", evs[0].SessionID)
	}
	if evs[0].Text != "success" {
		t.Errorf("Text = %q, want the nested subtype", evs[0].Text)
	}
	if evs[0].Control == nil {
		t.Fatal("Control is nil: the doubly-nested payload was dropped")
	}
	if len(evs[0].Control.Cancelled) != 1 || evs[0].Control.Cancelled[0] != "c07ca05a" {
		t.Errorf("Cancelled = %v, want the destroyed uuid", evs[0].Control.Cancelled)
	}
	if evs[0].Control.StillQueued == nil {
		t.Error("StillQueued = nil, want present-and-empty: the key was on the wire")
	}
}

// Only the interrupt receipt's payload has ever been recorded. A receipt for
// some other control_request must still reach Wake with its correlator, or
// the request it answers hangs forever - the same rule
// TestDecodeUnansweredControlSubtypeKeepsItsRequestID states inbound.
func TestDecodeControlReceiptWithoutAPayloadKeepsItsCorrelator(t *testing.T) {
	line := []byte(`{"type":"control_response","response":{"subtype":"some_future_subtype","request_id":"r9"}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindControlReceipt {
		t.Fatalf("got %+v, want one KindControlReceipt", evs)
	}
	if evs[0].RequestID != "r9" {
		t.Errorf("RequestID = %q, want %q", evs[0].RequestID, "r9")
	}
	if evs[0].Text != "some_future_subtype" {
		t.Errorf("Text = %q, want the subtype so it is identifiable", evs[0].Text)
	}
	if evs[0].Control == nil {
		t.Fatal("Control is nil")
	}
	if evs[0].Control.StillQueued != nil || evs[0].Control.Cancelled != nil {
		t.Errorf("Control = %+v, want both arrays nil - neither key was on the wire", evs[0].Control)
	}
}

// TestDecodeSetModeReceiptCarriesTheModeItLandedOn reads the receipt that is
// the whole authority on what a mode change became - mode-set.jsonl:29,
// verbatim. Without this the mode is a fact only the CLI knows.
func TestDecodeSetModeReceiptCarriesTheModeItLandedOn(t *testing.T) {
	line := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"spike-ee9cab7f-5361-4695-bb9d-0b7b73003e18","response":{"mode":"acceptEdits"}}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindControlReceipt {
		t.Fatalf("got %+v, want one KindControlReceipt", evs)
	}
	if evs[0].PermissionMode != "acceptEdits" {
		t.Errorf("PermissionMode = %q, want the doubly-nested %q", evs[0].PermissionMode, "acceptEdits")
	}
	if evs[0].Control == nil || evs[0].Control.Error != "" {
		t.Errorf("Control = %+v, want no error on a success", evs[0].Control)
	}
}

// TestDecodeSetModeReceiptReportsANormalizedMode is findings §6's trap, which
// is the one that will be got wrong: `manual` is accepted and silently becomes
// `default`. A label built on the string that was *sent* is wrong here, and
// wrong on a cycle position rather than only in principle.
// mode-vocabulary.jsonl:5, which asked for manual.
func TestDecodeSetModeReceiptReportsANormalizedMode(t *testing.T) {
	line := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"spike-bf805f80-aa6e-4a54-97a7-b3d90c651686","response":{"mode":"default"}}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if evs[0].PermissionMode != "default" {
		t.Errorf("PermissionMode = %q, want default - manual was asked for and normalized", evs[0].PermissionMode)
	}
}

// TestDecodeSetModeRefusalCarriesItsReason pins the other receipt shape: a
// refusal is subtype "error" with a **top-level** error string, not a success
// carrying a failure. mode-vocabulary.jsonl:8, the bypassPermissions refusal
// findings §7 calls a real safety property.
//
// The reason has to survive decode or an operator whose key was refused is
// told only that something failed - which is the silence §4 of the design
// forbids.
func TestDecodeSetModeRefusalCarriesItsReason(t *testing.T) {
	line := []byte(`{"type":"control_response","response":{"subtype":"error","request_id":"spike-938807cb-86b4-49d2-be03-436b74f58934","error":"Cannot set permission mode to bypassPermissions because the session was not launched with --dangerously-skip-permissions"}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindControlReceipt {
		t.Fatalf("got %+v, want one KindControlReceipt", evs)
	}
	if evs[0].Text != "error" {
		t.Errorf("Text = %q, want the subtype error", evs[0].Text)
	}
	if evs[0].RequestID != "spike-938807cb-86b4-49d2-be03-436b74f58934" {
		t.Errorf("RequestID = %q, want the refusal attributable", evs[0].RequestID)
	}
	if evs[0].Control == nil {
		t.Fatal("Control is nil: the refusal's reason was dropped")
	}
	if !strings.Contains(evs[0].Control.Error, "--dangerously-skip-permissions") {
		t.Errorf("Control.Error = %q, want the CLI's reason", evs[0].Control.Error)
	}
	if evs[0].PermissionMode != "" {
		t.Errorf("PermissionMode = %q, want empty - a refusal moved nothing", evs[0].PermissionMode)
	}
}

// TestDecodeInitCarriesThePermissionMode is the second observable findings §10
// names, and the only one that can see a mode Wake did not ask for: Path B
// changes the mode from inside a permission allow and produces **no receipt**
// at all (§5). It arrives on every turn and costs nothing to read.
//
// At spawn it is normalized rather than an echo of the flag - the existing
// rule, unchanged - so this decodes it and rules nothing about what it means.
func TestDecodeInitCarriesThePermissionMode(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"init","session_id":"s1","permissionMode":"plan"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindSystem {
		t.Fatalf("got %+v, want one KindSystem", evs)
	}
	if evs[0].PermissionMode != "plan" {
		t.Errorf("PermissionMode = %q, want plan", evs[0].PermissionMode)
	}
}

// A withdrawal names one request and that is the whole frame, so a withdrawal
// naming nothing is the shape worth pinning: it must still arrive as a
// withdrawal, and it must arrive **empty**.
//
// The empty half is the load-bearing one and it is a rule for the consumer, not
// for this decoder: a client retires an ask by matching RequestID, so an id
// invented here - the session's, the last ask's, anything - would retire an ask
// that is still live and take a prompt off the screen of an agent that is still
// blocked on it. Reporting the withdrawal with nothing in it says "something was
// withdrawn and this frame does not say what", which is exactly what was on the
// wire and is the safe direction.
//
// This shape is not recorded. Both recorded withdrawals name their ask; the
// ruling here is the one messageStateEvent and rateLimitEvent already get - a
// frame whose identity is its own top-level type is not degraded by an
// unreadable payload - and it is stated because the alternative is a decoder
// that drops the frame and leaves the daemon's ask outstanding forever.
func TestAWithdrawalThatNamesNoRequestIsStillAWithdrawalAndStillEmpty(t *testing.T) {
	evs, err := DecodeLine([]byte(`{"type":"control_cancel_request"}`))
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindRequestWithdrawn {
		t.Fatalf("got %+v, want one KindRequestWithdrawn", evs)
	}
	if evs[0].RequestID != "" {
		t.Errorf("RequestID = %q, want empty - the frame named nothing, and an id invented here retires an ask that is still live", evs[0].RequestID)
	}
}

// ControlResult says an absent "cancelled" and a present-but-empty one are
// different facts - the first means Wake did not ask, the second that it
// asked and nothing was queued. Nothing above the airlock may re-read
// Event.Raw, so that distinction is only usable if it survives serialization
// to a client. omitempty on either field would silently collapse it, which
// is a one-word edit nobody would question.
func TestControlResultKeepsAbsentAndEmptyApartOnTheWire(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		marker  string
		want    string
	}{
		{
			name:    "a plain interrupt never asked",
			fixture: "interrupt-mid-tool.jsonl",
			marker:  `"request_id":"1fc6c7cc-7865-4242-a251-e76e78a7fc15"`,
			want:    `"cancelled":null`,
		},
		{
			name:    "cancel_queued asked, nothing was queued",
			fixture: "interrupt-cancel-queued-empty.jsonl",
			marker:  `"still_queued":[],"cancelled":[]`,
			want:    `"cancelled":[]`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			line, n := findFixtureLine(t, c.fixture, c.marker)
			b, err := json.Marshal(onlyEvent(t, line, n))
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			if !strings.Contains(string(b), c.want) {
				t.Errorf("serialized event = %s, want it to contain %s", b, c.want)
			}
		})
	}
}

// The fate of a message Wake sent. command_uuid is the uuid Wake stamped;
// the frame's own uuid identifies the frame and is dropped.
func TestDecodeCommandLifecycle(t *testing.T) {
	line := []byte(`{"type":"command_lifecycle","command_uuid":"5490097f","state":"queued","uuid":"779b76c0","session_id":"s1"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindMessageState {
		t.Fatalf("got %+v, want one KindMessageState", evs)
	}
	if evs[0].MessageID != "5490097f" {
		t.Errorf("MessageID = %q, want the command_uuid, not the frame uuid", evs[0].MessageID)
	}
	if evs[0].Text != "queued" {
		t.Errorf("Text = %q, want the state", evs[0].Text)
	}
	if evs[0].SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", evs[0].SessionID, "s1")
	}
}

// The state set is open. "discarded" is in the 2.1.226 binary's enum and no
// recording has produced one, so it is the honest test for a state Wake has
// never seen: the frame is one Wake models, and demoting it to KindUnknown
// would lose a message's fate to a vocabulary change.
func TestDecodeCommandLifecycleAcceptsAnUnrecordedState(t *testing.T) {
	evs, err := DecodeLine([]byte(`{"type":"command_lifecycle","command_uuid":"c1","state":"discarded","session_id":"s1"}`))
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindMessageState {
		t.Fatalf("got %+v, want one KindMessageState", evs)
	}
	if evs[0].Text != "discarded" {
		t.Errorf("Text = %q, want the unrecognized state carried through", evs[0].Text)
	}
}

func TestDecodeRateLimitEvent(t *testing.T) {
	line := []byte(`{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1786249200,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false},"uuid":"u1","session_id":"s1"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindRateLimit {
		t.Fatalf("got %+v, want one KindRateLimit", evs)
	}
	if evs[0].Text != "allowed" {
		t.Errorf("Text = %q, want the status %q", evs[0].Text, "allowed")
	}
}

// /clear kills a session id. The frame carries the id that died and a
// new_conversation_id that is NOT the one replacing it - the successor
// appears nowhere on this frame, and first shows up on the next frame, a
// hook_started, before the init that also carries it. So the event reports
// the death and nothing else; see
// TestSessionResetDoesNotNameItsSuccessor for the recorded proof.
func TestDecodeConversationReset(t *testing.T) {
	line := []byte(`{"type":"conversation_reset","new_conversation_id":"b3144871","uuid":"u1","session_id":"fc32ab1e"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindSessionReset {
		t.Fatalf("got %+v, want one KindSessionReset", evs)
	}
	if evs[0].SessionID != "fc32ab1e" {
		t.Errorf("SessionID = %q, want the id that ended", evs[0].SessionID)
	}
	if evs[0].Text != "" {
		t.Errorf("Text = %q, want empty - new_conversation_id is not the successor and must not be handed up as one", evs[0].Text)
	}
}

// A result is one turn ending. Treating it as process exit tears down live
// sessions - one recorded process emitted seven.
func TestDecodeResultIsATurnEndNotAnExit(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"success","is_error":false,"num_turns":1,"result":"hello","session_id":"s1","uuid":"u1"}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindTurnEnd {
		t.Fatalf("got %+v, want one KindTurnEnd", evs)
	}
	if evs[0].Text != "hello" {
		t.Errorf("Text = %q, want %q", evs[0].Text, "hello")
	}
}

func TestDecodeMalformedLineReturnsError(t *testing.T) {
	if _, err := DecodeLine([]byte(`{not json`)); err == nil {
		t.Fatal("want error for malformed JSON, got nil")
	}
}

func TestDecodeUnknownTypeIsNotAnError(t *testing.T) {
	// Forward compatibility: a new frame type must never crash the decoder.
	evs, err := DecodeLine([]byte(`{"type":"some_future_thing","session_id":"s1"}`))
	if err != nil {
		t.Fatalf("unknown type should not error: %v", err)
	}
	if len(evs) != 1 || evs[0].Kind != KindUnknown {
		t.Fatalf("got %+v, want one KindUnknown", evs)
	}
	if evs[0].Text != "some_future_thing" {
		t.Errorf("Text = %q, want the wire type so it is identifiable", evs[0].Text)
	}
}

// The stream must survive shapes no fixture recorded. Every case here is
// valid JSON in an unexpected arrangement: none may error, none may panic,
// and each must land somewhere a human can identify. "A malformed line logs
// and skips - it never crashes the render loop" is only true if these hold.
func TestDecodeOddButValidFramesNeverError(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantKind  EventKind
		wantText  string
		wantReqID string
	}{
		{
			name:     "message object with a wrongly typed field",
			line:     `{"type":"user","session_id":"s1","message":{"role":5}}`,
			wantKind: KindUnknown,
			wantText: "user",
		},
		{
			name:     "empty content array",
			line:     `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[]}}`,
			wantKind: KindUnknown,
			wantText: "assistant",
		},
		{
			// "image" used to be the example here; it is modeled now, so this
			// uses a block type Wake genuinely does not know.
			name:     "unrecognized block type keeps the block type, not the frame type",
			line:     `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"video","source":{}}]}}`,
			wantKind: KindUnknown,
			wantText: "video",
		},
		{
			name:      "control_request with no request body still keeps its correlator",
			line:      `{"type":"control_request","request_id":"r3"}`,
			wantKind:  KindUnknown,
			wantText:  "control_request",
			wantReqID: "r3",
		},
		{
			name:     "rate_limit_event with no rate_limit_info",
			line:     `{"type":"rate_limit_event","session_id":"s1"}`,
			wantKind: KindRateLimit,
			wantText: "",
		},
		{
			// A control frame keeps its identity inside the nested body:
			// with no "response" there is no subtype, no request_id and no
			// payload, so there is nothing left to be a receipt of. Same
			// ruling as a control_request with no "request".
			name:     "control_response with no response body",
			line:     `{"type":"control_response"}`,
			wantKind: KindUnknown,
			wantText: "control_response",
		},
		{
			// Unlike a control frame, a command_lifecycle's identity is its
			// top-level type. A missing state leaves a message state nobody
			// can act on, not an unrecognized frame - the same ruling as a
			// rate_limit_event with no rate_limit_info.
			name:     "command_lifecycle with no state",
			line:     `{"type":"command_lifecycle","command_uuid":"c1","session_id":"s1"}`,
			wantKind: KindMessageState,
			wantText: "",
		},
		{
			name:     "user frame with no message at all",
			line:     `{"type":"user","session_id":"s1"}`,
			wantKind: KindUserText,
			wantText: "",
		},
		{
			name:     "message is a number, not a string or object",
			line:     `{"type":"assistant","session_id":"s1","message":42}`,
			wantKind: KindAssistantText,
			wantText: "42",
		},
		{
			name:     "message is null",
			line:     `{"type":"user","session_id":"s1","message":null}`,
			wantKind: KindUserText,
			wantText: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			evs, err := DecodeLine([]byte(c.line))
			if err != nil {
				t.Fatalf("DecodeLine: %v", err)
			}
			if len(evs) != 1 {
				t.Fatalf("got %d events, want 1: %+v", len(evs), evs)
			}
			if evs[0].Kind != c.wantKind {
				t.Errorf("Kind = %q, want %q", evs[0].Kind, c.wantKind)
			}
			if evs[0].Text != c.wantText {
				t.Errorf("Text = %q, want %q", evs[0].Text, c.wantText)
			}
			if evs[0].RequestID != c.wantReqID {
				t.Errorf("RequestID = %q, want %q", evs[0].RequestID, c.wantReqID)
			}
		})
	}
}

// Echoed separates text Wake or Claude put on the user side of the
// transcript from something the human actually typed.
//
// Both recorded KindUserText events are echoes - compaction.jsonl:35 is
// isSynthetic (the machine-written compaction summary) and :36 is isReplay
// (a <local-command-stdout> echo) - so 100% of the KindUserText this
// decoder produces from the corpus is non-human. Under
// --replay-user-messages, which Wake depends on, every message Wake sends
// comes back as a user frame too. Without this field the DM view either
// renders every sent message twice or re-parses Event.Raw to find the flag,
// and re-parsing Raw above this file breaks the project's first rule.
func TestEchoedMarksReplayedAndSyntheticFrames(t *testing.T) {
	t.Run("isSynthetic compaction summary", func(t *testing.T) {
		line, n := findFixtureLine(t, "compaction.jsonl", `"content":"This session is being continued`)
		ev := onlyEvent(t, line, n)
		if ev.Kind != KindUserText {
			t.Fatalf("Kind = %q, want %q", ev.Kind, KindUserText)
		}
		if !ev.Echoed {
			t.Error("Echoed = false: a synthetic compaction summary is not a human turn")
		}
	})

	t.Run("isReplay local-command-stdout", func(t *testing.T) {
		line, n := findFixtureLine(t, "compaction.jsonl", `<local-command-stdout>`)
		ev := onlyEvent(t, line, n)
		if ev.Kind != KindUserText {
			t.Fatalf("Kind = %q, want %q", ev.Kind, KindUserText)
		}
		if !ev.Echoed {
			t.Error("Echoed = false: a replayed frame is not a human turn")
		}
	})

	t.Run("a genuine user turn is not echoed", func(t *testing.T) {
		line := `{"type":"user","session_id":"s1","message":{"role":"user","content":"what is 2+2?"}}`
		ev := onlyEvent(t, line, 0)
		if ev.Kind != KindUserText {
			t.Fatalf("Kind = %q, want %q", ev.Kind, KindUserText)
		}
		if ev.Echoed {
			t.Error("Echoed = true on a frame carrying neither flag")
		}
	})

	t.Run("recorded tool results are not echoed", func(t *testing.T) {
		line, n := findFixtureLine(t, "tool-use.jsonl", `"type":"tool_result"`)
		ev := onlyEvent(t, line, n)
		if ev.Echoed {
			t.Error("Echoed = true on a tool result carrying neither flag")
		}
	})
}

// Echoed is a property of the frame; Kind describes each block. One frame
// can emit several events of different kinds, so the flag has to ride all
// of them - which is exactly why this is a bool and not a KindUserEcho.
// A replayed frame carrying tool_result blocks would otherwise have to
// overwrite KindToolResult and destroy the tool correlation.
func TestEchoedRidesEveryEventFromTheFrame(t *testing.T) {
	line := []byte(`{"type":"user","session_id":"s1","isReplay":true,"message":{"role":"user","content":[{"type":"text","text":"do it"},{"tool_use_id":"toolu_1","type":"tool_result","content":"done"}]}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	if evs[0].Kind != KindUserText || evs[1].Kind != KindToolResult {
		t.Fatalf("kinds = %q,%q; want %q,%q", evs[0].Kind, evs[1].Kind, KindUserText, KindToolResult)
	}
	for i, ev := range evs {
		if !ev.Echoed {
			t.Errorf("evs[%d] (%s) Echoed = false, want true on every event from a replayed frame", i, ev.Kind)
		}
	}
	// The kind must survive the flag, or nothing can pair the result with
	// its call.
	if evs[1].Tool == nil || evs[1].Tool.ID != "toolu_1" {
		t.Errorf("tool correlation lost on a replayed frame: %+v", evs[1].Tool)
	}
}

// One block whose shape collides with wireBlock must not take its siblings
// down with it. []wireBlock is all-or-nothing: a single type mismatch
// collapses the whole frame to one content-free KindUnknown, discarding the
// assistant's real prose. That is the exact failure this file's header says
// the airlock exists to eliminate, surviving one level below where the
// defense was applied.
func TestOneMalformedBlockDoesNotDiscardItsSiblings(t *testing.T) {
	// The middle block collides: "text" is a string in wireBlock, an object
	// here. A future Claude release shipping a richer block looks like this.
	line := []byte(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"real prose"},{"type":"text","text":{"nested":"object"}},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`)

	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 3 {
		t.Fatalf("got %d events, want 3 - one per block", len(evs))
	}
	if evs[0].Kind != KindAssistantText || evs[0].Text != "real prose" {
		t.Errorf("evs[0] = %q/%q, want the prose to survive its bad sibling", evs[0].Kind, evs[0].Text)
	}
	if evs[1].Kind != KindUnknown {
		t.Errorf("evs[1].Kind = %q, want %q for the malformed block", evs[1].Kind, KindUnknown)
	}
	// The block's own type survives even when its body does not, so the
	// frame is identifiable in a bug report.
	if evs[1].Text != "text" {
		t.Errorf("evs[1].Text = %q, want the block type %q", evs[1].Text, "text")
	}
	if evs[2].Kind != KindToolUse {
		t.Fatalf("evs[2].Kind = %q, want %q", evs[2].Kind, KindToolUse)
	}
	if evs[2].Tool == nil || evs[2].Tool.Name != "Bash" {
		t.Errorf("tool call lost to an unrelated bad sibling: %+v", evs[2].Tool)
	}
}

// Blocks that are not objects at all degrade one-for-one rather than
// collapsing the frame. Nothing recovers a type from a bare number, so
// these fall back to the frame type.
func TestNonObjectBlocksDegradeIndividually(t *testing.T) {
	evs, err := DecodeLine([]byte(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[1,2]}}`))
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	for i, ev := range evs {
		if ev.Kind != KindUnknown {
			t.Errorf("evs[%d].Kind = %q, want %q", i, ev.Kind, KindUnknown)
		}
		if ev.Text != "assistant" {
			t.Errorf("evs[%d].Text = %q, want the frame type as fallback", i, ev.Text)
		}
	}
}

// tool_result content is a bare string on 34 of the 44 recorded results and
// an array of blocks on the other 10. The array form must arrive as prose.
//
// "in some form" is what this used to assert, via strings.Contains - and the
// raw-JSON fallback satisfied that, so the assertion passed for as long as
// the bug existed. It now pins the exact string, which is the only version
// that can tell prose from a JSON literal containing prose.
func TestDecodeToolResultWithBlockArrayContentIsProseNotJSON(t *testing.T) {
	line := []byte(`{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"nested"}]}]}}`)

	ev := onlyEvent(t, string(line), 0)
	if ev.Kind != KindToolResult {
		t.Fatalf("Kind = %q, want %q", ev.Kind, KindToolResult)
	}
	if ev.Text != "nested" {
		t.Errorf("Text = %q, want exactly %q - anything more is wire format in front of a reader", ev.Text, "nested")
	}
}

// The subagent dispatch receipt's own shape: two text blocks, the first the
// report and the second machine addressing. Both are prose to a reader, and
// the separator has to be a blank line or the addressing runs on into the
// last sentence of the report.
func TestDecodeToolResultJoinsEveryTextBlock(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"the report"},{"type":"text","text":"agentId: a1"}]}]}}`

	if got, want := onlyEvent(t, line, 0).Text, "the report\n\nagentId: a1"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

// The one recorded array with no text in it at all
// (interrupt-cancel-queued-empty.jsonl:28, a lone tool_reference). It has no
// prose, so it renders none - rather than falling back to its JSON, which is
// what the reader used to get. The ⏺ header still names the call, so the
// event does not vanish; only its empty body does.
func TestDecodeToolResultWithNoTextBlocksYieldsNoProse(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"tool_reference","tool_name":"Monitor"}]}]}}`

	ev := onlyEvent(t, line, 0)
	if ev.Kind != KindToolResult {
		t.Fatalf("Kind = %q, want %q", ev.Kind, KindToolResult)
	}
	if ev.Text != "" {
		t.Errorf("Text = %q, want empty - a shape with no prose must not degrade to its JSON", ev.Text)
	}
	if ev.Tool == nil || ev.Tool.ID != "toolu_1" {
		t.Errorf("Tool = %+v, want the call it answers - the header is what keeps the event visible", ev.Tool)
	}
}

// A tool that returns an image - Read on a PNG - records the tool_result with
// an array carrying an image block and no text. It used to render nothing (a
// ⏺ Read header over an empty ⎿ body); it now carries the same [Image]
// placeholder a decoded image on a user turn already does, so the result reads
// rather than vanishes. Wake never draws the bytes - the placeholder is all of
// it.
func TestDecodeToolResultWithImageBlockYieldsPlaceholder(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGg"}}]}]}}`

	ev := onlyEvent(t, line, 0)
	if ev.Kind != KindToolResult {
		t.Fatalf("Kind = %q, want %q", ev.Kind, KindToolResult)
	}
	if ev.Text != ImagePlaceholder {
		t.Errorf("Text = %q, want %q - a tool_result image must read as the placeholder, not as nothing", ev.Text, ImagePlaceholder)
	}
}

// A tool_result carrying prose and an image interleaves them in order, each
// image contributing one placeholder, joined by the blank line every other
// pair of blocks already takes.
func TestDecodeToolResultJoinsTextAndImageInOrder(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":[{"type":"text","text":"here it is"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGg"}}]}]}}`

	if got, want := onlyEvent(t, line, 0).Text, "here it is\n\n"+ImagePlaceholder; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

// The recorded rewind fixture (testdata/transcript/rewind-tree.jsonl, Task 0)
// proves both halves DecodeTranscriptNode reads: several user nodes carrying
// their own uuid and their parent's, and the rewind marker's own leaf.
func TestDecodeTranscriptNodeReadsTheTree(t *testing.T) {
	lines := readLines(t, "testdata/transcript/rewind-tree.jsonl")
	var users, markers int
	var leaf string
	for _, l := range lines {
		n, ok := DecodeTranscriptNode(l)
		if !ok {
			continue
		}
		if n.Kind == "user" && n.UUID != "" && n.ParentUUID != "" {
			users++
		}
		if n.Kind == "last-prompt" && n.Rewound {
			markers++
			leaf = n.LeafUUID
		}
	}
	if users < 2 {
		t.Fatalf("expected multiple user nodes, got %d", users)
	}
	if markers < 1 || leaf == "" {
		t.Fatalf("expected a rewind marker with a leaf, got markers=%d leaf=%q", markers, leaf)
	}
}

// A subagent's line is not the tree the operator's conversation lives on -
// the same rule DecodeTranscriptLine applies (TestASidechainLineIsDropped),
// and for the same reason: a disjoint-root sidechain node (parentUuid:null)
// written last could otherwise be mistaken by the active-branch walk for the
// real conversation's leaf.
func TestDecodeTranscriptNodeDropsASidechainLine(t *testing.T) {
	const line = `{"type":"user","isSidechain":true,"uuid":"s1","parentUuid":null}`
	if _, ok := DecodeTranscriptNode([]byte(line)); ok {
		t.Error("a sidechain line produced a tree node: a subagent's turn is not the operator's conversation")
	}

	// And the same line without the flag is kept, so the test above is about
	// the flag rather than about the shape.
	without := strings.Replace(line, `"isSidechain":true,`, "", 1)
	if n, ok := DecodeTranscriptNode([]byte(without)); !ok || n.UUID != "s1" {
		t.Errorf("the same line without isSidechain produced ok=%v n=%+v", ok, n)
	}
}

// --- helpers ---------------------------------------------------------------

// onlyEvent decodes a line that must yield exactly one event. n is the
// fixture line number for the failure message, or 0 for a literal.
func onlyEvent(t *testing.T, line string, n int) Event {
	t.Helper()
	evs, err := DecodeLine([]byte(line))
	if err != nil {
		t.Fatalf("line %d failed to decode: %v", n, err)
	}
	if len(evs) != 1 {
		t.Fatalf("line %d produced %d events, want 1: %+v", n, len(evs), evs)
	}
	return evs[0]
}

// readLines is fixtureLines by a path relative to the repository root
// (repoRoot, airlock_test.go's own constant) and as [][]byte rather than
// []string - what a decoder taking the wire's own type wants, not a string
// comparison.
func readLines(t *testing.T, path string) [][]byte {
	t.Helper()
	lines := fixtureLines(t, filepath.Join(repoRoot, path))
	out := make([][]byte, len(lines))
	for i, l := range lines {
		out[i] = []byte(l)
	}
	return out
}
