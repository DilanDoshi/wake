# Changing a session's permission mode — the mechanism deferred I7 needed

Recorded 2026-08-12 against **Claude Code 2.1.228**, macOS 15 (arm64), model `claude-sonnet-5`.

This is the recording spike Phase 4 item 2a asks for. Deferred **I7** says the composer *"shows a
permission mode it cannot set and never reflects the real one"*, that `Session` has no setter and
`rpc.Frame` no field for one, and that whether a mode can be changed mid-session **at all** is
unrecorded — with one lead: `permission_suggestions` on the `can_use_tool` request carrying
`{"type":"setMode","mode":"acceptEdits","destination":"session"}`.

**The answer is yes, twice.** There are two independent mechanisms and both are now on the wire:

1. **`set_permission_mode`** — a `control_request` a client may write at any time, answered with a
   receipt naming the mode the session actually ended up in. §4.
2. **`updatedPermissions`** on a permission allow, echoing the ask's own `permission_suggestions`
   back. §5. Agent-initiated, only available at an ask, and it produces **no receipt**.

So I7's blocker is gone, and what stands between the label and the truth is now Wake's own wiring
rather than a missing CLI capability.

**Three things bite, and all three are the kind that ship silently.** The requested mode is not
necessarily the mode you get (§6). `bypassPermissions` is refused unless the process was launched
dangerously (§7) — a real safety property Wake gets for free. And a mid-session mode change **does
not survive a park and wake** (§8), which is I7's own failure mode — a shown mode that is not the
real one — arriving through the fix for it.

Every **wire shape** here was observed on the real stdout of a real `claude` process. One class of
claim is not a stdout observation and is labelled where it appears: §2's request shape, which was
read out of the 2.1.228 binary to *design* the probe and is then confirmed by the recordings. §9 is
what is not settled.

Fixtures, all in `testdata/stream/`:

| File | Lines | What it proves |
|---|---|---|
| `mode-set.jsonl` | 51 | the whole mechanism: ask → allow → `set_permission_mode` → **the next `Write` does not ask** |
| `mode-cycle.jsonl` | 79 | `plan` and `auto` both take; `plan` behaves like plan mode; and the refusal for an invalid mode |
| `mode-vocabulary.jsonl` | 18 | what the verb accepts: `manual` normalizes, `default` and `dontAsk` take, `bypassPermissions` is refused |
| `mode-suggestion-accepted.jsonl` | 50 | the second path — `updatedPermissions` on the allow changes the mode, with no receipt |
| `mode-park.jsonl` | 14 | a session set to `plan` mid-session, then parked |
| `mode-wake.jsonl` | 10 | the **same session id** resumed: the mode is gone |

`mode-park.jsonl` and `mode-wake.jsonl` are two processes over one session id, the same pairing
`resume-park`/`resume-wake` uses.

---

## 1. Why this was the blocker

I7's shape is worth restating because the fix has to close all of it: the mode is
`Config.PermissionMode`, read once at spawn, with no setter on `Session`, no field on `rpc.Frame`,
and a `⇧⇥` that moves a label. `CLAUDE.md` removed the indicator from the hint line rather than
show a control that reaches no process — *"shipping the indicator without the mechanism is worse
than shipping neither."*

The reason that stood was not the wiring, which is a day's work. It was that **nothing recorded
said a running session's mode could be changed**, and this project does not design around
unrecorded behaviour. That is what these six fixtures are for.

## 2. The request shape, and where it came from

Read out of the 2.1.228 binary before any recording, to know what to probe (**binary string
inspection, not a stdout observation** — it is the class §6 of the stream-json note uses):

```js
{type:"control_request", request_id:`set-mode-${randomUUID()}`,
 request:{subtype:"set_permission_mode", mode: e.permissionMode, ultraplan: e.ultraplan}}
```

Alongside it, this error string — which is why the probe was worth running rather than assuming:

```
set_permission_mode is not supported in this context (onSetPermissionMode callback not registered)
```

That sentence is the whole reason a recording was needed: the verb existing in the binary says
nothing about whether the handler is registered in `--print --input-format stream-json` mode, which
is the only mode Wake ever runs. Everything from §3 down is what the process actually did.

Wake writes no `ultraplan` and nothing here recorded one.

## 3. `permission_suggestions` is real, and it is on the ask

The lead I7 recorded is confirmed. `mode-set.jsonl:13` — every key and value the frame carries,
re-indented for width, with the scratch directory in `file_path` shortened to `...ELIDED...`:

```json
{"type":"control_request","request_id":"ad6e1502-2ee8-48e8-9da8-6c6b344fc54c",
 "request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write",
  "input":{"file_path":"...ELIDED.../work/mode-1.txt","content":"ONE\n"},
  "description":"mode-1.txt",
  "permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],
  "tool_use_id":"toolu_015ssaGAcZPwWaeudAxY7Hse"}}
```

Read off it:

- The request's key set is `subtype`, `tool_name`, `display_name`, `input`, `description`,
  `permission_suggestions`, `tool_use_id`. **No `session_id`** — the existing rule that a permission
  request must be correlated on `request_id` is unchanged, and `request_id` sits on the envelope.
- `permission_suggestions` is an **array**, its element is `{type, mode, destination}`, and
  `destination` is `"session"`. The only `type` recorded is `setMode` and the only `mode` recorded
  is `acceptEdits`.
- It is not present on every ask. `mode-cycle.jsonl:32`, an `ExitPlanMode` ask, carries
  `permission_suggestions: null`. So a client may not assume the key is there.

Four of this spike's asks are ordinary `Write` permission asks and one is an `ExitPlanMode`
approval, and **all five decode exactly as every other ask in the corpus does** — that is what
`internal/core/ask_test.go`'s census bump asserts. It matters for I7 in one specific way: three of
those asks were raised by a session whose mode had been changed *mid-session*, so
`set_permission_mode` moves **when** an ask is raised and nothing about the **shape** of one. The
airlock needs no change for the cycle.

## 4. Path A — `set_permission_mode`, and it works

`mode-set.jsonl` is the clean demonstration. Spawned `--permission-mode manual`, then:

| Line | Frame |
|---|---|
| `:5` | `init`, `permissionMode: "default"` — the spawn-flag normalization, unchanged |
| `:7` | `tool_use` `Write` |
| `:13` | `can_use_tool` for it — **the mode asks** |
| — | driver allows it |
| — | driver writes `{"type":"control_request","request_id":"spike-…","request":{"subtype":"set_permission_mode","mode":"acceptEdits"}}` |
| `:29` | `{"type":"control_response","response":{"subtype":"success","request_id":"spike-ee9cab7f-…","response":{"mode":"acceptEdits"}}}` |
| `:31` | `init`, `permissionMode: "acceptEdits"` |
| `:32` | `tool_use` `Write` — **and no `can_use_tool` follows it** |

The second write ran unasked and the file was on disk. That is the mode change taking effect, not
merely being acknowledged.

Three properties to build on:

- **It is answered like any other control request**: `subtype: "success"`, the `request_id` echoed
  nested at `.response.request_id`, and the payload nested twice at `.response.response`. The same
  trap the existing note records for reading these applies unchanged.
- **The receipt names the resulting mode**, `{"mode":"acceptEdits"}`. §6 is why that is not the
  same as the mode you asked for.
- **The next turn's `init` reports it.** So there is a second, independent observable, and it is the
  one a client that missed a receipt can still read.

`mode-cycle.jsonl` shows the same verb reaching two more modes in one process — `:5` → `{"mode":"plan"}`
with `:7` `init` `plan`, then `:56` → `{"mode":"auto"}` with `:58` `init` `auto` and `:59` a `Write`
that does not ask. **`plan` genuinely behaves as plan mode**: at `:11` the model's `Write` was
redirected into a plan file under `~/.claude/plans/` rather than the requested path, and it then
raised an `ExitPlanMode` approval at `:32`. So the mode reached mid-session is the real mode, not a
label the CLI is humouring.

