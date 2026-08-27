# The room — design brief

**Date:** 2026-08-09
**Status:** **All four decisions settled with the owner 2026-08-09.** Three of the four were
*reframed* in the settling rather than picked from the options as written — see the amendment,
which supersedes the body above wherever they disagree. Nothing here is implemented yet.
**Reads against:** `docs/superpowers/specs/2026-08-08-wake-design.md` §6 §7 §8 §12; the 919-line
recorded corpus in `testdata/stream/`; `internal/ui/{app,dm,dm_blocks}.go`;
`internal/daemon/agent.go`.

Four decisions. Each is two or three options with the trade-off named, a recommendation, and a
mark for whether it is cheap or needs machinery that does not exist. The last section is every
place the corpus is silent.

---

## The numbers everything below rests on

Measured over all 26 fixtures, 919 lines, **46 recorded turns**, 26 distinct `claude` processes.

| | |
|---|---:|
| Lines that are `system` lifecycle frames | **628 (68.3%)** |
| …of which pure chatter (`hook_started`, `hook_response`, `thinking_tokens`) | **512 (55.7%)** |
| Events per turn — mean / median / max | **19.8 / 11 / 59** |
| `can_use_tool` permission asks in the whole corpus | **3** |
| `tool_use` events (26 of them a subagent's) | 44 |
| Turns producing **no** parent assistant prose at all | **8 of 46** |
| Multi-block frames (one line ≠ one event) | **0** |

`internal/core/event.go` states the older count of the same fact: *"These are 118 of the 184 lines
in the six original fixtures, so anything rendering them unfiltered drowns."* The full corpus is
worse, not better.

> **One correction to the framing this brief was commissioned under.** A recorded turn is not ~85
> events. The mean is **19.8** and the largest single turn in 919 lines is **59** (a turn with two
> subagents). That changes the arithmetic below in Wake's favour, so it is stated rather than
> quietly used.

---

## Decision 1 — What the room shows

The filter is the design. `core.EventKind` already distinguishes every candidate
(`KindAssistantText`, `KindThinking`, `KindToolUse`, `KindToolResult`, `KindTurnEnd`,
`KindPermissionRequest`, `KindSystem`), and `Event.Subagent` and `Event.Notice` already separate a
subagent's speech from the agent's and a notice from chatter. **All three options below are a
`switch` over fields that exist. None needs new decoding, a new frame kind, or a daemon change.**

Measured yield, per turn and as a share of the raw stream:

| Option | What earns a line | Total | Share | Per turn (mean/max) | 30 agents, one turn each |
|---|---|---:|---:|---:|---:|
| **1A** | turn ends · permission asks · session resets · non-benign rate limits | 50 | 5.4% | 1.1 / 2 | ~33 lines |
| **1B** | 1A + your own message + the **agent's own** prose | 97 | 10.6% | 2.1 / 4 | ~63 lines |
| **1C** | everything except system chatter and thinking | 208 | 22.6% | 4.5 / **19** | ~135 lines |

**1A — boundaries and blocks only.** *What you would miss:* what any agent actually said. Thirty
rows of `john ▸ turn ended` with no content, and you open a DM to learn anything. It also cannot
express the async case at all: `subagent-background.jsonl:30` ends its turn `terminal_reason:
"completed"` while the subagent is still on its first tool call.

**1B — plus the agent's prose.** *What you would miss:* every tool call and diff (44 + 44 events),
all thinking (29), all lifecycle. That is precisely the list §9 promises the DM keeps and the room
drops, so nothing is lost that the design did not already commit to losing. The honest cost is
that **8 of 46 recorded turns produced no parent prose at all** — for those the room shows a bare
turn boundary, which is 1A's failure mode surfacing 17% of the time.

**1C — everything but chatter.** *What you would miss:* nothing meaningful. What you would pay is
variance, not mean: 4.5 lines per turn on average but **19 from one turn**, so a single agent
running two subagents pushes twenty-nine other agents off the pane while they are producing two
lines each. The mean is affordable and the tail is what a reader experiences.

**A sub-choice inside 1B.** Subagent prose is 9 of the 54 `assistant_text` events. Including it
costs 106 lines instead of 97 — but it reintroduces the interleaving problem that
`dm_blocks.go` needed a gutter and a per-block header to solve (`subagent-parallel.jsonl` alternates
two subagents and their parent line by line). **Exclude it from the room; the DM already has the
attribution machinery and the room would need its own copy.**

> **Recommendation: 1B, subagent prose excluded.** It is the only option whose per-turn cost is
> bounded — a maximum of 4 lines across 46 recorded turns — while still telling you what an agent
> said rather than only that it stopped. **Cheap.**

---

## Decision 2 — What surfaces an agent into the room

### What the recordings establish

**One trigger is real, and it stops a process.** From `permission.jsonl:20`:

```json
{"type":"control_request","request_id":"a059b289-60ce-4d7e-b0a2-6328b18a13d5","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{...},"description":"probe-note.txt","permission_suggestions":[...],"tool_use_id":"toolu_015ieYRXYKkeSmxoadCemATC"}}
```

and the stream-json note, §6: *"The process then **blocks** until the client writes a
`control_response` on stdin."* Every session Wake spawns passes `--permission-prompt-tool stdio`,
so this is the shape every ask takes. The daemon already models it end to end: `agent.observe`
stores `ev.RequestID` on `KindPermissionRequest`, `stateLocked` returns `rpc.StateBlocked` for it,
and `rpc.SessionStatus` carries the `RequestID` so a client that reattached after the ask went past
can still settle it.

**Three of spec §6's four `needs-you` triggers have never been observed.**

- `AskUserQuestion` and `ExitPlanMode` appear in `init.tools` on **every** recording and **never
  once as a `tool_use` block** in 919 lines. Whether either arrives as a blocking `can_use_tool` or
  as an ordinary tool call is unknown — and that is exactly the difference between attention and
  content.
- `SendUserMessage` (the agent-initiated message `--brief` enables) appears **nowhere in the
  corpus**. `--brief` was in the argv of every recording in all three notes. 46 turns, 26
  processes, zero.

**A turn ending does not mean the work stopped.** `subagent-background.jsonl:87`:

```json
{"origin":{"kind":"task-notification"},"subtype":"success","num_turns":1,"terminal_reason":"completed","type":"result"}
```

Three of 46 recorded results carry that `origin` — turns nobody asked for. And `agent.go`'s own
header already concedes the blind spot: *"An agent that starts a turn on its own (--brief lets it)
is owed nothing by Wake and reads as idle while it works."*

**The six liveness states are already computed and already pushed.** `spawn.go`'s watchdog
broadcasts `FrameStatusPush` on every state change. Nothing here needs a poll.

### Options

**2A — attention is a sort order on the roster, and nothing else.** Room shows Decision 1's filter
for everyone; the roster orders `blocked → working (stalest first) → idle → silent → ended`;
`⌃⇧A` jumps. *Evidence:* strongest of the three — every input exists and arrives unasked.
*Cost:* an agent that blocks while you are inside a DM is a row moving in a sidebar you are not
looking at. And only `blocked` is evidence: `silent` is a five-minute timer and `idle` is provably
wrong for an agent working on its own. **Cheap.**

**2B — attention is a filter on the room: an agent appears only when it needs something.** The
owner's original instinct, read literally. *Evidence:* one recorded trigger, and it is the one that
halts a process. *Cost, measured:* the room would have been silent across 46 recorded turns except
for **3 moments**. The premise of the product is that the room is the primary surface; this makes
it a mostly-empty one, and "not blocked" does not mean "fine" on the async path. **Cheap.**

**2C — attention is a notification, additive to the room filter.** Decision 1's filter is the
baseline; a `blocked` agent is additionally promoted into the room as an answerable card, pinned
until settled, and sorted to the top of the roster. *Evidence:* the ask carries `request_id`, and a
subagent's carries `agent_id` (`subagent-permission.jsonl:22`); `rpc.FrameAllow` / `FrameDeny`
already exist and `daemon.agent.apply` already answers them. *Cost:* the room has to hold which
asks are outstanding and in what order — modest state, but state. The answering key itself is
already owed by deferred item I7. **Cheap, plus one piece of new state.**

> **Recommendation: 2C.** The only trigger the corpus records is one that stops a process dead, so
> it earns an interruption rather than a place in a sort order — and everything except the pinning
> is already built and already on the wire.

**A ruling this decision should make explicit:** do not build attention around §6's four-trigger
list. Build it around `rpc.StateBlocked`, which is derived from the one trigger that has been
observed. The other three get designed when they get recorded.

---

## Decision 3 — What the manager is

**3A — a real `claude` session** at the group root, with a system prompt and `--mcp-config`
pointing at Wake.
*Can:* reason, delegate, summarise, answer "why are these three stuck" — a question, not a query.
With §12's MCP tools it can also operate: *"This is what separates a manager that describes the
fleet from one that operates it."*
*Cannot:* be cheap. ~800 MB resident (spec §5, measured) and one of the twelve default live-cap
slots, and it burns quota on every `↵` in the room — which §7 makes the **default** action. It also
inherits every trap the corpus records: its `session_id` changes on `/clear`, `result` is per-turn
not per-process, and it can open turns nobody asked for.
**Needs machinery that does not exist:** `internal/core/mcp.go` is unwritten.

**3B — a Wake-local construct, no model.** The default addressee is Wake. `↵` with no `@` goes to a
parser: `who's blocked`, `broadcast …`, `/new backend`, `stop @alex`.
*Can:* answer everything that is a **query over state the daemon already holds** — and answer it
*more* accurately than a model, because it reads the live roster rather than a snapshot pasted into
a prompt four minutes ago. Zero quota, zero slot, zero memory, deterministic, and testable as the
pure function `router.go` is already specced to be.
*Cannot:* reason. It cannot answer "figure out why auth is failing across three repos", cannot
delegate a judgement, and cannot write a summary of what twenty agents did.
**Cheap.**

**3C — both, staged.** Local router is the default addressee; `@manager` addresses a real session
when one is spawned. The MCP server is built once and serves the manager whenever it exists.
*Cost:* two things and a rule for which handles what — but the rule is legible: **a query is local,
a question is a session.**

### What the MCP server changes about the answer

It is what makes 3A worth its slot — and it is also the observation that argues for 3B first:
**all four §12 tools (`list_agents`, `agent_status`, `spawn_agent`, `send_to_agent`) are functions
over daemon state that 3B has too, without a model in the loop.** The MCP server is the interesting
half; the model is the expensive half. Note also that `mcp__*` tool calls were **never exercised in
any recording** (stream-json §12: *"MCP tool calls (`mcp__*`) — none were exercised"*), despite five
MCP servers being attached in every `init` frame.

**One collision the owner has to settle either way.** `internal/daemon/names.go` (in
`.worktrees/identity`) rules: *"Nothing is ever addressed by name on the wire… `wake attach <name>`
resolves the name to an id in the **client**."* A manager calling `send_to_agent("alex", …)` is a
name arriving from a model, so either the daemon learns to resolve names — re-opening a ruling made
to keep a typed word away from the proof that authorises a SIGKILL — or the tool takes an id the
model got from `list_agents`. This is not a corpus question; it is a decision.

> **Recommendation: 3C, with 3B built first.** The four MCP tools are queries over daemon state, so
> the local router buys the entire answer surface for zero quota and leaves the model as an
> addressee that can be added later without moving anything.

---

## Decision 4 — Room and DM: one pane or two

### What is already free

- **The transport.** `daemon.fanOut` already broadcasts **every session's** events to **every**
  attached client; `App.apply` drops the ones whose `SessionID` is not its own. A room is a second
  consumer of a stream the client already receives and throws away. No daemon change, no new frame
  kind, no second connection.
- **The drain.** `inbox.go` already decouples the socket from the draw loop and folds batches.
- **The cost model.** `DM.SetSize` re-wraps only on a **width** change; height moves a window over
  lines already rendered. `View` is a fixed ~250 µs. A room therefore costs **one extra
  `renderEvent` per event that passes the Decision-1 filter** — 10.6% of events under 1B, at one
  width.

### What is not built

`internal/ui/{chat,board,groups,roster,layout}.go` from spec §4's package list **do not exist**.
`App` holds one `DM` bound to one `sessionID`. There is no focus model and no size split.

### Options

**4A — one pane, two modes.** `⌃⇧D` swaps the pane between room and DM; the other keeps its lines.
`App` grows a mode and a `map[sessionID]DM`, and `apply` stops discarding foreign frames. The
spec's **awareness strip** (§8) is one line and covers "don't lose the fleet while in a DM" without
a second pane.
*Cost:* you cannot watch the room while working in a DM.
**Cheap.**

**4B — two panes, split down, cmux style.** Room above, DM below, both live.
*Cost — and this is the specific one:* a second pane is a **second width**. `internal/render` holds
**one process-global mutex** for every session, so two widths are two glamour caches and two
re-wraps that serialize behind it. The measured 4.4× recovery on the draw path (1,024 frames:
350 ms → 80 ms; a 40-column drag from 40 re-wraps to 1) was won by making a drag cost one re-wrap.
Two panes make it two, and `App.View` is unmemoized with a value receiver, so the cursor blink
redraws whatever is on screen — doubled.
**Needs machinery that does not exist:** focus model, size split, per-pane width settling.

**4C — the full §8 grid.** Columns, one vertical split each, two sidebars, responsive breakpoints,
persisted widths, drag-to-resize. Everything 4B needs and a layout engine.
**Not a Phase 2 decision.**

> **Recommendation: 4A.** The room is already a second consumer of a stream the client receives and
> drops, and 4A is the only option that adds no second width behind a process-global render mutex.

---

## What we would be designing on a guess

**Eleven items.** Each is a place the corpus is silent, so a choice made here could not be shown
wrong by anything Wake currently holds.

1. **Agent-initiated messages (`--brief` / `SendUserMessage`).** Zero occurrences in 919 lines,
   across 26 processes that all passed `--brief`. Frame type, whether it blocks, whether it carries
   a `session_id` — all unknown. Spec §6 makes it a `needs-you` trigger and §8 makes it room
   content; both are unfounded today.
2. **`AskUserQuestion`.** Advertised in `init.tools` on every recording, never emitted. Whether it
   is a blocking `can_use_tool` or an ordinary tool call decides whether it is attention or content.
3. **`ExitPlanMode` / "plan ready".** Same: advertised, never emitted. `--permission-mode plan` was
   never recorded at all — only `manual` and `auto`.
4. **Whether a permission ask has a timeout.** Every recorded ask was answered by the driver. The
   corpus shows blocking; it does not show *indefinite* blocking, which is what makes `blocked` the
   top-priority state.
5. **Two permission asks outstanding at once.** Never recorded. `daemon.agent.pending` is a single
   string, so a second ask overwrites the first in `SessionStatus` — and `subagent-parallel.jsonl`
   shows two subagents running concurrently, so a parent ask plus a subagent ask is plainly
   reachable. The room's answer card is built directly on that field.
6. **Interrupting a session with a permission request outstanding.** Named unrecorded by both the
   interrupt note (§13) and CLAUDE.md. The room makes it common: the natural response to a blocked
   agent you do not want is `⎋`, not Deny.
7. **What decides async dispatch.** `run_in_background: true` → async twice, `false` → foreground
   six times, **key absent → async** once. Subagent §11 calls it *"the highest-value follow-up: the
   async path is the one that breaks `Stop()` and `KindTurnEnd`, and Wake cannot opt out of it."*
   It decides whether a turn boundary is ever a truthful room line.
8. **Whether an unprompted turn can be predicted.** 3 of 46 results carry
   `origin: {"kind":"task-notification"}` and nothing announces one is coming. A room that draws
   turn boundaries draws them for turns nobody asked for, with no way to mark them in advance.
9. **`rate_limit_info.status` other than `"allowed"`.** All 25 recorded samples are `allowed`.
   Spec §6 already removed rate-limiting from `error` for this reason; a fleet quota indicator in
   the room would be designed on a single observed value.
10. **MCP tool calls.** `mcp__*` never exercised in any recording, though five servers are attached
    in every `init`. The manager's entire operating capability (Decision 3A) rests on a path with
    zero recorded frames.
11. **Anything about N concurrent turns.** Every fixture is one process driven by one driver.
    Broadcast is N turns by definition, and what 30 of them do to fan-out, to the process-global
    render mutex, or to the API is unmeasured. `make soak` runs 20 **fake** sessions replaying
    fixtures — that measures Wake, not Claude.

**None of the four recommendations above depends on any of these eleven.** 1B filters on kinds that
are recorded; 2C keys on `StateBlocked`, derived from the one trigger that is; 3B is a function over
state Wake already holds; 4A adds no new wire, no new width, and no new process. That is the point
of choosing them.


---

# Amendment — settled with the owner, 2026-08-09

The brief framed the room as **a filtered activity feed**, which makes clutter a tuning problem:
you are always choosing how much of a firehose to admit. The owner reframed it and the reframing
is better, so it supersedes the framing above rather than sitting beside it.

## The room is a conversation, not a feed

*"The group chat should be like a messaging platform, like iMessage."* In iMessage you do not see
what people are **doing** — you see what they **said to you**. Activity lives outside the message
list. That kills the clutter problem structurally:

| Surface | Carries | Which is |
|---|---|---|
| **Main pane** | your broadcasts, agents' replies, anything an agent needs from you | things addressed **to you** |
| **Right sidebar** | tool calls, current file, liveness state | things **not** addressed to you |
| **Left sidebar** | workspaces, with unread counts | the conversation list |

**Decision 1 stands, relocated.** "Prose + boundaries" was the right answer; the prose goes in the
chat and the boundary marker goes in the sidebar, rather than both being lines in one feed.

**Decision 2 stands as 2C.** A blocked agent is promoted into the room as an answerable card.

**The line between the two is mechanical, not a heuristic:** Wake sends every message, so Wake
knows which turn is a reply to something it sent versus a turn an agent started alone. The corpus
covers the edge — 3 of 46 results carry `origin: task-notification`.

## What a finishing agent posts, and why it is the agent's own words

The owner asked for *"I have finished working on…"* in the chat. **Yes, and it must be the agent's
own closing prose rather than a line Wake writes.** Measured over the corpus:

| | |
|---|---:|
| Closing prose blocks | 44 of 52 turns |
| Median length | **21 characters** (`"I'll create the file."`) |
| Over 400 chars (~5 lines) | **2 of 44** |
| Longest | 13,499 (a `/context` dump, not a sign-off) |
| Turns with no prose at all | **8 of 52** |

Three reasons it is the agent's words:

1. **It is already there and free.** `"Fixed the retry header, tests pass"` tells you whether to
   look; `alex finished` does not, and costs the same line.
2. **A Wake-authored "finished" would sometimes be false.** A turn ending does not mean the work
   stopped — an async subagent streams past its own `result`. If Wake asserts completion it is
   wrong 3 times in 46. The agent's words are the agent's claim; Wake only relays. Same reason the
   permission-mode indicator was removed.
3. **It is the content Decision 1 already selected.** No new machinery.

*The gap:* 8 of 52 turns produce no prose. **That** is where a synthetic marker earns its place —
a quiet `⤷ finished` when there is nothing else to show.

## Clutter at 30 agents, measured

A broadcast to 30 agents means 30 replies, and that is **what was asked for**, not clutter. At a
median of 21 characters it is ~30 lines: a screen and a half, scannable. Collapse past ~400
characters. Expect roughly **one** collapsed reply per broadcast, not thirty.

*Caveat, stated rather than hidden:* these fixtures are probe sessions with short synthetic tasks,
so real work will produce longer sign-offs than a 21-character median. The **shape** holds — most
short, a few long, collapse the tail — but the number is optimistic.

The real risk was never solicited replies. It is **unsolicited volume**, which the split above
already handles.

## The collapse threshold is a surface boundary, not a display nicety

Raised by the owner: *"what if the agents are working on design-heavy things and require you to
read larger amounts of text?"*

**That flow does not belong in the room at all.** Deep reading is DM work. The room is a hub, and
a hub is bad at deep reading by design; making it good at both would wreck it at the thing it is
for. So:

```
sydney <> auth-fix
  I've mapped three approaches to the token refresh. The
  short version is that option 2 is the only one that…
  ⤷ 1,400 words · ⏎ open DM
```

**A long reply in the room is a pointer, never the content.** This is the one case where
*"this person has a message, go review it"* is right — it is wrong as the primary mechanism and
right at the threshold.

## The fleet is not homogeneous, and this supersedes "the room is the primary surface"

The owner's example — planning a design with one agent while planning a product with another —
reveals two relationships, not one:

- **Thinking *with* an agent** → a DM, open and persistent, full fidelity. Possibly two at once,
  which is exactly the original `⌘⇧D` "open one below".
- **Supervising agents** → the room. Short signals, blockers, done markers.

So the room is **the pane you return to**, not necessarily the one you stare at. Spec §8 frames it
as the primary surface; this amendment says room and DM are **peers**. What follows:

- Opening a DM from the room must be one keystroke and must not lose the room.
- **Multiple DMs open at once is a mode, not a nice-to-have.** `⌘⇧D` moves up in priority.
- **Unread state has to be real** — per-agent in the room, per-workspace in the left sidebar. An
  hour inside a DM must not cost you what accumulated.

## Clarifying questions — belongs in the room, cannot be built yet

A clarifying question is the cleanest case of "an agent needs you" and belongs in the room as the
same answerable card as a permission ask. **But `AskUserQuestion` and `ExitPlanMode` appear in
`init.tools` on every recording and never once as a `tool_use` block in 919 lines**, so whether
one arrives as a blocking `control_request` or as an ordinary tool call is unknown — and those are
different mechanisms. One-hour spike: give a session a genuinely ambiguous task and record what
comes out. Until then it stays in §10, not in the design.


---

## Decision 3 — settled: a real `claude` session, scoped as a service

**Chosen against the recommendation, and the owner's example is why.** The brief recommended a
Wake-local router on the grounds that all four §12 MCP tools are queries over daemon state. The
example that overturned it:

> `@manager tell all backend working members that aren't complete to pause and do blank`

| Part | What it needs |
|---|---|
| "backend" | **A judgment about what each agent is working on.** Not a field. No lookup gives it. |
| "working … aren't complete" | Liveness state — the daemon has this exactly |
| "pause" | An action **on a set** — interrupt each one's turn |
| "and do blank" | A new instruction fanned out to that filtered set |

A local router cannot do "backend" without being told which agents, which defeats the point.

### Consequences, each of which is real work

- **The MCP server stops being optional.** §12 moves from later to **prerequisite** — a `claude`
  process cannot see daemon state without tools.
- **`send_to_agent` is not enough.** The example needs *filter, then act on all of them*, and
  **`interrupt` becomes a tool**, because "pause" is the `⎋` that shipped this week.
- **Facts come from the daemon, reasoning from the model.** `alex has been waiting 4 minutes` is
  the daemon's number; *"which usually means it hit something big"* is inference. Worth knowing
  which half is which when the manager says something surprising.
- **The name-on-the-wire ruling holds.** `send_to_agent("alex", …)` is a name arriving from a
  model, and `internal/daemon/names.go` rules that nothing is addressed by name on the wire —
  names resolve client-side, ids cross the socket, and a test enforces it because that property is
  what keeps the reaper's identity proof safe. **`send_to_agent` takes an id from `list_agents`.**
  One extra tool call; the property survives.

### Scope: a service, not a participant — settled on measurement

The owner wanted it to see everything, and named the case: *"both peter and john working on
blank."* Measured, at 30 agents, mean room message 372 chars ≈ 93 tokens:

| turns/agent/day | room messages | manager context | 20 questions costs |
|---:|---:|---:|---:|
| 10 | 300 | 28k | 0.6M tokens |
| 25 | 750 | 70k | **1.4M tokens** |
| 50 | 1,500 | 139k | 2.8M |

**The manager re-reads its whole context on every message.** The 200k window is exhausted at
**71 turns per agent** — and it degrades exactly when it would be most useful, because compaction
takes the oldest history and the collision worth noticing happened hours ago.

**But the owner's own example does not need message history.** *"Peter and john working on the
same thing"* is answerable from what they are **doing**, not what they **said**:

```
list_agents →
  peter  <> api-v2     working   Edit(auth/token.go)
  john   <> api-v2     working   Edit(auth/token.go)     ← a string comparison
```

`ToolCall.Display` already carries this, decoded, on the wire. So: **service scope, rich tools,
plus a `roll_up` digest** Wake builds from data it already has — broad awareness **on demand, paid
once**, rather than carried always.

*What is genuinely given up:* noticing something **semantic** — two agents solving one problem in
different words with no shared file to give it away. Real, rarer than the file case, weakest value
per token, and the first thing compaction destroys. **Revisit only with a specific missed case in
hand**, which is a far better basis for 1.4M tokens a day than a guess now.

---

## Decision 4 — settled: side by side, not stacked

**Chosen against the recommendation, and the reasoning is sound.** The brief recommended one pane
because a second pane is a second width behind `internal/render`'s process-global mutex. The owner
chose a **vertical split — the DM opens beside the room, not below it** — because *vertical space
is the scarce resource*: a conversation is a vertical list, and a top/bottom split halves the
lines each side can show.

```
┌ WORKSPACES ─┐┌─ group chat ──────┐┌─ sydney <> auth-fix ─┐
│ ● wake    3 ││ john ⤷ turn ended ││ I've mapped three    │
│   pufferfish││ ⚠ alex wants      ││ approaches to the    │
│   dotfiles  ││   rm -rf build/   ││ token refresh…       │
│             ││   [a]llow [d]eny  ││ ⏺ Read(token.go)     │
└─────────────┘└───────────────────┘└──────────────────────┘
                                    ↑ right sidebar auto-closed
```

**Settled shape:**

- The DM opens **beside** the room, with a **draggable divider**.
- **The right sidebar auto-closes** when a DM opens, reopenable by key. Not just for the columns:
  when you are deep with one agent, *what everyone else is doing* is the information you are not
  using — and the room is still beside you, so the fleet is not lost. It does the awareness job the
  sidebar was doing, and **no awareness strip is needed.**
- **Below 120 columns the DM takes over the pane instead of splitting.** §8 already collapses the
  groups sidebar between 120 and 160; this is the same rule with one more trigger.

Measured, at a 16-wide left and 20-wide right sidebar:

| terminal | both sidebars | right closed |
|---:|---:|---:|
| 200 | 82 per pane | **92** |
| 160 | 62 | **72** |
| 120 | 42 — poor | **52** — marginal |

Closing the right sidebar buys ~10 columns per pane: the difference between tight and comfortable.

### The cost this incurs, stated plainly because it is the opposite of the obvious

**A vertical split is the more expensive one for this codebase.** `DM.SetSize` re-wraps only on a
**width** change — so a top/bottom split gives both panes one width, one glamour cache, one
re-wrap, while side-by-side gives two widths and two of everything. And a draggable divider turns
width changes from *"occasionally, when I resize my terminal"* into *"whenever I want more room"*,
which is precisely the motion the measured 4.5× draw-path win was about (a 40-column drag from 40
re-wraps to 1).

**This is a deliberate trade of render cost for reading benefit, and it is the right direction for
a tool someone stares at all day.** What it obliges: **the divider drag must settle** the way the
window drag does (~80 ms), so a drag costs one re-wrap rather than one per column. That machinery
exists; it has to apply to the divider too.
