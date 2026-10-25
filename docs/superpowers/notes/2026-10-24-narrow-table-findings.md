# Narrow tables: how Claude Code draws a markdown table that does not fit

Observed 2026-10-24 against **Claude Code 2.1.310**, interactive, model haiku, cwd `/tmp/…`. Nothing
from the session is committed. This note describes the behaviour Wake matches, which is maintained
by hand.

## Provenance

A throwaway interactive `claude` ran in a pty that a scratch harness read through `vt10x` (the
emulator the screen tests use). The harness resized it with `TIOCSWINSZ`, which is the
`SIGWINCH` a real terminal sends. The model was asked to repeat a fixed reply verbatim, made of
paragraphs and six small tables, so every width drew the same source:

- **1**: three columns of one-word cells (`Name | Role | Status`, five to eight letters each)
- **2**: `Flag | Default | What it does`, with one 80-character cell and one 35-character cell
- **C** and **E**: `Key | Value`, with one cell of 20 and 10 six-letter words
- **F**: `k | v`, one-letter cells
- **G**: a code-span header, a bold header, an empty cell, a link and `&`

Each table was swept one column at a time across its switch. Widening back to 120 was checked too.

## Wide: a bordered grid

Claude Code draws a grid with an outer border and a rule between every row. Each cell has one
space of padding. Header text is centred whatever the column's alignment. Body cells follow the
alignment row (`:---`, `:---:`, `---:`), and a short cell in a tall row is centred vertically.
The grid is as wide as its content and never stretches to the terminal. The table budget is the
terminal width less four: the two-cell `⏺` gutter, then two more. Table 1 (30 cells) drew at its
natural width at 34 columns and shrank at 32.

## Shrinking: the grid wraps before it switches

Below its natural width the grid stays a grid and its columns shrink:

- **Each column keeps its longest word while it can.** The width left over goes to the columns that
  still want more, in proportion to how much more each one wants. In table 2 at 50 columns,
  `Flag` (8) and `Default` (7) kept their natural widths and `What it does` took the remaining 21.
- **When even the longest words do not fit, words break.** Cells hard-wrap mid-word
  (`reviewer` → `reviewe`/`r`). The columns scale down in proportion to their natural widths and
  never go below **three cells**.

## The switch: records

The grid becomes **records** when either of these holds:

- **a row would need more than four lines** at those column widths, or
- **the columns cannot each keep three cells**.

| Table | Last grid width | First records width | Which limit |
|---|---|---|---|
| E | 34 (cell at 20: 4 lines) | 33 (cell at 19: 5 lines) | four lines |
| 2 | 50 (cell at 21: 4 lines) | 48 (cell at 19: 5 lines) | four lines |
| C | 49 (cell at 35: 4 lines) | 48 | four lines, one column early |
| F | 17 (3 + 3 cells) | 16 | three cells |
| 1 | 24 | 22 | three cells |
| G | 26 | 22 | three cells |

Table C switches one column earlier than the rule says: at 48 its cell is 34 wide and holds five of
its words a line, which is four lines. Claude Code wraps a cell's text twice: once at a wider
measure, then again at the column. That second wrap is visible in table E at 34 columns, where a
continuation opens with a space. The extra line most likely comes from there. The other sweeps
match the rule exactly.

**Widening switches back.** The layout is a function of the width at each redraw, with no memory
of what was drawn before.

## Records layout

Each body row becomes one record, with one line per column:

- **The header text, then a colon, both in bold**, then a space, then the cell's text. The cell is
  drawn as inline markdown, so bold stays bold and a link shows its text. A code-span header is
  bold like any other.
- **An empty cell keeps its label**: `Kind:` with nothing after it.
- **Alignment is dropped.** Every value starts after its label, including the values of a
  right-aligned column.
- **A long value wraps at the pane width. Its continuation lines start at the left margin**, under
  the label. They are not hung under the value, and the label is not repeated.
- **No blank line between fields.** Between two records there is a rule of `─`, 40 cells long, or
  shorter on a narrow terminal. There is no rule before the first record or after the last. The
  block has a blank line above and below it, as any block does.
- The header row is never drawn as a record of its own.

## Quirks not copied

- **The rule overflows its gutter.** It is `min(40, terminal − 1)` cells long but is drawn inside
  the two-cell gutter. Below 42 columns its last `─` therefore wraps onto a row of its own.
- **A value is wrapped twice.** It is wrapped one cell wider than its box, then wrapped again by the
  box. A word can end up alone on a row (`…the model the` / `session` / `runs on` at 42 columns),
  and a continuation can keep a leading space.

## Wake on `main` before this change

Wake's grid is glamour v1.0.0's. Glamour hands the table to lipgloss's table with the pane's width:

- **It stretches the table to the full pane.** There is no outer border and no rule between rows.
- **A link in a cell becomes a numbered footnote** (`docs[1]`), listed under the table.
- **When the table is narrower than its natural width**, lipgloss shrinks the biggest columns
  first, then the ones furthest above their median. It wraps body cells, breaks words, and **cuts
  a header with `…`** (`What it do…` at 40 columns). Lower still it **cuts body cells outright**:
  table 1 at 30 columns drew `workin` and `blocke`.

## What Wake does with it

`internal/render/tables.go` draws a top-level table as records whenever glamour's grid would not
show it whole: a header cut, a word broken or cut, a link's address cut in the list under the
grid, or a row past Claude Code's four lines. Below the point where glamour starts dropping text,
its grid is never kept. That is earlier than Claude Code, which breaks words inside its own grid.
The reasoning is in `docs/notes/decisions.md` (2026-10-24).

The records follow the layout above with one change: the rule is never wider than the text
(40, or the pane's text width when that is less).

| Table | Claude Code: last grid / first records | Wake: last grid / first records |
|---|---|---|
| E (Key/Value) | 34 / 33 | 34 / 33 |
| 2 (Flag/Default) | 50 / 48 | 50 / 49 |
| 1 (Name/Role/Status) | 24 / 22 | 31 / 30 |
