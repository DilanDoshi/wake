# Forking a session that is still running — what `claude` actually does

Recorded 2026-08-10 against **Claude Code 2.1.226**, macOS 15 (arm64), model `claude-opus-5[1m]`.

This extends `2026-08-09-resume-fork-findings.md` and closes three of the items in its §10. That
note said of the live fork, in as many words:

> §5 shows the *parent's file* is what a concurrent process corrupts and §7 shows a fork does not
> write to the parent's file, which makes a live fork *look* safe. **That is an inference, not a
> recording. Do not build the live-fork affordance on it.**

It is now a recording. So is forking a fork, and so is `--fork-session` with no `--resume`.

Every **wire shape** here was observed on the real stdout of a real `claude` process. Four stdout
fixtures, **73 lines**, **four distinct processes** (four distinct pids in
`init.messaging_socket_path`). Nothing is hand-written. Where a quoted line is shortened the
elision is marked inline with `...ELIDED...`; every other quoted line is byte-for-byte what came
out of the pipe.

Two kinds of claim here are **not** stdout observations, and each is labelled where it appears:

- **`[disk]`** — the contents of `~/.claude/projects/<slug>/<uuid>.jsonl`. Q1's third question is
  a question about the filesystem, so there is no stdout answer to it. These files are **not**
  committed and are **not** the same format as the stream; they are quoted only where the bytes
  decide something. Snapshots were taken by the driver at named moments and compared by sha256.
- **`[testimony]`** — process exit codes and stderr byte counts, observed via `cmd.Wait()` and a
  `stat` at recording time; **and every prompt Wake wrote to stdin.** Neither is a byte on any
  committed stream, so there is nothing to commit.

  The prompts deserve the label rather than a footnote. Nothing passes `--replay-user-messages`,
  so **no user turn Wake sent appears on any of these streams** — the only `user` frame in all 73
  lines is `live-fork-child.jsonl:26`, and its content is a `tool_result`. Everything below of the
  form *"the fork was asked X"* is therefore driver testimony, and where that matters to a finding
  it is called out in place. §7 is where it matters most.

Anything neither observed nor backed by a fixture is in §12.

| File | Lines | Identity flags | What it proves |
|---|---:|---|---|
| `live-fork-parent.jsonl` | 19 | `--session-id <P>` | the parent, **held open and idle across the whole life of a fork taken from it**, then given another turn that worked |
| `live-fork-child.jsonl` | 32 | `--resume <P> --fork-session --session-id <F>` | **the answer to Q1**: the fork starts, inherits the live parent's conversation, and raises a real `can_use_tool` |
| `fork-of-fork.jsonl` | 10 | `--resume <F> --fork-session --session-id <G>` | **the answer to Q2**: a second-generation fork, carrying *both* passphrases |
| `fork-session-no-resume.jsonl` | 12 | `--fork-session --session-id <N>` | **the answer to Q3**: accepted, exit 0, zero stderr — and `SessionStart:startup`, so the flag did nothing |

There is deliberately **no `.stderr.txt` in this set**. Every one of the four processes wrote zero
bytes to stderr `[testimony]`, and the precedent from the previous spike is that an empty stderr
file is not committed. Q3 was the run that might have produced one; it did not, and that absence
is the finding.

Four ids recur and are worth having in front of you:

```
P = 2ca1b3a0-0319-4eb4-904c-9c6563985736   the parent, alive throughout
F = 37f6e67d-99f1-4d58-8309-be14ef0693ae   forked from P while P was running
G = 94a6165c-29be-43f8-95c2-ccedde5c722a   forked from F
N = 634d4233-2efc-46bd-a2c7-2c8b83300f3d   --fork-session with nothing to fork
```

and two passphrases. **Which session each was told to is `[testimony]`** — the prompts are not on
any stream — so the committed occurrence counts are given beside them, because those are what a
reader can actually check:

```
amber-lynx-4417   [testimony] told only to P, in P's first turn
                  appears in live-fork-parent (×2), live-fork-child (×2), fork-of-fork (×2)
teal-otter-8823   [testimony] told only to F, in F's second turn - after F forked away from P
                  appears in fork-of-fork (×2) and NOWHERE ELSE - not once in live-fork-child
```

That second line is the one to keep in view. `teal-otter-8823` is **absent from `live-fork-child.jsonl`**,
because the only place it could have appeared is the prompt that introduced it, and prompts are not
echoed. §7 states plainly what that does and does not let a reader conclude.

---

## 1. The invocation

`session.go`'s `buildArgs`, byte for byte, with only the identity flags varying. Nothing was
dropped: all eight fixed flags including `--verbose` and `--permission-prompt-tool stdio`, and
`--name` / `--permission-mode` in the positions `buildArgs` puts them:

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

`--permission-mode manual` throughout, so a `can_use_tool` would actually be raised. `init`
normalizes it to `"default"` on all six `init` frames, as the stream-json note's trap says.

**The identity flags sit exactly where `--session-id` sits in `buildArgs`**, and the fork's are the
three the previous spike settled on: `--resume <parent> --fork-session --session-id <new>`. A
recording made with a different flag set is not evidence about Wake.

---

## 2. Q1 — the fork starts, and it starts while the parent is running

The parent took one turn (learning `amber-lynx-4417`), then **held stdin open and idle without
exiting**. The fork was launched into that window and ran to completion inside it.

`live-fork-child.jsonl:1` — the first byte off the fork's stdout, carrying the id Wake chose:

```json
{"type":"system","subtype":"hook_started","hook_id":"8aec5676-3017-4043-a358-eb516c5d4e39","hook_name":"SessionStart:fork","hook_event":"SessionStart","uuid":"a0802a7f-bf31-460a-85e9-4d3ddd6f5539","session_id":"37f6e67d-99f1-4d58-8309-be14ef0693ae"}
```

