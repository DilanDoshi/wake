# `--resume` and `--fork-session` — what `claude` actually does

Recorded 2026-08-09 against **Claude Code 2.1.226**, macOS 15 (arm64), model `claude-opus-5[1m]`.
The spike `docs/notes/v1_goals.md` §"Ordering" calls for: *"one resume spike, which unblocks three
features at once"* — park/wake, session importing (b), and fork.

Every **wire shape** here was observed on the real stdout of a real `claude` process. Six stdout
fixtures, **137 lines**, **six distinct processes** (six distinct pids in
`init.messaging_socket_path`), plus one stderr fixture from a seventh process that produced no
stdout at all. Nothing is hand-written. Where a quoted line is shortened the elision is marked
inline with `...ELIDED...`; every other quoted line is byte-for-byte what came out of the pipe.

Two kinds of claim here are **not** stdout observations, and each is labelled where it appears:

- **`[disk]`** — the contents of `~/.claude/projects/<slug>/<uuid>.jsonl`. Question F is a question
  about the filesystem, so there is no stdout answer to it. These files are **not** committed and
  are **not** the same format as the stream; they are quoted only where the bytes decide something.
- **`[testimony]`** — process exit codes, observed via `cmd.Wait()` at recording time. An exit code
  is not a byte on a stream, so there is nothing to commit.

Anything neither observed nor backed by a fixture is in §10.

| File | Lines | Identity flags | What it proves |
|---|---:|---|---|
| `resume-fork-parent.jsonl` | 34 | `--session-id <P>` | the parent every other recording resumes: learns a passphrase, then writes `parent.txt` through a `can_use_tool` |
| `resume-fork-both-flags.jsonl` | 10 | `--resume <P> --fork-session --session-id <F>` | **the answer to A**: accepted, and every frame carries `<F>` — Wake's id, not a minted one |
| `resume-fork-no-session-id.jsonl` | 32 | `--resume <P> --fork-session` | the counterfactual: with no `--session-id`, the CLI mints one and it is on the first frame. Also a fork's `can_use_tool` |
| `resume-session-id-without-fork.stderr.txt` | — | `--resume <P> --session-id <N>` | **rejected**, exit 1, zero bytes of stdout. There is no companion `.jsonl` on purpose |
| `resume-plain-permission.jsonl` | 32 | `--resume <P>` | a plain resume reuses `<P>`, and its `can_use_tool` still arrives — the park/wake path |
| `resume-collide-first.jsonl` | 19 | `--session-id <L>` | held open and idle while a second process resumed `<L>`; took a normal turn afterwards |
| `resume-collide-second.jsonl` | 10 | `--resume <L>` (concurrent) | **the answer to D**: a resume against a *live* session is accepted, silently |

The last two are a true A/B in the sense the stream-json note uses: one variable, the second
process's existence, with the first process's argv unchanged.

Three ids recur throughout and are worth having in front of you:

```
P = 24a6bea1-4bcd-4c1b-99d5-136ed007ef70   the parent
F = 6e5e2c6d-861c-456a-ae9a-3396552c0c3f   the id Wake asked for, and got
L = 3e335d2d-a9eb-4867-a2d4-7367075f071a   the live session two processes shared
```

---

## 1. The invocation

`session.go`'s `buildArgs`, byte for byte, with only the identity flags varying. Nothing was
dropped: all eight fixed flags including `--forward-subagent-text`, and `--name` /
`--permission-mode` in the positions `buildArgs` puts them:

```
claude --print
       --input-format stream-json
       --output-format stream-json
       --verbose
       --permission-prompt-tool stdio
       --brief
       --include-hook-events
       --forward-subagent-text
       <identity flags>
       --name spike
       --permission-mode manual
```

`--permission-mode manual` throughout, so a `can_use_tool` would actually be raised. As the
stream-json note's trap says, `init` normalizes it: every fixture reports
`"permissionMode":"default"`.

**`--fork-session` and `--resume` are placed exactly where `--session-id` sits in `buildArgs`.** A
recording made with a different flag set is not evidence about Wake, and this is the flag set Wake
ships.

---

## 2. A — `--resume … --fork-session --session-id <new>` is **accepted**, and `--session-id` wins

