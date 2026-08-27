# Phase 4 — scope draft

**Status: a draft for the owner to argue with, not a plan.** Written 2026-08-12 while Phase 2's
last two tasks were in flight, deliberately before a compaction, so that nothing about what Phase 4
*must* contain has to be reconstructed later.

Phase 4 is not "polish". It is **everything §17 of the design spec calls v1 that phases 1–3 did not
absorb** — and §17 is the boundary the whole project is disciplined against. `CLAUDE.md`'s scope
section says the failure mode here is drifting toward being a worse cmux; §17 is what that
discipline is measured against, so shipping "v1" without it is the same drift wearing a different
hat.

**Nothing in here is decided.** Where I have a recommendation I say so and say why. Several items
need a design conversation and are marked as such; I should not invent them overnight.

---

## 1. What is genuinely left from §17

Traced item by item against the tree. §17's "In" list, with what happened to each:

| §17 item | State |
|---|---|
| Daemon core with detach/reattach | **built** |
| Spawn, park, wake with assigned UUIDs and names | **built** (Phase 3) |
| Stop one agent; `wake stop`; `wake status` | **built** |
| Group chat, filtered | **built** |
| DM at full Claude Code fidelity | **built** |
| **Bounded grid: columns, split-down, solo, board** | **NOT BUILT** — only the two-pane split exists |
| `@name` routing, manager default, broadcast with count | **built** / manager in flight |
| ~~mention-mode toggle~~ | **BUILT** 2026-08-12 — `⌃T` flips direct/open, per §7's own subsection |
| Attention-ranked roster with task roll-up | **built** |
| Groups sidebar | **built** |
| Inline permission / plan / question approval | **built** |
| **Shift+Tab mode cycling** | **NOT BUILT** — deferred I7 |
| Markdown, diff, tool-call, task-list rendering | **built** |
| Mouse throughout | **built** |
| **Undo for destructive actions** | **NOT BUILT** |
| **Profiles, first-run generation, command profiles** | **NOT BUILT** — `!cmd` is built |
| Manager with MCP tools | in flight (Tasks 14–15) |
| **cmux status pills when present** | **NOT BUILT** |

So Phase 4's floor is **five items** (mention-mode shipped 2026-08-12). Two more belong with them and are not in §17 at all:

- **Creating and naming agents from inside Wake** — no `/new`, no `+`, no rename. It is the largest
  gap between the founding message and the build, it belongs to no phase, and `docs/goals.md` §3
  has the full argument. **Cheap now**: Task 6 built the slash layer specifically so `/new` and
  `/add-<name>` need no new machinery.
- **Session importing** — Phase 3's last piece. Adopting sessions somebody started elsewhere
  (scattered terminals, cmux). The recording spike already settled the hard half: a session that is
  still *open* cannot be adopted **and cannot be detected as open**, so the liveness check must be
  Wake's own and the flow must refuse rather than warn.

---

## 2. The six, with what is already known about each

### 2·0. Bound what the manager can do to the machine — **added 2026-08-12, and it comes first**

Added by Phase 2 Task 15's review (C1). It is not from §17 and it is not polish: it is a **gap this
project's own artefacts asserted did not exist**, which is the failure mode `CLAUDE.md`'s legend rule
is written against.

The manager is an ordinary `claude` session. Nothing passes `--allowed-tools`, and
`--strict-mcp-config` bounds MCP **servers** rather than built-ins, so it holds `Bash`, `Write`,
`Edit` and `Task` in `auto` — which this repository's own recordings show running `Bash` with zero
`can_use_tool` frames. So Task 14's escalation chain has a one-hop version that skips the fleet
entirely, with `wake stop` on the end of it. `managerVerbs` bounds the fleet and nothing bounds the
machine.

**The fix is `--allowed-tools` as a third element of the same one-literal append in
`internal/core/argv.go`**, keyed on the same emptiness test — so no value can express
tools-without-restriction, exactly as none can express `--mcp-config` without `--strict-mcp-config`.
It is **not** `manual`: MCP tools are ordinary tools to the permission system, so every
`list_agents` would raise a card and block on a human, which destroys the property the manager
exists for.

**A recording spike comes first** — what the flag accepts, whether MCP tools are nameable in it,
what `init.tools` reports back, what a refused call looks like. Unrecorded, all of it.

Task 15 shipped the honesty fix in the meantime (the prompt and `CLAUDE.md` now say the bound is
over the fleet and name what is outside it, held by a test). The full argument, the corpus evidence
and the ruling against `manual` are in `docs/notes/deferred.md`, which is where this item lives.