**31 of the 32 lines carry `37f6e67d-…`**, and the parent's `2ca1b3a0-…` appears **nowhere** in
the file. The one line without a `session_id` is `:17`, the `can_use_tool` — which carries none at
all, exactly as CLAUDE.md's standing trap says (*"A permission request also carries no
`session_id` — correlate on `request_id`"*). That is worth naming rather than rounding away: an
"every line carries the id" claim would be false here, and false in the one place a client has to
correlate differently.

`[testimony]` exit 0, zero bytes on stderr.

### The parent was one process throughout, and the bytes say so

This is the claim the whole note rests on, so it needs to survive the obvious attack: *how do you
know the parent did not exit and get restarted?* Timestamps cannot answer that, and neither can
the shape of the capture file — **adjacency of `:12` and `:13` is a property of the file, not of a
process**, and a driver that teed a restarted process into the same handle would produce the very
same adjacency. (The predecessor spike used two *separate* files for its collision recording for
exactly this reason.)

Two discriminators in the committed bytes settle it, and neither is about time.

**1. The socket path is per-process, and it is the same on both of the parent's turns.**

```
live-fork-parent.jsonl:5    init   "messaging_socket_path":"/tmp/cc-socks/28739.sock"   (turn 1)
live-fork-parent.jsonl:13   init   "messaging_socket_path":"/tmp/cc-socks/28739.sock"   (turn 2)
```

That the value is per-*process* and not per-*session* is proved by the committed corpus, not
assumed: four session ids in `testdata/stream` appear under two sockets each, because each was
recorded twice by two processes. The clearest pair is the predecessor's parent —

```
resume-fork-parent.jsonl        session 24a6bea1-…   /tmp/cc-socks/90679.sock
resume-plain-permission.jsonl   session 24a6bea1-…   /tmp/cc-socks/92763.sock
```

— same session, different process, different socket. (`resume-park` / `resume-wake`,
`resume-collide-first` / `-second`, and `interrupt-resume-after` / `interrupt-then-close` are the
other three.) So one process emitted both of the parent's turns; a process's lifetime is
contiguous; therefore the parent existed continuously from `:5` to `:13`, and the fork's frames lie
inside that interval.

**Pid reuse cannot manufacture this either.** The four processes were allocated 28280, 28739,
28934 and 29593, monotonically, over the 82 seconds of the spike. A restarted parent at ~16:54:29
would fall between the fork's 28934 and the second fork's 29593 and would have been given a pid in
that range — not 28739 again.

**2. A restarted parent would have had to announce itself, and there is nothing there.**

Every process in this corpus leads with a `SessionStart:*` `hook_started`/`hook_response` group
before its first `init`. The parent's is at `:1`–`:4`. Before its turn-2 `init` at `:13` there is
**no `SessionStart` frame of any kind** — the only hook frames between are the `Stop` pair at
`:8`–`:11`:

```
live-fork-parent.jsonl:1-4    SessionStart:startup   hook_started ×2, hook_response ×2
live-fork-parent.jsonl:5      init                   28739.sock
live-fork-parent.jsonl:8-11   Stop                   hook_started ×2, hook_response ×2
live-fork-parent.jsonl:13     init                   28739.sock      <- no SessionStart before it
```

And there is no third way to start one. A restarted parent would have to be spawned `--session-id`
(refused, the id is in use), `--resume` (`SessionStart:resume`) or `--fork-session`
(`SessionStart:fork`) — every one of which puts a `SessionStart` group ahead of its `init`, on the
evidence of all ten fork/resume fixtures in the corpus.

**What does not prove it, and is deleted from this note:** that the parent answered another turn
afterwards. A process that died and was `--resume`d answers a later turn perfectly well — this
project recorded exactly that in `resume-park.jsonl` / `resume-wake.jsonl`. The later turn shows
the *session* survived; only the two discriminators above show the *process* did.

`[testimony]` corroborating, and only corroborating: one `cmd.Wait()` per process, called once at
the end. The parent was started at 09:54:07.952 local and reaped at 09:54:30.802 with exit 0; the
fork was started at 09:54:16.940 and reaped at 09:54:27.997. Every timestamped frame in the fork
falls strictly between the parent's two:

```
live-fork-parent.jsonl:6    assistant   2026-08-10T16:54:11.713Z    (parent's turn 1 - "OK")
live-fork-child.jsonl:4     assistant   2026-08-10T16:54:21.541Z
live-fork-child.jsonl:12    assistant   2026-08-10T16:54:26.377Z
live-fork-child.jsonl:26    user        2026-08-10T16:54:26.475Z
live-fork-child.jsonl:27    assistant   2026-08-10T16:54:27.441Z
live-fork-parent.jsonl:14   assistant   2026-08-10T16:54:30.357Z    (parent's turn 2 - "ALIVE …")
```

The timestamps place the fork inside the interval. The socket path and the missing `SessionStart`
are what make the interval one process.

---

## 3. Q1 — the fork inherits the *live* parent's conversation

`amber-lynx-4417` existed only inside `2ca1b3a0-…`. The fork was a separate process with no other
context, and `[testimony]` it was asked *"What passphrase did I ask you to remember earlier in
this conversation?"* — the prompt is not on the stream, but the answer is, and the answer is a
string the fork's own process was never given.

`live-fork-child.jsonl:4`:

```json
{"type":"assistant","message":{"model":"claude-opus-5","id":"msg_011CduRAmbYxTKDn39EoGwQn","type":"message","role":"assistant","content":[{"type":"text","text":"amber-lynx-4417"}],...ELIDED...,"usage":{"input_tokens":2,"cache_creation_input_tokens":896,"cache_read_input_tokens":37803,...ELIDED...},...ELIDED...},"parent_tool_use_id":null,"session_id":"37f6e67d-99f1-4d58-8309-be14ef0693ae","uuid":"04a1fe92-e402-47d3-b98f-2bdcee81b447","timestamp":"2026-08-10T16:54:21.541Z","request_id":"req_011CduRAkgjSdvWTUjfVz6Zn"}
```

