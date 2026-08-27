# What Wake is, and what was asked for

This file exists because the phase map and the founding requirements lived only in a chat that got
compacted. They are written down here so they cannot be lost again. **The design spec
(`docs/superpowers/specs/2026-08-08-wake-design.md`) is still the source of truth for behaviour**;
this is the shorter thing — what the owner actually asked for, and where each part of it stands.

---

## 1. The ask, in the owner's own words

Quoted from the message that started the project, trimmed only where it repeats itself. The
spelling is theirs; the emphasis is mine, on the phrases that turned out to be load-bearing.

> I am trying to make a new app for claude code users! I would prefer it to be like a terminal
> emulator like cmux or at least like that style. […] I want this app to be **as close as you can
> to regular claude code cli, same type of UI, mostly, same /commands, @ commands, shortcuts like
> shift enter for plan/auto/manual mode**, etc. basically i want this app to be **a new way for
> developers to interface with claude code and manage agents**.
>
> the premise is this, **a main pane opens for a group chat with all your sessions/agents
> available (by default)** in the group chat — when you type in the group chat, all your agents can
> see your message when you send. **if you @ an agent you can talk directly to them** in the group
> chat unless you do like a shortcut that is like send all etc.
>
> main group chat is the main pane in the middle where you can `/new agent(session) in <project
> dir or by default at root dir>` or `/add-<agent-name>` to add a live agent to the groupchat […]
> or you can do a keyboard shortcut like cmd/ctrl d to make a new one.
>
> **there will be two side bars. the left will be group chats, kinda like workspaces in cmux.**
> also **it should inherit a lot of the same keyboard shortcuts so native cmux users can transition
> quickly.** […] **the right sidebar (smaller, thinner) will have the list of all your sub agents**
> categorized by group chat or just list all so you can toggle into them to manage them directly
> there. and **you can open them like in a new pane next to your groupchat.**
>
> **i want the colors to be like claude, grey/black and orange** and maybe like yellow as well.
>
> […] maybe not for MVP […] each session/agent created […] to have a pixel icon, like up to 16
> different ones — and def for MVP it would **default to a random name like alex or john or
> sydney** and then you can either **rename or assign a 'task'** so they are called like
> `sydney <> dev-5748` or `alex <> ui fixes`.
>
> **in the main group chat you should only be seeing responses from the sessions if they need
> something from you or call you** — so by default they should be set to **automode**. there should
> be like **one status update per agent in an expanded view of the right sidebar**.
>
> […] i just want it to be **native and fast**.

Two sentences in there are the product, and everything else is arrangement:

- *"a new way for developers to interface with claude code and manage agents"*
- *"you should only be seeing responses from the sessions if they need something from you or call
  you"*

The second is the filter, and it is why Wake is a room and not a log. `CLAUDE.md`'s scope
discipline section says the failure mode for this project is drifting toward being a worse cmux;
this is the sentence to re-read when that is in doubt.

---

## 2. The phases

Set when Phase 1's plan was written: *"Phase 2 covers the room; Phase 3 covers the fleet."*

| Phase | What it is | Status |
|---|---|---|
| **1** | Core, daemon, DM at Claude Code fidelity | **Done** — 11 tasks, merged |
| **2** | The room: fold, lines, cards, both sidebars, routing, layout, split, MCP | **13 of 16 done.** Left: 14 (acting tools + digest + `wake mcp`), 15 (the manager as a real `claude` session scoped as a service), 16 (what the room costs at 30 agents) |
| **3** | **The fleet:** pool, park/wake, session importing, fork | Fork is built — daemon verb, `wake fork <who> [name]`, and `⌃F`. **Park/wake is built** — `⌃C`, `⌃Q`, `/resume`, the park book, and bare `wake` reopening the room with a restore offer on it. Left: pool, session importing |
| **4** | Whatever §17 still names that phases 1–3 did not absorb | **Scoped**: `docs/superpowers/specs/2026-08-12-phase-4-scope.md` — six §17 items plus two that belong to no phase. A draft to argue with, not a plan |

**Phase 4 is named deliberately.** §17's v1 list is bigger than three phases: profiles, undo,
mouse throughout, cmux status pills, the full grid, first-run generation. Some of those have since
been absorbed (mouse and `!cmd` were pulled into the MVP). Naming the rest stops "Phase 3" quietly
becoming the place things go to be forgotten.

---

## 3. Every founding requirement, and where it stands

**Built and working:**

