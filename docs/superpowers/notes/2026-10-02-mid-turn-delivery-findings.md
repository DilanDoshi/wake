# Mid-turn delivery — what claude does with a message written while it works

Recorded 2026-10-02 against **Claude Code 2.1.288**, model haiku, cwd `/tmp/steer-probe`, with Wake's own argv
(every visibility flag, `--permission-prompt-tool stdio`, `--permission-mode auto`) plus `--safe-mode`. Seventeen
sessions, one fixture each, every one built the same way: an opening turn runs a 25 s **foreground** Bash (or a
long text-only reply), and 4 s into it a second stamped user line is written - the variable under test.

## Provenance caveats

- **Recorded under the real `HOME`**, for the partial-messages note's reason (credentials live in the keychain),
  with `--safe-mode`, which loads no CLAUDE.md, skills, plugins, hooks or MCP servers while authentication
  works normally - so no hook frames and no operator configuration reach the fixtures. Then
  `scripts/scrub-fixtures.py`, which neutralised twelve 2.1.288 bundled command names into the allowlist's
  placeholders (`alpha14`…).
- **Auto mode did not hold on haiku**: every session runs in `default` - its `init` reads `default`, or reads
  `auto` and the next status frame `default`. Nothing recorded here depends on the mode.
- The first take used `sleep 25; echo SLEPT`, which 2.1.288 refuses ("use Monitor") and the model then ran in the
  background - so it tested nothing. The recorded command is `python3 -c 'import time; time.sleep(25)'`.
- `cancel_async_message` is not in the official docs. Its shape (`{"subtype":"cancel_async_message",
  "message_uuid":…}`) comes from independent open-source Agent SDK clients and is verified here by recording.
- `testdata/transcript/midturn-absorbed.jsonl` is two records of `midturn-absent`'s on-disk transcript, the
  opening user record and the `queued_command` attachment, the latter without its `rendered` field (the CLI's
  own prompt wording, which Wake does not read).

| Fixture (`testdata/stream/`, with `testdata/input/<name>.stdin.jsonl`) | The second line |
|---|---|
| `midturn-absent.jsonl` | no `priority`, no `origin` - Wake's shape before this change |
| `midturn-next.jsonl` / `midturn-next-human.jsonl` | `priority:"next"`, without / with `origin:{kind:"human"}` |
| `midturn-later.jsonl` | `priority:"later"` |
| `midturn-now-human.jsonl` / `midturn-now-bare.jsonl` | `priority:"now"`, with / without the human origin |
| `midturn-text-next.jsonl` / `midturn-text-now-human.jsonl` | `next` / `now`+human during a text-only reply |
| `midturn-next-then-now.jsonl` | a `next`, then 2 s later a `now`+human |
| `midturn-two.jsonl` | two lines, 2 s apart, no priority |
| `midturn-slash.jsonl` | `/context`, no priority |
| `midturn-cancel.jsonl` / `midturn-cancel-late.jsonl` | `cancel_async_message` before / after claude took the line up |
| `midturn-cancel-slash.jsonl` | `cancel_async_message` of a queued `/context` |
| `midturn-recall-now.jsonl` | take the queued line back, then send it and a second as one `now`+human |
| `midturn-esc.jsonl` | an `interrupt` with a line queued |
| `midturn-idle-now.jsonl` | `now`+human to an idle session |

## 1. A line written mid-turn is read at the next tool boundary - with no priority at all

`midturn-absent.jsonl`: the line is acknowledged `queued` at once (`:33`), the Bash finishes (`:35`), the line
is replayed (`:36`) and `started` (`:37`), and the model's reply obeys it in the same turn. Its `completed`
(`:59`) precedes the turn's `result`, whose `user_message_uuids` names both messages (`:60`).

`priority:"next"` (`midturn-next.jsonl:32-36`) and `next` with a human origin (`midturn-next-human.jsonl:32-36`)
do exactly the same. **Wake's type-ahead held such a line until the agent was idle** on the premise that a busy
stdin coalesces or drops it; that premise was never recorded, and this falsifies it.

## 2. Written during a text-only reply, it waits for the turn to end

`midturn-text-next.jsonl`: `queued` at `:24`, the reply finishes untouched (`:342`), then the line opens its own
turn (`started` `:344`). `priority:"later"` waits for the turn's end even across a tool boundary
(`midturn-later.jsonl:32`, `:50-52`).

## 3. `now` with a human origin moves running work to the background