and the turn end, `live-fork-child.jsonl:10`:

```json
{"is_error":false,...ELIDED...,"session_id":"37f6e67d-99f1-4d58-8309-be14ef0693ae",...ELIDED...,"permission_denials":[],"terminal_reason":"completed",...ELIDED...,"subtype":"success","api_error_status":null,"result":"amber-lynx-4417",...ELIDED...}
```

The token counters corroborate it exactly rather than approximately. The parent's turn 1 built a
prompt of `17845 + 19958 = 37803` cached tokens (`live-fork-parent.jsonl:12`). The fork's first
turn reads **`cache_read_input_tokens: 37803`** — the identical prefix, to the token — for a
one-sentence prompt it could not have built itself.

## 3a. Q1 — and the parent survives, unchanged

After the fork had exited, the parent was given one more turn.
`live-fork-parent.jsonl:14`:

```json
{"type":"assistant","message":{"model":"claude-opus-5","id":"msg_011CduRBNqN1XyNyjUeMocU5","type":"message","role":"assistant","content":[{"type":"text","text":"ALIVE amber-lynx-4417"}],...ELIDED...,"usage":{"input_tokens":2,"cache_creation_input_tokens":53,"cache_read_input_tokens":37803,...ELIDED...},...ELIDED...},"parent_tool_use_id":null,"session_id":"2ca1b3a0-0319-4eb4-904c-9c6563985736","uuid":"1489bf96-6bb8-4f1e-b5a1-a1f6537f2e6a","timestamp":"2026-08-10T16:54:30.357Z","request_id":"req_011CduRBMzFvTUArMYgWGqhU"}
```

and `live-fork-parent.jsonl:19`:

```json
{"is_error":false,"duration_api_ms":4042,"num_turns":1,"stop_reason":"end_turn","session_id":"2ca1b3a0-0319-4eb4-904c-9c6563985736",...ELIDED...,"permission_denials":[],"terminal_reason":"completed",...ELIDED...,"subtype":"success","api_error_status":null,"result":"ALIVE amber-lynx-4417",...ELIDED...}
```

`[testimony]` exit 0, zero bytes on stderr.

**The isolation is on the counters, not only in the wording of the reply.** The parent's turn 2
reads `cache_read_input_tokens: 37803` — the same 37803 it built in turn 1, and the same 37803 the
fork read. The fork's two turns created 896 and 576 tokens of new prefix on top of it, and **none
of that reached the parent**: had the parent absorbed any of the fork's work its cached prefix
would have grown, and it did not move by a single token. It added only its own 53.

The reply itself carries two facts on purpose. `ALIVE` says the process still takes work;
`amber-lynx-4417` says its **own** conversation is intact — it still knows the thing it was told
before anything forked away from it. A fork did not take the history with it and did not disturb
it.

---

## 4. Q1 — a fork of a live parent is a full Wake agent

The fork's second turn provoked a real tool call against a real file.
`live-fork-child.jsonl:17`, whole line:

```json
{"type":"control_request","request_id":"410e53db-3ccc-45c6-aed9-ebb19e1bbd62","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"/private/tmp/claude-501/-Users-dev-Documents/b428db43-0dfa-4bc9-8a0e-0610222aa668/scratchpad/lf/cwd/live-fork.txt","content":"ok\n"},"description":"live-fork.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_01RQSkLNnaLj7Kseju9BhEko"}}
```

The driver answered with the `allow` frame `encode.go` writes, the tool ran, `live-fork.txt`
exists on disk, and the turn ended normally — `live-fork-child.jsonl:27`
`"content":[{"type":"text","text":"OK"}]`, `:32` `"result":"OK"`, `"permission_denials":[]`.

It carries **no** `requires_user_interaction` and its `input` holds only `file_path` and
`content` — no `questions` — so `core.askKind` classifies it `AskPermission`, the same as every
other ordinary ask in the corpus. That is asserted rather than asserted-about: see §11.

This is the run that would have established nothing if the prompt had been answerable without a
tool. The `tool_use_id` and the file on disk are what make it a tool call rather than a claim
about one.

---

## 5. Q1 `[disk]` — the parent's transcript file is **byte-identical** across the fork

This is the question §10 of the previous note refused to answer by inference, and it is the one
that decides whether `wake fork` can be a live action.

The parent's `~/.claude/projects/<slug>/2ca1b3a0-….jsonl` was snapshotted immediately before the
fork process was started and again immediately after it was reaped:

```
P.before-fork   53346 bytes   15 lines   sha256 878d5b595b50b8d90ac91b2393440f60ae876be078d726fc82230b0d89cd4fb7
P.after-fork    53346 bytes   15 lines   sha256 878d5b595b50b8d90ac91b2393440f60ae876be078d726fc82230b0d89cd4fb7
```

`cmp` reports no difference. **Not one byte of the parent's file changed while a fork was taken
from it.**

Those hashes are committed, in `testdata/stream/live-fork-parent.transcript.sha256`. The
transcripts themselves are `~/.claude/projects` state and are not this project's to commit, so
without that file this — the strongest single claim in the note — would be a sentence nobody could
ever re-check.

The `last-prompt` pointer — the thing §5 of the previous note watched get rewritten twice by two
processes sharing an id — is also untouched. Line 12 of the parent's file, identical in both
snapshots:

```
P.before-fork:12   last-prompt  leafUuid=9c37fe76-627c-48f7-b16c-70dc7f952271  sessionId=2ca1b3a0-…
P.after-fork:12    last-prompt  leafUuid=9c37fe76-627c-48f7-b16c-70dc7f952271  sessionId=2ca1b3a0-…
```

The file did eventually grow — but only from the **parent's own** later turn, and only by
appending. `P.final` is 21 lines, 56973 bytes; its first 15 lines are the 15 that were there
before the fork, and the six new ones are the parent's turn 2, chained onto the leaf the parent
already owned:

