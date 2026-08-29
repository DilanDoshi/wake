# The board grows a second view: a live wall of tiles

**Status:** design, approved 2026-08-27. Implements `deferred.md`'s 2026-08-26 entry *"a second
board view, tiled rather than rows"* — and **reverses the §2c ruling it was blocked on**, under four
guardrails that keep it a status wall rather than the multiplexer the non-negotiables refuse.

## 1. The ask, and the ruling it reverses

`/board` today is one row per agent: name, state, activity, last line, in attention order. The owner
wants a second presentation of the same fleet — **a grid of tiles**, one per agent, each showing not
just the settled fields a row carries but the agent's **live output as it is written**, its **state**
(idle / working / needs-you), and a count of the **subagents it has running**. Bordered tiles,
rounded edges. A wall you glance across to see the whole fleet working at once, and jump into one
from.

This sits against a ruling already on record, and the reversal is deliberate. `board.go`'s header and
the 2026-08-12 phase-4 scope call (§2c) both say the board is *"an OVERVIEW, not panes. No
transcripts, ever … a tiled grid of thirty conversations is unreadable by arithmetic, and chasing it
is the multiplexer the non-negotiables refuse."* A tile showing live output *is* a small transcript,
so the row-only board was the safe reading and the tile view was parked pending an owner ruling.

**The owner's ruling, 2026-08-27:** a tile view is in scope, because the thing §2c actually refuses
is *panes you operate inside* — transcripts you scroll and stdin you type — not the visual shape of a
cell. A view-only tile with a bounded live tail, no scrollback and no input, is still a surface you
*act from*, not one you work in. So "board is an overview, not panes" narrows to **"board is an
overview, not panes you operate"**, and §2 of the design's non-goals ("not a multiplexer") holds
because the four guardrails below are what a multiplexer would have to break.

## 2. The guardrails — what keeps this a status wall

These are the feature's definition, not its trimmings. Cross any of the first three and it is the
multiplexer §17 lists as Out.

1. **View-only.** No keystroke ever reaches an agent's stdin from a tile. Keys drive the board —
   move the cursor, jump in, park, close. `↵` is how you leave the wall *into* a real DM to work.
2. **Bounded live tail, no scrollback.** A tile shows live text bounded to its own body and
   nothing you can scroll back through. The 2026-08-28 fill-the-window change narrowed this from
   the DM preview's fixed three-row bound (`maxPreviewRows`) to *the cell's own height*: a big
   cell fills with output, kept to `maxTileTailRows` per agent so the retention stays bounded and
   the per-token work flat. Still a `tail -f` snippet, never a conversation — no history, no scroll.
3. **Fixed grid, no per-tile resize, no pane tree.** Equal cells, sized to fill the frame — a
   near-square grid that auto-resizes with the window rather than a wall of fixed tiny tiles. No
   divider to drag inside the board, no split, no nesting. The bounded grid of the real panes is
   elsewhere; this is a dashboard.
4. **Act from it, never in it.** `↵`/click jumps into the agent's DM in the real grid; `⌃C` parks;
   `esc` closes. The tile is a preview you leave.

## 3. Entry and mode

`/board` opens the **rows** view exactly as today — the cheap default, the fast glance, the one a
narrow window keeps. A single key toggles rows ⇄ tiles while the board is up: **`⇥`**, read as
"switch layout". It costs no legend slot — the board advertises its own keys on `boardKeyLine`, which
brackets nothing and so is exempt from the card-key bijection guard.

Mechanically this is a `Tiled bool` on the existing `Board` model and a branch in `boardView`, not a
new modal — `deferred.md`'s own "a second render function switched by a mode field on the same
model". The toggle is not persisted: `board.go`'s statelessness holds ("Wake opens it, Wake closes
it, the daemon never hears of it"), so every open starts in rows.

## 4. The tile

A rounded border; a header of liveness glyph + name + state; then content chosen **by state**, which
is today's `boardDetail` logic laid into a box instead of a row:

```
╭─ ◐ john ───────────────╮   ╭─ ● sydney ─────────────╮   ╭─ ○ marcus ─────────────╮
│ working · Edit(auth.ts) │   │ NEEDS YOU · question   │   │ idle                   │
│ …tightening the guard   │   │ Allow write to .env?   │   │ done · shipped the     │
│  so a nil never reaches │   │                        │   │  migration             │
│ ⤷ 2 subagents           │   │ ⤷ 0 subagents          │   │ ⤷ 0 subagents          │
╰─────────────────────────╯   ╰────────────────[live]──╯   ╰────────────────────────╯
   working → live tail            blocked → the ask            idle → settled last line
```

