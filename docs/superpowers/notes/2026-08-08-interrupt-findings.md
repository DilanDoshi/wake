# Interrupting a turn — what `claude` actually does

Recorded 2026-08-08/09 against **Claude Code 2.1.226**, macOS 15 (arm64), model `claude-sonnet-5`.
Companion to `2026-08-08-stream-json-findings.md`, which this note **corrects in two places**
(§11).

Every **wire shape** here was observed on the real stdout of a real `claude` process. Ten fixtures,
**305 lines**, ten distinct processes. Nothing is hand-written and nothing comes from
`~/.claude/projects/*.jsonl`. Where a quoted line is shortened the elision is marked inline with
`...ELIDED...`; every other quoted line is byte-for-byte what came out of the pipe.

Three kinds of claim here are **not** stdout observations, and each is labelled where it appears:

- **Binary-read** — the request/response schema and its field semantics, read out of the 2.1.226
  binary by string inspection (§2, §7). Marked `[binary]`.
- **Testimony** — the process exit codes (§6). Observed via `cmd.Wait()` at recording time, but an
  exit code is not a byte on a stream, so there is nothing meaningful to commit.
- **Inference** — anything reasoned from the fixtures rather than read off them. Collected
  separately in §12 so it can be checked and, where wrong, thrown away without taking the
  observations with it.

Anything neither observed nor backed by a fixture is in §13.

**Corrected 2026-08-09 — one claim in §4a is false, and the correction is inline there rather than
applied to it.** `2026-08-09-interrupt-permission-findings.md` recorded the case §13 named as the
likeliest real collision — an interrupt landing on an *outstanding permission ask* — and it ends
`error_during_execution` with the tool **listed** in `result.permission_denials`. Everything §4a
says was true of the runs that recorded it; it does not generalise past them. The paragraph is
left as it was written, with the correction attached beneath it, so what was known on 2026-08-08
stays legible.

**Promoted 2026-08-09 — fixtures now live flat in `testdata/stream/`, inside the golden test.** They
were recorded into `testdata/stream/interrupt/`, one directory below the existing corpus, because
`internal/core/fixtures_test.go` globs `testdata/stream/*.jsonl` and **fails on any frame that
decodes to `KindUnknown`**, by design: *"the decoder needs a case for it"*. These recordings contain
29 such frames (17 `command_lifecycle`, 12 `control_response`), so promoting them turned the suite
red until `protocol.go` grew those two cases. The nesting existed only because this spike ran
alongside live edits in `internal/` and must not redden a suite it is not allowed to fix.

Both halves are now confirmed by running them. Nested: green. Promoted, before the decoder change:
red with exactly those 29 errors — 17 and 12, split as predicted — and nothing else. Promoted, after:
green, 536 lines, zero unknowns. See §10 for what is done and what is still open.

| File | Lines | What it proves |
|---|---:|---|
| `interrupt-mid-tool.jsonl` | 60 | interrupt while a `Bash` tool is running: receipt, tool reported `user-rejected`, `result` `error_during_execution` / `aborted_tools`, **then a normal turn on the same session id** |
| `interrupt-mid-generation.jsonl` | 19 | interrupt while the model is generating, no tool running: `aborted_streaming`, a different `[Request interrupted by user]` string, then a normal turn |
| `interrupt-idle.jsonl` | 14 | interrupt **before any turn** and again **after a completed turn** — both no-ops, both acked |
| `interrupt-double.jsonl` | 43 | two interrupts 300 ms apart; both acked; the second is a no-op |
| `interrupt-queued-survives.jsonl` | 40 | plain interrupt with a message queued: `still_queued` lists its uuid and **that message then runs** |
| `interrupt-cancel-queued.jsonl` | 25 | `cancel_queued:true`: `cancelled` lists the uuid, `still_queued` empty, the message **never runs** |
| `interrupt-cancel-queued-empty.jsonl` | 64 | `cancel_queued:true` against an *empty* queue: `cancelled` present but `[]`. Also the only fixture with background-task frames |
| `interrupt-no-request-id.jsonl` | 19 | an interrupt sent **without `request_id`** still works, and its receipt carries no `request_id` either |
| `interrupt-then-close.jsonl` | 11 | one interrupt, then stdin closes with no successful turn behind it — the exit-code case (§6) |
| `interrupt-resume-after.jsonl` | 10 | a **separate process** `--resume`-ing the session from `interrupt-then-close.jsonl`, recalling pre-interrupt context |

Three of these are contrast pairs. Only the last is a true A/B in the sense the stream-json note
uses — same argv, same prompt, one variable:

- `interrupt-then-close` / `interrupt-resume-after` — **two processes over one session id**, the
  second differing only in `--resume` instead of `--session-id`.
- `interrupt-queued-survives` / `interrupt-cancel-queued` — the `cancel_queued` flag is the claim
  being tested, but the running turn also differs (a blocking `Bash` vs a long generation), because
  a blocking tool call could not be provoked reliably (§14). The queued-message behaviour is
  legible in each independently; do not read the pair as isolating one variable.
- `interrupt-cancel-queued` / `interrupt-cancel-queued-empty` — both set `cancel_queued:true`, and
  they differ in whether anything was actually queued. Same caveat: the running turns differ too.

