# Goal & Loop — surfacing Claude Code's native `/goal` and `/loop` in Wake

**Date:** 2026-09-11 · **Status:** Design (shape approved; pending spec review) ·
**Verified against:** claude 2.1.268

## 1. Premise

Operators running 15–30 agents want two of Claude Code's native "keep working" features
visible and controllable from the room and DMs:

- **`/goal <condition>`** — a session contract: the agent keeps taking turns until a
  completion condition holds. Implemented as a session-scoped **Stop hook**; after each turn a
  small fast model judges the condition met / not-yet / impossible.
- **`/loop [interval] <prompt>`** — re-run a prompt on a fixed cron interval, or **self-paced**
  (Claude chooses the delay each iteration).

Wake has neither today. This design adds them as **trigger + monitor + render**, with **no
scheduling engine of Wake's own**: Claude runs the loop/goal inside the agent process; Wake
forwards the commands and renders what it observes on the wire.

## 2. Scope check (§17 / §2)

Neither feature is a terminal emulator, multiplexer, or model-agnostic layer (§2), and neither is
on §17's Out list. They add **no Wake-side timer and no per-agent ticker** — the scheduling is
Claude's, inside the agent process — so *"cheap to leave open"* holds: Wake reads frames it already
receives and renders per change. New owned state is minimal (a small goal/loop struct on the fleet
report), in the spirit of the existing `Effort`/`Model`/`PRs`/`Color`/`Commands` facts. This is
new capability past v1's In list, but consistent with the non-negotiables.

## 3. Wire facts (recorded probes, 2026-09-11)

All under `claude --print --input-format stream-json --output-format stream-json --verbose
--include-hook-events` — **Wake's exact spawn mode**. Behaviour only; committable fixtures to be
captured in a sterile `HOME` and scrubbed at build time (these probes ran under a real `HOME`).