- **working** → the live output tail (§6). This is the one new source; everything else the tile
  draws already exists on `Agent`.
- **blocked** → the tool it is blocked on (`ag.Tool`), `boardDetail`'s own field, so a blocked tile
  is what the eye lands on. The mockup's fuller "Allow write to .env?" is aspirational: the card
  subject is not on `Agent`, so showing it would need the card made reachable from the board — a
  listed option (§9), not assumed by this design.
- **idle / ended** → the settled `LastLine`, as the row draws it.
- **subagents** → one count line, `⤷ N subagents`, from `Fleet.RunningTasks`. Not a list and not
  nested tiles: the owner's call is that a tile stays small so more of the fleet fits, and the
  *which* comes from jumping into the DM, where the right sidebar already lists them.
- A **blocked** tile takes the accent/warn border, so "needs you" is legible in a wall of thirty.

Every agent-authored field on the tile — `Doing`, `Tool`, `LastLine`, the live tail — passes through
`oneLine` at the draw, `boardRow`'s own rule: a control byte in prose must not redraw or forge a
tile, and a forged tile impersonates a fleet state.

## 5. The grid

Attention-ordered, the roster's order, so blocked agents sort to the top-left. Filled left to right,
then top to bottom. Cells are equal, sized so roughly **three to four columns** fit a wide terminal;
the exact column count is a pure function of width and a fixed minimum tile width, a table test the
way `layout.go`'s breakpoints are.