```
P.final:15  assistant  uuid=667300b1…  parentUuid=9c37fe76…   (the pre-fork leaf)
P.final:18  system     uuid=321c48ce…  parentUuid=667300b1…
P.final:19  user       uuid=092e373d…  parentUuid=321c48ce…
P.final:20  assistant  uuid=1489bf96…  parentUuid=092e373d…   ("ALIVE amber-lynx-4417")
P.final:21  last-prompt              leafUuid=1489bf96…
```

There is no branch, no second child of `667300b1`, no out-of-order flush. Contrast §5 of the
previous note, where two processes on the *same* id gave one assistant turn two children and the
loser's turn went silently off the main line.

The fork's own file appeared beside the parent's, named for the id Wake chose:

```
53346  2ca1b3a0-….jsonl   (P, unchanged across the fork)
69064  37f6e67d-….jsonl   (F, new, 31 lines)
```

**So the parent's file did not change while a fork was taken from it, and that is now measured
rather than inferred** — which is the whole of what the previous note's §10 asked for.

Read it as the measurement it is, not as a guarantee. Two before/after pairs (this one and §8's),
compared by sha256 at one moment each, on one machine, one CLI version. **"It did not change" is
what the bytes support; "it cannot change" is not**, and §12 says so again where it belongs.

---

## 6. Q1 — nothing on either stream says a live parent was forked

`live-fork-parent.jsonl` emitted **nothing at all** while the fork existed. Its turn-1 `result`
(`:12`) and its turn-2 `init` (`:13`) are consecutive lines; there is no frame in between.
Searched over the whole file, the fork's id `37f6e67d-…` appears on **zero** lines and the
substring `fork` appears on **zero** lines.

The other direction is the same story the previous note's §3 told about a dead parent. In
`live-fork-child.jsonl` the parent's id `2ca1b3a0-…` appears on **zero** lines. The 10,298-byte
`init` at `:3` has 23 top-level keys —

```
agents, analytics_disabled, apiKeySource, capabilities, claude_code_version, cwd,
fast_mode_disabled_reason, fast_mode_state, mcp_servers, memory_paths,
messaging_socket_path, model, output_style, permissionMode, plugins,
product_feedback_disabled, session_id, skills, slash_commands, subtype, tools, type, uuid
```

— and not one of them names an ancestor. The only `fork` substrings anywhere in the file are the
`SessionStart:fork` hook name (`:1`, `:2`) and the filename `live-fork.txt` that the recording
itself chose; the only `original` is `originalFile` inside the `Write` hook's payload.

**Wake must record the parent→fork edge itself, and this is true for a live parent exactly as it
was for a dead one.** Nothing on either side of the fork will tell it later.

The `SessionStart:fork` / `SessionStart:startup` discriminator reproduced (§2, §9), and it is still
disqualified — but **the reason is sharper than the previous note's, and these recordings are what
sharpen it.** That note said the frames exist only because `--include-hook-events` is passed *and*
two installed plugins register a `SessionStart` hook, so an uninstall removes the discriminator.
True, and not the whole of it: **the two plugins do not both run on a fork.**

```
live-fork-parent.jsonl          SessionStart:startup   hook_started ×2   (one response 346 B, one 7,636 B)
fork-session-no-resume.jsonl    SessionStart:startup   hook_started ×2
live-fork-child.jsonl           SessionStart:fork      hook_started ×1   (the 346 B one only)
fork-of-fork.jsonl              SessionStart:fork      hook_started ×1
resume-plain-permission.jsonl   SessionStart:resume    hook_started ×1
```

So the frame's *cardinality* varies with the start mode as well as with the plugin inventory: a
startup fires both hooks, a fork and a resume fire only the small one — the 7,636-byte
context-injecting response never appears on a forked or resumed process at all. A discriminator
whose presence depends on which plugins are installed **and** whose count depends on which of them
a given start mode happens to run is not a protocol from either direction. **Nothing may key on
it**, and that conclusion is now over-determined rather than merely prudent.

---

## 7. Q2 — a fork of a fork works, and carries the whole chain

`F` was forked from a live `P` and then, `[testimony]` in its own second turn, told a **second**
passphrase `teal-otter-8823` that `P` has never seen and never will. `G` was then forked from `F`
and asked for both.

`fork-of-fork.jsonl:1`, first byte off stdout:

```json
{"type":"system","subtype":"hook_started","hook_id":"763eb753-3a7e-4c03-84c3-0815fbeaccbe","hook_name":"SessionStart:fork","hook_event":"SessionStart","uuid":"0fbb0b8e-bd69-412d-8568-2c9822d2fc47","session_id":"94a6165c-29be-43f8-95c2-ccedde5c722a"}
```

`fork-of-fork.jsonl:4`:

```json
{"type":"assistant","message":{"model":"claude-opus-5","id":"msg_011CduRDddAHeu5NjEZfWyDx","type":"message","role":"assistant","content":[{"type":"text","text":"amber-lynx-4417 teal-otter-8823"}],...ELIDED...,"usage":{"input_tokens":2,"cache_creation_input_tokens":65,"cache_read_input_tokens":39275,...ELIDED...},...ELIDED...},"parent_tool_use_id":null,"session_id":"94a6165c-29be-43f8-95c2-ccedde5c722a","uuid":"25d6cfab-4e1f-49bc-8cdc-b4823d80375c","timestamp":"2026-08-10T16:55:00.212Z","request_id":"req_011CduRDbkYqU3Nvh2o7ferz"}
```

and `fork-of-fork.jsonl:10`:

```json
{"is_error":false,...ELIDED...,"session_id":"94a6165c-29be-43f8-95c2-ccedde5c722a",...ELIDED...,"subtype":"success","api_error_status":null,"result":"amber-lynx-4417 teal-otter-8823",...ELIDED...}
```

**Both passphrases, in order, under a third session id.** The second one is what makes this more
than a repeat of §4 of the previous note: had `G` inherited only `P`'s history it could have
produced `amber-lynx-4417` and nothing else. It carries the generation introduced *after* the
first fork.

### What the stream cannot rule out here, and where the weight actually sits

**`teal-otter-8823` appears nowhere in `live-fork-child.jsonl`** — zero occurrences in 32 lines —
and it appears twice in `fork-of-fork.jsonl`. That is not an anomaly, it is the
`--replay-user-messages` consequence: the only frame that ever held that string in `F` was the
prompt introducing it, and prompts are not echoed. So from the committed stdout alone **a reader
cannot exclude that `G` was simply told `teal-otter-8823` in its own prompt.** That it was told
only to `F`, one process and one fork earlier, is `[testimony]`.

The claim does not rest on the testimony, though, and this is why the `[disk]` section matters
rather than merely corroborating. §8 shows `G`'s transcript containing `F`'s own turn-2 messages
under **`F`'s uuids** — `f92167cb`, `cf0054f5`, `08d1d8ea`, `f6b24374`, `9337dfe4`, `c0f6818b` —
messages that exist in `F`'s file and were written there before `G`'s process started. `G` did not
merely produce the right string; it inherited the message chain that string was introduced into.
The `[disk]` evidence carries Q2, and the stream corroborates it.

`[testimony]` exit 0, zero bytes on stderr. All 10 lines carry `94a6165c-…`.

---

## 8. Q2 `[disk]` — two generations, one preserved `uuid` chain, and a lineage frozen at fork time

`F`'s file was snapshotted before and after `G` was taken from it:

```
F.before-second-fork   69064 bytes   31 lines   sha256 bba4575083322aae5a8668ddae72a81818d60b6310668026e25ff0c24285daa6
F.after-second-fork    69064 bytes   31 lines   sha256 bba4575083322aae5a8668ddae72a81818d60b6310668026e25ff0c24285daa6
```

Byte-identical, and committed alongside the parent's pair in
`testdata/stream/live-fork-parent.transcript.sha256`. **A fork did not write to its parent's file
at either generation.**

`G`'s file is a copy of `P`'s history *and* `F`'s, rewritten with `G`'s `sessionId`, with every
per-message `uuid` preserved across **both** hops. The same messages, three files, three session
ids, one uuid each:

```
P:5   attachment  uuid=d227fb45…  sessionId=2ca1b3a0-…
F:6   attachment  uuid=d227fb45…  sessionId=37f6e67d-…
G:6   attachment  uuid=d227fb45…  sessionId=94a6165c-…

P:15  assistant   uuid=667300b1…  sessionId=2ca1b3a0-…      (P's turn 1 - "OK")
F:13  assistant   uuid=667300b1…  sessionId=37f6e67d-…
G:13  assistant   uuid=667300b1…  sessionId=94a6165c-…

F:30  assistant   uuid=c0f6818b…  sessionId=37f6e67d-…      (F's own turn 2 - never in P)
G:23  assistant   uuid=c0f6818b…  sessionId=94a6165c-…
```

So `uuid` is stable across a fork **and across a fork of a fork**, while `sessionId` is not. If
Wake ever needs to prove two transcripts share an ancestor it has a key to do it with at any
depth — but that is an inference about intent from three files, not something the CLI states, and
§6 established the stream never mentions an ancestor at all.

**And a fork's lineage is frozen at the moment it forked.** `G` was taken at 16:54:56Z, twenty-six
seconds *after* `P` finished its turn 2 at 16:54:30Z — and `P`'s turn 2 is in neither `F` nor `G`.
The uuids `321c48ce`, `092e373d` and `1489bf96` exist only in `P`'s file.

State that precisely, because the tempting phrasing overreaches. What was recorded is that **a
parent's later turn does not appear in a fork's transcript**, `F`'s and `G`'s both being closed
files by the time anyone looked. It is *not* a recording about a live fork failing to see a live
parent's new work: `F`'s process had already exited when `P` took its turn 2, and `G`'s had not
yet started. **Nothing in this spike was running at the same moment as a parent turn**, so "a fork
is not a live subscription" is the one phrasing these bytes cannot reach. §12 carries it as an
open item, and it is the composition a live DM fork produces on its very next keystroke.

The final state of the project directory, four transcripts, each carrying only its own lineage:

```
56973  2ca1b3a0-….jsonl   P   21 lines
69064  37f6e67d-….jsonl   F   31 lines
53460  634d4233-….jsonl   N   the Q3 run
69292  94a6165c-….jsonl   G   30 lines
```

---

## 9. Q3 — `--fork-session` without `--resume` is **accepted, and does nothing**

The help text says *"When resuming, create a new session ID instead of reusing the original (use
with --resume or --continue)"*. The three possibilities were refused, ignored, or something else.
It is **ignored**.

