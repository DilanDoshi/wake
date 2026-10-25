# A `!cmd` reaching its agent: what Claude Code does, and what the headless wire allows

Recorded 2026-10-25 against **Claude Code 2.1.310**, model haiku, cwd `/tmp/bang-probe` (one `note.txt`), for
Dilan's ruling that a `!cmd`'s output reaches the agent it was typed to (`decisions.md`, 2026-10-25).

## Provenance

- **Under the real `HOME` with `--safe-mode`**, as every live recording since the partial-messages note: claude
  will not sign in under a scratch `HOME` (credentials live in the keychain), and safe mode loads no CLAUDE.md,
  skills, plugins, hooks or MCP servers. Then `scripts/scrub-fixtures.py` (it neutralised twelve command names
  into the allowlist's placeholders); `corpus_test.go` passes unedited.
- `testdata/transcript/bash-mode.jsonl` is three records trimmed to the keys Wake reads, under neutral ids:
  lines 1-2 are an interactive session's `!cat note.txt missing.txt`, line 3 is the headless session's record
  of the line Wake writes for the same command.
- `testdata/stream/shell-run.jsonl` is that headless session's stdout, Wake's argv (every visibility flag,
  `--permission-prompt-tool stdio`, `--permission-mode auto`) plus `--safe-mode`; its stdin is
  `testdata/input/shell-run.stdin.jsonl`, byte for byte what `core.EncodeShellRun` writes.
- The interactive session was driven through a pty. A Claude Code session started from inside another one
  inherits a child-session marker and saves no transcript; `CLAUDE_CODE_FORCE_SESSION_PERSISTENCE=1` overrides it.
- The `shouldQuery:false` findings (§3) come from probe sessions that were not committed as fixtures; Wake does
  not write that field today. Record them before it ever does.

## 1. Claude Code's own `!` writes two string-content records, and starts a turn

`bash-mode.jsonl:1` is the command, `<bash-input>…</bash-input>`; `:2` is what it printed,
`<bash-stdout>…</bash-stdout><bash-stderr>…</bash-stderr>`. Both are **string** content, never a block array.
The streams are merged as a terminal shows them: a clean exit put everything in `<bash-stdout>` (a probe ran
`echo; ls /nope; echo` and got all three lines there), and a failing exit put everything in `<bash-stderr>` with
`<bash-stdout>` empty (`:2`). No exit status is recorded.

**Every `!` was followed by a model turn** - the model replied to the output - in auto mode and in default mode
alike, two bangs in one session each answered. Older builds added the record and waited for the next prompt;
2.1.310 does not.

## 2. Wake can write that record, and it is answered as a turn

`shell-run.stdin.jsonl` is one line holding both halves: `<bash-input>` with the command, `<bash-stdout>` with
Wake's merged, bounded output (and its `… exited 1` line), an empty `<bash-stderr>`. Unstamped. Claude takes it
as a turn: `shell-run.jsonl:31` is the model's reply naming what the output said, `:36` a `success` result with
`num_turns: 1`. **The line is never replayed**, though the session runs with `--replay-user-messages`: no user
frame appears on stdout (`TestAShellRunIsAnsweredWithATurnAndNotReplayed`). On disk it is one ordinary `user`
record holding the string exactly as written (`bash-mode.jsonl:3`).

## 3. `shouldQuery:false` holds a line with no turn (not used; kept for the owner's choice)

The Agent SDK documents `shouldQuery` on a user message: `false` appends it to the transcript without triggering
an assistant turn, merged into the next message that does trigger one - named for "the output of a command you
ran out of band". On 2.1.310, with Wake's argv:

- **Idle**: `queued`, `started`, an `init`, the replay, then a `success` result with `num_turns: 0`, $0 and no
  inference, then `completed`. The next real turn's answer used the output. On disk: an ordinary `user` record
  under the line's uuid.
- **Busy, at a tool boundary**: read inside the running turn, as any queued line is (the
  mid-turn-delivery findings §1); on disk a `queued_command` attachment. No turn of its own.
- **Busy, a text-only reply**: waits for the turn's end, then is appended with a `num_turns: 0` result.
- **Process ended, then `--resume`**: the resumed session still read the output, so a park keeps it.
- **Tagged string content with `shouldQuery:false`**: also held; a later turn listed every output it had been
  shown.

A `num_turns: 0` result decodes as `LocalCommand` (`protocol.go`), so it is not taken for an accepted API turn.
Writing it would need the daemon not to mark a turn owed, or the agent reads working for a moment.
