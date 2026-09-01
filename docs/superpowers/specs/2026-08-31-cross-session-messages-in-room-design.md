# Cross-session messages in the room — design

**Date:** 2026-08-31 · **Status:** approved, implementing · **Branch:** `feat/cross-session-room`

## Problem

Claude Code has a built-in peer channel: one session sends a message to another and it lands in the
recipient's context wrapped as `<cross-session-message from="uds:/tmp/cc-socks/<pid>.sock"
from-name="<sender>" from-mode="<mode>">…</cross-session-message>`. When a Wake agent receives one,
nothing appears in the room — the operator supervising the fleet never sees that an agent was told
something by a peer. The owner wants it rendered in the group chat with the **sender's name** (and
its identity colour when the sender is a fleet agent), and folded (`⌃E`/click) when long, **the
instant it arrives**.

## Evidence (probed 2026-08-31, Claude Code 2.1.252)

Two headless `--print --input-format stream-json` sessions, one messaging the other over the
`cc-socks` channel, recipient stdout captured:

- **Without `--replay-user-messages`:** the message reaches the model's context and the on-disk
  transcript (as a `user` frame with `isMeta:true`, `userType:"external"`), but **never appears on
  the recipient's live stdout** — zero `user` frames. The room, fed by the live stream, is
  structurally blind to it. This is the documented no-echo behaviour.
- **With `--replay-user-messages`:** the message **does** replay onto live stdout as a `user` frame
  carrying the same `<cross-session-message …>` envelope, with `isReplay:true` **and**
  `isSynthetic:true`. An ordinary Wake-sent stdin turn, replayed under the same flag, carries
  `isReplay:true` (and *not* `isSynthetic`).

This resolves the question event.go marked unobserved ("whether `--replay-user-messages` even sets
`isReplay`" — yes) and is the whole basis for the containment below. The recording will be captured
sterile and committed as a fixture; findings note in `docs/superpowers/notes/`.

## Decision: enable `--replay-user-messages`, reversing a documented choice

"Instant" is non-negotiable, and the live stream is the only surface that is instant. So Wake will
pass `--replay-user-messages` to every agent (`internal/core/argv.go`, `buildArgs`). This reverses
the "Wake emits all but `--replay-user-messages`" line in CLAUDE.md's CLI table; that line and the
decisions/notes get updated in the same PR with the evidence above.

**Why it is safe for regular usage.** A replayed frame carries `isReplay:true`, which the airlock
already folds to `Echoed:true` (`frameEvent`), and `typedByHand` already returns false for an
`Echoed` frame — so the room's existing `fold` (`fleet.go`, `if !typedByHand(ev)`) **already drops
every replayed frame**. Turning the flag on therefore changes nothing the operator sees for ordinary
sends; their single source stays the App's local echo (`sendRoom`/`echoToRouted`). The added stream
volume is negligible — user turns are rare, not the token firehose `--include-partial-messages`
already carries.

**Respecting event.go:646.** That ruling forbids keying suppression/de-duplication on `Echoed`,
because a sent message must appear as *either* the replayed frame *or* the local echo and never both,
and that single-source decision belongs to the App. This design honours it: the admit for a
cross-session message is keyed on the **envelope** (a real distinguishing property present on both
the live and on-disk wires), not on `Echoed`/`isReplay`/`isSynthetic`. A cross-session message has
exactly one source (the replayed frame; Wake never sent it, so there is no local echo to double it),
so no de-duplication is involved at all.

## Architecture

### 1. Airlock — a new event kind (`internal/core`)

`frameText` already unwraps pure wire markers on the user's side (the interrupt marker →
`NoticeTurnInterrupted`). Add the cross-session envelope the same way: when a `user` frame's text is
the `<cross-session-message …>` envelope, emit **`core.KindCrossSession`** carrying:

- `Event.Text` = the inner body (envelope stripped),
- sender attribution = the `from-name` value, and the `from` address.

Sender fields ride the `Event`. Options considered: new fields (`Event.FromName`, `Event.FromAddr`)
vs. reusing something. New fields are clearest and are presentation-only (like `FromRoom`). The kind
is Wake vocabulary, added in `event.go` and made total everywhere a kind is switched
(`vocabulary.go` if a table names it). This lives in the airlock because the envelope is pure wire
format with no argument on the other side — the airlock's own rule.

Because the decode is in the **shared** path (`messageEvents`/`frameText`, reached by both
`DecodeLine` and `DecodeTranscriptLine`), a cross-session message decodes to `KindCrossSession` from
the **live stream** (instant, via the flag) *and* from **on-disk reads** (DM reopen, room resume) —
one decoder, no second path.

### 2. Room fold admits it (`internal/ui/fleet.go`, `observe.go`)

`fold` currently drops all but `typedByHand` user text. Add a case: `KindCrossSession` is admitted to
`forRoom` (and carried to the DM), attributed to the **sending** agent. In `observe`, the room
`Append` uses the sender — resolved by `Fleet.ByName(fromName)` for its identity colour when it is a
fleet agent, else a neutral "external" style with the raw `from-name`.

### 3. Room render — attributed + collapsible (`internal/ui/chat_blocks.go`)

`roomBlock` routes `KindUserText`→`youSaid` (headless, never folds). Add a `KindCrossSession` arm
that heads the block with the **sender's name** in its colour (reusing `speakerStyle`/`identityStyle`
for a fleet sender; a neutral style otherwise) and renders the body, and make `roomCollapsible`
return true for it so the existing `roomInlineRows`/`collapsed` folding (`⌃E` global, click
per-line) applies to a long one exactly as it does to an agent turn.

### 4. DM single-source (`internal/ui/observe.go`)

The DM draws exactly what it is fed (`dm_blocks.go` header: the feeder picks one source). Under the
flag, replayed copies of Wake's own sends must not reach the DM (the local echo is their source).
`observe`'s DM-append gains the same discriminator: drop `Echoed` `KindUserText` (the replayed
ordinary send), admit `KindCrossSession` (its sole source). Render in the DM with the sender's head
too.

### 5. Spawn flag (`internal/core/argv.go`)

Add `--replay-user-messages` to `buildArgs`. `argv_test.go` / any flag guard updated.

## Data flow

peer sends → recipient agent injects + (flag) replays `user` frame with envelope on stdout →
daemon `fanOut` broadcasts the frame unchanged → client airlock `DecodeLine` → `KindCrossSession`
(sender, body) → `observe`: `fold` admits → room `Append(ev, senderAgent)` and DM append →
`roomBlock`/DM render sender-headed, collapsible. On reopen/resume the same `KindCrossSession` comes
from `DecodeTranscriptLine`, so history agrees.

## Testing

TDD, red first.

- **Airlock** (`protocol_*_test.go`): a recorded sterile stream fixture (`testdata/stream/`) and a
  transcript fixture (`testdata/transcript/`) decode the envelope to `KindCrossSession` with the
  right sender name and stripped body; a non-envelope replayed user frame stays `KindUserText`/
  `Echoed`. `TestDecodeRecordedFixtures` must still pass; `corpus_test.go` guard satisfied via the
  scrubber.
- **Fold** (`fleet`/`observe` tests): `KindCrossSession` reaches `forRoom`; an ordinary `Echoed`
  user frame does not; the DM admits the cross-session one and drops the replayed ordinary send.
- **Attribution** (`theme`/`chat_blocks` tests): fleet sender → its identity colour + name; external
  sender → neutral style + `from-name`.
- **Folding** (`frame_test`/render): a long cross-session message collapses past `roomInlineRows`
  and expands under `⌃E`/click.
- **Screen** (`cmd/wake/*_unix_test.go`, real pty): a replayed cross-session frame lands a
  sender-headed line in the room.
- **Argv** (`argv_test.go`): `--replay-user-messages` is emitted.

## Non-negotiables respected

- Airlock stays four files; the new kind is decoded there and nowhere else.
- `attention.go` untouched; `fold`/render stay pure.
- No poll, no per-frame work added; the flag rides the existing stream.
- One implementation: extend `frameText`/`fold`/`roomBlock` in place, no parallel path.

## Refinements from review (2026-08-31)

The code review and an adversarial pass sharpened three points before merge:

- **Room resume dropped the line (real defect, now fixed).** The room's restore filter
  `collapseBroadcasts` (`roomhistory.go`) admits a non-`KindUserText` line only while its session has
  a proven-broadcast turn open, so a lone `KindCrossSession` was silently dropped on any room restore
  (`⌃Q`→`wake`→`/resume`, or reopening the room over a running fleet) — defeating the feature on the
  exact supervision surface it exists for. Fixed: `collapseBroadcasts` keeps `KindCrossSession`
  unconditionally (a first-class room event), and `roomHistoryLines` heads it with the sender
  (`from-name` alone; the identity colour is the live path's, dropped on a restore where the sender
  may not be running). Pinned by `TestACrossSessionMessageSurvivesARoomRestore`.

- **The discriminator is string content, not a flag (spoofing closed).** `crossSession` fires only on
  a user frame whose content is a bare string. `EncodeUserMessage` always writes *array* content, so
  every message Wake sends (operator, manager MCP, routed) replays as array content and never reaches
  `crossSession` — a message that merely contains the envelope cannot forge a peer line. Pinned by
  `TestAnArrayContentUserFrameWithTheEnvelopeIsNotACrossSessionMessage`.

- **The DM filter's true scope, stated honestly.** `replayedUserEcho` (renamed from `replayedOwnSend`)
  drops *every* replayed `KindUserText` from the live DM feed, not only the operator's own send: the
  manager's sends, a compaction summary and `<local-command-stdout>` are replayed echoes too. This
  keeps the live DM the conversation (consistent with the room's fold) rather than letting the flag
  flood it; the cost — those frames land in an *open* DM only on reopen (a disk re-read), with a
  compaction still announced live by `NoticeContextCompacted` — is taken deliberately. `FromAddr` was
  dropped as an unused field; `focusAdmits` now admits a cross-session line in either the sender's or
  the receiver's `@name`-narrowed thread.

## Docs updated in this PR

CLAUDE.md CLI table (`--replay-user-messages` now emitted), the "messages don't echo back"
assumptions in the room rulings, `decisions.md` (the reversal + evidence), a findings note, and the
Key locations row for the new decode.
