# Effort and model, from inside the room

**Status:** design, approved 2026-08-13. Branch `feat/effort-and-model`.

Wake can start a session and change its permission mode from inside itself. It cannot change what a
session *thinks with* — the reasoning effort or the model — except by typing a command whose only
answer is a usage line. This closes that, for every session including the manager.

## 1. The question that started it, and its answer

> Typing `/effort` at an agent prints `Usage: /effort <low|medium|high|xhigh|max|ultracode|auto>`.
> It should pop up the menu Claude Code pops up. Same for `/model`. Is that possible?

**Not by forwarding.** Agents run `--print --input-format stream-json`, which is headless. There is
no interactive picker on that side and no frame that could carry one; the usage line is what claude
prints to a pipe. `internal/daemon/effort.go`'s header asserts the opposite — *"`/effort` with no
argument … opens claude's own picker"* — and that sentence is now known to be false in Wake's mode.
It is corrected in the same change.

**Possible if Wake draws it.** Wake renders the picker and, on selection, sends `/effort max` as
ordinary text. That is the path that already works: the command passes through byte for byte and
`agent.noteEffort` watches it go past. The menu is new; the mechanism underneath it is not.

## 2. What was verified, and how

Everything below was checked against this tree or this machine. Nothing here rests on recollection
of the CLI.

| Claim | How |
|---|---|
| The bare form prints usage and changes nothing | Owner's screenshot, `@john /effort`, 2026-08-13 |
| `effort` and `model` are both words claude advertises | `init.slash_commands` in `testdata/stream`, 133 commands, both present alongside `config` and `update-config` |
| `--effort` takes exactly five levels | `claude --help`: `--effort <level>  Effort level for the current session (low, medium, high, xhigh, max)` |
| `/effort` takes seven | The usage line in the screenshot: `+ ultracode, auto` |
| `--model` takes aliases **or** full names | `claude --help`: *"Provide an alias for the latest model (e.g. 'fable', 'opus', or 'sonnet') or a model's full name (e.g. 'claude-fable-5')"* |
| Nothing on the wire lists available models | Full key dump of a real `init` frame: `model` is the current one; there is no list |
| Model is observable per turn; effort is on no frame at all | `core.initFacts` reads `init.model`; `deferred.md` records that effort reaches Wake nowhere |
| `--max-budget-usd` exists and works only with `--print` | `claude --help` |
| A Wake-originated card would be deleted within milliseconds | `Cards.Reconcile` drops every card whose `(AgentID, RequestID)` is absent from the fleet report, and a report lands whenever any of thirty agents changes state |

**Version drift:** CLAUDE.md says the CLI surface is verified against **2.1.228**. The binary on this
machine is **2.1.232**, and the recorded corpus was captured at **2.1.226**. The `--help` readings
above are 2.1.232. Re-stating the verified version is part of this change.

## 3. Decisions

| # | Decision | Rejected |
|---|---|---|
| D1 | Every session gets the pickers; the manager is one addressee among them | Manager-only, which leaves the reported case (`@john /effort`) unfixed |
| D2 | Wake owns `/effort` and `/model` **only in their bare form** | Owning the words outright, which would refuse `ultracode` — a level claude accepts |
| D3 | The model picker offers aliases plus a typed escape | Hardcoded model ids, stale the day a model ships |
| D4 | Ships: both pickers, plus spawn-time effort and model for every verb including `wake manager` | Pickers alone, which leaves the manager unconfigurable at spawn |
| D5 | An unaddressed `/effort` in the room follows the default addressee, and the picker names its target | Refusing like `/name` does, which costs two commands for the common case |
| D6 | `@all` is allowed for **both** commands, with the agent count on the card | Refusing broadcast retuning |

## 4. Architecture: two kinds of slash command

Wake's commands divide on a property the current design does not name. `/resume`, `/new`, `/name`
and `/task` are **addressed to Wake** — they are target-independent, and `/resume` must work on a
parked session, which is why `App.submit` runs `slash` *before* routing. `/effort` and `/model`
configure **one session**, so they need a resolved target and must run after it.

`App.submit` gains one stage:

```
bang        !ls                        addressed to nobody
slash       /resume /new /name /task   addressed to Wake            unchanged
  ↓                                    (still above the ended check)
route       @who … | @all … | default addressee
configure   /effort /model             addressed to a resolved target   NEW
  ↓
send
```

