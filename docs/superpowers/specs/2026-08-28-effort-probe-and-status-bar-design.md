# Effort probe + status bar restructure — design

**Date:** 2026-08-28
**Branch:** `feat/effort-probe-status-bar`
**Status:** proposed — awaiting owner review before implementation

## Problem

An operator running a fleet cannot see, at a glance, **what model, how much context, and what
effort** an agent is running at:

- The **room** (the primary surface) draws no status bar at all — only transcript, working line,
  menu, and composer+legend. An operator who lives in the room sees none of it.
- The **conversation** status bar shows `path · branch · model · ctx:% · permissions`, but **drops
  model/context when unknown** and **omits effort entirely** — a documented decision
  (`internal/ui/statusbar.go` header) resting on *"effort is on no frame Wake receives at all"*.
- The current footer draws the **legend above the status bar**; the operator asked for the
  **info row on top, helpers below** (matching Claude Code).

The effort omission is the load-bearing one. It was *correct until 2026-08-13*: the bare `/model`
reply (`testdata/stream/bare-model.jsonl`) carries `Current model: Opus 5 (1M context) (effort:
xhigh)`, and the recording proves that costs `num_turns: 0`, `total_cost_usd: 0`, `duration_ms: 11`
— **the CLI answers it locally with no model inference**. So effort *is* readable, by asking. This
is recorded in `docs/notes/deferred.md` (the "a session's effort is readable after all" entry) as a
deliberately-deferred feature. This spec builds it.

## Goals

1. **Room + conversations show an info bar**: `~/dir · branch · Model · ctx:% · effort · permissions`.
   - Conversation: its own agent.
   - Room: the agent named by the composer's `@name` target, else the **manager**.
2. **Effort is real**: the daemon confirms the level by probing bare `/model`, invisibly.
3. **Info row above the helpers (legend) row.**

## Non-goals

