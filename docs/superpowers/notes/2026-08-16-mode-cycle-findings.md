# What ⇧⇥ actually cycles in Claude Code

**Method:** worked out against Claude Code 2.1.233 by observation rather than
by trusting documentation or memory, and checked against its own
`chat:cycleMode` switch.

This note exists because the ruling in `internal/ui/mode.go` rests on it, and
because `2026-08-12-permission-mode-findings.md` §7 left one sentence open:
*"If ⇧⇥'s cycle ever includes bypassPermissions, it must handle a refusal that
arrives only as an error receipt."* The answer turns out to be that Claude Code
does not include it either, for a reason its own switch spells out.

## 1. The switch

The cycle is captured as the factual mapping in `internal/ui/testdata/claude-mode-cycle.json` — the behaviour Wake asserts against, not Claude Code's implementation.

## 2. What it means under Wake's conditions

Wake never passes `--dangerously-skip-permissions`, so
`isBypassPermissionsModeAvailable` is false for every session it starts (§7 of
the 2026-08-12 note is the recording for that). Auto is available — it is the
mode every Wake session spawns in. Under those two conditions the switch
reduces to:

| From | Next |
|---|---|
| `default` | `acceptEdits` |
| `acceptEdits` | `plan` |
| `plan` | `auto` |
| `auto` | `default` (via the `default:` arm — there is no `case "auto"`) |
| `dontAsk` | `default` |

**A four-position cycle, and `dontAsk` is not one of them.**

## 3. Three things that follow

**`acceptEdits` is a cycle position and `dontAsk` is an exit.** Wake's original
ruling refused both together on the grounds that both loosen permissions and a
key with no confirmation should not reach either. Half of that survives contact
with the evidence: Claude Code, which is not shy about `acceptEdits`, does not
cycle into `dontAsk` either. Two designs reaching the same answer independently
is a better argument than one design's caution, and it is the argument
`modeCycle`'s comment now makes.

**The order is not monotonic in strictness, and Wake's used to be.** Wake walked
`auto → default → plan → auto` — every press asking for more human involvement
until it wrapped. Claude Code's traversal from `auto` is
`auto → default → acceptEdits → plan → auto`, where the second press *loosens*.
Adopting it costs that invariant and keeps the half that matters: the first
press from the mode every session actually starts in still tightens, which is
what a key nobody has read the legend for is judged on.

**bypassPermissions is not merely refused, it is not on the cycle Wake can
reach.** The `case "bypassPermissions"` arm exists, and its answer under
`canCycleToAuto` is `auto` — recorded in the fixture for completeness and
excluded from the comparison test, because no Wake session can be in that mode
to press ⇧⇥ from. Asserting an answer for it would be a guard over a state that
cannot arrive. §7's open sentence is therefore closed by construction rather
than by handling: the refusal-by-error-receipt path it warned about is
unreachable while nothing here passes `--dangerously-skip-permissions`.

## 4. What is recorded, and where

`internal/ui/testdata/claude-mode-cycle.json` holds the reduced table above,
the two conditions it was read under, and the one mode excluded from the
comparison. `TestTheCycleIsClaudeCodesOwnCycle` holds `nextMode` to it and
fails if the exclusion list grows — an exclusion is how a real disagreement
would come to look like agreement.

**No extraction script.** The fixture is maintained by hand, like the palette
and keymap fixtures, and this note is the record of what it should say.

## 5. What this does not settle

- **Whether `auto` is available in every environment.** `canCycleToAuto` reads
  `isAutoModeAvailable` and a gate function, and both are runtime state this
  note did not exercise. Wake spawns `auto` and the CLI accepts it, which is the
  evidence that matters here; a build where the gate is off would walk
  `plan → default` instead, and Wake would be one position out.
- **What the ⇧⇥ *label* says at each position.** Read separately: the hint line
  renders `chord="shift+tab" action="auto-accept edits"`, which names only the
  first move. Wake's legend spells the mode it is in rather than the move it
  would make, and nothing here changes that.
- **`manual`.** Absent from this switch entirely, consistent with
  2026-08-12 §6: it is accepted and silently normalized to `default`, so it is
  a value a session can be *in* and never one a cycle produces.
