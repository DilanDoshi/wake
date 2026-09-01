// What the airlock reads about the session itself rather than the
// conversation: the model it runs, and how full its context window is. See
// fixtures_helpers_test.go for why this file may name Claude's frame types.

package core

import (
	"encoding/json"
	"testing"
)

// init names a model and nothing else. It is the only frame that carries one
// before a turn has ended, so a status bar has something to draw immediately.
func TestInitCarriesTheModel(t *testing.T) {
	evs := decodeLineT(t, `{"type":"system","subtype":"init","session_id":"s1","model":"claude-sonnet-5","permissionMode":"auto"}`)

	if len(evs) != 1 {
		t.Fatalf("init decoded to %d events, want 1", len(evs))
	}
	facts := evs[0].Session
	if facts == nil {
		t.Fatal("init carried no session facts; the model is on it")
	}
	if facts.Model != "claude-sonnet-5" {
		t.Errorf("model = %q, want %q", facts.Model, "claude-sonnet-5")
	}
	if facts.ContextWindow != 0 {
		t.Errorf("init reported a context window of %d; only result names one", facts.ContextWindow)
	}
}

// Every other system subtype carries no facts at all. Stamping an empty model
// onto them would blank a status bar once per hook event.
func TestOtherSystemSubtypesCarryNoSessionFacts(t *testing.T) {
	for _, subtype := range []string{"compact_boundary", "hook_started", "status", "thinking_tokens"} {
		evs := decodeLineT(t, `{"type":"system","subtype":"`+subtype+`","session_id":"s1"}`)
		if evs[0].Session != nil {
			t.Errorf("subtype %q carried session facts: %+v", subtype, evs[0].Session)
		}
	}
}

// The context level is all three input fields, not input_tokens alone. On the
// very first recorded turn those differ by four orders of magnitude - 2 against
// 50,788 - so reading the obvious field reports an empty window all session.
func TestResultCarriesTheWholeContextLevel(t *testing.T) {
	evs := decodeLineT(t, `{"type":"result","subtype":"success","session_id":"s1","model":"claude-sonnet-5",`+
		`"usage":{"input_tokens":2,"cache_creation_input_tokens":26768,"cache_read_input_tokens":24018,"output_tokens":4},`+
		`"modelUsage":{"claude-sonnet-5":{"contextWindow":1000000}}}`)

	facts := evs[0].Session
	if facts == nil {
		t.Fatal("result carried no session facts; usage is on it")
	}
	if want := 2 + 26768 + 24018; facts.ContextTokens != want {
		t.Errorf("context tokens = %d, want %d (input + cache creation + cache read)", facts.ContextTokens, want)
	}
	if facts.ContextWindow != 1000000 {
		t.Errorf("context window = %d, want 1000000", facts.ContextWindow)
	}
}

// A turn with several tool-use round-trips reports the context *level* - the
// final call's input side - not the sum of every call's input. The top-level
// usage sums input across the whole turn, so it grows with the round-trip
// count while usage.iterations' last element stays the level alone. The
// numbers below are auto-read-outside.jsonl's real 2-turn result: 74,433
// summed against 37,931 for the final call. The corpus also holds
// question-plan-bare.jsonl (num_turns 14) at 619,215 summed against 49,869 -
// ~12x, enough to peg the ctx bar near empty on a window three quarters free.
// TestRecordedContextTokensAreTheFinalIteration checks the whole corpus.
func TestContextTokensIsTheFinalCallNotTheTurnSum(t *testing.T) {
	evs := decodeLineT(t, `{"type":"result","subtype":"success","session_id":"s1","model":"claude-sonnet-5",`+
		`"usage":{"input_tokens":4,"cache_creation_input_tokens":18697,"cache_read_input_tokens":55732,"output_tokens":172,`+
		`"iterations":[{"input_tokens":2,"cache_creation_input_tokens":1429,"cache_read_input_tokens":36500}]},`+
		`"modelUsage":{"claude-sonnet-5":{"contextWindow":1000000}}}`)

	facts := evs[0].Session
	if facts == nil {
		t.Fatal("result carried no session facts; usage is on it")
	}
	if want := 2 + 1429 + 36500; facts.ContextTokens != want {
		t.Errorf("context tokens = %d, want %d (the final round-trip's input, not the turn's summed %d)",
			facts.ContextTokens, want, 4+18697+55732)
	}
	if facts.ContextWindow != 1000000 {
		t.Errorf("context window = %d, want 1000000", facts.ContextWindow)
	}
}

