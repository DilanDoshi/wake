package core

import (
	"fmt"
	"strings"
	"testing"
)

// A usage limit fails a turn the way an expired login does, but it recovers on
// its own when the quota resets, so it raises a notice of its own. The frame's
// top-level "error" names the kind: "authentication_failed" is recorded live in
// testdata/stream/api-error-auth.jsonl, and "rate_limit" is the value the same
// synthetic frame carries on disk for a session or weekly limit.
func TestAUsageLimitIsToldApartFromAnExpiredLogin(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want Notice
	}{
		{"rate_limit", NoticeUsageLimit},
		{"authentication_failed", NoticeAPIError},
		{"server_error", NoticeAPIError},
		{"", NoticeAPIError},
	} {
		line := fmt.Sprintf(`{"type":"assistant","is_api_error_message":true,"error":%q,"session_id":"s1","message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"You've hit your session limit · resets 9:50pm (America/Los_Angeles)"}]}}`, tc.kind)
		evs, err := DecodeLine([]byte(line))
		if err != nil || len(evs) != 1 {
			t.Fatalf("%q: decode gave %v, %v", tc.kind, evs, err)
		}
		if evs[0].Kind != KindAPIError || evs[0].Notice != tc.want {
			t.Errorf("a failed turn with error %q decoded as %q/%q, want %q/%q", tc.kind, evs[0].Kind, evs[0].Notice, KindAPIError, tc.want)
		}
		if !strings.Contains(evs[0].Text, "session limit") {
			t.Errorf("%q: the API's message was not carried: %q", tc.kind, evs[0].Text)
		}
	}
}
