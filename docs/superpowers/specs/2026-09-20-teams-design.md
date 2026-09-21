# Teams — named tags that group and route the fleet

**Status: in progress on `feat/team-tags`.** Phase 1 (the tag through the stack)
is built and green. Routing and rendering follow.

This is the realization of spec §11's "configured groups", which
`internal/ui/groups.go` calls itself the v1 stand-in for, and the owner request
recorded in `docs/notes/deferred.md` (2026-08-16, held pending two rulings).
The owner greenlit it 2026-09-20 with the four scope decisions below.

It stays inside the non-negotiables because **the room stays one**: teams section
the roster and board and fan out `@team`, but they do not split the group chat.
Splitting the room into per-team surfaces is the separate "multiple room surfaces"
concept `deferred.md` keeps behind a non-negotiable — teams do not cross that line.

## What a team is

A **team** is an operator-chosen tag on a session: one session belongs to at most
one team. It is Wake's own grouping of the fleet by hand, the way `/color` is the
operator's grouping by eye — set from the client, never derived, and never told to
claude (it reaches no argv). Teams are **implicit**: a team exists exactly while at
least one live session carries its tag; `/team infra` creates "infra" on first use
and it stops drawing when its last member leaves or is retagged. `/team none`
clears an agent's tag.

The tag does three jobs, and the fence follows from them: it **heads a section** in
the roster and board, and it is **addressed as `@team`**. So a team name is one
lower-case token of the mention charset (letters, digits, dash, underscore),
bounded, with `none`/empty clearing — `rpc.NormalizeTeam`, `NormalizeColor`'s shape
over a free-text token rather than a closed set.

## The four scope decisions (owner, 2026-09-20)

1. **One team per agent.** A comparable `string` on `ui.Agent`, exactly as `Color`
   is — not a slice/map, which would break `Fleet.Observe`'s `now == was`.
2. **The manager and un-tagged sessions pin at the top**, in a header-less block,
   above the team sections.
3. **`@team` routes** — a message to `@backend` fans out to every live backend
   member. In scope now.
4. **Both board modes are sectioned** — the row-per-agent overview and the tiled
   live wall.

## Section order vs attention rank (the one real cost)

**Team sections are drawn in creation order; attention rank applies within each
section.** This is decision Q1(a), and it has a cost worth stating plainly.

Today the roster's whole point is that the most-urgent agent floats to the absolute
top. With fixed-order sections, a `needs-you` agent in the third team floats only to
the top of *its section*, below the header-less block and any earlier team — so
"blocked is on the first screen" no longer holds. This weakens a load-bearing tell:
now that the room pins no ask card (`App.cardOf("")` is false), the only always-on
tells for a blocked agent with its conversation closed are the roster row and the
awareness strip's `N need you`, plus `⌃X` (next-blocked). Sectioning weakens the
first of those.

