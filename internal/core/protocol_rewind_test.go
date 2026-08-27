// Decoding Claude's answer to a rewind_conversation request (Task 2). The
// outbound half - what Wake sends - is encode_test.go's; this is the
// receipt, off testdata/stream/rewind-conversation.jsonl (Task 0). See
// fixtures_helpers_test.go for why this file may name Claude's frame types.

package core

import (
	"path/filepath"
	"testing"
)

// TestDecodeRewindReceipt is the golden case: the recorded success receipt,
// decoded into KindRewindReceipt with its payload.
func TestDecodeRewindReceipt(t *testing.T) {
	line, n := lineContaining(t, "testdata/stream/rewind-conversation.jsonl", `"rewound":true`)
	ev := onlyEvent(t, line, n)
	if ev.Kind != KindRewindReceipt {
		t.Fatalf("kind = %q, want rewind_receipt", ev.Kind)
	}
	if ev.Rewind == nil || !ev.Rewind.Rewound {
		t.Fatalf("rewind result missing/false: %+v", ev.Rewind)
	}
	if ev.Rewind.PrefillText == "" || ev.Rewind.PrecedingAssistantUUID == "" {
		t.Fatalf("prefill/leaf not decoded: %+v", ev.Rewind)
	}
	if ev.RequestID == "" {
		t.Fatalf("request id not correlated")
	}
}

// TestDecodeRewindFailureCarriesError is the refusal shape: rewound false and
// a reason, nested at the same innermost level a success carries its fields -
// see wireControlBody.Error, which is not wireControlResp.Error one level up
// (the mode-refusal error).
func TestDecodeRewindFailureCarriesError(t *testing.T) {
	line := `{"type":"control_response","response":{"subtype":"success","request_id":"r1","response":{"rewound":false,"prefillText":null,"precedingAssistantUuid":null,"error":"stale target"}}}`
	ev := onlyEvent(t, line, 0)
	if ev.Kind != KindRewindReceipt || ev.Rewind == nil || ev.Rewind.Rewound || ev.Rewind.Error != "stale target" {
		t.Fatalf("bad failure decode: kind=%q rewind=%+v", ev.Kind, ev.Rewind)
	}
}

// TestAModeReceiptIsStillAModeReceipt guards the discriminator: adding the
// rewind branch must not reclassify a set_permission_mode receipt, which
// carries no "rewound" key at all.
func TestAModeReceiptIsStillAModeReceipt(t *testing.T) {
	line := `{"type":"control_response","response":{"subtype":"success","request_id":"m1","response":{"mode":"plan"}}}`
	ev := onlyEvent(t, line, 0)
	if ev.Kind != KindControlReceipt || ev.PermissionMode != "plan" {
		t.Fatalf("mode receipt misclassified: kind=%q mode=%q", ev.Kind, ev.PermissionMode)
	}
}

// lineContaining returns the one line of a fixture - given as a path
// relative to the repo root, e.g. "testdata/stream/x.jsonl" - that contains
// marker, and its 1-indexed line number for onlyEvent's failure messages.
// Wraps findFixtureLine, which already fails the test unless there is
// exactly one match.
func lineContaining(t *testing.T, fixturePath, marker string) (string, int) {
	t.Helper()
	return findFixtureLine(t, filepath.Base(fixturePath), marker)
}