This was the question that was going to decide the design, and the answer is the good one.

`resume-fork-both-flags.jsonl:1` — the **first byte off stdout**, carrying the id Wake chose:

```json
{"type":"system","subtype":"hook_started","hook_id":"e3ba7c93-eec1-4921-a683-9e1cd732ab81","hook_name":"SessionStart:fork","hook_event":"SessionStart","uuid":"8f7313f5-195f-4464-aa98-a61fdbf0611b","session_id":"6e5e2c6d-861c-456a-ae9a-3396552c0c3f"}
```

All ten lines carry `6e5e2c6d-…` and nothing else does; the parent's `24a6bea1-…` never appears on
the stream. `[testimony]` exit 0, and `resume-fork-both-flags`'s stderr file is zero bytes.

**So Wake does not have to learn a forked session's id from the stream.** The ownership `maySpawn`
is built on — Wake mints the UUID, puts it in the argv, and the reaper proves a process group by
finding it there — survives forking unchanged. A fork is an ordinary Wake session from the moment
it starts, exactly as `v1_goals.md` hoped but could not assert.

### And the CLI says why, in its own words

The adjacent case is **rejected**, and the message is the specification. Whole file,
`resume-session-id-without-fork.stderr.txt`:

```
Error: --session-id can only be used with --continue or --resume if --fork-session is also specified.
```

`[testimony]` exit 1, and **zero bytes on stdout** — which is why there is no
`resume-session-id-without-fork.jsonl`. (`fixtures_test.go` fails a fixture that produces no
events; the precedent is `session-id-conflict.stderr.txt`, which has no companion either.)

**This is a hard constraint on `buildArgs`, and it points at park/wake rather than at fork.** Wake
currently appends `--session-id` whenever `cfg.SessionID` is set. A park/wake path that adds
`--resume <id>` and leaves `--session-id <same id>` in place **will not start** — the CLI refuses
the pair regardless of whether the two ids are equal, and it refuses it before emitting anything, so
the only diagnosis is the stderr tail. The resume path must *replace* `--session-id`, not
supplement it. The fork path is the one case where both may be passed.

Read the three together and the flag matrix is complete:

| argv | Result |
|---|---|
| `--session-id <new>` | fresh session, Wake's id (`resume-fork-parent.jsonl`) |
| `--session-id <in use>` | `Error: Session ID … is already in use.` (`session-id-conflict.stderr.txt`, 2026-08-08) |
| `--resume <id>` | resumes, **reuses** `<id>` (`resume-plain-permission.jsonl`) |
| `--resume <id> --session-id <new>` | **refused**, exit 1, no stdout |
| `--resume <id> --fork-session` | forks, CLI **mints** the id |
| `--resume <id> --fork-session --session-id <new>` | forks, **Wake's** id |

---

## 3. B — the id is on frame 1, and `init` is not the earliest thing that names it

When Wake supplies `--session-id`, §2 already answers B: the id is Wake's, and it is on the first
frame. The question that remains is what happens when it does not — which is the shape park/wake
and any future "adopt whatever the CLI minted" path would face.

`resume-fork-no-session-id.jsonl:1`, first byte off stdout, with a **minted** id:

```json
{"type":"system","subtype":"hook_started","hook_id":"42dae10c-9133-4159-9a9a-0ea0c2f9f30f","hook_name":"SessionStart:fork","hook_event":"SessionStart","uuid":"7ad6a3e1-db59-44b0-9496-591a722521e4","session_id":"43824f4f-41e9-4754-8234-718c4e0edf5c"}
```

`43824f4f-…` is on 31 of the 32 lines — every line that has a `session_id` at all. The one that does
not is the `can_use_tool` at `:17`, which carries no `session_id`, exactly as the stream-json note
already established for that frame.

So the `/clear` trap repeats: **the id arrives on a `hook_started` before the `init` that announces
it** — `:1` versus `:3` here. Anything keying on `init` misses two frames. `KindSessionReset`'s
existing rule, *re-key on `session_id` changing between events*, is the right one and needs no
change.

### The start mode is legible, and it is legible on frame 1 — with a caveat that disqualifies it

`hook_name` discriminates all three ways a process can begin. One line from each of three
recordings:

```
resume-fork-parent.jsonl:1        "hook_name":"SessionStart:startup"    (--session-id, fresh)
resume-collide-second.jsonl:1     "hook_name":"SessionStart:resume"     (--resume)
resume-fork-no-session-id.jsonl:1 "hook_name":"SessionStart:fork"       (--resume --fork-session)
```

**Wake may not depend on this.** `hook_started` frames exist because `--include-hook-events` is
passed *and* because something on this machine registers a `SessionStart` hook — here two installed
plugins do (`openai-codex/codex`, `claude-plugins-official/superpowers`); `~/.claude/settings.json`
registers none. On a machine with no `SessionStart` hook there is no such frame and `init` is first.
A discriminator that disappears with a plugin uninstall is not a protocol.

And `init` itself carries **no** linkage. The 11,233-byte `init` in each of the three fork/resume
recordings contains no `fork`, `resume`, `parent` or `original` substring anywhere. There is no
`forked_from`, no parent session id, nothing on the stream that says this session has an ancestor.
**Wake must record the parent→fork edge itself; the CLI will not tell it later.**

---

## 4. C — a fork inherits the conversation

The parent was told a passphrase and nothing else was ever told it. `resume-fork-parent.jsonl` turn
1 asked it to remember `cobalt-mango-7731`; the fork was asked *"What passphrase did I ask you to
remember earlier in this conversation?"* in a separate process with no other context.

`resume-fork-no-session-id.jsonl:4`:

```json
{"type":"assistant","message":{"model":"claude-opus-5","id":"msg_011CdswxnYs4P24eUGpmLr2u","type":"message","role":"assistant","content":[{"type":"text","text":"cobalt-mango-7731"}],...ELIDED...},"parent_tool_use_id":null,"session_id":"43824f4f-41e9-4754-8234-718c4e0edf5c","uuid":"20794ccb-9c73-4d1d-9bbb-c80ac47d0d15","timestamp":"2026-08-09T22:16:59.139Z","request_id":"req_011Cdswxj9EXpCgCYCHBuy1x"}
```

and the turn end, `resume-fork-no-session-id.jsonl:10`:

```json
{"is_error":false,...ELIDED...,"session_id":"43824f4f-41e9-4754-8234-718c4e0edf5c",...ELIDED...,"subtype":"success","api_error_status":null,"result":"cobalt-mango-7731",...ELIDED...}
```

The `--session-id`-carrying fork answered identically, `resume-fork-both-flags.jsonl:4`:
`"content":[{"type":"text","text":"cobalt-mango-7731"}]`, `session_id
6e5e2c6d-861c-456a-ae9a-3396552c0c3f`.

Both forks read a fact that existed only in `24a6bea1-…`'s history, in a process that had never
seen it. **A fork inherits the conversation, and it does so under a different session id.**

Note the cache counters as corroboration rather than as the claim: `:4` reports
`"cache_read_input_tokens":38563` for a one-sentence prompt. The parent's context was loaded.

---

## 5. D — `--resume` does **not** collide, and that is worse than colliding

`--session-id` refuses a live id. `--resume` does not refuse anything.

`resume-collide-first` was started with `--session-id 3e335d2d-…`, took one turn, and then **held
stdin open and idle for 90 seconds** — a live process with a live transcript on disk. 34 seconds
into that window, a second process ran `--resume 3e335d2d-…`.

The second process started normally. `resume-collide-second.jsonl:1`:

```json
{"type":"system","subtype":"hook_started","hook_id":"1de1e440-201f-4773-9d38-4ebd6fc5f3e5","hook_name":"SessionStart:resume","hook_event":"SessionStart","uuid":"5423be28-d91b-41ae-9d96-f1b633b5126b","session_id":"3e335d2d-a9eb-4867-a2d4-7367075f071a"}
```

…read the live session's context, `resume-collide-second.jsonl:4`:

```json
{"type":"assistant","message":{"model":"claude-opus-5","id":"msg_011Cdsx8FKSXKEbWXL7RFYjS","type":"message","role":"assistant","content":[{"type":"text","text":"silver-heron-9042"}],...ELIDED...},"parent_tool_use_id":null,"session_id":"3e335d2d-a9eb-4867-a2d4-7367075f071a","uuid":"0ecc80fe-e33f-429c-9807-f7c8b9ca8a7e","timestamp":"2026-08-09T22:19:07.260Z","request_id":"req_011Cdsx8EMeAzZ1MA1SQURa7"}
```

…and exited 0 with **zero bytes on stderr** `[testimony]`. There is no
`resume-collide-second.stderr.txt` because there was nothing in it.

The first process was unharmed and unaware. After the hold expired it took a normal turn,
`resume-collide-first.jsonl:14`:

```json
{"type":"assistant",...ELIDED...,"content":[{"type":"text","text":"ALIVE"}],...ELIDED...,"session_id":"3e335d2d-a9eb-4867-a2d4-7367075f071a","uuid":"ce936469-231b-4e83-be7c-028510f063b1","timestamp":"2026-08-09T22:20:19.685Z",...ELIDED...}
```

ending `"subtype":"success"`, `"terminal_reason":"completed"`, `"is_error":false` (`:19`), exit 0.

**So there is no error to detect.** Two live processes carried session id `3e335d2d-…`
simultaneously, each answering correctly from its own in-memory history, neither told about the
other. This is precisely the "two live sessions sharing one id" state `v1_goals.md` identified as
breaking the roster, `maySpawn` and the reaper — and `--resume` walks into it with no diagnostic at
all.

### `[disk]` What it does to the transcript

