# Select and copy — Design

**Date:** 2026-08-14
**Status:** Approved for planning
**Repo:** `github.com/DilanDoshi/wake`

---

## 1. Premise

Drag across text in a pane, let go, and it is on the clipboard. No key, no mode, no menu.

Wake passes `tea.WithMouseCellMotion()` at its one `tea.NewProgram` site, so it already owns the
mouse: the wheel scrolls, a click focuses, a divider drags. That ownership is what takes the host
terminal's own drag-selection away — an operator has to know to hold `⌥` or `⇧`, and
`docs/live-testing.md` §4 has carried "text selection still works the way you expect — or note
that it does not" as an unanswered question since the mouse landed.

Giving the mouse back is not the answer, and §4 of this document says why. Wake owns the
selection instead.

---

## 2. What Claude Code does, measured

This was measured against Claude Code 2.1.232 on 2026-08-14
rather than assumed, because the founding ask was "like claude code cli can" and the naive
reading — that Claude Code simply does not grab the mouse and the terminal does the work — is
wrong.

Claude Code's renderer, attached to `process.stdout`, carries its own selection engine:

```js
getSelectedText()        // reads cells out of this.frontFrame.screen
copySelectionNoClear()   // → lI(text).then(seq => stdout.write(seq))
copySelection()          // copy, then clear + notify
clearTextSelection()
setSelectionBgColor(c)   // stylePool.setSelectionBg(...) — highlight is a cell bg swap
moveSelectionFocus(dir)  // left/right/up/down/lineStart/lineEnd
this.selection = { focus, scope }   // scope carries {x1, x2} column bounds
```

Four findings that bear on this design:

1. **It owns the mouse.** The alt-screen wrapper is `<AltScreen mouseTracking={…}>`, and
   `reenterAltScreen()` re-emits `enterAltScreen + mouseMode(this.altScreenMouseTracking)`. It
   carries its own DECSET parser mapping `1000`/`1002`/`1003` to
   `{type:"mouseTracking", mode:"normal"|"button"|"any"}`.
2. **`copyOnSelect` is a real setting, defaulting to on** — `{id:"copyOnSelect", label:"Copy on
   select", value: t.copyOnSelect ?? true}` in the config screen, gated behind the same flag as
   `autoScroll`. Nothing would be left for that toggle to control if the terminal were doing the
   selecting.
3. **The clipboard write is layered**, which is why it works in Apple Terminal, over SSH and
   inside cmux alike: native tool first (`pbcopy` / `wl-copy` / `xclip` / `xsel` /
   `powershell.exe`), then `tmux load-buffer -w -` when `$TMUX` is set, then OSC 52
   (`\x1b]52;c;<base64>`) as the wire fallback — **DCS-wrapped under tmux and chunked under GNU
   screen**. It even carries a workaround note for a VS Code 1.123/1.124 OSC 52 mojibake bug.
4. **`selection.scope` bounds a selection to a column range.** Claude Code has one surface and
   still built column scoping. Wake has a grid, so scoping is not a refinement here — it is the
   feature.

**Wake reimplements the behaviour, never the code.** This is a reading of a shipped binary for
protocol facts, in the same spirit as `docs/superpowers/notes/2026-08-*-findings.md`. No Claude
Code source is copied.

---

## 3. Non-goals

- **No cell buffer.** See §4.
- **No keyboard selection in v1.** `moveSelectionFocus`'s equivalent — extend by
  char/line/line-start/line-end — is real work and every key it needs would have to earn a slot
  in `legendEntries`, which already truncates below 282 columns. Deferred, not rejected.
- **Nothing but transcript text is selectable.** Not the roster, not the composer, not the
  status bar, not the awareness strip. A selection is for reading back what an agent said.
- **No paste.** Reading the clipboard is a separate surface with its own security questions;
  the composer already has bubbles' own paste.
- **No selection persistence.** It does not survive a park, a detach, or a pane close.

---

## 4. The approach, and the two rejected ones

