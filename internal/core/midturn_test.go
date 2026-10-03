package core

import (
	"errors"
	"testing"
)

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

// recordedInput is line n (1-based) of a testdata/input fixture, with its newline.
func recordedInput(t *testing.T, name string, n int) string {
	t.Helper()
	lines := fixtureLines(t, "../../testdata/input/"+name)
	if n > len(lines) {
		t.Fatalf("%s has %d lines, want line %d", name, len(lines), n)
	}
	return lines[n-1] + "\n"
}

// A send-now is the line that moved claude's running Bash to the background
// (midturn-now-human.jsonl), byte for byte; every other send is unchanged.
func TestASendNowIsTheRecordedLineAndAnOrdinarySendIsUnchanged(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		now     bool
	}{
		{"midturn-now-human.stdin.jsonl", true},
		{"midturn-absent.stdin.jsonl", false},
	} {
		want := recordedInput(t, tc.fixture, 2)
		got, err := EncodeUserMessage("Also: end your reply with the word PINEAPPLE.", nil, recordUUID(t, []byte(want)), tc.now)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Errorf("%s:\n got %s want %s", tc.fixture, got, want)
		}
	}
}

// Taking a message back is the line that kept it from ever running
// (midturn-cancel.jsonl:31-33), byte for byte; a blank id is refused unwritten.
func TestARecallIsTheRecordedCancelLine(t *testing.T) {
	want := recordedInput(t, "midturn-cancel.stdin.jsonl", 3)
	got, err := EncodeCancelAsyncMessage("0df2c4b8-f0c0-416c-834e-217290d26c54", "7796a089-09da-46e4-9b4e-77caf2c8213a")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got  %s want %s", got, want)
	}
	for _, ids := range [][2]string{{"", "m"}, {"r", ""}} {
		if _, err := EncodeCancelAsyncMessage(ids[0], ids[1]); !errors.Is(err, ErrNotWritten) {
			t.Errorf("EncodeCancelAsyncMessage(%q, %q) = %v, want ErrNotWritten", ids[0], ids[1], err)
		}
	}
}
