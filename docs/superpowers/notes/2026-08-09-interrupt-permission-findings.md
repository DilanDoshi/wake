# Interrupting a session with a permission request outstanding

Recorded 2026-08-09 against **Claude Code 2.1.226**, macOS 15 (arm64).
Closes the fourth bullet of §13 of `2026-08-08-interrupt-findings.md` — *"Interrupting a session
with a permission request outstanding… Whether the pending `control_request` is withdrawn,
answered, or left dangling is unknown"* — which that note called the most likely real-world
collision at fleet scale.

**It is withdrawn, and the CLI says so on a frame type nothing in this corpus had ever seen** — and
it says so **before** it ends the turn, which is what makes the collision benign in Wake today
(§2a). Neither of the two failures named before this ran can happen on the recorded ordering: the
turn-end frame always arrives, and the ask is always already dead when it does.

Three fixtures, **85 lines**, three distinct processes, all recorded with **Wake's exact
`buildArgs`** (§1). Every wire shape below was observed on the real stdout of a real `claude`
process; nothing is hand-written and nothing comes from `~/.claude/projects/*.jsonl`. Where a
quoted line is shortened the elision is marked inline with `...ELIDED...`; every other quoted line
is byte-for-byte what came out of the pipe.

Claims that are **not** stdout observations are labelled where they appear: `[testimony]` for exit
codes and filesystem state, `[inference]` for anything reasoned from the fixtures rather than read
off them. §7 collects what this run did **not** establish.

| File | Lines | What it proves |
|---|---:|---|
| `interrupt-pending-basic.jsonl` | 25 | the collision: ask outstanding 2.0 s, interrupt instead of an answer. `control_cancel_request` → receipt → rejected tool → `aborted_tools` **in that order**, then a normal turn on the same session id |
| `interrupt-pending-answer-after.jsonl` | 25 | the same collision, then a well-formed `control_response` **allow** for the abandoned `request_id`. Nothing comes back, nothing runs, the process is fine |
| `interrupt-pending-no-interrupt.jsonl` | 35 | **the control**: identical argv and prompt, ask left outstanding **30.1 s with no interrupt**, then allowed. No `control_cancel_request`, no timeout, the tool runs |

The third file is the one that makes the first two mean anything. Without it, "the ask was
withdrawn" is equally consistent with an internal deadline on unanswered asks.

---

## 1. The invocation

This is `session.go`'s `buildArgs` **exactly**, including `--forward-subagent-text`, which the
earlier interrupt recordings dropped. `PermissionMode` is `manual`; `Model` and `Effort` are empty,
so no `--model` and no `--effort`, which is how Wake spawns today.

```
claude --print
       --input-format stream-json
       --output-format stream-json
       --verbose
       --permission-prompt-tool stdio
       --brief
       --include-hook-events
       --forward-subagent-text
       --session-id <fresh uuidgen>
       --name spike
       --permission-mode manual
```

Two consequences of matching Wake rather than the earlier corpus:

- **The model is `claude-opus-5[1m]`, not `claude-sonnet-5`.** Wake passes no `--model`, so the CLI
  used this machine's configured default. `init.model` reads `"claude-opus-5[1m]"` on all six
  `init` frames. Nothing here depends on the model, but a diff against the earlier fixtures will
  show it.
- **`--forward-subagent-text` is present and no subagent ran**, so `parent_tool_use_id` is `null` on
  all 15 frames that carry it. The flag changed nothing observable here.

`init.permissionMode` reads `"default"` on every `init` despite `--permission-mode manual` — the
normalization trap CLAUDE.md already records, reconfirmed.

`init.capabilities` is unchanged from the earlier corpus:

```json
"capabilities":["interrupt_receipt_v1","interrupt_cancel_queued_v1","msg_lifecycle_v1"]
```

**There is no capability string for the withdrawal frame in §2.** It is not announced.

### Provoking the collision

The prompt was the same in all three runs:

> Use the Write tool right now to create a file called note.txt in the current working directory
> whose entire contents are the single word ok. Do not read anything first, do not run any other
> tool, do not explain. Just make the Write tool call.

