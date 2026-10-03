package core

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The manager's one built-in is the one its recorded init names
// (manager-tools.jsonl, recorded with the manager's own tool flags). An unknown
// --tools name is dropped without a word, so a misspelt constant would be a
// manager with nothing to relay through and no error anywhere.
func TestTheManagersBuiltInIsTheOneItsRecordedInitNames(t *testing.T) {
	var init struct {
		Tools []string `json:"tools"`
	}
	if err := json.Unmarshal([]byte(fixtureLines(t, "../../testdata/stream/manager-tools.jsonl")[0]), &init); err != nil {
		t.Fatalf("read the recorded init: %v", err)
	}
	if want := []string{ToolSendMessage}; !slices.Equal(init.Tools, want) {
		t.Errorf("the manager's recorded init.tools = %q, want %q", init.Tools, want)
	}
}

// A SendMessage call carries who it went to and what it said, and its result
// reads as the sentence claude wrote rather than the JSON it arrived in
// (manager-relay.jsonl: one live turn relaying the operator's `@"wf peer" …`).
func TestASendMessageCallCarriesItsRecipientAndText(t *testing.T) {
	var sends []PeerSend
	var results []string
	for n, line := range fixtureLines(t, "../../testdata/stream/manager-relay.jsonl") {
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("manager-relay.jsonl:%d: %v", n+1, err)
		}
		for _, ev := range evs {
			switch {
			case ev.Kind == KindToolUse && ev.Tool != nil && ev.Tool.Send != nil:
				sends = append(sends, *ev.Tool.Send)
			case ev.Kind == KindToolResult:
				results = append(results, ev.Text)
			}
		}
	}
	if want := []PeerSend{{To: "wf peer", Text: "are you there? reply if you can"}}; !reflect.DeepEqual(sends, want) {
		t.Errorf("sends = %+v, want %+v", sends, want)
	}
	if len(results) != 1 || !strings.HasPrefix(results[0], "“relay operator ping to wf peer” → wf peer (") {
		t.Errorf("results = %q, want the one receipt's own sentence", results)
	}
}

// Only a SendMessage with somebody to reach and something to say is a send.
func TestOnlyASendMessageWithARecipientAndTextIsASend(t *testing.T) {
	for name, tc := range map[string]struct {
		tool  string
		input map[string]any
	}{
		"another tool":      {"Write", map[string]any{"to": "wf peer", "message": "hi"}},
		"no recipient":      {ToolSendMessage, map[string]any{"message": "hi"}},
		"a blank recipient": {ToolSendMessage, map[string]any{"to": " ", "message": "hi"}},
		"no text":           {ToolSendMessage, map[string]any{"to": "wf peer"}},
	} {
		if got := toolCall("t1", tc.tool, tc.input).Send; got != nil {
			t.Errorf("%s: Send = %+v, want none", name, got)
		}
	}
}

// Only a receipt of SendMessage's recorded shape is read as its sentence: any
// other JSON text - an MCP tool's error, an API body - keeps every key it has.
func TestOnlyASendMessageReceiptIsReadAsItsSentence(t *testing.T) {
	block := func(text string) json.RawMessage {
		raw, _ := json.Marshal([]map[string]string{{"type": "text", "text": text}})
		return raw
	}
	for _, text := range []string{
		`{"message":"failed","code":"E_AUTH","retry_after":30}`,
		`{"message":"Not Found","documentation_url":"https://example.com"}`,
		`{"message":""}`,
		`{"success":true}`,
	} {
		if got := toolResultText(block(text)); got != text {
			t.Errorf("toolResultText(%s) = %q, want it verbatim", text, got)
		}
	}
	receipt := `{"success":true,"message":"“hi” → wf peer","msg_id":"m1"}`
	if got := toolResultText(block(receipt)); got != "“hi” → wf peer" {
		t.Errorf("the receipt read as %q, want its sentence", got)
	}
}
