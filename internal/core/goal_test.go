package core

import (
	"path/filepath"
	"testing"
)

// goalOps decodes a fixture and returns every KindGoal op it produced, proving
// the airlock recognises the /goal lifecycle from the synthetic message shape.
func goalOps(t *testing.T, fixture string) []GoalOp {
	t.Helper()
	var ops []GoalOp
	path := filepath.Join("../../testdata/stream", fixture)
	for n, line := range fixtureLines(t, path) {
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("%s:%d decode: %v", fixture, n+1, err)
		}
		for _, ev := range evs {
			if ev.Kind != KindGoal {
				continue
			}
			if ev.Goal == nil {
				t.Fatalf("%s:%d KindGoal with nil Goal", fixture, n+1)
			}
			ops = append(ops, *ev.Goal)
		}
	}
	return ops
}

func TestGoalSetAndProgressDecode(t *testing.T) {
	const cond = "count from 1 to 3, one number per turn, then say COUNT DONE"
	ops := goalOps(t, "goal-achieved.jsonl")
	if len(ops) != 3 {
		t.Fatalf("goal-achieved produced %d goal ops, want 3 (set + two progress)", len(ops))
	}
	if ops[0].Op != GoalSet || ops[0].Condition != cond {
		t.Errorf("op 0 = %+v, want set with the condition", ops[0])
	}
	for i, op := range ops[1:] {
		if op.Op != GoalProgress {
			t.Errorf("op %d = %q, want progress", i+1, op.Op)
		}
		// A progress refresh re-carries the condition (drives the next turn) and
		// the evaluator's latest reason.
		if op.Condition != cond {
			t.Errorf("progress %d condition = %q, want the goal's own", i+1, op.Condition)
		}
		if op.Reason == "" {
			t.Errorf("progress %d has no reason", i+1)
		}
	}
	// Achieve is silent: the last turn is not followed by a clearing frame.
	for _, op := range ops {
		if op.Op == GoalCleared || op.Op == GoalNone {
			t.Errorf("goal-achieved emitted %q, but achieve carries no frame", op.Op)
		}
	}
}

func TestGoalClearedAndNoneDecode(t *testing.T) {
	ops := goalOps(t, "goal-cleared.jsonl")
	if len(ops) != 3 {
		t.Fatalf("goal-cleared produced %d ops, want 3 (set, cleared, none)", len(ops))
	}
	if ops[1].Op != GoalCleared || ops[1].Condition != "keep the build green" {
		t.Errorf("op 1 = %+v, want cleared with the condition", ops[1])
	}
	if ops[2].Op != GoalNone || ops[2].Condition != "" {
		t.Errorf("op 2 = %+v, want none with no condition", ops[2])
	}
}

func TestGoalStatusDecodes(t *testing.T) {
	ops := goalOps(t, "goal-status.jsonl")
	if len(ops) != 2 {
		t.Fatalf("goal-status produced %d ops, want 2", len(ops))
	}
	for i, op := range ops {
		if op.Op != GoalSet || op.Condition != "ship PR 123 with a green build" {
			t.Errorf("op %d = %+v, want set with the condition", i, op)
		}
	}
}

// A recognised goal frame is machinery, not prose: it produces the KindGoal
// event and suppresses the assistant/user text it would otherwise render.
func TestGoalFramesSuppressTheirText(t *testing.T) {
	line := `{"type":"assistant","message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Goal set: do the thing"}]},"session_id":"s","uuid":"u"}`
	evs, err := DecodeLine([]byte(line))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != KindGoal {
		t.Fatalf("goal frame decoded to %d events %v, want one KindGoal", len(evs), kinds(evs))
	}
}

// The synthetic-model gate keeps an agent's own prose from being read as the
// command, and a real assistant turn stays assistant text.
func TestGoalGateIgnoresAgentProse(t *testing.T) {
	// A real model saying the words is not a goal.
	prose := `{"type":"assistant","message":{"model":"claude-sonnet-5","role":"assistant","content":[{"type":"text","text":"Goal set: this is just me talking"}]},"session_id":"s","uuid":"u"}`
	evs, err := DecodeLine([]byte(prose))
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Kind != KindAssistantText {
		t.Errorf("agent prose decoded to %v, want KindAssistantText", kinds(evs))
	}
	// A synthetic frame that is not a goal announcement (the bare-/model receipt)
	// is not hijacked.
	model := `{"type":"assistant","message":{"model":"<synthetic>","role":"assistant","content":[{"type":"text","text":"Current model: Opus 5"}]},"session_id":"s","uuid":"u"}`
	evs, err = DecodeLine([]byte(model))
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Kind == KindGoal {
			t.Errorf("the bare-/model receipt was decoded as a goal")
		}
	}
}
