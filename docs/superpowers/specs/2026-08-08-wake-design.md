# Wake — Design

**Date:** 2026-08-08
**Status:** Approved for planning
**Repo:** `github.com/DilanDoshi/wake`

---

## 1. Premise

Wake is a terminal application for developers who run many Claude Code sessions at once.

Today, running 15–30 sessions means 15–30 terminal panes, hand-tiled, each one a separate
context you have to visit to learn anything. The cost is not compute — it is attention. You
cannot tell at a glance which two of thirty agents need you, and you cannot address the fleet
as a group.

Wake makes the fleet a **room**. A group chat is the primary surface: you talk to a manager,
`@name` an individual, or broadcast to everyone. Agents stay quiet unless they are blocked or
have something to report. A roster ranks every agent by whether it needs you. Any agent can be
opened as a full 1:1 conversation that is indistinguishable from Claude Code itself.

**Wake is Claude Code plus a room.** Not a replacement — a superset. Close the sidebars, open a
DM, and you are in Claude Code. Open the room and you are running a fleet.

### Why now

Every primitive Wake needs already exists in the Claude Code CLI (verified against v2.1.226):

| Need | Mechanism |
|---|---|
| Programmatic session control | `--print --input-format stream-json --output-format stream-json` |
| Stable identity Wake assigns | `--session-id <uuid>` |
| Display name | `--name <name>` |
| Permission modes | `--permission-mode manual\|auto\|acceptEdits\|plan\|dontAsk\|bypassPermissions` |
| Agent-initiated messages | `--brief` (enables `SendUserMessage`) |
| Lifecycle visibility | `--include-hook-events`, `--include-partial-messages`, `--replay-user-messages` |
| Subagent visibility | `--forward-subagent-text` |
| Isolation | `--worktree [name]` |
| Spend control | `--max-budget-usd` |
| Lossless park/resume | `--resume <uuid>` |

Wake never screen-scrapes. Every piece of state is read from structured JSON.

---

## 2. Non-goals

Naming these prevents the project from dying at 40% done.

- **Wake is not a terminal emulator.** It does not implement PTY handling or VT100 parsing.
- **Wake is not a multiplexer.** No browser panes, no SSH workspaces, no cloud VMs, no
  arbitrary shells. The host terminal (cmux, Ghostty, iTerm, tmux) keeps that job.
- **Wake is not model-agnostic in v1.** Claude Code only. One interface keeps the door open.
- **Wake is not a native GUI in v1.** See §14 for how that stays reachable.

---

## 3. Stack

| Layer | Choice | Why |
|---|---|---|
| Language | **Go** | N agent streams = N goroutines; single static binary |
| TUI | **Bubble Tea** | 44k stars, MIT, the de-facto standard |
| Styling | **Lipgloss** | rounded borders, adaptive color — the Claude Code look |
| Widgets | **Bubbles** | textarea, viewport, list |
| Markdown | **Glamour** | terminal markdown with syntax highlighting, free |
| Subprocess | `os/exec` | no PTY needed; stream-json is line-oriented |

Precedent: Charm's Crush is a coding-agent TUI on this exact stack.

Platform: **macOS first.** Go cross-compiles to Linux with no porting work when wanted.

---

## 4. Architecture

Wake is **one binary, two processes**. `wake` starts the daemon if it is not running, then
attaches as a TUI client over a unix socket.

```
                    ┌──────────────────────────────────┐
  wake-core ────────┤ unix socket · JSON-RPC           ├──→ wake TUI (Go, Bubble Tea)
   (daemon)         └──────────────────────────────────┘ └─→ Wake.app (SwiftUI)   later
      │
      ├─ session supervision (N × claude stream-json)
      ├─ pool: park / wake / resume
      ├─ attention derivation
      ├─ routing: @name · manager · broadcast
      ├─ roster persistence
      └─ MCP server for the manager
```

The daemon earns its place on its own merits: **closing the TUI must not kill the fleet.** Shut
the laptop, reattach later, agents kept working. It also means a future native frontend is a
new client against a proven core, not a rewrite.

### Packages

