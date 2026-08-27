# What a subagent actually looks like on the wire

Recorded 2026-08-09 against **Claude Code 2.1.226**, macOS 15 (arm64), model `claude-sonnet-5`.
Companion to `2026-08-08-stream-json-findings.md` and `2026-08-08-interrupt-findings.md`.

Wake's whole design is a room of agents with each agent's **subagents** down a right-hand sidebar,
and every session it spawns already passes `--forward-subagent-text`. Until this spike nobody had
seen what it forwards: `parent_tool_use_id` appeared **67 times across the previous 19 fixtures and
was `null` on every one**, because not one recorded session had ever dispatched a subagent.

Seven fixtures, **383 lines**, seven distinct processes, **nine subagent dispatches**. Nothing is
hand-written and nothing comes from `~/.claude/projects/*.jsonl`. Where a quoted line is shortened
the elision is marked inline with `...ELIDED...`; every other quoted line is byte-for-byte what came
out of the pipe.

Two kinds of claim here are **not** stdout observations, and each is labelled where it appears:

- **Testimony** — process exit codes and wall-clock gaps, read from the driver at recording time.
  An exit code is not a byte on a stream, so there is nothing to commit. Marked `[testimony]`.
- **Inference** — anything reasoned from the fixtures rather than read off them. Collected in §10
  so it can be checked and, where wrong, thrown away without taking the observations with it.

Anything neither observed nor backed by a fixture is in §11. **§11 is the section to read before
adding a struct field.** A tracked plan to "just add the `parent_tool_use_id` field" was a guess
until this spike; §11 is the list of the next such guesses.

**All 383 lines decode with zero `KindUnknown`.** `internal/core/fixtures_test.go` is green with
these fixtures in place and `internal/` untouched — 919 lines in the corpus now. Details in §9.

| File | Lines | Flag | What it proves |
|---|---:|---|---|
| `subagent-task.jsonl` | 59 | yes | one foreground subagent: the whole shape, start to `tool_result` |
| `subagent-parallel.jsonl` | 50 | yes | **two** concurrent subagents, their frames interleaved line by line |
| `subagent-background.jsonl` | 87 | yes | an **async** subagent: the turn ends while it is still running, and a second turn opens on its own |
| `subagent-permission.jsonl` | 41 | yes | a subagent's `Write` raising its own `can_use_tool`, carrying **`agent_id`** |
| `subagent-manual-mode.jsonl` | 31 | yes | `--permission-mode manual`: forwarding is unchanged, a subagent's `Read` asks nothing |
| `subagent-no-forward.jsonl` | 50 | **no** | control for `subagent-task`: same prompt, one variable |
| `subagent-no-forward-async.jsonl` | 65 | **no** | control where the subagents went async; **complicates** the flag story (§8) |

The last two are the only recordings here whose argv is **not** Wake's. They exist to isolate
`--forward-subagent-text` and are labelled as controls everywhere they are used; nothing about
Wake's own behaviour is read off them.

---

## 1. The invocation

Exactly `session.go`'s `buildArgs`, `--forward-subagent-text` included, for the five non-control
fixtures:

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
       --permission-mode auto        (manual on subagent-permission / subagent-manual-mode)
```

**Permissions: `--permission-mode auto` together with `--permission-prompt-tool stdio` lets the
`Task` call through, and so does `manual`.** Neither mode blocked the dispatch, and in `auto` the
subagent's `Bash`, `Read` and `Write` all ran with **no `can_use_tool` at all** — zero
`control_request` frames in the three `auto` fixtures totalling 196 lines. Under `manual` a
subagent's `Read` still asked nothing (`subagent-manual-mode.jsonl`, 31 lines, zero
`control_request`) while its `Write` did (§7). The driver auto-allowed by echoing `request.input`
back, exactly as the interrupt spike's did.

`--permission-mode manual` is reported by `init` as `"default"`
(`subagent-manual-mode.jsonl:5`), which re-confirms the normalization trap already in CLAUDE.md.
`--permission-mode auto` is reported as `"auto"`.

---

## 2. Q1 — how forwarded subagent text arrives: **ordinary `assistant` and `user` frames**

No new frame type, no new subtype. A subagent's speech is an `assistant` frame that looks like any
other, distinguished only by three top-level keys. `subagent-task.jsonl:50`, the subagent's final
report, abridged in the message body only:

```json
{"type":"assistant","message":{...ELIDED (ordinary assistant message, one text block)...},"parent_tool_use_id":"toolu_016bEJvdzUw44eHsL8giKXCQ","session_id":"88ca1b79-1d9b-4464-b511-068fc740224e","uuid":"a1525c5b-6c50-4c83-8acd-bd88ac00f7e6","timestamp":"2026-08-09T10:39:09.286Z","request_id":"req_011Cds2kKjorDhk3gFGWsWL9","subagent_type":"general-purpose","task_description":"Count lines in all files in directory"}
```

The key sets are the whole story. Measured over all seven fixtures, an `assistant`/`user` frame
carrying a non-null `parent_tool_use_id` has one of exactly three key sets, and every one of them
contains **`subagent_type` and `task_description`**:

```
48x  message parent_tool_use_id request_id session_id subagent_type task_description timestamp type uuid
31x  message parent_tool_use_id           session_id subagent_type task_description timestamp type uuid
 1x  message parent_tool_use_id           session_id subagent_type task_description timestamp tool_use_result type uuid
