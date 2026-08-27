// The answer Wake writes back for an approved question, and every way one can
// fail to arrive.
//
// The defect these exist for produced *valid bytes*: a bare allow is
// well-formed, correctly addressed, and the turn it settles ends subtype
// "success". So the strong assertion here is not about shape - it is the
// rebuilt answer diffed against the CLI's own echo of the one it received
// (question-answer.jsonl's tool_use_result), which is as close to a golden as
// an outbound frame can come given Wake writes these and never reads them.
//
// This file is one of the airlock's own tests, so it may name Claude's frame
// types - and only ever to prove the airlock encodes them. See
// fixtures_helpers_test.go's header for the full list.

package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"
)

// recordedAnswer reads question-answer.jsonl for the two halves of the one
// answer anybody has ever recorded: the input the ask carried, and the choices
// the CLI reported receiving.
//
// The second half is the strong part. tool_use_result on the answered frame is
// the CLI's own echo of the updatedInput it was handed, so a test can rebuild
// the answer and diff it against what the process on the other end said it
// got - which is as close to a golden as an outbound frame can come, given
// Wake writes these and never reads them.
func recordedAnswer(t *testing.T) (asked map[string]any, answers map[string]string, echoed map[string]any) {
	t.Helper()
	line, _ := findFixtureLine(t, "question-answer.jsonl", `"tool_name":"AskUserQuestion"`)
	var req struct {
		Request struct {
			Input map[string]any `json:"input"`
		} `json:"request"`
	}
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		t.Fatalf("decode the recorded ask: %v", err)
	}

	line, _ = findFixtureLine(t, "question-answer.jsonl", `"answers":{"What output format`)
	var res struct {
		ToolUseResult map[string]any `json:"tool_use_result"`
	}
	if err := json.Unmarshal([]byte(line), &res); err != nil {
		t.Fatalf("decode the recorded echo: %v", err)
	}
	raw, ok := res.ToolUseResult["answers"].(map[string]any)
	if !ok || len(raw) == 0 {
		t.Fatalf("the recorded echo carries no answers: %v", res.ToolUseResult)
	}
	answers = make(map[string]string, len(raw))
	for q, a := range raw {
		answers[q] = a.(string)
	}
	return req.Request.Input, answers, res.ToolUseResult
}

