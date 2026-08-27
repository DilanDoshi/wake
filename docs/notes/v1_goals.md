# v1 goals

The product-level list, as distinct from `deferred.md` (things put off) and `decisions.md`
(rulings made). **Tracked**, and moved here from `notes/` on 2026-08-09.

`docs/superpowers/specs/2026-08-08-wake-design.md` §17 holds the formal v1 In/Out boundary. This
file is where the boundary gets **argued with** — new ideas, what they would cost, and what has
turned out to be true since §17 was written.

---

## New: session importing

**Asked for 2026-08-09.** *"If you have a bunch of sessions open in terminals scattered, or in
cmux, be able to open a group chat among them — select which ones you want to add — and then
query them via the group chat."*

This is a strong idea and it is the one feature that would make Wake useful on **day one of
adoption** rather than day one of a new project. Nobody starts fresh; everybody already has
sessions open.

**It is two very different features wearing one name, and only one of them is buildable.**

### (a) Adopt a *live* process — not buildable, and the reason is structural

A `claude` running in someone's terminal is an interactive TUI attached to a pty. Its stdout is
escape sequences, not stream-json. Wake would have to screen-scrape it, and:

- **"Wake never screen-scrapes" is a `CLAUDE.md` non-negotiable**, and the reason is not
  fastidiousness — every piece of state Wake has comes from structured JSON, which is what makes
  the airlock, the attention model and the whole daemon possible.
- There is no stdin channel to take over. Wake sends messages by writing stream-json to a pipe it
  owns; another terminal owns that process's stdin.
- Even if both were solved, the process was not started with `--permission-prompt-tool stdio`, so
  Wake could never answer a permission ask for it.

**Do not build this.** It would be a second, worse Wake bolted to the side of the first.

### (b) Adopt a session's *transcript* — buildable, and the corpus already backs it

`claude` persists every session to disk, and `--resume <session-id>` re-enters one. The corpus
has `resume-park.jsonl` and `resume-wake.jsonl`, and the interrupt spike established that **an
interrupted session `--resume`s losslessly**. So:

1. **Discover** sessions on disk (`~/.claude/projects/…`), with their directory, last-active
   time and a first-message preview. **Checked on this machine, not assumed: 115 project
   directories, 444 session transcripts**, one file per session named by its UUID, under a
   directory named for the working directory it ran in — so the picker gets the session id, the
   repo and the recency for free, with no new bookkeeping and nothing on a timer.
2. **Present a picker** — this is the "select which ones you would like to add" step, and it is
   the whole UX of the feature.
3. **Resume each chosen one** as a normal Wake session, in stream-json mode with a
   `--permission-prompt-tool stdio` of its own. From that moment it is an ordinary Wake agent:
   nameable, DM-able, addressable in the room.

**Both questions are now answered, and the answer to the first changed the design.** Built
2026-08-12; the recordings are `docs/superpowers/notes/2026-08-09-resume-fork-findings.md` §5,
`2026-08-10-live-fork-findings.md` §12 and `2026-08-12-session-discovery-findings.md`.

- **What happens to the original terminal's process?** The guess above was that `--resume` would
  collide the way `--session-id` does. **It does not collide — it branches**, which is worse: both
  processes are accepted, both answer correctly from their own history, the transcript branches in
  place with last-writer-wins, and there is **no error on any wire**. So there is nothing to detect,
  and the liveness check has to be Wake's own.
- **And Wake's own check cannot see the case this feature is for.** `resumeSafe` matches
  `core.SessionArgvMarkers` — a flag and its value — and 2026-08-12 §5 counted four live `claude`
  processes on this machine whose entire command line is the word `claude`. The CLI minted their ids
  and no id appears in any argv. That is precisely *"open in terminals scattered."*
- **Does `--resume` accept the flags Wake needs?** Yes — all eight, with `--permission-prompt-tool
  stdio` honoured on both a resumed and a forked process (2026-08-09 §6).