```
cmd/wake/                entrypoint; starts or attaches to the daemon
internal/core/
  protocol.go            ← the ONLY file that knows Claude's JSON
  session.go             one agent: spawn · send · events · park · wake
  pool.go                live-cap scheduler
  registry.go            roster on disk
  attention.go           events → needs-you | working | idle | parked | error
  router.go              @name · manager · broadcast
  mcp.go                 manager-facing tool server
internal/rpc/            daemon ↔ client transport
internal/ui/
  app.go                 bubbletea root model
  chat.go  dm.go  board.go
  groups.go  roster.go  composer.go
  layout.go              the bounded grid
  theme.go               Claude palette
internal/render/         markdown · diffs · tool blocks · task lists
internal/cmuxbridge/     optional status pills
```

### Boundaries that matter

- **`protocol.go` is the airlock.** Claude-specific JSON lives in exactly one file. Everything
  above it sees Wake's own `Event` type. This is the entire cost of staying Codex-ready — one
  file, no speculative abstraction.
- **`attention.go` is a pure function.** Events in, ranked state out, no processes. The hardest
  logic in the app is unit-testable without spawning anything.
- **The UI never touches a process.** It receives messages and renders.

### Wake owns almost no state

Claude persists every transcript to `~/.claude/projects/<cwd>/<uuid>.jsonl`. Wake stores only
the roster, group membership, and layout. If Wake crashes, nothing is lost — relaunch and 20
sessions resume by UUID. **Protect this property.**

---

## 5. Session model

An agent is a `claude` process in streaming JSON mode with an identity Wake assigns:

```
claude --print --verbose
       --input-format stream-json --output-format stream-json
       --session-id <uuid-wake-generated>
       --name <sydney>
       --permission-mode auto
       --permission-prompt-tool stdio
       --brief
       --include-hook-events --include-partial-messages
       --forward-subagent-text
       [--worktree <name>] [--model …] [--effort …] [--max-budget-usd …]
```

Two of those flags were added after the Task 1 spike and are not optional:

- **`--verbose`** — without it, stream-json mode exits 1 with an error on stderr. The command
  as originally specced did not run at all.
- **`--permission-prompt-tool stdio`** — undocumented, absent from `claude --help`. Without it
  every permission ask is **auto-denied** and reported after the fact, which makes §6's
  highest-priority `needs-you` trigger unobservable. The request arrives as a `control_request`
  that blocks until answered on stdin and carries **no `session_id`**; correlation is by
  `request_id`.

### Session identity is not immutable

`/clear` **mints a new session id mid-process.** The new id appears on a `system/hook_started`
frame *before* the `system/init` that announces it. This invalidates three things at once: the
roster key, the `--resume` target, and the `~/.claude/projects/<cwd>/<uuid>.jsonl` transcript
path §4 relies on.

Wake must therefore **track `session_id` from the frame stream and re-key the registry whenever
it changes**, rather than treating its assigned UUID as permanent. Wake still *originates*
identity; it does not *own* it for the session's lifetime.

Related: `init.permissionMode` is normalized, not an echo of the spawn flag — `manual` reports
as `"default"`. Track the mode Wake passed, never the mode `init` reports.

Agents get a random human name on creation (`sydney`, `alex`, `john`, …) plus an optional task
label, rendered as `sydney ▸ dev-5748`. Both are renameable.

### The pool

20 live sessions is roughly 16 GB resident (measured: ~800 MB per session). That is fine on a
128 GB machine and impossible on 16 GB, so the pool is not optional for anyone shipping this.

- **Live cap** (default 12) — agents holding a process.
- **Parked** — UUID on disk, process terminated, zero memory.
- Waking is `claude --resume <uuid>`, lossless, because Wake assigned the UUID.
- Park selection: oldest-idle background agents first. Never park an agent that is working,
  blocked, or visible.

### Ending things

Detaching without killing is the daemon's whole point, but it is only half a lifecycle. These
are four different intents and Wake needs a distinct verb for each — conflating any two of them
is how a fleet becomes unstoppable or a laptop becomes a graveyard of orphaned processes.

