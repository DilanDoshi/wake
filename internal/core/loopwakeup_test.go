package core

import (
	"cmp"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

const loopWakeupFixture = "../../testdata/transcript/loop-wakeup-turn.jsonl"

// A turn claude starts itself for a loop is written to disk as a user record that
// carries the loop's own command, marked wakeupSource and turnOrigin "scheduled".
// It is claude's, never the operator's, and it restores as what the live stream
// calls it: a loop wakeup's started turn. loop-wakeup-turn.jsonl (2.1.329).
func TestALoopWakeupOnDiskIsClaudesOwnTurnNotTheOperators(t *testing.T) {
	lines := fixtureLines(t, loopWakeupFixture)
	if len(lines) != 1 {
		t.Fatalf("the fixture holds %d records, want the one wakeup record", len(lines))
	}
	var rec struct {
		UUID string `json:"uuid"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatal(err)
	}
	evs, err := DecodeTranscriptLine([]byte(lines[0]))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || operatorTurn(evs[0]) || !evs[0].LoopWakeup || !evs[0].MessageStarted() || evs[0].MessageID != rec.UUID {
		t.Errorf("the wakeup record decoded to %+v, want one loop wakeup started turn named by the record's uuid %s", evs, rec.UUID)
	}
}

// What makes a user record a turn claude started itself: wakeupSource, or
// turnOrigin "scheduled". It restores as a started turn, a loop's marked
// LoopWakeup, in the operator's place. A line claude injected (isMeta) says
// nothing, so a turn is never started twice, and what the record's text decodes
// to besides the operator's words - a notice - is left alone. A headless stdin's
// turn (Wake's own sends) carries another turnOrigin and stays the operator's.
func TestWhichUserRecordsAreATurnClaudeStartedItself(t *testing.T) {
	const twoBlocks = `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`
	const interrupted = `"[Request interrupted by user]"`
	for name, tc := range map[string]struct {
		marks, content string
		want           string // operator | started | loop | notice | nothing
	}{
		"a loop's wakeup":                      {`"wakeupSource":"loop_wakeup","turnOrigin":"scheduled"`, "", "loop"},
		"a loop wakeup's source alone":         {`"wakeupSource":"loop_wakeup"`, "", "loop"},
		"a wakeup prompt in two text blocks":   {`"wakeupSource":"loop_wakeup"`, twoBlocks, "loop"},
		"another wakeup's source":              {`"wakeupSource":"schedule_wakeup","turnOrigin":"scheduled"`, "", "started"},
		"a scheduled turn":                     {`"turnOrigin":"scheduled"`, "", "started"},
		"an isMeta record carrying the key":    {`"isMeta":true,"wakeupSource":"loop_wakeup"`, "", "nothing"},
		"an interruption on a wakeup's record": {`"wakeupSource":"loop_wakeup"`, interrupted, "notice"},
		"a headless stdin's turn":              {`"turnOrigin":"sdk"`, "", "operator"},
		"a turn typed interactively":           {`"turnOrigin":"human"`, "", "operator"},
		"no marks":                             {``, "", "operator"},
	} {
		t.Run(name, func(t *testing.T) {
			marks := tc.marks
			if marks != "" {
				marks += ","
			}
			line := fmt.Sprintf(`{"type":"user","uuid":"u1",%s"message":{"role":"user","content":%s}}`, marks, cmp.Or(tc.content, `"tick"`))
			evs, err := DecodeTranscriptLine([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			got := "nothing"
			if len(evs) > 0 {
				switch ev := evs[0]; {
				case operatorTurn(ev):
					got = "operator"
				case ev.Notice != "":
					got = "notice"
				case ev.MessageStarted() && ev.LoopWakeup:
					got = "loop"
				case ev.MessageStarted():
					got = "started"
				default:
					got = "something else: " + string(ev.Kind)
				}
			}
			if got != tc.want || len(evs) > 1 {
				t.Errorf("%s decoded to %d events, the first a %q, want one %q: %+v", strings.TrimSuffix(marks, ","), len(evs), got, tc.want, evs)
			}
		})
	}
}

// Only the operator's words are taken out: what else a marked record decodes to is
// kept, so a mark on a record that is not a user turn costs nothing.
func TestAMarkedRecordThatIsNotAUserTurnKeepsItsEvents(t *testing.T) {
	for name, marks := range map[string]string{"a wakeup": `"wakeupSource":"loop_wakeup"`, "a scheduled turn": `"turnOrigin":"scheduled"`, "an isMeta line": `"isMeta":true`} {
		line := `{"type":"assistant","uuid":"u1",` + marks + `,"message":{"role":"assistant","content":[{"type":"text","text":"tick"}]}}`
		evs, err := DecodeTranscriptLine([]byte(line))
		if err != nil || len(evs) != 1 || evs[0].Kind != KindAssistantText || evs[0].Text != "tick" {
			t.Errorf("%s: an assistant record decoded to %+v (err=%v), want its text", name, evs, err)
		}
	}
}