`fork-session-no-resume.jsonl:1`, first byte off stdout:

```json
{"type":"system","subtype":"hook_started","hook_id":"2410ccbc-7178-43d6-ac45-189c1cc9c99f","hook_name":"SessionStart:startup","hook_event":"SessionStart","uuid":"287510ce-988c-4605-a0ab-673272734924","session_id":"634d4233-2efc-46bd-a2c7-2c8b83300f3d"}
```

`SessionStart:**startup**`, not `:fork` — the same hook name a plain `--session-id` spawn produces
(`live-fork-parent.jsonl:1`). The turn ran normally, `fork-session-no-resume.jsonl:12`:

```json
{"is_error":false,...ELIDED...,"session_id":"634d4233-2efc-46bd-a2c7-2c8b83300f3d",...ELIDED...,"usage":{"input_tokens":2,"cache_creation_input_tokens":20713,"cache_read_input_tokens":17071,...ELIDED...},...ELIDED...,"subtype":"success","api_error_status":null,"result":"OK",...ELIDED...}
```

`[testimony]` exit 0, **zero bytes on stderr** — which is why there is no
`fork-session-no-resume.stderr.txt`. All 12 lines carry `634d4233-…`.

`[disk]` And `N`'s transcript is an ordinary fresh root, measured the same way §5 and §8 measure
theirs rather than argued from token counts: 16 lines, 53460 bytes, its first message
`uuid=1bb1d6cc…` with `parentUuid: None`, **zero** of `P`'s eight message uuids anywhere in it, and
zero occurrences of `amber-lynx-4417`.