### 2a. Shift+Tab permission-mode cycling — do this first

**It is a founding requirement**, named by function in the owner's original message: *"shortcuts
like shift enter for plan/auto/manual mode"*. Every agent currently runs in `auto` and there is no
way to change it.

**It has a debt attached.** `⇧⇥` is currently bound to *next blocked agent*, because `⌃⇧A` turned
out unbindable. `docs/goals.md` §5 records that when I7 ships, `⇧⇥` goes back to the permission
mode and next-blocked moves. `⌃X` and `⌃Y` are the keys reserved for it.

**Known unknowns:** deferred I7 notes `permission_suggestions` on the `can_use_tool` request carries
`{"type":"setMode","mode":"acceptEdits","destination":"session"}` — unexplored. Whether a mode can
be changed mid-session at all is **unrecorded**, and this project does not design around unrecorded
behaviour. **A recording spike comes before the implementation.**

*Why first:* it is the only Phase 4 item that is both a founding requirement and currently a
**lie-shaped absence** — the app shows a mode it cannot set.

### 2b. Creating and naming agents in-app

`/new [name] [in <dir>]`, `/add-<name>`, a `+` on the roster, and renaming or re-labelling an
existing agent so `alex <> ui fixes` becomes reachable. Today the label is derived from the git
branch and cannot be set.

**Decided already:** the slash layer is a layer, and its guard is over *what Wake must not claim*
so the passthrough to Claude Code's own commands survives. Adding a command means a map entry and a
count bump. **But note** the Task 6 review's finding: `commands`' exact-word key **cannot route
`/add-<agent-name>`**, and the shapes that would route it are what the guard forbids. That tension
needs resolving before `/add-` is designed.

**Needs a decision:** does a rename change the routing handle, or only the display? A name is never
an address internally — but `@name` is how an operator addresses one, and renaming under an
in-flight `@` is a real hazard.

### 2c. The bounded grid — **the hard half is settled, 2026-08-12**

**Owner's ruling: "board" is an OVERVIEW, not panes.** One row per agent — what it is doing,
whether it needs you, its last line — with **no transcripts**. A dashboard you act *from*: jump to
one, answer a card, park a few.

That is the ruling that matters, and it settles the risk this item carried. "Board" was the word
most likely to pull Wake toward being a terminal multiplexer, which `CLAUDE.md` names as this
project's failure mode. **The answer is about attention, not about windows** — which is what the
whole product is about, and it is the only reading that works at 30 agents, where a tiled grid is
unreadable by arithmetic.

**Still open, and much smaller now:** what §17's *columns / split-down / solo* mean beside that.
The two-pane split with a draggable divider and the 120-column takeover already ship, so these are
variations on something that exists rather than a new subsystem. Whether more than one conversation
tiles at once — and what the cap is — can be answered after the overview is in use, because the
overview may remove the reason to want it.

**Substrate:** `internal/ui/layout.go` is a pure value — width and flags in, column counts out, no
drawing — which is why every responsive rule is a table test. `decisions.md` records that three
unreachable floor-clauses were written and deleted there, so **the breakpoints are the only floor**.

### 2c-old. The bounded grid — needs a design conversation

§17 says *columns, split-down, solo, board*. Today there is one split with a draggable divider and
a 120-column takeover.

**This is the largest item and the least specified.** `internal/ui/layout.go` is a pure value —
width and flags in, column counts out — which is the right substrate, and `docs/notes/decisions.md`
records that three unreachable floor-clauses were written and deleted there, so the breakpoints are
the only floor.

**I should not design this alone.** What "board" means, whether panes tile or stack, and what
happens at 30 agents are product questions.

### 2d. Undo for destructive actions

§17 pairs this with the mouse. The destructive actions now are: `wake stop`, `⌃C` park (already
recoverable), `⌃Q` (recoverable), `⎋` interrupt (destroys an outstanding ask and a billed turn), and
a deny on a permission card.

**Phase 3 changed this item's shape.** Park made the two big ones reversible, so what is left is
narrower than when §17 was written: mainly interrupt and deny. **Worth re-scoping rather than
implementing as written.**

### 2e. Profiles — **CUT, owner's ruling 2026-08-20**

Skipped outright: the flag-bundle reading solves a problem this workflow does not have — spawning
happens through `/new` inside the room, not through flag-laden shell invocations. The one real gap
that conversation surfaced is filed in `deferred.md` (2026-08-20): `/new` cannot set a spend
ceiling, and the fix is two flags on `takeNewFlags`' table, not a profile. The 2026-08-12 ruling
below is kept as the record of what was considered.

