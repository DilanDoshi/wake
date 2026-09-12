# Goal Feature Implementation Plan (Stage 1)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an operator set/see Claude Code's native `/goal` on any agent from Wake — trigger by
forwarding `/goal <condition>`, monitor the wire signals, and render `◆ <condition>` across the
sidebar, DM status bar, room bar, and board. No engine — Claude runs the goal.

**Architecture:** `/goal` is a real headless CLI command; the airlock recognizes its lifecycle
signals (the `Goal set:` / `Goal cleared:` / achieved / impossible synthetic messages and the `Stop`
hook) as a Wake-native goal op, the daemon folds it onto the supervised agent and puts it on the
fleet report (for late attach), and `internal/ui` folds it onto `Agent.Goal` (live + report) and
renders it. This is the exact flow `Commands`/`Effort`/`PRs` already take.

**Tech Stack:** Go 1.26+; bubbletea/lipgloss/bubbles/glamour; the existing airlock, daemon, rpc, ui.

**Spec:** `docs/superpowers/specs/2026-09-11-goal-loop-design.md` (read it alongside this plan).

## Global Constraints

- **Airlock:** only `internal/core/{protocol,wire,vocabulary,encode}.go` may know Claude's JSON.
  Add the goal recognizer to `vocabulary.go`/`protocol.go` — **no fifth airlock file**
  (`airlock_test.go`).
- **Identity flags** stay only in `argv.go` — untouched here.
- **TDD:** failing test first; `make ci` exit 0 is the only gate; 80% coverage floor.
- **Fixtures:** record into a sterile/credential-only `HOME`, then `scripts/scrub-fixtures.py`;
  `internal/core/corpus_test.go` guards them. Every `testdata/stream/*.jsonl` line must decode
  (`TestDecodeRecordedFixtures`).
- **Never test against a live LLM** in the suite — replay the committed fixtures.
- **Files:** 200–400 lines typical, 800 hard max (`TestNoNonTestFileCrossesTheHardMax`).
- **Glyph:** goal is `◆` (Wake's own; not Claude's `◎`). Palette/colour only in `theme.go`.
- **Git:** branch `feat/session-goal`; commit per task; **no Claude attribution** anywhere; two
  review passes (code + adversarial) before the PR; the spec doc rides in this PR.