`slash()` keeps its closed map, its count constant and its guard exactly as they are. `configure` is
a **second closed set** with its own count constant, running on the remainder after addressing.

**It does not route.** It receives the targets `App.route` already resolved and never forms a second
opinion — `sendRoom`'s header rules that two callers of the router are two answers to one question,
with the promise on screen and the turns on the wire free to disagree.

**Reached from two places, one implementation.** `sendDM` (target is the pane's own recipient) and
`sendRoom` (targets are the route's). A DM to a parked or ended agent still refuses first: there is
nothing to configure.

## 5. The fence carve-out

`internal/ui/slash.go`'s rule is *resolve against a closed set Wake owns, and anything else is text*,
with `TestWakeOwnsNoCommandTheRecordedCorpusShowsClaudeAdvertising` refusing any word the corpus
shows claude advertising. Both `effort` and `model` are in that corpus. The rule narrows:

> Wake may own a word claude advertises **only in a form claude is recorded doing nothing with.**
> The recording is the admission ticket.

Concretely:

```
/effort            → Wake draws the picker
/effort max        → passthrough, byte for byte, unchanged
/effort ultracode  → passthrough, unchanged
/effort nonsense   → passthrough; claude refuses it, as it does today
```

The guard is **not** deleted. It gains an exemption list, and **each entry must name a fixture in
`testdata/stream` or the test fails.** A word cannot be exempted by assertion, only by evidence.
That is what keeps this from being the prefix rule the fence was built against: it admits exactly
the forms somebody has recorded as inert, and nothing else.

**What this costs.** The rule now has a second clause, and a second clause is a second thing that can
be reasoned around. The mitigation is that the clause is not satisfiable by argument — it needs a
committed recording, which is the same bar `docs/superpowers/notes/` already sets for every CLI
claim in this tree.

## 6. Two vocabularies, not one

`--effort` takes five levels; `/effort` takes seven. They are different surfaces that happen to share
a name — `effort.go` says so already, and widening the shared constant would have put `ultracode` on
an argv the CLI refuses.

| Constant | Surface | Values |
|---|---|---|
| `core.EffortLevels` | `--effort` argv | low, medium, high, xhigh, max |
| `core.EffortCommands` | `/effort` text, picker options | + ultracode, auto |
| `core.ModelAliases` | `/model` text, picker options | the `--help` examples, + a typed escape |

`core.ValidEffort` stays the argv predicate — every existing caller (`spawnFlags`, `admit`,
`bookEffort`) keeps asking it and keeps refusing seven-set values. `noteEffort` moves to the
seven-set predicate, because it records what was *typed*, not what could go on a command line.

### 6.1 How far derivation actually reaches

The palette pattern — a committed fixture → a test, maintained by hand — applies to
these lists, but **not equally, and the spec says so rather than overclaiming**:

- **Effort is genuinely derivable.** `--help` prints a closed parenthesised set: `(low, medium, high,
  xhigh, max)`. A script can parse it and a test can assert `core.EffortLevels` against the fixture.
- **The model alias list is not.** `--help` gives *examples* — "e.g. 'fable', 'opus', or 'sonnet'" —
  and an `e.g.` closes nothing. The script extracts what is there; the fixture records that it is a
  sample; the test asserts the picker contains at least the sampled aliases and does not assert
  completeness, because completeness is not knowable from this source.

**The typed escape is what makes an incomplete list safe.** A model shipped tomorrow is reachable on
day one without a Wake release, so the list never becomes a fence that blocks a working value. That
is the whole reason D3 chose aliases over ids.

### 6.2 The park consequence, already handled

A session set to `ultracode` at runtime is recorded by `noteEffort`, carried into `parked.json`, and
then dropped on the wake by `bookEffort` — which already guards with `core.ValidEffort`, logs, and
brings the session back with no `--effort` at all. Lossy and honest, and it is existing behaviour
rather than something this change introduces. It is documented here so it is not rediscovered as a
bug.

## 7. The picker

**Its own type, and the reason is mechanical.** `Cards.Reconcile` rebuilds the open set from the
daemon's fleet report, keyed on `(AgentID, RequestID)`, and drops anything absent. A Wake-originated
picker has no request id and appears in no report, so it would be deleted on the next status push —
which lands whenever any of thirty agents changes state. A picker in `Cards` would vanish in
milliseconds on a busy fleet.

So `Picker` is a distinct type with a distinct lifecycle: created by the operator, settled by the
operator, invisible to the daemon. It **shares `cards_blocks.go`'s rendering** rather than growing a
second drawing path — one implementation of "a bordered list with a cursor", two things that use it.

- **One at a time.** A second `/effort` replaces the first rather than stacking.
- **Arrows and `↵` only; `esc` cancels.** No bare digit keys. The arm-then-confirm rule exists
  because a card arrives *unbidden* and the first character of a draft lands in an empty composer. A
  picker is opened deliberately, two keystrokes ago, so the accident it guards against cannot occur —
  and avoiding digits removes the class entirely rather than managing it.
- **The header names the target**, which is what makes D5 safe: `effort for @manager`, `effort for
  @john`, `effort for 30 agents`. A misfire is visible before a key is pressed rather than after.
- **On confirm**, one command writes one send frame per target — `/resume all`'s rule, for
  `/resume all`'s reason: bubbletea runs every `tea.Cmd` on its own goroutine and rpc's write lock is
  process-wide, so thirty targets must be one command writing thirty frames.

### 7.1 The typed escape

The model picker's last row is `type one…`. Selecting it closes the picker and puts `/model ` in the
composer with the cursor after the space — it does **not** open a second input. The operator finishes
the line and presses `↵`, at which point the draft has an argument and §5's carve-out no longer
applies to it: it passes through byte for byte, exactly as a hand-typed `/model opus` does.

That is the whole mechanism, and it is chosen because it adds no new input mode. The escape is a
shortcut to the passthrough that already exists, not a parallel path into it.

### 7.2 Targets that cannot take a command

`/effort` and `/model` become text on an agent's stdin, so they need a session that can receive text.

- **In a DM**, the existing `sendDM` refusals fire first and unchanged: a parked agent gets
  `parkedAdvice`, an ended one gets `endedAdvice`, and no picker opens. There is nothing to
  configure, and opening a picker that cannot be confirmed is the lying surface `wake stop`'s rule
  refuses.
- **In the room**, `App.route` already resolves only live agents, so a parked or ended `@who` yields
  no targets and the existing refusal is what the operator sees. `@all` resolves to the live fleet,
  which is why the card's count is the count it will actually write to.
- **With no targets at all** — an unaddressed draft in a room with no manager — the existing
  `noneListening` / `managerParked` refusals fire, and again no picker opens.

No new refusal sentences are introduced. This section exists to record that the ordering makes that
true rather than leaving it to be discovered.

## 8. Model reaches the wire

`rpc.SpawnFrame` gains `Model`. It takes Effort's argument, not Role's: Role is refused because an
MCP config **names a command to execute**, and a client that chose one would be choosing the command
line of the session holding tools over the whole fleet. A model names nothing. It is checked before
anything spawns, and the worst a hostile client does is pick a model on a session it was already
permitted to start.

- `core.Config.Model` stops being a field with an emitter and no producer.
- `parkedRecord` carries `Model`, so it survives a park — which is what makes `wake manager --model
  opus` mean anything after a `⌃Q`. Effort already does this; model joins it.
- `--effort` and `--model` become spawn flags for every verb that starts a session, rather than
  `wake new`-only. `main.go`'s "`--effort` is only for `wake new`" refusal goes.
- **The manager's tools and scope stay derived from its name.** That rule is untouched and this does
  not dent it: `managerConfig` keys on the name and still owns `--mcp-config`,
  `--strict-mcp-config`, `--tools ""` and `--append-system-prompt`. Effort and model are ordinary
  spawn configuration that happens to also apply to a session called `manager`.

## 9. What the surface is allowed to claim

- **Model self-corrects.** It is on every `init` frame, so a wrong label is wrong for at most one
  turn. The status bar already reads it.
- **Effort never confirms.** It is on no frame Wake will ever receive. `effort.go`'s existing ruling
  stands — *a record Wake is unsure of is worse than one that is merely stale, because the stale one
  is at least what Wake asked for* — and this change puts it on the surface: the pane shows the level
  **Wake asked for**, and nothing anywhere says "applied", "confirmed" or "set". The picker uses no
  such word either.

This is the permission mode's rule read in the one direction it cannot go: mode moves on the receipt
because mode *has* one. Effort has none, so it moves on the ask and says that is what it is.

## 10. Task 0: the recordings, and the contingency

The carve-out's premise is that **both** bare forms are inert. The screenshot establishes `/effort`.
`/model` bare has no evidence at all.

Task 0 is one live `claude` run per command in stream-json mode, committed to `testdata/stream` —
the corpus that the exemption list in §5 must name. It costs a few cents and cannot run in CI, which
is what `docs/live-testing.md` exists for.

**If bare `/model` turns out to do something real**, D2's premise fails for that word only:
`/model` falls back to a Wake-owned verb or a key, `/effort` ships as designed, and the fallback is a
one-line change to the `configure` map rather than a redesign. This is written down now so it is a
branch in the plan rather than a surprise in the build.

## 11. Testing

TDD, 80% gate, `make test` twice (with and without `-race`).

| Layer | Approach |
|---|---|
| The two vocabularies and their predicates | Pure table tests, plus the extraction fixture assertion |
| `configure` routing, targets, refusals | Table tests over `App.route`'s output |
| The fence rule | `slashguard_test.go` — exemption list held to `testdata/stream` filenames |
| Picker rendering | `frame_test.go` character assertions |
| Picker keys, and that it survives a status push | pty harness, `screen_unix_test.go` |
| `SpawnFrame.Model` end to end, and across a park | daemon contract tests over a real socket |

The picker-survives-a-status-push test is the direct regression for §7's finding, and it is the one
most likely to catch a future refactor that folds `Picker` back into `Cards`.

## 12. Out of scope

Into `docs/notes/deferred.md`, each with its reason:

- `output_style` and `fast_mode_state` — both on the `init` frame, so both are safe candidates later.
  They are out because each new picker spends fence budget that §5's rule must justify separately.
- `--max-budget-usd` — spawn-only, no runtime command, and the cap is unobservable even though spend
  is not.
- **The manager setting effort or model on other agents — refused, not deferred.**
  `cmd/wake/mcpguard_test.go` already refuses mode on the argument that a manager which could set one
  would be *the fleet deciding it will not be asked*, in every future decision that session makes
  rather than one, and unlike a message it shows up in no row that surface returns. Effort and model
  are the same class and cost more. The MCP surface stays at five tools.

## 13. Files

| Change | Where |
|---|---|
| The `configure` set and its router | `internal/ui/configure.go` — new |
| The picker type, keys and rendering | `internal/ui/picker.go` — new; draws through `cards_blocks.go` |
| One new stage in submit | `internal/ui/send.go` |
| The narrowed fence rule | `internal/ui/slashguard_test.go` |
| The two new vocabularies | `internal/core/effort.go` · `internal/core/model.go` — the second is new |
| Extraction and fixture | `scripts/extract-claude-flags.py` → `internal/core/testdata/claude-flags.json` — new |
| `noteEffort` widened; the false picker sentence corrected | `internal/daemon/effort.go` |
| `Model` on the wire | `internal/rpc/wire.go` · `internal/daemon/spawn.go` · `parkbook.go` · `park.go` |
| `--effort`/`--model` for every spawning verb | `cmd/wake/spawnflags.go` · `main.go` · `manager.go` |
| Recorded bare forms | `testdata/stream/` |
| Verified-version line, key locations, load-bearing rules | `CLAUDE.md` |
| Deferred entries | `docs/notes/deferred.md` |

## 14. Risks

1. **The fence rule gains a clause.** Mitigated by making the clause unsatisfiable without a
   committed recording (§5).
2. **Bare `/model` may not be inert.** Contingency in §10; it degrades one word, not the design.
3. **The model alias list cannot be complete.** Accepted and designed around by the typed escape
   (§6.1); the spec does not claim otherwise.
4. **Effort can never be confirmed.** Not solvable from this side. Handled by never claiming it
   (§9).
5. **`Picker` and `Card` could be merged by a later refactor**, reintroducing the reconcile deletion.
   The pty regression test in §11 is the guard.
