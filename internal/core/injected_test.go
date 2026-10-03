package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// Claude marks a user line it wrote itself - isMeta on disk, isSynthetic on the
// stream, an origin other than a human, promptSource "system". Such a line is
// never the operator's turn (decisions.md, 2026-10-03). These tables say what
// each recorded mark is; drift_test.go holds this machine's own transcripts to
// the same tables, so a mark seen in the wild and one recorded are ruled alike.

// ruledOrigins is every origin.kind a user line has been seen to carry.
var ruledOrigins = map[string]string{
	"human":             "typed by the operator in an interactive claude",
	"peer":              "a peer's message (KindCrossSession) or a subagent's hand-back (dropped)",
	"task-notification": "a background task's ending, dropped by decodeTranscript",
	"auto-continuation": "claude resuming after a usage-limit reset; isMeta, dropped",
}

// ruledPromptSources is every promptSource a user line has been seen to carry.
var ruledPromptSources = map[string]string{
	"typed":  "typed at an interactive prompt",
	"queued": "typed while a turn ran, delivered after it",
	"sdk":    "a headless session's stdin, which is Wake's own sends",
	"system": "claude's own injection",
}

// userMarks are the marks a user line carries, read off the line itself rather
// than off what the decoder made of it.
type userMarks struct {
	Type         string `json:"type"`
	Meta         bool   `json:"isMeta"`
	Synthetic    bool   `json:"isSynthetic"`
	PromptSource string `json:"promptSource"`
	Origin       struct {
		Kind string `json:"kind"`
	} `json:"origin"`
}

// marksOf reads a user line's marks. ok is false for a line that is not one;
// err is a user line whose marks changed type - the drift these tests watch for,
// so it is reported rather than read as unmarked.
func marksOf(line []byte) (m userMarks, ok bool, err error) {
	var kind struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(line, &kind) != nil || kind.Type != "user" {
		return userMarks{}, false, nil
	}
	if err := json.Unmarshal(line, &m); err != nil {
		return userMarks{}, false, err
	}
	// encoding/json reads a null as the zero value, so a mark claude starts
	// writing as null would otherwise read as absent.
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(line, &raw)
	for _, key := range []string{"isMeta", "isSynthetic", "promptSource", "origin"} {
		if string(raw[key]) == "null" {
			return userMarks{}, false, fmt.Errorf("mark %s is null", key)
		}
	}
	return m, true, nil
}

func (m userMarks) injected() bool {
	return m.Meta || m.Synthetic || m.PromptSource == "system" || (m.Origin.Kind != "" && m.Origin.Kind != "human")
}

// operatorTurn is an event a view draws as the operator's own words.
func operatorTurn(ev Event) bool {
	return ev.Kind == KindUserText && !ev.Echoed && ev.Notice == "" && ev.Subagent == nil && strings.TrimSpace(ev.Text) != ""
}

// Every line the corpus records as claude's own decodes to something other than
// the operator's turn, on both wires. Red on 1e21aa1 once the hand-back and
// injected-meta fixtures exist: that is the regression this guard is for.
func TestNoInjectedLineInTheCorpusIsTheOperatorsTurn(t *testing.T) {
	seen := map[string]int{}
	check := func(wire, path string, decode func([]byte) ([]Event, error)) {
		for i, line := range fixtureLines(t, path) {
			m, ok, err := marksOf([]byte(line))
			if err != nil {
				t.Errorf("%s:%d: a mark changed type: %v", path, i+1, err)
			}
			if !ok || !m.injected() {
				continue
			}
			for mark, on := range m.present() {
				if on {
					seen[wire+" "+mark]++
				}
			}
			evs, err := decode([]byte(line))
			if err != nil {
				t.Errorf("%s:%d: %v", path, i+1, err)
			}
			for _, ev := range evs {
				if operatorTurn(ev) {
					t.Errorf("%s:%d is claude's own line and decodes as the operator's turn: %.60q", path, i+1, ev.Text)
				}
			}
		}
	}
	for _, f := range transcriptFiles(t) {
		check("transcript", f, DecodeTranscriptLine)
	}
	for _, f := range fixtureFiles(t) {
		check("stream", f, DecodeLine)
	}
	// Each mark on the wire that carries it, so neither half can go unasserted.
	for _, want := range []string{"transcript isMeta", "transcript origin", "transcript system", "stream isSynthetic", "stream origin"} {
		if seen[want] == 0 {
			t.Errorf("no recorded %s line: this guard is asserting nothing about it", want)
		}
	}
}

// present names the marks a line carries, origin only when it is not a human's.
func (m userMarks) present() map[string]bool {
	return map[string]bool{
		"isMeta": m.Meta, "isSynthetic": m.Synthetic, "system": m.PromptSource == "system",
		"origin": m.Origin.Kind != "" && m.Origin.Kind != "human",
	}
}

// Each mark alone makes a line claude's own, so no arm of injected() can go
// missing behind another: every recorded system line also carries isMeta.
func TestEachMarkAloneMakesALineInjected(t *testing.T) {
	for line, want := range map[string]bool{
		`{"type":"user","isMeta":true}`:                      true,
		`{"type":"user","isSynthetic":true}`:                 true,
		`{"type":"user","promptSource":"system"}`:            true,
		`{"type":"user","origin":{"kind":"peer"}}`:           true,
		`{"type":"user","origin":{"kind":"human"}}`:          false,
		`{"type":"user","promptSource":"sdk"}`:               false,
		`{"type":"user","isMeta":false,"isSynthetic":false}`: false,
	} {
		m, ok, err := marksOf([]byte(line))
		if !ok || err != nil || m.injected() != want {
			t.Errorf("%s: injected() = %v (ok=%v err=%v), want %v", line, m.injected(), ok, err, want)
		}
	}
	for _, changed := range []string{`{"type":"user","isMeta":"yes"}`, `{"type":"user","isMeta":null}`, `{"type":"user","origin":null}`} {
		if _, ok, err := marksOf([]byte(changed)); ok || err == nil {
			t.Errorf("%s: a mark that changed type was read as unmarked rather than reported", changed)
		}
	}
}

// A mark the corpus records has a ruling. A new origin or promptSource is a new
// kind of line claude writes, and it is ruled here before anything draws it.
func TestEveryRecordedMarkIsRuled(t *testing.T) {
	for _, f := range append(transcriptFiles(t), fixtureFiles(t)...) {
		for i, line := range fixtureLines(t, f) {
			m, ok, err := marksOf([]byte(line))
			if err != nil {
				t.Errorf("%s:%d: a mark changed type: %v", f, i+1, err)
			}
			if !ok {
				continue
			}
			if _, ruled := ruledOrigins[m.Origin.Kind]; m.Origin.Kind != "" && !ruled {
				t.Errorf("%s:%d carries origin %q, which ruledOrigins does not rule", f, i+1, m.Origin.Kind)
			}
			if _, ruled := ruledPromptSources[m.PromptSource]; m.PromptSource != "" && !ruled {
				t.Errorf("%s:%d carries promptSource %q, which ruledPromptSources does not rule", f, i+1, m.PromptSource)
			}
		}
	}
}
