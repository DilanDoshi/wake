# Tiled board: full transcripts in the tiles

**Status:** design, approved 2026-09-01. Extends `2026-08-27-tiled-board-design.md` and **revises its
guardrail 2** — from *"a `tail -f` snippet, never a conversation"* to *"the last screenful of the real
conversation, no scrollback"* — under the owner's 2026-09-01 ruling. Bundles a daemon fix that carries
the observed model on the fleet report.

## 1. The ask, and the guardrail it revises

The tiled board (`/board`, then `⇥`) draws each agent as a bordered cell. Its body today is a
`tail -f` of **only the assistant text block being actively typed**: it shows live text while
`State == working` and text is streaming, clears the instant the block lands, and falls back to a
single `boardDetail` summary line the rest of the time. So the moment an agent runs a tool, waits on a
question, or goes idle, the tail is empty and the tile is one line in a wall of blank. Tool calls,
tool results, and finished prose never persist in the tile.

The owner wants each tile to read like a **real transcript**: the conversation's recent exchange —
prose, tool calls, diffs, results — following live as responses come in and **staying** afterwards.
The chosen behaviour (2026-09-01) is **transcript in every state**: idle tiles show the last exchange,
working tiles show text streaming in, blocked tiles show the transcript with the pending ask at the
bottom. One rule for all tiles, following the bottom.

**This revises guardrail 2 of the 2026-08-27 design.** That guardrail read *"Bounded live tail, no
scrollback … a `tail -f` snippet, never a conversation."* It now reads:

> **2. Bounded transcript window, no scrollback.** A tile shows the tail of the real conversation —
> the last screenful, rendered at full DM fidelity — bounded to its own cell body and with nothing
> you can scroll back through. Still no history you can page and no input; the window follows the
> newest line live. The retained render is bounded to the cell (`tileTailRows`), so per-token work
> stays flat.

Guardrails **1 (view-only)**, **3 (fixed grid, no per-tile resize, no pane tree)** and **4 (act from
it, never in it)** are untouched. No keystroke reaches an agent's stdin from a tile, the grid stays
equal cells sized to fill the frame, and `↵`/click still leaves the wall *into* the agent's real DM.
A transcript window that you cannot scroll, cannot type into, and cannot resize is still a status
wall — it is a richer read of the same fleet, not a multiplexer.

## 2. Scope

**Tiled grid only.** The row/list view (`boardView`'s non-tiled branch — `boardRow`/`boardDetail`) is
untouched: one row per agent with name, state, activity and last line, the cheap fast-glance default a
narrow window keeps. A full transcript makes no sense in a single row. Everything here is gated on
`board.Up && board.Tiled`, exactly as `App.tails` is today, so row mode costs nothing and `⇥` back to
rows drops it all.

## 3. Why it stays cheap — the cost model

The reason the board does not render transcripts today is that `internal/render` (glamour) holds **one
process-global mutex shared by every session**, so rendering 30 tiles per frame would serialise the
whole fleet's drawing (`partial.go` prices it at up to 65×). This design avoids that by leaning on a
fact already true of the DM pane:

- **A DM renders each block through glamour exactly once, at ingest** (`DM.Append`), and caches the
  rendered lines in `transcript.lines`. Drawing (`transcript.view`) is a slice of that cache; the
  expensive pass reruns **only on a width change**.

So a tile reuses that cached render. The work is bounded three ways, and each keeps the first
non-negotiable ("cheap to leave open") intact:

1. **Only while the board is open and tiled.** `boardDMs` is `nil` otherwise — zero cost in row mode
   and when the board is closed, the exact lifetime `App.tails` already has.
2. **Only the tiles on screen.** The grid already pages overflow through the cursor window
   (`tileWindowStart`), so at most `cols*rows` transcripts are ever live — a handful, not thirty.
3. **Once per block, not per frame.** Completed blocks feed the board DM through `DM.Append` (one
   glamour render each, cached); partial tokens go to the DM's plain-text `partial` preview (no
   glamour), the same path the pane uses. A frame just slices the cache.

The residual costs are all **per change, not per frame**: a one-time render when a board DM is first
built or paged onto, and a re-wrap when the tile width changes (a window resize). Board closed ⇒ no
board DMs ⇒ nothing.

