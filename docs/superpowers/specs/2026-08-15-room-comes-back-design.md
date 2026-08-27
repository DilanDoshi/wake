# The room comes back: history, a second ⎋, and a working line

*2026-08-15. Three changes asked for in one sitting, and they share a subject: what the **room** is
missing that a conversation already has.*

A conversation pane restores itself from claude's transcript, clears its draft on a key, and draws
the working line with the turn's age and its token count. The room does none of the three. It opens
empty over a fleet that has been talking for an hour, its draft can only be cleared by deleting it,
and it says nothing at all while three agents work.

---

## 1. `⎋⎋` clears the draft

### What is there now

`App.interrupt` has one special case:

```go
if a.focus == "" && !a.composerEmpty() {
    return a.clearDraft(), nil, true
}
```

In the **room** with something typed, `⎋` clears the draft. That is a shipped ruling (`3f8c662`):
the room is not one agent, so stopping "whichever one the cursor rested on" stopped a turn somebody
was not looking at. Everywhere else `⎋` interrupts and the draft is left exactly where it was,
deliberately — *"a person types the correction and stops the turn in either order"*.

So a **conversation pane has no way to clear a draft** short of holding backspace.

### The ruling

**`⎋⎋` clears the focused pane's draft, and it is additive.** The room keeps its single `⎋`. A
conversation pane's first `⎋` interrupts, exactly as today, and *arms*; a second consecutive `⎋`
clears the draft.

Three things follow, and each is a decision rather than a detail.

**It is an arm, not a timer.** This tree already has arm-then-confirm: a card's `a`/`d` are read
only from an empty composer and settled by a second `↵`, and *"every input that is not the confirm
takes the arm back"*. A double-key with a stopwatch would be a second mechanism for the same idea
plus the first thing on a timer in a program whose first non-negotiable is that nothing runs on
one. So: any key that is not `⎋` disarms, and so does a click. The two presses are consecutive
keystrokes, with no clock between them.

**It costs no legend entry.** `TestEveryKeyTheLegendNamesIsBoundAndEveryBoundKeyIsNamed` requires a
bijection between `legendEntries` glyphs and the `tea.Key…` cases in `App.key`. `⎋⎋` adds no case —
it is still `tea.KeyEsc` — so it needs no glyph, and adding `⎋⎋ clear` would be a glyph with no
binding, which is a build failure. It is also the right answer on its own terms: the legend already
needs 301 columns and an 80-column pane keeps six entries.

**The armed pane says so.** Discoverability has to come from somewhere, and the card's precedent is
that an affordance which exists only in one state is advertised in that state. While armed, the
focused conversation's legend reads `⎋ clear draft` where it read `⎋ interrupt`. Same glyph, same
binding, one word different — so the bijection is untouched and the key advertises itself at the
only moment it means anything.

**The room is unchanged and its `⎋⎋` is not a second meaning.** In the room a draft is gone after
the first press, so a second `⎋` finds an empty composer and does what `⎋` on an empty room composer
has always done. Nothing new is bound there.

---

## 2. The room comes back with what was said

### What is there now

`internal/ui/history.go` asks the daemon for one session's transcript the moment a **DM** is built,
and `DM.Before` folds the answer in above whatever the pane holds. `daemon.History` reads the file
claude keeps, through `core.DecodeTranscriptLine`, bounded to the last 400 events and 4MB.

`Room.said` is built from live events and from nothing else. So:

- `⌃Q`, then `wake`, then `/resume all` — a working fleet above an empty group chat.
- `wake` over a fleet that has been running since before this window existed — same.

### Where the history comes from

**Claude's transcripts, re-derived.** Not a room log of Wake's own: *"Wake owns almost no state"* is
a non-negotiable, and the four things it does own — roster, park book, groups, layout — are each
something nothing else records. What was said in the room is recorded, thirty times over, in the
files claude already writes.

