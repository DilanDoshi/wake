# A clarifying question — how `AskUserQuestion` and `ExitPlanMode` reach Wake

Recorded 2026-08-09/10 against **Claude Code 2.1.226**, macOS 15 (arm64), model `claude-opus-5`.
Companion to `2026-08-08-interrupt-findings.md`, whose conventions this note follows.

Every **wire shape** here was observed on the real stdout of a real `claude` process. Six fixtures,
**462 lines**, six distinct processes. Nothing is hand-written and nothing comes from
`~/.claude/projects/*.jsonl`. Where a quoted line is shortened the elision is marked inline with
`...ELIDED...`; every other quoted line is byte-for-byte what came out of the pipe.

Three kinds of claim here are **not** stdout observations, and each is labelled where it appears:

- **Binary-read** — the tool's own schema and the code that consumes the answer, read out of the
  2.1.226 binary by string inspection. Marked `[binary]`.
- **Sent, not recorded** — the frames the driver wrote on *stdin*. A recording of stdout cannot
  contain them, exactly as `encode.go`'s header says. Marked `[sent]`.
- **Inference** — anything reasoned from the fixtures rather than read off them. Collected in §8.

Anything neither observed nor backed by a fixture is in §9.

**The headline, because it inverts the premise the spike was commissioned on.** The premise was
that `AskUserQuestion` had never appeared as a `tool_use` block in 919 recorded lines, so it must
arrive some *other* way. It arrives **both ways at once**: an ordinary `assistant` frame carrying a
`tool_use` block, immediately followed by a blocking `can_use_tool` `control_request` naming the
same `tool_use_id`. It had never appeared in the corpus because no recorded agent had ever called
it — not because it travels on a channel the corpus could not see.

So the mechanism question resolves to the **first** of the two candidate shapes, and Wake already
models the transport end to end. What Wake does **not** model is the payload: the answer is not the
`allow` — it rides *inside* the `allow`, and Wake's current allow drops it (§4, §7).

| File | Lines | What it proves |
|---|---:|---|
| `question-answer.jsonl` | 62 | the whole loop: `tool_use` → `can_use_tool` → **20.045 s of silence** → an `allow` carrying `updatedInput.answers` → the model told which options were chosen → the turn completes `success` |
| `question-bare-allow.jsonl` | 49 | the same ask answered **the way Wake answers today** (no `updated_input`): the tool runs and the model is told **"The user did not answer the questions."** The turn still ends `success` |
| `question-plan.jsonl` | 106 | `EnterPlanMode` → `system/status permissionMode:"plan"` → an `AskUserQuestion` *inside* plan mode, answered → `ExitPlanMode`, allowed with a deliberately wrong `updatedInput`, **approved anyway** → `system/status permissionMode:"auto"` |
| `question-plan-bare.jsonl` | 103 | the same plan-mode sequence with **both** asks answered the Wake way. `ExitPlanMode` → *"User has approved your plan."*; `AskUserQuestion` → *"The user did not answer the questions."* The contrast is the whole finding |
| `question-park-close.jsonl` | 55 | a question parked 8.462 s, then stdin closed: `AbortError`, `non_execution_kind: "permission-rule"`, and the tool **listed in `result.permission_denials`** on a `success` turn |
| `question-park-long.jsonl` | 87 | a question parked **342.618 s** with zero bytes on stdout in between. No timeout, no auto-resolve, no cancel frame. It blocks indefinitely |

Two of these are true A/Bs in the sense the stream-json note uses — same argv, same prompt, one
variable:

- `question-answer` / `question-bare-allow` — identical prompt, identical argv; the only difference
  is whether the `allow` carried `updatedInput`. This is the pair that decides what Wake must build.
- `question-plan` / `question-plan-bare` — same plan-mode scenario, differing in the same variable.
  The prompts differ in wording, so read it as two independent processes agreeing about
  `ExitPlanMode` rather than as a controlled experiment.

`question-park-close` / `question-park-long` differ only in how long the ask was parked (8 s vs
343 s) before stdin closed, and they end identically. That is the point of the pair.

---

## 1. The invocation