Unlike the blocking-`Bash` problem the earlier note spends §14 on, this needs no cooperation from
the model beyond one tool call, and `--permission-mode manual` does the blocking. It worked first
time, three times out of three: `can_use_tool` arrived 5.3 s, 5.6 s and 11.3 s after the prompt in
the three runs.

---

## 2. Question 1 — the ask is **withdrawn**, on a frame type Wake has never seen

The ask, `interrupt-pending-basic.jsonl:13`:

```json
{"type":"control_request","request_id":"72f34560-c3e6-4e40-8909-75a81b9a7888","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"/private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/permint/cwd/note.txt","content":"ok"},"description":"note.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_01UB3VfL4tqoBqCRK3h3nrkG"}}
```

The interrupt went out 2.0 s later with the ask unanswered. **The very next line on stdout**, `:14`,
verbatim and complete — this is the whole frame, not an excerpt:

```json
{"type":"control_cancel_request","request_id":"72f34560-c3e6-4e40-8909-75a81b9a7888"}
```

- **`type` is `control_cancel_request`.** It appears **zero times** in the other 26 fixtures and
  919 lines of `testdata/stream/`. It is new to this corpus.
- **It carries the `request_id` of the ask**, byte-identical to `:13`'s. A client can match it.
- **Two keys, and that is all**: `type` and `request_id`. **No `session_id`**, no `uuid`, no reason,
  no subtype — the same attribution shape as the `can_use_tool` it cancels, and the same reason
  `request_id` is the only correlator that exists.
- **It arrives *before* the interrupt's own receipt** (`:15`) and before the rejected `tool_result`
  (`:16`). Withdrawal is the first thing on the wire, not a footnote after the abort.

`interrupt-pending-answer-after.jsonl:14` is the independent second observation, same shape, same
position:

```json
{"type":"control_cancel_request","request_id":"0864a06e-9ef7-4bf5-bef6-17c9295ee2d4"}
```

### The control: nothing else withdraws an ask

`interrupt-pending-no-interrupt.jsonl` is the same argv and the same prompt with the interrupt
removed. The ask (`:12`, `request_id` `2e015d75-2565-4975-8c81-21987063b1b3`) was written at driver
time `12:43:44.347` and answered `allow` at `12:44:14.466` — **30.1 s untouched**. The file contains
**no `control_cancel_request` at any line**. The only frame that could have been emitted inside
those 30.1 s is the `rate_limit_event` at `:13`, which carries no timestamp; `:14`–`:21` are eight
`PostToolUse:Write` hook frames, which cannot precede the answer that ran the tool. The tool result,
`:22`:

```json
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01NDpSP557gYPCq8gkNAKFCu","type":"tool_result","content":"File created successfully at: /private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/permint/cwd/note.txt (file state is current in your context — no need to Read it back)"}]},"parent_tool_use_id":null,"session_id":"ea6a6e09-3691-4b1e-87df-adc722e255ff","uuid":"6e5a3f91-6e70-40f3-8915-6fd58bd0a186","timestamp":"2026-08-09T19:44:14.512Z","tool_use_result":{"type":"create","filePath":"/private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/permint/cwd/note.txt","content":"ok","structuredPatch":[],"originalFile":null,"userModified":false}}
```

and the turn ended clean, `:28` from `permission_denials` to the end of the line, verbatim:

```json
"permission_denials":[],"terminal_reason":"completed","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required","subtype":"success","api_error_status":null,"result":"Created `note.txt` containing `ok`.","ttft_ms":7850,"ttft_stream_ms":4096,"time_to_request_ms":19,"type":"result","duration_ms":40971,"uuid":"29d0a312-2abe-4d82-affb-c8c139778721"}
```

So the ask survives at least 30 s of silence and is still answerable at the end of it. **The
withdrawal in `:14` is caused by the interrupt.** *(The 30.1 s is the length of the test, not a
proven bound — see §7.)*

### 2a. The ordering — withdrawal **first**, turn-end **last**

A client processes these in arrival order, so the order is the answer, not a detail. Both collision
fixtures are byte-identical in shape here, `:13`–`:18`:

| Line | Frame | Wake's `Event` today |
|---:|---|---|
| `:13` | `control_request` / `can_use_tool` — the ask | `KindPermissionRequest` → `agent.pending` set, `StateBlocked` |
| `:14` | **`control_cancel_request`** — the ask is dead, named by `request_id` | `KindUnknown`, id dropped |
| `:15` | `control_response` — the interrupt receipt | `KindControlReceipt` |
| `:16` | `user` / `tool_result` — `user-rejected` | tool result |
| `:17` | `user` / `[Request interrupted by user for tool use]` | `KindUserText` |
| `:18` | `result` — `error_during_execution` / `aborted_tools` | **`KindTurnEnd`** → `agent.pending` clears |

*(The right-hand column is read off the committed code, not off the stream: `protocol.go` maps
`"result"` to `KindTurnEnd` and everything unrecognised to `KindUnknown`, and `agent.go`'s `note`
sets `a.pending = ev.RequestID` on `KindPermissionRequest` and clears it on `KindTurnEnd`, with
`a.pending != ""` returning `rpc.StateBlocked`. The golden test in §8 independently confirms `:14`
is the **only** line in these fixtures that reaches the `KindUnknown` branch.)*

Two things follow, and they close both halves of the failure that was feared:

1. **A turn-end frame does arrive.** `:18` is a `result`, so `KindTurnEnd` fires and `pending`
   clears. The "withdrawn silently, `pending` never clears, `StateBlocked` forever" failure
   **requires a withdrawal with no `result`, and that is not what the CLI does here.**
2. **The ask is already dead when the turn-end arrives**, not still live. The withdrawal at `:14`
   precedes the `result` at `:18` by four frames, and §6 confirms independently that the CLI has
   genuinely let go: a well-formed `allow` for that `request_id`, written *after* `:18`, does
   nothing at all. So the inverse failure — `pending` clearing while the process is still blocked,
   Wake reporting a blocked agent as **idle** with no attention signal — **also does not occur**.
   When `pending` clears there is nothing left to be blocked on.

**So the collision is benign in Wake today**, and the reason is ordering rather than anything Wake
does: the CLI puts the withdrawal first and the turn-end last, and Wake happens to act on the last
one. What is left is not a wedge but a **lie on screen** — Wake cannot see `:14` (§8), so a
permission prompt the operator is looking at stays up after the request behind it is gone, and
their answer to it goes into the void (§6). That is a UI-correctness bug, not a stuck agent.

The whole sequence is fast: the interrupt was written at `19:41:42.122` and `:16` is stamped
`19:41:42.123`, `:17` `19:41:42.124`. `:14`, `:15` and `:18` carry no timestamp, so the window in
which Wake shows `StateBlocked` on an already-dead ask is bounded by the frames either side of it —
**single-digit milliseconds**, and never observed to be longer. `[inference]` that it is always this
short: three recordings, all with one ask and a tool that had not started.

---

## 3. Question 2 — the receipt says **nothing** about the blocked tool call

`interrupt-pending-basic.jsonl:15`, verbatim:

```json
{"type":"control_response","response":{"subtype":"success","request_id":"7c630a06-1598-4d11-8494-13216aa7697d","response":{"still_queued":[]}}}
```

- `subtype` is `"success"`, as it is on all 12 receipts in the earlier corpus and both here — 14
  now, still no counterexample. It remains transport-level, not a verdict.
- **`still_queued` is `[]`.** The blocked tool call is not a queued *message*, and it does not
  appear there.
- **`cancelled` is absent.** The request did not set `cancel_queued`, and the earlier note's rule
  holds: the key tracks the request, not what was destroyed. The ask was destroyed anyway and no
  key on the receipt mentions it.
- The `request_id` is the **interrupt's** (`7c630a06…`), not the ask's (`72f34560…`). The two ids
  never meet on one frame; the only frame naming the ask is `control_cancel_request`.

`interrupt-pending-answer-after.jsonl:15` is identical in shape. **The receipt is not where a client
learns the ask is dead.** Anything watching only the receipt sees a payload it cannot distinguish
from an interrupt that hit an idle session.

---

## 4. Question 3 — no third turn-end shape

`interrupt-pending-basic.jsonl:18`, abridged to the fields that matter — the head of the line, then
everything from `permission_denials` to the end of it verbatim:

```json
{"is_error":true,"duration_api_ms":3332,"num_turns":3,"stop_reason":"tool_use", ...ELIDED (session_id, total_cost_usd, usage, modelUsage)...
"permission_denials":[{"tool_name":"Write","tool_use_id":"toolu_01UB3VfL4tqoBqCRK3h3nrkG","tool_input":{"file_path":"/private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/permint/cwd/note.txt","content":"ok"}}],"terminal_reason":"aborted_tools","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required","subtype":"error_during_execution","errors":["[ede_diagnostic] result_type=user last_content_type=n/a stop_reason=tool_use"],"type":"result","duration_ms":5388,"uuid":"796d6b07-82ce-44f5-9339-daa51f17256d"}
```

`subtype: "error_during_execution"`, `terminal_reason: "aborted_tools"`, `is_error: true`,
`stop_reason: "tool_use"`, `errors` present, **no `result` key**. `interrupt-pending-answer-after.jsonl:18`
is the same on every one of those. **There is no third `terminal_reason`** — a pending *ask* reads
as `aborted_tools`, the same as a running *tool*.

The tool result and the bracket string are the mid-tool shapes too, `:16` and `:17`:

```json
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user doesn't want to proceed with this tool use. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file). STOP what you are doing and wait for the user to tell you how to proceed.","is_error":true,"tool_use_id":"toolu_01UB3VfL4tqoBqCRK3h3nrkG"}]},"parent_tool_use_id":null,"session_id":"d17c58ad-602c-48df-b03f-2140030fca45","uuid":"00eedada-da79-4353-a1f9-d5946f36a885","timestamp":"2026-08-09T19:41:42.123Z","tool_use_result":"User rejected tool use","tool_result_meta":[{"id":"toolu_01UB3VfL4tqoBqCRK3h3nrkG","non_execution_kind":"user-rejected"}]}
```

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]},"parent_tool_use_id":null,"session_id":"d17c58ad-602c-48df-b03f-2140030fca45","uuid":"360e5545-4912-46f4-9200-7e85ab42bea2","timestamp":"2026-08-09T19:41:42.124Z"}
```

`[Request interrupted by user for tool use]` — the **"for tool use"** literal, even though no tool
ever started running. The two literals in `2026-08-08-interrupt-findings.md` §4b remain the whole
set; this case does not add a third.

### But `permission_denials` is **not** empty, and that falsifies a documented discriminator

`2026-08-08-interrupt-findings.md` §4a states:

> `result.permission_denials` is `[]` on every interrupted turn, so the interrupt does **not** show
> up as a permission denial. That is the field that separates them.

and CLAUDE.md repeats it:

> a denial ends the turn `subtype: "success"` with the tool listed in `result.permission_denials`;
> an interrupt ends it `"error_during_execution"` with `permission_denials` empty.

**Both are false for this case.** `:18` above ends `error_during_execution` **with the `Write`
listed in `permission_denials`**, and `interrupt-pending-answer-after.jsonl:18` does the same with
its own `tool_use_id`. The earlier claim was true of the runs that recorded it — an interrupt
landing on a tool that was *already running*, which was never a permission denial — and it does not
generalise to an interrupt landing on a tool that was *waiting to be allowed*.

The pair still separates, on `subtype` rather than on `permission_denials`. Against the corpus'
own denial fixtures (`permission-denied.jsonl:25`, `permission-deny-response.jsonl:25`, both
`subtype: "success"`, `terminal_reason: "completed"`, `is_error: false`, denials populated):

| | operator denies | interrupt with ask outstanding | interrupt with tool running |
|---|---|---|---|
| `subtype` | `success` | `error_during_execution` | `error_during_execution` |
| `terminal_reason` | `completed` | `aborted_tools` | `aborted_tools` |
| `is_error` | `false` | `true` | `true` |
| `permission_denials` | populated | **populated** | `[]` |
| `non_execution_kind` | `permission-rule` | `user-rejected` | `user-rejected` |

So `permission_denials` no longer answers "was this denied or interrupted"; it answers "did a tool
fail to run", and `subtype` answers the rest. Anything in Wake that renders "you denied this" must
read `subtype`/`terminal_reason` first.

`usage` on `:18` is a full block (`output_tokens: 145`, `total_cost_usd: 0.2197505`), consistent
with §5 of the earlier note: an `aborted_tools` result is not free.

---

## 5. Question 4 — the process is **still usable**, and Wake must not respawn

This is the one that decides an implementation, so it is recorded in both collision fixtures.

`interrupt-pending-basic.jsonl:20`, the next turn's assistant frame:

```json
{"type":"assistant","message":{"model":"claude-opus-5","id":"msg_011Cdsk8EyhZZEgqNBKumPhF","type":"message","role":"assistant","content":[{"type":"text","text":"ALIVE"}], ...ELIDED (stop_reason, usage, diagnostics, context_management)... },"parent_tool_use_id":null,"session_id":"d17c58ad-602c-48df-b03f-2140030fca45","uuid":"f7a82f66-4530-45b0-8870-7c85a3c26eb8","timestamp":"2026-08-09T19:41:44.970Z","request_id":"req_011Cdsk8DkXs2Hi4zNiDsFVe"}
```

and `:25`, its result:

```json
{"is_error":false,"duration_api_ms":4668,"num_turns":1,"stop_reason":"end_turn","session_id":"d17c58ad-602c-48df-b03f-2140030fca45", ...ELIDED (total_cost_usd, usage, modelUsage)... ,"permission_denials":[],"terminal_reason":"completed","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required","subtype":"success","api_error_status":null,"result":"ALIVE","ttft_ms":1344,"ttft_stream_ms":1344,"time_to_request_ms":11,"type":"result","duration_ms":1422,"uuid":"2773174a-94d9-4542-a790-ade41647e7dc"}
```

The message was sent 1.5 s after the aborted `result` and answered in 1.4 s. `session_id` is
unchanged across all 25 lines of the file (checked programmatically over all three fixtures: exactly
one distinct `session_id` each). `interrupt-pending-answer-after.jsonl:25` is the same, `"result":"ALIVE"`,
after an intervening late `control_response` that changed nothing.

**Wake does not need to respawn a session it interrupted mid-ask.** Send the next message.

`[testimony]` All three processes exited **0** with **empty stderr**, read from `cmd.Wait()` at
recording time. In each, the last turn before stdin closed was a `success`, so this is consistent
with — and does not independently test — the earlier note's reading that the exit code follows the
last turn's `is_error`.

---

## 6. Question 5 — a late answer is **silently ignored**

`interrupt-pending-answer-after.jsonl` sent this on stdin 1.5 s after the aborted `result`, using
the `request_id` from the withdrawn ask and echoing its `input` back unchanged — a well-formed
`EncodeAllow` frame in every respect:

```json
{"response":{"request_id":"0864a06e-9ef7-4bf5-bef6-17c9295ee2d4","response":{"behavior":"allow","updatedInput":{"content":"ok","file_path":"/private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/permint/cwd/note.txt"}},"subtype":"success"},"type":"control_response"}
```

*(An outbound frame, so it is not on any recording — it is copied from the driver log. Everything
quoted from here on is stdout again.)*

**Nothing came back.** The frames either side of the write, `:17` and `:20`, carry the timestamps
that bracket it:

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}, ...ELIDED... ,"timestamp":"2026-08-09T19:42:57.588Z"}
```

