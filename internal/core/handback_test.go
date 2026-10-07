package core

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// A subagent that reports through SubagentHandback (claude 2.1.271+, auto mode
// only - and Wake spawns auto) hands its report back as an <agent-message>
// envelope on a user line. It is claude's note that a background task ended,
// the job <task-notification> did before it, so it is no event on either wire:
// live it would otherwise be an Echoed user turn, restored the operator's own.
// testdata/stream/subagent-handback.jsonl, testdata/transcript/subagent-handback.jsonl.
func TestASubagentHandbackIsNoEventOnEitherWire(t *testing.T) {
	stream := fixtureLines(t, filepath.Join("..", "..", "testdata", "stream", "subagent-handback.jsonl"))
	evs, err := DecodeLine([]byte(stream[0]))
	if err != nil {
		t.Fatalf("DecodeLine: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("the live hand-back decoded to %+v, want no event", evs)
	}

	disk := fixtureLines(t, filepath.Join("..", "..", "testdata", "transcript", "subagent-handback.jsonl"))
	var shape struct {
		Origin struct {
			Handback bool `json:"handback"`
		} `json:"origin"`
	}
	if err := json.Unmarshal([]byte(disk[0]), &shape); err != nil || !shape.Origin.Handback {
		t.Fatalf("fixture line 1 is not a hand-back (origin.handback=%v, err=%v)", shape.Origin.Handback, err)
	}
	evs, err = DecodeTranscriptLine([]byte(disk[0]))
	if err != nil {
		t.Fatalf("DecodeTranscriptLine: %v", err)
	}
	if len(evs) != 0 {
		t.Errorf("the restored hand-back decoded to %+v, want no event: it came back as the operator's own turn", evs)
	}
}

// A turn that quotes the envelope stays the operator's: Wake's own sends are
// array content, and a hand-started claude writes a typed turn as a string with
// neither isSynthetic (the live hand-back's mark) nor isMeta (its mark on disk).
func TestATypedTurnQuotingTheEnvelopeStaysTheUsersTurn(t *testing.T) {
	const quote = `look at <agent-message from=\"a1\">report</agent-message>`
	for name, line := range map[string]string{
		"array":  `{"type":"user","session_id":"s1","message":{"role":"user","content":[{"type":"text","text":"` + quote + `"}]}}`,
		"string": `{"type":"user","session_id":"s1","message":{"role":"user","content":"` + quote + `"}}`,
	} {
		for wire, decode := range map[string]func([]byte) ([]Event, error){"stream": DecodeLine, "disk": DecodeTranscriptLine} {
			evs, err := decode([]byte(line))
			if err != nil || len(evs) != 1 || evs[0].Kind != KindUserText {
				t.Errorf("%s content on the %s: got %+v (err=%v), want the typed turn", name, wire, evs, err)
			}
		}
	}
}

// isMeta is claude's own mark on a line it injected - a skill's body, an image
// note, an idle notice, a continuation nudge. One no decoder claims is never the
// operator's turn; what it is instead is decided per kind (cross-session is one,
// claimed before this). Only on the marker: the same text without it is typed.
// testdata/transcript/injected-meta.jsonl.
func TestAnUnclaimedMetaLineIsNeverTheOperatorsTurn(t *testing.T) {
	for i, line := range fixtureLines(t, filepath.Join("..", "..", "testdata", "transcript", "injected-meta.jsonl")) {
		evs, err := DecodeTranscriptLine([]byte(line))
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if len(evs) != 0 {
			t.Errorf("line %d decoded to %+v, want no event", i+1, evs)
		}
		evs, err = DecodeTranscriptLine([]byte(strings.Replace(line, `"isMeta":true,`, "", 1)))
		if err != nil || len(evs) != 1 || evs[0].Kind != KindUserText {
			t.Errorf("line %d without isMeta decoded to %+v (err=%v), want the typed turn it would then be", i+1, evs, err)
		}
	}
}
