// The subagent dimension of the airlock, at unit level: attribution off a
// forwarded frame, and the dispatch receipt where the two identifier spaces
// meet. The corpus-wide half is in fixtures_subagent_test.go.
//
// Split out of protocol_test.go when that file passed this project's 800-line
// hard max - the same limit that split the airlock itself and then
// fixtures_test.go, and it applies to tests too. protocol_test.go carries the
// airlock's own rule and the shared helpers.

package core

import "testing"

// The three keys that ride all 80 forwarded frames, and the whole of C1: a
// subagent's work decoded to byte-for-byte the shape a parent's produced.
func TestForwardedFrameIsAttributedToItsSubagent(t *testing.T) {
	line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_b","name":"Bash","input":{"command":"ls -la"}}]},"parent_tool_use_id":"toolu_dispatch","subagent_type":"general-purpose","task_description":"Count lines"}`

	ev := onlyEvent(t, line, 0)
	if ev.Subagent == nil {
		t.Fatal("Subagent is nil: the subagent's tool call is filed as the agent's own")
	}
	if ev.Subagent.Dispatch != "toolu_dispatch" {
		t.Errorf("Dispatch = %q, want the parent_tool_use_id - it is what separates two concurrent subagents", ev.Subagent.Dispatch)
	}
	if ev.Subagent.Type != "general-purpose" {
		t.Errorf("Type = %q, want the subagent_type", ev.Subagent.Type)
	}
	if ev.Subagent.Task != "Count lines" {
		t.Errorf("Task = %q, want the task_description - the only human-readable name on the frame", ev.Subagent.Task)
	}
	if ev.Subagent.Result != "" {
		t.Errorf("Result = %q, want empty: this is the subagent speaking, not a receipt for it", ev.Subagent.Result)
	}
}

// The other half, and the one that makes the first half mean anything: a
// frame the agent itself produced must not be attributed to a subagent.
// parent_tool_use_id is present-and-null on every parent frame in the corpus,
// which is a key the decoder sees and must read as absent.
func TestParentFrameIsNotAttributedToASubagent(t *testing.T) {
	line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]},"parent_tool_use_id":null}`

	if ev := onlyEvent(t, line, 0); ev.Subagent != nil {
		t.Errorf("Subagent = %+v, want nil - a null parent_tool_use_id is the agent itself", ev.Subagent)
	}
}

// The attribution is frame-level, like Echoed, so every block of a forwarded
// frame carries it. An assistant frame that thinks and then calls a tool must
// not have half of it filed under the parent.
func TestSubagentRidesEveryBlockOfAForwardedFrame(t *testing.T) {
	line := `{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hm"},{"type":"tool_use","id":"toolu_b","name":"Bash","input":{"command":"ls"}}]},"parent_tool_use_id":"toolu_dispatch","subagent_type":"general-purpose","task_description":"Count lines"}`

	evs, err := DecodeLine([]byte(line))
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("got %d events, want 2", len(evs))
	}
	for i, ev := range evs {
		if ev.Subagent == nil || ev.Subagent.Dispatch != "toolu_dispatch" {
			t.Errorf("event %d (%s) Subagent = %+v, want the frame's dispatch", i, ev.Kind, ev.Subagent)
		}
	}
	// Separate pointers, or one block's edit is visible on its sibling.
	if evs[0].Subagent == evs[1].Subagent {
		t.Error("both events share one *Subagent: mutating either would change the other")
	}
}

// The dispatch receipt. tool_use_result.agentId is the only thing on the
// frame that says this result is a subagent finishing rather than any other
// tool returning - and Dispatch comes from the block's tool_use_id, which is
// the join the findings note reported as missing.
func TestDispatchReceiptIsDistinguishedFromAnyOtherToolResult(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_dispatch","type":"tool_result","content":[{"type":"text","text":"the report"}]}]},"parent_tool_use_id":null,"tool_use_result":{"status":"completed","agentId":"a73fc145a7d730b54","agentType":"general-purpose"}}`

	ev := onlyEvent(t, line, 0)
	if ev.Subagent == nil {
		t.Fatal("Subagent is nil: the receipt is indistinguishable from an ordinary tool result")
	}
	if ev.Subagent.Result != SubagentFinished {
		t.Errorf("Result = %q, want %q", ev.Subagent.Result, SubagentFinished)
	}
	if ev.Subagent.Dispatch != "toolu_dispatch" {
		t.Errorf("Dispatch = %q, want the block's tool_use_id - it is what ties the receipt to the speech it repeats", ev.Subagent.Dispatch)
	}
	if ev.Subagent.Agent != "a73fc145a7d730b54" {
		t.Errorf("Agent = %q, want the agentId", ev.Subagent.Agent)
	}
	if ev.Text != "the report" {
		t.Errorf("Text = %q, want the report as prose", ev.Text)
	}
}