`midturn-now-human.jsonl`: `queued` (`:34`), and instead of waiting 25 s the Bash's tool_result reports it moved
to the background so the message could be read (`:37`); the line `started` (`:39`) inside the same turn. During a
text-only reply the same line ends the turn (`aborted_streaming`, `midturn-text-now-human.jsonl:26`) and opens
the next (`:28`).

## 4. `now` without that origin ends the turn at the boundary

`midturn-now-bare.jsonl`: the Bash runs to completion (`:37`, `"interrupted":false`), the turn ends
`aborted_tools` with an empty result (`:38`), and the line gets a turn of its own (`:40`).

## 5. `now` behind a queued `next` does not background anything

`midturn-next-then-now.jsonl`: both queue (`:39-40`); the Bash runs to completion, the turn ends `aborted_tools`
(`:43`), the `now` line runs first as its own turn (`:45`), and the earlier `next` line after it as another
(`:72`). So a send-now cannot simply write a `now` behind what is queued.

## 6. `cancel_async_message` takes a queued line back - until claude has taken it up

`midturn-cancel.jsonl`: the lifecycle reads `cancelled` (`:32`) **before** the receipt, which carries
`{"cancelled":true}` (`:33`) - a bool under the key an interrupt receipt uses for a uuid list. The line never
runs. After claude took it up the receipt is `{"cancelled":false}` (`midturn-cancel-late.jsonl:38`) and it is
delivered. A queued slash command cancels the same way (`midturn-cancel-slash.jsonl:36-38`).

**Take back, then one `now`** is Claude Code's send-now: `midturn-recall-now.jsonl` cancels the queued line
(`:36-37`), writes it and a second as one `now`+human (`:38`), and the Bash moves to the background (`:41`) while
the combined message is read in the same turn (`:43`).

## 7. A slash command written mid-turn waits for the turn's end

`midturn-slash.jsonl`: `/context` is `queued` (`:33`), the turn ends normally (`:55`), and only then does it run
as a command (`:57-61`) - claude gives commands Claude Code's "after the turn" rule itself.

## 8. Two lines at one boundary are read together; esc leaves the queue alone

`midturn-two.jsonl`: both `queued` (`:32-33`), both `started` at the same boundary (`:38-39`), one reply honours
both, and the result names all three messages (`:64`). `midturn-esc.jsonl`: an interrupt with a line queued
answers `still_queued` naming it (`:33`), and it runs as the next turn (`:39`) - unchanged since 2.1.226
(`interrupt-queued-survives.jsonl`). `now`+human to an idle session is an ordinary turn
(`midturn-idle-now.jsonl:22-23`).

## 9. On disk, a line taken up mid-turn is an attachment, not a user record

In the transcript a line claude took up mid-turn is an `attachment` of type `queued_command` carrying the
content (`prompt`), the uuid Wake stamped (`source_uuid`) and `commandMode:"prompt"`
(`testdata/transcript/midturn-absorbed.jsonl:2`); a line that opened its own turn is an ordinary user record
under Wake's uuid. Wake decoded no attachment, so a re-read conversation lost these - already true of the
manager's sends, which the daemon has always written mid-turn.

## 10. A line that opens its own turn starts after the last turn's result

`midturn-later.jsonl:50-52`: `result`, the first message's `completed`, then the queued line's `started`. The
daemon cleared what an agent owed at the result and nothing re-set it, so the agent read idle for that whole
turn unless a tool was running.

## What Wake does with this

- Every message to a working agent is written at once; claude decides when it is read (§1, §2, §7). It is pinned
  above the composer until its `started` (or, if that was lost, its `completed` or the result naming it, §1), then
  drawn where the model read it. A `/rename` to a busy agent is still held and goes one per turn, for
  `renamesync.go`'s hold. `internal/ui/queue.go`.
- `↑` with messages queued takes them back (§6) into the draft; `⌃]` takes them back and sends them with the draft
  as one `now`+human (§3, §5, §6). `internal/ui/recall.go`.
- `decodeTranscript` restores a `queued_command` as the user turn it was, under `source_uuid`, marked
  `Event.Absorbed`; rewind targets skip it (§9). The daemon's agent owes a result again on `started` (§10).

## Unverified

- Rewinding to a message taken up mid-turn (its record is an attachment).
- `now` during a subagent, an MCP call, or WebFetch/WebSearch (the docs name all three as work that moves).
- A taken-back or hurried message carrying images, and a queue deeper than two.
- `queued_turn_count` was read but nothing depends on it.
