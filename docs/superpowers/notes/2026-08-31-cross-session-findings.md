# Cross-session messages in stream-json — findings

**Probed 2026-08-31 against Claude Code 2.1.252.** Method: two headless
`claude --print --input-format stream-json --output-format stream-json --verbose`
sessions, one messaging the other over Claude Code's peer channel (the
`cc-socks` socket named in `init.messaging_socket_path`), the recipient's stdout
captured. The findings below are behaviour; the shapes are committed as
`testdata/stream/cross-session.jsonl` and `testdata/transcript/cross-session.jsonl`.

## What a cross-session message is

Claude Code lets one session send a message to another. It arrives in the
recipient's context wrapped as a `<cross-session-message from="uds:/tmp/cc-socks/<pid>.sock"
from-name="<sender>" from-mode="<mode>">…body…</cross-session-message>` envelope,
inside a longer content string: a one-line preamble before the envelope and a
block of harness guidance after it. `from-name` is the sender session's own
`--name`; `from` is present when the sender is addressable by socket and absent
otherwise.

## The load-bearing fact: it is not on the live stream without a flag

- **Without `--replay-user-messages`:** the message reaches the model's context
  and the on-disk transcript (a `user` frame with `isMeta:true`,
  `userType:"external"`, `isSidechain:false`), but **never appears on the
  recipient's live stdout** — zero `user` frames. The only live observable is the
  recipient's assistant reply, and only if the model happens to mention it. So a
  surface fed by the live stream (Wake's room) is structurally blind to it.
- **With `--replay-user-messages`:** it replays onto live stdout as a `user`
  frame carrying the same envelope, with **`isReplay:true` and `isSynthetic:true`**.

This resolves the question `internal/core/event.go` marked unobserved — whether
`--replay-user-messages` sets `isReplay`. It does. An ordinary Wake-style stdin
send, replayed under the same flag, carries `isReplay:true` and **not**
`isSynthetic` — so both replays fold to `Echoed:true`, which the room's existing
`typedByHand` fold already drops. That is what makes turning the flag on safe for
ordinary use: only the envelope-bearing frames are surfaced.

## The stripped body

The airlock keeps only the body inside the envelope tags — the preamble and the
trailing harness guidance are boilerplate every cross-session message carries,
and the tags are wire format. `core.crossSession` (in `wire.go`) does the strip;
`messageEvents` calls it on the one path a user frame's string content takes, so
it resolves the same from the live stream (`DecodeLine`) and the transcript
(`DecodeTranscriptLine`).

## Provenance caveat

The committed fixtures are minimal and sterile — placeholder session/uuid values
and a `/tmp/cc-socks/<pid>.sock` address, no init environment dump — authored
from the observed shape rather than a raw capture, for the same reason the corpus
guard exists: a recording is a photograph of the machine that took it. The
shapes (fields, envelope, flags) are what the probe observed.