### 2e-superseded. Profiles — settled 2026-08-12

**Owner's ruling: a profile is a name for a set of spawn settings** — directory, permission mode,
model, allowed tools — so `wake new --profile backend` replaces four flags.

Small, obvious, and it **composes with the manager work**: `--allowed-tools` is about to become a
real setting (§2·0), and a profile is where it belongs rather than as another flag nobody passes.
First-run generation then has an obvious job: write one starter profile.

**Still open:** what a *command* profile is. §17 lists it separately and nobody has defined it.
Do not invent one — ask.

### 2e-old. Profiles, first-run generation, command profiles

Never discussed in any depth. First-run generation is the one with a real argument behind it: the
first `wake` on a fresh machine currently spawns an agent in `$PWD` with `auto` permissions and no
configuration, which is a reasonable default and not a considered one.

**Needs a conversation.** What a profile *is* has never been settled.

### 2f. cmux status pills — **CUT, owner's ruling 2026-08-20**

*"no cmux pills, not necessary."* The item below is kept as the record of what was cut and why it
was already last in line; `internal/cmuxbridge/` will not be built, and its row is gone from
`CLAUDE.md`'s key-locations table.

### 2f-old. cmux status pills

Explicitly optional in §17 — *"when present"*. The binding constraint is a **licence** one and it is
non-negotiable: `CLAUDE.md` says never copy cmux source, reach it only through its CLI (`cmux
events`, `cmux set-status`), because cmux is GPL-3.0-or-later and copying one file makes Wake GPL
forever.

**Lowest value of the six.** Recommend last, or cut.

---

## 3. What Phase 4 must NOT do

- **Not a terminal emulator.** No PTY, no VT100, no arbitrary shells, no browser panes. The grid
  item (2c) is where this pressure will come from, and "board" is the word most likely to be read
  as "panes running things". It is not.
- **Not a second implementation of anything.** The layout value, the slash layer, the legend
  bijection and the argv grammar all exist and all have guards. Extend in place.
- **Not cheap to leave open, broken.** Every Phase 4 item adds per-frame or per-agent cost. At 30
  agents a per-agent cost on a ticker multiplies by 30, and Task 16's measurement is the baseline
  this must be held against.

---

## 4. Recommended order, and why

0. **Bound the manager's tools** (2·0) — added 2026-08-12. Not from §17; it closes a gap two of this
   project's own artefacts asserted did not exist, and it shares 2a's spike shape.
   **Needs a recording spike first.**
1. **Shift+Tab modes** (2a) — founding requirement, removes a lie-shaped absence, and pays the `⇧⇥`
   debt. **Needs a recording spike first.**
2. **Create and name agents** (2b) — largest founding gap, cheapest it will ever be.
3. **Session importing** — Phase 3's last, and the spike already did the hard part.
4. **Undo, re-scoped** (2d) — smaller than it looks now that park exists.
5. **The grid** (2c) — largest, needs a design conversation before a plan.
6. **Profiles / first-run** (2e) — needs a conversation about what a profile is.
7. ~~**cmux pills** (2f)~~ — **cut by the owner, 2026-08-20.**

---

## 5. The things that are not features and must not be forgotten

- **Nothing has been driven by a human at a real terminal.** `docs/live-testing.md` is the standing
  list. This is a bigger risk to v1 than any item above.
- ~~**Nothing proves a woken session answers from the conversation it parked with**~~ — **PROVEN
  at the claude layer, 2026-08-21**: `testdata/stream/resume-memory-{before,after}.jsonl` — a fact
  told under a minted `--session-id` comes back verbatim under `--resume <that id>`, on the same
  id. The Wake layer above stays fixture-tested; findings in
  `docs/superpowers/notes/2026-08-21-runtime-command-findings.md`. Was Lane B item #1.
- **The manager reads agent-authored text and can act on the fleet.** Task 14 contained the forgery
  half; persuasion is bounded only by the two-verb list. **Any Phase 4 item that widens what a
  manager can do reopens that**, and the argument lives beside `managerVerbs`. **And the two-verb
  list bounds the fleet, not the machine** — see 2·0, which is the first item for that reason.
- **`--strict-mcp-config` is not optional** for the manager, or it inherits every MCP server on the
  machine.
- **The CLI drifts.** Verified against 2.1.226, currently 2.1.228. Re-verify the flag table each
  phase.