| Verb | Effect | Trigger |
|---|---|---|
| **park** | process terminated, UUID kept, resumable | automatic, pool-driven |
| **detach** | TUI exits, every session keeps working | ~~`⌃C`~~ **`⌃O`** — superseded |
| **stop** | this agent ends, leaves the roster | key in the roster or DM |
| **quit** | every session stops, daemon exits | `wake stop` |

> **Superseded 2026-08-10, shipped 2026-08-11** — see
> `docs/superpowers/specs/2026-08-10-lifecycle-and-restore.md`, which is binding over this table.
> Detach moved to **`⌃O`**; **`⌃C` parks the focused agent**, which this table has as automatic and
> pool-driven only; and **`⌃Q`** parks the whole fleet and then quits, which is a fifth trigger this
> table has no row for. The row above is left in place with its correction attached rather than
> rewritten, because a dated spec is a record — but a stale table is what the next reader builds on,
> which is why the correction is here and not only over there.

**Stop closes stdin and lets the in-flight turn finish.** It does not signal. An agent killed
mid-`Edit` leaves a half-written file, and Wake is not entitled to do that to someone's repo.
A hard kill exists for a wedged agent, behind a confirmation, and is never the default.

**`wake status`** reports whether a daemon is running and how many sessions it holds. A
background process the user cannot see or stop is a liability, not a feature — and at 20 agents
"is it still running?" is the first question anyone asks.

The daemon stops its sessions before exiting. If it dies without doing so, its children are
orphaned rather than reaped, so `wake status` must be able to detect and adopt a fleet whose
daemon is gone — the session UUIDs are on disk precisely so that is recoverable.

---

## 6. Attention model

At 20 agents this *is* the product. The question is never "how do I talk to them" — it is
"which two of these need me right now."

| State | Trigger |
|---|---|
| **needs-you** | permission request · plan ready · `AskUserQuestion` · agent-initiated message |
| **working** | turn in progress |
| **idle** | turn ended, nothing pending |
| **parked** | no live process |
| **error** | process died · API failure |

Two corrections from the Task 1 spike:

- **A denial is not an error.** Both denial paths — client `control_response` and auto-deny —
  end the turn with `subtype: "success"` and `is_error: false`, listing the tool in
  `result.permission_denials`. Deriving `error` from a denial would mark healthy agents broken.
  (The two paths differ: a client deny is `non_execution_kind: "permission-rule"`;
  `"user-rejected"` belongs to auto-deny.)
- **"Rate limited" is removed from `error` because it is unfalsified.** `rate_limit_event`
  fires **once per process** on the first API-hitting turn, and all nine recorded samples read
  `status: "allowed"`. Wake has never observed an exhausted window. Treat `rate_limit_info` as a
  timestamped spawn-time snapshot for a fleet-level indicator, not as a live attention trigger,
  until the exhausted case is recorded.

Roster sort: `needs-you` → `working` → `idle` → `parked`.

**Within `working`, sort by staleness descending.** The agent that has been on one `pytest`
call for twelve minutes floats above the one that started thirty seconds ago. Stuck agents
surface themselves.

`⌃⇧A` jumps to the next agent needing you, newest wait first, and opens its DM.

---

## 7. Routing

Three targets, with cost made visible because broadcast is N full turns.

| Action | Target | Cost |
|---|---|---|
| `↵` in group chat | manager (default) | 1 turn |
| `@alex …` | alex | 1 turn |
| `⌃↵` | everyone | N turns, count shown before it fires |
| `↵` in a DM | that agent, **locked** | 1 turn |

### The `@` rule

`@` is overloaded, deliberately — Slack and Discord already trained everyone.

- **Leading `@`** routes to an agent.
- **Inline `@`** opens the file picker, exactly as Claude Code does.

`@alex can you fix @src/auth.ts` does the obvious thing.

### Mention mode

What `@alex hey` means is configurable, because the two readings differ by 20× in cost.

| Mode | Behavior | Cost |
|---|---|---|
| `direct` (default) | only alex receives it | 1 turn |
| `open` | everyone receives it, alex is addressed | N turns |

Set via `routing.mention` in config; `⌃T` flips it for the session. The composer always shows
the current target and mode, so it is never a memory problem:

```
  → @alex · direct              ↵ send   ⌃T open     ⌃↵ all (20)
  → @alex · 19 also see this    ↵ send   ⌃T direct   ⌃↵ all (20)
```

---

## 8. Views

### Group chat — awareness, routing, triage

**Filtered by default.** Twenty agents emitting every tool call is a firehose nobody reads. The
group chat shows only: your messages, the manager, agent→you messages, blocking events, and
turn boundaries. Full transcripts live in DMs.

### DM — a full 1:1 session

**Unfiltered. This is literally Claude Code.** Everything cut from the group chat comes back:
thinking blocks, every `⏺` tool call, full diffs, the task-list widget, inline permissions,
`/commands`, the `@file` picker, Shift+Tab, complete scrollback.

This view is load-bearing. If deep 1:1 work is second-class, users go back to plain Claude Code
for real thinking and Wake becomes a dashboard. The DM must be somewhere you can comfortably
spend an hour.

### Board (`⌃B`) — one row per agent with current task and progress.

### Layout: a bounded grid

Columns, each optionally split once vertically. Not a pane tree.

```
┌────────┬──────────────────┬──────────────────┬───────┐
│ GROUPS │ #pufferfish      │ @alex            │AGENTS │
│▸puffer │ ⏺ john done      │ ⏺ Edit(auth.ts)  │●sydney│
│  ui-swp│ ⏺ mgr 19 working │   ⎿ +14 −3       │◐john  │
│─ DMs ──│ ╭──────────────╮ ├──────────────────┤◐marcus│
│ @mgr   │ │> who's stuck?│ │ @sydney        ✕ │       │
│▸@alex  │ ╰──────────────╯ │ ⚠ needs you      │       │
│▸@sydney│                  │ ╭──────────────╮ │       │
│        │                  │ │> allow it    │ │       │
└────────┴──────────────────┴──────────────────┴───────┘
```

Responsive: at ≥160 columns all four regions show; at 120–160 the groups sidebar collapses to
icons; below 100 both sidebars auto-hide. Widths persist to config.

An **awareness strip** appears in every view including solo, so you can disappear into a DM for
an hour without losing the fleet:

```
 #pufferfish · ● 2 need you · ◐ 17 working · ○ 3 idle          ⌃⇧A →
```

---

## 9. Claude Code feature mapping

| Feature | Group chat | DM |
|---|---|---|
| Task list | roll-up: `john ▸ 3/7 wiring auth middleware` | full checklist, as rendered by Claude Code |
| Tool calls `⏺` / `⎿` | blocking only | all |
| Thinking blocks | hidden | collapsible |
| Diffs | hidden | full +/− |
| Permission requests | inline, answerable | inline, answerable |
| Plan mode / `ExitPlanMode` | inline, answerable | inline, answerable |
| `AskUserQuestion` | inline, answerable | inline, answerable |
| Shift+Tab mode cycling | sets group default | sets that agent |
| Custom `/commands` | yes (`~/.claude/commands`) | yes |
| Compaction | one line | full notice |

**Known gap requiring a spike:** built-in slash commands (`/model`, `/clear`, `/compact`,
`/context`, `/resume`) are implemented inside the interactive CLI. Which of them survive
stream-json mode is unverified. This is the first thing to test — see §16.

---

## 10. Keybindings

`⌘` keys are consumed by the host terminal and do not reach TUI applications by default.
More importantly, **cmux already owns `⌘D`.** So Wake takes a clean division of labor:

> **`⌘` belongs to the host terminal.** `⌘D` splits cmux panes.
> **`⌃` belongs to Wake.** `⌃D` splits Wake columns.

Same letters, same mental model, zero collisions, and it works over ssh where `⌘` never arrives.

