# stream-json on the wire — what `claude` actually emits

Recorded 2026-08-08 against **Claude Code 2.1.226**, macOS 15 (arm64), model `claude-sonnet-5`.

Every **wire shape** here was observed on the real stdout of a real `claude` process. Nothing is
inferred from `~/.claude/projects/*.jsonl` transcript files, and nothing is hand-written. Where a
quoted line is shortened, the elision is marked inline with `...ELIDED...`; every other quoted
line is byte-for-byte what came out of the pipe.

Two kinds of claim here are **not** stdout observations, and both are labelled inline where they
appear: statements read out of the 2.1.226 binary by string inspection (§6's
`--permission-prompt-tool` semantics, §9's `supportsNonInteractive` rule) and behaviour observed
only on **stderr** (§1's `--verbose` rejection, §10's session-id conflict). Anything neither
observed nor backed by a committed fixture is listed in §12.

Fixtures live in `testdata/stream/`:

| File | Lines | What it proves |
|---|---|---|
| `basic-turn.jsonl` | 12 | system/init, assistant text, result, hook frames, `rate_limit_event` |
| `tool-use.jsonl` | 24 | `tool_use` and `tool_result` nesting, `thinking` blocks, `thinking_tokens` |
| `permission.jsonl` | 35 | a real interactive permission request (`control_request`/`can_use_tool`) **allowed** by the client |
| `permission-deny-response.jsonl` | 25 | the identical argv and prompt, **denied** by the client — same code path, opposite decision |
| `permission-denied.jsonl` | 25 | the same ask **without** a permission client: `system/permission_denied`, where `message` is a **string** |
| `slash-commands.jsonl` | 44 | `/context` `/model` `/compact` `/clear` in stream-json; per-turn `result`; `conversation_reset` |
| `compaction.jsonl` | 44 | `system/compact_boundary`, and `user` frames whose `message.content` is a **string** |
| `resume-park.jsonl` | 12 | a turn under `--session-id <fresh-uuid>` — the "park" half of park/wake |
| `resume-wake.jsonl` | 10 | a **separate process** under `--resume <same-uuid>` recalling it, `session_id` unchanged |
| `session-id-conflict.stderr.txt` | 1 | stderr (not stdout) from re-using a live `--session-id` |

`permission.jsonl` and `permission-deny-response.jsonl` are an A/B pair: identical flags, identical
prompt, differing only in the `control_response` the client wrote back. `resume-park.jsonl` and
`resume-wake.jsonl` are two separate processes over one session id.

---

## 1. The invocation

`--verbose` is **mandatory**, not optional. Without it the process refuses to start:

```
$ claude --print --input-format stream-json --output-format stream-json --session-id ... (no --verbose)
Error: When using --print, --output-format=stream-json requires --verbose
```

Exit code 1, message on **stderr**, and **zero bytes on stdout**. Wake must read stderr — a
startup rejection is invisible on the JSON channel.

The argv used for every recording here:

```
claude --print
       --input-format stream-json
       --output-format stream-json
       --verbose
       --session-id <uuid>          # or --resume <uuid>
       --name spike
       --permission-mode auto|manual
       --brief
       --include-hook-events
       [--permission-prompt-tool stdio]   # permission.jsonl and
                                          # permission-deny-response.jsonl only — see §6
```

`--permission-mode manual` was used only for the three permission fixtures; everything else used
`auto`. Note that `init` does **not** echo `manual` back — see §3.

Two environment hazards found while recording:

1. **`claude` on PATH may not be Claude Code.** On this machine `which claude` resolves to a
   cmux wrapper shim (`$TMPDIR/cmux-cli-shims/<id>/claude` → `cmux-claude-wrapper`) which
   `exec`s the real binary at `/Users/dev/.local/bin/claude`. The wrapper does **not** corrupt
   stdout — every line was still valid JSON — but it **injects extra hooks**: the same prompt run
   through the shim produced `SessionStart:startup` ×3, `Stop` ×5 and `UserPromptSubmit` ×1,
   versus ×2, ×2 and ×0 direct. Wake should resolve the binary explicitly rather than trusting
   bare `claude` on PATH.
2. **Nested-session env leaks in.** A `claude` spawned from inside a Claude Code session
   inherits `CLAUDECODE`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_CODE_CHILD_SESSION`,
   `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_CODE_EXECPATH`. All recordings here scrubbed those. Wake's
   daemon should scrub them too, or a Wake started from a Claude session will hand its children
   a foreign session identity.

---

## 2. Envelope shape — the top-level frame

Newline-delimited JSON, one object per line, no wrapping array, no trailing frame beyond
`result`. Lines get large: `system/init` is 11.0 KB, a `SessionStart` hook response carrying
injected context is 7.6 KB, and the longest line in these fixtures is 14.8 KB (a `/context`
result). Counts below are over the nine committed JSONL fixtures — 231 lines total. A 16 MB scanner
buffer was enough; a 64 KB default is not.

**Field order is not stable and `type` is not first.** The `result` frame starts with
`"is_error"` and carries `"type":"result"` near the end. Anything that sniffs the frame kind by
string prefix will break; decode properly.

Every frame observed carries `session_id` and `uuid` at the **top level**, with exactly one
exception: `control_request`, which carries neither — true of both instances in the fixtures
(see §6). `session_id` is the field that carries the session id; it is never nested inside
`message`.

Full inventory of `type` / `subtype` pairs observed, with occurrence counts in the fixtures.
**One caveat before you read it as a decoder spec:** `subtype` is a top-level key on every row
*except* the two `control_*` rows, where it is nested — `.request.subtype` on
`control_request`, `.response.subtype` on `control_response`. A decoder that reads `subtype`
off the envelope will find nothing on those frames.

| `type` | `subtype` | n | Notes |
|---|---|---:|---|
| `system` | `hook_started` | 61 | only with `--include-hook-events` |
| `system` | `hook_response` | 61 | only with `--include-hook-events` |
| `assistant` | *(none)* | 26 | 23 model + 3 synthetic (§4) |
| `system` | `thinking_tokens` | 21 | streaming thinking-token estimate |
| `system` | `init` | 19 | emitted **once per turn**, not once per process |
| `result` | `success` | 19 | one per **turn** |
| `rate_limit_event` | *(none)* | 9 | **once per process**, not per turn — see §4a |
| `user` | *(none)* | 6 | tool results, replays, compaction summaries |
| `system` | `status` | 4 | `compacting` / compaction outcome |
| `control_request` | `can_use_tool` **at `.request.subtype`** | 2 | nested, not top-level — as with `control_response` below. Top-level keys are exactly `type`, `request_id`, `request`; there is no top-level `subtype` to read. Also no `session_id`, no `uuid` |
| `system` | `permission_denied` | 1 | `message` is a **string** |
| `system` | `compact_boundary` | 1 | |
| `conversation_reset` | *(none)* | 1 | emitted by `/clear` |
| `control_response` | `success` **at `.response.subtype`** | 0 | also nested, not top-level. Observed while probing (ack for a client-sent `control_request`); no committed fixture sends one, see §6 |

Additional top-level `type` literals present in the 2.1.226 binary but **not** observed in these
recordings: `stream_event` (needs `--include-partial-messages`), `control_cancel_request`,
`attachment`, `summary`.

---

## 3. `system` frames

### `system/init` — once per **turn**

Abridged for length — the real line (`basic-turn.jsonl:5`) is 11 KB. Only the six large arrays
are replaced; **key order is the file's own**:

```json
{"type":"system","subtype":"init","cwd":"/Users/dev/Documents/wake/.worktrees/t1-spike","session_id":"26c6aa72-5bee-4047-ae04-e5c08206561d","tools":["...ELIDED (161 entries)..."],"mcp_servers":["...ELIDED (6 entries)..."],"model":"claude-sonnet-5","permissionMode":"auto","slash_commands":["...ELIDED (133 entries)..."],"apiKeySource":"none","claude_code_version":"2.1.226","output_style":"default","capabilities":["interrupt_receipt_v1","interrupt_cancel_queued_v1","msg_lifecycle_v1"],"analytics_disabled":false,"product_feedback_disabled":false,"uuid":"8d93c66a-abf1-493a-adc1-2becc1b8f50f","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required"}
```

**`permissionMode` is normalized, not an echo of the spawn flag.** `--permission-mode auto`
reports `"auto"`, but `--permission-mode manual` reports **`"default"`**
(`permission.jsonl:5`, `permission-denied.jsonl:5` — both spawned with `manual`). `manual` is a
legal `--permission-mode` choice; it simply does not survive round-trip through `init`. Anything
in Wake that displays or re-sends a permission mode read off `init` will show and send the wrong
value. Track the mode Wake passed, not the mode `init` reports.

The complete top-level key set: `agents`, `analytics_disabled`, `apiKeySource`, `capabilities`,
`claude_code_version`, `cwd`, `fast_mode_disabled_reason`, `fast_mode_state`, `mcp_servers`,
`memory_paths`, `messaging_socket_path`, `model`, `output_style`, `permissionMode`, `plugins`,
`product_feedback_disabled`, `session_id`, `skills`, `slash_commands`, `subtype`, `tools`,
`type`, `uuid`.

`messaging_socket_path` is **nullable**: `/tmp/cc-socks/<pid>.sock` on **18 of the 19** `init`
frames in the corpus, `null` on `permission-deny-response.jsonl:5`. (19, not 9 — `init` is
per-turn, so `slash-commands.jsonl` alone contributes seven.) Do not type it as a plain string.
(It is otherwise a handy per-process discriminator — the pid in the path is how you can tell two
recordings came from two different processes.)

Note the casing inconsistency: `permissionMode` and `apiKeySource` are camelCase while every
other key on this frame is snake_case.

**`init` is not a once-per-process handshake.** `slash-commands.jsonl` contains seven `init`
frames in one process — one at the head of each turn. Wake must treat `init` as "a turn is
starting", not "the agent booted".

### `system/hook_started` / `system/hook_response`

```json
{"type":"system","subtype":"hook_started","hook_id":"351936c2-ab89-46f9-8b7f-318fe50b7e8c","hook_name":"SessionStart:startup","hook_event":"SessionStart","uuid":"eeb0ec14-a04f-49d9-b3bb-6988ca319586","session_id":"26c6aa72-5bee-4047-ae04-e5c08206561d"}
```

```json
{"type":"system","subtype":"hook_response","hook_id":"351936c2-ab89-46f9-8b7f-318fe50b7e8c","hook_name":"SessionStart:startup","hook_event":"SessionStart","output":"","stdout":"","stderr":"","exit_code":0,"outcome":"success","uuid":"6dcf37bd-5fbd-4a3b-a078-28d421cfe881","session_id":"26c6aa72-5bee-4047-ae04-e5c08206561d"}
```

Paired by `hook_id`. **Responses can arrive out of order** relative to their `started` frames,
and can interleave with other frame types — in `permission.jsonl` a `rate_limit_event` (`:18`)
lands between two `hook_response` frames (`:17`, `:19`). Never assume adjacency.

Hook frames are **environment-dependent**. They come from the *user's* `~/.claude/settings.json`
plus anything the terminal wrapper installs. In these recordings `SessionStart`, `PreToolUse`,
`PostToolUse` and `Stop` hooks all fired. `hook_response.output` can be large — the biggest here
is a 7.6 KB line from a `SessionStart` hook injecting skill context. Wake must tolerate zero hook frames, many hook
frames, and hook frames it has never seen before — the count is not a contract.

Crucially: **`hook_started` frames precede `system/init`.** The first line on stdout is not
guaranteed to be `init`.

### `system/thinking_tokens`

```json
{"type":"system","subtype":"thinking_tokens","estimated_tokens":50,"estimated_tokens_delta":50,"uuid":"f47b1321-f7b9-462d-8d6c-dc1232124134","session_id":"19f846a4-d9aa-475e-8aa0-c5dd55bfc30e"}
```

High-frequency progress ticks while the model thinks. 21 of them across these fixtures.

### `system/status`

```json
{"type":"system","subtype":"status","status":"compacting","session_id":"fc32ab1e-3bef-4583-8e9f-a2ec66949500","uuid":"6fedc01e-e77c-4948-9392-ee1fa6893876"}
```

```json
{"type":"system","subtype":"status","status":null,"compact_result":"success","session_id":"b84fd8fb-74b2-4627-89d6-7a4cd4ccdeb8","uuid":"9159a2c5-b170-4753-8bd9-f3caf6856cce"}
```

```json
{"type":"system","subtype":"status","status":null,"compact_result":"failed","compact_error":"Not enough messages to compact.","session_id":"fc32ab1e-3bef-4583-8e9f-a2ec66949500","uuid":"a2e030d8-0231-4bf7-85ad-2dae44172af5"}
```

`status` is nullable. Compaction outcome rides on the same subtype as the "started" tick.

### `system/compact_boundary`

```json
{"type":"system","subtype":"compact_boundary","session_id":"b84fd8fb-74b2-4627-89d6-7a4cd4ccdeb8","uuid":"420fb27f-6838-4f71-bd58-4671cfd993b8","compact_metadata":{"trigger":"manual","pre_tokens":50826,"post_tokens":4477,"cumulative_dropped_tokens":46349,"duration_ms":16813,"preserved_segment":{"head_uuid":"957dd3f4-7163-4bac-883f-391d22345346","anchor_uuid":"e1cf4c13-9e1a-4ac7-9648-a697e8e5e314","tail_uuid":"ebb5bcb8-6f23-4b89-9895-2509fc772e3d"},"preserved_messages":{"anchor_uuid":"e1cf4c13-9e1a-4ac7-9648-a697e8e5e314","uuids":["957dd3f4-7163-4bac-883f-391d22345346","ebb5bcb8-6f23-4b89-9895-2509fc772e3d"],"all_uuids":["957dd3f4-7163-4bac-883f-391d22345346","bcbea956-c60e-4632-ab37-f43bbd2414ff","c4535877-0976-408b-ba1d-3f3fd0ea5f7d","ebb5bcb8-6f23-4b89-9895-2509fc772e3d"]}},"logical_parent_uuid":"ebb5bcb8-6f23-4b89-9895-2509fc772e3d"}
```

This is the signal a DM view needs to draw a "context compacted" divider.

### `system/permission_denied` — **`message` is a bare string**

```json
{"type":"system","subtype":"permission_denied","tool_name":"Write","tool_use_id":"toolu_01E1cZWs9PbUg4zgHps9APA7","message":"Claude requested permissions to write to /Users/dev/Documents/wake/.worktrees/t1-spike/probe-note.txt, but you haven't granted it yet.","uuid":"c96cabc4-615e-42eb-ad4a-f8cc541171da","session_id":"eeb67ae9-98db-4d16-bc2a-05916d612382"}
```

See §7 — this is the single most dangerous shape for a naive decoder.

---

## 4. `assistant` frames

```json
{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_011CdrFRmHwQLLmPqikQFPgo","type":"message","role":"assistant","content":[{"type":"text","text":"hello"}],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":2,"cache_creation_input_tokens":26768,"cache_read_input_tokens":24018,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":26768},"output_tokens":1,"service_tier":"standard","inference_geo":"not_available"},"diagnostics":null,"context_management":null},"parent_tool_use_id":null,"session_id":"26c6aa72-5bee-4047-ae04-e5c08206561d","uuid":"833e7746-ca9b-48c2-b1af-782fb3ffde1f","timestamp":"2026-08-09T00:44:49.470Z","request_id":"req_011CdrFRj4VYDAL4q1qR3gJF"}
```

Top-level keys: `message`, `parent_tool_use_id`, `request_id`, `session_id`, `timestamp`, `type`,
`uuid`. `message` is the Anthropic Messages API assistant message verbatim, plus two
Claude-Code-only nullable fields (`diagnostics`, `context_management`).

**One content block per frame.** Every assistant frame recorded carried exactly one element in
`message.content`. A single model response with thinking + text + a tool call arrives as **three
separate `assistant` frames sharing one `message.id` and one `request_id`** —
`permission.jsonl:12,13,14` are all `msg_011CdrH1gL6kgKxdVMW8necT` /
`req_011CdrH1f3TUWaeFHEPiRNYY`, carrying `thinking`, then `text`, then `tool_use`.
(`tool-use.jsonl:11,12` is the two-block version of the same pattern.) Do not assume one frame
per model turn; group by `message.id` if you need the whole message.

`stop_reason` is `null` on all 23 model-produced assistant frames — the real stop reason only
appears on the `result` frame. (The 3 synthetic frames below are the exception: they carry
`"stop_reason":"stop_sequence"`.)

### `thinking` block

```json
{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_011CdrFVNcYQ1FpGrMS9XaVx","type":"message","role":"assistant","content":[{"type":"thinking","thinking":"","signature":"Eu8JCokBCBAYAipAUDxhC9bT...ELIDED..."}],...ELIDED...},"parent_tool_use_id":null,"session_id":"19f846a4-d9aa-475e-8aa0-c5dd55bfc30e","uuid":"4f6e1925-bd5f-451b-9185-5c1ad0454cbb","timestamp":"2026-08-09T00:45:42.622Z","request_id":"req_011CdrFVMEwSAP5JcD7CAXGo"}
```

Note `"thinking":""` with a 1692-character `signature`. Thinking text was **empty in every recording**
— the block arrives, the prose does not. Wake should render thinking blocks as a collapsed
"thought" affordance and must not assume `thinking` is non-empty.

### `tool_use` block

```json
{"type":"assistant","message":{"model":"claude-sonnet-5","id":"msg_011CdrFVNcYQ1FpGrMS9XaVx","type":"message","role":"assistant","content":[{"type":"tool_use","id":"toolu_01VVMHC7aeZ2AfSjphCjgMrx","name":"Bash","input":{"command":"echo wake-spike-ok","description":"Echo test string"},"caller":{"type":"direct"}}],"stop_reason":null,"stop_sequence":null,"stop_details":null,"usage":{"input_tokens":2,"cache_creation_input_tokens":21083,"cache_read_input_tokens":29706,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":21083},"output_tokens":2,"service_tier":"standard","inference_geo":"not_available"},"diagnostics":null,"context_management":null},"parent_tool_use_id":null,"session_id":"19f846a4-d9aa-475e-8aa0-c5dd55bfc30e","uuid":"a124e49c-134b-466f-9f47-1e1a3a94cc52","timestamp":"2026-08-09T00:45:42.858Z","request_id":"req_011CdrFVMEwSAP5JcD7CAXGo"}
```

`tool_use` nests **inside `message.content`**, exactly as the assumed shape expects. It carries
one extra Claude-Code-only field: `caller`, an object (`{"type":"direct"}`).
`input` is free-form and tool-specific — it must be `json.RawMessage`/`any`, never a struct.

### Synthetic assistant frames (local slash-command output)

```json
{"type":"assistant","message":{"id":"e1d3c4d8-684c-4523-a7f4-428164971b6b","container":null,"model":"<synthetic>","role":"assistant","stop_details":null,"stop_reason":"stop_sequence","stop_sequence":"","type":"message","usage":{...ELIDED...},"content":[{"type":"text","text":"Not enough messages to compact."}],"context_management":null},"parent_tool_use_id":null,"session_id":"fc32ab1e-3bef-4583-8e9f-a2ec66949500","uuid":"f22cfd4c-7612-456f-86eb-4724d4355734","timestamp":"2026-08-09T01:03:50.900Z"}
```

`model` is the literal string `"<synthetic>"`, `id` is a plain UUID rather than a `msg_…` id,
`stop_reason` is `"stop_sequence"`, and there is **no `request_id`**. This is how local slash
commands deliver their output. Wake can use `model == "<synthetic>"` to style these differently
from model output.

---

## 4a. `rate_limit_event` — the frame Wake will care about at fleet scale

**Exactly once per process** — not once per turn. Verbatim, `basic-turn.jsonl:7`:

```json
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed","resetsAt":1786249200,"rateLimitType":"five_hour","overageStatus":"rejected","overageDisabledReason":"org_level_disabled","isUsingOverage":false},"uuid":"96017f82-d191-4a11-84bc-1e4a9a1b5254","session_id":"26c6aa72-5bee-4047-ae04-e5c08206561d"}
```

Top-level keys: `type`, `rate_limit_info`, `uuid`, `session_id`. No `subtype`. The payload:

| Field | Observed | Meaning |
|---|---|---|
| `status` | `"allowed"` | the only value seen — other values are **unverified** |
| `resetsAt` | `1786249200` | **Unix seconds**, not milliseconds and not RFC 3339 |
| `rateLimitType` | `"five_hour"` | which window is being reported |
| `overageStatus` | `"rejected"` | whether overage billing would cover an exhausted window |
| `overageDisabledReason` | `"org_level_disabled"` | present when overage is unavailable |
| `isUsingOverage` | `false` | |

### Cadence: once per process, and then never again

Nine events across nine JSONL fixtures — **exactly one per process, no matter how many turns the
process serves, and no matter how many of those turns call the model.**

"Turns that billed" below means a turn whose cumulative `modelUsage` / `total_cost_usd`
**increased** — increase, not merely change, because `/clear` *resets* `total_cost_usd` to `0`
(§8) and so posts a large negative delta on a turn that billed nothing. It is also deliberately
**not** `num_turns ≥ 1`: one turn bills without incrementing it, and counting by `num_turns`
would undercount `compaction.jsonl` by one (see §8 and the `:37` row):

| Fixture | `rate_limit_event` at | `result` frames | Turns that billed | Second event? |
|---|---|---|---|---|
| `slash-commands.jsonl` | `:7` | 7 | **3** — `:12`, `:30`, `:44`. Of the other four, `:15`/`:18`/`:23` are local commands that cost nothing and `:37` is the `/clear`, which resets the cumulative total to `0` | none |
| `compaction.jsonl` | `:7` | 5 | **5** — `:12`, `:19`, `:26`, `:44`, **and `:37`**, the `/compact` summarization, which bills about `$0.043` while reporting `num_turns: 0` (§8) | none |
| `permission.jsonl` | `:18` | 1 | 1, but `num_turns: 2` — two model round-trips inside it | none |
| the other six | one each | 1 each | 1 each | none |

`compaction.jsonl` is the strongest single case: **five** billed API calls, one event.

It always lands after the first `assistant` frame, but its exact position varies — immediately
after it in `basic-turn.jsonl` (`:6` → `:7`), only after the first tool round-trip in
`tool-use.jsonl` (`:11` → `:18`). It is never emitted before the first API response, and never
emitted twice.

**This is the part that changes how you build the indicator.** This section recommends plumbing
`rate_limit_info` to a fleet-level display; a builder who assumes per-turn refresh gets exactly
one sample per process **lifetime**. Wake parks sessions for long stretches — a session parked
for an hour would show quota data captured at spawn, and that staleness would read as a Wake bug
rather than a property of the wire. Timestamp the sample, treat it as a spawn-time snapshot, and
do not expect the process to volunteer an update. (Whether any event arrives when quota is
actually exhausted mid-process is **unverified** — see the caveat below.)

**"Spawn-time" is not "never refreshes", and for a fleet the difference matters.** The design
spec defines a parked session as one whose process has been terminated, and waking as
`claude --resume <uuid>` (`docs/superpowers/specs/2026-08-08-wake-design.md`, §5 *The pool*) — so
a wake is a new process, and each new process emits its own event: `resume-wake.jsonl:5` is a
`rate_limit_event` on a `--resume` spawn. The sample therefore refreshes at each wake; between
spawns — including for however long a session sits parked — it never refreshes.

**Still: do not treat this as noise to skip.** It is the 4th most common top-level type here and
appears in **all nine** JSONL fixtures. Wake supervises 15–30 concurrent sessions against one
shared account quota — this is the only frame that reports that shared resource, and one session
hitting the window affects every other. A decoder built from §7.4 alone would log-and-skip it
correctly and never surface rate-limit state, which is the wrong outcome for a fleet supervisor.

Caveat: every recording ran with quota available, so **only `status: "allowed"` was ever
observed**. What an exhausted or throttled window looks like on the wire — and whether hitting
one mid-process emits a second event, which would falsify the once-per-process rule above — is
unverified (§12). Do not guess the value set.

---

## 5. `user` frames

Three distinct shapes, all `type: "user"`.

### (a) tool result — `content` is an array, `tool_result.content` is a **string**

```json
{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01VVMHC7aeZ2AfSjphCjgMrx","type":"tool_result","content":"wake-spike-ok","is_error":false}]},"parent_tool_use_id":null,"session_id":"19f846a4-d9aa-475e-8aa0-c5dd55bfc30e","uuid":"92922910-97fa-49ed-b868-a91d01a71045","timestamp":"2026-08-09T00:45:42.904Z","tool_use_result":{"stdout":"wake-spike-ok","stderr":"","interrupted":false,"isImage":false,"noOutputExpected":false}}
```

Points to note:

- `tool_result` nests inside `message.content` and pairs with the `tool_use` by `tool_use_id`.
- Inside the block, key order is `tool_use_id` **before** `type`. Irrelevant to a JSON decoder,
  relevant to anyone regex-sniffing.
- `content` inside `tool_result` was a **string** in every recording. The Messages API also
  permits an array of blocks (for images); treat it as `string | []block`.
- `is_error` is **omitted entirely** when the tool succeeded via the Write path
  (`permission.jsonl` line 29) but present as `false` on the Bash path. Decode as `*bool` or
  default-false.
- There is an extra **top-level** `tool_use_result` sibling of `message`, whose shape is
  tool-specific: an object (`{stdout,stderr,interrupted,isImage,noOutputExpected}` for Bash,
  `{type,filePath,content,structuredPatch,originalFile,userModified}` for Write) **or a bare
  string** on the denial path. Must be `json.RawMessage`.
- On a denial there is also a top-level `tool_result_meta`, and its `non_execution_kind`
  distinguishes *which* denial happened:
  `"permission-rule"` when a client answered `behavior:"deny"`
  (`permission-deny-response.jsonl:19`), `"user-rejected"` when there was no permission client to
  ask and Claude auto-denied (`permission-denied.jsonl:18`). See §6.

### (b) compaction summary — `message.content` is a **plain string**

```json
{"type":"user","message":{"role":"user","content":"This session is being continued from a previous conversation that ran out of context. The summar ...ELIDED (3466 chars of summary prose)..."},"session_id":"b84fd8fb-74b2-4627-89d6-7a4cd4ccdeb8","parent_tool_use_id":null,"uuid":"e1cf4c13-9e1a-4ac7-9648-a697e8e5e314","timestamp":"2026-08-09T01:06:48.757Z","isReplay":false,"isSynthetic":true}
```

Top-level keys on that frame: `isReplay`, `isSynthetic`, `message`, `parent_tool_use_id`,
`session_id`, `timestamp`, `type`, `uuid`. The prose ends with an instruction to the model plus
an absolute path to the pre-compaction transcript.

### (c) local-command stdout — also a **plain string**

```json
{"type":"user","message":{"role":"user","content":"<local-command-stdout>Compacted </local-command-stdout>"},"session_id":"b84fd8fb-74b2-4627-89d6-7a4cd4ccdeb8","parent_tool_use_id":null,"uuid":"f1a6dbdc-8875-4c49-a6ea-292436c2b53c","timestamp":"2026-08-09T01:06:48.965Z","isReplay":true}
```

Wake should hide or specially render `<local-command-stdout>` wrappers — they are transcript
plumbing, not conversation.

**Not observed:** an echo of the user message Wake itself sent. Without
`--replay-user-messages`, your own input never comes back. Wake must render the outgoing message
from its own state.

---

## 6. Permission requests — the part that drives the attention model

**The brief's argv can never surface a permission request.** With only
`--permission-mode manual`, Claude Code decides there is nobody to ask, auto-denies, and reports
it after the fact as `system/permission_denied` (§3). The agent then apologises and the turn
ends `subtype: "success"`. Nothing ever blocks. Recorded in `permission-denied.jsonl`.

To get a real, blocking request you must add the undocumented flag **`--permission-prompt-tool stdio`**
(`--permission-prompt-tool <tool>` is a hidden flag: "MCP tool to use for permission prompts
(only works with --print)"; the literal value `stdio` is the magic value the Agent SDK passes
when a `canUseTool` callback is supplied). With it, the ask arrives as a `control_request`:

```json
{"type":"control_request","request_id":"a059b289-60ce-4d7e-b0a2-6328b18a13d5","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"/Users/dev/Documents/wake/.worktrees/t1-spike/probe-note.txt","content":"ok"},"description":"probe-note.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_015ieYRXYKkeSmxoadCemATC"}}
```

**This frame has no `session_id` and no `uuid`** — the only frame type observed without them.
Wake must attribute a permission request to a session by which process pipe it arrived on.

The process then **blocks** until the client writes a `control_response` on stdin. Approve:

```json
{"type":"control_response","response":{"subtype":"success","request_id":"a059b289-60ce-4d7e-b0a2-6328b18a13d5","response":{"behavior":"allow","updatedInput":{"file_path":"/Users/dev/Documents/wake/.worktrees/t1-spike/probe-note.txt","content":"ok"}}}}
```

Deny:

```json
{"type":"control_response","response":{"subtype":"success","request_id":"...","response":{"behavior":"deny","message":"spike: denied by probe"}}}
```

`updatedInput` is the input the tool will actually receive; the recording echoed `request.input`
back unchanged. (Passing `{}` instead was **not** tested — do not assume it is safe.)

Both decisions have a committed fixture, recorded with identical flags and prompt:

- **Allow** (`permission.jsonl`) — the file was created and a normal `tool_result` came back with
  no `is_error` key at all (`:29`).
- **Deny** (`permission-deny-response.jsonl`) — no file was created, and at `:19`:

  ```json
  {"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"spike: denied by probe","is_error":true,"tool_use_id":"toolu_01LpZihQAgXGtEAgfWteAtdP"}]},"parent_tool_use_id":null,"session_id":"6aed1619-5359-4966-80b1-957acb63de00","uuid":"fdb67fad-7df2-4f5f-82ed-7f46968a8b1c","timestamp":"2026-08-09T01:30:46.521Z","tool_use_result":"Error: spike: denied by probe","tool_result_meta":[{"id":"toolu_01LpZihQAgXGtEAgfWteAtdP","non_execution_kind":"permission-rule"}]}
  ```

Two things fall out of that fixture, and the first **corrects an earlier draft of this document**:

1. **A client deny is `non_execution_kind: "permission-rule"`, not `"user-rejected"`.**
   `"user-rejected"` is what the *auto-deny* path emits (`permission-denied.jsonl:18`). The two
   mechanisms are distinguishable on the wire, and Wake should not conflate them: `permission-rule`
   means "a human said no", `user-rejected` means "nobody was there to ask".
2. **The `message` string in the deny response is echoed verbatim to the model** — it lands in
   `tool_result.content`, and in `tool_use_result` prefixed with `Error: `. Whatever Wake puts in
   a denial is read by the agent, so it is a place to say *why*, not just "denied".

Both denials still end the turn `subtype: "success"` with the tool listed in
`result.permission_denials`.

`--permission-prompt-tool stdio` **alone** is sufficient. The SDK's
`{"type":"control_request","request":{"subtype":"initialize"}}` handshake is *not* required —
`permission.jsonl` was recorded without it. Sending `initialize` is still useful: the ack is a
`control_response` containing the full command/agent/model catalogue for the session (a 41.9 KB
line in the probe run), which is the cheapest way to enumerate available slash commands.

Consequence for Task 4: **`--permission-prompt-tool stdio` must be in Wake's spawn argv**, and
`internal/core/session.go` needs a stdin writer for control responses, not only user messages.
Without it, "agent needs you" is unobservable and every ask silently fails.

---

## 7. Where reality diverges from the assumed decoder

The assumed Go shape is
`{type, session_id, message:{role, content:[{type, text, thinking, id, name, input, tool_use_id, content}]}, result, subtype}`.
It is directionally right and wrong in five places. The first two are hard failures.

1. **`message` is not always an object.** On `system/permission_denied`, `message` is a bare
   string. `json.Unmarshal` into a struct field typed as a struct returns
   `cannot unmarshal string into Go struct field ... of type struct { ... }` and the whole frame
   is lost. Because these frames share the top-level `message` key with `assistant`/`user`,
   a single flat envelope struct **will** hit this. Fix: type the envelope's `Message` as
   `json.RawMessage` and decode it per-`type`, or give `permission_denied` its own struct.

2. **`message.content` is not always an array.** Compaction summaries and
   `<local-command-stdout>` frames carry `"content":"<string>"`. Same class of failure. Fix:
   `Content json.RawMessage`, then branch on the first non-space byte (`[` vs `"`).

3. **`tool_result.content` is a string, not a nested block array.** The assumed shape puts
   `content` on the block struct alongside `text`; if it is typed `[]ContentBlock` every
   tool result fails to decode. It is `string` in practice and `string | []block` in principle.

4. **Frame types the shape does not mention exist and are common.**
   `rate_limit_event` (9 lines, one per **process** — §4a), `conversation_reset`,
   `control_request` and `control_response` are all top-level `type` values.
   `rate_limit_event` and `conversation_reset` have no `subtype` at all; `control_request` and
   `control_response` have one, but **nested** (`.request.subtype` / `.response.subtype`), never
   top-level. The six `system` subtypes
   `hook_started`, `hook_response`, `thinking_tokens`, `status`, `compact_boundary`,
   `permission_denied` account for **149 of the 231** fixture lines. An unknown-`type` default
   branch must log-and-skip, never error the stream.

5. **Fields the shape omits but Wake needs.** Top-level: `uuid`, `parent_tool_use_id`,
   `request_id`, `timestamp`, `tool_use_result`, `tool_result_meta`, `isReplay`, `isSynthetic`.
   In blocks: `signature` (thinking), `caller` (tool_use), `is_error` (tool_result, sometimes
   absent). `parent_tool_use_id` was `null` in all 32 frames that carried it — subagent
   attribution (`--forward-subagent-text`) was **not** exercised and remains unverified.

Two more that are semantic rather than structural:

6. **`result` is per-turn, not per-process.** `slash-commands.jsonl` has seven `result` frames
   in one process. If Wake treats `result` as "the agent exited", it will tear down live
   sessions. `result` means "this turn is done, send the next message".

7. **`system/init` is per-turn too** — seven of them in the same file. It is a turn header, not
   a boot handshake.

---

## 8. The `result` frame

```json
{"is_error":false,"duration_api_ms":3375,"num_turns":1,"stop_reason":"end_turn","session_id":"26c6aa72-5bee-4047-ae04-e5c08206561d","total_cost_usd":0.1678794,"usage":{"input_tokens":2,"cache_creation_input_tokens":26768,"cache_read_input_tokens":24018,"output_tokens":4,"server_tool_use":{"web_search_requests":0,"web_fetch_requests":0},"service_tier":"standard","cache_creation":{"ephemeral_1h_input_tokens":26768,"ephemeral_5m_input_tokens":0},"inference_geo":"not_available","iterations":[{"input_tokens":2,"output_tokens":4,"cache_read_input_tokens":24018,"cache_creation_input_tokens":26768,"cache_creation":{"ephemeral_5m_input_tokens":0,"ephemeral_1h_input_tokens":26768},"type":"message"}],"speed":"standard"},"modelUsage":{"claude-sonnet-5":{"inputTokens":2,"outputTokens":4,"cacheReadInputTokens":24018,"cacheCreationInputTokens":26768,"webSearchRequests":0,"costUSD":0.1678794,"contextWindow":1000000,"maxOutputTokens":64000,"canonicalModel":"claude-sonnet-5","provider":"firstParty"}},"permission_denials":[],"terminal_reason":"completed","fast_mode_state":"off","fast_mode_disabled_reason":"sdk_opt_in_required","subtype":"success","api_error_status":null,"result":"hello","ttft_ms":2016,"ttft_stream_ms":1398,"time_to_request_ms":19,"type":"result","duration_ms":3471,"uuid":"22d2f80a-9349-4628-969a-cd2a5ba6cb32"}
```

- `result` is the final assistant text as a **string** (empty string for `/clear`).
- `subtype` was `"success"` in all 19 results in these fixtures, including the turn where a tool
  was denied. **`is_error: false` does not mean nothing went wrong.**
- `permission_denials` is a per-turn array of `{tool_name, tool_use_id, tool_input}` — a useful
  secondary attention signal on the no-`--permission-prompt-tool` path.
- **`num_turns` counts the model round-trips that surfaced as `assistant` frames**, not turns in
  the conversation, and not every round-trip the turn paid for (see the next bullet). It equals
  the number of distinct assistant `request_id`s the turn produced: `0` for a local slash
  command, `1` for a plain answer, **`2`** whenever the model went back to the API after a tool
  result — which is four of the nine fixtures (`tool-use:24`, `permission:35`,
  `permission-denied:25`, `permission-deny-response:25`).
- **`num_turns` is not a "did this turn hit the API" flag, and neither is `duration_api_ms`.**
  `compaction.jsonl:37` reports `num_turns: 0` **and** `duration_api_ms: 0`, yet
  `modelUsage["claude-sonnet-5"]` jumps at `:26` → `:37` by **+2005 `inputTokens`, +1437
  `outputTokens`, +50 806 `cacheReadInputTokens`, +4 `cacheCreationInputTokens`, +`$0.0428268`**.
  That is `/compact` summarizing the conversation. **`inputTokens` is not the call's input size**:
  add the two cache counters and it read 52 815 input tokens, 50 806 of them (96%) cache reads.
  Anything that sizes or budgets a call off `inputTokens` alone understates it by whatever the
  cache supplied. The call surfaces **no assistant frame at all** (`:27`–`:37` is
  `status` → hooks → a second `status` → `init` → `compact_boundary` → two `user` frames →
  `result`), so a turn can spend real money with zero visible model output. Compute spend from
  `modelUsage` / `total_cost_usd` **increases** — not raw deltas, see the `/clear` reset in the
  next bullet — never from `num_turns` or `duration_api_ms`.
- `total_cost_usd` and `modelUsage` are cumulative across turns, not per-turn — and `/clear`
  resets `total_cost_usd` to `0`. The observed run went
  `0.1354 → 0.1354 → 0.1354 → 0.1354 → 0.1927 → 0 (after /clear) → 0.1364`.
- `stop_reason` here is real (`"end_turn"`), unlike on the assistant frames.
- `terminal_reason: "completed"`. `api_error_status` is nullable.
- Mixed casing again: `modelUsage` camelCase, everything else snake_case.

---

## 9. Slash commands in stream-json — the §9 open question, closed

Sent as ordinary user messages whose text is the command, over the same
`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"/compact"}]}}` line
that carries prose. Recorded in `slash-commands.jsonl` and `compaction.jsonl`.

| Command | Survives? | What comes back |
|---|---|---|
| `/context` | **Yes** | synthetic `assistant` text + `result` with the full markdown usage table, `num_turns: 0` |
| `/model` | **Yes** | synthetic `assistant` text: `"Current model: Sonnet 5 (effort: xhigh)\nUsage: /model <name>. Available: sonnet, opus, haiku, fable, ..."` |
| `/compact` | **Yes** | `system/status status:"compacting"` → `system/status compact_result:"success"` → `system/init` → `system/compact_boundary` → two `user` frames (summary, `<local-command-stdout>`) → `result`. Verified end to end: context went 50 826 → 4 477 tokens and the session still recalled the number it had been asked to remember. **It is not free**: the summarization is a billed API call — about `$0.043`, 52 815 input tokens of which only 2005 are fresh, 1437 out — even though its `result` reports `num_turns: 0` and `duration_api_ms: 0` and emits no assistant frame. Full delta in §8. |
| `/clear` | **Yes — but it changes the session id.** | `conversation_reset` → `system/init` **with a new `session_id`** → `result` with `result: ""`. Memory is genuinely gone (the follow-up answered `NONE`). |
| `/resume` | **No** | declared `type:"local-jsx"` in the binary (an interactive React picker) with no `supportsNonInteractive`. Use the `--resume` CLI flag instead. |

The `/clear` reset, verbatim:

```json
{"type":"conversation_reset","new_conversation_id":"b3144871-905e-492b-a40a-3d4cc2bcae1a","uuid":"6549c5d3-e9b7-4661-b219-cddc815b658e","session_id":"fc32ab1e-3bef-4583-8e9f-a2ec66949500"}
```

Note the trap: `new_conversation_id` (`b3144871…`) is **not** the new `session_id`. The session
id that every later frame carries is `6524c398-cff7-4716-8eff-1c1b3d704a31`, which appears
nowhere in the reset frame.

It does **not** wait for the next `init` either. The new id first appears four frames earlier, on
a `hook_started` at `slash-commands.jsonl:32` whose `hook_name` is `SessionStart:clear` — the
`init` that also carries it is `:36`. A decoder that only learns session ids from `init` will
misattribute `:32`–`:35` to the dead session. Read `session_id` off **every** frame.

**Consequence for Wake:** the UUID Wake assigns via `--session-id` is authoritative only until
someone types `/clear`. Wake must track `session_id` from the frame stream and update the
registry whenever it changes, or park/wake will reattach to the wrong conversation.
(*Not tested:* what `--resume <original-uuid>` does after a `/clear`. The pre-clear transcript
still exists on disk under the original UUID, so it most likely resumes that — but this was not
verified and Task 4 should not rely on it.)

The underlying rule, read off the 2.1.226 binary: local commands carry a
`supportsNonInteractive: true` flag (`model`, `clear`, `compact`, `context`, `autocompact`,
`color`, `auto-mode-setup`, … all have it). Commands declared `type: "local-jsx"` render a React
component and cannot work headless. `/model` has *both* a `local` and a `local-jsx` definition;
the headless one is what answers in stream-json.

---

## 10. Session identity, park and wake

All three claims below are backed by committed fixtures.

- `--session-id <uuid>` with a fresh UUID: accepted, and that UUID is the `session_id` on every
  frame. Wake-assigned UUIDs are real session ids. (`resume-park.jsonl`, session
  `0bd89bfa-ff4f-4828-89e7-f47f5f8e943c`.)
- `--resume <uuid>` in a **separate later process**: **lossless and id-preserving.**
  `resume-park.jsonl` is a first process told "Remember the number 47", answering `ok`.
  `resume-wake.jsonl` is a second process, started with `--resume` on the same UUID, answering
  `47`. `session_id` is `0bd89bfa-…` on every frame of both files. Park/wake works.
- **Re-using `--session-id` on an existing session fails.** Verbatim, and the entire content of
  `session-id-conflict.stderr.txt`:

  ```
  Error: Session ID 0bd89bfa-ff4f-4828-89e7-f47f5f8e943c is already in use.
  ```

  That line is the whole fixture, and it is the only part of this bullet that is committed
  evidence. The accompanying "exit code 1, zero bytes on stdout" is **testimony** — both were
  observed at recording time (`echo $?` and `wc -c`), but an empty stdout capture is
  indistinguishable from a missing one, so there is nothing meaningful to commit. Treat the
  stderr string as fixture-backed and the exit/stdout behaviour as observed-but-unbacked.

  Wake must use `--session-id` exactly once per session and `--resume` forever after, and must
  read stderr: this failure is invisible on the JSON channel.

**Scope caveat.** All three were exercised **headlessly**. The brief's Step 5 called for opening
the interactive `claude --resume <uuid>` TUI and confirming by eye; that cannot be driven from
this environment, so it was replaced with the headless `--resume` above. The substitution is
sound for Wake's purposes — Wake never drives a TUI, it spawns `--print` processes — and the
property Wake actually depends on (a separate process resumes a Wake-assigned UUID with history
intact and `session_id` preserved) is exactly what the two fixtures show. But if some later task
needs the *interactive* resume path specifically, that remains unverified (§12).
- Transcripts land in `~/.claude/projects/<cwd-with-slashes-as-dashes>/<uuid>.jsonl`. The
  worktree used here mapped to
  `~/.claude/projects/-Users-dev-Documents-wake--worktrees-t1-spike/`.
  Note the `--worktrees`: both `/` **and** `.` collapse to `-`, so the mapping is not a simple
  `s|/|-|g` and is not reversible. Do not reconstruct this path by hand; read `cwd` and
  `memory_paths` off `system/init` instead.

---

## 11. Input format

One JSON object per line on stdin. What was sent for every recording:

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Reply with exactly: hello"}]}}
```

- Accepted exactly as the brief specifies.
- The process **stays alive across turns**: write the next user line after seeing a `result`
  frame and the same process handles it. Seven turns were driven through one process.
- Closing stdin ends the process after the in-flight turn completes. Leaving it open keeps the
  process alive and idle between turns. That is a **live** session in Wake's terms, not a parked
  one: the design spec defines parked as "process terminated, zero memory"
  (`docs/superpowers/specs/2026-08-08-wake-design.md`, §5 *The pool*), so a held-open idle process
  still occupies a live-cap slot rather than freeing one. Parking requires the process to end;
  waking is `--resume` in a new process (§10).
- `control_response` lines go on the same stdin channel (§6).

---

## 12. Unverified — do not design around these

Flags and modes never exercised:

- **`--replay-user-messages`.** CLAUDE.md lists it as a flag Wake depends on, and §5 records what
  happens *without* it (your own input never comes back). What it emits when **enabled** — frame
  type, whether the echo is distinguishable from a real `user` frame, whether it sets `isReplay` —
  was never recorded. Task 4 should not assume the echoed frame is distinguishable from a genuine
  `user` frame without testing it.
- `--include-partial-messages` / `stream_event` frames (token-level streaming).
- `--forward-subagent-text` and non-null `parent_tool_use_id`.
- The **interactive** `claude --resume <uuid>` TUI (§10). Headless `--resume` is fixture-backed;
  the TUI path is not, and could not be driven from the recording environment.
- `--permission-prompt-tool stdio` with `updatedInput: {}` rather than an echo of `request.input`.

Frames and values never seen:

- `attachment`, `summary`, `control_cancel_request` frames (present in the binary, never seen).
- `interrupt` control requests, despite `interrupt_receipt_v1` appearing in `init.capabilities`.
- Multi-block `message.content` arrays on a single frame — always length 1 here.
- `result.subtype` values other than `"success"` (e.g. error / max-turns paths).
- `rate_limit_info.status` values other than `"allowed"` (§4a) — every recording had quota
  available, so throttled and exhausted windows are unseen. This is the most likely of these to
  bite Wake in production.
- **Whether exhausting a window mid-process emits a second `rate_limit_event`.** §4a's
  once-per-process rule rests on nine processes that all had quota throughout; if the CLI does
  push an update on exhaustion, that rule is wrong in exactly the situation a fleet quota
  indicator exists to catch. Nothing in the corpus settles it.
- Whether `tool_result.content` ever arrives as an array (image results).
- MCP tool calls (`mcp__*`) — none were exercised.

Also unverified: what `--resume <original-uuid>` does after a `/clear` (§9), and whether
`permissionMode` values other than `manual` are normalized by `init` (§3 shows `auto` survives
and `manual` becomes `default`; the other four choices were not tried).

---

## 13. Method

Recorded with a throwaway `scripts/spike` (deleted in the commit that added the first six
fixtures): a plain recorder, a `permprobe` that speaks the control protocol, and a `multi`
driver that feeds several turns down one stdin. Each recording used a fresh `uuidgen`, ran with
`cwd = /Users/dev/Documents/wake/.worktrees/t1-spike`, scrubbed the inherited
`CLAUDE_CODE_*`/`CLAUDECODE` env, and invoked `/Users/dev/.local/bin/claude` directly to
bypass the cmux shim. stdout was passed through byte-for-byte; the fixtures are that stdout,
unedited. Every line in every JSONL fixture parses as JSON.

Which driver produced which fixture:

| Fixture | Driver | Notes |
|---|---|---|
| `basic-turn`, `tool-use`, `permission-denied` | `spike` | one prompt, stdin closed |
| `permission`, `permission-deny-response` | `permprobe` | `--permission-prompt-tool stdio`, no `initialize` handshake; answers the `can_use_tool` with `allow` / `deny` respectively |
| `slash-commands`, `compaction`, `resume-park`, `resume-wake` | `multi` | next prompt written on each `result`; `resume-wake` re-ran the same driver with `--resume` instead of `--session-id` |
| `session-id-conflict.stderr.txt` | none | direct `claude` invocation, stderr redirected — no driver involved |

The last two rounds of recordings were made after `scripts/spike` was deleted, using the already
built binaries from the scratch directory; the sources are recoverable from this document's
description but were not re-committed, since the fixtures are the deliverable.

The conflict fixture is the complete unmodified stderr of the following, run from
`/Users/dev/Documents/wake/.worktrees/t1-spike`. Note it invokes the resolved binary, not bare
`claude`, consistent with every other recording here — bare `claude` would hit the cmux shim
described in §1:

```bash
echo '{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Say ok"}]}}' \
  | env -u CLAUDECODE -u CLAUDE_CODE_ENTRYPOINT -u CLAUDE_CODE_SESSION_ID \
        -u CLAUDE_CODE_CHILD_SESSION -u CLAUDE_CODE_EXECPATH -u CLAUDE_PID \
        -u CLAUDE_EFFORT -u CLAUDE_PLUGIN_DATA \
    /Users/dev/.local/bin/claude \
      --print --input-format stream-json --output-format stream-json --verbose \
      --session-id 0bd89bfa-ff4f-4828-89e7-f47f5f8e943c --name spike \
      --permission-mode auto --brief --include-hook-events
```

which exited 1 with zero bytes on stdout — observed, not committed; see the note in §10.

Fixtures were scanned for credentials before commit; `init.apiKeySource` is `"none"` and no
tokens appear. They do contain this machine's absolute paths and the user's installed
skill/plugin/MCP inventory, which is expected for a recording and is what `.gitignore` warns
about.
