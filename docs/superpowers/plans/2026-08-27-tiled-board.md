# Tiled Board (a view-only live wall) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give `/board` a second presentation — a grid of bordered, view-only tiles, each showing an agent's state, live output tail, and running-subagent count — toggled with `⇥`, jumped into with `↵`/click.

**Architecture:** A `Board.Tiled bool` selects a second render path in `boardView`; no new modal. Each tile's live text comes from a new **App-level** map (`App.tails`, keyed by session id, holding the existing `partial` type) folded in `App.observe` and **gated on the tiled board being up** — the same "pay only for what's on screen" rule `App.wants` applies to DM previews, kept off `Fleet` so a streamed token never triggers a fleet-sized copy. Geometry (columns, 2-D cursor, windowing) is a set of pure functions with table tests. `↵`/click open the agent's DM as a new column via the existing `openRight` (which already dedups and focuses).

**Tech Stack:** Go 1.26+, Bubble Tea / Lipgloss (Charm), the in-tree `internal/ui` model. Tiles reuse `titledBox` (rounded border) and the `partial` preview type.

**Spec:** `docs/superpowers/specs/2026-08-27-tiled-board-design.md` — the plan argues from it; executors read both.

## Global Constraints

- **Gate:** `make ci` exit 0 is the only gate (GitHub Actions is unfunded). Run it before opening the PR, not after; say the exit code in the PR body.
- **TDD, red first.** Write the failing test, run it, see it fail, then implement. 80% coverage floor (`make cover`).
- **`make test` runs the suite twice** — with `-race` and without. A green race run is not evidence alone.
- **`internal/ui` never spells Claude's vocabulary.** Use `rpc.State*` / `core.Kind*` constants, never string literals for states or event kinds.
- **Files: 200–400 typical, 800 hard max.** `board.go` is 333 lines; the tile renderer and geometry go in a **new** file `internal/ui/boardtile.go`, not appended to `board.go`.
- **Every agent-authored field passes through `oneLine`** before it is drawn (`boardRow`'s rule): a control byte in `Doing`, `Tool`, `LastLine` or a live tail must not redraw or forge a tile.
- **No parallel implementations.** Extend `board.go`/`partial` in place; reuse `titledBox`, `openRight`, `partial`. Do not write a second box-drawer or a second preview accumulator.
- **Surgical changes, brief comments** (one or two lines on *why*, not *what*). No speculative options.
- **No Claude attribution** in commits or the PR (no `Co-Authored-By`, no generated-with footer).
- **Branch:** `feat/tiled-board` (already created; the spec is committed on it).
- **Run Wake only with `WAKE_SOCKET` set** if a step ever launches the binary; `make` targets already export a scratch socket.

---

### Task 1: The live-tail store (App-level, gated)

The data layer for the tiles' live text. A new `App.tails map[string]partial` accumulates each agent's streamed output **only while the tiled board is up**, reusing the `partial` type verbatim. No UI in this task.

**Files:**
- Modify: `internal/ui/app.go` — add `tails` field to `App` (near `dms`, app.go:178); add `withTail` (mirror `withDM` at app.go:776); add `foldTail`; call `foldTail` in `observe` (app.go:692, after the `a.dms[sessionID]` append near app.go:753).
- Modify: `internal/ui/board.go` — `closeBoard` drops the tails.
- Test: `internal/ui/boardtail_test.go` (new).

**Interfaces:**
- Consumes: the existing `partial` type (`partial.add(string) partial`, `partial.cleared() partial`, field `partial.text`), and `core.Event` with `.Kind` / `.Text`. Gate reads `a.board.Up` and the not-yet-added `a.board.Tiled` — this task adds `Tiled` to the `Board` struct as an unused field so the gate compiles; Task 3 wires the toggle.
- Produces: `func (a App) foldTail(sessionID string, ev core.Event) App`, `func (a App) withTail(id string, p partial) App`, and the map `a.tails[id] partial` (zero value for an agent with no live tail) that Task 3's renderer reads.

- [ ] **Step 1: Add the `Tiled` field and the `tails` map (compile-only scaffolding)**

In `internal/ui/board.go`, add the field to `Board`:

```go
type Board struct {
	Up       bool
	Selected string
	// Tiled draws the fleet as a grid of live tiles rather than one row per
	// agent. The row view is the default; ⇥ toggles (Task 3).
	Tiled bool
}
```

In `internal/ui/app.go`, add to the `App` struct beside `dms`:

```go
	// tails is each agent's live output tail while the tiled board is up,
	// keyed by session id and holding the same partial preview a DM shows.
	// On App (not Fleet) so a streamed token never triggers a fleet-sized
	// copy - App.wants' own reason, one surface over. Empty when the wall is
	// down: foldTail is gated and closeBoard drops it.
	tails map[string]partial
```

- [ ] **Step 2: Write the failing test for the gate and the clears**

Create `internal/ui/boardtail_test.go`:

```go
package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func partialEv(text string) core.Event {
	return core.Event{Kind: core.KindPartialText, Text: text}
}

func TestTheTailAccumulatesOnlyWhileTheTiledBoardIsUp(t *testing.T) {
	base := App{}
	cases := []struct {
		name string
		up   bool
		tile bool
		want string
	}{
		{"board down", false, false, ""},
		{"rows up, not tiled", true, false, ""},
		{"tiled up", true, true, "hello"},
	}
	for _, tc := range cases {
		a := base
		a.board = Board{Up: tc.up, Tiled: tc.tile}
		a = a.foldTail("s1", partialEv("hello"))
		if got := a.tails["s1"].text; got != tc.want {
			t.Errorf("%s: tail = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestTheTailClearsWhenTheBlockLands(t *testing.T) {
	a := App{board: Board{Up: true, Tiled: true}}
	a = a.foldTail("s1", partialEv("half a sen"))
	a = a.foldTail("s1", core.Event{Kind: core.KindAssistantText, Text: "half a sentence"})
	if got := a.tails["s1"].text; got != "" {
		t.Errorf("tail after the block landed = %q, want empty", got)
	}
}

func TestTheTailClearsWhenTheTurnEnds(t *testing.T) {
	a := App{board: Board{Up: true, Tiled: true}}
	a = a.foldTail("s1", partialEv("interrupted mid"))
	a = a.foldTail("s1", core.Event{Kind: core.KindTurnEnd})
	if got := a.tails["s1"].text; got != "" {
		t.Errorf("tail after turn end = %q, want empty", got)
	}
}

func TestFoldingATailDoesNotTouchTheFleet(t *testing.T) {
	a := App{board: Board{Up: true, Tiled: true}, fleet: NewFleet()}
	before := a.fleet
	a = a.foldTail("s1", partialEv("tokens"))
	// The fleet value is unchanged: foldTail writes only App.tails, so a
	// streamed token costs no fleet-sized copy. Same maps, same pointers.
	if !sameFleet(before, a.fleet) {
		t.Fatal("foldTail copied or mutated the fleet; tails must live off it")
	}
}
```

Add the `sameFleet` helper at the bottom of the test file (agents map identity is the check that matters — a copy would allocate a new map):

```go
func sameFleet(x, y Fleet) bool {
	return fmt.Sprintf("%p", x.agents) == fmt.Sprintf("%p", y.agents)
}
```

(Add `"fmt"` to the imports.)

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/ui/ -run 'TestTheTail|TestFoldingATail' -v`
Expected: FAIL — `foldTail` / `withTail` undefined.

- [ ] **Step 4: Implement `withTail`, `foldTail`, and the `observe` call**

In `internal/ui/app.go`, add beside `withDM`:

```go
// withTail returns an App whose tails map is its own, one entry replaced.
// Mirrors withDM: the map is tiny (partial is two strings and an int, and
// the strings are bounded by partial.add), so copying it per token is cheap.
func (a App) withTail(id string, p partial) App {
	next := make(map[string]partial, len(a.tails)+1)
	maps.Copy(next, a.tails)
	next[id] = p
	a.tails = next
	return a
}

// foldTail accumulates one agent's live output tail while the tiled board is
// up, and clears it when the block lands or the turn ends - the DM preview's
// own triggers (dm.go), off the same partial type. Gated: when the wall is
// down or in rows, this is a no-op and App.tails stays empty.
func (a App) foldTail(sessionID string, ev core.Event) App {
	if !a.board.Up || !a.board.Tiled {
		return a
	}
	switch ev.Kind {
	case core.KindPartialText:
		return a.withTail(sessionID, a.tails[sessionID].add(ev.Text))
	case core.KindAssistantText, core.KindTurnEnd:
		if a.tails[sessionID].text == "" {
			return a
		}
		return a.withTail(sessionID, a.tails[sessionID].cleared())
	}
	return a
}
```

In `observe` (app.go:692), immediately after the DM-append block (the `if dm, ok := a.dms[sessionID]; ok && a.wants(...)` at app.go:753):

```go
	a = a.foldTail(sessionID, ev)
```

- [ ] **Step 5: Implement the close-clears-tails half**

In `internal/ui/board.go`, `closeBoard`:

```go
func (a App) closeBoard() App {
	a.board = Board{}
	a.tails = nil
	return a
}
```

Add the failing test first, in `boardtail_test.go`:

```go
func TestClosingTheBoardDropsTheTails(t *testing.T) {
	a := App{board: Board{Up: true, Tiled: true}}
	a = a.foldTail("s1", partialEv("some output"))
	a = a.closeBoard()
	if len(a.tails) != 0 {
		t.Errorf("tails after close = %d entries, want 0", len(a.tails))
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestTheTail|TestFoldingATail|TestClosingTheBoardDropsTheTails' -v`
Expected: PASS (all five).

- [ ] **Step 7: Commit**

```bash
git add internal/ui/app.go internal/ui/board.go internal/ui/boardtail_test.go
git commit -m "feat: live-tail store for the tiled board, gated on the wall being up"
```

---

### Task 2: Tile geometry (pure functions)

The column count, the 2-D cursor step, and the row-windowing — all pure, all table-tested, so the renderer and the mouse share one arithmetic (`boardChromeRows`' rule: draw and mouse measure one number).

**Files:**
- Create: `internal/ui/boardtile.go` — geometry constants and the three pure functions.
- Test: `internal/ui/boardtile_test.go` (new).

**Interfaces:**
- Consumes: nothing from earlier tasks; `clamp` and `max` already exist in the package.
- Produces:
  - `func tileColumns(width int) int` — tiles across, ≥ 1.
  - `func tileCellWidth(width, cols int) int` — each tile's width.
  - `type tileDir int` with `tileUp, tileDown, tileLeft, tileRight`.
  - `func tileNav(cursor, cols, total int, dir tileDir) int` — new cursor index, no wrap.
  - `func tileWindowStart(cursor, total, cols, visibleRows int) int` — index of the first drawn tile.
  - `func tileHeight() int` — rows one tile occupies (border + body).
  - constants `minTileWidth`, `tileGap`, `tileBodyRows`.

- [ ] **Step 1: Write the failing table tests**

Create `internal/ui/boardtile_test.go`:

```go
package ui

import "testing"

func TestTileColumns(t *testing.T) {
	cases := []struct{ width, want int }{
		{0, 1},    // never zero columns
		{30, 1},   // one tile plus slack
		{60, 2},
		{120, 4},
	}
	for _, tc := range cases {
		if got := tileColumns(tc.width); got != tc.want {
			t.Errorf("tileColumns(%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

func TestTileNavDoesNotWrap(t *testing.T) {
	// A 2x2-ish grid over 5 agents (cols=2): indices 0..4.
	cases := []struct {
		name              string
		cursor            int
		dir               tileDir
		want              int
	}{
		{"right within row", 0, tileRight, 1},
		{"right at row edge stays", 1, tileRight, 1},
		{"left at row start stays", 0, tileLeft, 0},
		{"down a row", 0, tileDown, 2},
		{"up a row", 2, tileUp, 0},
		{"up from top stays", 1, tileUp, 1},
		{"down past the end stays", 4, tileDown, 4},
		{"right onto nonexistent last stays", 4, tileRight, 4},
	}
	for _, tc := range cases {
		if got := tileNav(tc.cursor, 2, 5, tc.dir); got != tc.want {
			t.Errorf("%s: tileNav(%d,2,5,%v) = %d, want %d", tc.name, tc.cursor, tc.dir, got, tc.want)
		}
	}
}

func TestTileWindowStartRidesTheCursorRow(t *testing.T) {
	// cols=2, visibleRows=2 → 4 tiles on screen. 10 agents = 5 rows.
	cases := []struct{ cursor, want int }{
		{0, 0},  // first row: window at 0
		{2, 0},  // second row still fits
		{4, 2},  // third row: window slides down one row (start index 2)
		{9, 6},  // last agent: last two rows (start index 6)
	}
	for _, tc := range cases {
		if got := tileWindowStart(tc.cursor, 10, 2, 2); got != tc.want {
			t.Errorf("tileWindowStart(%d,10,2,2) = %d, want %d", tc.cursor, got, tc.want)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestTile(Columns|Nav|WindowStart)' -v`
Expected: FAIL — undefined `tileColumns`, `tileNav`, `tileWindowStart`, `tileDir`.

- [ ] **Step 3: Implement the geometry**

Create `internal/ui/boardtile.go`:

```go
package ui

// The tiled board: the fleet as a grid of view-only tiles rather than a column
// of rows. A second presentation of the same overview, toggled by ⇥ - not
// panes you operate. See board.go's header for the narrowed §2c ruling and the
// four guardrails that keep this a status wall.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// minTileWidth is the narrowest a tile can be and still hold a name in the
	// border edge and a state word under it. Below one tile's worth, the grid
	// draws a single column. Tuned against the pty harness (spec §9).
	minTileWidth = 26

	// tileGap is the blank column between neighbouring tiles.
	tileGap = 1

	// tileBodyRows is the fixed inner height every tile pads to, so a wall of
	// tiles is a regular grid: a state line, up to maxPreviewRows of live tail,
	// and the subagent count. Tuned against the pty harness.
	tileBodyRows = maxPreviewRows + 2
)

// tileHeight is the rows one tile occupies: its body plus the rounded border's
// top and bottom.
func tileHeight() int { return tileBodyRows + 2 }

// tileColumns is how many tiles fit across a width, at least one.
func tileColumns(width int) int {
	return max((width+tileGap)/(minTileWidth+tileGap), 1)
}

// tileCellWidth is each tile's width once the column count is chosen: the frame
// split evenly, less the gaps between tiles.
func tileCellWidth(width, cols int) int {
	if cols < 1 {
		return width
	}
	return (width - tileGap*(cols-1)) / cols
}

type tileDir int

const (
	tileUp tileDir = iota
	tileDown
	tileLeft
	tileRight
)

// tileNav is the cursor one step in the grid, without wrapping: up and down
// move a whole row (± cols), left and right move one, and a step off an edge or
// past the last agent stays put.
func tileNav(cursor, cols, total int, dir tileDir) int {
	switch dir {
	case tileUp:
		if cursor-cols >= 0 {
			return cursor - cols
		}
	case tileDown:
		if cursor+cols < total {
			return cursor + cols
		}
	case tileLeft:
		if cursor%cols != 0 {
			return cursor - 1
		}
	case tileRight:
		if cursor%cols != cols-1 && cursor+1 < total {
			return cursor + 1
		}
	}
	return cursor
}

// tileWindowStart is the index of the first tile drawn, so the cursor's row is
// on screen. It pages by whole rows of `cols`, the cursor riding the bottom
// edge once it is past the first window - boardWindowStart in two dimensions.
func tileWindowStart(cursor, total, cols, visibleRows int) int {
	row := cursor / cols
	totalRows := (total + cols - 1) / cols
	startRow := clamp(row-visibleRows+1, 0, max(totalRows-visibleRows, 0))
	return startRow * cols
}
```

(The `lipgloss`, `ansi`, `strings`, `rpc` imports are used by the renderer in Task 3; if this task's build complains about unused imports, add them in Task 3 instead — keep only what compiles. Simplest: add the imports in Task 3's step when `tile`/`tileView` land, and here import nothing.)

Correction for this task: create `boardtile.go` with **no imports** and only the geometry above (remove the import block). Task 3 adds the imports with the renderer.

- [ ] **Step 4: Run to verify passing**

Run: `go test ./internal/ui/ -run 'TestTile(Columns|Nav|WindowStart)' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/boardtile.go internal/ui/boardtile_test.go
git commit -m "feat: pure tile geometry for the board grid (columns, 2-D cursor, windowing)"
```

---

### Task 3: The tile, the tiled view, and the ⇥ toggle

Render the grid, and let `⇥` flip between rows and tiles. A tile is a rounded box (`titledBox`) with the agent's name in the top edge and, by state, the live tail / the ask / the last line, plus a subagent count.

**Files:**
- Modify: `internal/ui/boardtile.go` — add `tileView`, `tile`, `tileBody`, and the imports.
- Modify: `internal/ui/board.go` — `boardKey` claims `⇥`; `boardView` branches on `Tiled`; `boardKeyLine` mentions `⇥`.
- Test: `internal/ui/boardtile_test.go` — render assertions; `internal/ui/board_test.go` — the toggle.
- Test (benchmark): `internal/ui/boardtile_test.go` — `BenchmarkTiledBoardFleetSecond`.

**Interfaces:**
- Consumes: Task 1's `a.tails[id] partial`; Task 2's `tileColumns`, `tileCellWidth`, `tileWindowStart`, `tileHeight`; existing `boardDetail`, `rowGlyph`, `labelOf`, `titledBox`, `oneLine`, `AccentStyle`/`warnStyle`/`TextStyle`, `mutedLine`, `ellipsis`, `a.fleet.RunningTasks(id)`, `a.boardCursor`, `a.paneHeight`.
- Produces: `func (a App) tileView(agents []Agent, width int) string`, and `boardView` returning it when `a.board.Tiled`.

- [ ] **Step 1: Write the failing toggle test** (in `internal/ui/board_test.go`, reusing the file's existing `boardApp` helper)

```go
func TestTabTogglesRowsAndTiles(t *testing.T) {
	a := boardApp(t)
	if a.board.Tiled {
		t.Fatal("the board opened in tiles; rows are the default")
	}
	next, _, handled := a.boardKey(tea.KeyMsg{Type: tea.KeyTab})
	if !handled {
		t.Fatal("⇥ was not claimed by the board")
	}
	if !next.board.Tiled {
		t.Fatal("⇥ did not switch the board to tiles")
	}
	back, _, _ := next.boardKey(tea.KeyMsg{Type: tea.KeyTab})
	if back.board.Tiled {
		t.Fatal("a second ⇥ did not switch back to rows")
	}
}
```

- [ ] **Step 2: Write the failing render tests** (in `internal/ui/boardtile_test.go`)

```go
func TestTiledBoardDrawsARoundedTilePerAgent(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	out := a.View()
	if !strings.Contains(out, "╭") || !strings.Contains(out, "╰") {
		t.Fatalf("the tiled board drew no rounded borders.\n%s", out)
	}
	for _, name := range []string{"sydney", "john", "marcus"} {
		if !strings.Contains(out, name) {
			t.Fatalf("no tile for @%s.\n%s", name, out)
		}
	}
}

func TestTileShowsTheLiveTailOfAWorkingAgent(t *testing.T) {
	a := boardApp(t)
	a.board.Tiled = true
	id := a.fleet.OnRoster()[0].ID
	a = a.foldTail(id, partialEv("wiring the auth guard"))
	if !strings.Contains(a.View(), "wiring the auth guard") {
		t.Fatalf("the working tile did not show its live tail.\n%s", a.View())
	}
}

func TestTileShowsSubagentCount(t *testing.T) {
	// boardApp's first agent is given two running subagents by the helper (see
	// boardApp). The tile states the count.
	a := boardApp(t)
	a.board.Tiled = true
	if !strings.Contains(a.View(), "subagent") {
		t.Fatalf("the tile did not state a subagent count.\n%s", a.View())
	}
}
```

> If `boardApp` does not already seat a subagent on an agent, extend it (it is a test helper) to dispatch one `core.Event` with a running `Task` for the first agent, or assert `⤷ 0 subagents` instead. Keep the helper honest — do not assert a count the fixture cannot produce.

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestTab|TestTiled|TestTileShows' -v`
Expected: FAIL — `⇥` not claimed / no tile output.

- [ ] **Step 4: Implement the renderer** — append to `internal/ui/boardtile.go` (and add the import block from Task 2's listing):

```go
// tileView is the whole frame as a grid of tiles: the same title and key line
// the row view draws, with tiles between them in attention order.
func (a App) tileView(agents []Agent, width int) string {
	cols := tileColumns(width)
	cellW := tileCellWidth(width, cols)
	rowsVisible := max((a.paneHeight()-boardChromeRows-1)/tileHeight(), 1)
	cursor := a.boardCursor(agents)
	start := tileWindowStart(cursor, len(agents), cols, rowsVisible)

	head := mutedLine(fmt.Sprintf("%s — %d agents", boardTitle, len(agents)), width)
	body := make([]string, 0, rowsVisible)
	for r := 0; r < rowsVisible; r++ {
		cells := make([]string, 0, cols)
		for c := 0; c < cols; c++ {
			i := start + r*cols + c
			if i >= len(agents) {
				cells = append(cells, strings.Repeat(" ", cellW))
				continue
			}
			ag := agents[i]
			cells = append(cells, a.tile(ag, cellW, i == cursor))
		}
		body = append(body, joinTilesRow(cells))
	}
	key := mutedLine(boardKeyLine, width)
	return head + "\n" + strings.Join(body, "\n") + "\n" + key
}

// joinTilesRow lays one row of tiles side by side with the gap between them.
func joinTilesRow(cells []string) string {
	gap := strings.Repeat(" ", tileGap)
	return lipgloss.JoinHorizontal(lipgloss.Top, interleave(cells, gap)...)
}

// tile is one agent as a rounded box: the name in the top edge, its state and
// what it is doing (or saying) in the body, and its subagent count.
func (a App) tile(ag Agent, width int, cursored bool) string {
	border := TextStyle
	switch {
	case cursored:
		border = AccentStyle
	case ag.State == rpc.StateBlocked:
		border = warnStyle
	}
	head := rowGlyph(ag) + " " + ag.Name
	body := a.tileBody(ag, width)
	return titledBox(body, width, border, oneLine(head), "", border, border)
}

// tileBody is the tile's inner rows: the state word, then what the agent is on
// (a live tail while it works, the ask while it is blocked, its last line when
// idle), then the subagent count - boardDetail's by-state logic, one field
// richer for the live tail. Every agent-authored line goes through oneLine.
func (a App) tileBody(ag Agent, width int) string {
	inner := max(width-boxFrameWidth, 1)
	lines := []string{border(labelOf(ag.State), inner)}

	if ag.State == rpc.StateWorking {
		if tail := a.tails[ag.ID].sized(inner); tail.text != "" {
			lines = append(lines, tail.view)
		} else if d := boardDetail(ag); d != "" {
			lines = append(lines, ansi.Truncate(oneLine(d), inner, ellipsis))
		}
	} else if d := boardDetail(ag); d != "" {
		lines = append(lines, ansi.Truncate(oneLine(d), inner, ellipsis))
	}

	subs := len(a.fleet.RunningTasks(ag.ID))
	lines = append(lines, fmt.Sprintf("⤷ %d subagents", subs))
	return strings.Join(padRows(lines, tileBodyRows), "\n")
}
```

Add two tiny helpers (in `boardtile.go`) if they do not already exist in the package — check first with grep, and reuse the existing one if present:

```go
// interleave puts `sep` between every pair of items and returns the flat slice.
func interleave(items []string, sep string) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, s := range items {
		if i > 0 {
			out = append(out, sep)
		}
		out = append(out, s)
	}
	return out
}

// padRows pads a slice of rows up to n with blank rows, so every tile is the
// same height and the grid stays regular.
func padRows(rows []string, n int) []string {
	for len(rows) < n {
		rows = append(rows, "")
	}
	return rows[:n]
}
```

> `border(text, width)` in `tileBody` renders a muted state line — reuse the package's existing muted renderer. If the helper is named differently (grep for `mutedLine`), use that: `mutedLine(labelOf(ag.State), inner)` returns a padded line; for a non-padded state word use `HintStyle.Render(labelOf(ag.State))`. Pick the existing one; do not add a new style. `boxFrameWidth` is defined in `titledbox.go`.

- [ ] **Step 5: Branch `boardView` and claim `⇥`** — in `internal/ui/board.go`:

In `boardView`, at the top:

```go
func (a App) boardView(agents []Agent, width int) string {
	if a.board.Tiled {
		return a.tileView(agents, width)
	}
	// ... existing row rendering unchanged ...
```

In `boardKey`, add a case before the `KeyUp`/`KeyDown` cases:

```go
	case tea.KeyTab:
		a.board.Tiled = !a.board.Tiled
		if !a.board.Tiled {
			a.tails = nil // rows draw no tails; drop what the wall accumulated
		}
		return a, nil, true
```

Update `boardKeyLine` to advertise it:

```go
	boardKeyLine = "↑↓←→ move  ⇥ rows/tiles  ↵ open  ⌃Y column  ⌃C park  esc close"
```

- [ ] **Step 6: Run to verify passing**

Run: `go test ./internal/ui/ -run 'TestTab|TestTiled|TestTileShows|TestSlashBoard' -v`
Expected: PASS. (`TestSlashBoard*` re-run to confirm the row view is unbroken.)

- [ ] **Step 7: Add the benchmark for visibility**

In `internal/ui/boardtile_test.go`:

```go
// BenchmarkTiledBoardFleetSecond prices a fleet-second of thirty agents
// streaming with the wall up: the fold plus a draw. It is the spec's cost
// gate - read the allocs/op, which must not scale with the token count the
// way a per-token fleet copy would (partial.go's withDM trap).
func BenchmarkTiledBoardFleetSecond(b *testing.B) {
	a := benchTiledApp(b, 30) // 30 agents, board up + tiled; helper below or reuse an existing fleet builder
	const tokensPerSecond = 1300
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for t := 0; t < tokensPerSecond; t++ {
			id := a.fleet.OnRoster()[t%30].ID
			a = a.foldTail(id, partialEv("tok "))
		}
		_ = a.View()
	}
}
```

> Build `benchTiledApp` from the existing fleet test builders (grep `func.*App.*testing` in `internal/ui/*_test.go` — reuse `boardApp`'s construction scaled to 30, or the fleet builder the streaming benchmarks already use). Do not invent a new fleet fixture if one exists.

Run: `go test ./internal/ui/ -run x -bench BenchmarkTiledBoardFleetSecond -benchmem`
Expected: a number; record allocs/op in the PR body. There is no hard threshold — the guard against the copy trap is `TestFoldingATailDoesNotTouchTheFleet` (Task 1).

- [ ] **Step 8: Commit**

```bash
git add internal/ui/boardtile.go internal/ui/board.go internal/ui/boardtile_test.go internal/ui/board_test.go
git commit -m "feat: render the tiled board and toggle it with tab"
```

---

### Task 4: 2-D cursor, open-as-column, and the tile mouse

Make `↑↓←→` walk the grid in tiles, make `↵`/click open the agent's DM as a **new column** (keeping the room), and hit-test a click against tile geometry. These key/mouse changes apply in **both** views — the row board's `↵` also becomes open-as-column, per the spec's table.

**Files:**
- Modify: `internal/ui/board.go` — `boardKey` (mode-aware `↑↓`, new `←→`, `↵`→`openRight`, `⌃D`→`openHere`); `boardMouse` (tile hit-test, click→`openRight`); a `stepBoard` helper.
- Test: `internal/ui/board_test.go` (extend), `internal/ui/boardtile_test.go` (mouse).
- Test (screen): `cmd/wake/boardtilescreen_unix_test.go` (new).

**Interfaces:**
- Consumes: Task 2's `tileNav`, `tileColumns`, `tileCellWidth`, `tileWindowStart`, `tileHeight`; existing `App.openRight`, `App.openHere`, `App.openBelow`, `a.boardCursor`, `a.fleet.OnRoster`, `a.layout.Width`, `a.closeBoard`.
- Produces: no new exported surface; behavior changes to `boardKey`/`boardMouse`.

- [ ] **Step 1: Write the failing tests** — in `internal/ui/board_test.go`:

```go
func TestTiledCursorMovesInTwoDimensions(t *testing.T) {
	a := boardApp(t) // ≥ 3 agents
	a.board.Tiled = true
	a.layout.Width = 120 // wide enough for multiple columns
	roster := a.fleet.OnRoster()
	a.board.Selected = roster[0].ID

	right, _, _ := a.boardKey(tea.KeyMsg{Type: tea.KeyRight})
	if right.board.Selected == roster[0].ID {
		t.Fatal("→ did not move the cursor to the next tile")
	}
}

func TestEnterOpensAsANewColumnKeepingTheRoom(t *testing.T) {
	a := boardApp(t)
	roster := a.fleet.OnRoster()
	a.board.Selected = roster[0].ID
	next, _, _ := a.boardKey(tea.KeyMsg{Type: tea.KeyEnter})
	na := next
	if na.board.Up {
		t.Fatal("↵ left the board up over the conversation")
	}
	if na.grid.Cols[0].Top != "" {
		t.Fatal("the room is no longer Cols[0]; ↵ replaced it instead of opening beside it")
	}
	if !na.grid.Has(roster[0].ID) {
		t.Fatal("↵ did not open the cursored agent's DM as a column")
	}
}

func TestCtrlDOpensIntoTheFocusedPane(t *testing.T) {
	a := boardApp(t)
	roster := a.fleet.OnRoster()
	a.board.Selected = roster[0].ID
	next, _, _ := a.boardKey(tea.KeyMsg{Type: tea.KeyCtrlD})
	if next.board.Up {
		t.Fatal("⌃D left the board up")
	}
	if !next.grid.Has(roster[0].ID) {
		t.Fatal("⌃D did not open the cursored agent")
	}
}
```

> `TestEnterOpensAsANewColumnKeepingTheRoom` asserts the room stays `Cols[0]` with an empty `Top` (the room's id is `""`). Confirm `Grid.Cols[0].Top == ""` is how the room is spelled (grid.go / panes.go); adjust the assertion to the real spelling if it differs. The room is never replaced — `openRight` opens beside `a.focus`.

- [ ] **Step 2: Update the existing tests that assumed `↵` opens here**

`TestTheBoardCursorMovesAndEnterOpens` and the `⌃D`/`⌃Y`/`⌃B` cases in `board_test.go` assert the board closes and a conversation opens; if any asserts *which pane* `↵` lands in as "here", update it to expect a new column (`openRight`). Run them and fix the assertions, not the behavior:

Run: `go test ./internal/ui/ -run 'TestTheBoardCursor|TestCtrlCParks|TestOpen' -v`

- [ ] **Step 3: Run the new tests to verify failure**

Run: `go test ./internal/ui/ -run 'TestTiledCursor|TestEnterOpensAsANewColumn|TestCtrlDOpensInto' -v`
Expected: FAIL — `←→` unclaimed / `↵` still opens here.

- [ ] **Step 4: Implement the key changes** — in `internal/ui/board.go`, `boardKey`:

Replace the `KeyUp`/`KeyDown` cases and the `KeyEnter`/`KeyCtrlD` cases:

```go
	case tea.KeyUp:
		return a.stepBoard(tileUp), nil, true
	case tea.KeyDown:
		return a.stepBoard(tileDown), nil, true
	case tea.KeyLeft:
		if !a.board.Tiled {
			break // rows have no horizontal axis; close and let ← do its job
		}
		return a.stepBoard(tileLeft), nil, true
	case tea.KeyRight:
		if !a.board.Tiled {
			break
		}
		return a.stepBoard(tileRight), nil, true
	case tea.KeyEsc:
		return a.closeBoard(), nil, true
	case tea.KeyEnter:
		return a.openBoardRow(App.openRight) // open beside the room, keep it
	case tea.KeyCtrlD:
		return a.openBoardRow(App.openHere) // into the focused pane
```

(Leave `KeyCtrlY`→`openRight`, `KeyCtrlB`→`openBelow`, `KeyCtrlC`→`parkBoardRow` as they are. Note `break` in a Go `switch` exits the switch, falling to the `return a.closeBoard(), nil, false` at the end — the "unclaimed key closes the board" path.)

Add `stepBoard`:

```go
// stepBoard walks the cursor one step. In rows it is ↑↓ by one; in tiles it is
// the 2-D walk, cols derived from the frame width the tiles are laid out at.
func (a App) stepBoard(dir tileDir) App {
	agents := a.fleet.OnRoster()
	if len(agents) == 0 {
		return a
	}
	cur := a.boardCursor(agents)
	var at int
	if a.board.Tiled {
		at = tileNav(cur, tileColumns(a.layout.Width), len(agents), dir)
	} else {
		switch dir {
		case tileUp:
			at = clamp(cur-1, 0, len(agents)-1)
		case tileDown:
			at = clamp(cur+1, 0, len(agents)-1)
		default:
			at = cur
		}
	}
	a.board.Selected = agents[at].ID
	return a
}
```

Delete the now-unused `moveBoard` if nothing else calls it (grep first — `boardMouse`'s wheel uses it; keep it or route the wheel through `stepBoard`). Prefer routing the wheel through `stepBoard(tileUp/tileDown)` and removing `moveBoard` to avoid two cursor-movers (no parallel implementations).

- [ ] **Step 5: Implement the tile mouse** — in `internal/ui/board.go`, `boardMouse`:

Route the wheel through `stepBoard`, and branch the click on `Tiled`:

```go
func (a App) boardMouse(m tea.MouseMsg) (App, tea.Cmd) {
	switch {
	case m.Button == tea.MouseButtonWheelUp:
		return a.stepBoard(tileUp), nil
	case m.Button == tea.MouseButtonWheelDown:
		return a.stepBoard(tileDown), nil
	case m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft:
		agents := a.fleet.OnRoster()
		i := a.boardHit(m.X, m.Y, agents)
		if i < 0 || i >= len(agents) {
			return a, nil
		}
		return a.closeBoard().openRight(agents[i].ID, agents[i].Name), nil
	}
	return a, nil
}

// boardHit is the agent index a click lands on, or -1. It reads the same
// geometry the draw used, so a click and a tile cannot disagree - the row
// view's boardChromeRows rule, in two dimensions.
func (a App) boardHit(x, y int, agents []Agent) int {
	if a.board.Tiled {
		cols := tileColumns(a.layout.Width)
		cellW := tileCellWidth(a.layout.Width, cols)
		rowsVisible := max((a.paneHeight()-boardChromeRows-1)/tileHeight(), 1)
		start := tileWindowStart(a.boardCursor(agents), len(agents), cols, rowsVisible)
		r := (y - boardChromeRows) / tileHeight()
		c := x / (cellW + tileGap)
		if r < 0 || r >= rowsVisible || c < 0 || c >= cols {
			return -1
		}
		return start + r*cols + c
	}
	// Row view: the existing arithmetic.
	line := y - boardChromeRows
	if line < 0 || line >= a.boardRowsVisible() {
		return -1
	}
	return boardWindowStart(a.boardCursor(agents), len(agents), a.boardRowsVisible()) + line
}
```

(This replaces the row-view click math inline in `boardMouse` today — fold it into `boardHit` so both views share one hit-tester. Update the existing row-view click test if it asserted the old inline path.)

- [ ] **Step 6: Run the unit tests to verify passing**

Run: `go test ./internal/ui/ -run 'TestTiledCursor|TestEnterOpens|TestCtrlDOpens|TestTheBoard|TestCtrlCParks|TestSlashBoard|TestEscCloses|TestAnUnclaimed' -v`
Expected: PASS (new and existing).

- [ ] **Step 7: Write the pty screen test** — create `cmd/wake/boardtilescreen_unix_test.go`, mirroring `boardscreen_unix_test.go`:

```go
//go:build unix

package main

import (
	"strings"
	"testing"
)

// The tiled board draws on a real screen: ⇥ switches from rows to tiles, the
// tiles carry rounded borders and names, and a click opens a column.
func TestTheTiledBoardDrawsAndClicksOnAScreen(t *testing.T) {
	s := newScreen(t /* match boardscreen_unix_test's setup: a room over a few agents */)
	s.send("/board\r")
	s.send("\t") // ⇥ to tiles
	s.waitFor(func(dump string) bool {
		return strings.Contains(dump, "╭") && strings.Contains(dump, "╰")
	}, "the tiled board drew no rounded borders")
	// A click on the first tile opens a column and closes the board.
	// Use the harness' click helper at a coordinate inside the first tile
	// (row boardChromeRows+1, a column inside the first cell), then assert the
	// pane legend returns (board closed) - boardscreen's own inverse check.
}
```

> Copy `boardscreen_unix_test.go`'s exact harness construction (`newScreen`/`s.send`/`s.dump`/`s.waitFor` — match the real method names in that file) and its agent set. For the click, reuse the harness' mouse helper if one exists (grep `Mouse`/`click` in `cmd/wake/*_unix_test.go`); if none exists, assert the ⇥ render only and leave the click to the unit test in Step 5 — do not hand-roll SGR mouse bytes unless the harness already has a helper.

- [ ] **Step 8: Run the screen test**

Run: `go test ./cmd/wake/ -run TestTheTiledBoardDraws -v`
Expected: PASS. (Run from a normal checkout under your home directory — the screen tests render the working directory and fail under `/tmp`.)

- [ ] **Step 9: Commit**

```bash
git add internal/ui/board.go internal/ui/board_test.go internal/ui/boardtile_test.go cmd/wake/boardtilescreen_unix_test.go
git commit -m "feat: 2-D cursor, open-as-column, and click hit-testing for the tiled board"
```

---

### Task 5: The recorded ruling and the docs

Make the code and the §2c ruling agree, and update the notes so the next agent reads the reversal as a ruling, not a regression.

**Files:**
- Modify: `internal/ui/board.go` — header comment.
- Modify: `docs/notes/deferred.md` — the 2026-08-26 entry.
- Modify: `docs/notes/decisions.md` — a note that §2c was narrowed and why.
- Modify: `CLAUDE.md` — the `/board` description in the surface table and the key-locations row.

**Interfaces:** none — documentation.

- [ ] **Step 1: Rewrite `board.go`'s header** to record the narrowed ruling and the four guardrails (view-only · bounded tail, no scrollback · fixed grid · act-from-not-in), and that the tile view exists as a second render path. Keep it brief — one or two paragraphs, the house style.

- [ ] **Step 2: Update `deferred.md`'s 2026-08-26 entry** — change its status to built, dated 2026-08-27, pointing to `docs/superpowers/specs/2026-08-27-tiled-board-design.md` and this plan. Do not delete the entry's history; append the resolution.

- [ ] **Step 3: Add a `decisions.md` note** — one short entry: "§2c narrowed 2026-08-27 — 'overview, not panes' → 'not panes you *operate*'; the tiled board is view-only under four guardrails. See the spec." This is the shared note other worktrees read.

- [ ] **Step 4: Update `CLAUDE.md`** — the `/board` row in the surface/slash tables gains "· `⇥` toggles a tiled live wall (view-only)"; the board key-locations row notes `boardtile.go`. Do **not** touch the legend tables or their derived numbers — the board is a command, not a legend key, so no bijection guard is involved.

- [ ] **Step 5: Verify the CLAUDE.md guards still pass**

Run: `go test ./internal/ui/ -run 'TestCLAUDEmd' -v` and `go test ./... -run 'TestNoNonTestFileCrossesTheHardMax' -v`
Expected: PASS — `boardtile.go` is well under 800 lines and does not become one of the two largest non-test files.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/board.go docs/notes/deferred.md docs/notes/decisions.md CLAUDE.md
git commit -m "docs: record the narrowed §2c ruling and the tiled board's guardrails"
```

---

## Finish (not a task — the gate and the two reviews)

Per `CLAUDE.md`'s git rules, before opening the PR:

- [ ] **Run the full gate:** `make ci`. Read the exit code. Fix anything red. Say the exit code in the PR body.
- [ ] **Code review** — dispatch the `code-reviewer` agent (or an equivalent read of the diff) against correctness, the non-negotiables, and the conventions. Its job: *this diff is sound.*
- [ ] **Adversarial review** — a separate pass whose job is to break the claims: the cost gate (does `TestFoldingATailDoesNotTouchTheFleet` actually go red if `foldTail` is moved onto `Fleet`?), the guardrails (can any key reach an agent's stdin from a tile?), the geometry edge cases (last partial row, one column, zero agents). Confirm each new test can fail.
- [ ] **Open the PR** into `main` with both reviews' findings and the `make ci` exit code in the body. No Claude attribution.

## Self-Review (plan author's check against the spec)

- **§2 guardrails** → Task 1 (view-only: tiles never write stdin — no key in `boardKey` sends to an agent), Task 3 (bounded tail via `partial`/`maxPreviewRows`; fixed grid via `titledBox`), Task 4 (act-from: `↵`/click → `openRight`). ✓
- **§3 entry/mode** → Task 3 (`⇥` toggle, rows default, `Tiled` branch). ✓
- **§4 the tile** → Task 3 (`tile`/`tileBody`: border, glyph+name, by-state content, `⤷ N subagents`; blocked draws `ag.Tool` per the spec's honesty edit). ✓
- **§5 the grid** → Task 2 (columns, 2-D `tileNav`, `tileWindowStart`) + Task 4 (`↑↓←→`). ✓
- **§6 liveness & cost** → Task 1 (App-level `tails`, gated, reuses `partial`; `TestFoldingATailDoesNotTouchTheFleet` is the copy-trap gate) + Task 3 (`BenchmarkTiledBoardFleetSecond`). **Refinement to note:** the spec said "a second map on `Fleet`"; grounding showed folding into `Fleet.Observe` copies the agents/tasks/checklists maps **per token** (the `withDM` trap), so the map lives on **App**, gated on the wall being up — the same reason `App.wants`/`dms` live on App. Same data, same `partial` type, same gate; only the owner moved, and for the exact cost reason the spec invoked. ✓
- **§7 opening** → Task 4 (`↵`/click `openRight` keeps the room and dedups via `Grid.Has`; `⌃D` `openHere`). ✓
- **§8 files/testing/ruling** → Task 3/4 tests, Task 5 docs + header. ✓
- **§9 non-goals** → no stdin path (Task 1/4), no nested tiles (Task 3 count only), no row-board tail (gate in Task 1), tile-width constant tuned in implementation (Task 2/3 `minTileWidth`/`tileBodyRows`). ✓

**Placeholder scan:** the two spots left to implementation are named and bounded — the exact `minTileWidth`/`tileBodyRows` values (spec §9 explicitly defers these to the pty harness) and the screen-test click helper (reuse the harness' own, or fall back to the unit-tested click). No `TODO`/`TBD` in shipped code.

**Type consistency:** `tileNav(cursor, cols, total int, dir tileDir)`, `tileColumns(width) int`, `tileCellWidth(width, cols) int`, `tileWindowStart(cursor, total, cols, visibleRows) int`, `tileHeight() int`, `foldTail(sessionID string, ev core.Event) App`, `withTail(id string, p partial) App`, `tile(ag Agent, width int, cursored bool) string`, `tileView(agents []Agent, width int) string`, `stepBoard(dir tileDir) App`, `boardHit(x, y int, agents []Agent) int` — used consistently across tasks.