```json
{"type":"assistant", ...ELIDED (message envelope)... ,"content":[{"type":"text","text":"ALIVE"}], ...ELIDED (stop_reason, usage, diagnostics)... ,"parent_tool_use_id":null,"session_id":"0284c02a-55e3-4fdd-89d4-e0b992f7b59b","uuid":"956c1c37-c2ec-4c99-90c2-469cc4488c97","timestamp":"2026-08-09T19:43:05.617Z","request_id":"req_011CdskE9xkQCEiQ8CyPMfGY"}
```

The `control_response` went out at **19:42:59.089Z**, inside that window. The only lines the fixture
has in it are `:18` (the aborted `result`, emitted at 19:42:57.6) and `:19` (the `init` for the
probe turn Wake sent at 19:43:04.090Z). **Between them: nothing.** No error frame, no second
receipt, no `control_cancel_request`, no `tool_result`, no assistant text.

Three further facts pin it down:

- **The tool did not run.** `[testimony]` `ls` of the scratch cwd immediately after the process
  exited showed `a.txt`, `b.txt`, `notes.md` and **no `note.txt`** — while the control run, whose
  identical `allow` was answered in time, did create it (2 bytes, `ok`).
- **The process did not die.** `:20`/`:25` are a normal turn *after* the late answer, and the exit
  code was 0 with empty stderr. `session.go`'s comment that one bad line on stdin kills the process
  does not apply: **a `control_response` for an abandoned `request_id` is not that kind of bad
  line**, exactly as the earlier note found for a `control_request` with no `request_id` at all.
