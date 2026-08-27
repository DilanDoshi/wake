# The live checklist is TaskCreate/TaskUpdate, not TodoWrite (2.1.240)

Recorded 2026-08-22 against claude 2.1.240. Fixture: `testdata/stream/task-checklist.jsonl`.

## What was wrong

`internal/render/todo.go` draws a task list correctly, but nothing fed it. It decoded
`TodoWrite` (`internal/core/vocabulary.go`'s `toolTodos`, keyed on the `todos` input array), and
**no `TodoWrite` call exists in the corpus** — 62 fixtures advertise it or its successors on
`init.tools` and zero call either. The renderer's whole path was untested by any recorded byte, and
the screen showed the symptom: an agent's working line fell back to the heartbeat word pool
(`✻ Lathing…`) where its own `activeForm` label should have been.

## Why: TodoWrite is retired

In Claude Code 2.1.240 the tool is gated off by default: `TodoWrite` is enabled only when the
`CLAUDE_CODE_ENABLE_TASKS` environment variable is not explicitly `false`, so it is disabled by
default. Its replacement is the `Task*` family, on by default. A
live `init` frame advertises `Task, TaskCreate, TaskGet, TaskList, TaskOutput, TaskStop, TaskUpdate`
and **not** `TodoWrite` (fixture line 7 lists the Task family; TodoWrite is absent).

The Task family is a *different subsystem* from the subagent dispatch Wake already draws
(`task_started`/`task_updated` **system** frames, `internal/core/task.go`). These are ordinary
`tool_use` calls.

## The wire shape

The checklist is built across many calls, where `TodoWrite` sent the whole list each call.

`TaskCreate` (fixture line 7) — input `{subject, description, activeForm}`, no status, no id:

    "name":"TaskCreate","input":{"subject":"explore the code",
      "description":"...","activeForm":"Exploring the code"}

Its `tool_result` is the only place the id appears:

    Task #1 created successfully: explore the code
    Task #2 created successfully: write the patch
    Task #3 created successfully: run the tests

`TaskUpdate` (fixture line 16) — input `{taskId, status, subject?, activeForm?}`:

    "name":"TaskUpdate","input":{"taskId":"1","status":"completed"}
    "name":"TaskUpdate","input":{"taskId":"2","status":"in_progress"}
    → "Updated task #1 status" / "Updated task #2 status"

- The id is a **sequential integer assigned in creation order** ("1", "2", "3"), reported in the
  create's result text and named back by `taskId` in the update.
- `status` is `pending` (implicit at create) | `in_progress` | `completed`. The 2.1.240 bundle's
  schema lists a fourth, `deleted`, which this recording never exercises.
- `activeForm` is set at create and carried onto the item; a status-only update omits it.

## How Wake decodes it

- `internal/core/vocabulary.go` — `toolChecklistOp` decodes one `TaskCreate`/`TaskUpdate` into
  `core.ChecklistOp` (one op, not a list). Gated on the tool **name**, unlike `toolTodos`: create and
  update share a generic `subject`, and the create-vs-update distinction is the tool identity.
- `internal/ui/checklist.go` — the `checklist` type folds a run of ops into an ordered
  `[]core.Todo`. Each item is keyed on the id claude assigns, reconstructed from a **monotonic create
  counter** (not a slice position): a delete removes an item without renumbering the survivors, and
  the next create takes the next counter value rather than a vacated one, so an update after a delete
  still lands on the item it names. A position-based fold silently corrupts here.
- Two folds share the type. `Fleet.foldChecklist` accumulates the *live* list for the working line
  (`activeForm`); `DM.foldChecklist` accumulates the *transcript's*, in both `Append` (live) and
  `Before` (restore), so a list built before this client attached comes back off disk — the reason
  the fold cannot live on the Fleet alone: restore replays through `DM.Before`, which the Fleet never
  sees.
- The rollup's `foldExempt` returns true on `len(Todos) > 0 || Checklist != nil`, so a checklist call
  stays drawn whole — including a restored one whose snapshot has not been folded yet — rather than
  folding into a run count, the treatment `TodoWrite` gets.

## Provenance caveat

The `taskId`→item mapping is the one inference: the id is authoritative only in the create's
*result text*, and Wake reconstructs it from a create counter rather than reading that text. This is
correct as long as claude assigns ids as a monotonic per-session counter — which every recorded call
here does, and which the delete/reorder handling is written against. The `deleted` status is decoded
from the 2.1.240 bundle schema and is **unrecorded**; a session that deletes or reorders items gets a
fixture and moves it out of that caveat.
