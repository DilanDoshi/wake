package core

import (
	"path/filepath"
	"testing"
)

// loopOps decodes a fixture and returns every LoopOp a scheduler tool_use
// produced, proving the airlock recognises /loop from the bundled scheduler
// tools the headless model reaches for.
func loopOps(t *testing.T, fixture string) []LoopOp {
	t.Helper()
	var ops []LoopOp
	path := filepath.Join("../../testdata/stream", fixture)
	for n, line := range fixtureLines(t, path) {
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s:%d decode: %v", fixture, n+1, err)
		}
		for _, ev := range evs {
			if ev.Kind == KindToolUse && ev.Tool != nil && ev.Tool.Loop != nil {
				ops = append(ops, *ev.Tool.Loop)
			}
		}
	}
	return ops
}

func TestFixedLoopDecodes(t *testing.T) {
	ops := loopOps(t, "loop-fixed.jsonl")
	if len(ops) != 1 {
		t.Fatalf("loop-fixed produced %d loop ops, want 1 (the recurring CronCreate)", len(ops))
	}
	if ops[0].Kind != LoopFixed || ops[0].Cron != "*/5 * * * *" {
		t.Errorf("op = %+v, want fixed with the cron", ops[0])
	}
}

func TestSelfPacedLoopDecodes(t *testing.T) {
	ops := loopOps(t, "loop-selfpaced.jsonl")
	if len(ops) != 1 {
		t.Fatalf("loop-selfpaced produced %d loop ops, want 1 (the ScheduleWakeup)", len(ops))
	}
	if ops[0].Kind != LoopSelfPaced || ops[0].DelaySeconds != 1200 || ops[0].Noop {
		t.Errorf("op = %+v, want self-paced with delaySeconds 1200 and noop false", ops[0])
	}
}

// A recorded self-paced run: three ScheduleWakeups, the last two quiet. It is the
// canonical stream a "quiet ×N" streak folds from - the accumulation itself is
// daemon/loop_test's, this proves the airlock recognises every tick of a real run.
func TestSelfPacedRunDecodesEveryTick(t *testing.T) {
	ops := loopOps(t, "loop-selfpaced-run.jsonl")
	if len(ops) != 3 {
		t.Fatalf("loop-selfpaced-run produced %d loop ops, want 3 ScheduleWakeups", len(ops))
	}
	quiet := 0
	for i, op := range ops {
		if op.Kind != LoopSelfPaced {
			t.Errorf("op %d = %+v, want self-paced", i, op)
		}
		if op.Noop {
			quiet++
		}
	}
	if quiet != 2 {
		t.Errorf("the run had %d quiet ticks, want 2", quiet)
	}
}

// A one-shot CronCreate is a reminder, not a loop; only a recurring one counts.
func TestOneShotCronIsNotALoop(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-opus-4-8","role":"assistant","content":[{"type":"tool_use","id":"t","name":"CronCreate","input":{"cron":"0 15 * * *","prompt":"remind me","recurring":false}}]},"session_id":"s","uuid":"u"}`
	for _, ev := range decodeOne(t, line) {
		if ev.Tool != nil && ev.Tool.Loop != nil {
			t.Errorf("a one-shot CronCreate decoded as a loop: %+v", ev.Tool.Loop)
		}
	}
}

// CronDelete ends a loop.
func TestCronDeleteStopsTheLoop(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-opus-4-8","role":"assistant","content":[{"type":"tool_use","id":"t","name":"CronDelete","input":{"id":"8bd517f7"}}]},"session_id":"s","uuid":"u"}`
	var got *LoopOp
	for _, ev := range decodeOne(t, line) {
		if ev.Tool != nil && ev.Tool.Loop != nil {
			got = ev.Tool.Loop
		}
	}
	if got == nil || !got.Stop {
		t.Errorf("CronDelete loop op = %+v, want Stop true", got)
	}
}

// A quiet self-paced tick sets Noop, which is the "quiet ×N" streak.
func TestSelfPacedNoopIsAQuietTick(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-opus-4-8","role":"assistant","content":[{"type":"tool_use","id":"t","name":"ScheduleWakeup","input":{"delaySeconds":1800,"noop":true,"prompt":"/loop watch CI"}}]},"session_id":"s","uuid":"u"}`
	var got *LoopOp
	for _, ev := range decodeOne(t, line) {
		if ev.Tool != nil && ev.Tool.Loop != nil {
			got = ev.Tool.Loop
		}
	}
	if got == nil || got.Kind != LoopSelfPaced || !got.Noop {
		t.Errorf("quiet self-paced op = %+v, want self-paced with Noop true", got)
	}
}

// A non-scheduler tool carries no loop op.
func TestOrdinaryToolHasNoLoop(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"claude-opus-4-8","role":"assistant","content":[{"type":"tool_use","id":"t","name":"Bash","input":{"command":"ls"}}]},"session_id":"s","uuid":"u"}`
	for _, ev := range decodeOne(t, line) {
		if ev.Tool != nil && ev.Tool.Loop != nil {
			t.Errorf("Bash carried a loop op: %+v", ev.Tool.Loop)
		}
	}
}