**So step 3 is not a resume.** An import is `--resume <src> --fork-session --session-id <new>` — the
corpus's own conclusion, *"it makes the fork the safe primitive for importing, not just for
forking"* — with 2026-08-10 §5's sha256 measurement behind it: a fork of a **live** parent leaves
that parent's transcript byte-identical. **The cost is the id.** An imported session is a new Wake
session carrying that conversation, not the original under new management, and that is the honest
trade against writing into a file another process may be writing to.

**Step 1 turned out to be the hard part, and not for the reason expected.** The directory is *not*
free with the slug: `/`, `.`, ` ` and `_` all map to `-`, so `-Users-dev-Documents-Project-Pufferfish`
is equally consistent with three real paths — and the transcript's own `cwd` is a property of a
*message*, wrong for 58 of 428 sessions here. Discovery therefore **verifies** rather than decodes,
and answers a directory for 331 of 428, none ambiguously.

**Step 2, the picker, is `wake import` with no argument** — a listing that dials nothing, because
this is the verb somebody runs before they have a fleet. A row is not an offer: it says a
conversation exists on disk and nothing about whether it is closed. There is **no TUI surface yet**;
`/import` is in `deferred.md`.

*Related, and already noted in `deferred.md`:* every `task_notification` names an `output_file`
— an on-disk JSONL transcript of a subagent — which Wake has never opened. Same machinery.

---

## New: fork a session

**Asked for 2026-08-09**, and called important. *Start a new session as a fork of an existing
one* — the new agent inherits the conversation so far and then diverges.

This is the cheapest of the three resume-family features, because **the CLI already has the
primitive**:

```
--fork-session    When resuming, create a new session ID instead of reusing
                  the original (use with --resume or --continue)
```

**Why that flag is load-bearing rather than a convenience, established from the corpus rather
than reasoned:** `--resume` on its own **reuses the session id**. Both recorded resume pairs show
it —

```
interrupt-then-close.jsonl    session_id ee4d083f-03d9-4ffa-8802-c6ce33cfd6d3
interrupt-resume-after.jsonl  session_id ee4d083f-03d9-4ffa-8802-c6ce33cfd6d3   (a separate process)
resume-park.jsonl             session_id 0bd89bfa-ff4f-4828-89e7-f47f5f8e943c
resume-wake.jsonl             session_id 0bd89bfa-ff4f-4828-89e7-f47f5f8e943c
```

— so a naive "resume it twice" fork would hand Wake **two live sessions sharing one id**, which
breaks three things at once: the roster is keyed on it, `maySpawn` requires it to be a UUID
precisely so the reaper can trust it, and the reaper proves a process group's identity by finding
that id in its argv. Two processes carrying one id is a `KillGroup` aimed at both. And the other
obvious route is closed too — `claude` **refuses a `--session-id` that is already in use**
(recorded, `testdata/stream/session-id-conflict.stderr.txt`).

So `--fork-session` is not a shortcut; it is the only door. Good news for the design: it means a
fork is an ordinary Wake session from the moment it starts — its own UUID, its own name from the
pool, its own roster row, its own DM.

**All four are now recorded and built.** `docs/superpowers/notes/2026-08-09-resume-fork-findings.md`
and `2026-08-10-live-fork-findings.md` are the recordings.

- `--resume … --fork-session --session-id <new>` coexists with all eight of Wake's fixed flags, and
  a forked process raises a real `can_use_tool` on stdout exactly as its parent does.
- **Wake keeps the id.** `--session-id` wins, so `maySpawn` is unchanged and the reaper still finds
  that UUID in the argv. Nothing has to be learned from the stream.
- The fork's transcript lands in its own `<uuid>.jsonl` and the parent's file is **byte-identical**
  across the fork, measured by sha256 at both generations.
- **You can fork a session that is currently running**, provided it is idle. Mid-turn, mid-tool and
  blocked-on-an-ask are unrecorded and are refused with a sentence saying when to try again.

