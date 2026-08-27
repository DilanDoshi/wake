# Runtime commands, the park-survival question, and the resume-memory proof

**Six recordings against claude 2.1.238, 2026-08-21**, all machine-driven, all under the real
`HOME` and scrubbed (`--check` passes; see the partial-messages note for why sterile-HOME capture
cannot authenticate here). Total spend ~$2.25 across the six results. Cite the fixture and the
line; every stream fixture's stdin sibling is in `testdata/input/`.

| Fixture | What it records |
|---|---|
| `add-dir-runtime.jsonl` | `/add-dir <dir>` refused by the CLI at runtime; the outside read stays denied |
| `debug-runtime.jsonl` | `/debug api` enabling logging mid-session; four queued turns coalescing into one result |
| `read-denied-headless.jsonl` | A `default`-mode denial pair (Read, then the Bash fallback) with no `--permission-prompt-tool` |
| `auto-read-outside.jsonl` | The control: the same outside Read under `auto`, succeeding with no ask |
| `resume-memory-before.jsonl` / `-after.jsonl` | The central claim: a resumed session answers from the parked conversation |

## 1. `/add-dir` does not exist at runtime — the park-survival question is half-answered

`add-dir-runtime.jsonl`: the first turn is `/add-dir <absolute dir>` and the CLI itself answers
`/add-dir isn't available in this environment.` in the bare-effort shape — one assistant line,
`num_turns: 0`, `$0`, its **own result frame** — and the next turn's Read of a file in that
directory is still denied. **A running agent cannot be given a directory.** That is the exact
evidence `deferred.md`'s 2026-08-16 entry asked for, and it lands on the budget's side of the
argument: nothing can restore an added directory after a park, so `--add-dir` is the kind of thing
that must survive one (a field on `parkedRecord`) or be silently lost on every wake. Whether to
add the field is a feature decision the owner holds; the evidence no longer is.

## 2. `/debug` works at runtime — the other half answers the other way

`debug-runtime.jsonl`: `/debug api` on stdin turned logging on mid-session. What the bytes prove:
the session's own `ls` mid-turn finds `~/.claude/debug/` holding exactly one log — this session's
`<session-id>.txt`, kilobytes deep. What is inference rather than bytes: that the *command* turned
it on, resting on no flag-less session that morning having produced a log (testimony about the
machine, not a frame). So logging **can** be re-asked after a wake, and the park book has no
need to carry the debug flags — with one asymmetry worth knowing: runtime `/debug` writes to
**claude's own** debug directory, not to the daemon-owned `debug/<name>.log` that `--debug-file`
feeds. A woken session is diagnosable, but not in the place Wake's flag would have put it.

## 3. Queued stdin turns can coalesce into one result

The same fixture: four user messages were piped at once, and the process emitted **one** result
frame with `num_turns: 4` — the model's one reply answers the first, third and fourth asks and
says nothing about `/add-dir`. Yet
`add-dir-runtime.jsonl`, two messages piped the same way, produced **two** results (the CLI-handled
command got its own). So "result and init are per-turn" (CLAUDE.md's trap) holds when the CLI
handles a message locally, and bends when queued prompts reach the model together. Wake writes one
message and waits for the result, so the ordinary path never queues — but the manager, or any
client writing bursts, can produce this, and a consumer counting results to count turns will
undercount. Observed, not explained; left as a trap to know about.

## 4. A second headless denial — a wider shape, not a first

The first cut of this section called the auto-deny path "recorded at last", and that was wrong:
`permission-denied.jsonl` (2.1.226) has carried it since 2026-08-08, documented as exactly that in
that day's findings note. What `read-denied-headless.jsonl` adds is the *pair* and the newer wire:
a Read outside the working directory denied (`system/permission_denied` with
`decision_reason_type: "workingDir"`), the model's own `Bash(cat)` fallback denied behind it, and
**both** entries in `permission_denials` on one `success` result — against 2.1.238.

The control beside it, `auto-read-outside.jsonl`, is its own finding: under
`--permission-mode auto` the *same* outside Read **succeeds with no ask at all** — the probe's
first discriminator had no teeth. Wake spawns every agent in `auto`; an agent's reads are not
confined to its spawn directory in that mode, which is worth knowing whenever `--add-dir`'s value
is being weighed. (One caveat the fixtures carry honestly: the runtime `/add-dir` refusal was
recorded in `default` mode; that it is mode-independent is plausible and unrecorded.)

## 5. A resumed session answers from the conversation it parked with — proven

Lane B item #1, the product's central claim, previously unasserted because the fake `claude` keeps
no transcript. `resume-memory-before.jsonl`: a session started under a minted `--session-id` is
told "the codeword is pomegranate" and replies OK; the process ends. `resume-memory-after.jsonl`:
a fresh process under `--resume <that id>` is asked for the codeword and answers **pomegranate**,
on the **same session id** — the id survives the resume, which is what Wake's park book relies on.
This proves the claude layer of park/wake; the Wake layer above it stays fixture-tested.

## 6. The multi-message token fold is verified — the partial-messages note's one remainder

`debug-runtime.jsonl` was recorded under `--include-partial-messages` and its turn used tools, so
it is the multi-message case the partial-messages note left unrecorded: **three** `message_start`s
in one turn, newest `message_delta.usage.output_tokens` per message 655, 431 and 243, and their
sum — **1,329 — equals the result frame's own `output_tokens` exactly.** The sum-of-newest-
per-message fold (`core.turnTokensEvent` → `Agent.TurnTokens`) is what the wire does, delimited
where `message_start` says.

**One trap found in the recording itself:** the before-session emitted a **raw non-JSON line on
stdout** — an MCP diagnostic, `Client.listTools() called but server does not advertise tools
capability - returning empty list` — as the *last* line of the capture, after the final `result`
frame and before EOF (line 11 of 11; a trailing line, not one splitting the stream, which would be
the worse version). `internal/core` logs and skips malformed lines (`Session.emit` →
`KindUnknown`), so Wake survives either placement; the committed fixture has the line removed
because the corpus requires every line to decode. The naive reading this kills stands: stdout
under `--output-format stream-json` is *not* guaranteed to be pure JSONL on a machine with MCP
servers configured.