It is accepted because:
- (b) "float a team with a blocked member to the top" does not rescue it either — a
  floated team still sits under the un-tagged block, which at 15+ un-tagged agents is
  already past the fold; and floating a whole section is a much larger, more frequent
  layout jump than one row moving, against a codebase whose stated value is roster
  stability (`attention.go`'s tie rule exists to stop rows swapping between frames).
- `⌃X` is unaffected by roster scroll: `App.nextBlocked` walks blocked agents and
  **opens** the agent's conversation, where the card draws — no window-scroll needed.
- The awareness strip's `N need you` count is independent of roster position.

**Implementation:** `Rank` the whole fleet once (as `Fleet.OnRoster`/`Agents`
already do), then **stable-partition** by section. `attention.go` stays pure and
untouched; within-section order is exactly today's order restricted to that team.
Feed the sectioned slice to `⌃X` (walk order = draw order). One cheap mitigation to
keep: the strip's `N need you` should stay un-truncatable at narrow widths, since it
is now the only always-visible cross-section tell.

## Team order is the daemon's, shipped on the report (Q3)

"Creation order" cannot be derived client-side: the daemon's report is sorted by
session UUID and `ui.Fleet.order` is first-*seen* order, so two clients — or one
after a reattach — would draw different section orders. And deriving from "oldest
current member" breaks stability: an unrelated stop changes which member is oldest
and reorders sections. So the **daemon holds `teamOrder []string`** under the same
lock rename/label/color take, appends a team on its first `/team`, and ships the
ordered list on `rpc.Status`. The UI draws only sections with ≥1 live member; empty
teams are pruned in **one place** when the report is built.

**Not persisted.** "Wake owns almost no state" permits a groups file (it lists
"groups" among what Wake may store), but the stronger rule is "a daemon restores
none of it": a team-order file read at startup would be restored live state for
teams with zero live members. In-memory daemon `teamOrder` is the name pool's kind
of state, not persisted state — a daemon crash loses live tags exactly as it loses
live colours (`roster.go` deliberately keeps `Color` out of the roster file; `Team`
is kept out for the same reason). **Known gap:** after `⌃Q` → `wake` → `/resume`,
teams reappear in resume order, not original creation order. Recorded in
`deferred.md`.

## Header style (Q2)

A centered rule, `──── backend ────`, drawn in both surfaces through the existing
`titledEdge` (`titledbox.go`) — no second rule-drawer, which "no parallel
implementations" refuses; `lastReadLine` is the other labelled-rule precedent. An
optional member count rides on the right, **dropped whole** when the width is tight
rather than cutting the name (`headLine`'s own budget ruling). The count is derived
at draw time from the partition, never stored.

## Phase 1 — the tag through the stack (built)

Mirrors `/color` end to end:
- `rpc.NormalizeTeam` + `rpc.TeamNone` + `rpc.FrameTeam` (in `rpc/team.go`, since
  `wire.go` is at the 800-line hard max).
- `rpc.SessionStatus.Team`, `core.Config.Team`, `daemon.agent.team`,
  `daemon/team.go` (`setTeam`/`teamSession`, refused for parked/ended via
  `renameableStates`), the tag on `snapshot` and in `parkedRecord` (captured by
  `recordFor`, restored on both wake paths — `unpark` and `unparkRecord`).
- `ui.Agent.Team` folded in `Fleet.WithStatus`; `App.teamAgent` (`/team`, `@who`
  bridge via `roomTargetCommands`, `/team none`).
- Guard rulings: manager refused `FrameTeam` (`managerVerbs`); `agent_status`
  withholds `Team` (`notInTheStatusReport` — the reversible default; whether to
  expose it read-only since it routes work is the owner's open question); `Team`
  not agent-authored (`agentAuthored`); counts bumped.

## Phase 2 — `@team` fan-out routing

Resolution stays pure in `core.Resolve` (`router.go`); `App.route` is its only
caller (`TestNothingRoutesARoomDraftExceptTheOneFunctionThatDoes`). A `Team` field
on `core.Addressee` carries membership in; precedence is **agent-name → team →
service → passThrough**, so an agent name always wins a collision (the header's
"a live agent name wins" rule), and `@all`/`@manager` are unreachable as teams.

Rulings (each gets a test):
- A team route is a **scoped broadcast, not a single-agent mention.** It must not set
  `roomRoute.mentioned` (which three readers take to mean one agent: `retarget`,
  `mentionCommand`, `renameMirrorFor`). Because a team name is not a live agent name,
  `fleetMention` is false by construction and the route falls out like `@all` — the
  right shape. A new marker on `core.Route` carries the team identity so the composer
  target line reads `→ @backend · 3`, not `@all` (`Broadcast` means `@all`; core owns
  that meaning). The `@team` is stripped from the sent text, as `broadcast()` does for
  `@all`.
- **Open mention mode does not widen a team send** — `@backend hi` in `⌃T` open mode
  reaches only backend, because `mentioned` is false. Correct (widening a team send to
  everyone defeats the team); pinned with a test and a one-line ruling.
- **`@team /command`:** an ordinary/claude command (`@backend /compact`) is an explicit
  fan-out to N agents, like `@all /compact` (this is not open mode's implicit widening,
  which the 2026-09-01 "never widen a knob" ruling is about). A Wake target-command
  (`@backend /color`, `/team`, `/name`, `/task`) is **refused in v1** with a notice
  naming the per-agent form — otherwise it falls through to N claude processes as the
  literal text `/color …`. **Never** team-wide `/quit` (stop "may not be aimed by a
  roster cursor", and a set is worse than a cursor).
- **Collisions are refused daemon-side, both directions**, since only the daemon sees
  both sets: a `/team` that equals a live agent name, and a `/name` into an existing
  team name, are refused; `all`/`manager`/`none` are reserved.
- Room narrowing: a lone `@team` does **not** narrow the room in v1 (that is the
  per-team filtered room `deferred.md` keeps behind the multi-room door). The team
  echo's `to` stamp: a team send stamped `to == ""` would show inside every narrowed
  `@john` thread; stamp it to members or record the gap.
- The manager is refused a team of its own — spec §12 gives it a seat in every group.

## Phases 3–6 — ordering and rendering

- **Phase 3:** daemon `teamOrder` + `rpc.Status.Teams`; a `Fleet` query that returns
  sections (header-less top block, then teams in order), attention-ranked within,
  from the stable-partition above.
- **Phase 4 (roster):** section headers via `titledEdge`; teach `rowsFor`, `window`,
  `At` (returns `ok=false` on a header row) and `Move`/`walkable` that header rows
  belong to no agent — **one row-counting function**, or a click lands one header off
  (the class of bug behind the pinned-card and room-selection offsets). Test: click
  the row under a header.
- **Phase 5 (board rows):** the same, over `boardRowHeight`/`boardRowFrom`/`boardHit`.
- **Phase 6 (board tiles):** per-team tile shelves; `tileGridFor`/`tileNav`/
  `tileWindowStart`/`boardHit` paginate per section rather than over one flat grid.

Every existing row and tile renders byte-identically; the only additions are the
header lines and the grouping they imply.

## Deferred / open questions

- Expose `Team` read-only to the manager (it routes work)? Owner's call; withheld for
  v1.
- Team order across a full daemon restart reappears in resume order, not creation
  order (not persisted, by the "restore nothing" rule).
- A team-of-one send is byte-identical to a private DM turn on disk, so it does not
  restore into the room; a send to ≥2 members does (multiplicity), consistent with
  `roomhistory.go` erring toward silence.
- `@` completion should offer team names (else `@back⇥` completes a path).
- A lone `@team` narrowing the room (the per-team filtered room) — the multi-room
  door, left closed.