```

while a parent frame (40 of them) has neither:

```
31x  message parent_tool_use_id request_id session_id                                timestamp type uuid
 9x  message parent_tool_use_id           session_id                                timestamp tool_use_result type uuid
```

**80 of the 383 recorded lines carry a non-null `parent_tool_use_id`.** All 80 also carry
`subagent_type`; all 80 also carry `task_description`. Zero exceptions.

The subagent's *prompt* is forwarded too, as a `user` frame under the same parent id
(`subagent-task.jsonl:20`) — so the sidebar can show what an agent was asked without reading the
parent's `tool_use` input.

Five `system` subtypes carry the subagent's lifecycle: `task_started` (9), `task_progress` (26),
`task_updated` (9), `task_notification` (9) and `background_tasks_changed` (6). Only `task_progress`
is **new to the corpus** — the other four appear once or twice in
`interrupt-cancel-queued-empty.jsonl`, where the interrupt spike met them as *background-task*
frames and recorded them as harmless. They are the same frames a subagent uses. `task_started` is
the richest and precedes the first forwarded frame in **all nine dispatches** —
`subagent-permission.jsonl:12` verbatim:

```json
{"type":"system","subtype":"task_started","task_id":"ab1b72d53680ae187","tool_use_id":"toolu_01WywnvpZQ88sJZTfHNVNJEc","description":"Write tally.txt with hello","subagent_type":"general-purpose","task_type":"local_agent","prompt":"...ELIDED (the subagent's full prompt)...","uuid":"83d60a91-4e21-4053-b8c5-a4728e0f8d87","session_id":"f4970eaf-d26a-4ddc-900c-9af7b39bcbc2"}
```

and `task_progress` is a live status line, `subagent-permission.jsonl:16`:

```json
{"type":"system","subtype":"task_progress","task_id":"ab1b72d53680ae187","tool_use_id":"toolu_01WywnvpZQ88sJZTfHNVNJEc","description":"Writing tally.txt","subagent_type":"general-purpose","usage":{"total_tokens":26984,"tool_uses":1,"duration_ms":4075},"last_tool_name":"Write","uuid":"a7522c09-1860-487b-b454-8c40dac3b912","session_id":"f4970eaf-d26a-4ddc-900c-9af7b39bcbc2"}
```

`task_started`, `task_progress` and `task_notification` all carry `tool_use_id`, so they join the
same correlation as §3. **`task_updated` does not** — its keys are exactly
`patch, session_id, subtype, task_id, type, uuid` on all nine, so it is reachable only via
`task_id`.

### The tool is called `Agent`, not `Task`

Worth stating on its own, because every prompt in this spike said "use the Task tool" and every
recorded `tool_use` block says otherwise. `subagent-task.jsonl:17`:

```json
"content":[{"type":"tool_use","id":"toolu_016bEJvdzUw44eHsL8giKXCQ","name":"Agent","input":{"description":"Count lines in all files in directory","prompt":"...ELIDED...","subagent_type":"general-purpose","run_in_background":false},"caller":{"type":"direct"}}]
```

`Task` is what `init.tools` advertises and what the model is prompted with; `Agent` is what lands on
the wire. All nine dispatches in this corpus are `"name":"Agent"`. **Anything in Wake that matches
on the tool name to find a subagent must match `Agent`.**

---

## 3. Q2 — `parent_tool_use_id` is the attribution, and it equals the parent's `Agent` `tool_use` id

Yes, and exactly. Checked programmatically per fixture: the set of distinct non-null
`parent_tool_use_id` values equals the set of `id`s on the parent's `Agent` `tool_use` blocks, in
all seven files, with no leftovers on either side.

`subagent-task.jsonl:17` issues `"id":"toolu_016bEJvdzUw44eHsL8giKXCQ"` (quoted above) and every one
of the 17 forwarded frames in that file carries
`"parent_tool_use_id":"toolu_016bEJvdzUw44eHsL8giKXCQ"`. The two-subagent case is the one that
proves it discriminates rather than merely being present — `subagent-parallel.jsonl:14` and `:15`
are two `Agent` blocks in a single assistant frame, `toolu_012Ft9BXrwnJVK3VrusxLKcJ` and
`toolu_01SrsyL4cWGpk9TPFcHJmFWg`, and the two subagents' closing reports separate cleanly:

```json
{"type":"assistant","message":{...ELIDED...,"content":[{"type":"text","text":"alpha.txt: 3 lines, beta.txt: 2 lines"}],...},"parent_tool_use_id":"toolu_012Ft9BXrwnJVK3VrusxLKcJ","session_id":"c2e0aefe-3911-4527-9165-bf5e7ee5edd9",...,"subagent_type":"general-purpose","task_description":"Count lines in alpha.txt and beta.txt"}
```

```json
{"type":"assistant","message":{...ELIDED...,"content":[{"type":"text","text":"gamma.txt: 4 lines, delta.txt: 1 line"}],...},"parent_tool_use_id":"toolu_01SrsyL4cWGpk9TPFcHJmFWg","session_id":"c2e0aefe-3911-4527-9165-bf5e7ee5edd9",...,"subagent_type":"general-purpose","task_description":"Count lines in gamma.txt and delta.txt"}
```

(`subagent-parallel.jsonl:37` and `:40`.)

**So the correlation holds and it is the attribution mechanism.** Two riders, both load-bearing:

1. `task_description` is a **human-readable name for the pane** that arrives on every forwarded
   frame, and `subagent_type` is the agent kind. Neither needs a lookup table; the sidebar can be
   built from a single frame without waiting for `task_started`.
2. `parent_tool_use_id` is **absent on every `system` frame**, including the four `task_*` subtypes
   that describe the subagent. Those correlate on `tool_use_id`/`task_id` instead. A renderer that
   routes on `parent_tool_use_id` alone will route the subagent's own progress line to the parent.

---

## 4. Q3 — the frames **interleave**, live. A renderer can stream them

Not batched. `subagent-parallel.jsonl:17`–`:44` is two subagents alternating on consecutive lines,
with the parent's `tool_result` frames arriving only at the very end:

```
:17  system/task_started    (agent A)
:18  user        parent=…usxLKcJ   A's prompt
:19  system/task_started    (agent B)
:20  user        parent=…HJmFWg    B's prompt
:21  assistant   parent=…usxLKcJ   A thinking
:25  assistant   parent=…usxLKcJ   A tool_use Bash
:28  assistant   parent=…HJmFWg    B thinking
:29  user        parent=…usxLKcJ   A tool_result
:33  assistant   parent=…HJmFWg    B tool_use Bash
:36  user        parent=…HJmFWg    B tool_result
:37  assistant   parent=…usxLKcJ   A final text
:40  assistant   parent=…HJmFWg    B final text
:43  user        parent=null       Agent tool_result for A
:44  user        parent=null       Agent tool_result for B
:45  assistant   parent=null       the parent's summary
```

`[testimony]` The driver's timestamps put `:18` at t+15.1 s and the turn's `result` at t+24.4 s, so
the forwarded frames arrived nine seconds before the turn ended rather than in a burst at the close.
The line order in the fixture is the committed half of that claim.

### The parent can be doing other work — and its turn can end first

`subagent-background.jsonl` is the strong case. The `Agent` call went **async**: its `tool_result`
came back immediately (`:16`) saying `"Async agent launched successfully."` with
`tool_use_result.isAsync: true` and `status: "async_launched"`, the parent spoke at `:18`, and the
turn **ended at `:30`** with `subtype:"success"`, `terminal_reason:"completed"` — while the subagent
was still on its first tool call. Lines `:31`–`:77` carry 17 more forwarded frames arriving **after
their turn was over**, and stdin had already been closed at `:30`.

Then, with no message from Wake, a whole new turn opens on its own: `:81` is a second `system/init`,
`:82` the parent reporting the result, `:87` a second `result` — and that result names its cause:

```json
{"origin":{"kind":"task-notification"},"subtype":"success","num_turns":1,"terminal_reason":"completed","type":"result"}
```

(`subagent-background.jsonl:87`, five keys of a long frame.) `subagent-no-forward-async.jsonl` does
it twice in one process — three `result` frames, `:28`, `:58`, `:65`, the last two originated by
notifications.

Three consequences, all for the daemon rather than the renderer:

- **A subagent outlives the turn that spawned it.** `KindTurnEnd` is not "this agent is idle", and an
  attention model that clears a session on `result` will clear one that is about to speak again.
- **`Stop()` does not stop it.** `session.go`'s contract is that closing stdin lets the agent finish
  the turn it is in and exit on EOF. Here stdin was closed while an async subagent ran, and the
  process stayed alive another **40 seconds** `[testimony]`, ran a whole further turn and only then
  exited 0. "Finishes the turn it is in" is not the whole of what it finishes.
- **An unprompted turn exists.** `origin: {"kind":"task-notification"}` is the first recorded `result`
  in this project that was not caused by something Wake wrote on stdin. Three of the ten `result`
  frames here carry it; the other seven have no `origin` key at all, so presence is the signal.

`run_in_background` is a real key on the `Agent` input and it correlates, with one hole. Across the
nine dispatches:

| `run_in_background` | dispatches | outcome |
|---|---:|---|
| `true` | 2 (`subagent-no-forward-async.jsonl:13`, `:14`) | both async |
| `false` | 6 | all six foreground |
| **key absent** | 1 (`subagent-background.jsonl:12`) | **async** |

So asking for background gets it, and asking for foreground gets that — but a dispatch that asked
for neither went async anyway, which is the one Wake most needs to predict. `[inference]` What
decides the absent case is not established. See §11.

---

## 5. Q4 — a subagent's own `tool_use` reaches the stream, on the parent's `session_id`

Both halves are yes-and-no in the useful direction: the tool calls **do** arrive, and the
`session_id` **does not** differ.

`subagent-task.jsonl:25`, the subagent's `Bash`, abridged in the message body only:

```json
{"type":"assistant","message":{...ELIDED...,"content":[{"type":"tool_use","id":"toolu_01T3GKNGYoAKykbVFvwipzsg","name":"Bash","input":{"command":"ls -la \"...ELIDED (the scratch cwd)...\"","description":"List directory contents"},"caller":{"type":"direct"}}],...},"parent_tool_use_id":"toolu_016bEJvdzUw44eHsL8giKXCQ","session_id":"88ca1b79-1d9b-4464-b511-068fc740224e","uuid":"7d1079fb-243c-496d-a6d9-aed5dd46641c",...,"subagent_type":"general-purpose","task_description":"Count lines in all files in directory"}
```

Across the seven fixtures the forwarded frames decode to 26 `tool_use` and 26 `tool_result` events —
`Bash` ×12, `Read` ×11, `Write` ×3 — against 9 `assistant` text events. **A subagent is mostly tool
traffic on this wire, not prose.** Its `tool_result` blocks arrive as `user` frames under the same
parent id, so call and result pair up inside the subagent without ambiguity.

**`session_id` is identical to the parent's on every forwarded frame**, checked per file: each of the
seven fixtures has exactly one distinct `session_id` across all its lines, subagent frames included.
There is no second session id anywhere in this corpus. A subagent is therefore **not** a second
session Wake could `--resume`; the only identifiers it has are `parent_tool_use_id`, the `task_id`
on the `task_*` frames, and `agent_id` on a permission ask (§7) — and the last two are the same
value.

The one thing that does **not** arrive attributed is hooks. A subagent's tool fires `PreToolUse`
hooks like any other, and those frames carry no `parent_tool_use_id`, no `subagent_type` and no
`tool_use_id` — `subagent-task.jsonl:23`:

```json
{"type":"system","subtype":"hook_started","hook_id":"5f96330e-6ee1-442d-8e80-9b7ecfad7862","hook_name":"PreToolUse:Bash","hook_event":"PreToolUse","uuid":"08c69646-13c8-46ff-be77-bcc024f8438a","session_id":"88ca1b79-1d9b-4464-b511-068fc740224e"}
```

70 `hook_started` and 70 `hook_response` in this corpus, none of them attributable to the subagent
that caused them.

---

## 6. Q5 — the parent's `Agent` `tool_result` **repeats the last forwarded message verbatim**

For a foreground subagent the `tool_result` content is an **array of two text blocks**, and the first
is byte-identical to the subagent's final forwarded `assistant` text. Verified by comparison, not by
eye: `subagent-task.jsonl:50`'s text and `:53`'s `content[0].text` are the same string.

`subagent-task.jsonl:53`, elided in the long tail only:

```json
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_016bEJvdzUw44eHsL8giKXCQ","type":"tool_result","content":[{"type":"text","text":"All four files end with a trailing newline...ELIDED...\n\n- alpha.txt: 3\n- beta.txt: 2\n- delta.txt: 1\n- gamma.txt: 4\n\nTotal file count: 4\nTotal line count: 10"},{"type":"text","text":"agentId: a73fc145a7d730b54 (use SendMessage with to: 'a73fc145a7d730b54', summary: '<5-10 word recap>' to continue this agent)\n<usage>subagent_tokens: 31516\ntool_uses: 6\nduration_ms: 18016</usage>"}]}]},"parent_tool_use_id":null,"session_id":"88ca1b79-1d9b-4464-b511-068fc740224e","uuid":"16387d21-9956-4fff-9f9f-64a938202a69","timestamp":"2026-08-09T10:39:11.421Z","tool_use_result":{"status":"completed","prompt":"...ELIDED...","agentId":"a73fc145a7d730b54","agentType":"general-purpose","content":[...],"resolvedModel":"claude-sonnet-5","totalDurationMs":18016,"totalTokens":31516,"totalToolUseCount":6,"usage":{...ELIDED...}}}
```

So the relation to what was forwarded is **duplication, not summary**. A renderer that shows both the
forwarded stream and the parent's tool result will print the subagent's report twice. The second
block is machine addressing, not content — an `agentId` and a `SendMessage` continuation hint —
and `tool_use_result` carries the structured version: `agentId`, `agentType`, `status`, `prompt`,
`resolvedModel`, `totalDurationMs`, `totalTokens`, `totalToolUseCount`, `toolStats`, `usage`.

`task_notification` (`subagent-task.jsonl:52`) carries the same text a **third** time, as `summary` —
also checked by comparison, `:50` == `:52.summary` == `:53.content[0].text`, all three byte-identical
— plus the on-disk transcript at `output_file: …/tasks/a73fc145a7d730b54.output`. One report, three
frames, and only the last of them has a shape a renderer would naturally suppress.

**Async is the exception and it matters.** When the subagent runs in the background the `tool_result`
is not a report at all, it is a launch receipt that arrives before the subagent has done anything —
`subagent-background.jsonl:16` is `"Async agent launched successfully."` with
`tool_use_result.isAsync: true`, `status: "async_launched"`, and it explicitly instructs the model
not to read the output file. Wake cannot treat "the `Agent` `tool_result` arrived" as "the subagent
finished": on the async path the finish is `task_notification` (`:80`), and `task_updated:79` dates
it `end_time: 1786272109004` — **39.0 s** after the launch receipt's
`timestamp: "2026-08-09T10:41:09.972Z"`.

---

## 7. A subagent's permission ask is its own `can_use_tool`, and it carries **`agent_id`**

This is the answer to the question the brief flagged as itself a finding, and it is a good outcome:
the ask is separate, and it is attributable.

`subagent-permission.jsonl:22`, verbatim and complete:

```json
{"type":"control_request","request_id":"84ac5988-9d95-4633-ab99-c19897801a2d","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"/private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/sub/cwd5/tally.txt","content":"hello\n"},"description":"tally.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_018qK9ZQkeAwaX9mXQCgZcmj","agent_id":"ab1b72d53680ae187"}}
```

Three facts, each checkable:

1. **`agent_id` is new.** The two `can_use_tool` frames already in the corpus —
   `permission.jsonl:20` and `permission-deny-response.jsonl:18`, both raised by the parent's own
   tools — have `request` keys exactly
   `description, display_name, input, permission_suggestions, subtype, tool_name, tool_use_id`.
   **No `agent_id`.** Presence of the key is what says "a subagent is asking".
2. **`agent_id` equals the subagent's `task_id`.** `ab1b72d53680ae187` is the `task_id` on
   `subagent-permission.jsonl:12`'s `task_started` and the `tool_use_id` there is the parent's
   `Agent` id, so the ask joins the sidebar through `task_started`.
3. **The envelope is otherwise unchanged.** Top-level keys are still exactly
   `request, request_id, type` — no `session_id`, no `parent_tool_use_id`. `request_id` remains the
   only correlator for the answer, exactly as for a parent's ask.

`agent_id` is a **second identifier space** for the same subagent: `parent_tool_use_id` on forwarded
frames, `task_id`/`agent_id` on the control and `task_*` frames. Nothing on the permission ask names
`parent_tool_use_id`, so joining them needs `task_started`, which arrives first in all nine
dispatches.

---

## 8. `--forward-subagent-text` — the A/B, and what it does not settle

Two controls, argv identical to Wake's minus the flag.

**The clean pair.** `subagent-task.jsonl` and `subagent-no-forward.jsonl` share prompt, permission
mode, directory shape and foreground dispatch, and differ in the flag alone. Forwarded block types,
counted over frames carrying `parent_tool_use_id`:

| | `assistant`/text | `assistant`/thinking | `assistant`/tool_use | `user`/tool_result | `user`/text |
|---|---:|---:|---:|---:|---:|
| with the flag | 1 | 3 | 6 | 6 | 1 |
| **without** | **0** | **0** | 6 | 6 | 1 |

The subagent in the control demonstrably produced a final report — it is in the parent's
`tool_result` at `subagent-no-forward.jsonl:44`, whose `content[0].text` begins

```
## Directory listing\n\n`…/scratchpad/sub/cwd6` contains 4 files, no subdirectories:
```

— and it **never appeared as a forwarded frame**. So on this pair the flag is what makes a
foreground subagent's prose visible, and its **tool calls and results arrive with
`parent_tool_use_id` whether or not the flag is passed**.

**And the pair that complicates it.** `subagent-no-forward-async.jsonl` has no flag either, and its
two subagents' prose *does* arrive — `:44` and `:48` are forwarded `assistant` text frames with
`parent_tool_use_id`. The difference from the first control is that both of its `Agent` calls set
`run_in_background: true` and went async (`isAsync: true` on `:18` and `:21`), where the first
control's set `false` and ran foreground. So the flag is not a global gate, and the two controls
differ in dispatch mode as well as in nothing else.

Stated as narrowly as the bytes allow:

- **Observed:** with the flag, every one of the five recordings carries forwarded `assistant` text.
  Without it, a foreground subagent's prose did not arrive (n=1) and an async one's did (n=1, two
  subagents). Subagent `tool_use`/`tool_result` arrived in all seven.
- **Not observed:** the flag isolated for an async subagent, or either arm repeated. One recording
  per cell. §11.

The immediate consequence for Wake is small and safe: **keep passing the flag.** Every fixture that
Wake's own argv produced has the prose in it. What is *not* safe is the reading that the flag is what
makes subagents visible at all — it is not. Dropping it would not quiet the subagent dimension; it
would leave the tool traffic (26 `tool_use` and 26 `tool_result` events across this corpus) still
arriving with a `parent_tool_use_id` nothing decodes.

---

## 9. What the decoder does with all this today

Run against the committed `DecodeLine`, all seven fixtures (383 lines) decode with **zero errors and
zero `KindUnknown`**. `make test` is green with them in `testdata/stream/`, both with and without
`-race`, and `internal/` was not touched. The corpus is now **919 lines**.

That is the good news and it is also the problem: nothing failed, so nothing announced what was lost.

| What arrives | Today's `Event` | What is dropped |
|---|---|---|
| 80 forwarded frames | `KindAssistantText` / `KindThinking` / `KindToolUse` / `KindToolResult` / `KindUserText` — the same kinds a parent frame produces | `parent_tool_use_id`, `subagent_type`, `task_description`. **`Event` has no field for any of them**, so above the airlock a subagent's speech is the agent's speech |
| `system/task_started` ×9, `task_progress` ×26, `task_updated` ×9, `task_notification` ×9 | `KindSystem`, `Text` = the subtype | `task_id`, `tool_use_id`, `subagent_type`, `description`, `last_tool_name`, `usage`, `summary`, `output_file` — every field there is |
| `can_use_tool` with `agent_id` ×1 | `KindPermissionRequest` | `agent_id`. A subagent's ask is indistinguishable from the parent's, so it routes to the parent |
| the `Agent` `tool_result` ×9 | `KindToolResult`, `Text` = the raw JSON array | `content` is an **array of blocks** here, not a string, so `jsonString` falls back to raw bytes and the renderer gets JSON rather than prose |
| `result` with `origin.kind = "task-notification"` | `KindTurnEnd` | `origin`. An unprompted turn looks like any other |

Concretely, and staying inside the airlock rule — **none of this was done here, and each is its own
task with its own review**:

1. **`Event` needs the subagent dimension.** `parent_tool_use_id` is the one field the tracked plan
   named, and it is genuinely the right one — but on its own it gives the sidebar an opaque
   `toolu_…`. `task_description` and `subagent_type` ride the same 80 frames and are what a human
   reads.
2. **`task_*` needs decoding, not defaulting.** `task_progress.description` + `last_tool_name` is
   exactly the one-line status a sidebar row wants, and `task_notification` is the only completion
   signal on the async path.
3. **`KindPermissionRequest` must carry `agent_id`**, or a subagent's blocked tool call is reported
   as its parent's.
4. **`KindTurnEnd` is not "idle".** §4: a turn can end with a subagent still running, and a later
   turn can begin with no input from Wake.
5. **`Stop()`'s doc comment is now incomplete.** §4: an async subagent kept the process alive well
   past the close of stdin.

---

## 10. Observed vs inferred

**Observed on stdout, fixture-backed** — every wire shape in §2, §3, §5, §6, §7; the key-set counts;
`parent_tool_use_id` equalling the `Agent` `tool_use` id in all seven files; one `session_id` per
fixture across all 383 lines; the interleaving order in §4; the block-type counts in §8; the absence
of `agent_id` from the two pre-existing `can_use_tool` frames.

**Testimony, nothing committed** — the driver's elapsed-time readings (§4), the seven exit codes (all
0), and the 40 s the background process outlived the close of stdin.

**Inferred from the fixtures** — each of these could be wrong without disturbing anything above:

- that `--forward-subagent-text` gates a *foreground* subagent's prose specifically (§8). What is
  observed is one A/B pair plus one control that behaves differently and differs in a second
  variable.
- that async dispatch is what makes the flag redundant (§8). Equally consistent with the two
  controls: something about parallel dispatch, or run-to-run variation in whether the subagent's
  closing message is emitted as a separate assistant turn.
- that `task_notification` is *the* completion signal on the async path (§6). It is the frame that
  arrived at completion in all nine dispatches; nothing establishes it cannot be missed or repeated.
- that the duplication in §6 is unconditional. Nine dispatches, and the byte-identity was checked on
  one.

---

## 11. Unverified — do not design around these

- **Whether `parent_tool_use_id` ever nests.** Every subagent here was one level deep. A subagent
  that itself calls `Agent` was never recorded, so whether the grandchild's frames carry the
  grandchild's parent id, the top-level one, or something else is unknown. Wake's sidebar is a flat
  list; if nesting is possible, that is a design question this note cannot answer.
- **What decides async when nothing asks for it.** `run_in_background: true` produced async twice and
  `false` produced foreground six times, but the dispatch that **omitted the key** went async
  (§4). Whether the CLI defaults it, the model's `--brief` context nudges it, or something in the
  prompt did, is not established. This is the highest-value follow-up: the async path is the one
  that breaks `Stop()` and `KindTurnEnd`, and Wake cannot opt out of it.
- **`--forward-subagent-text` isolated for an async subagent** (§8), and either control repeated.
- **Interrupting a subagent.** The interrupt corpus lists this as open and it still is. Whether
  `interrupt` aborts the subagent, the parent, or both; whether the forwarded frames get a
  `user-rejected` `tool_result`; what `terminal_reason` an aborted `Agent` turn reports — none of it
  was tried. §9 of the interrupt note also shows a background task surviving an interrupt, which is
  now a much more common shape than it was.
- **`SendMessage` to a running subagent.** Two fixtures hand the model an `agentId` and tell it to
  use `SendMessage` to continue that agent. Never sent. Whether it produces more forwarded frames
  under the same `parent_tool_use_id`, a new one, or nothing is unknown.
- **A denied subagent tool call.** The one recorded subagent ask was allowed. Whether a deny reaches
  the subagent as a `tool_result` under its `parent_tool_use_id`, and whether it lands in the
  parent's `result.permission_denials`, was not recorded.
- **Subagent types other than `general-purpose`.** `init.agents` lists 16 on this machine; one was
  exercised. Whether a plugin or project agent forwards the same keys is unobserved.
- **Whether `subagent_type` and `task_description` are always present.** 80 of 80 here, but all nine
  dispatches were `local_agent` in one `task_type`. `task_started.task_type` having a value at all
  implies others exist.
- **`task_updated.patch` beyond `{"status":"completed","end_time":…}`.** Nine frames, one shape. A
  failing or cancelled subagent was never recorded, so no failure `status` value is known.
- **Whether a subagent can outlive the *process*.** The async one died with it; nothing tested a
  subagent still running at teardown.
- **`output_file`.** Every `task_notification` names an on-disk JSONL transcript of the subagent, and
  the async `tool_result` explicitly warns the model off reading it. Wake never opened one. Whether
  it is the fidelity source for a subagent pane is untested — and reading it would be Wake owning
  state it currently does not.
- **Anything about a subagent under `--resume`.** Not tried.

---

## 12. Method

Recorded with a throwaway Go driver in the scratchpad (never in the repo, and deleted with the
scratch directory): it spawns `claude` with the argv in §1, tees stdout to the fixture
byte-for-byte through an `io.TeeReader`, auto-allows any `can_use_tool` by echoing `request.input`
back, sends the next scripted prompt on each `result`, and closes stdin after the last. Each
recording used a fresh `uuidgen` for the session id; ran with `cwd` a directory under the scratchpad
holding four trivial text files (3, 2, 4 and 1 lines), so the recordings carry no Wake source;
scrubbed the same `CLAUDECODE` / `CLAUDE_CODE_*` / `CLAUDE_PID` / `CLAUDE_EFFORT` /
`CLAUDE_PLUGIN_DATA` env `session.go` scrubs; and invoked `/Users/dev/.local/bin/claude` directly
to bypass the cmux shim. A second throwaway, `scripts/spike-subagent-decode`, ran the committed
`DecodeLine` over the finished fixtures to produce §9's table; it read `internal/core`, changed
nothing, and was deleted before the commit.

The prompt that reliably provokes a dispatch is the brief's:
*"Use the Task tool to launch a general-purpose agent that reads every file in this directory and
reports how many lines each one has. Do not read the files yourself."* It dispatched every time it
was used, and so did each variant below — **nine dispatches across seven runs, no void run.** Three
details worth passing on:

1. **Naming the tool "Task" in the prompt works, but the wire says `Agent`.** Do not grep fixtures
   for `"name":"Task"`; there are none.
2. **Provoking a permission ask needs `Write` *and* `manual`.** Under `auto` a subagent's `Write`
   ran unasked (`subagent-background.jsonl:48` and `:54`, zero `control_request` in that file).
   Under `manual`, `Read` still ran unasked (`subagent-manual-mode.jsonl`) and `Write` asked
   (`subagent-permission.jsonl:22`). That pair is the contrast, same permission mode either side.
3. **Asking for two agents "in a single message, in parallel" reliably gets two `Agent` blocks in
   one assistant frame** — which is what makes the interleaving in §4 recordable at all.

`subagent-background.jsonl` was recorded while aiming at something else — it was meant to be a second
permission probe, and the model chose to background the agent. It is the most informative fixture
here, which is an argument for keeping recordings that miss their target.

Fixture integrity was checked before commit, over all 383 lines: every line parses as JSON; every
`uuid` is RFC 4122 **version 4**; exactly one distinct `session_id` per file and seven distinct ids
overall; `init.apiKeySource` is `"none"` on all ten `init` frames and no credential-shaped strings
appear. The fixtures do contain this machine's absolute paths and the user's installed
skill/plugin/MCP inventory, which is expected for a recording and matches the existing corpus.

Which run produced which fixture:

| Fixture | Prompt / variable |
|---|---|
| `subagent-task` | the brief's prompt, `--permission-mode auto` |
| `subagent-parallel` | "use the Task tool twice … two general-purpose agents in parallel", `auto` |
| `subagent-background` | "…an agent that writes tally.txt…", `auto`; the model dispatched it async |
| `subagent-permission` | "…an agent whose only job is to use the Write tool…", `--permission-mode manual` |
| `subagent-manual-mode` | the brief's prompt but Read-only, `manual` — the no-ask control for the above |
| `subagent-no-forward` | **control**: `subagent-task`'s prompt, argv minus `--forward-subagent-text` |
| `subagent-no-forward-async` | **control**: `subagent-parallel`'s prompt, argv minus the flag; went async |
