package core

import (
	"encoding/json"
	"testing"
)

// At a --resume claude closes a turn left with no reply by writing a <synthetic>
// "No response requested." record ahead of the next prompt; it is not on stdout
// (BUG-65). resume-interrupted.jsonl's follows an interrupt mid-tool,
// resume-killed.jsonl's a process killed after its prompt.
var resumeFixtures = []string{
	"testdata/transcript/resume-interrupted.jsonl",
	"testdata/transcript/resume-killed.jsonl",
}

// recordOf is one transcript line's type, model and text, read off the raw line
// so the test finds the record without going through the decoder under test.
func recordOf(t *testing.T, line []byte) (kind, model, text string) {
	t.Helper()
	var r struct {
		Type    string `json:"type"`
		Message struct {
			Model   string          `json:"model"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &r); err != nil {
		t.Fatalf("fixture line: %v", err)
	}
	return r.Type, r.Message.Model, messageText(r.Message.Content)
}

// closingRecord is the index of the fixture's one synthetic assistant record, and
// the model the agent's own last reply carries.
func closingRecord(t *testing.T, lines [][]byte) (closing int, agentModel string) {
	t.Helper()
	closing = -1
	for i, line := range lines {
		kind, model, _ := recordOf(t, line)
		switch {
		case kind != "assistant":
		case model == syntheticModel && closing >= 0:
			t.Fatalf("two synthetic records, lines %d and %d", closing+1, i+1)
		case model == syntheticModel:
			closing = i
		default:
			agentModel = model
		}
	}
	if closing < 0 || agentModel == "" {
		t.Fatalf("no synthetic record (%d) or no agent reply (%q)", closing, agentModel)
	}
	return closing, agentModel
}

func TestAResumesCloseOfAnUnansweredTurnDecodesToNothing(t *testing.T) {
	for _, path := range resumeFixtures {
		lines := readLines(t, path)
		closing, _ := closingRecord(t, lines)
		if _, _, text := recordOf(t, lines[closing]); text != noResponseText {
			t.Fatalf("%s line %d reads %q, not the resume's close", path, closing+1, text)
		}
		var prompt, reply bool
		for i, line := range lines {
			evs, err := DecodeTranscriptLine(line)
			if err != nil {
				t.Fatalf("%s line %d: %v", path, i+1, err)
			}
			if i == closing && len(evs) > 0 {
				t.Errorf("%s line %d, the resume's close, decoded to a %s %q", path, i+1, evs[0].Kind, evs[0].Text)
			}
			for _, ev := range evs {
				prompt = prompt || (ev.Kind == KindUserText && ev.Text == "continue")
				reply = reply || (ev.Kind == KindAssistantText && ev.Text == "finished")
			}
		}
		if !prompt || !reply {
			t.Errorf("%s: the prompt after the close (%v) or the agent's reply (%v) went missing", path, prompt, reply)
		}
	}
}

// The <synthetic> model is what tells the close from an agent that writes the
// same words: on a real model's record they are the agent's reply, and draw.
func TestAnAgentThatWritesTheWordsStillDraws(t *testing.T) {
	lines := readLines(t, resumeFixtures[0])
	closing, agentModel := closingRecord(t, lines)
	var r map[string]any
	if err := json.Unmarshal(lines[closing], &r); err != nil {
		t.Fatal(err)
	}
	r["message"].(map[string]any)["model"] = agentModel
	line, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	evs, err := DecodeTranscriptLine(line)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != KindAssistantText || evs[0].Text != noResponseText || evs[0].LocalCommand {
		t.Errorf("an agent's own %q under %s decoded to %+v, want its one reply", noResponseText, agentModel, evs)
	}
}
