# A subagent is a conversation, not a paragraph

**Status:** design, approved 2026-08-15. Implements `deferred.md` I6's remaining third and item 2 of
`2026-08-09-subagent-findings.md` §9 — the one of that list's five that never got built.

## 1. Two complaints, one cause

The owner reported two things about a fleet running subagents:

1. Wake has no subagent status bar. Claude Code draws one — a row per running task carrying its
   type, what it is doing, how long it has been doing it, and what it has spent.
2. Every subagent call and response is dumped into the DM pane.

These are the same defect read from two ends. Claude Code emits two parallel streams for a
dispatch, and Wake keeps exactly one of them:

| Stream | Frames | Today |
|---|---|---|
| The subagent's **work** | `assistant`/`user` carrying `parent_tool_use_id` | Decoded to `core.Event.Subagent`, rendered inline by `ui.DM.eventBlock` under a header and a `│ ` gutter |
| The subagent's **status** | `system/task_started`, `task_progress`, `task_updated`, `task_notification`, `background_tasks_changed` | `systemEvent` → `KindSystem`, `Notice` unset, `noticeBlock` returns `""`. **Drawn nowhere** |

With no collapsed surface the work has nowhere to go but the transcript, and there it drowns the
conversation the operator opened. Measured over the committed fixtures, the subagent's share of
message frames in a dispatching turn is **55–77%**: `subagent-task.jsonl` 17 of 22,
`subagent-background.jsonl` 20 of 26, `subagent-no-forward.jsonl` 13 of 18.

Wake is already *receiving* every subagent's full transcript — `argv.go` passes
`--forward-subagent-text` unconditionally. The fix is not to stop receiving it. It is to give it
somewhere to live.

## 2. What Claude Code does

**Measured against 2.1.233** by observation, the way the palette values are.
Behaviour only — no code was copied, and none
may be: it is proprietary and minified, and the rule against copying cmux source applies here with
more force, not less.

**One transcript per task, selected by id.** The pane does not filter a single list; the store is
keyed:

> `viewingAgentTaskId` unset → render `transcripts[main]`.
> Set → render `transcripts[taskId]`, and the task's own status drives the loading state.

Entering sets `viewingAgentTaskId` and a `viewing-agent` selection mode; leaving clears both.
Selecting row 0 — the `● main` row — is what leaves. A finished task's messages are retained while
viewed and evicted after, and a retained one is rehydrated from `task_notification.output_file`.

**The list is its own key context**, `Footer`:

| Key | Action |
|---|---|
| `up` / `ctrl+p` · `down` / `ctrl+n` | walk the rows |
| `enter` | open the selected task's history |
| `escape` | clear the selection |
| `x` · `backspace`/`delete` | close · dismiss |

**The strip counts kinds separately** — `N agent(s)` and `M active shell(s)`, joined ` · `. That is
not decoration: the task registry holds both, and §3 is why.

## 3. The wire

Every number below is derived from `testdata/stream/*.jsonl` by scanning the committed corpus, not
from documentation.

| Subtype | × | Keys, and how often they ride |
|---|---|---|
| `task_started` | 10 | `task_id`, `tool_use_id`, `description`, `task_type` 10/10 · `subagent_type`, `prompt` **9/10** |
| `task_progress` | 26 | `task_id`, `tool_use_id`, `description`, `subagent_type`, `usage`, `last_tool_name` 26/26 |
| `task_updated` | 10 | `task_id`, `patch` 10/10 — and **no `tool_use_id`** |
| `task_notification` | 10 | `task_id`, `tool_use_id`, `status`, `output_file`, `summary` 10/10 · `usage` 9/10 |
| `background_tasks_changed` | 8 | `tasks[]`, each `{task_id, task_type, description}` |

`usage` is `{total_tokens, tool_uses, duration_ms}`. `patch` is `{status, end_time}` on all ten,
which §11 of the findings note flags as one shape observed ten times rather than a settled schema.

### 3.1 A task is not always a subagent

`task_type` has two recorded values and the second is why this section exists:

- `local_agent` ×9 — a subagent. `task_id` is 17 characters.
- `local_bash` ×1 — a **background shell** (`interrupt-cancel-queued-empty.jsonl:38`). `task_id` is
  9 characters, and it carries neither `subagent_type` nor `prompt`.

A `local_bash` task has no forwarded frames, so a view that assumed every task was a subagent would
offer a drill-in onto an empty transcript. It is also what Claude Code's `1 shell · 6 agents` is
counting. **`task_type` is the discriminator and the unknown case must degrade to shown-but-not-
enterable**, never to assumed-subagent.

Statuses, both terminal frames: `task_notification.status` is `completed` ×9, `stopped` ×1;
`task_updated.patch.status` is `completed` ×9, `killed` ×1. The `local_bash` task is the one that
stopped and was killed. Four words, two of them from one recording each.