// A plain tool result must not acquire a subagent. Without this the previous
// test passes for a decoder that marks every tool result as a receipt.
func TestPlainToolResultIsNotADispatchReceipt(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"ok"}]},"tool_use_result":{"stdout":"ok"}}`

	if ev := onlyEvent(t, line, 0); ev.Subagent != nil {
		t.Errorf("Subagent = %+v, want nil - no agentId on this frame", ev.Subagent)
	}
}

// tool_use_result is a bare string on 6 of the 19 frames that carry it. A
// struct-typed field would lose those frames whole, exactly as Message would.
func TestStringToolUseResultDoesNotLoseTheFrame(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_1","type":"tool_result","content":"ok"}]},"tool_use_result":"Error: spike: denied by probe"}`

	ev := onlyEvent(t, line, 0)
	if ev.Kind != KindToolResult {
		t.Fatalf("Kind = %q, want %q - a string tool_use_result cost the whole frame", ev.Kind, KindToolResult)
	}
	if ev.Subagent != nil {
		t.Errorf("Subagent = %+v, want nil", ev.Subagent)
	}
}

// Only the two recorded statuses license a view hiding anything, so an
// unrecorded one must not inherit that licence. SubagentUnknown is the value
// that means "show it".
func TestDispatchStatusResolvesOnlyWhatWasRecorded(t *testing.T) {
	cases := map[string]SubagentResult{
		"completed":      SubagentFinished,
		"async_launched": SubagentLaunched,
		"failed":         SubagentUnknown,
		"":               SubagentUnknown,
	}
	for status, want := range cases {
		t.Run(status, func(t *testing.T) {
			line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_d","type":"tool_result","content":[{"type":"text","text":"x"}]}]},"tool_use_result":{"status":"` + status + `","agentId":"a1"}}`
			ev := onlyEvent(t, line, 0)
			if ev.Subagent == nil {
				t.Fatal("Subagent is nil")
			}
			if ev.Subagent.Result != want {
				t.Errorf("Result = %q, want %q", ev.Subagent.Result, want)
			}
		})
	}
}

// A grandchild dispatch - a subagent dispatching a subagent - is listed in
// §11 of the findings note as never recorded. The speaker attribution wins,
// which errs toward showing a body rather than suppressing it as a duplicate.
func TestForwardedReceiptKeepsTheSpeakerRatherThanGuessing(t *testing.T) {
	line := `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"tool_use_id":"toolu_inner","type":"tool_result","content":[{"type":"text","text":"x"}]}]},"parent_tool_use_id":"toolu_outer","subagent_type":"general-purpose","task_description":"outer","tool_use_result":{"status":"completed","agentId":"a1"}}`

	ev := onlyEvent(t, line, 0)
	if ev.Subagent == nil {
		t.Fatal("Subagent is nil")
	}
	if ev.Subagent.Dispatch != "toolu_outer" || ev.Subagent.Task != "outer" {
		t.Errorf("Subagent = %+v, want the forwarding frame's speaker", ev.Subagent)
	}
	if ev.Subagent.Result != "" {
		t.Errorf("Result = %q, want empty - suppressing an unrecorded shape's body is the unsafe direction", ev.Subagent.Result)
	}
}

// I1. agent_id is on exactly one of the three recorded can_use_tool frames,
// so its presence is the signal. Dropped, an operator approves an
// irreversible write without knowing who asked.
func TestPermissionRequestFromASubagentNamesIt(t *testing.T) {
	line := `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"/tmp/tally.txt"},"tool_use_id":"toolu_w","agent_id":"ab1b72d53680ae187"}}`

	ev := onlyEvent(t, line, 0)
	if ev.Kind != KindPermissionRequest {
		t.Fatalf("Kind = %q, want %q", ev.Kind, KindPermissionRequest)
	}
	if ev.Subagent == nil {
		t.Fatal("Subagent is nil: a subagent's ask is indistinguishable from its parent's")
	}
	if ev.Subagent.Agent != "ab1b72d53680ae187" {
		t.Errorf("Agent = %q, want the agent_id", ev.Subagent.Agent)
	}
	if ev.Subagent.Dispatch != "" {
		t.Errorf("Dispatch = %q, want empty - the ask envelope names no parent_tool_use_id, and pretending otherwise would invent a join", ev.Subagent.Dispatch)
	}
}

// The contrast, and without it the previous test passes for a decoder that
// marks every ask as a subagent's.
func TestPermissionRequestFromTheParentNamesNoSubagent(t *testing.T) {
	line := `{"type":"control_request","request_id":"r1","request":{"subtype":"can_use_tool","tool_name":"Write","input":{"file_path":"/tmp/x"},"tool_use_id":"toolu_w"}}`

	if ev := onlyEvent(t, line, 0); ev.Subagent != nil {
		t.Errorf("Subagent = %+v, want nil - no agent_id on this ask", ev.Subagent)
	}
}