## 4. Approach B — board-scoped transcripts

A new map on `App`, isolated from the pane world, mirroring `App.tails`:

```go
// boardDMs holds one rendered conversation per on-screen tile, built while the
// tiled board is up and dropped whole when it closes. Isolated from App.dms:
// the pane world (the ⇥ ring, App.wants, hideDM, focus) never sees these, so a
// board tile costs nothing anywhere a pane is counted.
boardDMs map[string]*DM
```

`*DM` because a board tile reuses the DM's whole event→rendered-transcript machinery (`Append`,
`Before`, `renderAll`, `transcript`), and the pointer keeps the map copy cheap (the `App.dms` rule).

### 4.1 Lifecycle

Built and extended in `App.apply`/`observe` while `board.Up && board.Tiled`; dropped in `closeBoard`
and on the `⇥`-to-rows toggle (`a.boardDMs = nil`, beside the existing `a.tails = nil`). This is the
same gate `foldTail` already checks, so the two live and die together.

- **On open-tiled, on `⇥`-to-tiles, on cursor page, on resize:** ensure a board DM exists for every
  **visible** agent (`ensureBoardDMs`). The visible set is `tileView`'s own window
  (`tileWindowStart` … `cols*rows`), computed from the one `boardTileGrid` the draw already uses, so
  the built set and the drawn set cannot disagree.
- **On close / toggle-to-rows:** `boardDMs = nil`. An agent whose tile is paged back on later is
  rebuilt fresh — the same "rebuild on the way back" the DM pane does for a subagent.

### 4.2 Seeding a board DM

Board DMs are **fully isolated from `a.dms`** — the whole point of Approach B — so they seed by **one
path, from disk**, never by cloning a pane DM. Cloning was considered and dropped: a clone taken while
the pane DM's own history read is still in flight would inherit an empty transcript and never receive
the reply (it folds into `a.dms`, not the clone), and reconciling that is more moving parts than a
uniform disk read. The redundant read for an agent that also has a pane open is a one-time local read
on board open, bounded to the visible tiles.

For each newly-visible agent, `ensureBoardDMs` builds `NewDM(id, name)` and seeds it through the
**existing** history wire and fold — `FrameHistory` on the socket, `DM.Before` for the answer. The
only thing that differs from a pane open is the bookkeeping of *which conversation asked*, and that is
**generalised rather than duplicated** (a non-negotiable): `askHistory`/`historyArrived` are
parameterised over the target map and its outstanding-ask set, so pane opens use `a.dms`/`askedHistory`
and the board uses `boardDMs`/`boardHistoryAsked` through the **same** functions. The board's
outstanding-ask set is dropped with `boardDMs` on close, so a board ask can never occupy the pane's
single-slot record and block a later pane open on the same id. `historyArrived` routes by whichever
set holds the standing ask; the two are mutually exclusive per id (the board is drawn *instead of*
panes, so no pane opens while it is up), so there is never a contest.

Without disk seeding an **idle** agent's tile would be blank (no live events to accumulate), which is
the original complaint; seeding is what makes "transcript in every state" real.

### 4.3 Live feed

A board fold, sibling to `foldTail` (which it replaces for the tiled case), routes each agent's events
into its board DM **while the board is up and tiled and the agent's tile is visible**:

```
foldBoard(sessionID, ev):
    if !board.Up || !board.Tiled: return           // gate, like foldTail
    if boardDMs[sessionID] == nil: return           // not a visible/seeded tile
    boardDMs[sessionID] = boardDMs[sessionID].Append(ev)
```

`DM.Append` already does the right thing with every event kind: partials → the plain-text preview,
tool uses/results → the folded run, assistant text → a rendered block, checklist ops → the pinned
board, `/clear` → blank. So the board tile gets the pane's exact fidelity for free. The gate on
`boardDMs[id] != nil` is what bounds the render to visible tiles: a paged-off agent has no board DM,
so its blocks are not rendered until the cursor pages it back and `ensureBoardDMs` seeds it from disk
(current as of that read).

`foldTail` and `App.tails` are **removed** — the transcript window supersedes the single-block tail
entirely, so keeping both would be two mechanisms for one job (a non-negotiable). `partial` stays: it
is the DM's own streaming preview, now reached through the board DM rather than a standalone tail.