| Asked for | Where |
|---|---|
| Native and fast, cmux-style, no Electron | Go + Bubble Tea; `make build && ./bin/wake` |
| Group chat as the main pane, filtered to what needs you | `internal/ui/chat.go` — the room |
| `@agent` to talk to one; broadcast to all | `internal/core/router.go`; `@all` |
| `@file` exactly like Claude Code | Passes straight through — nothing to build |
| Left sidebar = workspaces, cmux-style | `internal/ui/groups.go` |
| Right sidebar = agents, what each is doing, toggle into them | `internal/ui/roster.go` |
| Open one in a pane **next to** the group chat | `internal/ui/panes.go`, draggable divider, takeover below 120 cols |
| Claude's colours — grey/black and orange | `internal/ui/theme.go` |
| Random default names (alex, john, sydney) | `internal/daemon/names.go`, 64-name pool |
| `sydney <> dev-5748` — name plus task | `internal/daemon/label.go`, read from `.git/HEAD` |
| Default to automode | `spawnPermissionMode = "auto"` |
| One status line per agent in the right sidebar | `roster.go`, attention-ordered |
| `!cmd` | `internal/ui/bang.go` |
| Markdown, diffs, tool calls, task lists at Claude Code fidelity | `internal/render/` |

**Asked for and NOT built — the honest list:**

| Asked for | Status |
|---|---|
| **Shift+Tab for plan/auto/manual mode** | **Not built, and the key is now taken — see §5.** Deferred I7 owns the mechanism |
| `/new agent in <dir>` | **Built, 2026-08-12.** `internal/ui/new.go` — `/new`, `/new <name>`, `/new in <dir>`, `/new <name> in <dir>`, and the founding message's own noun (`/new agent in ~/p`) skipped rather than taken as a name. One `rpc.FrameSpawn` from the TUI; the daemon still owns the name and the room waits for the report before it opens the pane |
| `/add-<agent-name>` | **Refused as spelled, 2026-08-12**, with the argument in `internal/ui/slash.go`'s header and `deferred.md`. A `<verb>-<suffix>` routing rule claims an operator's whole command set — eight of the 133 commands in this repo's recorded corpus begin `new-` — it is not decidable from the draft (`/add-dir` vs `/add-sydney`), and there is nothing for it to do, because every live agent is already in the room. `/add <name>` was **not** shipped in its place: a command that does nothing is a lying feature. It becomes a real verb the day a group has membership |
| A `+` on the right sidebar to create a session | Not built — `/new` is the way in today. Another lane owns `roster.go` |
| **The manager, from inside Wake** | **Built, 2026-08-15.** Spec §12 gives it "a permanent seat in every group" and the build had no way to seat it: `wake manager` at a shell was the only thing that produced one, so the room refused every unaddressed draft and pointed the operator *out* of the room to fix it. Every verb that opens the TUI now seats one (`cmd/wake/ensuremanager.go`), and `/manager` is the switch — absent → spawn, parked → wake, running → park. A **command rather than a key**: with the default on it is the rarest verb in the build, and every remaining ctrl byte is either Enter, Tab, the composer's newline, or a flow-control code sitting beside a `⌃Q` bound to *quit* |
| **Toggle into an agent from the right sidebar** | **Built, 2026-08-12 (second live run).** `↑↓` walks the roster and `⌃D` opens what it lands on; a **click on a row** opens that agent. Until then `Roster.Move` had no caller and `press` took only the x coordinate, so an agent that was neither first in attention order, nor blocked, nor already open could not be reached from inside Wake at all |
| `⌃D` to make a new agent | `⌃D` exists but means **open DM**. The founding message asked for it to create one. `⌃F` forks and `/new` creates; a *key* that creates is still unbound |
| One status update per agent in an **expanded** right sidebar | Only the one line is built. The expanded view is not |
| **Rename an agent, or assign it a task** | **Built, 2026-08-12.** `/name <new>` and `/task <what it is working on>`, each addressed to the conversation you are in or to an `@handle`. `alex <> ui fixes` is reachable. **A rename changes the handle** — `@old` stops resolving, because a name is released and reissued and an alias would resolve to two live agents. Refused for a parked or ended session, and for the manager |
| Pixel icons, up to 16 | Explicitly **out of v1** (§17: "Pixel sprites — block glyphs stand in") |
| Subagents listed and categorised in the right sidebar | Deferred by the owner in favour of shipping the room first |

**That theme is closed as of 2026-08-12.** It read: *"Wake can manage agents but cannot create or
name them from inside itself"* — the largest single gap between the founding message and the build,
and not any phase's task. `/new` creates, `/name` renames, `/task` labels, and `/add-<agent-name>`
is refused with its argument written down rather than left open. What is left of the theme is a
**key** that creates (the founding message asked for ⌃D; ⌃D opens a DM and ⌃F forks) and the `+` on
the right sidebar, both of which are surface rather than capability.

---

## 3a. Settled 2026-08-10 — the lifecycle verbs change

**Shipped in full, 2026-08-11.** `⌃C` **parks** the focused agent rather than detaching; `⌃Q` parks
the whole fleet and exits; **`⌃O`** detaches; `/resume` brings a parked session back; and **bare
`wake` reopens the room** rather than always spawning — over whatever fleet there is, over a fleet
parked by `⌃Q` with no daemon behind it at all, and, on a machine with nothing running, as first run,
which still spawns. A room that opens on parked sessions **offers them back**: one line naming who is
parked and `/resume all`.