### 3.2 Three identifier spaces, and the frame that joins them

Verified over the corpus, both single and concurrent dispatches:

```
task_started.tool_use_id == the parent's Agent tool_use block id == forwarded parent_tool_use_id
task_started.task_id     == can_use_tool.agent_id               == tool_use_result.agentId
```

`task_started` is the **only** frame carrying both, and it arrives first in all nine dispatches. So
it is the join, and everything else keys off what it recorded. Neither id may be inferred from the
other; a decoder that treated `task_id` as a dispatch id would file a permission ask against a
transcript that does not exist.

## 4. The airlock change

Inside the existing four files. No fifth — `protocol.go` is 457 lines and `wire.go` 368, so the
rule that split this package is not in play.

`wire.go` gains the fields. `vocabulary.go` resolves `task_type` and the status words into Wake's
own. `protocol.go`'s `systemEvent` fills a new `Event.Task *TaskUpdate`:

```go
type TaskUpdate struct {
    ID       string      // task_id
    Dispatch string      // tool_use_id; empty on task_updated
    Kind     TaskKind    // TaskAgent | TaskShell | TaskKindUnknown
    Phase    TaskPhase   // TaskStarted | TaskProgress | TaskEnded
    Label    string      // description - the only human-readable name on the frame
    Type     string      // subagent_type
    Tool     string      // last_tool_name
    Tokens   int
    Elapsed  time.Duration
    Status   TaskStatus  // TaskRunning | TaskDone | TaskStopped | TaskStatusUnknown
}
```

Rulings this shape encodes:

- **`Kind` is resolved, not passed through.** `local_agent`/`local_bash` are Claude's words; a
  third value resolves to `TaskKindUnknown`, which the view shows and refuses to enter.
- **`prompt` is not decoded.** It is the subagent's entire instruction and it is on 9 of 10
  `task_started` frames. Nothing in this design draws it, and the airlock does not carry what
  nothing reads.
- **`output_file` is not decoded either**, for now. §7 says why.
- **`Phase` collapses four subtypes into three**, because `task_updated` and `task_notification`
  both report the same ending and only their key sets differ.

`Notice` is untouched. These frames are not notices — a notice is a line in a transcript, and this
is a record the view keeps.

## 5. Where the forwarded frames go

`DM` today holds one `events`/`tr` pair. It gains a second store keyed by dispatch id, and
`DM.Append` routes on one condition it already has:

```
ev.Subagent == nil  → the conversation's own transcript, unchanged
ev.Subagent != nil  → the transcript for ev.Subagent.Dispatch
```

`Subagent.Dispatch` is `parent_tool_use_id`, which §3.2 proves is the same id `task_started`
recorded — so the rows and the transcripts key off one value with no second correlation to
maintain.

Three consequences, each a test:

- **A dispatch receipt stays in the parent.** `Subagent.Result != ""` marks the agent reporting
  *about* a subagent, which is the parent speaking. `eventBlock`'s existing `receiptNote` branch
  already separates them and keeps its one-line form.
- **A subagent's permission ask still reaches the operator.** It is routed by `request_id` and
  drawn by `Cards`, neither of which this touches. It carries `agent_id` == `task_id`, so the card
  can name which row is blocked — but the ask is answered where asks are answered today. **A
  blocked subagent must never be reachable only by drilling into it.**
- **Nothing de-duplicates.** The existing rule holds: `SubagentFinished` still suppresses the
  receipt body because the report is above it — except the report is now above it *in the
  subagent's own transcript*, which the parent is not showing. So the receipt's note changes from
  "its report is above" to naming where it went.

`dm_blocks.go`'s header and gutter are **kept**, and become how the drilled-in view draws. They
were always right for an interleaved stream; they were wrong only as the parent's default.

## 6. The footer

**Built below the transcript rather than pinned above it**, which is where this design changed
during the build. `paneChrome` is the App-level pin *above* a pane — where a card and the picker
go — and Claude Code's own list is a footer under the input. Drawing it in `DM.View` beside the
heartbeat is both the right reading of "what is happening now" and the one that leaves
`mouse.go`'s `startSelection` alone: that measures the rows *above* a transcript, and these are not
among them.

The rows are still chrome, so they come out of the transcript's height. `chromeHeight` asks
`taskRowCount`, which derives the number without rendering — `SetSize` calls it on every re-lay —
and a test pins the count against the draw, because a count that drifts is a pane one row too tall.

Measured off the real binary in a pty at 110 columns:

```
▸○ orla
   · general-purpose  Counting lines in alpha                   5s · ↓ 29.0k tokens
```