The argv for every recording here, and it is `session.go`'s `buildArgs` **exactly** — including
`--forward-subagent-text`, which the interrupt spike omitted — with `Model` and `Effort` unset
because `internal/daemon/spawn.go` sets neither, and `--permission-mode auto` because
`spawnPermissionMode` is `"auto"`:

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
       --permission-mode auto
```

**No flag enables any of this.** `init.capabilities` on all six fixtures still reads exactly:

```json
"capabilities":["interrupt_receipt_v1","interrupt_cancel_queued_v1","msg_lifecycle_v1"]
```

and `AskUserQuestion` and `ExitPlanMode` are both in `init.tools` on all six, as they were on all 26
earlier recordings. `init.permissionMode` reads `"auto"` on every one — note that this is the first
corpus in which the normalized value happens to match the flag Wake passed, which does **not**
weaken CLAUDE.md's rule that it is normalized rather than echoed.

`--permission-prompt-tool stdio` is load-bearing here for a second reason beyond permissions:
without it there is no channel on which a question could arrive at all. That was not tested (§9).

---

## 2. Which shape does a question arrive in? **Both — a `tool_use` block *and* a blocking `can_use_tool`.**

Two frames, consecutive, same `tool_use_id`.

First an ordinary `assistant` frame (`question-answer.jsonl:36`, abridged to its first option — the
full line is 2 335 bytes):

```json
{"type":"assistant","message":{"model":"claude-opus-5","id":"msg_011Cdt91bMXsWzoLFBspNbFa","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_011aRg7881b5CExFiPFVwamy","name":"AskUserQuestion","input":{"questions":[{"question":"What output format should the report use?","header":"Format","multiSelect":false,"options":[{"label":"Markdown (.md)","description":"reports/orders-summary.md — a table plus a totals line. ...ELIDED...","preview":"...ELIDED..."} ...ELIDED... ]}]}}]}, ...ELIDED (parent_tool_use_id, request_id, session_id, timestamp, type, uuid)... }
```

Its key set is `message, parent_tool_use_id, request_id, session_id, timestamp, type, uuid` — an
ordinary assistant frame in every respect. `parent_tool_use_id` is `null` on every frame in this
corpus; no subagent asked anything.

Then, ~2 ms later, the ask itself (`question-answer.jsonl:37`, `input` elided — full line 1 777
bytes):

```json
{"type":"control_request","request_id":"fc9fe513-1029-459d-ba84-6cede45604b7","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","display_name":"AskUserQuestion","input":"...ELIDED...","tool_use_id":"toolu_011aRg7881b5CExFiPFVwamy","requires_user_interaction":true}}
```

**The subtype is `can_use_tool`.** There is no new control subtype, no new frame type, and nothing
in these 462 lines that `DecodeLine` does not already have a case for. `controlRequestEvent` decodes
this to `KindPermissionRequest` with `Tool` populated and `RequestID` carried, which is exactly what
`agent.observe` stores and `FrameAllow`/`FrameDeny` answer.

### The one new field: `requires_user_interaction`

`requires_user_interaction: true` appears on **every** `can_use_tool` in this corpus and on **none**
of the 26 earlier recordings (`grep -c requires_user_interaction testdata/stream/*.jsonl` was 0
before these six). It is not in `wireFrame` and is discarded today.

This is the field that separates "may I run this tool" from "I am asking you a question". Both are
`can_use_tool`; only one of them is a question. Wake has no other discriminator that is not a tool
name, and a tool-name allowlist is the wrong shape — `[binary]` any tool whose `checkPermissions`
returns `behavior: "ask"` for interaction reasons can carry it.

### What the payload carries

`input.questions` is an array. Each question is `{question, header, multiSelect, options[]}`, each
option `{label, description, preview?}`. Both recorded questions in `question-answer.jsonl` carried
all three option keys. Verbatim, the first question of `question-answer.jsonl:37`'s `input`:

```json
{"question":"What output format should the report use?","header":"Format","options":[{"label":"Markdown (.md)","description":"reports/orders-summary.md — a table plus a totals line. Human-readable; a build expecting CSV would fail to parse it.","preview":"# Orders Summary\n\n| name | qty | unit_price | line_total |\n|---|---|---|---|\n| widget | 3 | 2.50 | 7.50 |\n...ELIDED..."},{"label":"CSV (.csv)","description":"reports/orders-summary.csv — machine-readable rows. A build expecting Markdown would render it as a wall of text.","preview":"name,qty,unit_price,line_total\n...ELIDED..."}],"multiSelect":false}
```

So the three things the task asked about are all present and all explicit:

| Asked | Where it lives | Recorded |
|---|---|---|
| the question | `input.questions[i].question` | yes, ×2 per ask in five of six fixtures |
| the options | `input.questions[i].options[j].label` + `.description` + optional `.preview` | yes |
| multi-select | `input.questions[i].multiSelect` | yes — **`false` on every recorded question.** A `true` was never provoked (§9) |

`header` is a short chip label (`"Format"`, `"Region join"`, `"Approach"`, `"Output"`). `[binary]`
the schema bounds it and the option count: *"The available choices for this question. Must have 2-4
options"*, *"Questions to ask the user (1-4 questions)"*, and question texts must be unique with
unique option labels within a question. Every recorded ask carried exactly 2 questions × 2–3
options, so the bounds themselves are binary-read, not observed.

`[binary]` The tool always asks, unconditionally — this is why it can never be auto-approved into
invisibility:

> `async checkPermissions(e){return{behavior:"ask",message:"Answer questions?",updatedInput:{questions:e.questions,...e.metadata&&{metadata:e.metadata}}}}`

---

## 3. Does the process block? **Yes, and the fixtures time it themselves.**

The gap is not testimony. `assistant` and `user` frames carry a `timestamp`; the `control_request`
between them does not. So the wall-clock hole is legible in the committed bytes:

| Fixture | `tool_use` frame | next frame after it | Gap | Held for |
|---|---|---|---:|---|
| `question-answer` | `:36` `00:42:10.261Z` | `:38` `00:42:30.306Z` | **20.045 s** | 20 s |
| `question-bare-allow` | `:35` `00:47:36.370Z` | `:37` `00:47:51.421Z` | **15.051 s** | 15 s |
| `question-plan-bare` | `:73` `00:48:31.345Z` | `:76` `00:48:46.375Z` | **15.030 s** | 15 s |
| `question-park-close` | `:34` `00:39:18.816Z` | `:36` `00:39:27.278Z` | **8.462 s** | until stdin closed |
| `question-park-long` | `:68` `00:44:19.778Z` | `:70` `00:50:02.396Z` | **342.618 s** | until stdin closed |

Each gap matches its scripted hold to within ~50 ms, and in every case the driver counted **zero**
stdout lines arriving during the hold. Nothing leaks out while a question is parked: no heartbeat,
no partial assistant text, no `system` chatter. From Wake's side a questioning agent is
byte-identical to a hung one — the same problem a `can_use_tool` already has, which is what
`rpc.StateBlocked` exists for.

The blocking is per-turn, not per-process: the `result` frame does not arrive either, so the turn
stays open the whole time.

---

## 4. How is it answered? **A `control_response` allow whose `updatedInput` carries the answer.**

The answer is not a `tool_result`. Wake never writes one; the CLI synthesises it. The answer is an
ordinary permission `allow` — and the *content* of the answer rides in `updatedInput`.

`[sent]` The line that produced `question-answer.jsonl:38`, with the echoed `questions` array
elided (`encodeControlResponse`'s exact envelope, from `EncodeAllow`):

```json
{"type":"control_response","response":{"subtype":"success","request_id":"fc9fe513-1029-459d-ba84-6cede45604b7","response":{"behavior":"allow","updatedInput":{"questions":"...ELIDED (echoed back unchanged)...","answers":{"What output format should the report use?":"Markdown (.md)","Should the report include catalog.csv's region column?":"Include region"}}}}}
```

`answers` is a flat map: **question text → chosen option label**. Not an index, not an object.
`[binary]` from the tool's output schema:

> `answers: Wn(N(),N()).describe("The answers provided by the user (question text -> answer string; multi-select answers are comma-separated)")`

What came back, `question-answer.jsonl:38` (abridged — `tool_use_result` repeats the whole
questions array):

```json
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"Your questions have been answered: \"What output format should the report use?\"=\"Markdown (.md)\", \"Should the report include catalog.csv's region column?\"=\"Include region\". You can now continue with these answers in mind.","tool_use_id":"toolu_011aRg7881b5CExFiPFVwamy"}]},"parent_tool_use_id":null,"session_id":"b45cc016-6428-4d9b-b805-e8fef5f87a08","uuid":"fefe1f42-b9e9-4db5-95ed-882a64b516d0","timestamp":"2026-08-10T00:42:30.306Z","tool_use_result":{"questions":[...ELIDED...],"answers":{...ELIDED...}},...}
```

The agent then wrote the file the operator asked for and the turn ended `subtype: "success"`,
`terminal_reason: "completed"`, `is_error: false`, `permission_denials: []`
(`question-answer.jsonl:62`). A question answered is not a turn failure.

**There is no receipt.** No `control_response` and no `control_cancel_request` appears on stdout
anywhere in these 462 lines — `grep` finds zero in all six files. The `tool_result` is the only
acknowledgement that an answer landed, and it names the `tool_use_id`, not the `request_id`.

### The trap, and it is the finding this spike exists to deliver

**Wake's allow today carries no `updatedInput`.** `rpc.Frame.UpdatedInput` is `omitempty` and
`internal/ui` never sets it, so `EncodeAllow(requestID, nil)` omits the key. That is a deliberate,
well-argued decision for a `Write` — "run it exactly as it asked" is the only shape with a recording
behind it. For a question it silently discards the operator's answer.

`[sent]` `question-bare-allow.jsonl` answers the identical ask with exactly the bytes Wake writes:

```json
{"type":"control_response","response":{"subtype":"success","request_id":"130aeb75-db9b-4883-a37a-8c9fbc3cfb7b","response":{"behavior":"allow"}}}
```

and the model is told, verbatim (`question-bare-allow.jsonl:37`):

```json
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"The user did not answer the questions.","tool_use_id":"toolu_01WNC2uXjRkJ8c9dgXmr9rZF"}]},"parent_tool_use_id":null,"session_id":"f191374c-a8a2-4ce2-94bb-083b31a2d600","uuid":"41aa5b91-129a-4eac-8e28-6c625d0b80a6","timestamp":"2026-08-10T00:47:51.421Z","tool_use_result":{"questions":[...ELIDED...]},...}
```

Note `is_error` is **absent** and the turn ends `success` with `permission_denials: []`. Nothing
anywhere reports that an answer was lost. An operator who clicked "allow" on a question in Wake
would watch the agent proceed as if nobody had replied — and this is the *approve* path, so it
cannot be caught by treating approval as the risky direction.

`[binary]` the mechanism, from the tool's `call`:

> `async call(e,t){let{questions:r,answers:n={},annotations:o}=e,{response:i,afkTimeoutMs:s}=e;return{data:{questions:r,answers:n,...}}}`

`answers` defaults to `{}` when the input does not carry it, and the empty map renders as *"The user
did not answer the questions."* Two sibling input fields exist and neither was sent in any
recording (§9): `response` (`[binary]` *"Freeform text the user typed instead of selecting a
structured option"*) and `annotations`.

---

## 5. `ExitPlanMode` — same envelope, **opposite** answer semantics

`ExitPlanMode` is reached without changing Wake's argv: the agent calls `EnterPlanMode` itself, in
`--permission-mode auto`. Both plan fixtures do this, and the mode change is announced on a frame
type already in the corpus (`question-plan-bare.jsonl:11` and `:75`):

```json
{"type":"system","subtype":"status","status":null,"permissionMode":"plan","uuid":"691b6859-bc63-4459-b5e0-53be46e4aa7b","session_id":"0bc5bb98-9096-4fb2-8ed5-8697516a48ef"}
```

```json
{"type":"system","subtype":"status","status":null,"permissionMode":"auto","uuid":"b98150b0-e704-44b6-959b-2cce09790790","session_id":"0bc5bb98-9096-4fb2-8ed5-8697516a48ef"}
```

The second lands **between** the `ExitPlanMode` ask and its `tool_result`, so `permissionMode` on a
`system/status` is how a session's mode actually changes under Wake's feet. `systemEvent` decodes it
to `KindSystem` with `Text: "status"` and drops the mode.

The ask itself is the same envelope with a different `input`
(`question-plan-bare.jsonl:74`, plan elided — full line 3 500 bytes):

```json
{"type":"control_request","request_id":"6198816b-b5d2-4287-829f-97e39aa9ced3","request":{"subtype":"can_use_tool","tool_name":"ExitPlanMode","display_name":"ExitPlanMode","input":{"plan":"...ELIDED (markdown)..."},"tool_use_id":"toolu_01XcjToEcpyNsR567rFkBzxv","requires_user_interaction":true}}
```

Same five answers to §2's questions: `can_use_tool`, `requires_user_interaction: true`, preceded by
its own `tool_use` block, blocking (15.030 s of silence, §3). The payload is one key, `plan`, a
markdown string. There are no options and no multi-select — **the decision is binary and it is the
allow/deny itself.**

And that is the difference that matters. A **bare** allow — the exact bytes Wake writes today — is a
complete, correct approval (`question-plan-bare.jsonl:76`):

```json
"User has approved your plan. You can now start coding. Start with updating your todo list if applicable\n\nYour plan has been saved to: /Users/dev/.claude/plans/glittery-scribbling-heron.md\n...ELIDED..."
```

| | `AskUserQuestion` | `ExitPlanMode` |
|---|---|---|
| envelope | `can_use_tool`, `requires_user_interaction: true` | identical |
| blocks | yes | yes |
| payload | `questions[]` with options | `plan` (markdown string) |
| what a bare `allow` means | *"you may ask"* — the answer is lost | **approval** — complete and correct |
| where the answer lives | `updatedInput.answers` | the `behavior` field itself |

So Wake is **already correct for `ExitPlanMode` and already wrong for `AskUserQuestion`**, with one
code path serving both. That asymmetry is the reason this spike had to record both.

One caveat on `question-plan.jsonl`, kept deliberately as a dated record of a driver mistake worth
knowing: its `ExitPlanMode` was answered with `updatedInput: {"questions":[],"answers":{}}` — a
shape Wake would never send, produced by a generic answer-builder that assumed every parked ask was
a question. It was **approved anyway**, with the full plan text intact in the `tool_result`
(`question-plan.jsonl:62`). `[inference]` `ExitPlanMode` appears to ignore `updatedInput` entirely,
but one accidental recording of one wrong shape is not a licence to send it, and §9 keeps it open.

---

## 6. What if it is never answered? **It blocks indefinitely. No timeout.**

`question-park-long.jsonl` parked one `AskUserQuestion` and answered nothing.

```
:68  assistant  tool_use AskUserQuestion   timestamp 2026-08-10T00:44:19.778Z
:69  control_request can_use_tool          (no timestamp on a control frame)
     ...  342.618 s, zero bytes on stdout  ...
:70  user  tool_result  AbortError         timestamp 2026-08-10T00:50:02.396Z
```

`:70` arrived **because stdin closed**, not because anything expired. Nothing self-resolved in five
minutes forty-three seconds — no auto-answer, no `control_cancel_request`, no `system` frame, no
`result`. This is the same behaviour `--permission-prompt-tool stdio` already has for a permission
ask, and Wake must assume the same: an unanswered question holds a turn open forever.

`[binary]` There *is* an auto-continue mechanism and it is off by default. `getAskUserQuestionTimeout`
reads a settings key, `askUserQuestionTimeout`, documented as *"Idle time before Claude's questions
auto-continue with any answers … when explicitly set to 60s/5m/10m"* — an interactive
away-from-keyboard timer, not a protocol timeout. When it fires the tool result carries
`afkTimeoutMs`, described as *"Set when the dialog auto-resolved after this many milliseconds of
idle (user away from keyboard). **Absent on every human-resolved path.**"* It is absent from every
frame in this corpus. A sibling key, `dialogExpiry`, defaults to 5 minutes but `[binary]` governs
*"a permission/user dialog forwarded to a **remote client**"* — not this path, and the 342.618 s
recording is the evidence that it does not apply here.

### Closing stdin does end it — as a *denial*, indistinguishably

Both park fixtures end the same way when stdin closes (`question-park-long.jsonl:70`):

```json
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"Tool permission request failed: AbortError: Tool permission stream closed before response received","is_error":true,"tool_use_id":"toolu_019ffupv2FPf2Y7rNgDRCcPe"}]},"parent_tool_use_id":null,"session_id":"9e7114da-fb50-4b68-aae4-788a2ced0d64","uuid":"ce13819e-8561-48be-9b48-03a4d98276d0","timestamp":"2026-08-10T00:50:02.396Z","tool_use_result":"Error: Tool permission request failed: AbortError: Tool permission stream closed before response received","tool_result_meta":[{"id":"toolu_019ffupv2FPf2Y7rNgDRCcPe","non_execution_kind":"permission-rule"}]}
```

**`non_execution_kind` is `"permission-rule"`** — which CLAUDE.md currently reads as *"a human said
no"*. And the turn that follows ends `subtype: "success"`, `terminal_reason: "completed"`,
`is_error: false`, with the tool **listed in `permission_denials`** (`question-park-close.jsonl:55`):

```json
{... "permission_denials":[{"tool_name":"AskUserQuestion","tool_use_id":"toolu_017bxTMoXQ4EDzTkdPqNuvcA","tool_input":{"questions":[...ELIDED...]}}], "terminal_reason":"completed", "subtype":"success", "is_error":false, ...}
```

That is byte-for-byte what a deliberate operator deny produces. **A question that died because
Wake's stdin went away is not distinguishable, on any recorded field, from one the operator
refused.** The interrupt note already had to correct one claim in this area; this is a third
producer of `permission-rule` and it is the one Wake causes by accident.

Both agents recovered in-session and finished the turn on their own — `question-park-close.jsonl:43`
reasons *"The interactive question tool isn't available in this session…"* and proceeds without
writing anything. That is graceful, and it is not a substitute for an answer.

---

## 7. Consequences for Wake

Ordered by what breaks if it is skipped. **None of this was implemented — the airlock rule holds
and `internal/` is untouched by this spike.**

1. **`updatedInput` must reach the wire for a question, and nothing above the airlock can send it
   today.** The transport field exists (`rpc.Frame.UpdatedInput`) and `core.EncodeAllow` already
   takes the map; `internal/ui` simply never populates it. The smallest correct change is a UI that
   can render options and an `App` path that puts `{"questions": <echoed>, "answers": {…}}` into
   `FrameAllow`. Until then **the approve path silently loses the operator's answer** (§4) — and it
   is silent in a way no existing test or field can catch, because the turn ends `success`.
2. **`requires_user_interaction` needs to survive the airlock.** It is the only recorded field that
   separates "may I run this" from "I am asking you something", and the two need different UI: one
   is a yes/no on an action, the other is a choice among options. Adding it to `wireFrame` and
   `Event` is a `protocol.go`/`wire.go` change with its own review.
3. **`Tool.Input` must reach the renderer for `AskUserQuestion`.** `toolCall` already carries the
   input; nothing renders `questions[]`, and an operator cannot choose between options they cannot
   see.
4. **`ExitPlanMode` needs no new mechanism, only a renderer.** A bare allow is a correct approval
   (§5). A plan shown as a raw `can_use_tool` on a tool called `ExitPlanMode` is the current
   behaviour and it is merely bad, not wrong.
5. **A blocked-on-question agent must be distinguishable from a hung one on the roster.**
   `rpc.StateBlocked` already covers it, since the frame is a `can_use_tool` — but §3 shows zero
   bytes for as long as the operator takes, and at 15–30 agents "silent" is the failure mode the
   liveness watchdog exists to notice.
6. **`session.Stop` on an agent with a question outstanding produces a false denial record** (§6).
   Closing stdin is Wake's graceful ending, and it lands as `permission-rule` in
   `permission_denials`. If Wake ever renders "you denied this", it will say so about a question the
   operator never saw.
7. **`system/status` carries `permissionMode` and Wake drops it** (§5). An agent can enter and leave
   plan mode on its own, so the mode Wake tracked at spawn goes stale mid-session. This intersects
   deferred I7.

---

## 8. Observed vs inferred

**Observed on stdout, fixture-backed** — every wire shape in §2, §3, §5 and §6; the two `tool_result`
strings in §4; the absence of any `control_response` or `control_cancel_request` in 462 lines; one
session id per fixture; six distinct pids; `requires_user_interaction: true` on all six asks and on
none of the 26 earlier recordings.

**Sent, not recorded** — the three `control_response` lines quoted in §4 and §5. They are stdin, so
no recording of stdout can contain them; their *effects* are the committed bytes.

**Read from the 2.1.226 binary, not observed** — the 1–4 question / 2–4 option bounds; the
uniqueness constraints; `checkPermissions` always returning `ask`; the `answers` /`response` /
`annotations` / `afkTimeoutMs` output schema; `askUserQuestionTimeout` and `dialogExpiry` semantics;
the claim that `afkTimeoutMs` is absent on human-resolved paths (consistent with the corpus, which
cannot prove a negative about a mechanism it never exercised).

**Inferred from the fixtures** — each of these could be wrong without disturbing anything above it:

- that `ExitPlanMode` ignores `updatedInput` (§5) — one accidental recording of one wrong shape;
- that no tool other than these two sets `requires_user_interaction` — the corpus contains no
  counter-example, but it also contains only these two;
- that the blocking is unconditional rather than dependent on `--permission-mode auto` — every
  recording used `auto`, because Wake does.

---

## 9. What this did not establish

- **`multiSelect: true`.** Every recorded question carried `false`. The comma-separated answer
  encoding is `[binary]` only, and Wake must not build a multi-select UI against it without a
  recording.
- **`response` (freeform) and `annotations` on the answer.** Both are in the schema, neither was
  ever sent, so what the model receives for a freeform reply is unknown. This is the shape a Wake
  operator would most plausibly want ("neither — do X instead").
- **Denying a question.** No fixture denies an `AskUserQuestion` or an `ExitPlanMode`.
  `EncodeDeny`'s message reaches the model as `Error: …` for an ordinary tool; whether a denied
  question reads that way, and whether the agent re-asks, is unrecorded. A denied plan is the more
  urgent of the two — rejecting a plan with feedback is a normal operator action and its wire
  behaviour is unknown.
- **An `updatedInput` that echoes `questions` unchanged but omits `answers`**, versus omitting the
  key entirely. `question-bare-allow` tests only the latter. Both should reach *"did not answer"*,
  and only one is recorded.
- **Answers that do not match any option label**, or that name a question not in the ask. `[binary]`
  the result formatter tolerates a non-matching value and phrases the tool result differently
  (*"The user answered: …"* rather than *"Your questions have been answered: …"*); never sent.
- **Interrupting a session with a question outstanding.** The interrupt spike proved a pending
  `can_use_tool` is withdrawn by `control_cancel_request`; whether a question — same subtype, but
  `[binary]` a "parked" one with its own preservation logic (`tengu_auq_park_preserved_at_shutdown`,
  *"cancel + deny skipped; park stays answerable"*) — behaves the same way is **not** recorded. This
  is the likeliest real collision at fleet scale and it is the obvious next spike.
- **Answering a question after the turn was interrupted**, and whether a stale answer errors.
- **A question from a subagent.** `parent_tool_use_id` is `null` on all 462 lines despite
  `--forward-subagent-text` being passed, and the `can_use_tool` carries no `agent_id`.
- **`askUserQuestionTimeout` actually set.** The default (unset) was recorded for 342.618 s;
  the behaviour when the key is present, and whether the AFK timer runs at all with no TTY, is
  unknown.
- **Whether a question can arrive as `request_user_dialog`.** `[binary]` a sibling control subtype
  exists — *"Requests the SDK consumer to render a tool-driven blocking dialog"* — carrying
  `dialog_kind` and an opaque `data` payload, answered `completed`/`cancelled`. It is gated on the
  client declaring `supportedDialogKinds` in an `initialize` control request, which Wake never
  sends: *"The CLI treats ABSENCE as 'cannot display' and fails closed."* No such frame appeared in
  these recordings. Nothing here proves it can never appear, only that it did not.
- **Two questions outstanding at once**, from one agent or from a queued message.
- **A question during `/compact`, or across a `/clear` session-id change.**

---

## 10. Method

Recorded with a throwaway Python driver in the scratchpad (never in the repo, nothing to delete from
the commit): it spawns `claude` with §1's argv, tees stdout to the fixture byte-for-byte before
anything parses it, auto-allows any `can_use_tool` for an ordinary tool by echoing `request.input`
back, parks `AskUserQuestion` / `ExitPlanMode` for a scripted hold, and then answers per scenario.
Each recording used a fresh `uuidgen` session id; ran with `cwd` set to a scratch directory outside
the repo holding four trivial files (`orders.csv`, `catalog.csv`, `README.md`, an empty `reports/`),
so a tool call had something real to touch and the recordings carry no Wake source; scrubbed the
inherited `CLAUDECODE` / `CLAUDE_CODE_*` / `CLAUDE_PID` / `CLAUDE_EFFORT` / `CLAUDE_PLUGIN_DATA`
env, exactly `core.nestedSessionEnv`; and invoked `/Users/dev/.local/bin/claude` directly to
bypass the cmux shim.

**Provoking the tool took one attempt, and the prompt is the reason.** The task was a genuine,
material ambiguity with an expensive wrong answer — write a report, but the output format must match
an unnamed downstream build, and a join column may or may not belong — closing with *"Do not guess
on either."* Every one of the four non-plan recordings produced an `AskUserQuestion` unprompted by
any mention of the tool. The prompt never names it. The plan recordings asked for `EnterPlanMode` by
name, which is the only instruction anywhere that names a tool, and both of those *also* produced a
spontaneous `AskUserQuestion` inside plan mode.

`question-park-long.jsonl` ends mid-turn: after stdin closed the agent kept working, and the driver
SIGKILLed it 30 s later. The last line is complete JSON and the bytes are honest; there is simply no
`result` frame. Two fixtures modified `orders.csv` and wrote `reports/orders-summary.md` in the
scratch cwd; it was reset between runs, which is why the later prompts see a slightly different
directory. `question-plan.jsonl` and `question-park-long.jsonl` overlapped in wall time with other
recordings — separate processes, separate session ids, and no shared state but the cwd.

Fixture integrity was checked before commit, over all 462 lines: every line parses as JSON; every
`uuid` is RFC 4122 **version 4**; exactly one distinct `session_id` per file; six distinct pids in
`init.messaging_socket_path` (`/tmp/cc-socks/<pid>.sock`), confirming six separate processes;
`init.apiKeySource` is `"none"` on every one and no credential-shaped strings appear. The fixtures
do contain this machine's absolute paths and the user's installed skill/plugin/MCP inventory, which
is expected for a recording.

### The golden test

`internal/core/fixtures_test.go` asserts zero `KindUnknown` across every fixture in
`testdata/stream/`. **It passes with all six committed, unmodified.** So does the whole
`internal/core` package, and so does `go test ./...` across all seven packages.

This is the opposite of the interrupt spike's result, and the reason is structural rather than
lucky: every frame in these recordings is a type `DecodeLine` already switches on. The
`control_request` is `can_use_tool`, which `controlRequestEvent` resolves to `KindPermissionRequest`
— it degrades to `KindUnknown` only for a subtype it does not know, and there is no new subtype
here. `system/status` and `rate_limit_event` were both already in the corpus.

**Zero frame types need a decoder case.** What the airlock is missing is not a *case* but two
*fields* — `requires_user_interaction` inbound (§2) and a populated `updatedInput` outbound (§4) —
and both are `protocol.go` / `wire.go` / `encode.go` changes with their own review, not something
this spike may make.