## 5. Path B — `updatedPermissions` on the allow

The second mechanism, and the one `permission_suggestions` exists for.
`mode-suggestion-accepted.jsonl` answers the ask at `:13` with an ordinary allow carrying the
request's own suggestions echoed back:

```json
{"type":"control_response","response":{"subtype":"success","request_id":"<the ask's>",
 "response":{"behavior":"allow","updatedInput":{…the ask's input…},
             "updatedPermissions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}]}}}
```

Result: `:30` `init` reports `acceptEdits`, and `:31`'s `Write` raises no ask. Same effect as §4,
reached from the other side.

Two differences from Path A, both structural:

- **It is agent-initiated and ask-scoped.** The suggestion is a thing the *session* offered on a
  frame it raised. There is no suggestion when there is no ask, so this path cannot express *"put
  this idle agent into plan mode"* — which is exactly what `⇧⇥` is.
- **There is no receipt.** The allow's own `control_response` is the client's, going the other way;
  nothing comes back saying the permission update was applied. The only observable is the next
  turn's `init`, one whole turn later.

So Path B is the right shape for a card affordance — *"allow, and stop asking about edits"* — and
the wrong one for a mode key. **Deferred I7 should be built on Path A.** If a card ever grows the
Path B affordance, note that it changes the whole session's mode from inside a single tool
approval, which is a wider consequence than the button appears to have.

## 6. The vocabulary is not the vocabulary `--permission-mode` takes

`mode-vocabulary.jsonl`, four `set_permission_mode` requests in one process:

| Line | Asked for | Answer |
|---|---|---|
| `:5` | `manual` | `success`, `{"mode":"default"}` |
| `:7` | `default` | `success`, `{"mode":"default"}` |
| `:8` | `bypassPermissions` | **`error`** — see §7 |
| `:9` | `dontAsk` | `success`, `{"mode":"dontAsk"}` |

And `mode-cycle.jsonl:79`, an invalid mode:

```
{"type":"control_response","response":{"subtype":"error","request_id":"spike-c0b6ea75-…",
 "error":"Cannot set permission mode: must be one of acceptEdits, auto, bypassPermissions, default, dontAsk, plan"}}
```

Two things follow, and the first is the one that will be got wrong.

**`manual` is accepted and silently becomes `default`.** It is not in the canonical list the error
message prints, but it is not refused either — it normalizes, exactly as `--permission-mode manual`
does at spawn. So a `⇧⇥` cycle that sends the word an operator sees will work for `manual`, and the
receipt will disagree with the label. **The receipt is the authority on what the mode became, never
the string that was sent.** Any indicator built on the requested string will be wrong for one of
its own cycle positions, which is I7's exact defect wearing a new hat.

**A refusal is `subtype: "error"` with a top-level `error` string**, not a `success` carrying a
failure. That is a different shape from a permission deny, which is a *successful* control response
carrying `behavior: "deny"` — the distinction the existing note draws for `can_use_tool` holds here
too, from the other direction.

## 7. `bypassPermissions` is refused, and Wake gets that for free

`mode-vocabulary.jsonl:8`:

```
"error":"Cannot set permission mode to bypassPermissions because the session was not launched with --dangerously-skip-permissions"
```

A running session **cannot escalate itself to `bypassPermissions`** unless the process was already
launched dangerously. Nothing in this tree passes `--dangerously-skip-permissions`, so every Wake
session is on the refusing side of that check.

This is worth recording as a property rather than a curiosity, because it bounds the blast radius
of the surface Task 14's threat model is about. An agent that talked a manager into sending
`set_permission_mode` — or a `⇧⇥` bound wrong — cannot reach the mode with no checks at all. Every
*other* mode in the list is reachable, including `dontAsk` and `acceptEdits`, so this is a floor and
not a fence: it stops the worst position, not the escalation.

