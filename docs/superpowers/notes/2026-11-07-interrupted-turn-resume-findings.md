# An interrupted turn, resumed: what claude keeps and what it drops

Recorded 2026-11-07 against **Claude Code 2.1.319**, model haiku, for `⌃Q` interrupting every agent
before it parks (`fix/quit-interrupts-and-parks-busy-agents`; `decisions.md`, 2026-11-07).

## Provenance

- **Live, without Wake in between**: a Python driver started `claude` with Wake's own argv (`buildArgs`:
  stream-json both ways, `--verbose`, `--permission-prompt-tool stdio`, `--brief`, the four visibility
  flags, `--permission-mode auto`, `--model haiku`, `--session-id`), wrote the stream-json lines Wake
  writes, and allowed every ask. Projects were at `/tmp/quitpark-probe` and `/tmp/quitpark-probe2`. The
  operator's own login was used with `CLAUDE*`, `CMUX*` and `WAKE_SOCKET` unset, since a sterile `HOME`
  cannot sign in.
- **No fixture is committed.** What was measured is the on-disk transcript beside the stream, and the
  stream shapes are already in the corpus (`interrupt-cancel-queued.jsonl`, `interrupt-mid-tool.jsonl`,
  `interrupt-then-close.jsonl`). The steps below re-check it.

## An interrupt, then stdin closed, then `--resume` and `continue`

The prompt asked for six steps in order: write `step1.txt`, `sleep 10`, write `step2.txt`, `sleep 10`,
write `step3.txt`, write `done.txt`, then reply `finished`.

- **The interrupt ends the process at once.** Two seconds into the first `sleep`, the driver wrote the
  interrupt with `cancel_queued` (what `InterruptAll` writes) and closed stdin. The process exited 0.83 s
  later with status 1 and an empty stderr, as `interruptedExit` expects. Only `step1.txt` existed.
- **The stream** ended with the receipt (`still_queued` empty, `cancelled` empty), the Bash tool result
  rejected (`The user doesn't want to proceed with this tool use…`), the user text
  `[Request interrupted by user for tool use]`, and a `result` of `error_during_execution` with
  `terminal_reason` `aborted_tools`.
- **On disk** the transcript holds the same three records: the rejected tool result, the interrupted
  user text, and the turn's end.
- **The resume writes one record of its own.** `claude … --resume <id>` (no `--session-id`, as a wake
  starts), then a user line `continue`. Ahead of that prompt the transcript gains one assistant record
  with the text `No response requested.`, model `<synthetic>`, usage present, and neither `isMeta` nor
  `isApiErrorMessage`. It is not on stdout. Wake decodes it as agent text marked `LocalCommand`
  (BUG-65).
- **`continue` picks the turn up where it stopped.** The model re-ran the rejected `sleep 10`, then
  wrote `step2.txt`, slept, wrote `step3.txt` and `done.txt`, and replied `finished`
  (`result` `success`). Nothing before the interruption was repeated.

## A message queued behind the turn, cancelled

The prompt asked for `sleep 15`, then the word `slept`. Two seconds into the sleep the driver wrote a
second user line, waited two seconds, wrote the interrupt with `cancel_queued`, and closed stdin.

- **The receipt names it**: `cancelled` lists the queued line's uuid, the list form
  `interrupt-cancel-queued.jsonl` records.
- **Its words are kept only in `queue-operation` records**: an `enqueue` and a `remove`, both carrying the
  text. No `user` record and no `queued_command` attachment holds it, and nothing reached stdout with it,
  `--replay-user-messages` notwithstanding.
- **So nothing Wake reads brings it back.** Core drops `queue-operation` records as on-disk plumbing, a
  conversation's `↑` history is its transcript's user turns, and the room's is its restored echo. That is
  why the `⌃Q` window prints the words itself (`internal/ui/parkdropped.go`).

## Re-checking

1. In an empty directory under `/tmp`, start `claude` with Wake's argv and a fresh `--session-id`. Send a
   prompt that runs a tool long enough to interrupt (a `sleep`).
2. Mid-tool, write the interrupt with `cancel_queued` set, as `core.EncodeInterrupt` builds it for
   `InterruptAll`, and close stdin. The process should exit within a second.
3. Start `claude` again with the same argv but `--resume <id>` in place of `--session-id`, and send the
   user line `continue`. Read the transcript under `~/.claude/projects/<slug>/<id>.jsonl`.
4. For the queued case, write a second user line before the interrupt and search the transcript for its
   words: expect them in `queue-operation` records only.