### 4.4 Rendering the tile

`tileMiddle` stops branching on state. It draws the board DM's transcript window at the cell's inner
size, following the bottom, for every state:

```
tileMiddle(ag, inner, rows):
    d := boardDMs[ag.ID]
    if d == nil: return padRows(nil, rows)          // not seeded yet (async history in flight)
    d, body := d.transcriptWindow(inner, rows)      // re-wrap if width moved, size, toBottom, view
    boardDMs[ag.ID] = d                              // keep the re-wrap
    return splitRows(body, rows)
```

`DM.transcriptWindow(w, rows)` is a new method that reuses the transcript machinery without the
composer chrome `DM.SetSize` assumes:

```
transcriptWindow(w, rows):
    if w != d.width: d.width = w; d.tr = d.tr.replace(renderTranscript(d))
    d.tr = d.tr.sized(w, rows).toBottom()
    return d, d.tr.view(marked{})                    // no selection inside a tile (view-only)
```

The streamed `partial` preview rides at the bottom when the agent is mid-block and there is room,
exactly as the DM pane places it under the transcript — so "responses coming in" animate. The
per-tile framing (state word, `⤷ N subagents`, status bar) is unchanged; only the middle changes from
tail/`boardDetail` to the transcript window. Every rendered line still goes through `oneLine`/truncate
before it joins the cell, the tile-forgery guard `tileBody` already documents (tiles sit side by side
via `JoinHorizontal`).

`tileTailCap` is renamed `tileTailRows` and keeps its job: how many transcript rows the cell body
retains (`cellH - tileFrameRows`), floored, bounded — guardrail 2.

## 5. Bundled fix — the model on the report

Independent of the transcript work and the reason two tiles show no model: the init-frame model rides
only the **live event stream** (`withFacts`, `f.Model` → `Agent.Model`), never the fleet report. A
client that has not witnessed an agent's `init` (doc-updater, manager — opened the board without their
turns crossing an init here) has an empty `Agent.Model`, and the status-bar segment is dropped. The
daemon's `a.model` is the **spawn alias** (`cfg.Model`), which is empty for a default-model spawn — so
carrying *that* would not fix a default spawn. The robust source is the model the daemon **observes on
init**, which it does not currently store.

