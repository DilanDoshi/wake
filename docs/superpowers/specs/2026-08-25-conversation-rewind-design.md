# Conversation rewind — design

**Status:** design, awaiting owner review. **Scope:** the `esc esc` "rewind" of Claude Code, brought
to a Wake conversation pane — go back to an earlier prompt and continue from there, with the agent
genuinely forgetting everything after it.

**Decisions already taken** (owner, 2026-08-25):

- **Conversation only.** Claude Code's rewind can also restore the working tree; that is explicitly
  **out**. Wake does not own files, and `rewind_files` is a *separate* Claude subtype we never send.
- **Full and correct**, not phased: reopen, reattach and the room all stay right after a rewind. No
  known-wrong states.
- **Trigger is `esc esc`, gated to idle + empty composer.** Wake's invariant is that mashing `esc`
  at a runaway agent always interrupts, so the second `esc` may open the picker **only** when the
  pane's agent is idle *and* the composer is empty. A running agent always eats `esc` as interrupt;
  a non-empty composer still clears the draft (today's behaviour). Consequence: no mid-turn rewind —
  you interrupt first, then rewind.
- **Conversation panes only.** The room (multiplexed group chat) does not initiate a rewind. This
  matches Claude Code's per-conversation model and Wake's "the room is the product, panes are
  substrate" principle.

## 1. The recorded protocol (this is the durable finding)

Verified against Claude Code **2.1.246** by a live probe (raw capture scrubbed into
`testdata/stream/rewind-conversation.jsonl`; the input line into `testdata/input/`). Conversation
rewind is a **`control_request` on the same stdin channel Wake already uses for `interrupt` and
`set_permission_mode`** — no interactive TUI, no transcript manufacturing.

**Request** (Wake → Claude):

```json
{"type":"control_request","request_id":"<id>",
 "request":{"subtype":"rewind_conversation",
            "target_message_uuid":"<user msg to rewind to>",
            "last_seen_user_message_uuid":"<current active-branch tip user msg>",
            "interrupt_if_running":false}}
```

Both UUIDs are **mandatory**. Omitting `last_seen_user_message_uuid` is exactly what returns
`"stale target"` — the handler sets its internal `_e` to null and refuses. `interrupt_if_running` is
optional; we send `false` because the trigger is idle-gated.

**Response** (Claude → Wake), a `control_response` with transport-level `subtype:"success"` carrying
a **nested rewind result**:

```json
{"type":"control_response","response":{"subtype":"success","request_id":"<id>",
 "response":{"rewound":true,"targetMessageUuid":"<uuid>",
             "prefillText":"<the rewound prompt's text>",
             "precedingAssistantUuid":"<the new leaf>","error":null}}}
```

- On failure, `rewound:false` and `error` is one of a fixed set observed/decoded from the 2.1.246
  bundle: `"stale target"`, `"target not found"`, `"unseen later turn"`, `"turn running"`,
  `"prompt pending"`, `"commands queued"`, `"state changed"`, `"delivered poll events in range"`,
  `"failed to persist rewind anchor"`. Transport is still `success`; the verdict is `rewound` +
  `error`, the same "success is not a verdict" shape the permission and interrupt receipts already
  have.
- `prefillText` is the text of the rewound prompt — what Claude Code drops back into the composer to
  re-edit. Wake does the same.

**Two facts that shape everything downstream:**

1. **`session_id` does not change.** The wire emits *only* the receipt — no `init`, no
   `conversation_reset`. Wake never re-keys the session.
2. **On disk the transcript is an append-only tree, and rewind does not delete.** Claude's transcript
   lines carry `uuid`/`parentUuid`; rewind appends a marker

   ```json
   {"type":"last-prompt","rewound":true,"leafUuid":"<precedingAssistantUuid>","lastPrompt":"…","sessionId":"…"}
   ```

   and repoints the *active leaf* to `leafUuid`. The rewound turns stay physically in the file as a
   **dead branch**. Behaviourally verified: after rewinding past a turn that taught the agent "42",
   the agent answered `7`, not `7,42`.

## 2. Why this is architectural, not a keybinding

Wake **reads conversations back from Claude's disk** (`daemon/history.go`, `DecodeTranscriptLine`)
rather than keeping its own — a non-negotiable. Today that reader is **linear**: `History()` keeps a
tail-400 ring in *file order*, and `DecodeTranscriptLine` decodes only `type`/`isSidechain`/
`timestamp` — it drops every frame's own `uuid`/`parentUuid` and discards `last-prompt` markers
(type ≠ user/assistant). So as-is, **reopening a pane after a rewind replays the dead branch** and
resurrects exactly what the user rewound away. The room reader (`ui/roomhistory.go`) has the same
exposure.

Making the transcript reader **tree-aware** — follow `parentUuid` from the active leaf instead of
reading the file top to bottom — is the load-bearing change. Everything else is small.

**Rejected alternatives.** ② *Linear marker-replay* (apply each `last-prompt{rewound}` as a
truncation while streaming): a fragile subset of the tree walk, breaks at the tail-window edge and on
stacked rewinds. ③ *Daemon rewind-ledger* (Wake records rewinds it performed and prunes from its own
state): tempting because Wake holds the receipt, but it **loses to "Wake owns almost no state,"** and
it cannot see a rewind performed in a native session Wake later imports. The transcript on disk is the
single source of truth; we read it.

## 3. Architecture and flow

```
esc esc (idle + empty)  ─────────────────────────────────────┐  internal/ui/escape.go
   │  read active branch from disk (tree-aware history)       │
   ▼                                                          │
rewind Picker of prior prompts  ── pick ──▶ FrameRewind ──────┼─▶ daemon/apply.go
   (internal/ui/picker.go)      target + last_seen            │      a.sess.Rewind(target,lastSeen)
                                                              │        core/session.go → EncodeRewind
                                                              │        → stdin control_request
   ┌──────────────────────────────────────────────────────────      Claude rewinds its own transcript
   ▼                                                                  (session_id stable; writes marker)
receipt: KindRewindReceipt  ◀───────────────────────────────── event stream (airlock decode)
   │  rewound? ── no ──▶ notice(error)
   │  yes:
   ├─ re-read this conversation tree-aware, replace the pane's transcript  (== reopen correctness)
   └─ drop prefillText into the composer
```

The daemon side is a near-copy of `FrameMode`: a client → daemon frame, dispatched in `apply.go`,
calling one new `Session` method, with the receipt returning on the ordinary event stream and **no
reply frame and no retained daemon state** (a rewind is a property of the transcript, not of the
session — same reasoning as mode).

## 4. Components

### 4.1 Core airlock (`internal/core`, the four files only)

- **`EncodeRewind(requestID, targetUUID, lastSeenUUID string) ([]byte, error)`** in `encode.go` —
  mirrors `EncodeSetMode`/`EncodeInterrupt`: refuse an empty `requestID` (an unattributable receipt
  across 15–30 sessions) and empty UUIDs (both are mandatory). Emits the request in §1.
  `interrupt_if_running:false` is a constant field, not a parameter — Wake never rewinds a running
  turn (idle gate), and the recorded corpus has only the `false` shape.
- **Decode the receipt** in `protocol.go`/`event.go`: extend the existing `controlResponseEvent`
  path. The rewind receipt is a `control_response` whose nested `response.response` carries
  `{rewound, targetMessageUuid, prefillText, precedingAssistantUuid, error}`. Surface it as a new
  `EventKind` (`KindRewindReceipt`) with a payload pointer (a `RewindResult`, the same pointer-payload
  shape as `Control *ControlResult`). Correlate on `request_id` like every other receipt.
- **Surface tree identity on the transcript path**, and *only* there. Add `uuid` and `parentUuid`
  to what `DecodeTranscriptLine` carries (a transcript-only decode; the live stream path is
  untouched), and stop discarding the `last-prompt` marker so the reconstructor can see
  `{rewound, leafUuid}`. The live stream decoder gains **nothing** — §4.3 explains why the live path
  needs no UUIDs.

All of this stays inside `protocol.go`/`wire.go`/`vocabulary.go`/`encode.go`; `airlock_test.go`'s
file set is unchanged.

### 4.2 Daemon (`internal/daemon`, `internal/rpc`)

- **`rpc.FrameRewind`** (`wire.go`) carrying `SessionID`, and two UUID fields (reuse/extend `Frame`).
  Documented beside `FrameMode` as "the same control-write path as `FrameSend`/`FrameInterrupt`."
  No `FrameRewindReply` — the receipt is a `control_response` on the event stream, exactly as
  `FrameMode` has none.
- **`apply.go`**: a `case rpc.FrameRewind:` that calls `a.sess.Rewind(target, lastSeen)`. Like
  `FrameMode` it may want to refuse while `a.blockedOnAsk()` (a rewind under an open permission ask
  is unrecorded; refuse with a sentence), and it discards the minted `request_id` for `FrameMode`'s
  reason (the receipt reaches clients on the stream; holding it is state with no reader).
- **`core.Session.Rewind(target, lastSeen)`** mirrors `SetMode`: mint a request id, `EncodeRewind`,
  write to stdin, return.
- **Tree-aware reconstruction** in `history.go` (and the room's reader) — §5.

### 4.3 UI (`internal/ui`)

- **Trigger** (`escape.go`): the second `esc` opens the rewind picker **iff** the pane's agent is
  idle (not running a turn — knowable from the same state the heartbeat/working line uses) **and**
  `composerEmpty()`. Otherwise today's rules stand: non-empty composer → clear-draft (armed);
  running agent → interrupt. This adds **no legend glyph** (still `tea.KeyEsc`, like `esc esc`
  clear-draft); the gated label swaps, the way `escape.go`/`detach.go` already swap labels.
- **The picker** reuses `ui.Picker` (drawn through `cards_blocks.go`'s `optionRow`, dismissed in
  `App.update` on the keys that go to the composer — a `Picker` is not a `Card`, per the existing
  ruling). Its options are the **active-branch user prompts**, newest first, each carrying its
  message `uuid`. They are produced by a fresh tree-aware read at trigger time (cheap, idle,
  user-initiated — not per-frame), which also yields `last_seen` = the newest active-branch user
  uuid. Picking option *k* sends `FrameRewind{target: k.uuid, lastSeen}`.
- **On `KindRewindReceipt`**:
  - `rewound:false` → `internal/notice` with the `error` (e.g. "unseen later turn"). No transcript
    change.
  - `rewound:true` → **re-read this conversation tree-aware and replace the pane's transcript**, then
    put `prefillText` into the composer. This single act *is* the live prune (it drops the dead
    branch that already streamed) and it is identical to what a reopen does, so live and reopen can
    never disagree. Because the wire carries no reset frame, this receipt is the one and only signal;
    it is presentation state, so being late is cosmetic, never a lost turn.

## 5. The reconstruction algorithm (the load-bearing detail)

Given a transcript file (a tree via `parentUuid`, with one or more `last-prompt` markers, possibly
several `rewound:true`), produce the **active branch**: the path root → active-leaf, emitting only
its `user`/`assistant` events, tail-bounded to `historyEvents`/`historyBytes` as today.

Design principle (exact rule pinned by fixtures in implementation):

1. **Index pass** — stream the file once, keeping a compact `uuid → parentUuid` map (two short
   strings per line; bounded and tiny beside the content the current reader already streams) and the
   file-order position of each node. Track the latest `last-prompt` marker's `leafUuid`.
2. **Resolve the active leaf.** The latest `last-prompt` marker names the intended leaf `L` (whether
   a normal turn or a `rewound:true` one). If turns were appended after it, the active leaf is the
   deepest descendant of `L` reached by, at each fork, taking the child **written after** the latest
   rewind marker — "newest branch wins," which is what makes a post-rewind continuation win over the
   dead branch that was written earlier.
3. **Collect** — walk `parentUuid` from the active leaf to the root, reverse, keep the `user`/
   `assistant` nodes, then apply the existing tail bounds.

Memory stays proportional to the *index*, not the content: content is only materialised for nodes on
the active branch within the tail window, exactly as `History()`'s ring does now. `slugOf`-style
purity is preserved — reconstruction reads identity fields, never constructs a path from a slug.

The **room reader** (`ui/roomhistory.go`, feeding `Room.Before`) applies the same active-branch
reconstruction before its existing broadcast/provenance rules run, so a rewound broadcast is gone
from a restored room for the same reason it is gone from the DM.

## 6. Edge cases and rulings

- **`last_seen` and the picker both come from one tree-aware read.** Live `core.Event`s carry no
  UUID and gain none; the read at trigger time (idle) is the authority for both the picker targets
  and `last_seen`. This is why §4.1 adds UUIDs to the *transcript* decoder only.
- **Stacked rewinds.** Rewinding, continuing, then rewinding again is multiple `last-prompt{rewound}`
  markers and multiple dead branches. "Newest branch wins at each fork" (§5) is what keeps this
  correct; it is the case most in need of a dedicated fixture.
- **Rewind that removes a broadcast.** DM correctness is total (re-read). The room's *reopen/reattach*
  correctness is total (tree-aware reader). The room's *live* view of a just-rewound broadcast may
  lag until its next history derivation — a small, bounded cosmetic gap, called out here rather than
  hidden; closing it fully would mean carrying UUIDs on the live path, which §4.1 deliberately avoids.
- **Parked / no process.** The picker is gated to an idle *drawn* pane with a live process; a parked
  session has none, so rewind is simply unavailable there (nothing to send stdin to).
- **Subagents.** A rewind targets the parent conversation's user prompts only; subagent transcripts
  are their own surface and are not rewind targets in v1.
- **Version drift.** The subtype is present in 2.1.246; CLAUDE.md pins 2.1.232 and the corpus
  predates it. Wake re-verifies against the installed binary, and targets current Claude — the
  fixture's own `init` names the version. If an operator's Claude is old enough to lack the subtype,
  the receipt is an `error` and the notice says so; nothing else breaks.

## 7. Testing plan (TDD)

Write the failing test first at every layer; `make ci` (exit code in the PR) plus the two-review rule
(code review + adversarial) gate the PR, per CLAUDE.md.

- **Airlock golden files.** Scrub the probe capture into `testdata/stream/rewind-conversation.jsonl`
  (request + receipt) and add the input line under `testdata/input/`; assert `EncodeRewind` bytes and
  the `KindRewindReceipt` decode against them. `corpus_test.go` requires a sterile `init`, so the
  capture is scrubbed (the probe ran under the real `HOME`; `scripts/scrub-fixtures.py --check` is the
  gate).
- **Reconstruction table tests** (pure, `internal/core` or `daemon`): hand-authored transcript trees —
  no rewind, one rewind, one rewind + continuation, **stacked** rewinds, a rewind with attachments
  between user and assistant — each asserting the exact active-branch event list. These may need 1–2
  more **cheap** probe recordings to capture real stacked-rewind trees; that spend will be confirmed
  before it happens.
- **Daemon contract test** over a real socket: `FrameRewind` → the session's stdin carries the
  right `control_request`; the receipt surfaces as `KindRewindReceipt`. Refusal while blocked-on-ask.
- **UI**: in-process — the `esc esc` gate (idle+empty opens the picker; running interrupts; non-empty
  clears draft), the picker options equal the active-branch prompts, a receipt re-reads and prefills;
  **pty** (`cmd/wake/screen_unix_test.go`) — the key actually opens the picker and the composer shows
  `prefillText` on success.
- **Race + non-race.** `make test` runs twice; a green race run is not evidence on its own.

## 8. Non-goals (v1)

File/working-tree rewind (`rewind_files`); rewind initiated from the room; mid-turn rewind
(`interrupt_if_running:true`); rewinding a subagent transcript; rewinding a parked session.

## 9. Open questions for review

1. **Trigger label.** `esc esc` adds no glyph; the gated label swap is the mechanism. Is a label
   swap enough discoverability, or should the idle+empty pane hint "esc esc to rewind" somewhere?
2. **Picker contents.** Newest-first list of *your* prompts (Claude Code's model). Confirm we don't
   also want to show the assistant reply preview per row.
3. **Room live lag** (§6). Accept the bounded cosmetic gap for v1, or invest in live-path UUIDs now?
```
