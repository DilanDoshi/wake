# The question card becomes a wizard: tabs, a review page, and free text

**Status:** design approved 2026-08-22. Supersedes nothing; extends `cards.go`'s three-shape model
for `ShapeQuestion` only. `ShapePermission` and `ShapePlan` are untouched.

Five defects were reported against one screenshot of a live `AskUserQuestion` ask. Each has a
distinct cause, and four of the five are in code this spec changes.

## 1. What was wrong

| # | Report | Cause |
|---|---|---|
| 1 | the room says nothing while an agent is blocked | `App.observe` routes `KindPermissionRequest` into `a.cards` and `continue`s, so it never reaches `Room.Append`; and `Cards.Undrawn` excludes agents that already have a pane, so an open DM leaves the room with no card either |
| 2 | the card renders "funny", not like a prompt | one ask is drawn three times - `KindToolUse` draws the call, `permissionBlock` draws the label **and the call again**, then the pinned card draws its own headline - and the card has no frame, so it reads as one more transcript block |
| 3 | no free text, no "chat about this" | there is none. `cardDenyReason` is a constant and a question can only be answered by picking a supplied option |
| 4 | the options swap under `↑↓` | `questionBody` inserts the detail row *inline* under the cursored option, so moving the cursor pulls the next label up and pushes a new detail row down; and `pickOption`'s `Move(1, 0)` **clamps** `Option` into the next question instead of resetting it, so question 2 opens with the cursor part-way down |
| 5 | the last question never submits | `Move(1, 0)` clamps at the last question, so `↵` re-picks the same option forever. Submitting actually needs `a` then `↵`, which the card only advertises once every question is answered |

## 2. The shape: N questions, then a review

A question ask becomes a wizard of `N+1` **steps** - one per question, then `stepReview`. This is the
central change and everything else follows from it.

`Card.Cursor` already indexes the question; it gains one legal value past the end. `Card.Step()`
reports which of the two a cursor is on, so nothing outside this file compares `Cursor` to
`len(Questions)`.

### The review page replaces arm-and-confirm, and is stronger than it

`cards.go` requires two presses to settle because a settle cannot be undone: an allow runs the tool,
a deny reaches the model verbatim, and nothing on the wire retracts either. The arm was the only
moment the operator could still change their mind.

The review page is that moment, made legible. It lists every question with the answer that will be
sent and offers two options; the arm showed a verb and nothing else. So **`ShapeQuestion` drops
arm-and-confirm** and `[a]nswer` disappears from its key line. `[d]eny` stays - it is the refusal,
and it is the one thing the review page cannot express.

`ShapePermission` and `ShapePlan` keep `armCard`/`confirmCard` exactly as they are. They have no
steps, no review, and one press is still not enough for them.

### The review page is an option list, so it introduces no key

`Submit answers` and `Cancel` are drawn through the same `optionRow` the questions use. `↑↓`, `↵`
and the digits therefore work on it unchanged, and no new binding is added to `cardkeys.go` -
which matters because the card is its own legend and every bracketed rune it draws is held to a
bijection with what `cardkeys.go` binds.

`Cancel` returns to the last question. It is not a deny: nothing is written and the ask stays up.

## 3. The strip

One tab per question plus a Submit tab, replacing both the `┤ chip ├` line and `question 1 of 4`:

```
← ☒ entity_types  ☐ loader  ☐ gate  ✔ Submit →
```

A tab is the question's `Header` (its chip), checked when that question has an answer. The cursored
tab wears the accent; the rest are muted. `←`/`→` at the ends are affordances for the keys that
walk it.

**It is tiered, like `preview.go`'s panel.** Full strip when it fits; otherwise the cursored tab and
a count; otherwise `question 1 of 4` alone. A strip cut mid-tab claims a question that cannot be
read.

## 4. Free text

`Other…` is a synthetic final option on every question, past the supplied ones. Picking it puts the
composer into **answer mode**: the box takes the title `answering: <chip>` (the composer already
supports a title), and `↵` sends what was typed as that question's answer, then advances a step.

`[d]eny` opens the same box titled `deny reason`, which closes the deferred item at
`docs/notes/deferred.md:1902` - the reason reaches the model verbatim as the tool result and is the
one channel for saying what to do instead of retrying the identical call.

### What it costs on the wire: nothing

`Card.answers()` is already `question text → chosen option label`. A typed answer is a different
label. `Card` gains `Typed []string` beside `Choice`, and `answers()` prefers a non-empty `Typed[i]`
over an option label. No frame kind changes, no encoder changes, and `asked` stays unexported.

`AskUserQuestion`'s own contract says users may always answer with custom text, so a label the
options do not contain is legitimate rather than a guess.

### Why this does not re-open the letter-key hazard

