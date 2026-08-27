// The task dimension of the airlock: the five system subtypes that report a
// dispatch's lifecycle, and the join that ties them to the speech they
// describe.
//
// Split from protocol_subagent_test.go, which owns attribution off a forwarded
// frame. These two halves meet on exactly one field - a task's Dispatch is a
// forwarded frame's Subagent.Dispatch - and TestTaskStartedJoinsBothIdentifier
// Spaces is where that is held to the corpus rather than to a comment.

package core

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The frame that opens a dispatch, and the only one carrying both identifier
// spaces. Everything a row shows before the first progress frame comes from
// here.
func TestTaskStartedIsDecodedAsATask(t *testing.T) {
	line := `{"type":"system","subtype":"task_started","task_id":"ab1b72d53680ae187","tool_use_id":"toolu_01Wyw","description":"Write tally.txt with hello","subagent_type":"general-purpose","task_type":"local_agent","prompt":"…","session_id":"s1"}`

	ev := onlyEvent(t, line, 0)
	if ev.Task == nil {
		t.Fatal("Task is nil: a dispatch starting is indistinguishable from lifecycle chatter")
	}
	if ev.Task.ID != "ab1b72d53680ae187" {
		t.Errorf("ID = %q, want the task_id", ev.Task.ID)
	}
	if ev.Task.Dispatch != "toolu_01Wyw" {
		t.Errorf("Dispatch = %q, want the tool_use_id - it is what ties this row to the frames the subagent forwards", ev.Task.Dispatch)
	}
	if ev.Task.Kind != TaskAgent {
		t.Errorf("Kind = %q, want %q", ev.Task.Kind, TaskAgent)
	}
	if ev.Task.Phase != TaskStarted {
		t.Errorf("Phase = %q, want %q", ev.Task.Phase, TaskStarted)
	}
	if ev.Task.Label != "Write tally.txt with hello" {
		t.Errorf("Label = %q, want the description - the only human-readable name on the frame", ev.Task.Label)
	}
	if ev.Task.Type != "general-purpose" {
		t.Errorf("Type = %q, want the subagent_type", ev.Task.Type)
	}
	if ev.Task.Status != TaskRunning {
		t.Errorf("Status = %q, want %q - a task that has just started has not ended", ev.Task.Status, TaskRunning)
	}
}

// The prompt is the subagent's entire instruction and rides 9 of the 10
// recorded task_started frames. Nothing draws it, so nothing decodes it: this
// package's rule is that a field arrives when something needs it.
func TestTheSubagentsPromptIsNotCarriedUp(t *testing.T) {
	line := `{"type":"system","subtype":"task_started","task_id":"a1","tool_use_id":"toolu_1","description":"d","task_type":"local_agent","prompt":"SECRET INSTRUCTION"}`

	ev := onlyEvent(t, line, 0)
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(b); strings.Contains(got, "SECRET INSTRUCTION") {
		t.Errorf("the prompt reached the event: %s", got)
	}
}

// A task is not always a subagent, and this is the whole reason Kind exists.
// interrupt-cancel-queued-empty.jsonl:38 is a background *shell* announced by
// the same subtype - no subagent_type, a differently shaped id, and no
// forwarded frames anywhere. Filed as a subagent it offers a drill-in onto an
// empty transcript.
func TestABackgroundShellIsNotASubagent(t *testing.T) {
	line := `{"type":"system","subtype":"task_started","task_id":"brzd9s5t3","tool_use_id":"toolu_01T64","description":"waiting for /tmp/wake-spike-sentinel to be created","task_type":"local_bash"}`

	ev := onlyEvent(t, line, 0)
	if ev.Task == nil {
		t.Fatal("Task is nil")
	}
	if ev.Task.Kind != TaskShell {
		t.Errorf("Kind = %q, want %q - local_bash is a shell, and it has no transcript to open", ev.Task.Kind, TaskShell)
	}
	if ev.Task.Type != "" {
		t.Errorf("Type = %q, want empty - this frame carries no subagent_type", ev.Task.Type)
	}
}