Two honesty notes on that, because it is weaker evidence than it looks:

- **The cache counters settle nothing here**, and an earlier draft of this section wrongly implied
  they did. `N`'s prompt totals `2 + 20713 + 17071 = 37786` tokens against the fork's
  `2 + 896 + 37803 = 38701` — within 2.4% of each other. Both numbers are dominated by the system
  prompt and this machine's plugin inventory, not by history, so "cold cache" is not a readable
  signal. The uuid chain is the evidence; the tokens are not.
- **`N` ran before `P` existed** (09:53:38 against 09:54:07), into an empty project directory. So
  the measurement shows `N` *is* a fresh root; it does **not** show that `--fork-session
  --session-id <fresh>` would decline to inherit had there been anything to inherit. That
  distinction is a §12 item.

**This completes the flag matrix.** Every row cites the recording it comes from, so a reader can
tell which were measured here, which in the two earlier spikes, and which by their stderr alone:

| argv | Result | Recorded in |
|---|---|---|
| `--session-id <new>` | fresh session, Wake's id | `live-fork-parent.jsonl`, `resume-fork-parent.jsonl` |
| `--session-id <in use>` | `Error: Session ID … is already in use.`, exit 1, no stdout | `session-id-conflict.stderr.txt` (2026-08-08) |
| `--resume <id>` | resumes, **reuses** `<id>` | `resume-plain-permission.jsonl` (2026-08-09) |
| `--resume <id> --session-id <new>` | **refused**, exit 1, no stdout | `resume-session-id-without-fork.stderr.txt` (2026-08-09) |
| `--resume <id> --fork-session` | forks, CLI **mints** the id | `resume-fork-no-session-id.jsonl` (2026-08-09) |
| `--resume <id> --fork-session --session-id <new>` | forks, **Wake's** id | `resume-fork-both-flags.jsonl` (2026-08-09), `live-fork-child.jsonl`, `fork-of-fork.jsonl` |
| **`--fork-session --session-id <new>`** | **fresh session, Wake's id — `--fork-session` is a no-op** | `fork-session-no-resume.jsonl` |

What the last row does **not** say: that `--fork-session` "relaxes" the `--session-id` rejection.
There was no rejection to relax. `634d4233-…` was a **fresh** id — it appears in no other fixture
in the corpus — and a fresh `--session-id` is accepted on its own with no other flag, per row one.
So what this row shows is narrower and more precise: **`--fork-session` is a no-op on an argv that
was already legal.** Whether it is *also* a no-op on an argv that would otherwise be refused —
`--fork-session --session-id <an id that already has a transcript>` — is **not recorded**, and it
is in §12 because it decides whether the danger below is silent or a loud exit 1.

The danger, stated with the right id named. A stray `--fork-session` left on an argv by a code
path that meant to resume and did not will not be caught by the CLI: the process starts a
brand-new empty session. Note which id that lands on — in `--resume <parent> --fork-session
--session-id <new>` the session being continued rides on **`--resume`**, so dropping the `--resume`
leaves `--session-id` naming the **new** id, the one that was meant to *receive* the fork. The
operator gets an empty agent under the fork's id and an untouched parent, rather than a clobbered
parent. Still a silent wrong answer rather than a startup failure, and still the failure mode a
fork implementation is most likely to ship — just not the catastrophic one an earlier draft of
this paragraph described.

---

## 10. What this settles for Wake

Not built here — this is a recording spike and `internal/` holds no new non-test code — but these
are the constraints the fork implementation inherits, on top of the five in the previous note's §8.

1. **`wake fork <who>` can be a live DM action against an idle agent.** The parent's process is
   unharmed (§3a), its transcript file did not change across the fork (§5), and the fork inherits
   the conversation as of the moment it was taken (§3). **The parent does not have to be stopped or
   parked** — but read the scope, because this constraint is exactly as wide as the recording and
   no wider: **the parent here was idle, with its turn finished and flushed.** The driver held it
   on a channel after its turn-1 `result` specifically so that it would be. Forking a parent that
   is mid-turn is **not** covered by this and is the first bullet of §12. If the implementation
   offers the action on a busy agent, that is a new recording, not an extension of this one.
2. **A fork is a snapshot, and the UI must not imply otherwise.** A parent's later turn does not
   appear in a fork's transcript, at either generation (§8). Note the recorded scope again: nothing
   here was running at the same moment as a parent turn, so "the two never see each other again
   while both are live" is not established (§12).
3. **Forks compose.** A fork can itself be forked, carries the whole chain, and is discoverable by
   the same `<uuid>.jsonl` rule at each recorded generation (§7, §8), with no new bookkeeping per
   generation. Two generations were recorded; that is not evidence about a depth limit in either
   direction, and the earlier draft's "no depth limit was reached" asserted nothing.
4. **The parent→fork edge is Wake's to store, at every generation.** The stream says nothing, in
   either direction, live or dead (§6). `uuid` stability across forks (§8) is a forensic tool for
   *reconstructing* lineage after the fact, not a substitute for recording it.
5. **`--fork-session` must never be emitted without `--resume`.** It is not refused there; on an
   already-legal argv it is simply a no-op, and the process starts an ordinary empty session under
   the id that was meant to *receive* the fork (§9). The flag is only safe as part of the fixed
   triple `--resume <parent> --fork-session --session-id <new>`, and `buildArgs` should treat that
   triple as one unit rather than three independent appends — which also removes any need to know
   what the CLI does with `--fork-session --session-id <an id that already exists>`, a combination
   nothing has recorded.
6. **Fork remains the safe primitive for session importing**, and now measurably so against a
   session someone still has open — which is exactly the case the previous note's §5 showed
   `--resume` corrupts silently.