---

## 1. The invocation

The argv for every recording here. It is `session.go`'s `buildArgs` minus `--forward-subagent-text`
(no subagents were exercised), so `parent_tool_use_id` is `null` on every frame:

```
claude --print
       --input-format stream-json
       --output-format stream-json
       --verbose
       --permission-prompt-tool stdio
       --session-id <fresh uuidgen>
       --name spike
       --permission-mode auto
       --brief
       --include-hook-events
```

**No flag enables interrupts.** `claude --help` has no interrupt-related option at all; the
capability is unconditional on the stdin control channel. `init.capabilities` on every fixture
still reads exactly:

```json
"capabilities":["interrupt_receipt_v1","interrupt_cancel_queued_v1","msg_lifecycle_v1"]
```

`--permission-prompt-tool stdio` was present throughout because Wake passes it. Whether an
interrupt works *without* it was not tested (§13) — but nothing in the code path read in §2
consults it.

---

## 2. The outbound frame — what Wake writes on stdin

```json
{"type":"control_request","request_id":"1fc6c7cc-7865-4242-a251-e76e78a7fc15","request":{"subtype":"interrupt"}}
```

That exact line, with a fresh `request_id` per send, produced every receipt in this note. The
`cancel_queued` variant adds one key:

```json
{"type":"control_request","request_id":"369900e4-8ebf-45c0-9406-cb447c161499","request":{"subtype":"interrupt","cancel_queued":true}}
```

**Same envelope as a permission answer, opposite direction.** A `can_use_tool` is a
`control_request` Claude sends *to* Wake; an interrupt is a `control_request` Wake sends *to*
Claude. `protocol.go` already reads the nested `.request.subtype` inbound; it has no encoder for
the outbound direction.

`[binary]` The full schema, from the zod definition in the 2.1.226 binary:

| Field | Type | Required | Notes |
|---|---|---|---|
| `subtype` | `"interrupt"` | yes | literal; described as *"Interrupts the currently running conversation turn."* |
| `reason` | string | no | marked `@internal`. *"Why the turn was interrupted, forwarded to the turn's AbortSignal.reason. Tool implementations branch on it…"* Known values: `interrupt`, `user-cancel`, `remote-cancel`, `consumer-error`, `workflow-abort`, `stalled`, `recovery-timeout`. **Open set.** Never sent in these recordings, so its effect is unverified (§13). |
| `cancel_queued` | boolean | no | see §7 |

`request_id` sits on the **envelope**, not inside `request` — the same nesting the inbound
`can_use_tool` uses.

### `request_id` is optional, and that is a trap

`interrupt-no-request-id.jsonl` sent this, mid-generation:

```json
{"type":"control_request","request":{"subtype":"interrupt"}}
```

It worked — the turn aborted normally. But the receipt came back **with no `request_id` key at
all** (`:8`):

```json
{"type":"control_response","response":{"subtype":"success","response":{"still_queued":[]}}}
```

Which is unanswerable as a correlator. At 20 agents with several interrupts in flight, a receipt
that names no request is a receipt you cannot attribute. **Wake must always send a `request_id`**,
even though the CLI does not require one. The same process then took a well-formed interrupt
(`:11`, `request_id` present) and a normal turn (`:12`–`:19`), so the malformed-ish frame did not
poison anything — worth knowing, given `session.go`'s note that one bad line on stdin kills the
process. A control request missing `request_id` is not that kind of bad line.

---

## 3. What comes back — the receipt

`interrupt_receipt_v1` is real and it is a `control_response`. Verbatim,
`interrupt-mid-tool.jsonl:50`:

```json
{"type":"control_response","response":{"subtype":"success","request_id":"1fc6c7cc-7865-4242-a251-e76e78a7fc15","response":{"still_queued":[]}}}
```

Top-level keys are exactly `type` and `response`. **No `session_id`, no `uuid`** — the same
attribution problem `control_request` has, and the reason `request_id` matters. Like every control
frame, its `subtype` is nested (`.response.subtype`), and there is a second nesting: the payload
is `.response.response`.

`subtype` was `"success"` on all 12 receipts recorded, including every no-op. **It is transport
level, not a verdict** — exactly as `protocol.go` already documents for the outbound permission
answer. An interrupt that interrupted nothing still answers `"success"`.

Four payload shapes were observed, and they differ only by what was queued and what was asked:

```json
{"still_queued":[]}                                                        (9 receipts)
{"still_queued":["c011f484-e92c-4cd6-b262-93978c2851e5"]}                  (1)
{"still_queued":[],"cancelled":["c07ca05a-0add-414b-a0de-05ff3b59bdf8"]}   (1)
{"still_queued":[],"cancelled":[]}                                         (1)
```

*(Counted 2026-08-09 over the committed fixtures. This list originally showed three and named the
fourth in prose two paragraphs down; the bytes say four.)*

`cancelled` appears **only** when the request set `cancel_queued:true`. The A/B is committed:
`interrupt-queued-survives.jsonl:27` has no `cancelled` key, `interrupt-cancel-queued.jsonl:14` and
`interrupt-cancel-queued-empty.jsonl:51` both have one — and the latter's is `[]`, so the key's
presence tracks the *request*, not whether anything was cancelled.