`cardkeys.go` reads `a`, `d` and the digits only when the composer is empty, because they are
characters people type and the first character of every draft is typed into an empty composer.
Answer mode is the only state in which `↵` on a **non-empty** draft means something other than
*send*, and it is entered by a deliberate pick or by `[d]eny`. Outside it the gate is untouched.

`esc` in answer mode returns to the options and drops the answer draft; a second `esc` interrupts,
riding `escape.go`'s existing arm rather than a second mechanism.

## 5. The keys

`cardKey`'s single gate splits in two. Character keys keep the empty-composer gate. Navigation keys
never needed it - nobody types `↑` into a draft - and requiring it is why the arrows silently
reverted to the roster the moment anything was typed, with the card still up.

| Key | Card drawn in focused pane | No card |
|---|---|---|
| `↑↓` | walks the options, **draft or not** | roster, unchanged |
| `←→` | walks steps, onto `Submit` | composer cursor, unchanged |
| `↵` | picks and advances; on `Submit`, sends | send / open picked |
| `a` `d` digits | empty composer only, unchanged | composer text |
| `⌥↑↓` | prompt history, unchanged | prompt history |
| `⇧←→` | move pane, unchanged | move pane |

`←→` are claimed **only while the composer is empty**. They are not in `App.key`'s switch today, so
on an empty composer they reach the textarea and do nothing - the claim costs nothing. A non-empty
draft needs them for its own cursor, which is also what makes answer mode editable.

`↑↓` are taken from the roster only while a card is drawn in the focused pane. `CLAUDE.md`'s ruling
that they are "unconditionally the roster's" narrows to "unconditionally, unless the pane you are
looking at is putting an ask to you", and `⌃N`/`⌃P` remain the only vertical movement inside a
draft.

## 6. The frame, and the dimming

The card is wrapped in `BoxStyle`'s rounded border with a warn-coloured edge. The headline goes in
the **top edge** and the key line in the **bottom edge**, so the frame costs two rows rather than
four - the composer already draws a titled edge this way and the code is shared rather than copied.

Blank-line separation between strip, question, options and detail. The reported "hard to read" is
mostly this: today every row is adjacent.

While an ask is up, the transcript behind it is drawn in `HintStyle` so the card reads as modal.

**This is the one piece with a real price and it is paid explicitly.** Restyling the transcript per
frame is the per-frame work the non-negotiables forbid, and lipgloss cannot recolour text that
already carries ANSI - it needs stripping and re-rendering. So the dimmed transcript is cached on
the `DM` and invalidated on the three things that change it: the card set, the transcript, and the
width. That is `DM.bar`'s own shape. `deferred.md:1790` measured 185 µs/op with no card and 231 with
a question; a benchmark is added beside it and **the number is reported in the PR**. If it cannot be
made cheap the dimming is dropped and the border kept, rather than a per-frame cost shipped quietly.

## 7. The room, and the transcript

`App.observe` appends the ask to the room **as well as** adding the card, and `roomBlock` grows a
`KindPermissionRequest` case drawing `⚠ iris has a question` from the same `headline()` the card
uses. It carries no keys, so *one ask, one surface* holds: the room announces, the card answers.
`TestEveryKindTheRoomAdmitsIsDrawnHereOrExcusedWithAReason` is what forces the ruling to be written
rather than defaulted.

In the DM, `permissionBlock` stops re-drawing the tool call that `KindToolUse` drew one block above.

## 8. Files

New, because `cards.go` (564) and `cards_blocks.go` (477) may not approach the 800-line hard max:

- `internal/ui/cardsteps.go` - the step model and the strip
- `internal/ui/cardreview.go` - the review page
- `internal/ui/cardanswer.go` - answer mode

Changed: `cards.go`, `cards_blocks.go`, `cardkeys.go`, `appview.go`, `dm_blocks.go`,
`chat_blocks.go`, `app.go`, `keys.go`, `mouse.go`.

`mouse.go`'s `startSelection` measures `paneChrome` to learn how many rows sit above a transcript.
The border and the strip change that height, and it stays correct **only** because it measures the
call rather than a number - which is the property to preserve, not to re-derive.

## 9. Tests

TDD throughout: each defect gets a test that goes red first.

- the row-shift: option row indices are stable across `↑↓` over a question whose options carry details
- `Option` resets to 0 on advancing a step, rather than clamping forward
- `↵` on the last question lands on `stepReview`, and `↵` on `Submit answers` writes the frame
- `Cancel` writes nothing and returns to the last question
- a typed answer reaches `answers()` as its own label, and a typed deny reason reaches `Deny`
- the arrows reach the card **with a draft in the composer**, and `a`/`d`/digits still do not
- `←→` walk steps on an empty composer and move the cursor on a non-empty one
- the room draws a line for an ask whose card a DM is drawing
- the DM draws the ask's call once, not twice
- pty, through `cmd/wake/screen_unix_test.go`: the border, the strip and the spacing, since nothing
  else in this tree can see a rendered screen
