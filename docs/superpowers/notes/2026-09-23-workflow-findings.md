# Workflow findings — what a headless `Workflow` run puts on the wire

Recorded 2026-09-23 against **2.1.281**, with Wake's own argv (every visibility flag, `--permission-prompt-tool
stdio`, `--permission-mode auto`). Three sessions, one fixture each:

| Fixture | What it is |
|---|---|
| `testdata/stream/workflow-run.jsonl` | A 2-phase, 3-agent workflow (`count-lines`) that completes |
| `testdata/stream/workflow-failed.jsonl` | 3 phases, a 6-wide parallel phase, then a deliberate `throw` — ends `failed` |
| `testdata/stream/workflow-slash.jsonl` | `/workflows`, `/workflow-launch-exec` (bare and with an argument), `/__remote-workflow` |
| `testdata/transcript/workflow-agent.jsonl` | One workflow agent's on-disk transcript, **trimmed to its `user`/`assistant` lines** |

## Provenance caveats

- **Recorded under the real `HOME`, then scrubbed**, for the partial-messages note's reason (credentials live
  in the keychain). Every `hook_*` frame was then **deleted** from all three stream fixtures: under the real
  `HOME` they carried the operator's own SessionStart hook output, and none of them bears on workflows.
- **The agent was told to call `Workflow` with a given script and not to load a skill.** A first recording
  let it load the bundled workflow-authoring skill, whose full text then arrived as a user frame; that
  recording was discarded rather than scrubbed.
- The agent transcript's other lines (attachments, including a `prompt_snapshot` of the sub-agent's prompt)
  were dropped, so the file is a minimal shape sample, not a byte-for-byte recording.

## 1. A workflow is a dispatch — `task_type: "local_workflow"`

It reuses the five `task_*` subtypes `core.taskUpdate` already decodes. `task_started` carries
`task_type:"local_workflow"`, a `w`-prefixed nine-character `task_id`, `tool_use_id` (the `Workflow` call),
`description` (the script's `meta.description`), **`workflow_name`** (`meta.name`) and the script as `prompt`.
No `subagent_type`. Today it resolves to `core.TaskKindUnknown`, `ui.Task.Openable` is false, and `Fleet.RunningTasks` drops it — so the
sidebar shows nothing while a workflow runs.

## 2. `task_progress` carries `workflow_progress`, a full snapshot

Beside the usual `description` (`"<phase>: <agent label>"`), `usage` and `last_tool_name` (the agent label, not
a tool), some progress frames carry `workflow_progress`: an array of `workflow_phase` entries
(`index`, `title`) followed by `workflow_agent` entries. **Every one is a full snapshot** — all phases and every
agent started so far (10 of 18 progress frames in the failed run; the list grows 6 → 7 as the reducer starts,
and every earlier agent is repeated). A frame without the key changes nothing structural.

A `workflow_agent` entry: `index`, `label`, `phaseIndex`, `phaseTitle`, `agentId` (seventeen characters, the
subagent id), `model`, `state`, `attempt`, `queuedAt`/`startedAt`/`lastProgressAt` (epoch ms), `tokens`,
`toolCalls`, `durationMs`, `lastToolName`, `promptPreview`, `resultPreview`.

`state` is recorded as **`start`** and **`done`** only. A queued agent, a failed agent and a retried one
(`attempt` > 1) are unrecorded.

## 3. Nothing a workflow agent says reaches stdout

No frame carries a `parent_tool_use_id` in either run — `--forward-subagent-text` does not forward a workflow
agent's speech. What each agent did is on disk, at
`~/.claude/projects/<slug>/<session>/subagents/workflows/<runId>/agent-<agentId>.jsonl` (with a `.meta.json`
beside it and a `journal.jsonl` for the run), and `agentId` is in the snapshot. The run id (`wf_…`) is on no
system frame: it appears only in the `Workflow` tool result's text, which also names the transcript directory
and script path. The agent transcript's lines are ordinary `user`/`assistant` lines with `isSidechain:true`.

## 4. Endings

Success: `task_updated {patch:{status:"completed", end_time}}` then `task_notification {status:"completed",
summary, usage, output_file}`. Failure: the patch adds **`error`** (a message plus a JS stack) and the
notification's `status` is `"failed"`, its `summary` beginning `Dynamic workflow "<description>" failed:`.
The notification then starts a **new turn** (the second `result` in each fixture) on which the parent
reports the outcome. `background_tasks_changed` brackets the run with the live set.

## 5. No native launch outside the tool

All four slash probes are no-op receipts (`num_turns:0`, `$0`): `/workflows` "isn't available in this
environment"; `workflow-launch-exec` is an internal hand-off with nothing pending; `__remote-workflow` runs only
in a remote session. **The only way to start a workflow headless is a prompt asking the agent to use one.**