### Ordering: the receipt lands **before** the turn's `result`

Across all ten fixtures, without exception:

| Situation | Order |
|---|---|
| a turn was running | receipt, then the aborted `result` (mid-tool `:50`→`:53`; mid-generation `:9`→`:11`) |
| no turn was running | receipt alone, no `result` at all (`interrupt-idle.jsonl:5` and `:14`) |

So the receipt is not "the turn is over" — it is "I have your interrupt". `interrupt-double.jsonl`
shows both in one file: `:39` receipt → `:42` result → `:43` receipt, the second interrupt having
arrived after the abort completed.

---

## 4. What the interrupted turn looks like on stdout

Two shapes, and **which one you get depends on whether a tool was running.**

### 4a. Mid-tool — the tool reports as *rejected*

`interrupt-mid-tool.jsonl` interrupted a `Bash` running
`until [ -f /tmp/wake-spike-sentinel ]; do sleep 2; done`, 2.0 s after the `tool_use` frame. The
tool result (`:51`):

```json
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01NyxocdUn6zSZn4UQQj5kQw","type":"tool_result","content":"The user doesn't want to proceed with this tool use. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file). STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true}]},"parent_tool_use_id":null,"session_id":"d5796f41-5693-4c77-b019-aef297b6296f","uuid":"1f40f31e-fb6c-487b-8467-61c773bbad83","timestamp":"2026-08-09T06:06:34.692Z","tool_use_result":"User rejected tool use","tool_result_meta":[{"id":"toolu_01NyxocdUn6zSZn4UQQj5kQw","non_execution_kind":"user-rejected"}]}
```

The answer to "denied, errored, or something else" is **denied** — and specifically:

> **`non_execution_kind` is `"user-rejected"`, the exact marker the stream-json note assigns to the
> auto-deny path.** An interrupt and "nobody was there to answer the permission ask" are
> indistinguishable on that field. See §11.

`result.permission_denials` is `[]` on every interrupted turn, so the interrupt does **not** show up
as a permission denial. That is the field that separates them.

> **Correction, 2026-08-09 — the sentence directly above is false as a general claim, and the
> paragraph is left standing as the dated record of what these ten fixtures showed.**
>
> It is true of every run recorded *here*, and every one of those interrupted a tool that was
> **already running** — which was never a permission denial to begin with. It does not hold for an
> interrupt landing on a tool that was still *waiting to be allowed*:
> `interrupt-pending-basic.jsonl:18` ends `subtype: "error_during_execution"` with the `Write`
> **listed** in `permission_denials`, and `interrupt-pending-answer-after.jsonl:18` does the same.
>
> So `permission_denials` answers "did a tool fail to run", not "was this denied or interrupted".
> The pair still separates, on `subtype` / `terminal_reason` / `is_error`: a denial is
> `success` / `completed` / `false`, an interrupt `error_during_execution` / `aborted_tools` /
> `true`. Details, the three fixtures and the comparison table:
> `2026-08-09-interrupt-permission-findings.md` §4.

Then a `user` frame carrying a bracket string (`:52`):

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]},"parent_tool_use_id":null,"session_id":"d5796f41-5693-4c77-b019-aef297b6296f","uuid":"45fa19e1-0a28-4f8b-b939-2696feb65380","timestamp":"2026-08-09T06:06:34.692Z"}
```

**It carries no `isSynthetic` and no `isReplay`.** Its full key set is
`message, parent_tool_use_id, session_id, timestamp, type, uuid` — identical to a genuine user
turn. `protocol.go` decodes it to `KindUserText` with `Echoed: false`, i.e. Wake will render
Claude's own abort marker as something the human typed. Wake should special-case the two literals
rather than trusting `Echoed`.

### 4b. Mid-generation — no tool, no tool_result

`interrupt-mid-generation.jsonl` interrupted 6 s into a long essay, after `init` and two
`thinking_tokens` frames and before any `assistant` frame. There is no `tool_result`, and the
bracket string is **different** (`:10`):

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]},"parent_tool_use_id":null,"session_id":"bc2d1140-0e0f-4309-8b15-d28867e1d3d2","uuid":"27cd23e8-9c65-427d-8b3d-39f19f0dd18b","timestamp":"2026-08-09T06:09:14.970Z"}
```

`[Request interrupted by user]` vs `[Request interrupted by user for tool use]`. Both literals are
present in the 2.1.226 binary. Anything matching on the string must match both.

---

## 5. The `result` frame — `subtype` is **not** always `"success"`

This is the single largest correction to the existing findings note.

Mid-generation, verbatim (`interrupt-mid-generation.jsonl:11`):

```json
{"is_error":true,"duration_api_ms":0,"num_turns":2,"stop_reason":null,"session_id":"bc2d1140-0e0f-4309-8b15-d28867e1d3d2","total_cost_usd":0,"usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":0,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":0,"ephemeral_5m_input_tokens":0},"inference_geo":"","iterations":[],"speed":"standard"},"modelUsage":{},"permission_denials":[],"terminal_reason":"aborted_streaming","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required","subtype":"error_during_execution","errors":["[ede_diagnostic] result_type=user last_content_type=n/a stop_reason=null"],"type":"result","duration_ms":6028,"uuid":"4da53b06-0825-407b-bb6e-bdc17d812db4"}
```