- No config subsystem / no `~/.wake/config` / no toggle. The new layout is the fixed default
  (owner's call, 2026-08-28). Wake stays opinionated.
- The probe confirms **effort only**. Model is already reliable from `init`; re-reading it adds no
  information.
- No manager-driven effort/model setting (still refused — `cmd/wake/mcpguard_test.go`).
- Confirmed effort is **not** persisted across a park — it is re-derived by a fresh probe after
  unpark, so `parkedRecord` is unchanged.

---

## Half 1 — the visible layout

### 1a. Footer order: info above legend

The legend lives inside `Composer.View` (`internal/ui/composer.go:327`, rendered last). The DM
appends its cached status bar `d.bar` **after** the composer (`internal/ui/dm.go:577-579`), so today
the order is box → target → legend → bar.

**Change:** give `Composer` a `bar string` field and a `WithBar` setter, rendered **between the
target line and the legend**:

```
box → [target] → [info bar] → legend
```

- `internal/ui/composer.go` — new field `bar`, method `WithBar(string) Composer`, and one line in
  `View` inserting `c.bar` (styled, width-clipped) before the hint. The composer never *computes* the
  bar (it reads the filesystem) — it only places a pre-rendered string, the way it already places a
  pre-rendered `target`.
- `internal/ui/dm.go` — stop appending `d.bar` after the composer; instead
  `composer.WithBar(d.bar)`. `d.withBar` (dm.go:235) and `barKey` (dm.go:217) are unchanged — the
  string is still cached and only recomputed on change. Height accounting moves with it: because
  `baseChrome`/`chromeHeight` measure `composer.View`'s rendered height, the bar row is now counted
  automatically; remove any separate `+1` for the appended bar (verify `dm.go` chrome math around
  dm.go:749-762 and `chromeHeight`).

**Net:** same number of rows, reordered. No new row is spent.

### 1b. The room info bar

The room draws its composer at `internal/ui/chat.go:574`. It already knows the narrowed agent via
`r.focus` / `r.focusName` (set from a lone `@name` — `internal/ui/roomfocus.go`, `send.go`'s
`retarget`).

**Change:** the room gains a cached bar exactly like the DM's:

- `internal/ui/chat.go` — a `bar string` + `barFrom barKey` on `Room`, and a
  `Room.withBar(agent Agent, mode string, width int) Room` mirroring `DM.withBar`. `View` passes it
  via `composer.WithBar(r.bar)`.
- `internal/ui/app.go` — when the App refreshes the room, it resolves the **info-bar agent**:
  `r.focus` if set, else `Fleet.manager()` (`internal/ui/service.go`). It folds that agent's record
  and mode and calls `room.withBar(agent, mode, width)`. When neither exists (empty room, no
  manager), the bar is `""` and the room draws none — `statusBar` already returns `""` when nothing
  is known.

**The info bar is not the banner, and the distinction is load-bearing.** The room *banner* (the top
ASCII header) must stay fact-free — `TestTheRoomBannerCarriesNoSessionFacts`
(`internal/ui/banner_test.go:98`) forbids `effort`, `context`, `Opus`, `Sonnet` there, because a
fact in the banner would "change under a cursor that moves for unrelated reasons." This spec touches
only the info bar under the composer, leaving the banner untouched, and the bar follows the
composer's `@`-target (`r.focus`), **never the roster cursor (`↑↓`)** — so it changes only when the
operator changes who they are addressing, the deliberate act the banner rationale rules out for the
*cursor*. That test stays green.

The room's mode segment is the *info-bar agent's* mode, consistent with the DM. This does **not**
add a key or change routing — it is a read of state the room already tracks.

### 1c. The effort segment

`internal/ui/statusbar.go` — add effort to `statusBar(a Agent, mode string, width int)`:

- Segment value: `a.Effort` (which now carries confirmed-or-asked-for from the report — see Half 2),
  formatted `effort:<level>` (the `ctx:` prefix convention, so it is unambiguous beside the model
  name). Empty when `a.Effort == ""` (probe pending/failed and nothing asked-for) — dropped, never
  guessed.
- Segment order (visual, left→right): `path · branch · model · ctx:% · effort`, then `permissions`
  appended specially (unchanged mechanism). Truncation still drops rightmost-first; effort is short
  (`xhigh` = 5 chars) so it rarely needs dropping.
- **Rewrite the file header.** The long "effort is deliberately missing" essay is now false; replace
  it with: effort is confirmed by the daemon's `/model` probe and carried on the report; the segment
  shows the confirmed level, or the asked-for one until the probe returns, or nothing when neither
  exists.

---

## Half 2 — the effort probe

### Data flow

```
startup (init seen) ──┐
/effort <level> sent ─┴─▶ agent.probeEffort()  [daemon]
                              │ enqueues pending{probe:true, Text:"/model"} on a.in
                              ▼
                        serveInput → apply → sess.Send("/model")   [sets a.probing=true]
                              ▼
                        CLI answers locally (num_turns:0, $0, ~11ms)
                              ▼
                        fanOut reads the reply event  [spawn.go:672]
                              │ a.probing && core.IsModelReply(ev)?
                              ▼  yes
                        extract core.EffortFromModelReply → a.confirmedEffort
                        SUPPRESS the reply event + its result (no observe, no broadcast)
                        broadcast ONE statusPush carrying the new effort
                              ▼
                        report → rpc.SessionStatus.Effort → ui.Agent.Effort → status bar
```

### 2a. Daemon-originated send (new)

Today every stdin send is a `pending{from *client, frame}` (`internal/daemon/apply.go:25`) — there
is no internal-origin concept. Add one:

- `internal/daemon/apply.go` — `pending` gains `probe bool`. In `apply` (apply.go:69) the `FrameSend`
  arm: when `p.probe`, send the text but **skip `noteSent` and `noteEffort`** (a probe is not an
  operator turn), and set `a.probing = true` (under `a.mu`) **before** `sess.Send`; clear it if Send
  errors. Errors on a probe go nowhere (`p.from` is nil) — a probe that fails to send is a no-op,
  logged, never surfaced.
- `internal/daemon/effort.go` (or a new small `probe.go`) — `func (a *agent) probeEffort()` builds
  the probe `pending` and does the non-blocking enqueue onto `a.in` (the same channel `submit` uses).
  Best-effort: if `a.in` is full or the agent is not running/idle, skip (effort simply won't refresh
  this cycle). **Refused for parked/blocked/ended agents** — stdin is closed for a blocked ask, and a
  probe there is indistinguishable from an operator answer (the same hazard `⌃C` refuses a blocked
  agent for).

### 2b. Triggers

- **Startup:** in `fanOut` (spawn.go:664), when the `init`/first-idle event for the session is
  observed and `!a.probed`, call `a.probeEffort()` and set `a.probed = true`. fanOut is a separate
  goroutine from serveInput, so this is an ordinary cross-goroutine enqueue.
- **After `/effort`:** in `apply` after `noteEffort` (apply.go:75) reports the level *changed*, call
  `a.probeEffort()` to confirm the new level. (Enqueue onto `a.in` from the serveInput goroutine is a
  non-blocking send to a buffered channel — order preserved, probe runs after the `/effort` it
  confirms.)

`a.probed` resets on nothing — a `/effort` change re-probes explicitly rather than via the flag, so
one boolean is enough.

### 2c. Suppression + parse (the invisibility)

The single fan-out choke is `fanOut` at **spawn.go:672** (drop here → reaches no client). The
recognizer and parser live in the airlock (`internal/core/vocabulary.go`), beside the existing
rendered-string recognizers (`interruptedMarker`, `interruptNotice`, `stripLocalCommandStdout`):

- `core.IsModelReply(text string) bool` — text begins with `Current model:` and contains an
  `(effort: …)` clause.
- `core.EffortFromModelReply(text string) (level string, ok bool)` — extracts the clause, validated
  against `core.EffortCommands` (the seven `/effort` levels). Asserted against
  `testdata/stream/bare-model.jsonl`.

In `fanOut`, **before** `a.observe(ev)` and the broadcast:

```
if a.isProbing() && core.IsModelReply(ev.Text) {
    if lvl, ok := core.EffortFromModelReply(ev.Text); ok { a.setConfirmedEffort(lvl) }
    a.armSwallowResult()   // eat the following result frame too
    a.clearProbing()
    s.broadcast(s.statusPush())   // publish the new effort, but NOT the event
    continue                       // no observe, no event broadcast — state untouched
}
if a.swallowResult() && ev.isResult { a.clearSwallow(); continue }  // eat probe's result
```

Keying on the **assistant text shape while probing** (not on `num_turns`/`<synthetic>`, which the
airlock does not decode) is robust to a real turn's frames interleaving: a real turn's result passes
through because `swallowResult` is only armed once the probe's own reply is recognized. A model
coincidentally emitting `Current model: … (effort: …)` inside the probe window is the only false
match — vanishingly unlikely, scoped to the window, documented.

Because the probe frames never reach `observe`, they never move the agent's working/idle state or
token accounting, so the paired `statusPush` on `a.changed()` (spawn.go:685) never fires *for the
probe* — the one push we send is the deliberate one carrying the new effort. (The fixture shows no
`working` frame for a bare `/model`; if one is ever observed in practice, the probing window already
covers it.)

### 2d. Confirmed vs asked-for

- `internal/daemon/agent.go` — new field `confirmedEffort string` beside `effort` (agent.go:128),
  written under `a.mu`.
- `snapshot()` (agent.go:669) publishes **confirmed-or-asked-for** for display:
  `Effort: cmp.Or(a.confirmedEffort, a.effort)`.
- The **park/relaunch** path is unchanged: `park.go:446` keeps `argvEffort(a.currentEffort(), …)` —
  relaunch must use what the operator *asked for* (a level `--effort` accepts), not a probed value
  like `auto`/`ultracode`. Display and relaunch are two consumers with two correct answers.

`rpc.SessionStatus.Effort` (lifecycle.go:357) and `ui.Agent.Effort` (fleet.go:109) are unchanged in
shape — they now simply carry the better value.

### 2e. Disk / history filter

A Wake probe leaves a `user` line (`/model`) and an `assistant` line (`Current model: …`) in
Claude's on-disk JSONL. Filter them from replay in `liveHistory`
(`internal/daemon/history.go:147-155`), which both the DM and the room read through (`History` →
`answerHistory`):

- When a kept event is `KindUserText` with trimmed text `== "/model"`, drop it and arm a one-event
  lookahead.
- While armed, drop the next kept `KindAssistantText` whose text satisfies `core.IsModelReply`; clear
  the arm on any other event.

Safe because an operator's bare `/model` is intercepted by Wake and never sent to a session
(`internal/ui/slash.go:375` `bareOnlyCommands`, `configure` slash.go:446) — so any `/model` on disk
is provably a probe. The *shape recognizer* stays in core; the *drop decision* is Wake policy in the
daemon.

---

## Testing

TDD, failing test first, per layer:

| Layer | Test |
|---|---|
| `core` (airlock) | `IsModelReply` / `EffortFromModelReply` table tests + assertion against `testdata/stream/bare-model.jsonl`. Airlock guard still green (functions are in `vocabulary.go`). |
| `daemon` probe | Fake session replaying init → `/model` reply → result: assert `confirmedEffort` set, reply + result **not** broadcast to a client, exactly one effort-carrying `statusPush`, agent state unmoved. Probe refused for parked/blocked. Re-probe fires after `/effort`. |
| `daemon` history | `liveHistory` over a fixture containing a `/model` + `Current model:` pair: assert both dropped, surrounding turns kept. |
| `ui` statusbar | effort segment present when known, dropped when `""`, truncation order, `effort:` format. |
| `ui` composer/dm | bar renders **above** the legend; chrome height unchanged; room bar follows focus-or-manager. |
| screen (`cmd/wake/*_unix_test.go`) | room and DM footers: info row above helpers row; room shows the addressed/manager agent's bar. |

## Docs & guards to update (in the same change)

- `internal/ui/statusbar.go` header — rewrite the effort essay.
- `CLAUDE.md` — the status-bar table row; the room-line rows (the room now draws an info bar); the
  **"Effort is the one thing Wake sets and cannot confirm"** load-bearing rule (now false — the probe
  confirms it); any legend/width numbers derived by `TestCLAUDEmdDescribesTheLegendItDraws` (the
  legend entries are unchanged, but re-run the guard).
- `docs/notes/deferred.md` — mark the "a session's effort is readable after all" entry **built**,
  with the mechanism.
- `docs/notes/decisions.md` — a short ruling: the daemon may issue an invisible probe; where the
  answer lands; why the parse is in the airlock.
- Key-locations table in `CLAUDE.md` — the new probe seam (`internal/daemon/probe.go` or the
  `effort.go` additions), the confirmed-effort field, the history filter.

## Non-negotiables respected

- **UI never touches a process** — the probe is entirely daemon-side. ✓
- **Only the four airlock files know Claude's JSON** — recognizer/parser in `vocabulary.go`. ✓
- **Cheap to leave open** — probe fires once per session + once per `/effort`, never on a timer. ✓
- **Wake owns almost no state** — `confirmedEffort` is transient, re-derived after restart/unpark. ✓

## Open risks

1. **Probe interleaving with a busy agent.** After `/effort` on a working agent, the probe queues
   behind the current turn; effort confirms a beat later. Acceptable — the asked-for value already
   shows immediately.
2. **A coincidental `Current model:` assistant line** inside the probe window is mis-suppressed.
   Vanishingly unlikely; documented; scoped to the window.
3. **Whether a bare `/model` ever emits a `working` frame** is unverified beyond the fixture; the
   probing window covers it if it does. Flag for `docs/live-testing.md`.
