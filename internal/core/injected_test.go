package core

import (
	"encoding/json"
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

func marksOf(line []byte) (userMarks, bool) {
	var m userMarks
	if json.Unmarshal(line, &m) != nil || m.Type != "user" {
		return userMarks{}, false
	}
	return m, true
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
	check := func(path string, decode func([]byte) ([]Event, error)) {
		for i, line := range fixtureLines(t, path) {
			m, ok := marksOf([]byte(line))
			if !ok || !m.injected() {
				continue
			}
			for mark, on := range map[string]bool{"isMeta": m.Meta, "isSynthetic": m.Synthetic, "origin": m.Origin.Kind != ""} {
				if on {
					seen[mark]++
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
		check(f, DecodeTranscriptLine)
	}
	for _, f := range fixtureFiles(t) {
		check(f, DecodeLine)
	}
	for _, mark := range []string{"isMeta", "isSynthetic", "origin"} {
		if seen[mark] == 0 {
			t.Errorf("no recorded line carries %s: this guard is asserting nothing about it", mark)
		}
	}
}

// A mark the corpus records has a ruling. A new origin or promptSource is a new
// kind of line claude writes, and it is ruled here before anything draws it.
func TestEveryRecordedMarkIsRuled(t *testing.T) {
	for _, f := range append(transcriptFiles(t), fixtureFiles(t)...) {
		for i, line := range fixtureLines(t, f) {
			m, ok := marksOf([]byte(line))
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