Mid-tool, abridged to the fields that differ (`interrupt-mid-tool.jsonl:53`):

```json
{"is_error":true,"duration_api_ms":56977,"num_turns":3,"stop_reason":"tool_use", ...ELIDED (usage/modelUsage as normal)... ,"permission_denials":[],"terminal_reason":"aborted_tools","subtype":"error_during_execution","errors":["[ede_diagnostic] result_type=user last_content_type=n/a stop_reason=tool_use"],"type":"result","duration_ms":59010,"uuid":"3f1da3aa-bf39-4fbb-9437-f7133e2f11cb"}
```

Eight `result` frames in this corpus are interrupted turns. On every one:

| Field | Interrupted | Normal |
|---|---|---|
| `subtype` | `"error_during_execution"` | `"success"` |
| `is_error` | `true` | `false` |
| `terminal_reason` | `"aborted_tools"` (tool running) or `"aborted_streaming"` (generating) | `"completed"` |
| `stop_reason` | `"tool_use"` or `null` | `"end_turn"` |
| `errors` | present — `["[ede_diagnostic] …"]` | **key absent** |
| `result` | **key absent entirely** | the final assistant text |
| `permission_denials` | `[]` | `[]` |

Two consequences fall straight out:

1. **`errors` and `terminal_reason` are the reliable discriminators.** `terminal_reason` even tells
   you *which* interrupt shape you got, which `subtype` does not.
2. **`result` is missing, not empty.** `protocol.go` types it `Result string`, so a `KindTurnEnd`
   from an interrupted turn arrives with `Text: ""` — the same as a turn that genuinely said
   nothing. Wake cannot currently tell an aborted turn from a quiet one.

### The cost fields go quiet, and one of them lies by omission

Two different things happen to the two usage blocks, and conflating them is easy.

**`usage` (per-turn) is all zeros on every `aborted_streaming` result** — `input_tokens`,
`output_tokens` and both cache counters `0`, `iterations: []`, and `inference_geo` an **empty
string** where a normal result says `"not_available"`. That holds in all five.

**`modelUsage` / `total_cost_usd` / `duration_api_ms` (cumulative) are carried forward untouched.**
In the four fixtures where the abort was the process's first turn they read `{}`, `0` and `0` —
which looks like a reset but is not one, it is a cumulative that never advanced.
`interrupt-cancel-queued-empty.jsonl:53` is the control: an `aborted_streaming` result *after* a
completed turn repeats `:45`'s `total_cost_usd: 0.1382016`, its whole `modelUsage` object, and even
its `duration_api_ms: 41608`, unchanged. Only `duration_ms` (1 984 vs 41 742) is the aborted turn's
own.

So the `/clear` trap in CLAUDE.md does **not** recur here — an interrupt never drives the cumulative
backwards. But `duration_api_ms` on an aborted turn is a stale copy of the previous turn's, not this
turn's, which is a fresh way for a naive per-turn metric to be wrong.

`[inference]` The tokens the model burned before the abort therefore appear **nowhere** in
`modelUsage`. Two independent processes (`interrupt-cancel-queued.jsonl:25`,
`interrupt-no-request-id.jsonl:19`) each aborted the same essay mid-generation and then ran the same
trivial "alive" turn, and both report `total_cost_usd: 0.014858999999999999` — identical to the
cent. If the aborted generation had contributed anything, those two totals would differ from each
other and from a clean baseline. This is arithmetic over the fixtures, not a statement about what
Anthropic bills. What is *observed* is narrower and enough for Wake: **the CLI reports no usage for
an interrupted-mid-generation turn.**

An `aborted_tools` result is the opposite — `interrupt-mid-tool.jsonl:53` carries a full `usage`
block and 5105 output tokens, because assistant frames had already completed. So "an interrupt is
free" is true for one shape and false for the other.

---

## 6. Does the session survive? **Yes — and the session id does not change.**

This is the load-bearing answer for Task 6.

- **Every fixture has exactly one distinct `session_id` across every frame.** Checked
  programmatically over all 305 lines. There is no `conversation_reset`, no new id, nothing like
  the `/clear` behaviour in §9 of the stream-json note.
- **The next message gets a normal turn.** Six of the ten fixtures contain an
  `error_during_execution` result followed by a `success` result **in the same process**:
  `interrupt-mid-tool` (`:53`→`:60`), `interrupt-mid-generation` (`:11`→`:19`),
  `interrupt-cancel-queued` (`:16`→`:25`), `interrupt-no-request-id` (`:10`→`:19`),
  `interrupt-queued-survives` (`:30`→`:39`) and `interrupt-cancel-queued-empty` (`:53`→`:61`).
  `interrupt-mid-tool.jsonl:54`–`:60` is the shape: `init` → `assistant "alive"` → `Stop` hooks →
  `result` `subtype: "success"`, immediately after the aborted turn.
