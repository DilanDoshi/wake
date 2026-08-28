# `@john` as a room *view* filter — design

**Status:** design, awaiting owner review. **Scope:** typing a lone `@name` in the room narrows what
the group chat *shows* — down to the thread with that agent — not only where the next `↵` routes.
Today a leading `@name` governs **routing** and nothing about **display** (`deferred.md`,
"2026-08-25 — `@john` as a room *view* filter"). This is a second, orthogonal filter composed on top
of the room's existing fleet-wide filter, never a replacement.

**Decisions already taken** (owner, 2026-08-27 — the three open questions the deferred entry left):

- **Trigger is a live query off the composer, not a mode.** The room narrows whenever the composer's
  resolved target is a lone live `@name`, and un-narrows the moment that target changes or the draft
  clears. No new keystroke, no legend entry, no persisted mode — it reuses the per-keystroke target
  resolution the composer already computes (`App.retarget` → `addressedAgent`). Consequence
  (accepted): a quick `@john, do X` collapses the room while you type it and re-widens the instant
  you send — the filter is exactly as live as the target line above the composer.
- **The thread with john = john's own lines · the manager's lines · every broadcast · your messages
  addressed to john.** Other agents' prose, and your messages addressed to a *different* agent, are
  hidden. The manager is included wholesale (it is your delegate with a permanent seat); "the
  manager addressed john specifically" is not tracked and is not required. Broadcasts are included so
  john's replies to a fleet-wide prompt keep the prompt that caused them, rather than reading as
  orphaned.
- **It applies to history, not only the live tail.** Focusing an agent narrows the restored
  transcript too, so opening the room on an hour-old fleet shows a real thread rather than an empty
  pane. This costs no reconstruction — see §3.

**Not in scope (v1):** no keybinding; the working line (`roomWorkingLine`) stays fleet-wide; cards
and permission asks are untouched (the room draws none — `CLAUDE.md`); no wire or airlock change.

## 1. What is already free, and the one thing that is not

The code map (`internal/ui/chat.go`, `send.go`, `mention.go`, `roomhistory.go`) settles what the
filter can lean on:

- **"Which agent produced a line" is free per room line.** `roomLine` carries `ev core.Event` and
  `by Agent`; an agent's prose, its turn-ends and its permission asks all carry `ev.SessionID`. So
  "john's own lines" and "the manager's lines" are an id comparison.
- **"Addressed to which agent" is *not* recorded anywhere.** The only trace today is the literal
  leading `@name` inside a user line's `ev.Text` (the room echo keeps the mention "and all",
  `send.go`'s `sendRoom`). A structured recipient exists on no field of `core.Event` or `roomLine`.

The filter needs exactly one new datum — an **addressed-to** on the room's own user echo — and it
needs it only to tell "you → @iris" apart from "you → @john" and a broadcast. Everything else is an
id comparison the room already has the inputs for.

## 2. The predicate (pure, id-comparison only)

New file `internal/ui/roomfocus.go`, a pure function in the spirit of `attention.go` — no I/O, no
roster lookup, table-testable in isolation:

```go
// focusAdmits reports whether a room line is drawn while the room is focused on
// one agent. focus=="" is the unfocused room and admits everything, so the
// filter is identity until a target resolves.
func focusAdmits(l roomLine, focus, managerID string) bool {
    if focus == "" {
        return true
    }
    if l.ev.Kind == core.KindUserText {
        // your turns: a broadcast (to=="") or one addressed to the focused agent.
        return l.to == "" || l.to == focus
    }
    // agent-produced (prose, turn-end, permission ask): the focused agent's or
    // the manager's.
    return l.ev.SessionID == focus || l.ev.SessionID == managerID
}
```

Two facts make this correct and cheap:

- **User lines are told from agent lines by `Kind`, never by an empty `SessionID`.** A live room echo
  is `core.Event{Kind: KindUserText}` with no `SessionID`, but a *restored* broadcast user line
  keeps the `SessionID` of whichever transcript `collapseBroadcasts` retained it from. Keying on
  `Kind` is right for both.