// Two task types are recorded and the binary's own wording hints at more. An
// unrecorded one must not inherit either reading: shown, never entered.
func TestAnUnrecordedTaskTypeIsNeitherAgentNorShell(t *testing.T) {
	for _, tt := range []string{"local_workflow", "monitor", ""} {
		t.Run(tt, func(t *testing.T) {
			line := `{"type":"system","subtype":"task_started","task_id":"a1","tool_use_id":"toolu_1","description":"d","task_type":"` + tt + `"}`
			ev := onlyEvent(t, line, 0)
			if ev.Task == nil {
				t.Fatal("Task is nil")
			}
			if ev.Task.Kind != TaskKindUnknown {
				t.Errorf("Kind = %q, want %q", ev.Task.Kind, TaskKindUnknown)
			}
		})
	}
}

// The live status line, and the four values a row redraws from. description
// changes through the dispatch - it is what the subagent is doing now, not
// what it was asked to do - which is why a row cannot be built once at start.
func TestTaskProgressCarriesTheLiveStatusLine(t *testing.T) {
	line := `{"type":"system","subtype":"task_progress","task_id":"ab1b72d53680ae187","tool_use_id":"toolu_01Wyw","description":"Writing tally.txt","subagent_type":"general-purpose","usage":{"total_tokens":26984,"tool_uses":1,"duration_ms":4075},"last_tool_name":"Write"}`

	ev := onlyEvent(t, line, 0)
	if ev.Task == nil {
		t.Fatal("Task is nil")
	}
	if ev.Task.Phase != TaskProgress {
		t.Errorf("Phase = %q, want %q", ev.Task.Phase, TaskProgress)
	}
	if ev.Task.Label != "Writing tally.txt" {
		t.Errorf("Label = %q, want the frame's own description, not the one task_started carried", ev.Task.Label)
	}
	if ev.Task.Tool != "Write" {
		t.Errorf("Tool = %q, want the last_tool_name", ev.Task.Tool)
	}
	if ev.Task.Tokens != 26984 {
		t.Errorf("Tokens = %d, want 26984", ev.Task.Tokens)
	}
	if want := 4075 * time.Millisecond; ev.Task.Elapsed != want {
		t.Errorf("Elapsed = %v, want %v - duration_ms is milliseconds and a raw int here is 4µs on the row", ev.Task.Elapsed, want)
	}
	if ev.Task.Status != TaskRunning {
		t.Errorf("Status = %q, want %q", ev.Task.Status, TaskRunning)
	}
}

// task_updated is the one lifecycle frame with **no tool_use_id**: its keys
// are exactly task_id and patch. A decoder that required the dispatch would
// lose the ending, and a view keyed only on dispatch could never retire a row.
func TestTaskUpdatedEndsATaskWithoutNamingItsDispatch(t *testing.T) {
	line := `{"type":"system","subtype":"task_updated","task_id":"ab1b72d53680ae187","patch":{"status":"completed","end_time":1786272221002}}`

	ev := onlyEvent(t, line, 0)
	if ev.Task == nil {
		t.Fatal("Task is nil")
	}
	if ev.Task.Phase != TaskEnded {
		t.Errorf("Phase = %q, want %q", ev.Task.Phase, TaskEnded)
	}
	if ev.Task.Status != TaskDone {
		t.Errorf("Status = %q, want %q", ev.Task.Status, TaskDone)
	}
	if ev.Task.Dispatch != "" {
		t.Errorf("Dispatch = %q, want empty - this frame carries no tool_use_id and inventing one is a fabricated join", ev.Task.Dispatch)
	}
	if ev.Task.ID != "ab1b72d53680ae187" {
		t.Errorf("ID = %q, want the task_id - the only thing that can retire the row", ev.Task.ID)
	}
}

// The other ending, and the one the async path gets: it names the dispatch and
// carries a final usage. Both are recorded ×10 and either may arrive first.
func TestTaskNotificationEndsATaskAndNamesIt(t *testing.T) {
	line := `{"type":"system","subtype":"task_notification","task_id":"ab1b","tool_use_id":"toolu_01Wyw","status":"completed","output_file":"/tmp/x.output","summary":"The file has been created.","usage":{"total_tokens":29523,"tool_uses":1,"duration_ms":5000}}`

	ev := onlyEvent(t, line, 0)
	if ev.Task == nil {
		t.Fatal("Task is nil")
	}
	if ev.Task.Phase != TaskEnded {
		t.Errorf("Phase = %q, want %q", ev.Task.Phase, TaskEnded)
	}
	if ev.Task.Status != TaskDone {
		t.Errorf("Status = %q, want %q", ev.Task.Status, TaskDone)
	}
	if ev.Task.Dispatch != "toolu_01Wyw" {
		t.Errorf("Dispatch = %q, want the tool_use_id", ev.Task.Dispatch)
	}
	if ev.Task.Tokens != 29523 {
		t.Errorf("Tokens = %d, want the final count", ev.Task.Tokens)
	}
}