- **It is not accepted-and-deferred either.** The next turn ran to `terminal_reason: "completed"`
  with `permission_denials: []` and no `Write` anywhere in it.

**Ignored. Not answered, not errored.** For Wake this is the benign outcome: an operator who hits
`⎋` and then, half a second later, hits allow on a prompt that is already gone does no damage.

---

## 7. What this did **not** establish

The equivalent section in `2026-08-08-interrupt-findings.md` is what stopped an unrecorded field
being implemented on a guess. Nothing below is safe to design around.

- **What `control_cancel_request` means outside this one collision.** Three recordings, one cause.
  Whether it also fires when an ask is abandoned for any other reason — the turn erroring out, the
  process shutting down, a `/clear`, a hook killing the tool — is unrecorded. **Do not read it as
  "interrupt happened".** It says one request is dead and names it; that is all that was observed.
- **Whether it is the *only* withdrawal signal.** A recording proves what appeared, never that
  nothing else can. There may be paths that abandon an ask silently, and the wedge this spike was
  written to find would still exist on them.
- **Whether a withdrawal can ever arrive with no `result` behind it.** This is the exact shape of
  the feared wedge, and all three recordings had a `result` four frames after the withdrawal. Three
  runs of one scenario cannot prove that pairing is invariant — a turn that was *only* a permission
  ask, with the interrupt racing the turn's own end, is the case most likely to break it and was not
  recorded.
- **Whether the ask is ever still live when the `result` lands.** §6 shows it is dead *after* the
  `result` in the one scenario tested. The window between `:14` and `:18` was never probed from the
  outside — an `allow` written *between* them, rather than after, would test the inverse failure
  directly and was not attempted.
- **Its full schema.** Two keys were observed. Nothing was read out of the 2.1.226 binary for this
  note — unlike the earlier one, there is **no `[binary]` claim here at all** — so whether
  `control_cancel_request` has optional fields (a reason, a session id, a subtype) is unknown.
- **Whether the ask blocks *indefinitely*.** The control held one open **30.1 s**. That is the
  measurement. A longer internal deadline is not excluded, and Wake's operators will hold asks open
  for minutes.
- **`cancel_queued: true` in this collision.** Every interrupt here was the plain form. Whether the
  receipt's `cancelled` array would name the ask, or only queued *messages*, was not tested — and
  §3 gives a reason to doubt it would name the ask.
- **A late *deny* rather than a late allow.** Only `behavior: "allow"` was sent after the
  withdrawal. A deny carries a `message` that reaches the model verbatim, so it is not obviously
  the same path.
- **More than one ask outstanding at once.** One `can_use_tool` per run. Whether an interrupt emits
  one `control_cancel_request` per outstanding request, or one for the batch, is unrecorded — and
  it is the case a parallel-tool turn will produce.
- **A subagent's ask.** `--forward-subagent-text` was passed but no subagent ran;
  `parent_tool_use_id` is `null` on all 15 frames that carry it. `subagent-permission.jsonl` shows a
  subagent ask carries an extra `agent_id`, and whether the cancel frame carries one too is unknown.
- **Interrupting *between* the ask and the tool actually starting**, i.e. racing an answer already
  in flight. The interrupt here always arrived with the ask cleanly unanswered.
- **Whether a *second* interrupt after the withdrawal is still the documented no-op.** Not tried in
  this shape.
- **Anything about `agent.pending` in the daemon.** This spike recorded the CLI. It did not run
  Wake, and it did not test `internal/daemon`.

---

## 8. Consequences for Wake — and the golden test

### Neither feared failure happens — and the reason is ordering, not code

Two failures were named before this ran, and §2a rules out both against the bytes:

| Feared failure | Requires | Recorded |
|---|---|---|
| ask withdrawn silently, `pending` never clears, agent reads `StateBlocked` forever | a withdrawal with **no `result`** | a `result` arrives every time, `:18`, four frames after the withdrawal |
| `result` arrives while the ask is **still live**, `pending` clears, a blocked agent reads **`StateIdle`** with no attention signal | the ask alive at turn-end | the withdrawal is `:14`, the `result` `:18`; §6 shows the CLI has genuinely let go |

The bytes show a third thing, and it is the good one: **the CLI withdraws the ask first and ends
the turn last**, so a client acting on either frame lands in the correct state. `KindTurnEnd`
clears `pending` at a moment when there is provably nothing left to be blocked on.

### The CLI does the right thing; **Wake currently cannot hear it**

That leaves a smaller, real defect. The CLI names the dead request; Wake drops the name. Today
`control_cancel_request` falls to `DecodeLine`'s default branch:

```go
return one(Event{Kind: KindUnknown, SessionID: f.SessionID, Text: f.Type, Raw: raw}), nil
```

which populates **no `RequestID`** — and the frame carries no `session_id` either, so the resulting
`Event` has an empty `SessionID`, an empty `RequestID`, and `Text: "control_cancel_request"`. The
id that would retire the ask survives only inside `Raw`, and reading JSON out of `Raw` above the
airlock is exactly what the airlock rule forbids.

The consequence is **a lie on screen, not a stuck agent**: a permission prompt the operator is
looking at stays up after the request behind it is gone, and the answer they give it is written
into the void (§6). A decoder change closes it — one with its own review, not part of a recording
spike.

### The golden test **fails**, and that is the finding

`internal/core/fixtures_test.go`'s `TestDecodeRecordedFixtures` asserts zero `KindUnknown` across
every fixture in `testdata/stream/`. With these three committed, run verbatim:

```
--- FAIL: TestDecodeRecordedFixtures (0.01s)
    --- FAIL: TestDecodeRecordedFixtures/interrupt-pending-answer-after.jsonl (0.00s)
        fixtures_test.go:62: ../../testdata/stream/interrupt-pending-answer-after.jsonl:14 decoded as unknown (wire type "control_cancel_request") - the decoder needs a case for it
    --- FAIL: TestDecodeRecordedFixtures/interrupt-pending-basic.jsonl (0.00s)
        fixtures_test.go:62: ../../testdata/stream/interrupt-pending-basic.jsonl:14 decoded as unknown (wire type "control_cancel_request") - the decoder needs a case for it
FAIL
FAIL	github.com/DilanDoshi/wake/internal/core	0.271s
```

**One frame type, `control_cancel_request`. Two lines**, `:14` of each collision fixture. That is
the whole failure: `go test ./...` is green in `cmd/wake`, `internal/daemon`, `internal/notice`,
`internal/render`, `internal/rpc` and `internal/ui`, and inside `internal/core` every other test
passes too — including `TestInterruptFrameTypesMapOneToOneOntoTheirKinds`, whose `control_response`
count rises from 12 to 14 without complaint, and `TestFixturesDecodeToTheExpectedKinds`.
`interrupt-pending-no-interrupt.jsonl` scores zero unknowns on its own.

**Nothing under `internal/` was modified by this spike.** Deciding what the decoder does with
`control_cancel_request` — a new `EventKind`, or `KindPermissionRequest` with a withdrawal flag, or
something else — is a separate change with its own review, and §7's first bullet is the reason it
needs one: the frame's meaning outside this collision is unrecorded, so the decoder cannot be
allowed to name it "interrupted" on the strength of three recordings that all interrupted.

### The four things Wake should take from this

1. **`⎋` on a blocked agent is safe and complete.** The ask dies, the turn ends, the session lives,
   the next message works. **No respawn.**
2. **The state machine is already correct, by ordering** (§2a). Nothing in `agent.go` needs to
   change to avoid a wedge or a phantom-idle. Whoever implements `⎋` should not add a special case
   for this collision; they should add the *fixture* to whatever they test it with.
3. **Retiring the ask should key on `control_cancel_request`'s `request_id`, once the decoder can
   see it** — not on the interrupt receipt, which never names the ask (§3), and not only on the
   aborted `result`, which arrives four frames later and leaves the prompt on screen in between.
4. **A late answer costs nothing** (§6), so racing that prompt is a display problem, never a
   correctness one.

---