Both processes wrote to the same file, and the history **branched in place**. From
`~/.claude/projects/<slug>/3e335d2d-….jsonl` after both had exited — the assistant turn
`62d3a136` (the first process's `OK`) has two children:

```
line 18  type=user     uuid=81d95b82…  parentUuid=62d3a136…  ts=22:19:05   (second process)
line 24  type=system   uuid=146a51d0…  parentUuid=62d3a136…  ts=22:18:48   (first process)
```

`146a51d0` is a `stop_hook_summary` **timestamped 22:18:48 and written after 22:20:18** — the first
process flushed state it had been holding since before the second one existed, on top of the second
one's writes. And the `last-prompt` pointer was rewritten twice, ending on the first process's leaf:

line 20, written by the second process:

```json
{"type":"last-prompt","lastPrompt":"What passphrase did I ask you to remember earlier in this conversation? Reply with exactly that passphrase and nothing else. Do not use any tool.","leafUuid":"0ecc80fe-e33f-429c-9807-f7c8b9ca8a7e","sessionId":"3e335d2d-a9eb-4867-a2d4-7367075f071a"}
```

line 27, written by the first, and the last line of the file:

```json
{"type":"last-prompt","lastPrompt":"Reply with exactly the word ALIVE and nothing else. Do not use any tool.","leafUuid":"ce936469-231b-4e83-be7c-028510f063b1","sessionId":"3e335d2d-a9eb-4867-a2d4-7367075f071a"}
```

The surviving leaf is the first process's. **The second process's turn is now off the main line**,
so whoever resumes `3e335d2d-…` next follows `ce936469` and silently does not have it.

**Consequence for session importing.** The v1_goals question was *"whether `--resume` collides the
same way `--session-id` does"*, framed as "the picker must either detect that or the flow must say
close it there first". The answer is that **it does not collide, so there is nothing to detect on
the stream**, and adopting a session someone still has open silently corrupts their history. The
flow must say *close it there first* — and the picker cannot learn from `claude` whether it is
closed. Whatever liveness check the picker uses has to come from somewhere else (a `ps` scan for
the id in an argv is what the reaper already does), and that check is now load-bearing rather than
a nicety.

**And it makes the fork the safe primitive for importing, not just for forking.** `--resume …
--fork-session --session-id <new>` against a session someone still has open costs the original
nothing (§7): the parent file is not touched.

---

## 6. E — a resumed process and a forked process both honour `--permission-prompt-tool stdio`

This was the one that could have blocked an agent forever. It does not.

The **fork** raised a `can_use_tool` on stdout, `resume-fork-no-session-id.jsonl:17`:

```json
{"type":"control_request","request_id":"b3e01af6-5744-40cf-af7e-2473c68ba3d3","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"/private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/rf/cwd/fork.txt","content":"ok\n"},"description":"fork.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_011tEugAQhS3DAdBx5kU3bE2"}}
```

The driver answered it with the `allow` frame `encode.go` writes, the tool ran, and the turn ended
normally (`:27`): `"content":[{"type":"text","text":"Created \`fork.txt\`."}]`. `fork.txt` exists on
disk.

The **plain resume** did the same, `resume-plain-permission.jsonl:17`:

```json
{"type":"control_request","request_id":"ffa5a7ad-5926-42c7-9d8d-6d0d5e206a14","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"/private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/rf/cwd/resumed.txt","content":"ok\n"},"description":"resumed.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_01LKzeVZxwWL86GD1qmNtNxG"}}
```

…ending `"result":"Created \`resumed.txt\`."`. Both are byte-identical in shape to the parent's own
ask at `resume-fork-parent.jsonl:19`, so the resumed and forked processes are not on some degraded
path — it is the same request.

The **run that would have established nothing** is worth naming: a prompt the model answers without
a tool proves only that the model was polite. Every permission run here provoked an actual `Write`
against a real file in a real scratch directory, and the `tool_use_id` and the file on disk are
what make it a tool call rather than a claim about one.

The rest of the argv is honoured too, in the weak sense that all eight fixed flags were present on
every successful run and none of them caused a rejection: `--verbose` (stream-json is legal),
`--brief`, `--include-hook-events` (hook frames on all six), `--forward-subagent-text`
(`parent_tool_use_id` is `null` throughout — no subagent was exercised), `--name`,
`--permission-mode`. The only flag combination the CLI refused is the one in §2.

---

## 7. F — `[disk]` a fork lands in its own file, and the parent is not touched

Before the fork, the project directory held one transcript. After
`--resume <P> --fork-session --session-id <F>`:

```
26   63601  24a6bea1-4bcd-4c1b-99d5-136ed007ef70.jsonl     (P, unchanged: 26 lines, 63601 bytes, before and after)
27   63944  6e5e2c6d-861c-456a-ae9a-3396552c0c3f.jsonl     (F, new)
```

The fork's file is **named for the id Wake chose**, which is the whole reason F matters: a fork is
discoverable by exactly the same rule as any other session (`~/.claude/projects/<slug>/<uuid>.jsonl`),
so it is re-forkable and importable with no new bookkeeping.

Its contents are a **copy of the parent's history rewritten with the fork's `sessionId`**, with the
per-message `uuid`s preserved. The same six leading messages appear in both files under different
session ids:

```
P:5  type=attachment sessionId=24a6bea1-…  uuid=783ec5c3…  parentUuid=None
F:6  type=attachment sessionId=6e5e2c6d-…  uuid=783ec5c3…  parentUuid=None
P:7  type=user       sessionId=24a6bea1-…  uuid=557c994c…  parentUuid=4e53e360…
F:8  type=user       sessionId=6e5e2c6d-…  uuid=557c994c…  parentUuid=4e53e360…
```

so **`uuid` is stable across a fork and `sessionId` is not**. If Wake ever needs to prove two
transcripts share an ancestor it has a key to do it with — but that is an inference about intent
from two files, not something the CLI states, and §3 established the stream never mentions the
parent at all.

The un-`--session-id`'d fork behaved identically: `43824f4f-….jsonl`, its own file, parent still 26
lines. Four transcripts exist in that directory at the end of the spike — parent, two forks, and the
collided live session — and each fork's file carries only its own lineage.

`P` did grow later, to 41 lines, when `resume-plain-permission` resumed it directly. That is the
contrast that makes the claim mean something: **a plain `--resume` appends to the parent's file and
a fork does not touch it**, which is exactly what makes a fork safe to take against a session
somebody else may care about — and, per §5, the only resume-family invocation that is.

---

## 8. What this settles for Wake

Not built here — this is a recording spike and `internal/` was not touched — but these are the
constraints the next change inherits.

1. **`buildArgs` must treat `--session-id` and `--resume` as alternatives, not as an addition.**
   Passing both without `--fork-session` is a startup failure with no stdout (§2). This is the
   park/wake path and it is the single most likely way to get this wrong, because the natural edit
   is to append `--resume` and leave the rest alone.
2. **Fork keeps Wake's identity model intact.** `--resume <parent> --fork-session --session-id <new>`
   is the invocation: Wake mints the UUID as it does today, `maySpawn` is unchanged, and the reaper
   still finds that UUID in the argv (§2).
3. **Wake must persist the parent→fork edge itself.** Nothing on the stream says a session was
   forked or from what (§3).
4. **Session importing cannot adopt a session that is still open, and cannot detect that it is.**
   No error, no frame, silent transcript branching (§5). The liveness check must be Wake's own, and
   the flow must refuse rather than warn.
5. **A resumed or forked session is a full Wake agent**: its permission asks arrive on the same
   `control_request` Wake already answers (§6), and its transcript is discoverable by the same rule
   as any other (§7).

---

## 9. The golden test

`internal/core/fixtures_test.go` globs `testdata/stream/*.jsonl` and fails on any frame that decodes
to `KindUnknown`. The six new fixtures were dropped in flat, alongside the existing corpus, and the
suite was run:

```
go test ./internal/core/ -run 'TestDecodeRecordedFixtures|TestMaxLineBytesClearsTheLongestRecordedFrame|TestFixturesDecodeToTheExpectedKinds' -count=1
ok  	github.com/DilanDoshi/wake/internal/core	0.336s
```

and then the whole package, and then the whole tree:

```
go test ./... -count=1
ok  	github.com/DilanDoshi/wake/cmd/wake	15.386s
ok  	github.com/DilanDoshi/wake/internal/core	3.919s
ok  	github.com/DilanDoshi/wake/internal/daemon	16.497s
ok  	github.com/DilanDoshi/wake/internal/notice	1.279s
ok  	github.com/DilanDoshi/wake/internal/render	1.558s
ok  	github.com/DilanDoshi/wake/internal/rpc	2.261s
ok  	github.com/DilanDoshi/wake/internal/ui	2.724s
```

**Green. Zero `KindUnknown`, and `internal/` was not modified.** The reason is that a fork and a
resume speak no new vocabulary: the 137 new lines use only `system/hook_started`,
`system/hook_response`, `system/init`, `assistant`, `user`, `result`, `rate_limit_event` and
`control_request`, all of which the corpus already contained and the decoder already handles. The
interrupt spike's 29 unknowns came from `command_lifecycle` and `control_response`, neither of which
appears here.

The unknown that was worth being ready for did not materialise, so the airlock does not need
widening and nothing about the decoder is in scope for this change.

---

## 10. What this did not establish

The equivalent section of `2026-08-08-interrupt-findings.md` is why three features were not built on
a guess. Same job here.

- **Forking a session that is currently running.** Every fork here resumed a parent whose process
  had already exited. `v1_goals.md` asks this directly — *"Can you fork a session that is currently
  running in Wake, or must it be stopped first?"* — and it decides whether `wake fork <who>` is a
  live DM action or a roster action on a stopped agent. §5 shows the *parent's file* is what a
  concurrent process corrupts and §7 shows a fork does not write to the parent's file, which makes
  a live fork *look* safe. **That is an inference, not a recording.** Do not build the live-fork
  affordance on it.
- **Forking a fork.** `43824f4f-…` and `6e5e2c6d-…` were never themselves resumed. Whether a
  second-generation fork works, and whether it copies the whole chain, is unrecorded.
- **What a subsequent `--resume` of a branched transcript actually loads.** §5 shows the surviving
  `leafUuid` is the first process's and reasons that the second's turn is orphaned. Nobody resumed
  `3e335d2d-…` afterwards to see. The mechanism by which `--resume` picks a leaf is not established
  at all.
- **Three or more concurrent processes on one id**, and whether the last-writer-wins pattern in §5
  is stable or just what two processes happened to do once.
- **Resuming from a different working directory.** Every process ran in the same cwd, so the session
  was always in the project slug the CLI derives from it. Whether `--resume <id>` finds a session
  recorded under a different directory — which is exactly what session importing's picker will try,
  since it lists sessions across 115 project directories — is **completely unrecorded** and is the
  next thing to measure.
- **`--continue`.** The help text names it beside `--resume` for both `--fork-session` and the
  `--session-id` rejection. Never sent.
- **`--fork-session` without `--resume` or `--continue`.** Help says *"use with --resume or
  --continue"*; whether it is refused, ignored, or does something is untested.
- **Whether a fork inherits anything beyond conversation text** — a pending permission ask, queued
  messages, an interrupted turn, background tasks, `/compact` state, spend counters. §4 tested one
  passphrase. `total_cost_usd` on a fork's first `result` (`0.0201805`) is not the parent's running
  total, but one number is not a finding about accounting.
- **Forking or resuming a session that was interrupted mid-turn**, which is the natural composition
  with `2026-08-08-interrupt-findings.md` and was not attempted.
- **The exit-code shape of a resumed session whose last turn was interrupted.** `ending.go`'s
  `interruptedExit` suppresses exactly one ending; nothing here exercised it on a `--resume` path.
- **Subagents.** `--forward-subagent-text` was passed on every run and `parent_tool_use_id` is
  `null` on every frame — no subagent ran. Whether a fork inherits a subagent's `output_file`
  references, or a running subagent at all, is unknown.
- **Whether `SessionStart:fork` / `:resume` / `:startup` is stable naming.** Three observations, one
  machine, and the frames only exist because two installed plugins register a `SessionStart` hook
  (§3). Nothing may key on it.
- **Any of this without `--permission-prompt-tool stdio`, or with a `--permission-mode` other than
  `manual`.** Wake passes both, so both were held fixed.

---

## 11. Method

Recorded with a throwaway Go driver in a scratch directory outside the repo (deleted with the
scratch; it is not committed). It spawns `/Users/dev/.local/bin/claude` directly — bypassing the
cmux shim first on `PATH`, as §1 of the stream-json note requires — tees stdout to the fixture
byte-for-byte through an `io.TeeReader`, auto-allows any `can_use_tool` by echoing `request.input`
back inside the `allow` frame `encode.go` writes, and drives scripted turns off `result` frames. It
scrubs the same eight `nestedSessionEnv` variables `internal/core/process.go` scrubs, and it names
its process argument list from a transcription of `buildArgs` rather than from a hand-written list.

`cwd` for every run was a scratch directory holding four trivial files (`a.txt`, `b.txt`,
`notes.md`, and later the files the recordings created), so a `Write` had somewhere real to land and
the fixtures carry no Wake source.

The collision recording (§5) is the only one needing two processes: the first was launched
detached with a 90-second idle hold after its first `result`, the second was launched 15 seconds
into that window, and the first was then given a second turn to prove it still worked.

Fixture integrity, checked over all 137 lines before commit: every line parses as JSON; every
`uuid` present is RFC 4122 **version 4**; exactly **one** distinct `session_id` per file; **six**
distinct pids in `init.messaging_socket_path` (`/tmp/cc-socks/<pid>.sock` — 90679, 91256, 92046,
92763, 93163, 93417), confirming six separate processes; `init.apiKeySource` is `"none"` on every
one; and no credential-shaped strings (`sk-ant-`, `ghp_`, `AKIA`, `xox…`, bearer tokens) appear
anywhere. The fixtures do contain this machine's absolute paths and the user's installed
skill/plugin/MCP inventory, which is expected for a recording and matches every fixture already in
the corpus.

Which scenario produced which fixture:

| Fixture | Identity flags | Turns |
|---|---|---|
| `resume-fork-parent` | `--session-id <P>` | remember `cobalt-mango-7731`; then `Write parent.txt` |
| `resume-fork-both-flags` | `--resume <P> --fork-session --session-id <F>` | recall the passphrase |
| `resume-fork-no-session-id` | `--resume <P> --fork-session` | recall the passphrase; then `Write fork.txt` |
| `resume-session-id-without-fork` | `--resume <P> --session-id <N>` | none reached — refused at startup |
| `resume-plain-permission` | `--resume <P>` | recall the passphrase; then `Write resumed.txt` |
| `resume-collide-first` | `--session-id <L>` | remember `silver-heron-9042`; hold 90 s idle; then reply `ALIVE` |
| `resume-collide-second` | `--resume <L>`, 15 s into the hold | recall the passphrase |