- **Immutable folds:** `Agent` must stay comparable (`Observe`'s `now == was`) — goal state on
  `Agent` is value-typed (string/bool), not a pointer/map.

---

## Task 1: Capture the goal-lifecycle fixtures

**Files:**
- Create: `testdata/stream/goal-achieved.jsonl` (set → not-yet-met → continue → achieved)
- Create: `testdata/stream/goal-cleared.jsonl` (set → `/goal clear`)
- Create: `testdata/stream/goal-status.jsonl` (set → bare `/goal` status)

**Interfaces:**
- Produces: the golden bytes every later decode task asserts against. Their exact `Goal set:` /
  `Goal cleared:` / achieved / `Stop`-hook shapes are the source of truth for Task 2.

- [ ] **Step 1: Capture a tool-free multi-turn goal.** In a scratch dir, headless, bounded:
  send `/goal you have counted from 1 to 3, one number per assistant turn, then say COUNT DONE`
  (tool-free, so no permission complexity; met after ~3 turns). Save stdout stream-json.
- [ ] **Step 2: Capture the clear path.** Fresh session: send `/goal <trivial condition>` then
  `/goal clear`; save stdout.
- [ ] **Step 3: Capture the status path.** Fresh session: `/goal <trivial condition>` then bare
  `/goal`; save stdout.
- [ ] **Step 4: Scrub + guard.** Run `python3 scripts/scrub-fixtures.py` (and `--check`), then
  `go test ./internal/core -run TestDecodeRecordedFixtures` — every line must decode (initially as
  `KindUnknown`/existing kinds; Task 2 gives the goal lines meaning). Confirm `corpus_test.go` green.
- [ ] **Step 5: Commit.** `git commit -m "test: record /goal lifecycle stream fixtures"`

**Note:** capture is the one place a live `claude` runs — never in CI. If a resumed goal re-emits a
`Goal set:`/status shape on `--resume`, capture that too (Task 11 depends on it); if it does not,
Task 11 records that and clears the badge conservatively.

---

## Task 2: Airlock — decode the goal lifecycle to a Wake-native op

**Files:**
- Modify: `internal/core/vocabulary.go` (add `goalOp`, mirroring `toolChecklistOp`)
- Modify: `internal/core/protocol.go` (emit the op from `messageEvents`/system handling)
- Modify: `internal/core/event.go` (add `KindGoal` + the payload type)
- Test: `internal/core/goal_test.go`

**Interfaces:**
- Produces: `core.KindGoal` events carrying `core.GoalOp{Op GoalOpKind, Condition, Reason string}`
  where `GoalOpKind ∈ {GoalSet, GoalCleared, GoalAchieved, GoalImpossible}`. Consumed by the daemon
  fold (Task 4) and the ui fold (Task 6).

- [ ] **Step 1: Write the failing decode test** against `testdata/stream/goal-achieved.jsonl` and
  `goal-cleared.jsonl`: the `Goal set: <cond>` synthetic line decodes to
  `KindGoal{Op:GoalSet, Condition:<cond>}`, the achieved entry to `GoalAchieved`, and `Goal
  cleared:` to `GoalCleared`. (Discriminate on the synthetic message text shape, not the tool name —
  the `askKind`/`toolChecklistOp` precedent.)
- [ ] **Step 2: Run it — expect FAIL** (`KindGoal` undefined).
- [ ] **Step 3: Implement** `core.GoalOp`/`KindGoal` + `goalOp(text)` recognizer + wire it into the
  message decode path. Keep the string matching in the airlock; expose only the typed op above it.
- [ ] **Step 4: Run — expect PASS**, and `TestDecodeRecordedFixtures` still green.
- [ ] **Step 5: Confirm `airlock_test.go` green** (no new airlock file; file set unchanged).
- [ ] **Step 6: Commit** `feat: decode the native /goal lifecycle in the airlock`

---

## Task 3: rpc — carry goal on the fleet report

**Files:**
- Modify: `internal/rpc/lifecycle.go` (`SessionStatus` gains `Goal *GoalStatus` or flat fields)
- Test: `internal/rpc/lifecycle_test.go`

**Interfaces:**
- Produces: `rpc.SessionStatus.Goal` (`{Condition string, Active bool}` — value fields, omitempty),
  the late-attach carrier. Consumed by daemon (Task 4) and ui report fold (Task 6). Mirror
  `Color`/`Commands`/PRs exactly (`json:"...,omitempty"`; additive, no frame-kind change).

- [ ] **Step 1: Write the round-trip/marshalling test** asserting a `SessionStatus` with a goal
  encodes/decodes with the condition + active flag, and that an empty goal omits the keys.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Add the fields** with the documented `omitempty` shape (no new `Frame*` constant, so
  `frameKinds`/distinctness tests are untouched).
- [ ] **Step 4: Run — PASS.**
- [ ] **Step 5: Commit** `feat: carry a session's goal on the fleet report`

---

## Task 4: daemon — fold goal onto the supervised agent

**Files:**
- Modify: `internal/daemon/agent.go` (an `a.goal` field; fold `KindGoal` in the observe path)
- Modify: `internal/daemon/roster.go` / wherever `SessionStatus` is built (populate `.Goal`)
- Test: `internal/daemon/goal_test.go`

**Interfaces:**
- Consumes: `core.KindGoal` from the agent's event stream.
- Produces: `rpc.SessionStatus.Goal` populated for every client, incl. one attaching *after* the
  goal was set (the late-attach reason the report exists — same as `Commands`/`Effort`).

- [ ] **Step 1: Failing test** — feed a fake agent the `goal-achieved` fixture events; assert
  `a.goal.condition` becomes the condition on `GoalSet` and clears on `GoalAchieved`/`GoalCleared`;
  assert the built `SessionStatus.Goal` reflects it.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Implement** the fold in the same place `agent.observe` folds `cwd`/model/commands;
  populate `SessionStatus.Goal` where `Color`/`Commands` are set.
- [ ] **Step 4: Run — PASS** (`-race` too).
- [ ] **Step 5: Commit** `feat: track a session's active goal in the daemon`

---

## Task 5: ui — fold goal onto `Agent`

**Files:**
- Modify: `internal/ui/fleet.go` (`Agent.Goal` field; fold `KindGoal` in `fold`; read report in
  `WithStatus`/`withFacts`)
- Test: `internal/ui/goal_test.go`

**Interfaces:**
- Consumes: live `core.KindGoal` events and `rpc.SessionStatus.Goal` from the report (both, the
  `Commands` pattern — live event for the watching client, report for a late attach).
- Produces: `Agent.Goal` (`{Condition string, Active bool}`, value-typed so `Agent` stays
  comparable). Consumed by every render task.

- [ ] **Step 1: Failing test** — a `KindGoal{GoalSet}` event sets `Agent.Goal.Active/Condition`; a
  `GoalAchieved`/`GoalCleared` clears it; a report carrying `Goal` sets it for a late attach.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Implement** both folds beside the `Commands` fold.
- [ ] **Step 4: Run — PASS.**
- [ ] **Step 5: Commit** `feat: fold a session's goal onto the ui Agent`

---

## Task 6: Render — sidebar/roster marker + activity line

**Files:**
- Modify: `internal/ui/roster.go` (`headLine` budget: name → `◆` → tokens; `rows` activity line)
- Test: `internal/ui/roster_test.go` (+ a screen test in `cmd/wake/` if layout-sensitive)

**Interfaces:**
- Consumes: `Agent.Goal`.
- Produces: `◆` head-line marker present whenever `Agent.Goal.Active` (persists while working — the
  owner's correction), and `◆ <condition>` on the indented activity line when the agent is idle and
  not showing a tool.

- [ ] **Step 1: Failing test** — a goal-active agent's `headLine` contains `◆` and, idle, its
  `rows` include `◆ <condition>`; a working agent keeps `◆` but the tool owns the activity line;
  under a narrow width the token count drops before `◆`.
- [ ] **Step 2: Run — FAIL.**
- [ ] **Step 3: Implement** the marker in `headLine` (budget order name → `◆` → tokens) and the
  activity line in `rows`.
- [ ] **Step 4: Run — PASS.**
- [ ] **Step 5: Commit** `feat: mark a goal in the roster`

---

## Task 7: Render — DM status-bar segment

**Files:**
- Modify: `internal/ui/statusbar.go` (`◆ <condition>` segment; extend `barKey`)
- Test: `internal/ui/statusbar_test.go`

**Interfaces:**
- Consumes: `Agent.Goal`. Produces: a `◆ <condition>` segment (truncates, drops-whole when narrow),
  cloned from `prSegment`/effort; `barKey` includes the goal so the cache invalidates on change.

- [ ] **Step 1: Failing test** — the bar for a goal-active agent contains `◆ <condition>`
  (truncated at width); no segment when inactive; `barKey` differs when the goal changes.
- [ ] **Step 2: Run — FAIL.** → **Step 3: Implement** beside `prSegment`. → **Step 4: PASS.**
- [ ] **Step 5: Commit** `feat: show the active goal in the conversation status bar`

---

## Task 8: Render — room bar (addressed agent)

**Files:**
- Modify: `internal/ui/send.go` (`withRoomBar`) / `internal/ui/chat.go` (`Room.bar`)
- Test: `internal/ui/chat_test.go` / `banner_test.go` (keep the room *banner* fact-free)

**Interfaces:**
- Consumes: `Agent.Goal` for the addressed agent. Produces: `◆ <condition>` in the room info bar
  for the addressed agent (a lone `@name`, else the manager), nothing for an empty room.

- [ ] **Step 1: Failing test** — addressing `@iris` (goal-active) renders `◆ <condition>` in the
  room bar; empty room renders none; the banner stays fact-free.
- [ ] **Step 2: FAIL → Step 3: Implement → Step 4: PASS.**
- [ ] **Step 5: Commit** `feat: show the addressed agent's goal in the room bar`

---

## Task 9: Render — board

**Files:**
- Modify: `internal/ui/board.go` (one-row-per-agent) and `internal/ui/boardtile.go` (tile status)
- Test: `internal/ui/board_test.go`

**Interfaces:**
- Consumes: `Agent.Goal`. Produces: `◆ <condition>` inline on the board row / tile status line
  where width allows.

- [ ] **Step 1: Failing test** — a goal-active agent's board row includes `◆ <condition>`.
- [ ] **Step 2: FAIL → Step 3: Implement → Step 4: PASS.**
- [ ] **Step 5: Commit** `feat: show goals on the board`

---

## Task 10: Trigger routing — `@who /goal` and passthrough

**Files:**
- Modify: `internal/ui/slash.go` (`roomTargetCommands` gains `goal`; confirm `/goal`/`/goal clear`
  are passthrough, not claimed by Wake)
- Test: `internal/ui/slash_test.go` / `slashguard_test.go`

**Interfaces:**
- Produces: bare `/goal …`/`/goal clear` reaching the focused agent unchanged; `@who /goal …` from
  the room routed to that one agent via the `mentionCommand` bridge (like `@who /color`), never
  broadcast.

- [ ] **Step 1: Failing test** — `@iris /goal ship X` routes `MentionDirect` to iris with the
  command intact; bare `/goal ship X` in a DM passes through to the focused agent; Wake claims
  neither `/goal` nor `/goal clear` (`slashguard_test.go`).
- [ ] **Step 2: FAIL → Step 3: Implement (add `goal` to the target-command set) → Step 4: PASS.**
- [ ] **Step 5: Commit** `feat: route @who /goal to the addressed agent`

---

## Task 11: Park / resume — goal survives a wake

**Files:**
- Test: `internal/ui/goal_test.go` / `internal/daemon/goal_test.go` (no `parkedRecord` change)

**Interfaces:**
- Consumes: the resumed session's frames. Produces: `Agent.Goal` re-derived on wake — **no
  `parkedRecord` field**, because `/goal` is restored by `--resume` and re-surfaced on the wire.

- [ ] **Step 1: Failing test** — after a park+wake, a session that had an active goal shows it again
  once its post-resume frames arrive (using the resume fixture from Task 1, or, if resume emits no
  goal signal, asserting the badge clears conservatively and documenting that in the spec's §6).
- [ ] **Step 2: FAIL → Step 3: Implement/adjust the fold → Step 4: PASS.**
- [ ] **Step 5: Commit** `test: a goal survives a park and wake`

---

## Task 12: Docs, guards, and the two review passes

- [ ] **Step 1:** Update `CLAUDE.md` — the "What it does today" `/goal` row, the Key-locations rows
  for the new files (`internal/core/goal.go` if split out, `internal/ui/goal*.go`), and any derived
  guards (`TestCLAUDEmdNamesTheTwoLargestNonTestFiles` if a file grew). Keep the spec + this plan in
  the PR.
- [ ] **Step 2:** `make ci` — read the exit code.
- [ ] **Step 3:** Code review (the `code-reviewer` agent / a correctness+non-negotiables read).
- [ ] **Step 4:** Adversarial review — try to break the strongest claims (the airlock discriminates
  goal lines from ordinary prose; the fold clears on achieve/clear; `◆` budget drops tokens first).
- [ ] **Step 5:** Open the PR into `main` with both review findings + the `make ci` exit code in the
  body. No attribution.

---

## Self-review (against the spec)

- **Coverage:** spec §4 trigger → Task 10; §5 monitor → Tasks 2/4/5; §6 park → Task 11; §7 render
  → Tasks 6–9; §3 wire facts → Task 1 fixtures. ✓
- **Deferred to the loop plan (not this PR):** all `/loop` work, `Agent.Loop`, `CronCreate`/
  `ScheduleWakeup` decode. The catalog's `↻` renderings land with loop.
- **Known fixture gap:** whether a resumed goal re-emits a signal (Task 1 note / Task 11) — resolved
  by the capture, not guessed.
- **Type consistency:** `core.GoalOp`/`KindGoal` (Task 2) → `rpc.SessionStatus.Goal` (Task 3) →
  `Agent.Goal{Condition,Active}` (Task 5) → read unchanged by Tasks 6–9. Names align.
