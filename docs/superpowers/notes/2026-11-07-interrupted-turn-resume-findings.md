# An interrupted turn, resumed: what claude keeps and what it drops

Recorded 2026-11-07 against **Claude Code 2.1.319**, model haiku, for `⌃Q` interrupting every agent
before it parks (`fix/quit-interrupts-and-parks-busy-agents`; `decisions.md`, 2026-11-07).

## Provenance

- **Live, without Wake in between**: a Python driver started `claude` with Wake's own argv (`buildArgs`:
  stream-json both ways, `--verbose`, `--permission-prompt-tool stdio`, `--brief`, the four visibility
  flags, `--permission-mode auto`, `--model haiku`, `--session-id`), wrote the stream-json lines Wake
  writes, and allowed every ask. Projects were at `/tmp/quitpark-probe`, `/tmp/quitpark-probe2` and `/tmp/quitpark-probe3`. The
  operator's own login was used with `CLAUDE*`, `CMUX*` and `WAKE_SOCKET` unset, since a sterile `HOME`
  cannot sign in.
- **No fixture was committed on 2026-11-07.** What was measured is the on-disk transcript beside the
  stream, and the stream shapes are already in the corpus (`interrupt-cancel-queued.jsonl`,
  `interrupt-mid-tool.jsonl`, `interrupt-then-close.jsonl`). The steps below re-check it. The resume's
  close was recorded on 2026-11-10 for BUG-65's fix; see the last section.

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
  `isApiErrorMessage`. It is not on stdout. Wake decoded it as agent text marked `LocalCommand`
  (BUG-65, fixed: core drops it, see the last section).
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

## An ask outstanding, then the interrupt and stdin closed in one burst

The prompt asked for one `Write`, under `--permission-mode default` so it asked. On the `can_use_tool`
request the driver wrote no answer: it wrote the interrupt with `cancel_queued` and closed stdin at once,
the order `stopForShutdown` writes them in.

- **The ask is withdrawn, not denied.** Next on stdout came `control_cancel_request` naming the ask, the
  interrupt's receipt, the tool result rejected (`The user doesn't want to proceed…`), the user text
  `[Request interrupted by user for tool use]`, and a `result` of `error_during_execution` with
  `aborted_tools`. The process exited 1 with an empty stderr; no file was written.
- **This is not what a bare stdin close does.** `question-park-close.jsonl` records an ask killed by
  EOF alone: the request fails with an `AbortError` and the turn carries on running tools.

## Re-checking

1. In an empty directory under `/tmp`, start `claude` with Wake's argv and a fresh `--session-id`. Send a
   prompt that runs a tool long enough to interrupt (a `sleep`).
2. Mid-tool, write the interrupt with `cancel_queued` set, as `core.EncodeInterrupt` builds it for
   `InterruptAll`, and close stdin. The process should exit within a second.
3. Start `claude` again with the same argv but `--resume <id>` in place of `--session-id`, and send the
   user line `continue`. Read the transcript under `~/.claude/projects/<slug>/<id>.jsonl`.
4. For the queued case, write a second user line before the interrupt and search the transcript for its
   words: expect them in `queue-operation` records only.

## The resume's close follows any turn left with no reply (2.1.322)

Recorded 2026-11-10 against **Claude Code 2.1.322**, haiku, `--safe-mode`, with the same driver and argv
and the same login (`CLAUDE*`, `CMUX*` and `WAKE_SOCKET` unset), in `/tmp/noresp-probe` and
`/tmp/noresp-probe3`. Committed as `testdata/transcript/resume-interrupted.jsonl` and `resume-killed.jsonl`.
Both are trimmed to the conversation the way `rewind-tree.jsonl` was: attachments are kept as envelopes
with their content dropped; the account (`session-owner`), request, cost, tool-list, latch, file-history and
mode records are dropped; thinking signatures are scrubbed. Then `scripts/scrub-fixtures.py`.

- **After an interrupt.** `resume-interrupted.jsonl`: the interrupt mid-`sleep`, stdin closed, `--resume`
  and `continue`. The close is line 27, a child of the `[Request interrupted by user for tool use]` record
  on line 23, written just before the `continue` on line 28.
- **After a kill, with no marker at all.** `resume-killed.jsonl`: the process was sent SIGKILL as soon as
  its prompt was replayed on stdout, before any reply. The resume writes the same close (line 19, ahead of
  the `continue` on line 20). Its parent is line 14, the last of the attachment envelopes after the
  bare prompt.
- **Not after a turn that finished.** The same steps with the turn left to finish write no close.
- **What this machine's own transcripts hold.** A read-only survey of 612 of them found 4 closes from
  real use, at 2.1.303, 2.1.307 (twice) and 2.1.314, besides the two recordings above. All were
  `<synthetic>`, flagged neither `isMeta` nor `isApiErrorMessage`, with the text exactly
  `No response requested.`. All 4 followed a tool result: a turn killed mid-tool, by a crash or by the
  kill at the grace.

So the close is claude's account of a turn that had no reply, whatever ended it. Core drops it by the
record itself: the `<synthetic>` model and the whole text (`isResumeClose`, `vocabulary.go`). It does not
check which record came before. An agent that writes the same words does so under a real model id, so its
reply still draws (`TestAnAgentThatWritesTheWordsStillDraws`).