- **Interrupting an idle session is a harmless no-op.** `interrupt-idle.jsonl` interrupts before the
  session has ever had a turn (`:5`) and again after a completed one (`:14`). Both get
  `{"still_queued":[]}`, neither produces a `result`, and the turn between them is normal.

So "stop this turn" and "stop this session" are genuinely different operations on the wire, which
is what Task 6 needed to know.

### The trap: the **process exit code** remembers

`[testimony]` Exit codes were read from `cmd.Wait()` at recording time. Nothing on stdout or stderr
records them, so there is nothing to commit — treat this bullet as observed-but-unbacked, the same
status §10 of the stream-json note gives its exit/stdout claims.

| Fixture | Last turn before stdin closed | Exit |
|---|---|---|
| `interrupt-mid-tool` | success | **0** |
| `interrupt-mid-generation` | success | **0** |
| `interrupt-idle` | success | **0** |
| `interrupt-no-request-id` | success | **0** |
| `interrupt-cancel-queued` | success | **0** |
| `interrupt-then-close` | interrupted | **1** |
| `interrupt-double` | interrupted | **1** |

**Both exit-1 runs wrote zero bytes to stderr.** `interrupt-then-close.jsonl` exists to isolate
this: a single interrupt, then stdin closes — so exit 1 is not an artefact of interrupting twice.

`[inference]` The pattern across seven runs is that the exit code follows the *last* turn's
`is_error`, not the interrupt itself. Two runs that were interrupted mid-life still exited 0 once a
normal turn followed. This is the reading that fits all seven; it is not read from the binary.

Consequence for `session.go`, which pairs a non-zero exit with stderr to build `Err()`: **an agent
you interrupt and then stop reports a bare `exit status 1` with nothing to explain it.** At 20
agents that is indistinguishable from a crash, and "stop this turn, then park it" is precisely the
sequence Wake is about to make its most common action. Wake should remember that the last turn
ended `error_during_execution` and suppress the error, or the roster will show phantom crashes.

### Park and wake still work

`interrupt-resume-after.jsonl` is a **separate process** started with
`--resume ee4d083f-03d9-4ffa-8802-c6ce33cfd6d3` — the session from `interrupt-then-close.jsonl`,
which had been interrupted mid-essay and had exited 1. It was asked what topic the essay was about
and answered `"Zero"`, on the same `session_id`, exiting 0. An interrupted session is a resumable
session, and the interrupted turn's context is still there.

---

## 7. `interrupt_cancel_queued_v1` — a field, not a separate request

**A field.** `cancel_queued: true` on the same `interrupt` request. There is no second subtype.

`[binary]` The capability string documents itself. Verbatim from 2.1.226, with the binary's
`\u2014` escapes rendered as em dashes and nothing else changed:

> `'interrupt_cancel_queued_v1'` = the interrupt control_request honors `cancel_queued:true`
> (queued and pending-dispatch commands are cancelled alongside the abort, listed on the response's
> `cancelled` field; `still_queued` is always empty — including any uuid that was mid-fold at the
> interrupt instant, since this request also aborts and the fold never delivers it).

and on the field itself:

> A Stop-means-stop-everything client (a remote UI's Stop button) sets this true so one round-trip
> halts the session; a wrapper that wants per-uuid control leaves it false and follows up with
> `cancel_async_message`.

That is Wake's decision, stated by the binary. Both halves are recorded.

### Without it, a queued message still runs

`interrupt-queued-survives.jsonl`: a blocking `Bash` running under command uuid `cd33e80a…`, a
second message `c011f484…` written to stdin while it ran, then a plain interrupt.

```
:23  command_lifecycle  c011f484…  queued
:27  control_response   {"still_queued":["c011f484-e92c-4cd6-b262-93978c2851e5"]}
:28  user/tool_result   user-rejected
:30  result             error_during_execution / aborted_tools
:31  command_lifecycle  cd33e80a…  cancelled     <- the turn that was running
:32  command_lifecycle  c011f484…  started       <- the queued one, unharmed
:39  result             success   "QUEUED-ONE"
:40  command_lifecycle  c011f484…  completed
```

**The queued message ran.** For Wake this is the whole point of the field: an operator who queued
three follow-ups and then realises the agent is on the wrong path does not want those three
follow-ups delivered to the wrong path.

### With it, the queued message is destroyed

`interrupt-cancel-queued.jsonl`, same setup but the running turn is a long generation rather than a
tool:

```
:10  command_lifecycle  c07ca05a…  queued
:13  command_lifecycle  c07ca05a…  cancelled     <- before the receipt
:14  control_response   {"still_queued":[],"cancelled":["c07ca05a-0add-414b-a0de-05ff3b59bdf8"]}
:16  result             error_during_execution / aborted_streaming
:17  command_lifecycle  5490097f…  cancelled     <- the turn that was running
:25  result             success   "alive"
```

Twelve seconds elapsed between `:16` and the next message; `SHOULD-NOT-RUN` never ran. Note the
ordering — **the `cancelled` lifecycle precedes the receipt**, and the running turn's own
`cancelled` lifecycle arrives *after* its `result`.

`interrupt-cancel-queued-empty.jsonl:51` is the empty-queue control: `cancel_queued:true` with
nothing queued answers `{"still_queued":[],"cancelled":[]}`.

---

## 8. `msg_lifecycle_v1` — a bonus, and it needs no flag

Not asked for, but it fell out of the queued recordings and Wake queues messages, so it is worth
recording. Stamp a top-level `uuid` on the user frame Wake writes:

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"…"}]},"uuid":"5490097f-85c4-41b2-b572-1a0ff10b187d"}
```

and a new **top-level frame type** appears, with no extra flag:

```json
{"type":"command_lifecycle","command_uuid":"5490097f-85c4-41b2-b572-1a0ff10b187d","state":"queued","uuid":"779b76c0-0ad3-473b-b22c-73af38d1fa6f","session_id":"ee8af758-0801-4422-ad93-7b71ccf7e861"}
```

17 of them across the fixtures. `state` observed: `queued`, `started`, `completed`, `cancelled`.
`[binary]` the enum also contains `discarded`, never observed. `command_uuid` is Wake's uuid;
`uuid` is the frame's own. `[binary]`: *"Commands enqueued without a uuid (e.g. the one-shot
`-p "prompt"` string path) emit no lifecycle events"* — which the corpus confirms, since every
un-stamped send in these recordings produced none.

This is the acknowledgement Wake needs for a queued message, and the only thing that reports a
queued message being destroyed. Note the collision the binary warns about: `cancelled` covers both
"the operator cancelled it" and "the turn that consumed it was aborted" — `5490097f…` in
`interrupt-cancel-queued.jsonl:17` is the *running* command, reported `cancelled` because it was
interrupted, not because anyone dequeued it.

---

## 9. Timing and idempotency

| Question | Answer | Fixture |
|---|---|---|
| Interrupt with no turn running, before any turn ever ran? | No-op. Receipt `{"still_queued":[]}`, no `result`, next turn normal. | `interrupt-idle.jsonl:5` |
| Interrupt with no turn running, after a turn completed? | Same no-op. | `interrupt-idle.jsonl:14` |
| Twice in a row (300 ms apart)? | First aborts, second is a no-op. Both acked, both `"success"`. Second receipt arrives after the `result`. No error, no second `result`. | `interrupt-double.jsonl:39,42,43` |
| Mid-tool vs mid-text? | Materially different: `aborted_tools` + a rejected `tool_result` + `[…for tool use]`, vs `aborted_streaming` + no tool frames + `[Request interrupted by user]`. Cost reporting differs too (§5). | `interrupt-mid-tool` / `interrupt-mid-generation` |
| Does it race a turn that is already ending? | Yes, harmlessly — you get the idle no-op. In an early probe an interrupt sent 2 s after a `tool_use` arrived *after* the turn had already ended (the tool had been blocked instantly); the receipt landed after the `result` and nothing was aborted. | not committed; behaviour identical to `interrupt-idle.jsonl:14` |

Idempotency, stated precisely: **an interrupt is not idempotent in what it does** (the first aborts
a turn, the second does not) **but it is idempotent in what it costs** — every extra interrupt is a
`"success"` receipt and nothing else. Wake can fire one on every Esc without debouncing.

`[inference]` A caveat on the double case: the two interrupts were 300 ms apart, and the abort had
already produced its `result` before the second landed (`:42` before `:43`). Two interrupts arriving
inside the *same* abort window were not recorded, so "the second is always a no-op" is one
observation, not a proven invariant.

### Background tasks are not interrupted

`interrupt-cancel-queued-empty.jsonl` is the one fixture where the agent put the waiting command in
a background task instead of a foreground `Bash`. The interrupt at `:51` aborted the turn; the task
kept running through the *following* turn and only died at process teardown (`:63`,
`"patch":{"status":"killed"}`). `[inference]` — the kill coincided with stdin closing, so this shows
the task surviving the interrupt but does not establish what would have killed it otherwise. Worth a
follow-up before Wake promises "stop" stops everything.

---

## 10. Consequences for Wake

Run against the current `DecodeLine`, all ten fixtures (305 lines) decode with **zero errors** —
the log-and-skip default branch does its job. What they decode *to* is the problem:

| Frame | Today's `Event` | Why it matters |
|---|---|---|
| `control_response` ×12 | `KindUnknown`, text `"control_response"`, **`RequestID` dropped** | the default branch in `DecodeLine` never populates `RequestID`, so the receipt cannot be matched to the interrupt that caused it |
| `command_lifecycle` ×17 | `KindUnknown` | the only signal that a queued message was accepted or destroyed |
| `result` (interrupted) ×8 | `KindTurnEnd`, `Text: ""` | indistinguishable from a turn that ended quietly. `subtype`, `is_error`, `terminal_reason` and `errors` are all discarded |
| `[Request interrupted by user]` | `KindUserText`, `Echoed: false` | renders as if the operator typed it |
| `system/background_tasks_changed`, `task_started`, `task_updated`, `task_notification` | `KindSystem` | four subtypes not in the existing corpus; harmless, but they exist |

The first four rows are why the fixtures were nested rather than committed flat (see the header):
`TestDecodeRecordedFixtures` treats every one of those 29 unknowns as a failure. Promoting the
directory turned the test into a precise, self-describing checklist for the work below.

**Status 2026-08-09.** Rows 1 and 2 are done: `control_response` decodes to `KindControlReceipt`
carrying `RequestID` and both arrays, `command_lifecycle` to `KindMessageState` carrying
`command_uuid` and the state. Rows 3–5 are untouched, and one correction to this table:
`control_response` was recorded as **four** distinct payload shapes, not the three listed in §3 —
`{"still_queued":[],"cancelled":[]}` from `interrupt-cancel-queued-empty.jsonl:51` is the fourth,
and §3's own prose names it two paragraphs after the list. The distinction is load-bearing, so the
decoder keeps absent and present-but-empty apart.

Concretely, and staying inside the airlock rule:

1. **`protocol.go` needs an `EncodeInterrupt(requestID string, cancelQueued bool)`** alongside
   `EncodeAllow`/`EncodeDeny`. Same shape family, same `request_id` non-empty check — and here the
   check has teeth for a different reason: the CLI *accepts* an empty one and answers unattributably
   (§2).
2. **`control_response` needs decoding, not defaulting.** At minimum carry `request_id` and the
   `still_queued` / `cancelled` arrays.
3. **`KindTurnEnd` needs to carry whether the turn was aborted.** Discriminate on
   `terminal_reason` — it is the only field that separates `aborted_tools` from `aborted_streaming`
   from `completed`, and unlike `subtype` it does not collapse them.
4. **`session.go`'s `Err()` must not report a bare `exit status 1` after an interrupt** (§6).
5. **A denial and an interrupt both say `user-rejected`** (§11). If Wake ever renders "you denied
   this", `permission_denials` is the field that tells the truth.

---

## 11. Corrections to `2026-08-08-stream-json-findings.md`

Two claims in that note are now falsified, and both are load-bearing enough to be repeated in
CLAUDE.md:

1. **§8: "`subtype` was `"success"` in all 19 results… `is_error: false` does not mean nothing went
   wrong."** Directionally right, factually incomplete. `result.subtype` also takes
   `"error_during_execution"`, with `is_error: true`, and the `errors` key only exists on that path.
   §12 of that note lists *"`result.subtype` values other than `"success"`"* as unverified — this
   closes it.

2. **§6 / §5a: "`permission-rule` means a human said no, `user-rejected` means nobody was there to
   ask."** The second half is wrong: an **interrupt** also produces
   `non_execution_kind: "user-rejected"` with `tool_use_result: "User rejected tool use"`
   (`interrupt-mid-tool.jsonl:51`). The two are not distinguishable on that field. They are
   distinguishable elsewhere — an auto-deny lands in `result.permission_denials` and ends the turn
   `"success"`; an interrupt leaves `permission_denials` empty and ends it
   `"error_during_execution"`.

Also closed from that note's §12: *"`interrupt` control requests, despite `interrupt_receipt_v1`
appearing in `init.capabilities`"* — recorded here, ten ways.

---

## 12. Observed vs inferred

Separated deliberately, because the previous spike spent its later rounds untangling exactly this.

**Observed on stdout, fixture-backed** — every wire shape in §2 (what was sent), §3, §4, §5, §7, §8;
one session id per fixture across all 305 lines; receipt-before-result ordering; the idle and double
no-ops; the queued/cancelled A/B; resume-after-interrupt.

**Read from the 2.1.226 binary, not observed** — the `reason` field and its value set; the
`cancel_queued` and `still_queued`/`cancelled` prose quoted in §7; the `discarded` lifecycle state;
the claim that un-stamped commands emit no lifecycle events (consistent with the corpus, but the
corpus cannot prove a negative about a mechanism it never exercised).

**Testimony, nothing committed** — the seven exit codes in §6.

**Inferred from the fixtures** — and each of these is a reading that could be wrong without
disturbing anything above it:

- that the exit code follows the last turn's `is_error` rather than the interrupt (§6);
- that an interrupted-mid-generation turn contributes nothing to `modelUsage` (§5) — the observation
  is only that two processes report identical totals;
- that a second interrupt inside a *single* abort window is also a no-op (§9);
- that background tasks survive an interrupt as such, rather than merely surviving until teardown
  (§9).

---

## 13. Unverified — do not design around these

- **`reason` on the interrupt request.** Never sent. Its effect on tool behaviour, on
  `terminal_reason`, and on whether it surfaces anywhere on stdout are all unknown. `[binary]` says
  tool implementations branch on it.
- **`cancel_async_message`** — `[binary]` a sibling control subtype, *"Drops a pending async user
  message from the command queue by uuid"*, with a `{cancelled: bool}` result. It is the per-uuid
  alternative to `cancel_queued`, and Wake may want it. Never sent.
- **Interrupting without `--permission-prompt-tool stdio`.** Every recording passed it because Wake
  does. Nothing suggests it matters; it was not tested.
- **Interrupting a session with a permission request outstanding.** The most likely real-world
  collision at fleet scale — an agent blocked on a `can_use_tool` that the operator interrupts
  instead of answering. Not recorded. Whether the pending `control_request` is withdrawn, answered,
  or left dangling is unknown.
- **Interrupting a subagent's turn** (`--forward-subagent-text`, non-null `parent_tool_use_id`).
  `parent_tool_use_id` was `null` on every frame here.
- **A `cancel_queued` interrupt against a queue deeper than one.** Every queued recording had
  exactly one message queued.
- **Whether an interrupt during `/compact` behaves like either shape.** Not tried.
- **`discarded` lifecycle state**, and whether a lifecycle frame ever arrives for a message Wake did
  not stamp.
- **The interrupt's effect on a background task in isolation** (§9).
- **Two interrupts inside one abort window** (§9).
- Whether `still_queued` can be non-empty *and* the request set `cancel_queued:true` — `[binary]`
  says never; not falsifiable from one recording.

---

## 14. Method

Recorded with a throwaway `scripts/spike-interrupt` (deleted in the commit that adds these
fixtures), a Go driver that spawns `claude`, tees stdout to the fixture byte-for-byte through an
`io.TeeReader`, auto-allows any `can_use_tool` by echoing `request.input` back, and fires scripted
interrupts on line triggers. Each recording used a fresh `uuidgen` for the session id, for every
`request_id`, and for every stamped command uuid; ran with
`cwd = <scratchpad>/int/cwd` (a directory outside the repo, so the recordings carry no Wake source);
scrubbed the inherited `CLAUDECODE` / `CLAUDE_CODE_*` / `CLAUDE_PID` / `CLAUDE_EFFORT` /
`CLAUDE_PLUGIN_DATA` env; and invoked `/Users/dev/.local/bin/claude` directly to bypass the cmux
shim described in §1 of the stream-json note. `interrupt-resume-after.jsonl` used no driver — a
bare shell pipeline with `--resume`. A second throwaway binary alongside it ran the committed
`DecodeLine` over the finished fixtures to produce §10's table; it read `internal/core` and changed
nothing.

Getting a genuinely long-running tool call took three attempts and the failures are worth recording,
because they will bite the next person:

1. `sleep 45` is **blocked by the CLI itself**, not by a user hook —
   `<tool_use_error>Blocked: standalone sleep 45. To wait for a condition, use Monitor with an
   until-loop…</tool_use_error>`. The turn ends in ~10 s and the interrupt arrives too late.
2. `until [ -f /tmp/…]; do sleep 2; done` with no explanation gets **refused by the model** about a
   third of the time ("it won't ever terminate"), or routed to a background `Monitor` task, in
   which case the foreground turn ends immediately.
3. The reliable way to hold a turn open is not a tool at all — it is a long *generation*
   (a 1500-word essay), which is what the mid-generation and `cancel_queued` fixtures use. It needs
   no cooperation from the model beyond starting.

Fixture integrity was checked before commit, over all 305 lines: every line parses as JSON; every
`uuid` is RFC 4122 **version 4**; exactly one distinct `session_id` per file; ten distinct pids in
`init.messaging_socket_path` (`/tmp/cc-socks/<pid>.sock`), confirming ten separate processes;
`init.apiKeySource` is `"none"` on every one and no credential-shaped strings appear. The fixtures
do contain this machine's absolute paths and the user's installed skill/plugin/MCP inventory, which
is expected for a recording.

Which driver scenario produced which fixture:

| Fixture | Scenario |
|---|---|
| `interrupt-mid-tool` | `midtool` — blocking `Bash`, interrupt 2 s after `tool_use`, then a follow-up turn |
| `interrupt-mid-generation` | `midtext` — long essay, interrupt 6 s after `init`, then a follow-up turn |
| `interrupt-idle` | `idle` — interrupt at t+1 s before any turn, a turn, then a second interrupt |
| `interrupt-double` | `double` — blocking `Bash`, two interrupts 300 ms apart, stdin closed |
| `interrupt-queued-survives` | `queued-survive` — blocking `Bash`, stamped message queued mid-turn, plain interrupt |
| `interrupt-cancel-queued` | `cancel-queued-text` — long essay, stamped message queued mid-turn, `cancel_queued:true` |
| `interrupt-cancel-queued-empty` | `cancel-queued` — the attempt that routed to a background task; kept for the empty-queue receipt |
| `interrupt-no-request-id` | `no-request-id` — raw line with no `request_id`, then a well-formed one |
| `interrupt-then-close` | `text-close` — one interrupt, stdin closed immediately |
| `interrupt-resume-after` | none — bare `--resume` pipeline, quoted in full below |

```bash
echo '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"In one word, what topic did I ask you to write an essay about?"}]}}' \
  | env -u CLAUDECODE -u CLAUDE_CODE_ENTRYPOINT -u CLAUDE_CODE_SESSION_ID \
        -u CLAUDE_CODE_CHILD_SESSION -u CLAUDE_CODE_EXECPATH -u CLAUDE_PID \
        -u CLAUDE_EFFORT -u CLAUDE_PLUGIN_DATA \
    /Users/dev/.local/bin/claude \
      --print --input-format stream-json --output-format stream-json --verbose \
      --permission-prompt-tool stdio --resume ee4d083f-03d9-4ffa-8802-c6ce33cfd6d3 \
      --name spike --permission-mode auto --brief --include-hook-events
```

which exited 0 and answered `"Zero"`.
