# File rewind: what a headless session does with `rewind_files`

**Recorded 2026-10-02 against Claude Code 2.1.288**, by a scratch driver writing Wake's own stream-json
argv (`--replay-user-messages`, `--permission-mode acceptEdits`, `--model haiku`) into fresh `/tmp/rwf-*`
projects. Every fact below is from these recordings or from Claude Code's public docs
(`checkpointing.md`, `agent-sdk/file-checkpointing.md`, `agent-sdk/typescript.md`'s `RewindFilesResult`,
`settings-reference.md`'s `fileCheckpointingEnabled`). The request's shape is also in the open-source
Python Agent SDK (`_internal/query.py`'s `rewind_files`; `subprocess_cli.py` sets the variable below).

**Provenance caveat:** recorded under the real `HOME`, then scrubbed with `scripts/scrub-fixtures.py`
(`--check` passes). An edit needs a model turn, the credentials live in the macOS keychain, and a sterile
`HOME` cannot log in — the partial-messages and conversation-rewind precedent. The request ids are the
driver's (`probe-…`).

| Fixture | What it holds |
|---|---|
| `testdata/stream/rewind-files.jsonl` + `testdata/input/rewind-files.stdin.jsonl` | four turns (Write, Edit, Write, no tool), then dry runs, the `dryRun` trap, unknown targets, a real restore |
| `testdata/stream/rewind-files-off.jsonl` + input | the same session resumed **without** the variable |
| `testdata/stream/rewind-files-resume.jsonl` + input | resumed **with** it: old checkpoints survive, a new one is taken |
| `testdata/stream/rewind-files-fork.jsonl` + input | `--resume P --fork-session --session-id F`: the fork restores to the parent's prompts |
| `testdata/stream/rewind-files-both.jsonl` + input | files then conversation, conversation then files, then `/clear` |
| `testdata/transcript/file-history.jsonl` | the first session's transcript, attachments dropped: its `file-history-*` lines |

## 1. Turning it on is an environment variable, and only that

A `-p` session ignores the `fileCheckpointingEnabled` setting (`settings-reference.md`). It checkpoints
when its environment carries `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true` — which is all the SDK's
`enableFileCheckpointing` option does. No flag, no control request.

An operator's opt-out wins: `CLAUDE_CODE_DISABLE_FILE_CHECKPOINTING=1` beside it, or the variable set to
`false`, both answer `File rewinding is not enabled.` (probed with no model turn; not committed, since
the answer is the `-off` fixture's verbatim).

## 2. The request

```json
{"type":"control_request","request_id":"<id>",
 "request":{"subtype":"rewind_files","user_message_id":"<user uuid>","dry_run":true}}
```

`user_message_id` is the user message's own uuid — the one Wake stamps on every send
(`core.EncodeUserMessage`) and the one `RewindTargets` already reads off disk. A real restore omits
`dry_run` (input lines 10–11).

**Trap: the key is snake_case.** `dryRun:true` (the TypeScript SDK's option name) is not an error — it is
ignored, and the request is a **real restore**: input line 8 / stream line 56 deleted both files. The
encoder's golden test pins the bytes against the recorded lines.

## 3. The replies

All arrive as `control_response`, correlated on `request_id`:

| Ask | Reply | Stream line |
|---|---|---|
| dry run, files would change | `success` · `{"canRewind":true,"filesChanged":[abs…],"insertions":N,"deletions":N}` | 54, 55, 61 |
| dry run, nothing to undo | `success` · `{"canRewind":true,"filesChanged":[],"insertions":0,"deletions":0}` | 53, 60 |
| real restore | `success` · `{"canRewind":true,"skippedLinks":0}` | 56, 59 |
| dry run, refused | `success` · `{"canRewind":false,"error":"No file checkpoint found for this message."}` | 57 |
| real restore, refused | `error` · `"error":"No file checkpoint found for this message."` top-level, no payload | 58 |
| either, checkpointing off | as the two rows above, `"File rewinding is not enabled."` | `-off` 3, 4 |

`filesChanged` holds absolute, realpath'd paths (`/private/tmp/…`). A refused **real** restore has the
bare shape a refused `set_permission_mode` has, so only the request id can say what it answers —
decisions.md's MCP Ruling 1, again.

## 4. What survives

- **Park and wake** (`--resume`, `-resume` fixture lines 3, 4, 19): checkpoints taken before the exit
  still restore, and the resumed process takes new ones — as long as it, too, has the variable.
- **Fork** (`-fork` lines 3, 4, 19): the fork restores to its parent's prompts; Claude copies the backups
  (`errors.md`). Parent and fork in one directory share the files a restore writes.
- **`/clear`** (`-both` lines 85–95): checkpoints from before it are gone (`No file checkpoint found`); the
  new conversation's are taken and restore.
- **A conversation rewind** leaves file checkpoints alone (`-both` lines 46–47): after rewinding the
  conversation to U3, U2 still dry-runs its file and U3 reports nothing to undo.

## 5. Both, in either order

Sent back to back, `rewind_files` then `rewind_conversation` (`-both` input lines 4–5, stream 44–45)
and the reverse (input 9–10, stream 59–60) each succeed, and the receipts come back in send order.
Order therefore does not decide correctness; failure does — a code restore can fail (backups swept,
unwritable), so Wake sends the files first and the conversation only on the files' success.

## 6. On disk

The transcript gains two line types: `file-history-snapshot` (one per prompt, keyed `messageId` = the
user uuid, carrying `trackedFileBackups`) and `file-history-delta` (one per newly tracked file). Neither
carries `uuid` or `parentUuid`, so neither is a tree node, and neither decodes to an event. Backups live
under `~/.claude/file-history/<session>/`; Claude keeps the newest 100 checkpoints and sweeps a session's
backups about 30 days after it last saved one (`checkpointing.md`).

Not tracked, per the docs: edits made through Bash, and a subagent's edits.