## 9. Observed vs testimony

**Observed on stdout, fixture-backed** — every wire shape in §2, §3, §4, §5, §6; the absence of
`control_cancel_request` from the control fixture and from the other 919 lines of the corpus; one
distinct `session_id` per fixture across all 85 lines; the ordering cancel → receipt → tool_result →
result; the populated `permission_denials` on an interrupted turn.

**Testimony, nothing committed** — the three exit codes and empty stderrs (§5); the scratch
directory listings that show `note.txt` absent after the late allow and present after the control's
in-time allow (§6); the driver-log timestamps that place the late `control_response` inside the
window `:17`–`:20` brackets (§6). The window itself, and its emptiness, are on the stream.

**Read from the binary** — nothing. This note makes no `[binary]` claim.

**Read off the committed code, not the stream** — §2a's right-hand column and §8's decoder branch:
`protocol.go`'s `result` → `KindTurnEnd` and default → `KindUnknown`, `agent.go`'s `note` setting
and clearing `a.pending`, and `a.pending != ""` → `rpc.StateBlocked`. Checkable, but not bytes off a
pipe.

**Inferred** — that the millisecond-scale gap between `:14` and `:18` generalises (§2a), and §8's
reading that the missing `RequestID` on the default branch is what leaves a stale prompt on
screen. Each follows from the committed decoder and the committed frame, and each is a claim about
Wake's code, not about Claude's.

---

## 10. Method

Recorded with a throwaway Go driver kept outside the repo (never committed), which spawns `claude`,
tees stdout to the fixture byte-for-byte through an `io.TeeReader`, and fires scripted stdin writes
on line triggers. It uses a fresh `uuidgen` for every session id and every `request_id`; scrubs the
inherited `CLAUDECODE` / `CLAUDE_CODE_*` / `CLAUDE_PID` / `CLAUDE_EFFORT` / `CLAUDE_PLUGIN_DATA`
env, matching `internal/core/process.go`'s `nestedSessionEnv` exactly; and invokes
`/Users/dev/.local/bin/claude` directly to bypass the cmux shim. Its outbound frames are
transcribed from `internal/core/encode.go` — `EncodeUserMessage`, `EncodeAllow` and
`EncodeInterrupt` — and the interrupt it writes is byte-shape-identical to `EncodeInterrupt`'s:

```json
{"request":{"subtype":"interrupt"},"request_id":"7c630a06-1598-4d11-8494-13216aa7697d","type":"control_request"}
```

*(key order differs from `EncodeInterrupt`'s output because the driver marshals a map; the JSON is
equivalent and the receipts prove the CLI read it.)*

`cwd` was a scratch directory outside the repo holding three trivial files (`a.txt`, `b.txt`,
`notes.md`) so a `Write` had somewhere real to land. The driver carries a hard watchdog — 150 s to
close stdin, 20 s more to kill — because a wedged process was the hypothesis; it never fired. Each
run took 13 s, 18 s and 51 s wall.

Fixture integrity was checked before commit over all 85 lines: every line parses as JSON; every
`uuid` is RFC 4122 **version 4**; exactly one distinct `session_id` per file; `init.apiKeySource` is
`"none"` on all six `init` frames and no credential-shaped strings appear. Two of the three files
carry a distinct pid in `init.messaging_socket_path` (`/tmp/cc-socks/17426.sock`,
`/tmp/cc-socks/17961.sock`); **`interrupt-pending-basic.jsonl` has no `messaging_socket_path` key on
either of its `init` frames**, which is a variation the earlier corpus does not show and which is
not explained here — the three distinct session ids and the sequential runs are what establish three
processes. The fixtures contain this machine's absolute scratch paths and the user's installed
skill/plugin/MCP inventory, which is expected for a recording.

| Fixture | Scenario |
|---|---|
| `interrupt-pending-basic` | ask outstanding 2.0 s → plain interrupt → alive probe 1.5 s after the aborted result → stdin closed |
| `interrupt-pending-answer-after` | same, plus a `behavior:"allow"` `control_response` for the withdrawn `request_id` 1.5 s after the aborted result, then the probe 5 s later |
| `interrupt-pending-no-interrupt` | control: no interrupt, ask left outstanding 30.1 s, then allowed, then the probe |
