package core

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		`{"success":false,"message":"permission denied"}`,
		`{"success":true,"message":"sent","msg_id":""}`,
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

// A send's `to` is an address in two of its three forms: the `uds:` socket a
// cross-session message arrives from, which a reply copies (cross-session.jsonl),
// and a name with the ref /list-agents prints beside it (list-agents.jsonl). A
// bare name is no address.
func TestASendsAddressIsASocketOrANameWithItsRefDropped(t *testing.T) {
	for path, recorded := range map[string]string{
		"../../testdata/stream/cross-session.jsonl": `from=\"uds:/tmp/cc-socks/40123.sock\"`,
		"../../testdata/stream/list-agents.jsonl":   "wf-gamma [68bfa0]",
	} {
		if !strings.Contains(strings.Join(fixtureLines(t, path), "\n"), recorded) {
			t.Fatalf("%s no longer records %s", path, recorded)
		}
	}
	for to, want := range map[string][2]string{
		"uds:/tmp/cc-socks/40123.sock": {"/tmp/cc-socks/40123.sock", ""},
		"wf-gamma [68bfa0]":            {"", "wf-gamma"},
		"release notes [68bfa0]":       {"", "release notes"},
		"wf-gamma":                     {"", ""},
		"wf peer":                      {"", ""},
		"uds:":                         {"", ""},
		"[68bfa0]":                     {"", ""},
		"wf-gamma [not-a-ref]":         {"", ""},
	} {
		if socket, name := SendAddress(to); socket != want[0] || name != want[1] {
			t.Errorf("SendAddress(%q) = %q, %q; want %q, %q", to, socket, name, want[0], want[1])
		}
	}
}

// Claude Code's registry names a running session's id, its name and its socket
// (sessions/78871.json, a sterile 2.1.311 session); the name is contained, since
// it is drawn, and a record missing any of the three is none.
func TestASessionRecordNamesItsSessionNameAndSocket(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/sessions/78871.json")
	if err != nil {
		t.Fatal(err)
	}
	want := SessionRecord{ID: "dae33b75-42fc-4230-8dd2-19a731dc141d", Name: "rec-beta", Socket: "/tmp/cc-socks/78871.sock"}
	if got, ok := DecodeSessionRecord(raw); !ok || got != want {
		t.Errorf("DecodeSessionRecord = %+v, %v; want %+v", got, ok, want)
	}
	if got, _ := DecodeSessionRecord([]byte(`{"sessionId":"s1","name":"a\u001b[2Jb","messagingSocketPath":"/s"}`)); got.Name != "a [2Jb" {
		t.Errorf("a drawn name kept a terminal control: %q", got.Name)
	}
	for _, raw := range []string{`{`, `{"name":"x","messagingSocketPath":"/s"}`, `{"sessionId":"s1","name":"x"}`,
		`{"sessionId":"s1","messagingSocketPath":"/s"}`} {
		if got, ok := DecodeSessionRecord([]byte(raw)); ok {
			t.Errorf("DecodeSessionRecord(%s) = %+v, want none", raw, got)
		}
	}
}

// The registry's records are its *.json files and nothing else beside them: a
// session's .key file is never read.
func TestTheRegistryIsItsRecordsAndNotItsKeys(t *testing.T) {
	config := t.TempDir()
	dir := filepath.Join(config, "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"78871.json", "78871.0a1b2c.key"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(SessionRegistry(config))
	var got []string
	for _, e := range entries {
		if IsSessionRecord(e.Name()) {
			got = append(got, e.Name())
		}
	}
	if err != nil || !slices.Equal(got, []string{"78871.json"}) {
		t.Errorf("the registry's records are %v (%v), want the one record", got, err)
	}
}