Still open, and both are §12 items rather than gaps in the build: what a fork of a *mid-turn*
parent inherits, and what happens when a parent takes a turn while a fork is running. Wake cannot
prevent the second — the operator owns the parent's composer — so the mitigation is a sentence on
every confirmed fork saying that a fork is a snapshot. **That sentence is built**, in the TUI's
fork action, which is the surface both candidates were going to share: `ui.forkOpened` says
*"@sydney is a fork of @alex. It has that conversation as of now - nothing @alex does next reaches
it."* on the notice row of the fork's own conversation. `wake fork` prints the fork's handle and
leaves the sentence to the DM it opens. This paragraph claimed the sentence already existed before
it did, which is why `docs/notes/deferred.md` records the claim as well as the work.

**Why this is worth doing early:** it is the natural way to explore two approaches from one
context — the thing 15–30 agents are *for* — and unlike session importing it needs no picker, no
discovery UI, and no adoption story. One verb, `wake fork <who>`, and the identity work just
landed the `<who>` resolution it needs.

---

## Pulled into the MVP 2026-08-09 — `!cmd`, `@` and mouse

### `!cmd` — must be built; it does not work in the mode Wake drives

**Measured, not assumed.** Sent `!cat note.txt` through Wake's exact argv:

```
assistant: "`note.txt` contains: ```hello from a real file```"
```

No `<local-command-stdout>` frame. The `!` was **not intercepted** — it reached the model as
text, and the model happened to do something sensible. `!cmd` is a feature of Claude Code's
*interactive* TUI, not of `--print` stream-json.

**Buildable, and the corpus already has the shape.** Slash commands *do* work in this mode —
`/context`, `/model` and `/compact` are all recorded — and `/compact` returns its output as a
**user** frame wrapped in `<local-command-stdout>Compacted </local-command-stdout>`, a tag
`internal/core` already decodes. So Wake intercepts `!` in the composer, runs the command
locally, and injects the result in that same wrapper.

No new frame type, no airlock change, no daemon work: composer interception + local exec +
inject. Belongs in the DM, and should work in the room addressed to nobody.

### `@file` — already works, nothing to build

Asked a session about `@note.txt` and got the file's actual contents back **with no tool call
at all**, so the CLI expands the reference before the model sees it. In a DM this works today.

### The collision, and how it resolves

`@` means two things: a **file reference** in Claude Code, **agent routing** in Wake's design.
In a DM there is no conflict — one agent, nothing to route to, so `@` is always a file.

In the room they collide. **Resolution: match a live agent name first, fall through to the CLI
otherwise.** Wake knows every live name; they come from a 64-name pool of short human words; and
`names.go` already guarantees no name prefixes another. So `@sydney` routes and `@src/token.go`
passes through untouched. The residual ambiguity is a file literally named `alex` at the repo
root — rare, and the room can show what it resolved to before sending.

This also preserves the rule `names.go` enforces with a test: **the name resolves in the client,
and only an id crosses the socket.**

### Mouse — promoted out of polish

Not a nicety; half of Phase 2's settled decisions assume it. The draggable divider, clicking an
agent in the sidebar, the `[a]llow`/`[d]eny` card. The wheel already scrolls; click-to-focus and
drag are missing.

One constraint that shapes the build: **bubbletea v1.3.10 has no wheel-only mouse mode** — only
1002 and 1003, and 1002 already reports motion while a button is held. So any mouse support means
drag events on every motion. Survivable now that the render is off the draw goroutine, and the
reason mouse and the divider should be designed together rather than bolted on separately.

---

## Committed for v1, from the spec's §17

Unchanged and still right. Reproduced here only where reality has moved:

| §17 item | Status |
|---|---|
| Daemon core with detach/reattach | **Built**, plus a reattach verb §17 did not anticipate |
| Spawn / park / wake with UUIDs **and names** | Spawn and names built; **park and wake are not** — no `--resume` path exists yet |
| Stop one agent · `wake stop` · `wake status` | **Built.** Stop closes stdin; hard kill is behind the daemon, not the CLI |
| DM at full Claude Code fidelity | **Built** — markdown, tool calls, diffs, scroll, interrupt |
| Group chat, filtered | **Built** — the room, its filter, both sidebars |
| `@name` routing, manager default, broadcast, mention toggle | **Built** — routing and broadcast in Phase 2, the manager in Task 15, the `⌃T` mention toggle 2026-08-12 |
| Attention-ranked roster with task roll-up | **Built** — blocked first, then working stalest-first, then idle |
| Bounded grid: columns, split-down, solo, board | Not built |
| Session importing — discover, pick, adopt | **Built** 2026-08-12 as `wake import`, as a fork rather than a resume |
| Groups sidebar | **Built** — workspaces with real unread |
| Inline permission / plan / question approval | Wire exists both directions (`FrameAllow`/`FrameDeny`); no UI |
| **Shift+Tab mode cycling** | **Removed, deliberately** — see `decisions.md`. It moved a label and changed nothing, failing silently in the unsafe direction. Needs a frame field, a session setter and a daemon verb; all three or the indicator lies again |
| Mouse throughout; undo for destructive actions | Wheel scroll only. bubbletea v1.3.10 has no wheel-only mouse mode, so any mouse costs drag events |
| Profiles, first-run generation, `!cmd` | Not built |
| Manager with MCP tools | Not built — shape is an open design question |
| cmux status pills when present | Not built |

## Also discussed, not in §17

- **Random names + a task label** — **built**. `<name> <> <label>`, label from the git branch,
  falling back to the directory basename. The `<>` separator is confirmed (§5 renders `▸`; the
  original brief said `<>`, and `<>` won).
- **`⌘⇧D` to open a DM below**, cmux-style, and multiple DMs. Not built; belongs with the grid.
- **A configurable toggle for send-to-one vs send-all.** Not built; belongs with routing.
- **Inherited cmux keybindings**, so cmux users transition without relearning. Not built. Note
  the legend is now enforced — a key may not be advertised unless it is bound, checked in both
  directions by `go/ast`.
- **Worktrees as a per-user config**, not a default. Not built.
- **Pixel icons, up to 16.** §17 defers to block glyphs. Still right.
- **Claude's palette** — grey, black, orange, maybe yellow. Built in `internal/ui/theme.go`.
- **Wake must be cheap to leave open.** Added as a **non-negotiable** 2026-08-09. Measured wins so
  far: 4.5× less CPU on the draw path under load, nothing added at idle. The outstanding cost is
  the daemon's ~86,400 `ps` spawns a day at 30 agents — still arithmetic, not a measurement.

## The two later-phase commitments worth not forgetting

- **Model-agnostic.** The airlock exists for this and nothing else: one package knows Claude's
  JSON, so a Codex port rewrites four files. It is now **enforced by a test**, not by discipline.
- **A native frontend.** §17 puts SwiftUI out of v1. The daemon boundary is what keeps it
  possible: the TUI is a client over a socket, and a second client is a second client.

---

## Ordering, as it stands

1. **The room** — the product's reason to exist, and the design decisions are with the owner now.
   Brief at `docs/superpowers/specs/2026-08-09-room-design-brief.md`.
2. **One resume spike, which unblocks three features at once.** Park/wake, session importing (b)
   and fork all rest on the same unrecorded ground: what `--resume` accepts alongside Wake's
   seven flags, whether `--fork-session` can be combined with `--session-id`, and whether a
   resume collides the way a duplicate `--session-id` does. About an hour of recording answers
   all of it, and this project does not design around unrecorded behaviour.
3. **Fork** — cheapest of the three once the spike lands. One verb, no picker, no discovery UI,
   and the identity work already shipped the `<who>` resolution it needs.
4. **Attention and routing** — cheap once the room exists; the liveness states are already there.
5. **Park / wake**, then **session importing** — both built. Importing landed 2026-08-12 as a *fork* rather than a resume, for the reason in its own section above.
6. **The permission mode mechanism** — small, and it un-removes a control the product wants.