**The offer is a line rather than an automatic restore, and that is a decision rather than a
shortcut.** Waking N sessions on start would put N spawns in front of the `FrameHello` that
`daemon.EnsureRunning`'s discriminator waits on, so `wake status`, `wake attach` and `wake fork`
would each relaunch a fleet on their way past. A prompt was the other alternative and blocks the
first frame on a decision the operator may not have come here to make.

`⌃C` detached *because* stopping was irreversible — park is what made the rebinding safe, so
park/wake was a prerequisite rather than a companion, and the keys were bound only once it worked.
The full argument, the recorded constraints and what it does not settle:
`docs/superpowers/specs/2026-08-10-lifecycle-and-restore.md`.

Two knock-ons worth seeing from here. `/resume` is Wake's **first slash command**, and what that
dragged into reach from §3's "cannot create or name agents from inside itself" gap is the *layer* —
`internal/ui/slash.go`, where adding a command is an entry in `commands`. It was not the gap itself,
and the gap was closed on 2026-08-12: `/new`, `/name` and `/task` each cost exactly that entry plus
their own work, and `/add-<agent-name>` was refused with its argument recorded. And a parked
agent is a session state `rpc` had no member for, which made five totality guards demand a verdict
the moment it was added — including one this task added, because bare `wake` is the single call site
in `cmd/wake` where `rpc.StateOrphaned` can actually arrive.

---

## 4. The two rules that override taste

From `CLAUDE.md`, repeated because they are the ones most likely to be argued away:

- **Wake is not a terminal emulator or multiplexer.** No PTY, no VT100, no browser panes, no
  arbitrary shells. The founding message said "like cmux or at least like that style" — *style*,
  and that is the whole of the resemblance. Chasing the rest is how this project dies at 40%.
- **Wake must be cheap to leave open.** It sits next to 15–30 `claude` processes on a laptop.
  Nothing may burn CPU while nothing is happening.

---

## 5. Settled: `⇧⇥` is the permission mode, and next-blocked is `⌃X`

**Resolved 2026-08-12.** This section was an open conflict for two phases; it is kept as the record
of how the reservation was made and spent, because that is the part worth copying next time.

The founding message names exactly one keyboard shortcut by function — *"shortcuts like shift
enter for plan/auto/manual mode"* — which in Claude Code is **Shift+Tab**, cycling the permission
mode.

`⇧⇥` was bound to **"next blocked agent"** (Task 11). It got there honestly: `⌃⇧A` was the intended
key and turned out to be unbindable in bubbletea v1.3.10, `⇥` took pane focus, and `⇧⇥` was the next
reasonable chord. The permission-mode indicator had already been *removed* from the hint line,
because it cycled a label that reached no process — deferred **I7** owned making it real, and
shipping the indicator without the mechanism is worse than shipping neither.

**I7 is built and the debt is paid.** `⇧⇥` cycles `plan` / `auto` / `default` and writes an
`rpc.FrameMode`; the label moves when the daemon's receipt arrives, never on the keystroke.
Next-blocked moved to **`⌃X`**, one of the two keys this section reserved. `⌃Y` is unspent and stays
reserved.

**Fork did not take `⇧⇥` and did not take `⌃O`, `⌃X` or `⌃Y`.** It took `⌃F`, paying one shadowed
textarea binding (`CharacterForward`, which is `→` by another name in a one-line composer),
precisely so this collision keeps its options.

**The rebinding spent one of the three.** `⌃O` is detach (2026-08-11, §3a), which was the argued
choice of the three because it is the only one the existing probe already covered — which left
`⌃X` and `⌃Y` for next-blocked, where one would do. That is the one this change spent. `⌃Q` cost
nothing from that set: it is a chord neither bubbletea nor `bubbles` binds, probed in
`internal/ui/keyprobe_test.go`, and the open question about it is a *terminal* one — `IXON` eats
`⌃Q` as XON when flow control is on — which is in `docs/live-testing.md` because `go test` has no
tty. `⌃X` does not inherit that: XON/XOFF are `⌃Q` and `⌃S`, and `⌃X` is neither.

**What the mode does not survive.** A mode set while an agent runs is a property of the *process*:
`--resume` does not carry one, so a woken session comes back in its spawn mode
(`docs/superpowers/notes/2026-08-12-permission-mode-findings.md` §8). The ruling was **say it, not
persist it** — `parked.json` gains no field, and the wake notice names the mode it came back in.

---

## 6. What has never been checked by a human

See `docs/live-testing.md`. Nothing in this file's "built and working" column has been used at a
real terminal by a person — it has been proved against recorded fixtures and synthesised events.
Those are different claims.