- **`managerID` and `focus` are ids the room holds, not names it resolves.** The room never resolves
  an `@name`; it compares the ids App hands it (§4). So the predicate stays pure and the room needs
  no fleet.

## 3. Why history needs no reconstruction

Room *history* contains only broadcasts and agents' public prose. `collapseBroadcasts`
(`roomhistory.go`) restores a `KindUserText` turn only when ≥2 transcripts prove it a broadcast, and
**drops a single-transcript (private/direct) user turn entirely**. So your private `@john` DM sends
were never in the room to reconstruct — and under the ruling in §"Decisions", they do not need to be.
What *is* restored — broadcasts and john's / the manager's public prose — is exactly what the
predicate admits by `Kind` and `SessionID`. Restored user lines default to `to == ""` (broadcast),
which is true of every one of them.

Therefore the historical predicate is the same `focusAdmits`, run over the same restored lines, with
no addressed-to reconstruction and no new pass in `roomHistoryLines` beyond leaving `to` at its zero
value.

## 4. The addressed-to stamp — `roomLine.to`

A UI-only field on `roomLine` (`chat.go`), **not** on `core.Event`: it is a display-routing fact this
client computed, not part of Claude's JSON vocabulary, so the airlock stays four files and the wire
is unchanged.

- **Type and default.** `to string` — an agent id, or `""` meaning "broadcast / not addressed to one
  agent". The zero value is broadcast, which is what every restored line and every agent-produced
  line is.
- **Where it is set.** Only the room's own user echo carries a non-empty `to`. In `sendRoom`, the
  `roomRoute` `r` is already in hand; the stamp is:
  `to = r.Targets[0]` **iff** `r.mentioned && r.mode == MentionDirect` (a lone direct mention
  resolves to exactly one target); otherwise `""`. Open mode (`@john` widened to the fleet), `@all`,
  `@manager` and an unaddressed draft are all broadcasts — `to == ""` — which is correct: an
  open-mode `@john` reached the whole fleet and belongs in every focused thread, john's included.
- **How it rides in.** `Append(ev, by)` keeps its signature and stamps `to == ""`. `sendRoom` uses a
  sibling `appendUser(ev, to)` (unexported, same package) so only the echo path sets a recipient and
  no other caller changes.

## 5. The transcript renders a focus-filtered subset

`Room` gains two persistent fields, `focus string` and `managerID string`, set for the draw by App
through `WithFocus(focus, managerID)` — the same "set on the way to `View`, never folded into" shape
`WithSelection`/`WithMenu` already use, except these two must persist because **every** render path
must apply the same filter (a width re-wrap and a history merge re-render the transcript and must not
re-admit hidden lines).

**A focus change is treated exactly like a width change.** The room already re-renders a slice on
resize (`renderRoom(r, lines)` takes whatever slice it is handed and re-wraps it through glamour).
On a focus change the room re-renders the filtered subset, **clears the selection, and jumps to
bottom** — the three things a width change already does (`SetSize` sets `following = true`; a width
change clears the selection per `CLAUDE.md`). This reuses the hardest existing machinery rather than
inventing a parallel one, and it is the natural read: entering a focus, you want the latest of that
thread. A focus change that does not change the id (`newFocus == r.focus`) is a no-op, so idle typing
after the name has resolved costs nothing.

**The one real risk — say it plainly for the adversarial pass.** Today `said` (the drawn lines) is
1:1 with `tr` (the transcript's rendered geometry: `roomSpans`, scroll anchoring, `lineMoves`,
`reclaimOldest`). A filter makes the rendered set a *subset* of the room's full history, so the two
can no longer be the same list. The chosen strategy keeps the delicate geometry untouched:

- **`said` remains the rendered subset, kept 1:1 with `tr`** — so `roomSpans`, `lineMoves`,
  `reclaimOldest`, scroll and selection are unchanged.
