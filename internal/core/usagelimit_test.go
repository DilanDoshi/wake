package core

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// A usage limit fails a turn the way an expired login does, but it recovers on
// its own when the quota resets, so it raises a notice of its own. The frame's
// top-level "error" names the kind: "authentication_failed" is recorded live in
// testdata/stream/api-error-auth.jsonl, and "rate_limit" is the value the same
// synthetic frame carries on disk for a session or weekly limit. Any other named
// kind fails one turn only; a frame naming none is read as a login's, as every
// failed turn was before the kinds were told apart.
func TestAUsageLimitIsToldApartFromAnExpiredLogin(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want Notice
	}{
		{"rate_limit", NoticeUsageLimit},
		{"authentication_failed", NoticeAPIError},
		{"", NoticeAPIError},
		{"server_error", NoticeTurnFailed},
		{"overloaded", NoticeTurnFailed},
		{"invalid_request", NoticeTurnFailed},
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

// A "<synthetic>" assistant frame is Claude answering a local command itself -
// /context, /compact, /mcp - with no inference, so it works on a dead login and
// must never read as proof the API answered. Every assistant text in the
// recorded slash-command corpus is held to that: synthetic ones are marked,
// real model turns are not.
func TestAClaudeLocalReplyIsMarkedAsNoInference(t *testing.T) {
	var synthetic, real int
	for _, line := range fixtureLines(t, filepath.Join("..", "..", "testdata", "stream", "slash-commands.jsonl")) {
		var probe struct {
			Type    string `json:"type"`
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &probe) != nil || probe.Type != "assistant" {
			continue
		}
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		for _, ev := range evs {
			if ev.Kind != KindAssistantText {
				continue
			}
			want := probe.Message.Model == "<synthetic>"
			if want {
				synthetic++
			} else {
				real++
			}
			if ev.LocalCommand != want {
				t.Errorf("assistant text from model %q: LocalCommand = %v, want %v (%q)", probe.Message.Model, ev.LocalCommand, want, ev.Text)
			}
		}
	}
	if synthetic == 0 || real == 0 {
		t.Fatalf("the corpus no longer holds both shapes: %d synthetic, %d real", synthetic, real)
	}
}

// The error kind is read raw, so a frame whose "error" is not a string still
// decodes rather than failing whole - naming no kind, it reads as a login's.
func TestANonStringErrorKindStillDecodes(t *testing.T) {
	line := `{"type":"assistant","is_api_error_message":true,"error":{"type":"overloaded"},"session_id":"s1","message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Overloaded"}]}}`
	evs, err := DecodeLine([]byte(line))
	if err != nil || len(evs) != 1 || evs[0].Kind != KindAPIError || evs[0].Notice != NoticeAPIError {
		t.Fatalf("an object error kind decoded as %+v, %v; want one KindAPIError/NoticeAPIError", evs, err)
	}
}
