# A transcript record's uuid: what Wake stamps, and what a fork copies

**Recorded 2026-09-29 against Claude Code 2.1.285**, into a sterile `HOME` with no credentials, so no
turn reached the model (each answer is the recorded "Not logged in" error record, which
`decodeTranscript` drops). Everything below is about what claude writes to
`~/.claude/projects/<slug>/<id>.jsonl`, not about stdout.

Only the `type:"user"` records were committed: the recordings' `attachment` records are the session's
environment (skill and agent listings, a prompt snapshot), which the corpus keeps out. The committed
lines are otherwise verbatim.

| Fixture | What it holds |
|---|---|
| `testdata/input/room-stamped-uuid.stdin.jsonl` | the one stream-json user line written, stamped with a **version-8** uuid |
| `testdata/transcript/room-stamped-uuid.jsonl` | the user record claude wrote for it |
| `testdata/transcript/fork-parent.jsonl` | a session's first user record (`--session-id P`) |
| `testdata/transcript/fork-child.jsonl` | `--resume P --fork-session --session-id F`: the copied record, then the fork's own |

## 1. The uuid Wake stamps is the uuid on disk

A user line written with a top-level `uuid` (Wake stamps one on every send so the CLI emits
`command_lifecycle` for it - `core.EncodeUserMessage`) is recorded under **that same uuid**. A
version-8 uuid, which no random-v4 generator produces, was accepted and persisted unchanged, and the
`--replay-user-messages` echo on stdout carried it too. Pinned by
`TestTheUUIDWakeStampsOnASendIsTheOneOnDisk`.

So a uuid is a provenance channel that survives to disk **without changing what the model reads** -
the model never sees it.

## 2. A fork copies its parent's records under the same uuids

`resume-fork-findings.md` §7 recorded this at 2.1.2xx and it still holds: the fork's file opens with
the parent's records **rewritten with the fork's `sessionId`** but with their `uuid`s - and their
`timestamp`s - unchanged, followed by the fork's own records under new uuids. There is **no fork
marker** (no `forkedFrom` or similar key) on any record. Pinned by
`TestAForkCopiesItsParentsRecordsUnderTheSameUUIDs`.

What follows for the room: the same uuid in two transcripts is one record a fork copied, never two
things anybody said - and the same *text* at the same *time* in two transcripts, which the room's
broadcast rule reads as a broadcast, is exactly what a fork's copy of a private DM turn looks like.