---

## 11. The golden test

`internal/core/fixtures_test.go` globs `testdata/stream/*.jsonl` and fails on any frame that
decodes to `KindUnknown`. The four new fixtures were dropped in flat alongside the existing
corpus, which is now **45 `.jsonl` files and 1,676 lines** (1,603 before these 73).

`live-fork-parent.transcript.sha256` sits in the same directory and is deliberately outside that
glob — it is `[disk]` measurement, not a stream, and nothing decodes it. The precedent is the two
`.stderr.txt` files already there.

```
go test ./internal/core/ -count=1
ok  	github.com/DilanDoshi/wake/internal/core	3.738s
```

and the whole tree:

```
go test ./... -count=1
ok  	github.com/DilanDoshi/wake/cmd/wake	15.402s
ok  	github.com/DilanDoshi/wake/internal/core	4.091s
ok  	github.com/DilanDoshi/wake/internal/daemon	16.818s
ok  	github.com/DilanDoshi/wake/internal/mcp	0.956s
ok  	github.com/DilanDoshi/wake/internal/notice	1.122s
ok  	github.com/DilanDoshi/wake/internal/render	0.823s
ok  	github.com/DilanDoshi/wake/internal/rpc	1.739s
ok  	github.com/DilanDoshi/wake/internal/ui	4.853s
```

**Green. Zero `KindUnknown`, and no non-test file under `internal/` was modified.** A live fork
speaks no new vocabulary: the 73 new lines use only `system/hook_started`, `system/hook_response`,
`system/init`, `assistant`, `user`, `result`, `rate_limit_event` and `control_request` — every one
of which the corpus already contained and the decoder already handles. The unknown that was worth
being ready for did not materialise.

### The one test that had to change, and why the number went up rather than out

`internal/core/ask_test.go` holds an **exact** count of each ask class in the corpus, deliberately
exact so that a re-recording has to be looked at. §4's `Write` ask is a tenth ordinary permission
ask, so `AskPermission` moved from 9 to 10.

It was checked, not slackened. `TestEveryRecordedAskIsClassifiedFromItsOwnWireFields` re-derives
every ask's class from the raw line by a path that shares nothing with the decoder, and the new
ask's own wire fields decide it: `request.requires_user_interaction` is **absent** and
`request.input` holds only `file_path` and `content`, with **no `questions` key** (`live-fork-child.jsonl:17`,
quoted whole in §4). Absent-and-no-questions is `AskPermission`, and the test's separate
`explicitFalse` guard still counts zero — the corpus still carries no explicit
`requires_user_interaction: false` anywhere, which is the basis for reading absent as
not-interactive.

Nothing else moved. `recordedWithdrawals` (2) and `recordedInterruptMarkers` (10) are unchanged
because these fixtures carry no `control_cancel_request` and no abort marker, and the
`command_lifecycle` / `control_response` / `control_cancel_request` one-to-one map is unaffected
because these fixtures carry none of those frames at all.

---

## 12. What this did NOT establish

The equivalent section of the previous two notes is why three features were not built on a guess.
Same job here — and the first two items below are the ones a fork implementation will hit first.

- **Forking a parent that is *mid-turn*.** The parent here was idle: it had finished a turn, its
  `result` had arrived, and its transcript had been flushed (`P.before-fork` already held the
  assistant reply the fork went on to inherit). An operator hitting "fork this" while the agent is
  generating, or mid-tool, or blocked on a permission ask, is a **different recording** and it was
  not made. What such a fork inherits — the in-flight turn, nothing, or a truncated prefix — is
  unknown, and so is whether the parent's flush and the fork's read can race.
- **Forking a parent that has *never* completed a turn**, and therefore may have nothing on disk
  to fork from at all.
- **A parent taking a turn while a fork is still *running*.** Everything in this spike was
  serialised: the fork's process had exited before the parent was released to take its turn 2, and
  the second-generation fork had not yet started. So every "the two do not see each other" fact
  here is about a *closed* transcript, and §8's "a fork is not a live subscription" is deliberately
  not claimed. **This is the composition a live DM fork produces on its second keystroke** — fork
  an agent, then keep typing to the original — and it is the single most likely next thing an
  operator does. Unrecorded.
- **Whether the parent notices later.** The parent took exactly one turn after the fork. Whether a
  parent that keeps working for minutes or hours alongside a live fork stays clean is three
  recordings away; one post-fork turn is a smoke test, not a soak.
- **Two live forks of one live parent**, or a fork taken while another fork of the same parent is
  running. Every recording here had at most two `claude` processes alive at once.
- **Forking a session that another process is *also* `--resume`-ing.** §5 of the previous note
  showed that state corrupts the parent's file; whether a fork taken from a transcript that is
  being branched under it copies a coherent prefix is unrecorded and is the nastiest composition
  available.
- **A live fork with the parent's process *stopped mid-fork*** — killing `P` while `F` is starting.
  Nothing here exercised a parent dying at an inconvenient moment.
- **Depth beyond two.** `G` was not itself forked. Two generations preserve `uuid` (§8); nothing
  says the third does, and nothing bounds the depth.
- **`--fork-session` alone, with no `--session-id`.** §9 ran `--fork-session --session-id <N>`.
  Whether the bare flag with neither a resume nor an id behaves the same was not run — one process
  was the budget for Q3.
- **`--fork-session --session-id <an id that already has a transcript>`.** §9's id was **fresh**,
  so that run cannot distinguish "the flag is inert" from "the flag suppresses the `--session-id`
  rejection". This is the row that decides whether §10.5's danger is a silent empty session or a
  loud exit 1 — and it is one cheap process to settle, against an id whose transcript already
  exists.
- **Whether `N` would have declined to inherit had there been anything to inherit.** `N` ran into
  an empty project directory, before `P` existed, so §9's `[disk]` measurement shows only that it
  *is* a fresh root.