If `⇧⇥`'s cycle ever includes `bypassPermissions`, it must handle a refusal that arrives only as an
error receipt — the label would otherwise move to a mode the session is not in, silently, which is
I7 again.

## 8. A mid-session mode change does not survive a park and wake

The one that will cause a bug if it is not designed for.

- `mode-park.jsonl`: spawned `--permission-mode auto`, set to `plan` (`:5` receipt, `:7` `init`
  `plan`), one turn, then stdin closed — the park path.
- `mode-wake.jsonl`: the **same session id**, resumed with the bare `--resume <id>` `identityArgs`
  emits for a wake, alongside `--permission-mode auto` as `buildArgs` appends it. `:3` — `init`
  reports **`auto`**.

The session keeps its id and its conversation; the mode reverts to the spawn flag. So a mode is a
property of the *process*, not of the session, and `--resume` does not carry it.

For Wake that is concrete and unavoidable, because `⌃Q` parks the whole fleet:

- An operator who cycles an agent to `plan`, quits with `⌃Q` and comes back gets an agent running
  `auto` — **with the indicator showing whatever Wake remembered**. That is a shown mode that is not
  the real one, which is the sentence I7 exists to delete.
- So whichever half of the pair Wake persists, it has to persist **both or neither**. If the mode
  goes in the park book beside the id and the directory, `launch` must re-apply it — either as
  `Config.PermissionMode` on the woken process, which is the cheap route since a wake builds an argv
  anyway, or as a `set_permission_mode` after the wake, which is the one that would need the
  session's first frame to arrive first. The argv route looks right and is **not recorded**: see
  §9 item 1.
- If Wake persists neither, the indicator must reset to the spawn mode on a wake, and that is a
  thing the UI has to be told rather than a default it will fall into.

This is the same shape as the label carried through `launch` for a wake rather than re-derived: a
woken session must not silently acquire a property nobody changed. Here it silently *loses* one.

## 9. What this does **not** settle

Nothing below has a byte behind it, and none of it may be designed against.

1. **Whether `--permission-mode` on a `--resume` sets the mode or is ignored.** §8 shows the woken
   process reporting `auto`, which was both the spawn flag *and* the original session's mode — so
   the recording cannot separate *"the flag took"* from *"it reverted to the session's original."*
   **This is the one to record before I7 ships**, because the whole persistence design in §8 rests
   on it, and it is one cheap pair: park a session spawned `auto`, wake it with
   `--permission-mode plan`, read `init`.
2. **Whether `set_permission_mode` is accepted mid-turn.** Every request here was written between
   turns, after a `result`. Whether one lands while the model is generating, or while a
   `can_use_tool` is outstanding, is unrecorded — and `⇧⇥` is a key an operator can press at any
   moment, so this is reachable rather than theoretical.
3. **What happens to an outstanding ask when the mode changes under it.** Related to 2 and not the
   same question. The interrupt/permission collision has a recorded ordering; this has none.
4. **`ultraplan`.** Present in the binary's request shape, never sent, never recorded.
5. **The other `permission_suggestions` types.** Only `setMode` was ever offered. `addRules` and
   `permissionUpdate` appear in the binary as sibling vocabulary and no recording contains one, so
   whether `updatedPermissions` accepts a rule the client composed itself is unknown.
6. **`plan`'s full behaviour under Wake.** `mode-cycle` shows a plan file written under
   `~/.claude/plans/` and an `ExitPlanMode` approval raised. Whether Wake's card renders that plan
   correctly, and what a fleet of thirty plan-mode agents writes to that directory, is untested.
7. **Whether the mode survives `/clear`**, which already changes the session id.
8. **Whether a subagent inherits a mid-session mode change.** No recording uses `Task`.

## 10. What this means for deferred I7