// allowPayload pulls the decision out of an encoded control_response, keeping
// updatedInput's presence separable from its contents.
func allowPayload(t *testing.T, line []byte) (behavior string, updated map[string]any, present bool) {
	t.Helper()
	var f struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Response  struct {
				Behavior     string          `json:"behavior"`
				UpdatedInput json.RawMessage `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}
	if err := json.Unmarshal(line, &f); err != nil {
		t.Fatalf("encoded line is not valid JSON: %v\n%s", err, line)
	}
	if f.Type != "control_response" || f.Response.Subtype != "success" {
		t.Fatalf("envelope = %q/%q, want control_response/success", f.Type, f.Response.Subtype)
	}
	if f.Response.Response.UpdatedInput == nil {
		return f.Response.Response.Behavior, nil, false
	}
	if err := json.Unmarshal(f.Response.Response.UpdatedInput, &updated); err != nil {
		t.Fatalf("updatedInput is not an object: %v", err)
	}
	return f.Response.Response.Behavior, updated, true
}

// The answer Wake builds has to equal the one the recorded process said it
// received - not merely be well-formed, which the bare allow already was.
func TestAnAnswerRebuildsTheUpdatedInputTheRecordedProcessReported(t *testing.T) {
	asked, answers, echoed := recordedAnswer(t)

	got, err := EncodeAnswer("req-1", asked, answers)
	if err != nil {
		t.Fatalf("EncodeAnswer: %v", err)
	}
	behavior, updated, present := allowPayload(t, got)
	if behavior != "allow" {
		t.Errorf("behavior = %q, want allow: an answer is an allow with the answer inside it", behavior)
	}
	if !present {
		t.Fatal("no updatedInput on the wire: this is the whole defect - the tool runs and the model is told nobody answered")
	}

	// Compared through JSON so the two sides are the same shape: echoed came
	// out of a fixture as map[string]any, and answers went in as
	// map[string]string.
	wantJSON, _ := json.Marshal(echoed)
	gotJSON, _ := json.Marshal(updated)
	var want, have any
	_ = json.Unmarshal(wantJSON, &want)
	_ = json.Unmarshal(gotJSON, &have)
	if !reflect.DeepEqual(have, want) {
		t.Errorf("the answer Wake builds is not the one the recorded process reported receiving\n got: %s\nwant: %s", gotJSON, wantJSON)
	}
}

// Absent still means "run it exactly as it asked", and an empty map cannot say
// anything else. This is the property EncodeAnswer had to be built around
// rather than through: {} is untested, so the transport must stay unable to
// express it.
func TestAnAllowStillOmitsUpdatedInputForNilAndForEmpty(t *testing.T) {
	for _, tc := range []struct {
		what  string
		input map[string]any
	}{
		{"nil", nil},
		{"an empty map", map[string]any{}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			got, err := EncodeAllow("req-1", tc.input)
			if err != nil {
				t.Fatalf("EncodeAllow: %v", err)
			}
			if _, _, present := allowPayload(t, got); present {
				t.Errorf("EncodeAllow with %s put updatedInput on the wire: {} is the one shape no recording covers, "+
					"and omitempty is what keeps it unsendable", tc.what)
			}
		})
	}
}

// An answer that would not arrive is refused and says why, rather than going
// out as a well-formed frame that loses it.
//
// Every case here is one an operator or a UI can produce, and every one of
// them is silent without this: the frame would be valid, the tool would run,
// and the model would be told nobody answered.
func TestAnAnswerThatCannotBeDeliveredIsRefusedAndSaysWhy(t *testing.T) {
	asked, answers, _ := recordedAnswer(t)
	oneQuestion := map[string]any{"questions": []any{map[string]any{"question": "pick one"}}}

	for _, tc := range []struct {
		what      string
		requestID string
		asked     map[string]any
		answers   map[string]string
		wants     string
	}{
		{
			what: "no choices at all", requestID: "req-1", asked: asked, answers: nil,
			wants: "nothing was chosen",
		},
		{
			what: "a question left unanswered", requestID: "req-1", asked: asked,
			answers: map[string]string{firstKey(answers): answers[firstKey(answers)]},
			wants:   "nothing was chosen",
		},
		{
			what: "a blank choice", requestID: "req-1", asked: oneQuestion,
			answers: map[string]string{"pick one": "  "},
			wants:   "is blank",
		},
		{
			what: "a choice for a question this ask did not put", requestID: "req-1", asked: oneQuestion,
			answers: map[string]string{"pick one": "a", "never asked": "b"},
			wants:   "did not put",
		},
		{
			what: "an ask carrying no questions", requestID: "req-1",
			asked:   map[string]any{"plan": "# a plan"},
			answers: map[string]string{"pick one": "a"},
			wants:   "carries no questions",
		},
		{
			what: "an ask whose questions have no text", requestID: "req-1",
			asked:   map[string]any{"questions": []any{map[string]any{"header": "Format"}}},
			answers: map[string]string{"pick one": "a"},
			wants:   "has no text",
		},
		{
			what: "no request id", requestID: "", asked: asked, answers: answers,
			wants: "empty request id",
		},
	} {
		t.Run(tc.what, func(t *testing.T) {
			line, err := EncodeAnswer(tc.requestID, tc.asked, tc.answers)
			if err == nil {
				t.Fatalf("EncodeAnswer accepted %s and produced %s", tc.what, line)
			}
			if line != nil {
				t.Errorf("a refused answer still returned %d bytes to write", len(line))
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error = %q, want it to name the problem (%q)", err, tc.wants)
			}
			if !errors.Is(err, ErrNotWritten) {
				t.Errorf("error %q does not wrap ErrNotWritten: internal/daemon reads a write failure as proof the "+
					"process is gone, so a refusal that looks like one takes a healthy blocked agent off the roster", err)
			}
		})
	}
}

func firstKey(m map[string]string) string {
	for k := range m {
		return k
	}
	return ""
}

// The ask a renderer is still holding must not change because somebody
// answered it.
func TestAnAnswerDoesNotWriteIntoTheAskItAnswers(t *testing.T) {
	asked, answers, _ := recordedAnswer(t)
	before := maps.Clone(asked)

	if _, err := EncodeAnswer("req-1", asked, answers); err != nil {
		t.Fatalf("EncodeAnswer: %v", err)
	}
	if !reflect.DeepEqual(asked, before) {
		t.Errorf("EncodeAnswer wrote into the ask's own input\n got: %v\nwant: %v", asked, before)
	}
}

// The same two outcomes through Session, which is what the daemon calls: a
// refusal reaches a caller with nothing written, and an accepted answer
// reaches stdin whole.
func TestAnsweringThroughASessionEitherReachesStdinOrSaysNothingWasWritten(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	err := s.AnswerQuestion("req-1", map[string]any{"plan": "x"}, map[string]string{"q": "a"})
	if err == nil {
		t.Fatal("AnswerQuestion accepted an ask carrying no questions")
	}
	if !errors.Is(err, ErrNotWritten) {
		t.Errorf("error %q does not wrap ErrNotWritten", err)
	}
	if buf.Len() != 0 {
		t.Errorf("a refused answer put %d bytes on stdin: %s", buf.Len(), buf.String())
	}

	asked, answers, _ := recordedAnswer(t)
	if err := s.AnswerQuestion("req-1", asked, answers); err != nil {
		t.Fatalf("AnswerQuestion: %v", err)
	}
	line := strings.TrimSuffix(buf.String(), "\n")
	if strings.Contains(line, "\n") {
		t.Errorf("the answer reached stdin as more than one frame: %s", buf.String())
	}
	if _, _, present := allowPayload(t, []byte(line)); !present {
		t.Errorf("the line that reached stdin carries no updatedInput: %s", line)
	}
}