- **Whether the one-hook-on-fork / two-hooks-on-startup asymmetry (§6) is stable.** Four
  observations on one machine with one plugin inventory. It is quoted to show the discriminator is
  unusable, which it establishes; it is not a claim about how `SessionStart` fan-out works.
- **`--continue`**, in any combination. The help text names it beside `--resume` for both
  `--fork-session` and the `--session-id` rejection. It has still never been sent.
- **Resuming or forking from a different working directory.** Every process in this spike and the
  last ran in one cwd, so every session was in the project slug the CLI derives from it. Still
  completely unrecorded, and still the next thing session importing needs.
- **What a fork inherits beyond conversation text.** Two passphrases and one `Write` were tested.
  A pending permission ask, queued messages, background tasks, `/compact` state, subagent state and
  spend counters are all untested. `total_cost_usd` on `G`'s first `result` (`0.0207975`) is not
  `F`'s running total, but one number is not a finding about accounting.
- **Subagents.** `--forward-subagent-text` was passed on every run and `parent_tool_use_id` is
  `null` on every frame — no subagent ran anywhere in this spike.
- **What `--resume` of a branched transcript loads.** Carried over unanswered from the previous
  note's §10: nobody has resumed a transcript that two processes branched, and the mechanism by
  which `--resume` picks a leaf is still not established.
- **Whether the parent's file being untouched is a *guarantee* or a *behaviour*.** Two
  fork-vs-parent pairs were compared by sha256, at one moment each, on one machine, one version.
  The claim recorded here is "it did not change", not "it cannot change".
- **Anything without `--permission-prompt-tool stdio`, or with a `--permission-mode` other than
  `manual`.** Wake passes both, so both were held fixed.

---

## 13. Method

Recorded with a throwaway Go driver in a scratch directory outside the repo (deleted with the
scratch; it is not committed), rebuilt to the previous note's §11 recipe. It spawns
`/Users/dev/.local/bin/claude` **directly** — bypassing the cmux shim first on `PATH`, which is
what `which claude` resolves to on this machine and which injects extra hooks — tees stdout to the
fixture byte-for-byte through an `io.TeeReader`, auto-allows any `can_use_tool` by echoing
`request.input` back inside the `allow` frame `encode.go` writes, and drives scripted turns off
`result` frames. It scrubs the same eight `nestedSessionEnv` variables `internal/core/process.go`
scrubs, and it builds its process argument list from a transcription of `buildArgs` rather than
from a hand-written list.

`cwd` for every run was a scratch directory outside the repo holding three trivial files
(`a.txt`, `b.txt`, `notes.md`, plus the `live-fork.txt` §4 created), so a `Write` had somewhere
real to land and the fixtures carry no Wake source. The owner's live fleet — a `wake` daemon and
three idle sessions — was not touched: different cwd, different project slug, freshly minted ids,
and nothing in this spike ran a `wake` verb.

The live fork (§2) is the recording that needed two processes. The parent ran on its own goroutine
and blocked, after its turn-1 `result`, on a channel the orchestrator held — stdin open, nothing
written, the process idling, which is the same technique the previous note's §5 collision used.
The fork was launched into that window and run to completion; the parent's file was snapshotted
immediately before the fork was started and immediately after it was reaped; only then was the
parent released to take its second turn. **The measured hold was 16.2 s** and it strictly contains
the fork's entire 11.1 s life, which §2 re-derives from the committed timestamps alone.

The `[disk]` snapshots are the driver's, taken at those named moments; their hashes and line counts
are committed as `testdata/stream/live-fork-parent.transcript.sha256` so §5, §8 and §9 stay
checkable after the scratch and the project directory are gone.

Which scenario produced which fixture. **The whole "Turns" column is `[testimony]`** — nothing
passes `--replay-user-messages`, so not one of these prompts appears on any committed stream:

| Fixture | Identity flags | Turns `[testimony]` |
|---|---|---|
| `live-fork-parent` | `--session-id <P>` | remember `amber-lynx-4417`; **hold idle 16.2 s while a fork is taken**; then reply `ALIVE` + the passphrase |
| `live-fork-child` | `--resume <P> --fork-session --session-id <F>`, launched 5.1 s into the hold | recall the passphrase; then remember `teal-otter-8823` and `Write live-fork.txt` |
| `fork-of-fork` | `--resume <F> --fork-session --session-id <G>` | recall **both** passphrases |
| `fork-session-no-resume` | `--fork-session --session-id <N>` | reply `OK` |

Fixture integrity, checked over all **73** lines before commit, by a script that walks every line
of every new file:

- every line parses as JSON — **73/73**;
- every `uuid` at any depth is RFC 4122 **version 4** — **72 uuid values, 72 v4, 0 exceptions**;
- exactly **one** distinct `session_id` per file — `live-fork-parent` 1, `live-fork-child` 1,
  `fork-of-fork` 1, `fork-session-no-resume` 1, and each is the id that run's argv asked for;
- **four** distinct pids in `init.messaging_socket_path` (`/tmp/cc-socks/<pid>.sock` — 28280,
  28739, 28934, 29593) across the six `init` frames, confirming **four** separate processes, which
  is exactly the number run;
- `init.apiKeySource` is `"none"` on **6/6** `init` frames;
- **no** credential-shaped strings anywhere — `sk-ant-`, `sk-proj-`, `ghp_`/`gho_`/`ghu_`/`ghs_`/`ghr_`,
  `github_pat_`, `AKIA…`, `ASIA…`, `xox[abposr]-`, `AIza…`, `Bearer <token>`, `Authorization: <token>`,
  `-----BEGIN … PRIVATE KEY-----`.

The fixtures do contain this machine's absolute paths and the user's installed skill / plugin /
MCP inventory, which is expected for a recording and matches every fixture already in the corpus.

`[testimony]` all four processes exited **0** with **zero** bytes on stderr. Total metered spend
across the four, from their own `total_cost_usd` fields: **$0.521**.