**A — cell buffer (Claude Code's).** Put a screen cell buffer under the whole frame; a selection
is a range over cells and the highlight is a background swap in a style pool. Claude Code can do
this because Ink already renders to a cell buffer. Wake composes ANSI strings through lipgloss
and has none. Building one is a renderer rewrite that would run under `View` for every pane on
every frame, straight into the **cheap to leave open** non-negotiable. **Rejected.**

**B — line-slice over the existing transcript. Chosen.** `transcript.view()` is the single draw
point for both the room and a DM, and `chunked.slice(from, to)` already hands it exactly the
visible lines. A selection is anchored to absolute `transcript.lines` indices; the highlight is
applied inside `view()`; extraction is `ansi.Strip` over `ansi.Cut`. Cost is O(pane height) and
never O(history) — the same bound `transcript` exists to enforce.

**C — shadow plain-text transcript.** Keep an ANSI-stripped copy of every line for extraction.
Doubles transcript memory across 30 sessions to save a `Strip` on the handful of lines anyone
actually selects. **Rejected** — strip on demand.

**Giving the mouse back was rejected before these.** It would cost the divider drag, the rule
drag and click-to-focus, all of which are built and used; and a native selection is a rectangle
over the whole terminal, so highlighting a paragraph in column 2 of a four-column grid takes
columns 1 and 3 with it on every row. `deferred.md`'s standing suspicion that cell-motion mode
costs more than it buys is about the *mode*, not about owning the mouse, and §9 answers it with
a measurement rather than a concession.

---

## 5. The selection value

> **As built:** the ordered range is the type `marked` and the method is
> `selection.marked()`, not `span()`. `internal/ui/roster.go` already has a `span` — the slice of
> agents a column has room for — and two unrelated types of that name in one package is the
> collision the compiler caught on the first build. Read every "span" below as `marked`.

```go
// selection is a live drag, or the highlight one left behind.
type selection struct {
    pane   string // DM id; "" is the room
    anchor point  // where the drag started
    head   point  // where the pointer is now
}

// point is a place in a transcript: an absolute line index, and a
// pane-local column.
type point struct{ line, col int }
```

`App.sel` holds one. There is one selection in the app, not one per pane — a drag that starts in
`bob` and a drag that starts in the room are the same gesture arriving twice, and the second
replaces the first.

**`line` is an absolute index into `transcript.lines`, not a screen row.** `chunked` is
append-only and never renumbers, so a selection needs no maintenance as events arrive. This is
what makes the ruling in §8.3 — that a selection floats with the scroll — the *cheap*
implementation rather than a compromise: the anchor keeps pointing at the line it was taken on,
the pane keeps following the bottom as it does today, and the highlight rides up the screen
because the screen moved under it.

`col` is pane-local (0 at the pane's left edge), because the mouse handler resolves terminal `x`
through `Layout.Hit` and the pane's own bounds before it ever reaches here. Column scoping is
therefore free: a `col` outside `[0, paneWidth)` is clamped, and `selection.scope` in §2's
listing is a thing wake gets by construction rather than a field it has to carry.

**Normalized on read, never on write.** `anchor` is where the drag began and `head` is where the
pointer is; a drag upward has `head` before `anchor` and that is correct. A `span()` method
returns them ordered by `(line, col)` for drawing and extraction. Storing them pre-ordered would
lose which end the pointer is on, which is the end a continued drag has to move.

**Half-open, and the pointer's own cell is inside.** A span is `[start, end)` in both axes,
because that is what `ansi.Cut(s, left, right)` takes and converting at each of the three call
sites is how one of them ends up converting twice. The cell *under* the pointer is part of the
selection — an operator dragging across four characters expects four — so `span()` adds one to
whichever column is the later of the two. Anchor and head are stored as the cells they are, and
only `span()` knows about the half-open convention.

`selection` is a value with value-receiver methods returning new values, like `Room`,
`transcript` and `Layout` before it.

---

## 6. The mouse

Three cases join `App.mouse`, all inside the branch where `Layout.Hit` already answers
`RegionPane` and `App.paneAt` already resolves which conversation a row belongs to. Nothing
about the divider drag or the rule drag changes: `a.dragAt != noDrag` claims motion first and a
divider is a different region, so the two gestures cannot both be live.

| Event | Behaviour |
|---|---|
| **press** in a pane | Focus that pane, exactly as today. Set `anchor = head` at the hit point. **No selection yet** — a press is not a drag. |
| **motion** with a press held | `head` = hit point, clamped to the origin pane's column and row bounds. Past the top or bottom edge, scroll that pane so the drag reaches scrollback. |
| **release** | If `head != anchor`, extract and copy, and leave the highlight up. If `head == anchor` it was a click: focus only, nothing selected, nothing copied. |

**The click case is the load-bearing one.** Click-to-focus is how a two-pane window whose room
could never take a keystroke got fixed, and a selection that fired on every click would put a
one-cell string on the clipboard every time somebody changed panes. The discriminator is
`head != anchor` and nothing else — not a timer, not a distance threshold.

**A drag that leaves the pane is clamped, not abandoned.** Dragging into the next column
continues the selection at the origin pane's right edge. This is `scope` doing its job, and it is
also the only behaviour that lets somebody select a full-width line without landing exactly on
the last cell.

**Release may report `MouseButtonNone`.** X10 encoding loses which button was let go of; the
existing release case already handles this for the divider drag and the same case handles this.

---

## 7. Drawing the highlight

`transcript.view()` gains one parameter: a span already resolved for *this* transcript, zero
value meaning none. There are two steps and they belong to different owners — `selection.span()`
orders the two ends (§5), and `App` decides whether the pane being drawn is the one `App.sel.pane`
names, passing the zero span to every other pane. So `transcript` stays a pure value type that
knows nothing about panes, ids or which one has the focus, and the room and a DM share the one
implementation rather than growing two.

For each visible line inside the span:

1. `ansi.Cut(line, 0, c0)` — the part before.
2. `ansi.Cut(line, c0, c1)` — the selected part.
3. `ansi.Cut(line, c1, width)` — the part after.

The middle is **stripped and re-rendered** in the theme's selection style, not wrapped in a
background. This is deliberate and it is the one visible compromise in the design: the blocks
contain SGR resets — glamour emits one at the end of every span — and a background applied
around a string containing a reset ends at that reset, so the highlight would break into stripes
mid-word. Stripping first guarantees a solid highlight at the cost of syntax colour *inside* the
selection, which is what a terminal's own selection does anyway.

The span is linear, not rectangular: first line from `c0` to end, middle lines whole, last line
from 0 to `c1`. A rectangular selection is a different feature with a different gesture.

The selection style is a new entry in `internal/ui/theme.go`, beside the existing palette. It
does not come from `claude-palette.json` — that file is asserted against Claude Code's extracted
palette by `palette_test.go`, and this is wake's own colour.

---

## 8. What clears a selection — three rulings

### 8.1 Any keystroke clears it, and is then handled normally

`esc` clears the highlight **and interrupts on the same press**. So does every other key.

The tempting alternative — `esc` clears a live selection and does nothing else, the way it
dismisses a picker — is wrong here and the reason is a fleet reason. `esc` is how an operator
stops a runaway agent. A stale highlight from a copy made two minutes ago would swallow that
press, and the operator would be looking at an agent that did not stop. The copy has already
happened by then; a lingering highlight has no function beyond showing what was taken, so it has
nothing to defend.

This is the card-arming precedent read the same way: *every input that is not the confirm takes
the arm back*. Here there is no confirm, so every input takes it back.

### 8.2 A width change clears it; a height change does not

A re-wrap renumbers `transcript.lines`, so held indices stop pointing at the text they were taken
on. Clearing is the only honest answer — a selection silently sliding onto different words is
worse than one that vanishes.

This is the existing rule, applied: *only a width change returns a reader to the newest line*,
for exactly the same reason. A height change re-wraps nothing and keeps the selection.

The clear happens where the re-wrap does, on the far side of the 80ms settle in
`internal/ui/geometry.go`, so a drag across 40 columns clears once rather than forty times.

### 8.3 A selection floats with the scroll

New events keep appending and the pane keeps following the bottom exactly as it does today. The
highlight rides up the screen with the text it was taken on and off the top edge if enough
arrives. It stays live and extendable while it is off screen.

The rejected alternative was pinning the pane while a selection is live. It is defensible — it
is what `autoScroll` sitting next to `copyOnSelect` in Claude Code's config suggests they do —
but it makes a selection a mode that silently stops a pane from following its agent, and in a
room of thirty that is a pane you will misread. Owner's call, 2026-08-14.

Note this does **not** fight the drag-past-the-edge scroll in §6: `transcript.Append` samples
`atBottom` before it appends, so a drag that scrolls up leaves the pane held, which is the
existing reader-has-scrolled-up behaviour and needs nothing new.

### And nothing else

A new event does not clear it (§8.3). A pane focus change does not clear it — only the press
that caused the focus change does, by starting a new one. Closing the pane it lives in does:
`App.sel.pane` names a conversation that is gone, and the resolve step drops it.

---

## 9. The clipboard, and the one real hazard

Extraction: for each line in the span, `ansi.Strip` the cut piece, trim the trailing pad lipgloss
adds to every line it renders to a width, and join with `\n`. Trailing-pad trimming is not
cosmetic — every line in a wake pane is padded to the pane width, so without it a two-word copy
arrives as two words and forty spaces.

The write is layered, in Claude Code's order and for Claude Code's reasons:

1. `pbcopy` on darwin — the only one that matters when wake runs on the machine you are sitting at.
2. `tmux load-buffer -w -` when `$TMUX` is set.
3. OSC 52 via `ansi.SetSystemClipboard`, DCS-wrapped under tmux and chunked under screen.

Layers 1 and 2 are subprocess writes and need no access to the terminal, so they live in a
`tea.Cmd` and are trivially testable. **Layer 3 is the hazard.** OSC 52 has to reach the terminal
and Bubble Tea owns stdout.

`converseModel` in `cmd/wake/attach.go` is the single `tea.NewProgram` site — both callers route
through it, which is why it was extracted — so it gains `tea.WithOutput(w)`, where `w` is a
mutex-guarded writer wake owns and the clipboard path can inject a sequence into between frames.

**That writer must embed `*os.File` and expose `Fd()`.** Bubble Tea and termenv detect a terminal
by type-asserting the output to a file and calling `IsTerminal(fd)`. A plain `io.Writer` wrapper
fails that assertion silently and the whole app loses its colour — every style in `theme.go`
degrades to plain text and nothing errors. This is written down because it is the exact shape of
failure this project keeps recording: green tests, working feature, and a screen nobody looked
at. `screen_unix_test.go` asserts colour is still being emitted after the change.

A clipboard write that fails is reported through `internal/notice`, never to stderr, and never
by crashing the render loop.

---

## 10. Cost

`deferred.md` already suspects cell-motion mode costs more than it buys: motion arrives once per
cell crossed, each one running `Update` and a `View`. A selection drag now also redraws a pane
per message.

The bound is the argument: the highlight touches only the lines `chunked.slice` already returned,
so the added work is O(pane height) — three `ansi.Cut` calls and one restyle per selected visible
line — and is independent of how long the conversation is. This is the same bound the transcript
was built to hold, and it is categorically cheaper than the divider drag's problem, which was a
*re-wrap* of the whole transcript and is why the 80ms settle exists at all.

**The selection drag deliberately does not go through the settle.** A highlight that lagged 80ms
behind the pointer would feel broken, and unlike a width drag there is nothing expensive on the
far side to defer.

A number nothing asserts is wrong by default, so this section claims no figure. The pty harness
measures a drag across a full pane and the result lands in `docs/notes/decisions.md`. If it comes
back bad, the fix is to narrow the redraw to the affected pane, not to remove the feature.

---

## 11. Testing

| Layer | What |
|---|---|
| `selection` | Table tests: span normalization for all four drag directions, clamping past every pane edge, the click case (`head == anchor` yields nothing). Pure function, no App. |
| extraction | Table tests over synthetic ANSI lines: single-line, multi-line, full-line, trailing-pad trimming, wide runes. |
| clipboard | The layer choice against a fake environment (`$TMUX` set and unset, darwin and not); OSC 52 byte-for-byte, including the tmux DCS wrap and the screen chunking. |
| `internal/ui` | `frame_test.go`-style: the selection style lands on exactly the right cells and on no others; a width change clears; a height change does not; a keystroke clears and still does its own job. |
| screen | The real pty (`cmd/wake/screen_unix_test.go`): a real drag across a real pane; the OSC 52 bytes actually arriving at the terminal; **colour still being emitted after `tea.WithOutput`**. CLAUDE.md sends anything about layout, keys or the mouse here, and §9's hazard is invisible anywhere else. |

Coverage gate is 80% as everywhere else. No test spends money and none needs a live LLM: a
selection is over rendered lines, and rendered lines come from recorded fixtures.

---

## 12. Files

| File | Change |
|---|---|
| `internal/ui/selection.go` | **New.** The value, `span()`, clamping, extraction. Pure. |
| `internal/ui/clipboard.go` | **New.** The three layers, the environment probe, the OSC 52 encoding. |
| `internal/ui/mouse.go` | Press sets the anchor; motion extends; release copies. |
| `internal/ui/transcript.go` | `view()` takes a span and applies the highlight. |
| `internal/ui/app.go` | Holds `sel`; resolves it per pane at draw; clears it per §8. |
| `internal/ui/theme.go` | The selection style. |
| `cmd/wake/attach.go` | `tea.WithOutput` and the guarded writer. |
| `docs/live-testing.md` | §4's open question becomes a checklist item with an answer. |
| `CLAUDE.md` | A load-bearing-rules entry, and a Key locations row. |

Every file stays well inside the 800-line hard max, and `internal/daemon/spawn.go` and
`internal/ui/app.go` remain the two largest non-test files, so
`TestCLAUDEmdNamesTheTwoLargestNonTestFiles` does not move.

No new key, so `legendEntries` and its bijection test are untouched and the 282-column legend
budget is unaffected. This is a feature with no keyboard surface at all.

---

## 13. Deferred

Recorded here so they are choices rather than omissions:

- **Keyboard selection** — `moveSelectionFocus`'s equivalent, and an explicit copy key. Blocked
  on legend budget more than on effort.
- **Double-click a word, triple-click a line.** Wants a timer, and a timer is the thing this
  project spends the most care avoiding.
- **Copying the source text rather than the screen text.** Would give back unwrapped lines and
  raw markdown. Needs a wrapped-line → source-block index that does not exist, and `blockLines`
  splits on `\n` *after* lipgloss and glamour have already wrapped, so soft wraps and real
  newlines are indistinguishable by then. Screen text is what you see and what you get.
- **A `copyOnSelect` setting.** Claude Code has one. Wake has no settings surface at all yet, and
  adding one for a single boolean is the wrong order to build it in.