`elapsedText` and `tokenText` in `heartbeat.go` already produce Claude's `1m 51s · ↓ 11.6k tokens`
exactly. Reused, not reimplemented — there is one formatter for this in the tree.

Three marks in the first column, not two: open, cursored, and both. The third is not a corner case —
the cursor sits on the open row for as long as nobody walks away from what they opened — and drawing
it as the open mark made walking onto that row invisible.

Bounded like every other pane element: below the width where the columns fit, the description is
truncated before the meta, because what it is doing is worth more than how long it has taken.
Finished tasks stay listed for the turn and stay enterable; the transcript is already in memory and
throwing it away at the moment the operator wants to read it is the whole complaint again.

## 7. Out of scope, deliberately

- **`output_file` rehydration.** Claude Code reloads a retained task's messages from disk. Wake has
  the live stream and holds it, so it needs the file only for a subagent from *before* the pane was
  opened. That is `history.go`'s problem, it needs the on-disk transcript format rather than the
  stream's, and `DecodeTranscriptLine` currently drops sidechain lines by design. Its own task.
- **`chat:killAgents`.** Claude Code can kill a turn's subagents without killing the turn; Wake's
  `⎋` interrupts everything. Listed in `claude-code-gap.md` and unchanged here.
- **Roster and awareness-strip counts.** The fleet-level `6 agents` reading. Cheap once the fold in
  §4 exists, and not in this change.
- **The herald frame.** `background_tasks_changed` is decoded to nothing: measured against the
  corpus it arrives one line *before* the authoritative frame for the same change and carries no
  dispatch, status or usage. `TestTheHeraldFrameIsRedundant` holds that ordering and is what would
  license decoding it.

## 8. Keys

**`⌃N` and `⌃P` walk the list; `↵` opens what the cursor is on.** This is the second place the
design changed during the build, and it changed toward parity rather than away from it: §2's
measurement shows Claude Code's Footer context binds `up`/**`ctrl+p`** and `down`/**`ctrl+n`** for
exactly this job. Wake takes the second pair and not the first.

`↑↓` were the plan and are wrong here. They are the roster's, and `pickAgent` *opens the sidebar* as
it moves — so a conversation claiming them would make the fleet cursor mean something different
depending on which pane had the focus, on the surface `⌃C` parks and `⎋` interrupts from.

`↵` consults the dispatch cursor before the roster's, because this one is only ever set by somebody
walking this list where the roster's is set by anything that opens the sidebar. A draft still sends.
`esc` clears the cursor **and** interrupts, the rule CLAUDE.md already states for a text selection:
a keystroke that stops a runaway agent must not be swallowed by something decorative. Leaving a
subagent's transcript is `↵` on the conversation's own row, which is what Claude Code's `main` row
does.

The keys are taken whether or not there is a list, the way every other case in `App.key` is — a key
bound only sometimes cannot be in the legend, and the legend is what advertises the surface. One
legend entry covers the pair, as `↑↓` and `⇞⇟` each do; the legend's width moves 282 → 300.

## 9. Testing

| Layer | What |
|---|---|
| `core` | Golden decode of all five subtypes against the corpus — **64** such frames today, 59 of them in the seven `subagent-*.jsonl` fixtures and 5 in `interrupt-cancel-queued-empty.jsonl`, which is the `local_bash` task §3.1 turns on. All of them currently decode to nothing. Table test over `task_type` and both status vocabularies, including the unrecorded-value case |
| `core` | The §3.2 joins asserted from the fixtures rather than restated, so a corpus that stops agreeing fails here |
| `ui` | The fold: events in, task list out. Pure, table-driven, no processes |
| `ui` | Routing: a forwarded frame does not reach the parent transcript, a receipt does, an ask still reaches `Cards` |
| screen | The pty harness for the rows, the truncation, and enter/exit — `internal/ui/frame_test.go` for the characters and `cmd/wake/screen_unix_test.go` for the keys |

Coverage gate is unchanged at 80%. TDD: the failing test first, per file.

## 10. Unverified — do not design around these

Carried forward from the findings note §11 and extended by this measurement:

- **`task_type` beyond two values.** Two recorded. The binary's own wording mentions monitors and
  workflows, which is a hint and not a recording.
- **`patch` beyond `{status, end_time}`.** Ten frames, one shape.
- **`stopped` vs `killed`.** One recording each, and both from the same `local_bash` task. Whether a
  subagent can end either way is unknown.
- **Nested dispatch.** A subagent dispatching a subagent is still unrecorded. The existing decoder
  keeps the speaker rather than guessing, and this design inherits that: a nested frame files under
  the outer dispatch.
- **Whether `subagent_type` and `description` are always present for a type other than
  `general-purpose`.** Every `local_agent` in the corpus is one.