- **A canonical full backing holds every room line** regardless of focus (extending the role `raw`
  already plays for restored history to the live tail as well). `Append` adds to the canonical
  backing always and to `said`/`tr` only when `focusAdmits` passes; a focus change rebuilds `said`
  from the canonical backing through the filter and re-renders.
- The 20k `roomRetentionEvents` cap applies to the canonical backing; the visible subset is never
  larger. Memory stays bounded by the existing cap.

The alternative — deriving `visible` on the fly and letting `said`/`tr` desync — was rejected because
it forces every geometry function to learn the difference between "a line exists" and "a line is
shown", which is precisely the shared, delicate code this project has been burned by before. Keeping
`said` 1:1 with `tr` localizes all new logic to the canonical backing and the rebuild.

## 6. The App seam — the whole "live query" wire

`App.retarget` (`send.go`) already resolves the composer's leading `@name` to an `Agent` per
keystroke and per fleet report, via the same router the send path uses. It is the one place that
knows "what is `@name` in the composer right now". The filter hangs off it:

```
keystroke / fleet report → App.retarget
    → route resolves a lone direct @john to agent id  → a.room = a.room.WithFocus("john-id", managerID)
    → target changes / draft clears / not a lone direct → a.room = a.room.WithFocus("",       managerID)
```

`managerID` comes from `Fleet.manager`. "A lone direct `@name`" is the existing `mentioned && mode ==
MentionDirect` reading (open mode is a broadcast — it widens the message, so it must *not* narrow the
view to one agent). The `WithFocus` write goes through `App.withRoom` so the room's text-selection
reconciliation (`Room.lineMoves`) runs on the re-render, same as every other room mutation.

## 7. The affordance

When focused, the room header reads `group chat › @john` (today's `roomTitle` is the constant
`"group chat"`; focus appends `› @name`). The composer's existing `→ @john · direct` target line is
the secondary tell. Nothing else changes on screen — the narrowing itself is the primary signal.

## 8. Testing

- **`roomfocus_test.go`** — table tests for `focusAdmits`: john's prose admitted, iris's prose hidden,
  the manager admitted, a broadcast admitted, "you → @iris" hidden while focused on john, "you →
  @john" admitted, and the `focus == ""` identity (every input admitted).
- **`frame_test.go`** — `App.View` with a resolved `@john` target shows only admitted lines; clearing
  the target restores every line. Proves the retarget → WithFocus → subset-render wire end to end in
  process.
- **History** — a `roomHistoryLines` + focus test: restore several transcripts (a broadcast, john's
  reply, iris's reply, a manager line), focus john, assert iris's reply is gone and the broadcast,
  john's reply and the manager line remain. Proves §3's "no reconstruction" claim.
- **A screen/pty test** (`cmd/wake/*_unix_test.go`) for the live round trip: type `@john` → the room
  narrows → send (or clear) → the room widens. This is the one that proves the query is genuinely
  live rather than a mode.
- **A re-wrap-under-focus test** — resize while focused and assert the subset re-wraps without
  re-admitting hidden lines (the §5 risk, made a regression).

## 9. Files touched

All within `internal/ui`, plus tests:

| File | Change |
|---|---|
| `roomfocus.go` (new) | `focusAdmits`, pure |
| `chat.go` | `roomLine.to`; `Room.focus`/`managerID`; `WithFocus`; `appendUser`; filter every render path; header `› @name`; canonical backing + rebuild-on-focus-change |
| `send.go` | stamp `to` in `sendRoom`; set focus from `App.retarget` |
| `roomhistory.go` | (only if a restored line ever needs a non-broadcast `to` — expected: none; leave `to` zero) |
| `roomfocus_test.go`, `frame_test.go`, a history test, a screen test | §8 |

No change to `internal/core`, `internal/daemon`, `internal/rpc`, the wire, the airlock, or any
keybinding/legend.
