# Workflows Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A running Claude Code workflow shows as one sidebar row under its agent; `↵` (or `/workflows`) opens a Wake-drawn view of its phases, agents and each agent's activity, from which the operator can stop the run or save its script as a reusable `/<name>`.

**Architecture:** The airlock decodes `local_workflow` task frames and their `workflow_progress` snapshots onto `core.TaskUpdate.Workflow`; the daemon retains the latest snapshot for late attach, serves stop (`stop_task`), on-disk run records, agent transcripts, and save; the UI folds the snapshot onto `ui.Task`, draws the row through the existing sub-row machinery, and draws a pane-scoped, key-capturing view.

**Tech Stack:** Go 1.26, Bubble Tea / lipgloss, the repo's pty `vt10x` screen harness, VHS + ffmpeg for the demo.

**Spec:** `docs/superpowers/specs/2026-09-24-workflows-design.md` · wire facts: `docs/superpowers/notes/2026-09-23-workflow-findings.md`

## Global Constraints

- No non-test file over **800 lines** (`TestNoNonTestFileCrossesTheHardMax`); `core/protocol.go`, `core/vocabulary.go` are at 799, `core/wire.go` 791, `rpc/wire.go` 792, `ui/dm.go` 799, `ui/app.go` 796, `ui/slash.go` 790 — new code goes in new files; an edit to a capped file pays for its line.
- Only `internal/core/{protocol,wire,vocabulary,encode}.go` spell Claude's JSON; wire words above the airlock use Wake words (`stop_run`, never `stop_task`).
- Every new airlock literal/tag is classified in `airlock_test.go` (`claudeWireVocabulary` / `deliberatelyGeneric` / `notWireVocabulary`); outbound-only words go in `notInTheCorpus`; bump `policedWordCount` with a changelog line.
- Every new string on `core.Event` is contained in `contain.go`.
- Every new rpc frame kind gets a `managerVerbs` (client→daemon) or `notAClientVerb` (daemon→client) verdict in `cmd/wake/mcpguard_test.go`; client verbs here are all **refused** to the manager.
- View keys are read in `workflowKey` above `App.key`'s switch — **no `legendEntries` change, no new `case` in `App.key`**.
- The wire carries names, never paths; the daemon owns every path (`--debug-file`'s ruling).
- TDD: failing test first, watch it fail, then implement. `make test` runs race + non-race.
- Comments brief, *why* not *what*; no speculative code; immutable values (`App`/`Task` returned, not mutated).
- Never run `wake` without `WAKE_SOCKET`; the demo and screen tests use scratch sockets and a scratch `HOME`.
- Conventional commits, no attribution lines.

## Review Focus

1. **A workflow with many agents/phases (e.g. 30 agents, 6 phases) in a short pane** — the view must window rows around the cursor and never draw taller than the pane (alt-screen scroll). Test in Task 8.
2. **The run ends while the view is open on it (or while `x` is armed)** — the view stays on the ended run showing its final state; an armed stop disarms and `↵` does nothing. Test in Task 10.
3. **Late attach mid-run** — a client attaching after `task_started` must draw the row *and* the current phase/agent counts, not zeros. Test in Task 4 (daemon) and Task 7 (fold of a started frame carrying a snapshot).
4. **Save with a name that already exists, contains `/` or `..`, or targets a symlinked `.claude`** — refused with a notice naming why; nothing written. Test in Task 6.
5. **A workflow agent whose transcript is missing or unreadable (run still starting, or files pruned)** — the agent level shows the snapshot's prompt/result previews and says the activity is unavailable, not an empty panel or an error loop. Test in Task 9.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/core/workflow.go` (new) | Wake types: `WorkflowUpdate`, `WorkflowSnapshot`, `WorkflowPhase`, `WorkflowAgent`, `WorkflowAgentState`, `WorkflowRun` |
| `internal/core/encode.go` | wire structs for progress/run records, `workflowOf`, `workflowSnapshotOf`, `DecodeWorkflowRun`, `EncodeStopTask` |
| `internal/core/wire.go` | `wireFrame.{WorkflowName,Prompt,WorkflowProgress}`, `wireTaskPatch.Error` |
| `internal/core/protocol.go` | `taskUpdate` sets `Workflow`; `DecodeSidechainLine` shares `DecodeTranscriptLine`'s body |
| `internal/core/vocabulary.go` | `local_workflow`, `failed` |
| `internal/core/task.go` | `TaskWorkflow`, `TaskFailed`, `TaskUpdate.Workflow` |
| `internal/core/write.go` | `Session.StopTask` |
| `internal/core/contain.go` | contain the new strings |
| `internal/rpc/workflow.go` (new) | frame kinds, `WorkflowFrame`, `ValidWorkflowName`, `ValidWorkflowAgentID`, scopes |
| `internal/rpc/wire.go` | `Frame.Workflow *WorkflowFrame` |
| `internal/daemon/agent.go` | retain latest snapshot on the running-task event |
| `internal/daemon/apply.go` | `FrameStopRun` → `StopTask` |
| `internal/daemon/server.go` | dispatch the new kinds |
| `internal/daemon/workflowdisk.go` (new) | session dir, run records, agent transcript reads |
| `internal/daemon/workflowsave.go` (new) | save: fences, directory rule, write |
| `internal/ui/tasks.go` | `Task.Workflow`, `Task.Error`, workflow `Name` |
| `internal/ui/fleettasks.go` | `RunningTasks` admits workflows |
| `internal/ui/rostersubs.go` | `workflowRow`; `viewingPicked` routes a workflow |
| `internal/ui/taskline.go` | `Workflow` kind word, `failed` |
| `internal/ui/fleet.go` / `chat_blocks.go` | room ending line |
| `internal/ui/workflowview.go` (new) | `WorkflowView` state, open/close, `/workflows`, `workflowKey` |
| `internal/ui/workflowdraw.go` (new) | list / run / agent rendering |
| `internal/ui/workflowdata.go` (new) | runs in scope: live rows ∪ disk runs; frames in/out |
| `internal/ui/workflowsave.go` (new) | save dialog |
| `internal/ui/slash.go` | register `workflows` |
| `internal/ui/keys.go` / `appview.go` / `mouse.go` | hook the view (above the switch; pane body; presses) |
| `cmd/wake/fakeagent_test.go`, `cmd/wake/workflowscreen_unix_test.go` (new) | scripted workflow + pty test |
| `demo/agent/claude`, `demo/scenarios/*`, `demo/tapes/13-*.tape`… | demo recordings |

---

### Task 1: Decode workflow frames (airlock)

**Files:**
- Create: `internal/core/workflow.go`
- Modify: `internal/core/task.go`, `vocabulary.go` (`taskKinds`, `taskStatuses`), `wire.go` (`wireFrame`, `wireTaskPatch`), `encode.go`, `protocol.go` (`taskUpdate`), `contain.go` (`containedTask`), `airlock_test.go` (vocabulary lists)
- Test: `internal/core/workflow_test.go` (new), `internal/core/protocol_task_test.go` (update two tests)

**Interfaces — Produces:**
```go
// core/task.go
const TaskWorkflow TaskKind = "workflow"
const TaskFailed TaskStatus = "failed"
// TaskUpdate gains:
Workflow *WorkflowUpdate `json:"workflow,omitempty"`

// core/workflow.go
type WorkflowUpdate struct {
	Name     string            `json:"name,omitempty"`     // meta.name, on task_started
	Script   string            `json:"script,omitempty"`   // the script, on task_started
	Error    string            `json:"error,omitempty"`    // a failed run's error, on task_updated
	Progress *WorkflowSnapshot `json:"progress,omitempty"` // latest snapshot, on task_progress
}
type WorkflowSnapshot struct {
	Phases []WorkflowPhase `json:"phases,omitempty"`
	Agents []WorkflowAgent `json:"agents,omitempty"`
}
type WorkflowPhase struct {
	Index int    `json:"index"`
	Title string `json:"title"`
}
type WorkflowAgentState string
const (
	WorkflowAgentRunning WorkflowAgentState = "running"
	WorkflowAgentDone    WorkflowAgentState = "done"
	WorkflowAgentFailed  WorkflowAgentState = "failed"
	WorkflowAgentUnknown WorkflowAgentState = "unknown"
)
type WorkflowAgent struct {
	Index     int                `json:"index"`
	Phase     int                `json:"phase"`
	Label     string             `json:"label"`
	AgentID   string             `json:"agent"`
	Model     string             `json:"model,omitempty"`
	State     WorkflowAgentState `json:"state"`
	Attempt   int                `json:"attempt,omitempty"`
	Tokens    int                `json:"tokens,omitempty"`
	ToolCalls int                `json:"tool_calls,omitempty"`
	Duration  time.Duration      `json:"duration,omitempty"`
	Prompt    string             `json:"prompt,omitempty"` // promptPreview
	Result    string             `json:"result,omitempty"` // resultPreview
}
// Done counts agents in WorkflowAgentDone; PhaseAgents returns the agents of one phase index.
func (s WorkflowSnapshot) Done() int
func (s WorkflowSnapshot) PhaseAgents(phase int) []WorkflowAgent
```

- [ ] **Step 1: Write the failing tests** in `internal/core/workflow_test.go`, driven by the recorded fixtures (read with the existing `fixtureLines`/`decodeFixture` helper used by `protocol_task_test.go` — reuse it, don't write a second):

```go
func TestAWorkflowStartsAsAWorkflowAndNamesItself(t *testing.T) {
	start := firstTask(t, "workflow-run.jsonl", TaskStarted)
	if start.Kind != TaskWorkflow || start.Workflow == nil || start.Workflow.Name != "count-lines" {
		t.Fatalf("start = %+v, want a workflow named count-lines", start)
	}
	if !strings.HasPrefix(start.Workflow.Script, "export const meta") {
		t.Fatalf("script = %.40q, want the script task_started carries", start.Workflow.Script)
	}
}

func TestEveryWorkflowSnapshotIsWholeAndResolved(t *testing.T) {
	last := lastSnapshot(t, "workflow-failed.jsonl")
	if len(last.Phases) != 3 || len(last.Agents) != 7 {
		t.Fatalf("phases/agents = %d/%d, want 3/7", len(last.Phases), len(last.Agents))
	}
	for _, a := range last.Agents {
		if a.State != WorkflowAgentDone || a.AgentID == "" || a.Phase == 0 {
			t.Fatalf("agent %+v: want done, with an id and a phase", a)
		}
	}
	if got := last.Done(); got != 7 {
		t.Fatalf("Done() = %d, want 7", got)
	}
}

func TestAFailedWorkflowCarriesItsError(t *testing.T) {
	end := endings(t, "workflow-failed.jsonl")
	if end[0].Status != TaskFailed || end[0].Workflow == nil || !strings.Contains(end[0].Workflow.Error, "deliberate probe failure") {
		t.Fatalf("task_updated = %+v, want failed with the error", end[0])
	}
	if end[1].Status != TaskFailed {
		t.Fatalf("task_notification status = %q, want failed", end[1].Status)
	}
}

func TestAStoppedWorkflowEndsHalted(t *testing.T) {
	for _, u := range endings(t, "workflow-stop.jsonl") {
		if u.Status != TaskStopped {
			t.Fatalf("ending %+v, want TaskStopped (killed/stopped)", u)
		}
	}
}

func TestANonWorkflowTaskCarriesNoWorkflow(t *testing.T) {
	for _, u := range allTasks(t, "subagent-background.jsonl") {
		if u.Workflow != nil {
			t.Fatalf("%+v: a subagent frame carries no workflow", u)
		}
	}
}
```
`firstTask`, `lastSnapshot`, `endings`, `allTasks` are small helpers in the test file that decode a fixture with `DecodeLine` and filter `ev.Task`.

Update `protocol_task_test.go`: `TestAnUnrecordedTaskTypeIsNeitherAgentNorShell` drops `"local_workflow"` from its unrecorded list (keep another unrecorded type such as `"local_monitor"`), and `TestAnEndingIsResolvedOnlyAsFarAsItWasRecorded` expects `"failed"` → `TaskFailed` (keep an unrecorded word such as `"paused"` → `TaskStatusUnknown`).

- [ ] **Step 2: Run** `go test ./internal/core -run 'Workflow|Ending|Unrecorded'` — expect compile failure (`TaskWorkflow` undefined), then after stubbing types, assertion failures.

- [ ] **Step 3: Implement.**
  - `task.go`: the two constants and the field.
  - `workflow.go`: the types above plus
    ```go
    func (s WorkflowSnapshot) Done() int {
    	n := 0
    	for _, a := range s.Agents {
    		if a.State == WorkflowAgentDone {
    			n++
    		}
    	}
    	return n
    }
    func (s WorkflowSnapshot) PhaseAgents(phase int) []WorkflowAgent {
    	var out []WorkflowAgent
    	for _, a := range s.Agents {
    		if a.Phase == phase {
    			out = append(out, a)
    		}
    	}
    	return out
    }
    ```
  - `vocabulary.go`: `"local_workflow": TaskWorkflow,` and `"failed": TaskFailed,` — pay for the two lines by tightening the adjacent comments (stay ≤ 800).
  - `wire.go` `wireFrame` task block: `WorkflowName string \`json:"workflow_name"\``, `Prompt string \`json:"prompt"\``, `WorkflowProgress []wireWorkflowItem \`json:"workflow_progress"\``; `wireTaskPatch` gains `Error string \`json:"error"\``. Update the "Prompt … deliberately not here" comment to say Prompt is read for a workflow's script only.
  - `encode.go` (reads, but encode.go holds the room — same note as `goalOp`):
    ```go
    // wireWorkflowItem is one workflow_progress entry: a phase or an agent, told apart by type.
    type wireWorkflowItem struct {
    	Type          string `json:"type"`
    	Index         int    `json:"index"`
    	Title         string `json:"title"`
    	Label         string `json:"label"`
    	PhaseIndex    int    `json:"phaseIndex"`
    	AgentID       string `json:"agentId"`
    	Model         string `json:"model"`
    	State         string `json:"state"`
    	Attempt       int    `json:"attempt"`
    	Tokens        int    `json:"tokens"`
    	ToolCalls     int    `json:"toolCalls"`
    	DurationMs    int    `json:"durationMs"`
    	PromptPreview string `json:"promptPreview"`
    	ResultPreview string `json:"resultPreview"`
    }
    const (
    	workflowPhaseItem = "workflow_phase"
    	workflowAgentItem = "workflow_agent"
    )
    // Recorded agent states are start and done; failed is Claude Code's documented word for a failed agent.
    var workflowAgentStates = map[string]WorkflowAgentState{
    	"start": WorkflowAgentRunning, "done": WorkflowAgentDone, "failed": WorkflowAgentFailed,
    }
    func workflowSnapshotOf(items []wireWorkflowItem) *WorkflowSnapshot {
    	if items == nil {
    		return nil
    	}
    	s := &WorkflowSnapshot{}
    	for _, it := range items {
    		switch it.Type {
    		case workflowPhaseItem:
    			s.Phases = append(s.Phases, WorkflowPhase{Index: it.Index, Title: it.Title})
    		case workflowAgentItem:
    			state, ok := workflowAgentStates[it.State]
    			if !ok {
    				state = WorkflowAgentUnknown
    			}
    			s.Agents = append(s.Agents, WorkflowAgent{Index: it.Index, Phase: it.PhaseIndex, Label: it.Label,
    				AgentID: it.AgentID, Model: it.Model, State: state, Attempt: it.Attempt, Tokens: it.Tokens,
    				ToolCalls: it.ToolCalls, Duration: time.Duration(it.DurationMs) * time.Millisecond,
    				Prompt: it.PromptPreview, Result: it.ResultPreview})
    		}
    	}
    	return s
    }
    // workflowOf is a task frame's workflow half, and nil on every frame that says nothing about one.
    func workflowOf(f wireFrame, kind TaskKind) *WorkflowUpdate {
    	w := WorkflowUpdate{Progress: workflowSnapshotOf(f.WorkflowProgress)}
    	if kind == TaskWorkflow {
    		w.Name, w.Script = f.WorkflowName, f.Prompt
    	}
    	if f.Patch != nil {
    		w.Error = f.Patch.Error
    	}
    	if w == (WorkflowUpdate{}) {
    		return nil
    	}
    	return &w
    }
    ```
    (`WorkflowUpdate` holds a pointer, so `==` compares it — valid Go.)
  - `protocol.go` `taskUpdate`: compute `kind := taskKind(f.TaskType)` once and add `Workflow: workflowOf(f, kind)`; keep the file ≤ 800 by reusing `kind` in the literal and trimming one comment line.
  - `contain.go` `containedTask`: when `c.Workflow != nil`, copy it and contain `Name`, `Script`, `Error`, and in `Progress` each phase `Title` and each agent `Label`, `AgentID`, `Model`, `State`, `Prompt`, `Result` (new slices — never mutate the decoded ones).
  - `airlock_test.go`: classify `workflow_name`, `prompt`(already?), `workflow_progress`, `workflow_phase`, `workflow_agent`, `phaseIndex`, `agentId`, `toolCalls`, `durationMs`, `promptPreview`, `resultPreview`, `local_workflow`, `start`, `error` — into `claudeWireVocabulary` when it is Claude's word, `deliberatelyGeneric` when it is a plain English key (`index`, `title`, `label`, `model`, `state`, `attempt`, `tokens`); bump `policedWordCount` with a changelog line. Run the guard, read its message, and put each word where it says.

- [ ] **Step 4: Run** `go test ./internal/core/...` — all green (includes `TestDecodeRecordedFixtures`, the corpus and containment guards).

- [ ] **Step 5: Commit** `feat(core): decode workflow tasks and their progress snapshots`

---

### Task 2: Stop, sidechain transcripts, run records (airlock)

**Files:** Modify `internal/core/encode.go`, `write.go`, `protocol.go`, `airlock_test.go`. Test: `internal/core/workflow_test.go`, `encode_test.go`.

**Interfaces — Produces:**
```go
func EncodeStopTask(requestID, taskID string) ([]byte, error)
func (s *Session) StopTask(taskID string) (string, error)
func DecodeSidechainLine(line []byte) ([]Event, error)
type WorkflowRun struct {             // core/workflow.go
	TaskID   string            `json:"task"`
	Name     string            `json:"name"`
	Summary  string            `json:"summary,omitempty"`
	Status   TaskStatus        `json:"status"`
	Error    string            `json:"error,omitempty"`
	Started  time.Time         `json:"started"`
	Duration time.Duration     `json:"duration,omitempty"`
	Tokens   int               `json:"tokens,omitempty"`
	Script   string            `json:"script,omitempty"`
	Progress *WorkflowSnapshot `json:"progress,omitempty"`
}
func DecodeWorkflowRun(raw []byte) (WorkflowRun, error)
```

- [ ] **Step 1: Failing tests.**
```go
func TestStopTaskEncodesTheRecordedRequest(t *testing.T) {
	got, err := EncodeStopTask("probe-1", "wbu5972hq")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"control_request","request_id":"probe-1","request":{"subtype":"stop_task","task_id":"wbu5972hq"}}` + "\n"
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if _, err := EncodeStopTask("", "x"); !errors.Is(err, ErrNotWritten) {
		t.Fatalf("empty request id: err = %v, want ErrNotWritten", err)
	}
	if _, err := EncodeStopTask("r", ""); !errors.Is(err, ErrNotWritten) {
		t.Fatalf("empty task id: err = %v, want ErrNotWritten", err)
	}
}
```
(Check `marshalLine`'s trailing newline and key order against `testdata/input/workflow-stop.stdin.jsonl` line 2; match whichever the other `Encode*` golden tests use.)
```go
func TestASidechainLineDecodesWhereATranscriptLineDoesNot(t *testing.T) {
	var side, plain int
	for _, line := range fileLines(t, "../../testdata/transcript/workflow-agent.jsonl") {
		evs, err := DecodeSidechainLine(line)
		if err != nil {
			t.Fatal(err)
		}
		side += len(evs)
		p, _ := DecodeTranscriptLine(line)
		plain += len(p)
	}
	if side == 0 || plain != 0 {
		t.Fatalf("sidechain %d events, transcript %d; want >0 and 0", side, plain)
	}
}
func TestARunRecordDecodes(t *testing.T) {
	run, err := DecodeWorkflowRun([]byte(runRecordFixture)) // a trimmed, scrubbed wf_*.json literal in the test
	if err != nil || run.TaskID != "wsmc7r0xw" || run.Status != TaskFailed || run.Error == "" ||
		run.Progress == nil || len(run.Progress.Agents) != 2 || run.Started.IsZero() {
		t.Fatalf("run = %+v err = %v", run, err)
	}
}
```
`runRecordFixture` is a hand-trimmed copy of the failed run's `wf_*.json` (taskId, workflowName, summary, status, error first line, startTime, durationMs, totalTokens, script one line, workflowProgress with one phase and two agents) — keys copied verbatim from the recording; no paths.

- [ ] **Step 2: Run** — fails to compile.

- [ ] **Step 3: Implement.**
  - `encode.go`: `outStopTaskRequest{Subtype string \`json:"subtype"\`; TaskID string \`json:"task_id"\`}`, `EncodeStopTask` refusing empty ids with `fmt.Errorf("%w: encode stop task: empty …", ErrNotWritten)`, marshalled like `EncodeSetMode`.
  - `write.go` `StopTask`: `SetMode`'s shape; records nothing.
  - `protocol.go`: rename the body of `DecodeTranscriptLine` to `decodeTranscript(line []byte, keepSidechain bool)`; `DecodeTranscriptLine` calls it with `false`, and add `DecodeSidechainLine` (in `encode.go`, for room) calling it with `true`. Net line change in protocol.go ≤ 1.
  - `encode.go` run record:
    ```go
    type wireWorkflowRun struct {
    	TaskID           string             `json:"taskId"`
    	WorkflowName     string             `json:"workflowName"`
    	Summary          string             `json:"summary"`
    	Status           string             `json:"status"`
    	Error            string             `json:"error"`
    	StartTime        int64              `json:"startTime"`
    	DurationMs       int                `json:"durationMs"`
    	TotalTokens      int                `json:"totalTokens"`
    	Script           string             `json:"script"`
    	WorkflowProgress []wireWorkflowItem `json:"workflowProgress"`
    }
    func DecodeWorkflowRun(raw []byte) (WorkflowRun, error) {
    	var w wireWorkflowRun
    	if err := json.Unmarshal(raw, &w); err != nil {
    		return WorkflowRun{}, fmt.Errorf("decode workflow run: %w", err)
    	}
    	if w.TaskID == "" {
    		return WorkflowRun{}, errors.New("decode workflow run: no task id")
    	}
    	status, ok := taskStatuses[w.Status]
    	if !ok {
    		status = TaskStatusUnknown
    	}
    	return containedRun(WorkflowRun{TaskID: w.TaskID, Name: w.WorkflowName, Summary: w.Summary, Status: status,
    		Error: w.Error, Started: time.UnixMilli(w.StartTime), Duration: time.Duration(w.DurationMs) * time.Millisecond,
    		Tokens: w.TotalTokens, Script: w.Script, Progress: workflowSnapshotOf(w.WorkflowProgress)}), nil
    }
    ```
    `containedRun` lives in `contain.go` and reuses the snapshot containment from Task 1.
  - `airlock_test.go`: classify `stop_task` (Claude's; `notInTheCorpus` "outbound only", like `interrupt`), `taskId`, `workflowName`, `startTime`, `totalTokens`, `workflowProgress`, `script`, `isSidechain` if newly named.

- [ ] **Step 4: Run** `go test ./internal/core/...` — green.
- [ ] **Step 5: Commit** `feat(core): encode stop_task, decode sidechain lines and workflow run records`

---

### Task 3: The rpc surface

**Files:** Create `internal/rpc/workflow.go`; modify `internal/rpc/wire.go` (one field); modify `cmd/wake/mcpguard_test.go`. Test: `internal/rpc/workflow_test.go`.

**Interfaces — Produces:**
```go
const (
	FrameStopRun            = "stop_run"             // client → daemon: stop a running workflow (Workflow.Task)
	FrameWorkflows          = "workflows"            // client → daemon: this session's runs on disk
	FrameWorkflowsReply     = "workflows_reply"      // daemon → client: Workflow.Runs
	FrameWorkflowAgent      = "workflow_agent"       // client → daemon: one agent's transcript (Workflow.Agent)
	FrameWorkflowAgentReply = "workflow_agent_reply" // daemon → client: Events, Workflow.Agent echoed
	FrameSaveWorkflow       = "save_workflow"        // client → daemon: Workflow{Task, Name, Scope}
	FrameWorkflowSaved      = "workflow_saved"       // daemon → client: Workflow.Path
)
const (
	ScopeProject = "project"
	ScopeUser    = "user"
)
type WorkflowFrame struct {
	Task  string             `json:"task,omitempty"`
	Agent string             `json:"agent,omitempty"`
	Name  string             `json:"name,omitempty"`
	Scope string             `json:"scope,omitempty"`
	Path  string             `json:"path,omitempty"`
	Runs  []core.WorkflowRun `json:"runs,omitempty"`
}
func ValidWorkflowName(name string) error    // one segment: ^[a-z0-9][a-z0-9_-]{0,63}$
func ValidWorkflowAgentID(id string) error   // ^[a-z0-9]{1,64}$
// rpc.Frame gains (wire.go):
Workflow *WorkflowFrame `json:"workflow,omitempty"`
```

- [ ] **Step 1: Failing tests** — table tests for both fences: accept `count-lines`, `deep_research2`, `a216187ba41a1087e`; refuse ``, `../x`, `a/b`, `.hidden`, `Upper`, `x y`, a 65-char name. And a round-trip test: a `Frame{Kind: FrameWorkflowsReply, Workflow: &WorkflowFrame{Runs: …}}` survives `WriteFrameTo`/read.
- [ ] **Step 2: Run** — fails.
- [ ] **Step 3: Implement** `workflow.go` (header comment: declared here because wire.go is at the hard max, like team.go) and the one `Frame` field (pay for the line in a wire.go comment).
- [ ] **Step 4:** `go test ./internal/rpc ./cmd/wake -run 'Workflow|EveryVerb'` — the mcpguard test now fails naming the new kinds. Add verdicts: `FrameStopRun`, `FrameSaveWorkflow` refused ("stops or writes something the manager's tools report nowhere; the operator's keys only"), `FrameWorkflows`, `FrameWorkflowAgent` refused (`FrameHistory`'s ruling: no manager tool reads a transcript); the three replies in `notAClientVerb`. Re-run — green.
- [ ] **Step 5: Commit** `feat(rpc): workflow frames and their fences`

---

### Task 4: Daemon — snapshot replay, stop, fork ruling

**Files:** Modify `internal/daemon/agent.go` (observe), `apply.go`, `server.go` (dispatch line). Test: `internal/daemon/taskreplay_test.go`, `internal/daemon/workflowstop_test.go` (new), `forksubagent_test.go`.

**Interfaces — Consumes:** `core.TaskUpdate.Workflow`, `Session.StopTask`, `rpc.FrameStopRun`. **Produces:** `(a *agent) runningWorkflow(taskID string) bool` (held under `a.mu`).

- [ ] **Step 1: Failing tests.**
  - `TestAReplayedWorkflowCarriesItsLatestSnapshot`: feed an agent the recorded `workflow-run.jsonl` task frames up to the third `task_progress` (use the existing taskreplay test's way of observing events), then `runningTaskFrames()`; want one frame whose `Event.Task.Phase == TaskStarted`, `Kind == TaskWorkflow`, `Workflow.Name == "count-lines"`, and `Workflow.Progress` equal to the last snapshot observed.
  - `TestStopRunReachesOnlyARunningWorkflow`: with the fake session used by `apply_test.go`, `FrameStopRun{Workflow: {Task: "w1"}}` for a running workflow writes one `stop_task` line naming `w1`; for an unknown id, a subagent task, and an ended workflow it writes nothing and the client gets an error frame (`"no running workflow w1"`).
  - `TestARunningWorkflowDoesNotBlockAFork` beside the shell case in `forksubagent_test.go`.
- [ ] **Step 2: Run** — fail.
- [ ] **Step 3: Implement.**
  - `agent.go` observe, in the `ev.Task != nil` switch:
    ```go
    case core.TaskProgress:
    	if started, ok := a.runningTasks[ev.Task.ID]; ok && ev.Task.Workflow != nil && ev.Task.Workflow.Progress != nil {
    		a.runningTasks[ev.Task.ID] = withProgress(started, ev.Task.Workflow.Progress)
    	}
    ```
    `withProgress` (in `taskreplay.go`) returns a copy of the event with a copied `Task` and `Workflow` whose `Progress` is replaced — never mutating the retained or the fanned-out event.
  - `apply.go`: `case rpc.FrameStopRun:` — `if p.frame.Workflow == nil || !a.runningWorkflow(p.frame.Workflow.Task) { a.refuse(p, fmt.Errorf("no running workflow %s", …)); return }` then `_, err = a.sess.StopTask(p.frame.Workflow.Task)`. `runningWorkflow` reads `a.runningTasks[id].Task.Kind == core.TaskWorkflow` under the lock.
  - `server.go`: add `rpc.FrameStopRun` to the `s.submit` case line.
- [ ] **Step 4: Run** `go test ./internal/daemon/...` — green.
- [ ] **Step 5: Commit** `feat(daemon): replay a workflow's latest snapshot and stop a running one`

---

### Task 5: Daemon — runs and agent transcripts off disk

**Files:** Create `internal/daemon/workflowdisk.go`; modify `server.go` (dispatch two cases, on `s.start`). Test: `internal/daemon/workflowdisk_test.go`.

**Interfaces — Produces:**
```go
func sessionDir(transcript string) string // strings.TrimSuffix(path, ".jsonl")
func WorkflowRuns(id string) ([]core.WorkflowRun, error)            // newest first, ≤ maxWorkflowRuns (50), each file ≤ maxRunBytes (8MB)
func WorkflowAgentHistory(id, agentID string) ([]core.Event, error) // ≤ historyEvents / historyBytes
func (s *server) sendWorkflows(c *client, id string)
func (s *server) sendWorkflowAgent(c *client, id, agentID string)
```

- [ ] **Step 1: Failing tests** — build a scratch projects dir (`t.Setenv("WAKE_PROJECTS", dir)`), write `<slug>/<uuid>.jsonl`, `<uuid>/workflows/wf_a.json` (the Task 2 trimmed record) and `wf_b.json` (status `completed`, later `startTime`), `<uuid>/subagents/workflows/wf_a/agent-abc123.jsonl` (copy of `testdata/transcript/workflow-agent.jsonl`). Assert: `WorkflowRuns` returns b then a; a corrupt `wf_c.json` is skipped (not fatal); a symlinked record is skipped; `WorkflowAgentHistory(id, "abc123")` returns the sidechain events; `"../x"` is refused by `ValidWorkflowAgentID`; an unknown agent returns an empty slice and a nil error. Also a dispatch test: `FrameWorkflows` answers `FrameWorkflowsReply` with `Workflow.Runs`, and `FrameWorkflowAgent` answers `FrameWorkflowAgentReply` with `Workflow.Agent` echoed and `Events`.
- [ ] **Step 2: Run** — fail.
- [ ] **Step 3: Implement.** `transcriptPath(s.transcriptID(id))` → `sessionDir`; `filepath.Glob(filepath.Join(dir, "workflows", "wf_*.json"))`, each through `regularTranscript` (Lstat, regular only) and a size check, `os.ReadFile`, `core.DecodeWorkflowRun`, skip on error (log with `log.Printf`, never fatal), sort by `Started` descending, cap. Agent: `ValidWorkflowAgentID`, `filepath.Glob(filepath.Join(dir, "subagents", "workflows", "*", "agent-"+id+".jsonl"))`, first regular match, read lines with the same bounded scanner `History` uses, `core.DecodeSidechainLine` per line, restamp `SessionID`. Dispatch both on `s.start` (the `FrameHistory` comment's reason); errors → `errorFrame(id, "could not read workflows: …")`.
- [ ] **Step 4: Run** — green.
- [ ] **Step 5: Commit** `feat(daemon): read workflow runs and agent transcripts off claude's disk`

---

### Task 6: Daemon — save

**Files:** Create `internal/daemon/workflowsave.go`; modify `server.go`. Test: `internal/daemon/workflowsave_test.go`.

**Interfaces — Produces:**
```go
func workflowScript(a *agent, id, taskID string) (string, error) // running: retained start event; else run record
func projectWorkflowDir(cwd string) string                        // Claude Code's documented rule
func userWorkflowDir() string                                     // $CLAUDE_CONFIG_DIR/workflows or ~/.claude/workflows
func saveWorkflow(dir, name, script string, project bool) (string, error)
func (s *server) saveWorkflowFrame(c *client, f rpc.Frame)
```

- [ ] **Step 1: Failing tests** (all under `t.TempDir()`, `HOME` and `CLAUDE_CONFIG_DIR` set to scratch):
  - project dir rule: cwd `repo/a/b` with `repo/.git` → `repo/.claude/workflows`; with an existing `repo/a/.claude/workflows` → that one; no git root → `cwd/.claude/workflows`.
  - `saveWorkflow` writes `name.js` with the script bytes (0644), creating dirs (0755); refuses an existing file (`already exists`), a symlinked `.claude` or `.claude/workflows` (project), a symlinked target (both scopes), an invalid name (via `rpc.ValidWorkflowName`).
  - `workflowScript`: from a running task's retained start event; from a run record once ended; `no script for task x` when neither.
  - frame test: `FrameSaveWorkflow` → `FrameWorkflowSaved{Workflow.Path}`; a refusal → `FrameError` whose text names the reason.
- [ ] **Step 2: Run** — fail.
- [ ] **Step 3: Implement.** Repository root: walk up from the agent's cwd (`a.cwd`, falling back to `a.dir`) looking for `.git` (file or dir) — no `git` subprocess. Walk from cwd up to root collecting the nearest existing `.claude/workflows` dir. `os.Lstat` checks per the spec; write with `os.OpenFile(path, O_WRONLY|O_CREATE|O_EXCL, 0o644)` so the no-overwrite rule is atomic. Dispatch on `s.start` (disk write).
- [ ] **Step 4: Run** — green.
- [ ] **Step 5: Commit** `feat(daemon): save a workflow's script as a reusable command`

---

### Task 7: UI — fold, sidebar row, ending lines

**Files:** Modify `internal/ui/tasks.go`, `fleettasks.go`, `rostersubs.go`, `taskline.go`, `fleet.go` (fold admit), `chat_blocks.go` (room line). Test: `tasks_test.go`, `rostersubs_test.go`, `taskline_test.go`, `workflowroom_test.go` (new).

**Interfaces — Produces:**
```go
// ui.Task gains
Workflow core.WorkflowSnapshot // latest snapshot; zero until one arrives
Error    string                // a failed workflow's error
func workflowRow(t Task, width int) string // "  ⎿ ◈ name d/n", the figure dropped whole when it does not fit
const workflowGlyph = "◈"
```

- [ ] **Step 1: Failing tests.**
  - fold: observing `workflow-run.jsonl`'s task frames yields one row, `Kind == TaskWorkflow`, `Name == "count-lines"`, `Workflow.Done() == 3` at the end, status `TaskDone`; a started frame that already carries `Workflow.Progress` (the replay shape) sets the snapshot immediately.
  - `RunningTasks` includes a running workflow and excludes it once ended; a running shell stays excluded.
  - `workflowRow` at width 23: `"  ⎿ ◈ count-lines 2/3"`; at width 16 the figure is dropped whole (`"  ⎿ ◈ count-lin…"` style clipping via `clip`), never `2/`.
  - roster `View` with an agent running a workflow draws the row under it; `rowsFor` still equals the drawn line count (`TestRowsDrawsExactlyTheLinesRowsForPromises` covers it once the row flows through `rows`).
  - `taskLine` for a workflow ending: `● Workflow "count-lines" finished · 9s`; failed: `● Workflow "wide-then-fail" failed · 10s` followed by the error's first line, in the error colour.
  - room: a workflow ending event folds into a room line headed by the agent's name; a subagent ending does not.
- [ ] **Step 2: Run** — fail.
- [ ] **Step 3: Implement.**
  - `Task.updated`: `if u.Workflow != nil { if u.Workflow.Name != "" { t.Name = u.Workflow.Name }; if u.Workflow.Progress != nil { t.Workflow = *u.Workflow.Progress }; if u.Workflow.Error != "" { t.Error = u.Workflow.Error } }` — placed after the `Label`/`Name` block so the workflow's name wins over its description.
  - `named`: fill the ending's `Workflow.Error` from the row when the ending (task_notification) lacks it (copy, never mutate).
  - `RunningTasks`: `row.Status == core.TaskRunning && (row.Openable() || row.Kind == core.TaskWorkflow)`.
  - `rows`/`subagentRow` call site: `if t.Kind == core.TaskWorkflow { return workflowRow(t, width) }`.
  - `taskLineKind`: `core.TaskWorkflow` → `"Workflow"`; `taskLineWord`/`Style`: `core.TaskFailed` → `"failed"`, `errStyle` (the theme's existing error style); after the line, a failed workflow appends `\n  ` + first line of `u.Workflow.Error`, clipped to width.
  - room: in `fleet.go` `fold`, admit `KindSystem` events whose `Task` is a workflow ending with a dispatch into the room slice; `chat_blocks.go` draws it through `taskLine` prefixed by the agent's name tag (the existing speaker style).
- [ ] **Step 4: Run** `go test ./internal/ui/...` — green.
- [ ] **Step 5: Commit** `feat(ui): a running workflow is a sidebar row and its ending a line`

---

### Task 8: UI — the view (list and run levels), opening it

**Files:** Create `internal/ui/workflowview.go`, `workflowdraw.go`, `workflowdata.go`; modify `slash.go` (one map entry + `wakeCommandCount`), `keys.go` (`workflowKey` call above the switch; `↵` on a workflow sub-row), `rostersubs.go` (`viewingPicked`), `appview.go` (pane body), `mouse.go` (press routing), `app.go` (one field + one `apply` case for the replies — pay for lines). Test: `workflowview_test.go`, `workflowdraw_test.go`.

**Interfaces — Produces:**
```go
type workflowLevel int
const (
	levelList workflowLevel = iota
	levelRun
	levelAgent
)
type WorkflowView struct {
	Pane     string // pane id the view is drawn in ("" = room)
	Session  string // "" = every agent (room list)
	Task     string // the run open at levelRun/levelAgent
	Level    workflowLevel
	Cursor   int  // list row, or phase row
	Column   int  // 0 phases, 1 agents
	Agent    int  // agent row within the phase (after filter)
	Filter   workflowFilter // all/running/done/failed
	Armed    bool // x pressed, ↵ confirms
	Expanded bool
	Scroll   int
	Save     *saveDialog
}
func (v WorkflowView) Open() bool
type workflowRunView struct { // one run as the view draws it
	Session, Agent, Task, Name, Summary string
	Status  core.TaskStatus
	Error   string
	Started time.Time
	Elapsed time.Duration
	Snap    core.WorkflowSnapshot
	Live    bool
}
func (a App) workflowRuns(session string) []workflowRunView // live Tasks rows ∪ a.workflowDisk[session], by task id, live wins; newest first
func (a App) openWorkflows(arg string) (App, tea.Cmd)       // /workflows
func (a App) openWorkflow(session, task string) (App, tea.Cmd)
func (a App) workflowKey(m tea.KeyMsg) (App, tea.Cmd, bool)
func (v WorkflowView) render(runs []workflowRunView, w, h int) string
```
App gains `workflow WorkflowView` and `workflowDisk map[string][]core.WorkflowRun` (replace-on-reply, copied).

- [ ] **Step 1: Failing tests.**
  - `/workflows` in a DM with one run opens `levelRun` on it; with two, `levelList`; with none, the view draws `No workflows in this session.` and `esc` closes it. In the room it lists every agent's runs grouped under agent names (`No workflows in this fleet.` when empty). Opening writes one `FrameWorkflows` per session in scope (assert on the frames the test `App` records, the way other ui tests capture `write`).
  - `↵`, `⌃D` and a click on a workflow sidebar row open `levelRun` for that task in the agent's pane.
  - run level render at 90×20 for `count-lines` mid-run: header contains `count-lines`, `2/3 agents`, `running`; phases column `✔ Count 2/2` and `2 Sum 0/1`; agents column rows `✔ count a.txt` with model and tokens; key line `↑↓ select · ↵ open · f filter · x stop · s save · esc back`. After the run ends the `x stop` item is absent.
  - navigation: `↓` moves phase; `→`/`↵` moves to the agents column; `f` cycles filters and hides non-matching agents; `esc` from agents → phases → (list if >1 run) → closed; `⌃C` closes and is not swallowed.
  - **Review Focus 1:** 30 agents in one phase at height 12 — the rendered block has exactly 12 lines and contains the cursored agent.
  - keys typed while the view is open never reach the composer (draft unchanged).
- [ ] **Step 2: Run** — fail.
- [ ] **Step 3: Implement.** Model on the resume picker for capture (`workflowKey` called in `App.key` right after `boardKey`, returning handled for every key while `a.workflow.Open() && a.focus == a.workflow.Pane`; `⌃C` closes then returns not-handled) and on the rewind picker for pane scoping. Draw: in `appview.go`'s DM/room pane builders, when the view is open for that pane, render `v.render(...)` sized to the pane body in place of the transcript + composer; the pane frame/title stays. `mouse.go`: a press inside that pane while the view is open selects the row under the pointer (or is ignored) and never starts a transcript selection. Rows window around the cursor (reuse the `window` idiom of `resumeWindow`). Glyphs: `✔` done, `⏺` running, `✗` failed, `·` unknown; phase `✔` when all its agents are done else its index. Elapsed for a live run = now − first agent's start is not on the wire — use `Task.Elapsed` (the progress frame's `duration_ms`) for live, `Duration` for disk.
- [ ] **Step 4: Run** — green.
- [ ] **Step 5: Commit** `feat(ui): the /workflows view — list and run levels`

---

### Task 9: UI — the agent level

**Files:** Modify `workflowview.go`, `workflowdraw.go`, `workflowdata.go`, `app.go` (reply case). Test: `workflowagent_test.go`.

**Interfaces — Produces:**
```go
// App gains workflowAgent map[string][]core.Event keyed session+"/"+agentID (replaced per reply)
func (a App) askWorkflowAgent(session, agentID string) tea.Cmd // one FrameWorkflowAgent
func renderWorkflowAgent(ag core.WorkflowAgent, events []core.Event, expanded bool, w, h, scroll int) string
```

- [ ] **Step 1: Failing tests.**
  - entering an agent writes one `FrameWorkflowAgent`; a new snapshot in which that agent's `ToolCalls`, `State` or `Tokens` changed writes exactly one more; an unchanged snapshot writes none; leaving the agent level stops asking.
  - render with the `workflow-agent.jsonl` events: `✔ Completed · haiku`, `2 tool calls`, `Prompt` block with the prompt, `Activity` with one headline per tool call (`⏺ Bash(wc -l …)` style from `toolHeadline`), `Outcome` with the result; `↵` toggles expanded, which adds each call's input and the first lines of its result.
  - **Review Focus 5:** no events (reply empty or not yet arrived) → Prompt and Outcome from the snapshot previews and `Activity unavailable` — no error notice, no repeated asks.
  - `j`/`k` scroll within bounds; the block never exceeds the given height.
- [ ] **Step 2: Run** — fail.
- [ ] **Step 3: Implement.** Activity reuses the conversation's own tool rendering (`toolHeadline` collapsed; the existing tool-result block for expanded) — no second renderer. The re-ask compares the agent's snapshot entry before/after in the `Tasks` fold path (`App.observe` → when the view is at `levelAgent` on that task).
- [ ] **Step 4: Run** — green.
- [ ] **Step 5: Commit** `feat(ui): drill into a workflow agent's prompt, activity and outcome`

---

### Task 10: UI — stop and save

**Files:** Modify `workflowview.go`; create `workflowsave.go`; modify `app.go` (reply case for `FrameWorkflowSaved`). Test: `workflowstop_test.go`, `workflowsave_test.go`.

**Interfaces — Produces:**
```go
type saveDialog struct {
	Name  string
	Scope string // rpc.ScopeProject / rpc.ScopeUser
}
func (a App) stopWorkflow() (App, tea.Cmd) // writes FrameStopRun
func (a App) saveWorkflow() (App, tea.Cmd) // writes FrameSaveWorkflow
```

- [ ] **Step 1: Failing tests.**
  - `x` on a running run arms (key line becomes `↵ stop count-lines · any key cancels`); `↵` writes one `FrameStopRun{Workflow{Task}}` and disarms; any other key disarms without writing; `x` on an ended run does nothing.
  - **Review Focus 2:** armed, then the run's ending arrives → disarmed, `↵` writes nothing.
  - `s` opens the dialog with the run's name; typing edits it, `⌫` deletes, `⇥` toggles scope and the shown path hint (`.claude/workflows/<name>.js` / `~/.claude/workflows/<name>.js`); `↵` writes `FrameSaveWorkflow{Task, Name, Scope}`; `esc` closes the dialog only; an invalid name (`rpc.ValidWorkflowName`) is refused in the dialog without writing.
  - `FrameWorkflowSaved` → notice `Saved /count-lines → <path> · runs as /count-lines in new sessions`; a `FrameError` for the save → notice with the reason.
- [ ] **Step 2: Run** — fail.
- [ ] **Step 3: Implement.** The arm lives on `WorkflowView.Armed`; `App.disarmed` is not involved (the view owns every key while open). Endings clear `Armed` in the fold path when the open task leaves `TaskRunning`.
- [ ] **Step 4: Run** — green.
- [ ] **Step 5: Commit** `feat(ui): stop a running workflow and save one as a command`

---

### Task 11: Screen test (real binary, pty)

**Files:** Modify `cmd/wake/fakeagent_test.go` (a `workflows` script); create `cmd/wake/workflowscreen_unix_test.go`.

- [ ] **Step 1: Write the test first.** Script: on `go`, print a `Workflow` tool_use + tool_result, `task_started` (`local_workflow`, `workflow_name:"count-lines"`, a two-line script in `prompt`), then three `task_progress` frames with `workflow_progress` copied from `workflow-run.jsonl` (ids/labels as recorded), holding the run open; on a `stop_task` control request print `task_updated {patch:{status:"killed"}}`, `task_notification {status:"stopped"}` and the success receipt (shapes from `workflow-stop.jsonl`). Test: `startWakeInAConversation`, send `go\r`, await `◈ count-lines`; pick the row (`⇧↓` until the cursor is on it) and `\r`; await `Phases` and `count a.txt`; `\x1b[C` to agents, `\r` into an agent, await `Prompt`; `\x1b` back twice; `x`, await `↵ stop`; `\r`; await `halted` in the transcript and the sidebar row gone. A second test saves: `s`, `\r`, await `Saved /count-lines`, and assert the file exists under the scratch project's `.claude/workflows/`.
- [ ] **Step 2: Run** `go test ./cmd/wake -run Workflow -count=1` — fails until Tasks 7–10 are wired; fix wiring gaps it reveals.
- [ ] **Step 3: Commit** `test(cmd): drive a workflow through the real binary`

---

### Task 12: Docs and derived guards

**Files:** `CLAUDE.md` ("What it does today": a `/workflows` + sidebar row; the slash commands row gains `/workflows`; "Key locations": one row naming the new files; the "two largest files" sentence if changed), `docs/notes/decisions.md` (one entry: the stop-one-agent/pause refusal and the fork ruling).

- [ ] **Step 1:** `go test ./internal/core -run CLAUDEmd` and `./internal/ui -run CLAUDEmd` — fix what they report.
- [ ] **Step 2: Commit** `docs: workflows in CLAUDE.md and decisions`

---

### Task 13: Demo videos

**Files:** `demo/agent/claude` (a `workflow` step kind emitting frames copied from `testdata/stream/workflow-*.jsonl`, answering `stop_task`, writing the agent transcripts and run record under the scratch `HOME`'s projects dir so the daemon can read them, and treating `/count-lines` as a launch), `demo/scenarios/*.json`, `demo/tapes/13-workflow-row.tape`, `14-workflow-view.tape`, `15-workflow-stop.tape`, `16-workflow-save.tape`, `17-workflows-room.tape`, `18-workflow-failed.tape`.

Flows, one video each: (1) a workflow starts and its sidebar row fills in; (2) `↵` → phases → agents → agent detail → expand → back; (3) `/workflows` in the room lists every agent's runs; (4) `x` `↵` stops a run — the row goes, `halted` line lands; (5) `s` saves, then `/count-lines` in the completion menu launches it again; (6) a failed run's ending and error in the view.

- [ ] **Step 1:** Extend the fake agent; run each tape with `demo/record.sh`; check every frame by eye (neutral `/tmp/<name>` project path, scratch `HOME`, fresh socket dir per take).
- [ ] **Step 2:** Convert to GIF (`ffmpeg … -vf "fps=12,scale=1200:-1:flags=lanczos"`) and keep MP4s; before/after stills (`main` build vs branch, same scenario).
- [ ] **Step 3: Commit** the tapes, scenarios and fake-agent change (not the media) `chore(demo): scripted workflow scenes`

---

### Task 14: Gate, reviews, PR

- [ ] `make ci` alone (no concurrent test runs), read the exit code.
- [ ] Codex adversarial review (background) + `code-reviewer` agent over `git diff --name-only origin/main...HEAD`; validate every finding against the code; fix CRITICAL/HIGH, re-run `make ci`.
- [ ] Sweep `pr-assets/*` per CLAUDE.md, push media to an orphan `pr-assets/feat/workflow-sidebar`, push the branch, open the PR with Summary, Screenshots (before/after), Videos (GIF inline + MP4 links), Test plan, `make ci` exit code, both reviews' findings.