**Goal — a genuine headless command.**
- `/goal <condition>` sent as a stream-json **input frame** (Wake's send path) is honored by the
  CLI: it emits a synthetic `Goal set: <condition>` assistant message (a 0-token `<synthetic>`
  frame, like the bare-`/effort` receipt) and starts a turn. It is **not** improvised.
- The evaluator is a **Stop hook**: `system/hook_started`/`hook_response`/`hook_progress` frames
  with `hook_name:"Stop"` fire at each turn-end — on the wire because Wake passes
  `--include-hook-events`.
- One goal per session; condition ≤ 4000 chars; met/impossible/`/goal clear` clears it.
- **Survives `--resume`** (condition restored; turn/timer/token baseline reset) → survives a Wake
  park + wake.
- Wake spawns `auto`, which is exactly what goal turns need to run unattended.
- Stop: `/goal clear` (aliases `stop`/`off`/`reset`/`none`/`cancel`) or interrupt.

**Loop — native mechanism, model-mediated trigger.**
- `/loop` is a bundled *skill*; **headless the slash is not a CLI command** — the model reads the
  text and reproduces the behaviour with the native bundled tools. All modes verified:
  - **Fixed-interval** (`/loop 5m …`) → `CronCreate {cron, prompt, recurring:true}`; tool_result
    names the job id, cadence, and *"session-only (dies when Claude exits), auto-expires 7 days."*
  - **Self-paced** (`/loop …`) → `ScheduleWakeup {delaySeconds, reason, noop, prompt}`;
    tool_result *"Next wakeup scheduled for HH:MM:SS (in Ns)…"*. Delay chosen dynamically with a
    reason; `noop:true` marks a quiet tick.
  - **Bare `/loop`** → runs the built-in maintenance prompt (verified: the model began the
    autonomous working-dir / branch check).
  - **`.claude/loop.md`** → read and executed (verified via a unique marker).
  - **Skill-as-prompt** (`/loop 20m /review-pr 1234`) — *not directly tested*; expected to work by
    the same prompt mechanism.
- The cron/wakeup tools are bundled (disabled only by `CLAUDE_CODE_DISABLE_CRON=1`), so a
  Wake-spawned agent has them. Scheduled fires occur **while the session is running and idle** —
  Wake keeps the process alive, so they fire.
- **Iterations arrive as self-started turns** (the runtime re-invokes the agent with the loop
  prompt); Wake already models idle-gap self-started turns (`Agent.notDone`).
- Park/resume: **fixed-interval cron is restored** on `--resume`; **self-paced is not**.

## 4. Trigger — mostly passthrough

- `/goal …`, `/goal clear`, and `/loop …` are **not Wake commands** — they pass through to the
  agent byte-for-byte, the existing rule for Claude's own slash commands (`/clear`, `/compact`).
  In a DM they reach the focused agent; from the room, `@who /goal …` / `@who /loop …` ride the
  existing mention→target bridge (`mentionCommand`, as `@who /color` does).
- So the trigger needs **little or no new send-path code**. We add `goal`/`loop` to
  `roomTargetCommands` so a room `@who /loop …` routes to that one agent rather than broadcasting.
- **Stop is not symmetric — and this refines the earlier "stop just removes the rendering" ruling:**
  - **Goal:** `/goal clear` genuinely stops it (passthrough). The badge clears when Wake observes
    the clear / achieved / impossible.
  - **Self-paced loop:** interrupt (esc) clears the pending wakeup.
  - **Fixed loop:** the cron keeps firing until cancelled, and **Wake cannot call `CronDelete`**
    (the non-negotiable: the UI never touches an agent's process/tools). "Stop a fixed loop" is a
    passthrough instruction the agent executes (`CronDelete <id>`). **Removing Wake's badge does
    not cancel the cron** — the badge clears on the *observed* cancellation, not on a keypress.

## 5. Monitor — recognize the native signals, fold to `Agent`

The signals are all `tool_use`/`tool_result`/hook frames the airlock already decodes. Following the
checklist precedent (`core.toolChecklistOp` in `vocabulary.go` decodes a specific tool's meaning
into a Wake op), the goal/loop recognizers live in the **existing airlock files** — **no fifth
airlock file** — and surface Wake-native events folded onto `Agent` in `internal/ui/fleet.go`,
carried on `rpc.SessionStatus` for late attach (the path `Effort`/`Model`/`PRs`/`Commands` take):

- **Goal:** `Goal set:` synthetic message → active + condition; `Stop`-hook frames and the bare
  `/goal` status message → latest verdict/reason; achieved/cleared/impossible → ends.
- **Loop (fixed):** a `CronCreate` tool_use → active, schedule from `{cron}`; tool_result → job id.
- **Loop (self-paced):** a `ScheduleWakeup` tool_use + tool_result → active, next-fire time,
  reason; `noop:true` → quiet-tick streak.
- **Iterations:** self-started turns while a loop/goal is active → `iter N`.

New state, minimal and comparable (so `Agent` stays usable by `Observe`'s `now == was`):
`Agent.Goal{active, condition, lastReason, …}` and
`Agent.Loop{mode: fixed|selfPaced, cadence|nextFire, iter, quiet, jobID}`. Folded in `WithStatus`;
carried on the report.

## 6. Park / resume

- **Goal survives** park + wake (restored on `--resume`) → keep the badge; the turn/timer reset is
  Claude's, and Wake re-derives from the woken session's frames.
- **Self-paced loop does not survive** → clear the badge on wake, exactly as permission mode's
  `modeReverted` does.
- **Fixed cron survives** → keep the badge; it re-registers on resume.
- **No new `parkedRecord` field** — the state is Claude's (restored by `--resume` or not); Wake
  re-derives it from frames, never writes it to the park book.

## 7. Render — the locked catalog

The renderings are the catalog already agreed with the owner (2026-09-10/11). Summary, with the
corrections the wire facts introduce:

- **Right sidebar (roster):** `◆` (goal) and `↻` (loop) are **always-on head-line markers**,
  budgeted name → `◆` → `↻` → tokens (tokens drop first). The indented activity line carries the
  detail when the agent is not running a tool: `◆ <condition>` and `↻ next in 20m` / `↻ every 5m` /
  `↻ quiet ×3` / `↻ paused · blocked`.
- **DM (Direction B):** goal → status-bar segment `◆ <condition>` (truncates, drops whole); loop →
  the working line (`✻ … · ↻ iter 4`) and, **between iterations**, a waiting line that wins over
  the done line (`↻ Looping · iter 4 done · next in 20m` / `next when idle`), the compacting-line
  precedent.
- **Room:** compact per-agent `◆`/`↻` badges on the roster; `N looping` in the awareness strip
  (blocked-mid-loop counts under `need you`, not `looping`); full goal/loop text only for the
  addressed agent in the room bar.
- **Board:** inline goal/loop where the wider row allows.
- **Edges:** (1) blocked-mid-loop pauses and renders `↻ paused · blocked`; (2) goal-met → green
  terminal line, cap/budget → muted line + notice; (3) manual stop removes the rendering **once the
  real cancellation is observed** (see §4).
- Waiting/next-fire figures come from the real `ScheduleWakeup`/`CronCreate` data; `noop:true` ⇒
  quiet streak; goal's latest reason shows under its condition.

## 8. Staging

1. **Goal first** (cleanest: a real headless CLI command, survives resume): airlock recognizers +
   `Agent.Goal` + report field + render (status bar, sidebar, room bar, board) + `@who /goal`
   routing + `/goal clear`. Sterile fixtures.
2. **Loop** (fixed, then self-paced): recognizers for `CronCreate`/`ScheduleWakeup` + `Agent.Loop`
   + report + render + the stop nuance. Sterile fixtures.

## 9. Tests

- **Airlock golden fixtures** (sterile, scrubbed): `goal-set` + `Stop`-hook, `cron-create`,
  `schedule-wakeup`, a self-started iteration turn. Two corpora if the on-disk transcript shape
  differs.
- **Fold tests** (pure): frames → `Agent.Goal`/`Agent.Loop`.
- **Screen tests** per surface (sidebar, DM status bar, DM waiting line, room bar, board) and the
  three edges.
- **Park/resume:** goal survives; self-paced loop clears; fixed cron survives.
- 80% coverage; `make ci` green before the PR; two review passes (code + adversarial).

## 10. Open questions

- **Goal glyph:** ~~keep `◆` or echo Claude's `◎`?~~ **Decided (owner, 2026-09-11): Wake's own
  glyphs — `◆` goal, `↻` loop.**
- **Fixed-loop stop UX:** Wake sends a `CronDelete <id>` passthrough instruction vs leaving
  cancellation to the operator/agent. The badge must follow the observed cancellation either way.
- **Skill-as-prompt fidelity** (`/loop 20m /review-pr`) — untested; verify before relying on it.
- **Goal verdict surfacing:** show the per-turn verdict inline in the transcript, or only as the
  badge's `lastReason`?