The fix, which the `Agent.Model` comment already anticipates (*"Putting them on rpc.SessionStatus is
what would close that, and it is daemon work"*):

- `internal/daemon/agent.go`: a new `observedModel string`, folded from `ev.Session.Model` in
  `agent.observe` beside the existing `Dir`/`SlashCommands` folds (guarded non-empty, last-writer-wins
  — the same shape the UI's `withFacts` uses). The daemon supervises and fans out **every** session,
  so it observes the manager's init too.
- `internal/rpc/lifecycle.go`: `SessionStatus.Model string` (`json:"model,omitempty"`).
- `internal/daemon/agent.go` `snapshot()`: `Model: a.observedModel`.
- `internal/ui/fleet.go` `WithStatus`: fold `s.Model` into `a.Model` **only when non-empty**, so a
  report assembled before the daemon saw an init does not blank a model the event stream already
  gave. This is symmetric with the event path; last-writer-wins keeps them consistent.

This fixes the tile status bar for every agent and, as a bonus, every late-attached DM status bar. It
must not trip `notCarriedOntoAnAgent` (that guard is about the ending fields a report does *not* carry;
`Model` is carried on purpose).

## 6. Guardrail audit

| Guardrail | Status |
|---|---|
| 1. View-only | **Intact.** No key reaches stdin; `marked{}` passed to `view` — no selection in a tile. |
| 2. Bounded tail, no scrollback | **Revised** (§1): bounded transcript *window*, still no scroll, still no input. |
| 3. Fixed grid, no resize, no tree | **Intact.** Grid geometry unchanged; the tile body just draws different content. |
| 4. Act from it, never in it | **Intact.** `↵`/click still `closeBoard().openRight(...)`. |

## 7. Testing

TDD, failing test first, against the recorded corpus — never a live model.

- **Screen (pty, real binary):** a tiled board whose agent has streamed prose **and** a tool call
  shows *both* in the tile — a multi-row transcript, not one line. `cmd/wake/*_unix_test.go` with a
  fixture that carries an assistant block, a tool use and a result.
- **Screen:** an **idle** agent's tile shows its last exchange (seeded from disk), not a blank body —
  the regression the whole change targets.
- **Isolation/cost:** closing the board (and `⇥`-to-rows) drops `boardDMs` (`== nil`); a paged-off
  agent has no board DM, so its blocks are not rendered — a counter on `renderTranscript` (the seam
  exists for exactly this, like `drawStatusBar`) proves only visible tiles render.
- **Seeding:** a board DM for an agent with an open pane DM is a clone (no `FrameHistory` written);
  one without issues exactly one `FrameHistory` and folds the reply.
- **Live feed:** an assistant block arriving for a visible tile appends to its board DM and shows;
  one for a paged-off agent does not build a board DM.
- **Model fix:** `Fleet.WithStatus` folds `SessionStatus.Model` onto `Agent.Model`; a report with an
  empty `Model` does not blank an existing one; the daemon's `observe` stores `ev.Session.Model`; a
  tile for a never-witnessed agent shows the model once a report lands.
- **Benchmark:** `BenchmarkBoardTileSecond` — one second of a fleet streaming with the tiled board up,
  proving the cost is the visible-tile render integral and not a per-token glamour pass (the pairing
  `BenchmarkStreamingFleetSecond`/`BenchmarkOneBlockStreamed` model).

`make ci` exit 0 before the PR, plus the two mandated reviews (code + adversarial), per CLAUDE.md.

## 8. Files touched

| File | Change |
|---|---|
| `internal/ui/boardtranscript.go` | **New.** `App.boardDMs`, `App.boardHistoryAsked`, `ensureBoardDMs`, `foldBoard`, `visibleBoardAgents`; the map's lifecycle. Keeps `boardtile.go` under the small-file rule. |
| `internal/ui/boardtile.go` | `tileMiddle` draws the transcript window; `tileTailCap`→`tileTailRows`. |
| `internal/ui/dm.go` | New `DM.transcriptWindow(w, rows)` — re-wrap on width, size, `toBottom`, `view(marked{})`. |
| `internal/ui/board.go` | `closeBoard`/`⇥`-to-rows drop `boardDMs` + `boardHistoryAsked`; guardrail-2 header rewrite. |
| `internal/ui/observe.go` | `foldTail` → `foldBoard`. |
| `internal/ui/history.go` | Generalise `askHistory`/`historyArrived` over the target map and its outstanding-ask set — one implementation, two callers (pane, board). |
| `internal/ui/tail.go` | **Removed** — superseded by the board DM. The `partial` type is untouched (it lives in `partial.go`). |
| `internal/rpc/lifecycle.go` | `SessionStatus.Model`. |
| `internal/daemon/agent.go` | `observedModel`; fold in `observe`; carry in `snapshot()`. |
| `internal/ui/fleet.go` | Fold `s.Model` in `WithStatus`. |
| `CLAUDE.md` | Update the board and tiled-board rows to describe the transcript window. |

## 9. Risks and edge cases

- **Width thrash.** A DM sized to tile width, then drawn as a pane at pane width, re-wraps. Harmless
  here: the board is drawn *instead of* panes, so while it is up only tiles draw; the pane re-wraps
  once on close, the cost a width change already pays. A cloned board DM is a separate object from
  `a.dms[id]`, so sizing the clone does not disturb the pane's own width.
- **History race.** `historyArrived`'s existing drop-if-anything-arrived-since guard applies to board
  DMs unchanged; a board DM mid-stream when its reply lands drops the reply and shows the live tail,
  which is the safe side the pane already takes.
- **A tile seeded but not yet answered** (async disk read in flight) draws a blank middle for a frame
  or two, then fills — the same first-open latency a DM pane has. Acceptable; the framing (state,
  subagents, bar) is drawn immediately.
- **Small cells.** At `minTileWidth` the inner width (~22) is above `minBlockWidth` (20), so glamour
  still wraps; below it a line is truncated, `tailLines`' existing rule.
- **`/board` from a fresh `wake`** with parked-but-not-live agents: parked sessions are not on
  `OnRoster`, so no tile and no board DM — unchanged.
