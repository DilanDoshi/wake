package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// loopWakeupStarted is what core restores a loop wakeup's on-disk record as
// (testdata/transcript/loop-wakeup-turn.jsonl), placed in one session at one time.
func loopWakeupStarted(t *testing.T, session string, at time.Time) core.Event {
	t.Helper()
	line, err := os.ReadFile("../../testdata/transcript/loop-wakeup-turn.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	evs, err := core.DecodeTranscriptLine([]byte(strings.TrimSpace(string(line))))
	if err != nil || len(evs) != 1 {
		t.Fatalf("the wakeup record decoded to %+v (err=%v), want one event", evs, err)
	}
	ev := evs[0]
	ev.SessionID, ev.At = session, at
	return ev
}

// A turn claude starts itself is not one the room sent, so what the agent says in
// it is not the broadcast's: it closes the public turn the way a user turn does,
// and the reply to it stays out of a restored room.
func TestATurnClaudeStartsItselfClosesAPublicTurnInARestoredRoom(t *testing.T) {
	for name, started := range map[string]core.Event{
		"a loop's wakeup":        loopWakeupStarted(t, "s1", base.Add(2*time.Second)),
		"another scheduled turn": {Kind: core.KindMessageState, Text: "started", SessionID: "s1", At: base.Add(2 * time.Second)},
	} {
		t.Run(name, func(t *testing.T) {
			r := restored(
				[]core.Event{
					opener("s1", base),
					heard("s1", "s1 carrying on", base.Add(time.Second)),
					started,
					heard("s1", "tick from the wakeup", base.Add(3*time.Second)),
				},
				[]core.Event{
					opener("s2", base.Add(40*time.Millisecond)),
					heard("s2", "s2 carrying on", base.Add(time.Second+40*time.Millisecond)),
				},
			)
			got, want := texts(r), []string{openerText, "s1 carrying on", "s2 carrying on"}
			if strings.Join(got, "|") != strings.Join(want, "|") {
				t.Errorf("the room restored %v, want %v: the reply to a turn nobody broadcast is not the broadcast's", got, want)
			}
		})
	}
}
