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
	var typed, started int
	var wakeupID string
	for i, line := range fixtureLines(t, loopWakeupFixture) {
		var rec struct {
			UUID         string `json:"uuid"`
			WakeupSource string `json:"wakeupSource"`
		}
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		if rec.WakeupSource != "" {
			wakeupID = rec.UUID
		}
		evs, err := DecodeTranscriptLine([]byte(line))
		if err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		for _, ev := range evs {
			if operatorTurn(ev) {
				typed++
			}
			if ev.LoopWakeup && ev.MessageStarted() {
				started++
				if ev.MessageID != wakeupID {
					t.Errorf("line %d: the wakeup's started turn names %q, want the record's own uuid %q", i+1, ev.MessageID, wakeupID)
				}
			}
		}
	}
	if typed != 1 {
		t.Errorf("%d operator turns restored, want only the /loop the operator typed", typed)
	}
	if started != 1 {
		t.Errorf("%d loop wakeups restored, want the one the record carries", started)
	}
}

// What makes a user record a turn claude started itself: wakeupSource, or
// turnOrigin "scheduled". A headless stdin's turn (Wake's own sends) and a typed
// one carry other turnOrigins and stay the operator's. A loop's isMeta companion
// carries the wakeupSource too and says nothing, or the wakeup would count twice.
func TestWhichUserRecordsAreATurnClaudeStartedItself(t *testing.T) {
	for name, tc := range map[string]struct {
		marks, content   string
		operator, wakeup bool
	}{
		"a loop's wakeup":                    {`"wakeupSource":"loop_wakeup","turnOrigin":"scheduled"`, "", false, true},
		"a loop wakeup's source":             {`"wakeupSource":"loop_wakeup"`, "", false, true},
		"a wakeup prompt in two text blocks": {`"wakeupSource":"loop_wakeup"`, `[{"type":"text","text":"a"},{"type":"text","text":"b"}]`, false, true},
		"its isMeta companion":               {`"isMeta":true,"wakeupSource":"loop_wakeup"`, "", false, false},
		"another wakeup's source":            {`"wakeupSource":"schedule_wakeup","turnOrigin":"scheduled"`, "", false, false},
		"a scheduled turn":                   {`"turnOrigin":"scheduled"`, "", false, false},
		"a headless stdin's turn":            {`"turnOrigin":"sdk"`, "", true, false},
		"a turn typed interactively":         {`"turnOrigin":"human"`, "", true, false},
		"no marks":                           {``, "", true, false},
	} {
		t.Run(name, func(t *testing.T) {
			marks := tc.marks
			if marks != "" {
				marks += ","
			}
			content := cmp.Or(tc.content, `"tick"`)
			line := fmt.Sprintf(`{"type":"user","uuid":"u1",%s"message":{"role":"user","content":%s}}`, marks, content)
			evs, err := DecodeTranscriptLine([]byte(line))
			if err != nil {
				t.Fatal(err)
			}
			var operator, wakeup int
			for _, ev := range evs {
				if operatorTurn(ev) {
					operator++
				}
				if ev.LoopWakeup && ev.MessageStarted() {
					wakeup++
				}
			}
			if (operator == 1) != tc.operator || (wakeup == 1) != tc.wakeup || len(evs) > 1 {
				t.Errorf("%s decoded to %+v: %d operator turns, %d loop wakeups; want operator %v, wakeup %v",
					strings.TrimSuffix(marks, ","), evs, operator, wakeup, tc.operator, tc.wakeup)
			}
		})
	}
}