// Four status words across the two terminal frames, and three of them come
// from one recording each. A task that stopped did not finish: a row saying
// "done" about a killed shell is a claim nothing on the wire supports.
func TestAnEndingIsResolvedOnlyAsFarAsItWasRecorded(t *testing.T) {
	cases := map[string]TaskStatus{
		"completed": TaskDone,
		"stopped":   TaskStopped,
		"killed":    TaskStopped,
		"failed":    TaskStatusUnknown,
		"":          TaskStatusUnknown,
	}
	for status, want := range cases {
		t.Run("notification/"+status, func(t *testing.T) {
			line := `{"type":"system","subtype":"task_notification","task_id":"a1","tool_use_id":"t1","status":"` + status + `"}`
			if got := onlyEvent(t, line, 0).Task; got == nil || got.Status != want {
				t.Errorf("Status = %+v, want %q", got, want)
			}
		})
		t.Run("updated/"+status, func(t *testing.T) {
			line := `{"type":"system","subtype":"task_updated","task_id":"a1","patch":{"status":"` + status + `"}}`
			if got := onlyEvent(t, line, 0).Task; got == nil || got.Status != want {
				t.Errorf("Status = %+v, want %q", got, want)
			}
		})
	}
}

// Why background_tasks_changed is decoded to nothing at all, held to the
// corpus rather than argued in a comment.
//
// It looks like the frame a row list wants - it is the whole live set - and it
// is the one frame here that is redundant. Every membership change it reports
// is reported again on the very next line by a frame that says more: a task it
// adds is announced by task_started, and a task it drops is ended by
// task_updated. It carries no dispatch, no status and no usage, so a row built
// from it could neither be opened nor say how it finished.
//
// Both halves are asserted. If a future recording announces a task this way and
// never starts it, or drops one that never ends, the herald stops being
// redundant and this fails - which is the signal to decode it.
func TestTheHeraldFrameIsRedundant(t *testing.T) {
	for _, path := range fixtureFiles(t) {
		lines := fixtureLines(t, path)
		started, ended := map[string]bool{}, map[string]bool{}
		heralded, dropped := map[string]bool{}, map[string]bool{}
		var live map[string]bool

		for _, line := range lines {
			var f struct {
				Type    string `json:"type"`
				Subtype string `json:"subtype"`
				TaskID  string `json:"task_id"`
				Tasks   []struct {
					TaskID string `json:"task_id"`
				} `json:"tasks"`
			}
			if err := json.Unmarshal([]byte(line), &f); err != nil || f.Type != "system" {
				continue
			}
			switch f.Subtype {
			case "task_started":
				started[f.TaskID] = true
			case "task_updated", "task_notification":
				ended[f.TaskID] = true
			case "background_tasks_changed":
				now := map[string]bool{}
				for _, t := range f.Tasks {
					now[t.TaskID] = true
					if !live[t.TaskID] {
						heralded[t.TaskID] = true
					}
				}
				for id := range live {
					if !now[id] {
						dropped[id] = true
					}
				}
				live = now
			}
		}
		for id := range heralded {
			if !started[id] {
				t.Errorf("%s: task %s is announced only by background_tasks_changed and never started - the herald is no longer redundant and has to be decoded", path, id)
			}
		}
		for id := range dropped {
			if !ended[id] {
				t.Errorf("%s: task %s leaves the live set with no ending frame - its row could never be retired", path, id)
			}
		}
	}
}

// And the decode itself says so: the frame arrives, and it produces an
// ordinary system event carrying its subtype and nothing more.
func TestTheHeraldFrameDecodesToNoTask(t *testing.T) {
	line := `{"type":"system","subtype":"background_tasks_changed","tasks":[{"task_id":"adf93","task_type":"local_agent","description":"Count lines"}]}`

	ev := onlyEvent(t, line, 0)
	if ev.Task != nil {
		t.Errorf("Task = %+v, want nil - see TestTheHeraldFrameIsRedundant", ev.Task)
	}
	if ev.Kind != KindSystem || ev.Text != "background_tasks_changed" {
		t.Errorf("Kind/Text = %q/%q, want the passthrough", ev.Kind, ev.Text)
	}
}

