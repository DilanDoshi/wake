package core

import (
	"testing"
	"time"
)

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

// A retry with no status - the network gone, a host that does not resolve - is
// told as a retrying notice carrying which try failed, how many claude allows
// and how long it waits before the next, so a reader can say what claude is
// doing in place of a timer. api-error-unreachable.jsonl's ten retries.
func TestANetworkApiRetryIsNoticedAsRetryingWithItsCountAndPause(t *testing.T) {
	var got []Event
	for _, dl := range decodeFixture(t, "../../testdata/stream/api-error-unreachable.jsonl") {
		if dl.Notice == NoticeAPIRetrying {
			got = append(got, dl.Event)
		}
	}
	if len(got) != 10 {
		t.Fatalf("%d retrying notices decoded, want the recording's ten", len(got))
	}
	third := got[2]
	if third.Kind != KindSystem {
		t.Errorf("a network retry decoded as %q, want a system event: a failed turn it is not", third.Kind)
	}
	if third.Retry == nil {
		t.Fatal("the retrying notice carries no count or pause")
	}
	if want := (APIRetry{Attempt: 3, Max: 10, Delay: 2486 * time.Millisecond}); *third.Retry != want {
		t.Errorf("the third retry decoded as %+v, want %+v", *third.Retry, want)
	}
}

// A retry the API answered with a status - an overload - is retrying too, and
// names the status so a reader can word the cause rather than guess at it.
func TestAStatusedApiRetryCarriesItsStatus(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"api_retry","attempt":2,"max_retries":10,"retry_delay_ms":1030,"error_status":529,"error":"overloaded"}`)
	evs, err := DecodeLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Notice != NoticeAPIRetrying || evs[0].Retry == nil {
		t.Fatalf("a 529 retry decoded as %+v, want one retrying notice with its retry", evs)
	}
	if want := (APIRetry{Attempt: 2, Max: 10, Delay: 1030 * time.Millisecond, Status: 529}); *evs[0].Retry != want {
		t.Errorf("the retry decoded as %+v, want %+v", *evs[0].Retry, want)
	}
}

// No other frame carries a retry, and a 401 keeps its own path: a login's
// failure, never a line that says claude is retrying.
func TestOnlyAnApiRetryCarriesARetry(t *testing.T) {
	for _, dl := range decodeFixture(t, "../../testdata/stream/api-retry-auth.jsonl") {
		if dl.Notice == NoticeAPIRetrying {
			t.Errorf("line %d: a 401 retry was told as retrying rather than as a login's failure", dl.line)
		}
		if dl.Retry != nil && dl.Kind != KindAPIError {
			t.Errorf("line %d: a %q event carries a retry", dl.line, dl.Kind)
		}
	}
	for _, dl := range decodeFixture(t, "../../testdata/stream/midturn-two.jsonl") {
		if dl.Retry != nil {
			t.Errorf("line %d: a %q event carries a retry", dl.line, dl.Kind)
		}
	}
}
