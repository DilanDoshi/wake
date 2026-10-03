package core

// A transcript record's own uuid reaches its events as MessageID. Two recorded
// facts make it worth carrying (2.1.285, sterile HOME, no model turn;
// docs/superpowers/notes/2026-09-29-transcript-uuid-findings.md):
//
//   - the uuid Wake stamps on a user line it writes is the uuid claude records
//     on disk for that turn - room-stamped-uuid.{stdin,}.jsonl;
//   - a --fork-session transcript copies its parent's records under the same
//     uuids - fork-parent.jsonl and fork-child.jsonl.

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodedTranscript(t *testing.T, path string) []Event {
	t.Helper()
	var out []Event
	for _, line := range readLines(t, path) {
		events, err := DecodeTranscriptLine(line)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out = append(out, events...)
	}
	return out
}

func recordUUID(t *testing.T, line []byte) string {
	t.Helper()
	var f struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal(line, &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return f.UUID
}

func TestTheUUIDWakeStampsOnASendIsTheOneOnDisk(t *testing.T) {
	stdin := readLines(t, "testdata/input/room-stamped-uuid.stdin.jsonl")[0]
	stamped := recordUUID(t, stdin)
	encoded, err := EncodeUserMessage("room hello", nil, stamped, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(encoded)); got != strings.TrimSpace(string(stdin)) {
		t.Fatalf("EncodeUserMessage no longer writes the recorded line:\n got %s\nwant %s", got, stdin)
	}
	events := decodedTranscript(t, "testdata/transcript/room-stamped-uuid.jsonl")
	if len(events) != 1 || events[0].Kind != KindUserText || events[0].MessageID != stamped {
		t.Errorf("the on-disk turn does not carry the uuid Wake stamped (%s): %+v", stamped, events)
	}
}

func TestAForkCopiesItsParentsRecordsUnderTheSameUUIDs(t *testing.T) {
	parent := decodedTranscript(t, "testdata/transcript/fork-parent.jsonl")
	child := decodedTranscript(t, "testdata/transcript/fork-child.jsonl")
	if len(parent) != 1 || len(child) != 2 {
		t.Fatalf("the recordings changed shape: parent %d, child %d user events", len(parent), len(child))
	}
	if child[0].MessageID == "" || child[0].MessageID != parent[0].MessageID || child[0].Text != parent[0].Text {
		t.Errorf("the fork's copy of the parent's turn does not share its uuid: parent %q, copy %q", parent[0].MessageID, child[0].MessageID)
	}
	if !child[0].At.Equal(parent[0].At) {
		t.Errorf("the fork's copy carries a different time (%v vs %v) - the multiplicity rule's leak depends on it not", child[0].At, parent[0].At)
	}
	if child[1].MessageID == parent[0].MessageID {
		t.Error("the fork's own turn carries the parent's uuid")
	}
}

// Every event a conversation record decodes to carries that record's uuid,
// assistant blocks included - which is what lets a fork's copied prose be told
// from its own.
func TestEveryTranscriptEventCarriesItsRecordsUUID(t *testing.T) {
	for _, line := range readLines(t, "testdata/transcript/rewind-tree.jsonl") {
		events, err := DecodeTranscriptLine(line)
		if err != nil {
			t.Fatal(err)
		}
		want := recordUUID(t, line)
		for _, ev := range events {
			if ev.MessageID != want {
				t.Errorf("a %v event carries %q, want its record's uuid %q", ev.Kind, ev.MessageID, want)
			}
		}
	}
}