// The window is looked up by the frame's own model, so a turn that used two
// does not take the wrong one's window.
func TestTheContextWindowIsTheModelsOwn(t *testing.T) {
	evs := decodeLineT(t, `{"type":"result","session_id":"s1","model":"claude-opus-5",`+
		`"usage":{"input_tokens":10},`+
		`"modelUsage":{"claude-sonnet-5":{"contextWindow":200000},"claude-opus-5":{"contextWindow":1000000}}}`)

	if got := evs[0].Session.ContextWindow; got != 1000000 {
		t.Errorf("context window = %d, want the frame's own model's 1000000", got)
	}
}

// A result with no usage says nothing about the window rather than claiming
// it is empty. An interrupted turn has no usage at all.
func TestAResultWithoutUsageCarriesNoFacts(t *testing.T) {
	evs := decodeLineT(t, `{"type":"result","subtype":"success","session_id":"s1"}`)
	if evs[0].Session != nil {
		t.Errorf("a result with no usage carried facts: %+v", evs[0].Session)
	}
}

// The recording is the authority: whatever the fixtures hold must decode to a
// window that could actually be a denominator.
func TestEveryRecordedContextWindowIsUsable(t *testing.T) {
	var seen int
	for _, path := range fixtureFiles(t) {
		for _, line := range fixtureLines(t, path) {
			evs, err := DecodeLine([]byte(line))
			if err != nil || len(evs) == 0 || evs[0].Session == nil {
				continue
			}
			f := evs[0].Session
			if f.ContextWindow == 0 {
				continue
			}
			seen++
			if f.ContextTokens > f.ContextWindow {
				t.Errorf("%s: %d tokens in a window of %d", path, f.ContextTokens, f.ContextWindow)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no recorded frame decoded to a context window; the corpus should carry several")
	}
}

// Across the whole corpus the decoded level is the final round-trip's input,
// never the turn's sum. The window guard above cannot catch the old summed
// reading - every recorded window is 1M and even a 14-call sum stays under it -
// so this compares against the level the recording itself names, and requires
// at least one turn whose sum would have inflated the level, so the guard is
// shown to bite rather than to pass vacuously.
func TestRecordedContextTokensAreTheFinalIteration(t *testing.T) {
	type usage struct {
		In    int `json:"input_tokens"`
		CC    int `json:"cache_creation_input_tokens"`
		CR    int `json:"cache_read_input_tokens"`
		Iters []struct {
			In int `json:"input_tokens"`
			CC int `json:"cache_creation_input_tokens"`
			CR int `json:"cache_read_input_tokens"`
		} `json:"iterations"`
	}
	var checked, inflating int
	for _, path := range fixtureFiles(t) {
		for _, line := range fixtureLines(t, path) {
			var raw struct {
				Type  string `json:"type"`
				Usage *usage `json:"usage"`
			}
			if err := json.Unmarshal([]byte(line), &raw); err != nil ||
				raw.Type != "result" || raw.Usage == nil || len(raw.Usage.Iters) == 0 {
				continue
			}
			evs, err := DecodeLine([]byte(line))
			if err != nil || len(evs) == 0 || evs[0].Session == nil {
				continue
			}
			last := raw.Usage.Iters[len(raw.Usage.Iters)-1]
			want := last.In + last.CC + last.CR
			if got := evs[0].Session.ContextTokens; got != want {
				t.Errorf("%s: context tokens = %d, want the final iteration's %d", path, got, want)
			}
			checked++
			if raw.Usage.In+raw.Usage.CC+raw.Usage.CR > want {
				inflating++
			}
		}
	}
	if checked == 0 {
		t.Fatal("no recorded result frame carried iterations; the corpus should hold many")
	}
	if inflating == 0 {
		t.Fatal("no recorded turn's summed reading exceeded its level; the fix cannot be shown to bite")
	}
}

func decodeLineT(t *testing.T, line string) []Event {
	t.Helper()
	evs, err := DecodeLine([]byte(line))
	if err != nil {
		t.Fatalf("decoding %s: %v", line, err)
	}
	if len(evs) == 0 {
		t.Fatalf("decoding %s produced no events", line)
	}
	return evs
}