- **A 2-D cursor.** The row board walks with `↑↓`; the grid adds `←→`, so `↑↓←→` move the selection
  among tiles. The cursor is still an agent id, not an index — the list re-ranks between frames, and
  an index would hand the cursor to whoever moved into the slot (`boardCursor`'s existing reason).
- **Windowing, not a scrollbar.** More agents than tiles that fit → the grid pages by *rows of
  tiles*, the cursor riding the edge, derived from the cursor rather than stored — `boardWindowStart`
  generalised from one dimension to two. The title still names the whole fleet count, so a window
  showing fewer tiles is never silently the fleet.
- **Narrow degrades.** Fewer columns as the terminal narrows, down to one, the same way the row view
  and the real grid's `dmTakeoverColumns` already degrade.

## 6. Liveness and cost

This is the part `partial.go` was written to protect, so the mechanism is spelled out and gated the
way that file demands.

**Where the tail lives today.** A live tail — the `partial` type: plain text, never glamour, bounded
to `maxPreviewRows` + `previewSlack`, cleared by the completed block or the turn ending — exists only
on an **open `DM`**. `App.wants` *drops* every `KindPartialText` token for an agent whose pane is not
drawn, and `DM.Leave` drops the tail on the way out. So there is no per-agent live tail for an agent
whose DM is closed, which is most of the fleet.

**What the wall adds.** A **second map on `Fleet`, keyed by session id**, holding each agent's
`partial` tail — the same shape and the same reason as `Fleet.tasks`: it cannot be a field on `Agent`
because `Agent` must stay comparable for `Observe`'s `now == was`, and a growing string breaks that.
The map reuses the `partial` type verbatim (`add`, `cleared`, `sized`, `wrapped`); no new rendering
path, no glamour.

**The gate.** Accumulation is gated on **the tiled board being up**, exactly as `App.wants` gates on
a pane being drawn — the same philosophy, "pay only for what is on screen", with "the wall is up" as
the on-screen condition. When the board is closed, or open in rows, the map accumulates nothing and
holds nothing. When the tiled wall is up, every working agent's tokens fold into its tail; a tile
reads its own. The clear is not gated (a block landing or a turn ending clears the tail whether or
not the wall is up), so a tile can never show a sentence that finished long ago — `partial.go`'s own
asymmetry.

**Cost, measured before it is locked.** The precedent is `BenchmarkStreamingFleetSecond`: thirty
agents streaming plain-text previews cost 7.4–8.3 ms per fleet-second, under 1% of a core. The wall
inherits that because it inherits the `partial` type. Two things could betray it and both get a
benchmark, red first:

- **The map must not be copied wholesale per token.** This is the `withDM` trap (530 MB/s of
  allocation, fixed once by keying on pointers). The tail map is mutated in place / keyed so a token
  copies a pointer, not a tail — the `withDM` fix's own shape, keys and copy-on-write discipline
  left to implementation but the outcome asserted. `BenchmarkTiledBoardFleetSecond` prices a
  fleet-second of thirty streaming agents with the wall up and holds the allocation flat.
- **Full-grid redraw at the token rate.** ~1,300 tokens/second across a fleet, each a potential
  redraw of thirty bordered tiles. The same benchmark measures the draw, and the tile `view` is
  cached on width/text change like the DM's preview and `DM.bar`, never per frame.

A number nothing asserts is wrong by default: neither figure ships as a claim, both ship as a test.

## 7. Opening

Click a tile, or `↵` on the cursored tile → **close the board and open that agent's DM as a new
column** beside the grid you had (`openRight`), focused; if that DM is already an open pane, focus it
rather than duplicate. The board is a full-frame modal drawn *instead of* the grid, and the grid
model is untouched while it is up, so closing reveals the same room and panes — "back to my previous
view" is free, and the new column is the only change.

This makes `↵` and click **add a column**, where the row board's `↵` opens *into* the focused pane.
So the placement keys shift by one, all still acting on the cursored tile:

| Key | Was (rows) | Is (both views) |
|---|---|---|
| `↵` / click | open here | **open in a new column** (`openRight`), focus if already open |
| `⌃D` | open here (synonym) | **open into the focused pane** (the old `↵`) |
| `⌃Y` | new column | new column (unchanged) |
| `⌃B` | split below | split below (unchanged) |
| `⌃C` | park cursored | park cursored (unchanged; stays open) |
| `esc` | close | close |

The unbounded-columns worry is not one: the real grid already windows many columns, and the bound
§8 names is "columns, each split once", not a column count. Pruning is `⌃W`, as ever.

## 8. Files, testing, and what gets written down

**Where it hooks in** — almost all of it is `internal/ui/board.go`:

- `Board` gains `Tiled bool`; `boardKey`/`boardMouse` learn `⇥` and the 2-D cursor; `boardView`
  branches to a tile renderer (`tileView`, `tile`, `tileColumns`).
- A tail map beside `internal/ui/fleettasks.go` (`Fleet.tails` or equivalent), folded in `apply`
  where `App.wants` already decides the DM's partials, gated on `a.board.Up && a.board.Tiled`.
- `internal/ui/mouse.go`'s board-click arithmetic learns tile geometry (2-D), the way
  `boardChromeRows` keeps the row view's click and draw measuring one number.
- `board.go`'s header comment is rewritten to record the narrowed ruling and the four guardrails, so
  the code and §2c agree.

**Testing** — TDD, red first:

- `frame_test.go` assertions for the tile layout, the 2-D cursor walk, and the by-state content.
- A pty screen test (`cmd/wake/screen_unix_test.go`) for the real tile render and click-to-column.
- `BenchmarkTiledBoardFleetSecond` (§6), the two figures as tests.
- A guard that the tail map accumulates **only** while the tiled board is up and holds nothing
  otherwise — the gate is the whole cost argument, so it is asserted, not assumed.
- The layout column-count as a width-in / columns-out table test, `layout.go`'s discipline.

**What gets written down** beyond the code: this spec; the rewritten `board.go` header; and, when the
feature lands, a one-line update to `deferred.md`'s 2026-08-26 entry marking it built and pointing
here, plus a `decisions.md` note that §2c was narrowed and why (so the next agent reads the reversal
as a ruling, not a regression).

## 9. Non-goals and open items

- **No input, ever.** Guardrail 1 is the feature; a tile that types is a different, refused feature.
- **No nested subagent tiles.** Settled for the count line (§4); the richer readings were considered
  and dropped for space and for the guardrails.
- **No live tail in the *row* board.** The row view stays settled-only and cheap; liveness is the
  tile view's reason to exist, and gating on `Tiled` keeps the row board's cost exactly what it is.
- **Fuller ask text on a blocked tile is an option, not a commitment.** §4 draws `ag.Tool`; showing
  the card subject ("Allow write to .env?") would mean making the outstanding card reachable from the
  board. Cheap, but out of this design's scope unless `ag.Tool` proves too thin in use.
- **Column count and minimum tile width** are a table-tested function of width; the exact minimum is
  set in implementation against the pty harness, not guessed here.