// The contrast, and without it every test above passes for a decoder that
// stamps a Task onto every system frame. init is the subtype that carries the
// most and must still carry no task.
func TestASystemFrameThatIsNotATaskCarriesNone(t *testing.T) {
	for _, subtype := range []string{"init", "hook_started", "compact_boundary", "thinking_tokens"} {
		t.Run(subtype, func(t *testing.T) {
			line := `{"type":"system","subtype":"` + subtype + `","session_id":"s1"}`
			ev := onlyEvent(t, line, 0)
			if ev.Task != nil {
				t.Errorf("Task = %+v, want nil", ev.Task)
			}
			if ev.Text != subtype {
				t.Errorf("Text = %q, want the subtype - the passthrough is not this change's to break", ev.Text)
			}
		})
	}
}

// THE JOIN, derived from the corpus rather than restated.
//
// Subagent.Dispatch is a forwarded frame's parent_tool_use_id and Task.Dispatch
// is task_started's tool_use_id. The whole design rests on those being one
// value, and on Task.ID being the id a permission ask carries as agent_id. If
// a future recording disagrees, this fails here rather than as an empty pane.
func TestTaskStartedJoinsBothIdentifierSpaces(t *testing.T) {
	for _, path := range fixtureFiles(t) {
		dispatches := map[string]string{} // task id -> dispatch, from task_started
		forwarded := map[string]bool{}    // dispatch ids seen on forwarded frames
		agents := map[string]bool{}       // agent ids seen on receipts and asks

		for _, d := range decodeFixture(t, path) {
			ev := d.Event
			if ev.Task != nil && ev.Task.Phase == TaskStarted {
				dispatches[ev.Task.ID] = ev.Task.Dispatch
			}
			if ev.Subagent == nil {
				continue
			}
			if ev.Subagent.Result == "" && ev.Subagent.Dispatch != "" {
				forwarded[ev.Subagent.Dispatch] = true
			}
			if ev.Subagent.Agent != "" {
				agents[ev.Subagent.Agent] = true
			}
		}
		if len(dispatches) == 0 {
			continue
		}
		known := map[string]bool{}
		for id, dispatch := range dispatches {
			known[dispatch] = true
			if dispatch == "" {
				t.Errorf("%s: task %s started with no dispatch", path, id)
			}
		}
		for d := range forwarded {
			if !known[d] {
				t.Errorf("%s: forwarded frames carry dispatch %q that no task_started announced - the transcript they belong to has no row", path, d)
			}
		}
		for a := range agents {
			if _, ok := dispatches[a]; !ok {
				t.Errorf("%s: agent id %q appears on a receipt or ask but names no started task - a blocked subagent could not be pointed at", path, a)
			}
		}
	}
}

// Every task frame in the corpus decodes to a task. The count is derived, so a
// fixture added later is covered by this test without editing it.
func TestEveryRecordedTaskFrameDecodes(t *testing.T) {
	var frames, decoded int
	for _, path := range fixtureFiles(t) {
		for _, line := range fixtureLines(t, path) {
			var f struct {
				Type    string `json:"type"`
				Subtype string `json:"subtype"`
			}
			if err := json.Unmarshal([]byte(line), &f); err != nil || f.Type != "system" || !isTaskSubtype(f.Subtype) {
				continue
			}
			frames++
			evs, err := DecodeLine([]byte(line))
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			for _, ev := range evs {
				if ev.Task != nil {
					decoded++
				}
			}
		}
	}
	if frames == 0 {
		t.Fatal("no task frames in the corpus: this test is measuring nothing")
	}
	if decoded != frames {
		t.Errorf("%d of %d task frames decoded to a task", decoded, frames)
	}
}

// The four that carry one task's phase. background_tasks_changed is not one of
// them and decodes to no task at all - TestTheHeraldFrameIsRedundant is why.
func isTaskSubtype(s string) bool {
	switch s {
	case "task_started", "task_progress", "task_updated", "task_notification":
		return true
	}
	return false
}
