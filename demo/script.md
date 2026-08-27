# The demo film — shooting script

What was actually shot, in order. Timecodes are the built master's.
Re-record one beat with `vhs tapes/<n>.tape`; re-cut with `./build.sh`.

---

## What changed once it met the build

Three things in the plan were wrong about Wake and were corrected against the
source rather than shot as written. They are recorded here because each one
would have put a false claim on camera.

**An unaddressed message does not broadcast.** `internal/core/router.go` has
`BroadcastName = "all"`, and a draft with no `@` goes to the **manager** — the
comment says why: a manager told to report on the fleet by a message it also
received has been handed the same job twice. So the film's central beat is
`@all`, and the caption says both halves.

**`⌃Q` then `wake` does not bring the fleet back.** A daemon restores nothing:
parked sessions are addressable through `/resume` and are otherwise absent, so
"park the fleet, walk away, come back" would have shown an empty room. The
beat became **`⌃O` detach**, which genuinely does what that beat promised —
and is the stronger claim, because the agents keep *working*.

**The board and the manager's fan-out were added.** `/board` had landed on
`main` after this branch's base, and "tell the agents working on X to do Y" is
not a tool — it is `list_agents`, a filter, and one `send_to_agent` per match.
Both are shot as they really behave.

---

## The beats

| # | Beat | Keys | The claim | Caption |
|---|---|---|---|---|
| 0 | **Title** | — | — | *Wake — a terminal for running a fleet of Claude Code sessions* |
| 1 | **Hook** | — | A fleet already in motion, unexplained for three seconds | Six Claude Code sessions. One room. |
| 2 | **The room opens** | `wake` | One command; every session is in it | `wake` opens the room. |
| 3 | **Broadcast** | `@all …` | Type once, six agents move, each answers in its own voice | Type once. Every agent hears it. |
| 4 | **One agent** | `@maya …` | One answers, one unread badge, the rest stay still | `@name` when you mean one of them. |
| 5 | **Triage** | `⌃X` | The blocked agent ranked itself to the top and turned amber | The roster ranks by who needs you. |
| 6 | **The answer** | `a` `↵` | Settled in the pane that raised it, turn still on screen | Answered in the pane that asked. |
| 7 | **A conversation** | `⌃D` `/` | Status bar, folded tool run, the session's own commands | Open one and you're in Claude Code. |
| 8 | **The grid** | `⌃Y` `⌃B` | Three columns, the third split once; the room is column zero | Columns, each split once. |
| 9 | **Fork** | `⌃F` | A new agent carrying the conversation so far | Branch a conversation mid-thought. |
| 10 | **The manager** | `@manager` | A rollup: working, blocked, done, and what waits on you | Every room seats a manager. |
| 11 | **Fan-out** | `@manager` | It lists the fleet, filters, and sends to each match — for real | "Tell the agents working on the api…" |
| 12 | **The board** | `/board` | The whole fleet, one row each, state and last line | Thirty agents don't fit in panes. |
| 13 | **Leaving** | `⌃O` `↵` | `Detached. 7 agents still running.` then `wake status` proves it | Leave, and they keep working. |
| 14 | **End card** | — | — | *Run a fleet of Claude Code sessions like a team, not tabs.* |

Beat 13's caption sits in a **top** band: the legend at the bottom swapping
`↵ send` to `↵ detach` is the thing being demonstrated, and the usual bottom
band would cover it.

## The cast

Names are Wake's own pool (`internal/daemon/names.go`), not invented. Every
agent carries a `/task` label, which is what heads its lines, fills the roster
and the board, and is the only handle the manager has for "the agents working
on the api" — the listing clips a long label, so the area leads it.

| Agent | Label | Carries |
|---|---|---|
| maya | `api · token-bucket middleware` | The longest turn; the metrics fan-out |
| omar | `web · 429 client state` | **Blocks on a permission** — the ⌃X and card beats |
| priya | `cli · --rate-limit flag` | The grid's second column |
| luca | `docs · changelog and limits page` | Finishes early and stays quiet |
| nora | `api · limiter tests` | A subagent dispatch; the fan-out |
| alex | `api · limiter benchmarks` | The fork target; the fan-out |
| manager | — | Seated by default in every room |

## The edit

Recorded at 2400×1320, delivered at 1920×1080 (scaled to 1056 and padded on
Wake's own ground). Captions are rendered PNGs composited by ffmpeg — an
opaque band rather than a gradient scrim, because the transcript fills the
pane and terminal text reads straight through anything softer. The band also
covers the legend, awareness strip and notice rows, which is where a staging
artifact lands.

Hard cuts throughout; a fade in at the head and out at the tail. **Silent by
design** — captions carry the argument, which is how a feed plays video. To
score it:

    ffmpeg -i wake-demo.mp4 -i track.m4a -c:v copy -shortest -c:a aac out.mp4
