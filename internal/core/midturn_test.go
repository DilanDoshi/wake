package core

import "testing"

// A cancel_async_message receipt carries `cancelled` as a bool, the key an
// interrupt's receipt carries as a uuid list. Both outcomes must decode as the
// receipt for the request that asked (docs/superpowers/notes/
// 2026-10-02-mid-turn-delivery-findings.md).
func TestARecallsReceiptDecodesAsTheReceiptForItsRequest(t *testing.T) {
	for _, tc := range []struct{ fixture, marker, requestID string }{
		{"midturn-cancel.jsonl", `"cancelled":true`, "0df2c4b8-f0c0-416c-834e-217290d26c54"},
		{"midturn-cancel-late.jsonl", `"cancelled":false`, "2082ad59-2d5e-4047-899f-48378c4fa2b9"},
	} {
		events, err := DecodeLine([]byte(fixtureLineContaining(t, tc.fixture, tc.marker)))
		if err != nil {
			t.Fatalf("%s: %v", tc.fixture, err)
		}
		if len(events) != 1 || events[0].Kind != KindControlReceipt || events[0].RequestID != tc.requestID {
			t.Errorf("%s: decoded %+v, want one receipt for request %s", tc.fixture, events, tc.requestID)
		}
	}
}
