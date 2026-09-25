# Workflows — the sidebar row, the `/workflows` view, stop and save

**Status:** design approved in conversation 2026-09-23/24 (owner chose "one row in the sidebar, detail in a
view", `↵` opens it, `/workflows` opens the same, stop and save included). Wire facts are recorded, not
assumed: `docs/superpowers/notes/2026-09-23-workflow-findings.md` and `testdata/stream/workflow-*.jsonl`.

## 1. What this is

Any agent in a fleet can run a Claude Code dynamic workflow (the `Workflow` tool, a saved `/<name>`, or the
bundled `/deep-research`). Today Wake decodes one as `TaskKindUnknown`, `ui.Task.Openable` is false, and
`Fleet.RunningTasks` drops it — **a running workflow is invisible**. Headless claude cannot draw its own
`/workflows` view (`workflow-slash.jsonl`: "isn't available in this environment"), so Wake draws one, the way
it draws bare `/effort` and `/model`.

Out of scope, because headless claude refuses or ignores them (findings §6): pause/resume, restart an agent,
stop one agent (accepted with `success` and ignored). A key Wake draws does what it says, so those keys are
absent rather than disabled. Launching needs nothing new: a saved workflow is already in
`init.slash_commands`, so the completion menu offers `/<name>` and it passes through untouched (findings §5).

## 2. What the operator sees

**Sidebar.** A running workflow is one row under the agent that runs it, beside its running subagents:

```
● iris           ↓ 48k
  ⎿ ◈ count-lines 2/3
```

`◈` (U+25C8, one cell everywhere) marks a workflow; the figure is agents done / agents started, dropped whole
rather than cut when the 24-column budget will not hold it (`subagentRow`'s token rule). It leaves when the
run ends, like every sidebar dispatch. The board's rows and tiles draw it through the same row function.

**`↵`, `⌃D` or a click on that row** opens the workflow view in that agent's pane. **`/workflows`** opens the
same view: in a conversation, that agent's runs; in the room, every agent's runs grouped under the agent's
name (the fleet-wide list is Wake's own). With exactly one run in scope the list is skipped, as Claude Code
does; with none it says `No workflows in this session.` / `No workflows in this fleet.`

**The run level** — a header and two columns:

```
count-lines · Count lines of a.txt and b.txt …          2/3 agents · 28s · running
╭ Phases ───────┬ Count · 2 agents ──────────────────────────────────────╮
│ ❯ ✔ Count 2/2 │ ❯✔ count a.txt   haiku · 16.5k                     4s │
│   2 Sum   0/1 │  ✔ count b.txt   haiku · 16.6k                     5s │
╰───────────────┴────────────────────────────────────────────────────────╯
↑↓ select · ↵ open · f filter · x stop · s save · esc back
```

A phase is `✔` when all its agents are done, else its number; an agent is `✔` done, `⏺` running, `✗` failed
(unrecorded state words draw `·` and their word). A failed run shows its error's first line under the header.

**The agent level** — `↵`/`→` on an agent: status and model; tokens · tool calls · duration; **Prompt**;
**Activity** (each tool call as its one-line headline, the way a conversation draws it); **Outcome** (the
result). `↵` expands Activity to each call's input and the start of its result; `j`/`k` scroll. It is read
from the agent's own transcript on disk and re-read when that agent's entry in a new snapshot changes —
an event, never a timer.

**Keys** (inside the view only, read above `App.key`'s switch like the resume picker, so no legend entries):
`↑↓` select · `↵`/`→` drill in (and expand, at the agent level) · `esc`/`←` back out (and close at the top) ·
`f` cycle the agent filter (all → running → done → failed) · `j`/`k` scroll the detail · `x` then `↵` stop the
run (running only; anything else disarms; the confirm is drawn while armed) · `s` save. `⌃C` closes the view
and keeps its meaning (kill-switch rule). The view's own key line advertises them.

**Save** — `s` opens a dialog over the view:

```
Save workflow · project · .claude/workflows/count-lines.js
Save as: > count-lines
↵ save · ⇥ project/user · esc cancel
```

`⇥` toggles project (`.claude/workflows/`) and user (`~/.claude/workflows/`, or `$CLAUDE_CONFIG_DIR/workflows/`).
The name defaults to the workflow's own and edits in place. The answer is a notice: saved, with the path and
that it runs as `/<name>` in new sessions — or the refusal.

**Endings.** The conversation gets `● Workflow "count-lines" finished · 9s` (green), `… halted · 4s`
(stopped), or `… failed · 10s` (red) with the error's first line; the **room** gets the same line headed by
the agent, because a fleet's workflow finishing is room news (subagent endings stay conversation-only).

## 3. Architecture

### 3.1 Airlock (`internal/core`)

- `TaskWorkflow TaskKind = "workflow"` (`taskKinds["local_workflow"]`) and `TaskFailed TaskStatus = "failed"`
  (`taskStatuses["failed"]`). Both words are now recorded; the two tests that pinned them unknown are updated.
- `TaskUpdate.Workflow *WorkflowUpdate` — Wake's types in a new `core/workflow.go`: `Name` (workflow_name),
  `Script` (task_started's prompt), `Error` (task_updated's `patch.error`), and `Progress *WorkflowSnapshot`
  (`Phases []WorkflowPhase{Index, Title}`, `Agents []WorkflowAgent{Index, Phase, Label, AgentID, Model,
  State, Attempt, Tokens, ToolCalls, Duration, Prompt, Result}`) with `WorkflowAgentRunning/Done/Failed/
  Unknown`. Nil on every non-workflow frame.
- The wire structs and the recogniser `workflowOf(f)` live in `encode.go` (the one airlock file with room —
  `goalOp`/`toolLoopOp`'s precedent); `wire.go` gains the three `wireFrame` fields and `wireTaskPatch.Error`;
  `protocol.go`'s `taskUpdate` gains one field, paid for by a line it already has.
- `EncodeStopTask(requestID, taskID)` + `Session.StopTask(taskID)` (`write.go`), recording nothing, like
  `SetMode`.
- `DecodeSidechainLine` — `DecodeTranscriptLine` minus the sidechain drop, one shared body; every line of a
  workflow agent's transcript is `isSidechain:true` (`testdata/transcript/workflow-agent.jsonl`).
- `DecodeWorkflowRun(raw) (WorkflowRun, error)` for `workflows/wf_*.json`: `TaskID, Name, Summary, Status,
  Error, Started, Duration, Tokens, Script, Progress` — its `workflowProgress` elements are the stream's shape,
  so one element decoder serves both.
- `contain.go`: `containedTask` contains every new string.
- Vocabulary guards: each new wire word is classified; `stop_task` is outbound-only (`notInTheCorpus`, as
  `interrupt` is).

### 3.2 Daemon

- **Snapshot replay.** `agent.runningTasks` keeps the latest `Workflow.Progress` on the retained
  `task_started` event, so `replayRunningTasks` hands a late attach the run *and* where it is.
- **Stop.** `rpc.FrameStopRun{SessionID, Workflow.Task}` → `apply` refuses unless that id is a running
  workflow of this session → `Session.StopTask`. The confirmation is the `killed`/`stopped` frames, not the
  receipt (success is not a verdict).
- **Runs on disk.** `FrameWorkflows{SessionID}` → `FrameWorkflowsReply{Workflow.Runs}`: the session directory
  is `transcriptPath` minus `.jsonl` (found by filename, never built from a slug); every `workflows/wf_*.json`
  under it, bounded in count and bytes, decoded by `DecodeWorkflowRun`.
- **An agent's transcript.** `FrameWorkflowAgent{SessionID, Workflow.Agent}` →
  `FrameWorkflowAgentReply{Events}`: `subagents/workflows/*/agent-<id>.jsonl`, the id fenced to one token
  (`rpc.ValidWorkflowAgentID`), decoded by `DecodeSidechainLine`, bounded like `History`.
- **Save.** `FrameSaveWorkflow{SessionID, Workflow{Task, Name, Scope}}`. **The wire carries a name and a
  scope; the daemon owns the path and the bytes** (`--debug-file`'s ruling). Name fenced by
  `rpc.ValidWorkflowName` (one segment, `[a-z0-9][a-z0-9_-]*`). Script from the retained start event while
  running, else the run record with that `taskId`. Project directory: the nearest existing
  `.claude/workflows` walking up from the agent's cwd to its repository root, else `<root>/.claude/workflows`,
  else `<cwd>/.claude/workflows` (Claude Code's documented rule). Refuses a symlinked `.claude`,
  `.claude/workflows` or target (project) / target (user), and **refuses an existing file** — overwriting a
  saved workflow is not a keystroke. Answers `FrameWorkflowSaved{Workflow.Path}` or a `FrameError`.
- New frames live in `rpc/workflow.go`; `rpc.Frame` gains one field, `Workflow *WorkflowFrame`. Every client
  verb is **refused to the manager** (`mcpguard_test.go`): stop and save change the machine and appear in no
  row the manager's tools return; the two reads are `FrameHistory`'s ruling.
- **Fork.** A running workflow does **not** block a fork: `subagenttrack.go` guards a subagent whose
  forwarded frames write the parent's conversation, and a workflow's agents write only their own sidechain
  files (findings §3) — the background shell's position. Pinned by a test beside the shell's.

### 3.3 UI

- **Fold.** `ui.Task` gains `Workflow core.WorkflowSnapshot` (latest), `Error`; `Tasks.Observe` replaces the
  snapshot when a frame carries one, and `Name` takes the workflow's name. `Fleet.RunningTasks` admits a
  running workflow beside openable agents.
- **Row.** `subagentRow` dispatches a workflow to `workflowRow`; every height/walk/click function already
  counts `RunningTasks` rows, so it participates for free (roster, board rows, tiles).
- **Opening.** `viewingPicked` and `↵` on a roster sub-row branch on `Kind == TaskWorkflow` to
  `openWorkflow(session, task)`.
- **View.** `App.workflow WorkflowView{Pane, Session, Task, Level, Phase, Cursor, Column, Filter, Armed,
  Expanded, Scroll, Save *saveDialog}` in `workflowview.go` (state + keys), `workflowdraw.go` (render),
  `workflowsave.go` (dialog), `workflowdata.go` (runs = live `Tasks` rows ∪ disk runs, de-duplicated by task
  id, live winning). Drawn **in place of its pane's body** (`dmPane`/`roomPane`), pane-scoped like the
  rewind picker; key-capturing like the resume picker; mouse presses inside the pane route to it (a click
  selects a row) so transcript selection never measures a pane that is not drawn.
- **`/workflows`** registered in `slash.go`'s `commands` (not advertised in any recorded headless
  `slash_commands`; `wakeCommandCount` 15 → 16); handler in `workflowview.go`.
- **Endings.** `taskLineKind` names `Workflow`; `taskLineWord`/`Style` gain `failed` (red); the failure's
  first line follows. `fold` admits a workflow ending to the room; `chat_blocks.go` draws it headed by the
  agent.

## 4. Testing

TDD per unit. Airlock: every fixture decodes; snapshots, endings, run records, sidechain lines, stop encoding
against `testdata/input/workflow-stop.stdin.jsonl`. Daemon: replay carries the latest snapshot; stop refused
off a non-workflow/finished id; runs and agent transcripts read from a scratch projects dir; save's fences
(name, symlinks, existing file, scope paths). UI: fold, row (width budget), open paths, every view level and
key, filter, arm/disarm, save dialog, room list, ending lines. **Screen (pty):** a `workflows` fake-agent
script driving a real run — row appears, `↵` opens, drill to an agent, stop, save.

## 5. Deliverables beyond the code

`CLAUDE.md` ("What it does today", key locations); the PR carries **demo videos** of each flow (VHS against
the real binary with a scripted `demo/agent/claude`, GIF inline + MP4 linked, on `pr-assets/<branch>`) and
before/after screenshots; `make ci` exit 0; code review + adversarial (Codex) review before opening.
