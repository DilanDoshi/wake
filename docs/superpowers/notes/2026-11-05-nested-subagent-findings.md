# A subagent that dispatches its own

**Recorded 2026-11-05 against Claude Code 2.1.314**, headless with Wake's argv (`--safe-mode`,
`--model haiku`, `--permission-mode auto`), in a scratch `/tmp` project. Three recordings; one is
committed, `testdata/stream/subagent-nested.jsonl` (a general-purpose subagent dispatching two Explore
subagents in parallel). The other two were a three-level chain and a probe of the tools each level is offered.
For BUG-61.

## On stdout

- **The inner `task_started` names its parent.** It carries `parent_task_id`, the dispatching subagent's
  `task_id`, and `spawn_depth: 2` (`subagent-nested.jsonl:38`, `:39`). The one the agent dispatched
  itself carries `spawn_depth: 1` and no `parent_task_id` (`:29`). New on every `task_started`, both
  levels: `run_id`, `is_backgrounded`.
- **The inner subagent's frames are forwarded under its own dispatch.** Their `parent_tool_use_id` is the
  inner `task_started`'s `tool_use_id`, not the outer's (`:40` onward), so its transcript files under its own
  row as any subagent's does. The outer's `Agent` call and its result are forwarded under the outer's.
  Forwarded frames now also carry `agent_id`.
- **Each task's usage is its own.** The outer's `total_tokens` went 12,982 → 15,314 across its two
  children's ~10,400 each (`:36`, `:62`): reading their reports into its context, not their spend.
- **In auto mode a subagent's `SubagentHandback` receipt** is one text block,
  `{"success":true,"message":"Report delivered to your caller."}` (`:53`, `:57`, `:64`); the same on all
  2,264 delivered receipts in the machine's subagent transcripts, and `"success":false` with a reason on
  the 8 refused ones. BUG-62.

## On disk

`<session>/subagents/agent-<id>.meta.json` holds `parentAgentId` and `spawnDepth`, and every level's
transcript sits flat in the same `subagents/` directory, so `FrameSubagent`'s read by agent id opens a
nested one unchanged.

## How deep

A depth-2 subagent is offered the Agent tool (the probe's answer), so a third level is possible. None was
seen: across the recording machine's 1,693 subagent `.meta.json` files, 1,427 are `spawnDepth` 1 and 266
are 2, each of the 266 naming a `parentAgentId`. In a three-level ask, the depth-2 subagent did the
work itself rather than dispatch.

## Re-checked on 2.1.322 (2026-11-11)

Same argv and project shape, for the inner subagent's ending line (deferred.md, 2026-11-05).

- **The foreground nest is unchanged.** The fixture's own prompt gave the same frames in the same order
  with the same keys on `task_started`, `task_updated` and `task_notification`. Not committed: it adds
  nothing `subagent-nested.jsonl` lacks.
- **No ending names its parent.** Of the frames core decodes, only the inner `task_started` carries
  `parent_task_id`, on both versions; its `task_notification` does not (`subagent-nested-background.jsonl:74`,
  `:96`), so `Tasks.named` carries it over from the row. 2.1.322's undecoded `background_tasks_changed`
  lists it per member too (`:50`).
- **A background nest ends its parent first.** Committed as `subagent-nested-background.jsonl`
  (`run_in_background` at both levels, `is_backgrounded: true`): the outer hands back at once and its one
  `task_notification` is `completed` (`:66`) while both children run. Their hand-backs then reach the
  agent itself (`:108`, `:114`), with the cross-session lead-in ahead of `<agent-message …>`. The
  recording opened with a `system/ui_invalidate` line, left out (deferred.md, 2026-10-03).
- **Claude Code's own UI draws no inner ending.** Two interactive probes (haiku, auto mode), one with the
  outer waiting for background children and one with it told not to: the main conversation drew only the
  outer's `Agent "…" finished`, and in both the outer stayed alive until its children ended.
- **A `claude` run by hand from inside a Claude Code session saves no transcript.** It inherits
  `CLAUDE_CODE_CHILD_SESSION` and says so under its prompt; the probes above ran that way. Wake's agents
  never inherit it (`core.nestedSessionEnv`), but a hand-run recording or probe needs
  `env -u CLAUDE_CODE_CHILD_SESSION`.