Stated as a recommendation, not a decision.

- **The blocker is cleared.** `set_permission_mode` (§4) is the mechanism. It needs a
  `rpc.Frame` kind, a setter that reaches `core.Session`'s stdin the way `Interrupt` does, and the
  key. Path B (§5) is not the one to build the key on.
- **The receipt is the source of truth for the indicator, not the keystroke** (§6). The pattern is
  `⌃F`'s: the key writes a frame, the daemon's answer is the confirmation, and the label moves when
  the answer arrives. A label that moves on the keypress is the current defect with a working
  mechanism behind it — and `manual` → `default` guarantees it would be wrong on at least one cycle
  position rather than only in principle.
- **`init.permissionMode` is now a real second observable** and its trap is one-directional. At
  spawn it is normalized and must not be read as an echo of the flag — the existing rule, unchanged.
  After a `set_permission_mode` it reports the mode the session is genuinely in (`plan`, `auto`,
  `acceptEdits`, `dontAsk` all observed). So it is the right thing to reconcile a client's belief
  against, and it costs nothing: it already arrives on every turn.
- **§8 has to be designed for in the same change**, or `⇧⇥` ships a mode that silently reverts on
  `⌃Q` — which is worse than today, because today the label is honest about reaching nothing.
- **`⇧⇥`'s debt comes due here.** `docs/goals.md` §5 records that next-blocked moves when I7 lands,
  with `⌃X` and `⌃Y` reserved. Nothing in this spike changes that.

## 11. Method

Same driver and same discipline as `2026-08-12-tool-bounding-findings.md` §12 — a throwaway Go
driver in a scratch directory outside the repo, spawning
`/Users/dev/.local/share/claude/versions/2.1.228` directly, teeing stdout to the fixture through
an `io.TeeReader`, auto-allowing `can_use_tool` by echoing `request.input` back, driving turns off
`result` frames, scrubbing the eight `nestedSessionEnv` variables, and building its argv from a
transcription of `buildArgs`. It gained two things for this spike: a `control` step that writes a
raw `control_request` and waits for the `control_response` **correlated on the nested
`request_id`**, and a resume arm that emits the bare `--resume <id>` `identityArgs` emits for a wake.

These six recordings pass **no** `--mcp-config`, which is why their `init.tools` counts are large
and grow between turns — they inherit this machine's own MCP servers. That is incidental here and
is written up as §9 of the tool-bounding note.

Fixture integrity, checked over all 222 lines of this spike's six files before commit: every line
parses as JSON; every `uuid` present is RFC 4122 **version 4**; exactly **one** distinct
`session_id` per file, with `mode-park` and `mode-wake` sharing one across two files; six distinct
pids in `init.messaging_socket_path`, confirming six separate processes; `init.apiKeySource` is
`"none"` on every one; and no credential-shaped strings (`sk-ant-`, `ghp_`, `AKIA`, `xox…`, bearer
tokens) appear anywhere. The fixtures do contain this machine's absolute paths and the user's
installed skill/plugin/MCP inventory, which is expected for a recording and matches every fixture
already in the corpus.

Which scenario produced which fixture:

| Fixture | Spawn mode | Steps |
|---|---|---|
| `mode-set` | `manual` | `Write` (asks, allowed); `set_permission_mode acceptEdits`; `Write` again |
| `mode-cycle` | `manual` | `set_permission_mode plan`; `Write`; `set_permission_mode auto`; `Write`; `set_permission_mode not-a-real-mode` |
| `mode-vocabulary` | `auto` | `set_permission_mode` × 4 — `manual`, `default`, `bypassPermissions`, `dontAsk`; one turn |
| `mode-suggestion-accepted` | `manual` | `Write` (asks, allowed **with `updatedPermissions`**); `Write` again |
| `mode-park` | `auto` | `set_permission_mode plan`; one turn; stdin closed |
| `mode-wake` | `auto` | `--resume <mode-park's id>`; one turn |
