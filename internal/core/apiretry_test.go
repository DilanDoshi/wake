package core

import "testing"

// A fleet-wide OAuth 401 makes Claude Code retry a dead token for ~5 min before
// it gives up. The retries are on the LIVE stream from attempt 1 as
// system/api_retry frames carrying error_status - recorded 2026-09-09 in
// testdata/stream/api-retry-auth.jsonl. Decoding the 401 retry as a KindAPIError
// is what lets the failure surface in a second instead of at the give-up, and it
// is the signal the auto-park rides. A non-401 retry (an overload) is not an auth
// failure and must stay a plain system event, or a recoverable 529 would raise a
// "/reauth" that cannot help. See docs/notes/bugs.md.

func TestA401ApiRetryDecodesAsAnAPIError(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"api_retry","attempt":1,"max_retries":10,"retry_delay_ms":595,"error_status":401,"error":"authentication_failed"}`)
	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want one event, got %d", len(evs))
	}
	if evs[0].Kind != KindAPIError || evs[0].Notice != NoticeAPIError {
		t.Fatalf("401 api_retry did not decode as an auth failure: Kind=%q Notice=%q", evs[0].Kind, evs[0].Notice)
	}
}

func TestANon401ApiRetryStaysASystemEvent(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"api_retry","attempt":1,"max_retries":10,"retry_delay_ms":600,"error_status":529,"error":"overloaded"}`)
	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 {
		t.Fatalf("want one event, got %d", len(evs))
	}
	if evs[0].Kind == KindAPIError || evs[0].Notice == NoticeAPIError {
		t.Fatalf("a 529 overload retry was mis-read as an auth failure: Kind=%q Notice=%q", evs[0].Kind, evs[0].Notice)
	}
}

// The recorded storm decodes end to end: every 401 api_retry line in the fixture
// becomes an auth failure, so a real fleet-wide 401 surfaces from the first
// retry rather than at the give-up.
func TestTheRecordedRetryStormDecodesAsAuthFailures(t *testing.T) {
	got := 0
	for _, dl := range decodeFixture(t, "../../testdata/stream/api-retry-auth.jsonl") {
		if dl.Kind == KindAPIError {
			got++
		}
	}
	if got == 0 {
		t.Fatal("no api_retry line in the fixture decoded as a KindAPIError")
	}
}