The cost of that choice is the whole of §2.3 below: the transcripts are per session, so the room's
order has to be reconstructed, and the room's *filter* has to be re-applied.

### 2.1 An event knows when it happened

`core.Event` carries no time. It does not need one live — events arrive in order down one socket —
but N transcripts merged into one room do.

**`Event.At time.Time`, stamped by `DecodeTranscriptLine` and by nothing else.** The on-disk record
carries `"timestamp":"2026-08-09T06:04:08.727Z"`; the stream does not carry one at all. That keeps
it inside the airlock and inside the one decoder that can see it, which is the same shape the
session id already has — *"the caller knows which session's file it opened"* — read one turn
further: the caller cannot know the time, so the decoder stamps it.

A live event's `At` is the zero time. That is not a gap; it is the discriminator §2.4 uses.

### 2.2 A frame of its own

`rpc.FrameRoomHistory` / `FrameRoomHistoryReply`, beside `FrameHistory` / `FrameHistoryReply`.

Not a reuse, and the reason is a ledger rather than a payload. `askHistory` is *once per session per
client* — a second fold would draw the conversation twice. If the room's ask shared that ledger,
then opening a conversation for a session the room had already asked about would find the ask spent
and leave the pane empty, which is the exact bug `history.go` was written to remove. Two questions
about the same file, asked for two surfaces, with two answers and two ledgers.

The daemon answers it with `History(id)` — the same function, no second reader of the format.

### 2.3 What the room asks about, and when

Two moments, and between them they are every way a session can arrive into a room that is missing
its history:

- **The seed.** `NewRoomApp` is handed the fleet report the caller already holds. Every live session
  in it is asked about, once.
- **A wake.** `wakeArrived` is the report in which a session this client asked to resume has come
  back live. That is `/resume` — the case in the ask — and it is asked about there.

**Not a spawn and not a fork**, and that is a ruling rather than an omission. A freshly spawned
session has no transcript, so asking is a wasted round trip. A **fork** has its parent's, which the
room already holds under the parent — folding it again would draw an hour of the parent's prose a
second time under a new name.

### 2.4 What comes back, and what does not

The room is a *filter*, not a log. `ui.fold` is already the pure function that decides what one
event contributes to the room, so the restored prefix is built by running each history event through
it — the same policy, not a second copy of it.

Three rulings on top of that:

**A turn you typed comes back only when two transcripts prove it was a broadcast.** A user turn on
disk is indistinguishable from a private one: `sendRoom` strips a leading `@name` before writing,
and `FromRoom` is presentation-only and on no wire. So the same bytes on disk are `@all fix the
tests` sent to the room and `fix the tests` typed into a conversation pane, and this project has a
shipped ruling that a DM is private. What *is* decidable is multiplicity — one broadcast to N agents
is the same text, at the same moment, in N files. **A user turn found in two or more transcripts
within `broadcastWindow` collapses to one room line, attributed to nobody; a user turn found in one
is dropped.** It errs toward silence, which is the safe direction: the cost is that `@noah do this`
does not come back, and the alternative cost is a private turn on a shared surface.

**The prefix is capped at `roomHistoryEvents` after the merge.** Thirty sessions at 400 events each
is 12,000 events in one room, and a room re-renders every event it holds through glamour on a
width change. The per-session bound protects the wire; this one protects the pane.

**A batch is dropped and forgotten if the room moved since the ask.** `historyArrived`'s guard,
unchanged in shape and for its reason: claude writes a turn to disk *and* streams it, so a batch
folded after that event landed would draw it twice. Forgotten as well as dropped, so the next
occasion asks again.

### 2.5 Where it lands

`Room.Before(earlier []roomLine)` — the room's counterpart to `DM.Before`, and merging rather than
prepending, because the batches do not all arrive at once. The room records how many of its leading
lines are history; `Before` merges the new batch into exactly those, by `At`, and re-appends the
live lines untouched. A resume therefore lands its lines among the history that was already there
rather than under the newest thing anybody said.

