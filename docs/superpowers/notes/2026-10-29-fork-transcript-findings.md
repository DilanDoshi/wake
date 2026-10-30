# A fork's transcript: when claude writes it

Recorded 2026-10-29 against **Claude Code 2.1.311**, model haiku, for a fork's conversation pane that
opened empty (seen first with the demo's stand-in claude, then live).

## Provenance

- **Live**, through Wake (`main` at 1d9699dc) on a scratch socket, with the operator's own login in
  safe mode (`CLAUDE_CODE_SAFE_MODE=1`), in a project at `/tmp/forkpane-proj`. One agent took two
  one-word turns, then ⌃F forked it and, later, `wake fork` forked it again. The fork took one turn.
- **No fixture is committed.** A sterile `HOME` cannot sign in, and what was measured is a file's birth
  time beside its records' order, which a stream fixture would not carry. The steps below re-check it.

## What claude writes

- **A fork writes no transcript until its first turn.** With the fork running idle
  (`--resume <parent> --fork-session --session-id <fork>`), there was no `<fork>.jsonl` in the project
  directory. The file's birth time was the moment the fork's first prompt was written, about fourteen
  seconds after its pane opened.
- **The file then opens with the copy**, within about 13ms: a handful of session records (title, agent
  name, mode, file-history snapshots, queue operations), then the parent's user, attachment, assistant
  and local-command records under the parent's own uuids with `sessionId` rewritten to the fork's, then
  the fork's first prompt and its reply.
- **An id with no transcript cannot be resumed.** `claude --print --input-format stream-json
  --output-format stream-json --verbose --resume <id>`, with no input, exits 1 saying `No conversation
  found with session ID: <id>` and a zero-turn error `result`. No request is made.

## What follows for Wake

- A fork's pane asks for its history once, when it opens (`internal/ui/history.go`), and found no
  file, so it drew nothing of the conversation the fork holds. `server.history` now answers from the
  source's transcript, read only as far as it was long at the fork's launch (`agent.forkBytes`), until
  the fork's own file exists. `wake import` and `/adopt` launch the same way and are covered by it.
- A fork parked before its first turn has nothing a wake can resume, in this daemon or the next
  (`parkedStatuses` already leaves such a record out of `/resume`). See `docs/notes/deferred.md`.

## To re-check on an upgrade

Fork an idle agent that has taken a turn, list the project directory for the fork's id before and
after the fork's first prompt, and read the new file's record types in order.