| Key | Action |
|---|---|
| `↵` | send to current target |
| `⌃↵` | broadcast (count shown) |
| `⇧⇥` | cycle manual → auto → plan |
| `⌃D` / `⌃⇧D` | split right / split down |
| `⌃W` | close focused conversation |
| `⌃\` | solo focused conversation |
| `⌃←` `⌃→` | move between conversations |
| `⌃N` | new agent (profile picker) |
| `⌃T` | toggle mention mode |
| `⌃⇧A` | jump to next agent needing you |
| `⌃B` | board overlay |
| `⌃K` | palette — jump to any agent |
| `⌃G` / `⌃R` | toggle groups / roster sidebar |
| `⌃Z` | undo |
| `⎋` | back to group chat |

All bindings remappable in config. `⌃⇧X` combinations require the Kitty keyboard protocol,
which Ghostty and cmux support; Bubble Tea detects this and Wake falls back to unshifted
alternates where unavailable.

Optionally, Wake can emit a Ghostty/cmux config snippet forwarding `⌘` chords for users who
want Mac-native bindings. Opt-in, never default.

### Mouse and undo

Bubble Tea provides full mouse support. Click a workspace to switch, a roster row to open its
DM, `✕` to close a column, `[+]` to spawn, Allow/Deny to answer. Drag borders to resize. Scroll
wheel scrolls.

Undo is application state, not a framework feature. `⌃Z` reverses closing a column, archiving
an agent, and renaming — all genuinely lossless because session UUIDs persist. An accidental
broadcast cannot be unsent, but `⌃Z` interrupts all N turns before they complete.

Known cost: while mouse tracking is on, terminal text selection requires holding Shift or
Option. This is inherent to TUIs and users notice it. A binding toggles mouse tracking off.

---

## 11. Profiles and configuration

Profiles replace the AppleScript window-tiling scripts this workflow currently uses. Each of
`/new-oscar`, `/new-victor`, `/new-sierra`, `/new-tango`, `/new-uniform`, `/new-romeo-testing`,
`/new-quebec` maps to one profile entry:

```toml
[[profile]]
name       = "backend"
dir        = "~/Documents/Project Pufferfish/delta-backend"
agent_name = "delta-backend"
effort     = "max"
# optional: model, worktree, max_budget_usd, suppress_hooks
```

`⌃N` opens the picker; `/new backend` works from the group chat.

**First-run generation.** Wake scans a directory for git repositories and offers to create a
group with one profile per repo. Going from install to a full fleet is one keystroke.

**Group = project, agents = repos.** This mapping falls out of the existing layout.

### Command profiles

A profile may run an arbitrary command instead of a Claude session:

```toml
[[profile]]
name    = "dev"
type    = "command"
dir     = "~/Documents/Project Pufferfish/pufferfish-web"
command = "npm run dev"
```

Streamed stdout with ANSI colors. **No PTY, no interactivity** — this is not a shell, and Wake
is explicitly not becoming one. Shells belong to the host terminal. This exists so a user can
add a long-running log column if they want one, the same way they added their own launch
scripts. Nothing is built in by default.

`!cmd` in any conversation runs a one-shot command with output inline, covering the quick cases
without any pane machinery.

### Inherited configuration

Wake-spawned sessions read the same `~/.claude/settings.json` as any session, so existing
permission allow-rules, hooks, and statusline carry over. Existing allow-rules directly reduce
attention-queue noise. A per-profile `suppress_hooks` switch avoids duplicating notifications
Wake already surfaces.

### Worktrees

`worktree = true` per profile, **default off.** Valuable when two agents share a repo;
unwelcome for users who do not work that way.

### Budgets

`max_budget_usd` per profile, **unset by default.** Subscription users on Max plans do not need
it; users on API billing running 20 max-effort sessions very much do.

---

## 12. The manager

The manager is a Claude session at the group's root directory — the same role `/new-tango`
already plays. It holds a permanent seat in every group.

Wake exposes an **MCP server** to it (`--mcp-config`) with `list_agents`, `agent_status`,
`roll_up`, `send_to_agent` and `interrupt`. This is what separates a manager that *describes* the
fleet from one that *operates* it: asking "who's stuck?" queries live state rather than a roster
pasted into its prompt four minutes ago, and "ask marcus what's blocking" actually sends.

Wake is the MCP server; the manager session is the client. Roughly 800 tokens of tool
definitions in the manager's context only.

> **Amended 2026-08-12** (Phase 2, after Tasks 14 and 15 shipped the surface). This list read
> `list_agents`, `agent_status`, **`spawn_agent`** and `send_to_agent`. Two changes, and the
> reasoning is Task 14's, adopted here because this document is the source of truth and leaving it
> contradicting the build is how the next reader picks the wrong one.
>
> **`spawn_agent` is removed.** The failure mode of a spawn tool is not one agent nobody asked for,
> it is thirty: a model calling tools in a loop with nobody watching, each call costing a name, a
> process and somebody's money, and **nothing in this build caps how many agents run at once**
> (spec §6's live-cap scheduler, `internal/core/pool.go`, is not built). The prerequisite is the
> cap, not the tool. `cmd/wake/mcpguard_test.go`'s `managerVerbs` holds that verdict per frame kind
> with the argument beside it, and names the missing cap as a `referent` something checks — so the
> day `pool.go` exists the verdict has to be re-argued rather than inherited. Whoever adds the tool
> owns the cap.
>
> **`roll_up` and `interrupt` are added.** `roll_up` is the whole fleet as one bounded digest, and
> it exists because the alternative — a manager that reads the room — costs ~70k of context and
> 1.4M tokens a day at 30 agents, and degrades exactly when it would be most useful, since
> compaction takes the oldest history and the collision worth noticing happened hours ago.
> `interrupt` is *"pause"* in the example that settled the manager's shape, and it is the one
> destructive-sounding verb that ends no session: the process keeps its id, takes the next message
> and resumes with the aborted turn's context intact.
>
> The rule the five share, and the one to apply to a sixth: **a verb is exposed only if an operator
> can undo it by looking at the room.** Every other verb the daemon serves is refused with its own
> recorded reason. What that list bounds is the **fleet** — see the note in §17 about what it does
> not bound.

---

## 13. cmux integration

**Optional. Nothing in the core depends on cmux being installed.**

When the cmux socket is present, Wake publishes agent status to cmux's sidebar pills via
`cmux set-status`. That is the whole v1 integration — roughly 30 lines, no new UI.

Deferred until earned: native notifications with inline Allow/Deny, and pop-out to a real
`claude --resume` in a cmux split.

Wake calls the cmux CLI and never copies its source. cmux is GPL-3.0-or-later; copying even one
file would make Wake GPL and permanently remove the license choice. Arm's-length CLI invocation
does not.

---

## 14. Theme

Claude Code's palette: near-black background, warm grey text, **orange accent** for active
state and attention, amber for warnings. Lipgloss adaptive colors handle light terminals.

Agent icons are 16 block-glyph sprites (`▞▚`, `▛▜`, `▙▟`, …) assigned deterministically by
name. Real pixel sprites are a later pass.

### Path to a native app

The daemon boundary means a SwiftUI frontend later is a *second client*, not a rewrite. The
core — process supervision, parking, attention, routing, roughly 55% of the work and the
subtle half — transfers completely. Views, keybindings, layout, and markdown rendering (~45%)
do not; SwiftUI means writing SwiftUI.

A thin `.app` wrapper hosting the TUI via SwiftTerm (MIT, actively maintained) is a cheaper
intermediate step that adds a Dock icon, native `⌘` keys, and native notifications without
changing the Go binary.

Neither is v1. The boundary that keeps both cheap costs about four days now.

---

## 15. Error handling

Every failure mode is non-fatal and visible:

| Failure | Behavior |
|---|---|
| Process dies | `error` state, one key to resume by UUID |
| Malformed JSON line | log and skip; never crash the render loop |
| Rate limited | back off, surface as attention state, do not spin |
| Wake crashes | roster on disk; sessions resume by UUID; nothing lost |
| Daemon unreachable | TUI shows a clear reconnect state and waits on the connection it already holds. **Not "retries with backoff"** — corrected 2026-08-09. A quitting daemon keeps its listener bound, so `Dial` succeeds against a daemon that will never answer, and backlog slots are consumed cumulatively and never returned. Measured on darwin: 128 slots, and closing a pending connection does **not** free its slot — so a 100 ms retry poll exhausts the backlog in ~12.8 s, after which `ECONNREFUSED` is indistinguishable from a stale socket and the next daemon SIGKILLs the live fleet. Wait for `FrameHello` or EOF on the one connection already held. |
| Profile dir missing | fail at spawn with the path, not a stack trace |

---

## 16. Testing

**80% coverage minimum**, TDD throughout, per project standards.

The load-bearing decision: **you cannot test reliably against a live LLM.** It is slow,
nondeterministic, and costs money every CI run. So the first thing built is a **fixture
recorder** — run real sessions once with `--output-format stream-json`, capture raw JSONL to
`testdata/`, replay forever. Every session that misbehaves in real use gets recorded and
becomes a regression test.

| Layer | Approach |
|---|---|
| `protocol` | golden files against recorded stream-json |
| `attention`, `router` | pure functions, table tests |
| `session`, `pool` | fake process implementing the same interface — park, wake, crash, resume |
| `rpc` | daemon/client contract tests |
| `ui` | `teatest` golden frames |
| integration | 3 real sessions, trivial prompts, full lifecycle asserted |
| **soak** | 20 fake sessions replaying fixtures for an hour |

The soak test is the one that matters at this scale. Nothing else catches a goroutine leak that
bites on hour three.

### Spike before implementation

One spike de-risks the entire project and should run first:

> Spawn two `claude` processes with Wake-generated `--session-id` UUIDs and `--name alex` /
> `--name sydney` in stream-json mode with `--brief` and `--include-hook-events`. Send `alex` a
> message. Print both streams merged with name prefixes. Then run `claude --resume <alex-uuid>`
> in another terminal and confirm it is the same conversation.

If this works, Wake is a rendering problem. It also answers the §9 open question about which
built-in slash commands survive stream-json mode. Roughly 80 lines, one day.

---

## 17. v1 boundary

**In:**

- Daemon core with detach/reattach
- Spawn, park, wake sessions with assigned UUIDs and names
- **Stop one agent; `wake stop` to quit the fleet; `wake status` to see it** — stop closes stdin
  and lets the turn finish, hard kill is behind a confirmation
- Group chat, filtered
- DM view at full Claude Code fidelity
- Bounded grid: columns, split-down, solo, board
- `@name` routing, manager default, broadcast with count, mention-mode toggle
- Attention-ranked roster with task roll-up
- Groups sidebar
- Inline permission / plan / question approval
- Shift+Tab mode cycling
- Markdown, diff, tool-call, task-list rendering
- Mouse throughout; undo for destructive actions
- Profiles, first-run generation, command profiles, `!cmd`
- Manager with MCP tools
- cmux status pills when present

> **Added 2026-08-12, and it is a gap rather than a feature.** The manager's MCP surface bounds
> what it can do **to the fleet** (§12's amendment). It is also a full Claude Code session in
> `auto`, so it holds `Bash`, `Write`, `Edit` and `Task` — nothing in the build passes
> `--allowed-tools`, and `--strict-mcp-config` bounds MCP *servers* rather than built-ins. So
> **nothing currently bounds what the manager can do to the machine**, and one of the things on
> that machine is `wake stop`. v1 is not done while that is true: the close is `--allowed-tools`,
> it needs a recording spike first, and it is the top item of Phase 4.

**Out:**

- Pixel sprites (block glyphs stand in)
- Native notifications, pop-out-to-split
- `.app` wrapper
- SwiftUI frontend
- Codex / model-agnostic support
- Arbitrary pane layouts, PTY shells, browser panes
- Search, export, history browsing

---

## 18. Risks

| Risk | Mitigation |
|---|---|
| Built-in `/commands` do not work in stream-json | Spike first (§16); reimplement the essential few |
| DM fidelity falls short and users return to plain Claude Code | Treat the DM as load-bearing, not a preview pane; measure it in dogfooding |
| 20 agents exhausts subscription rate limits | Pool cap, per-profile budgets, surface rate-limit state as attention |
| Daemon adds lifecycle complexity | Contract tests; clear reconnect UI; daemon is stateless beyond the roster |
| Crowded space (Claude Squad, agent-deck, dmux, agent teams) | Those are session managers. The group chat is the differentiator — keep it the product, not a feature |
| Scope creep toward becoming cmux | §2 non-goals are binding |