Live lines are never re-ordered. Their `At` is zero and they are not in the merged range at all,
which is why §2.1's "a live event's `At` is the zero time" is load-bearing rather than incidental.

---

## 3. The room draws a working line

### What is there now

`heartbeatLine` is Claude Code's line, matched against it: the six-frame asterisk,
the 120ms step, the word pool, the shimmer geometry, `✻ Calculating… (1m 51s · ↓ 11.6k tokens)`.
`beat.go` is the one ticker, and it stops dead when nothing is working.

It is drawn in `DM.View` and in no other place. `Room.View` joins a transcript and a composer.

So the surface an operator spends most of their time on says nothing while the fleet works. Nothing
regressed — PR #15 built the line for the conversation pane and it was never hung on the room.

### The ruling

**At most one row, always.** The room may have thirty working agents and a row per agent is thirty
rows of pane taken from the transcript. Worse, a block of rows that comes and goes changes the
pane's height at an arbitrary moment — the failure `DM.chrome` exists for, where a frame one row
taller than it was given scrolls the alt screen away on every draw, caught by the pty harness and by
nothing else.

**The row belongs to the oldest running turn, and the others are a count:**

```
✻ noah · Sprouting… (1m 26s · ↓ 5.5k tokens)                    one agent working
✻ noah · Sprouting… (2m 4s · ↓ 5.5k tokens) · +2 more working   three
```

Every figure on the row belongs to one named agent. A fleet-summed token total and a
"longest-of-several" age on the same row would be two agents' numbers in one sentence, and the row
would be readable as neither. The count is a plain fact beside them.

**It is not the awareness strip's job.** The strip counts states — `✻ 3 working` — and answers
"should I stop what I am doing". This row answers "how long has it been and how much has come back",
which is the question the strip has never carried, and it reuses `workingLine` rather than
reimplementing anything.

**The token figure is a session total that steps at turn end**, which is what the conversation pane
already shows and is a deliberate difference from Claude's own climbing figure. Claude reads
streaming deltas; Wake does not ask for them, because `--include-partial-messages` turns every token
chunk into a frame and at fifteen to thirty agents through one socket that is the work-per-frame the
first non-negotiable forbids. Same glyph, same shape, a coarser clock.

---

## Testing

| Claim | Where |
|---|---|
| `⎋` in a conversation interrupts and does not clear | `internal/ui/` in-process |
| `⎋⎋` in a conversation clears the draft; `⎋`, a rune, `⎋` does not | in-process, plus the pty harness for the real key bytes |
| `⎋` in the room still clears on the first press | in-process (existing test must stay green) |
| An armed pane's legend says `clear draft` | in-process, reading `App.View` |
| `DecodeTranscriptLine` stamps `At`; `DecodeLine` leaves it zero | `internal/core`, golden against `testdata/transcript/` |
| Two transcripts with the same user text collapse to one room line | in-process, table |
| A user turn in one transcript does not reach the room | in-process, table |
| A resumed session's history merges into the prefix, not under the live lines | in-process |
| A batch is dropped and forgotten when the room moved | in-process, mirroring the DM's |
| The prefix is capped after the merge | in-process |
| The room draws one working row, naming the oldest turn | in-process, plus a pty test for the height |
| The room's pane stays in bounds when the row appears | pty harness |

No test may reach a live LLM. History is replayed from `testdata/transcript/`; the room's fold is
driven from `testdata/stream/`.

---

## Out

- A last-read marker for the room. It is an open `deferred.md` item with its own argument, and it is
  a different feature that happens to touch `Room`.
- A climbing token count. §3.
- A room log of Wake's own. §2.
- `⎋⎋` meaning anything other than "clear the draft" — Claude Code's own second `⎋` opens a rewind
  list, and Wake has nothing to rewind.
