# Board Tile Transcripts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make each tile of the tiled board (`/board`, then `⇥`) render its agent's real conversation transcript — prose, tool calls, results — following live, in every state; and carry the observed model on the fleet report so every tile's status bar shows it.

**Architecture:** A board-scoped `App.boardDMs` map (isolated from the pane `a.dms`, mirroring the existing `App.tails` lifecycle) holds one `DM` per on-screen tile. Each board DM is seeded from disk via the existing history wire and fed live via a `foldBoard` sibling of `foldTail`. The tile draws the board DM's cached transcript window (`DM.transcriptWindow`) — reusing the DM's render-once-cache-forever machinery so no glamour runs per frame. The row/list board view is untouched.

**Tech Stack:** Go 1.26+, bubbletea/lipgloss/glamour (Charm), the `internal/core` airlock, `internal/rpc` frames, `internal/daemon`, `internal/ui`.

**Spec:** `docs/superpowers/specs/2026-09-01-board-tile-transcripts-design.md`

## Global Constraints

- **Gate:** `make ci` exit 0 is the only gate. Run it and read the exit code **before** opening the PR. `make test` runs the suite twice (with `-race` and without).
- **TDD:** failing test first (RED), minimal implementation (GREEN), refactor. Never test against a live LLM — replay recorded fixtures in `testdata/`.
- **Files:** 200–400 lines typical, **800 hard max** (`TestNoNonTestFileCrossesTheHardMax`). Functions under 50 lines, nesting under 4.
- **No parallel implementations.** Extend/generalise existing code in place; never a second version beside the first.
- **Immutability:** `App`, `DM`, `Fleet`, `transcript`, `partial` methods take value receivers and return a new value. Never mutate in place.
- **Airlock untouched:** only `internal/core`'s four airlock files decode Claude JSON. This work reads already-decoded `core.SessionFacts.Model`; it adds no airlock file.
- **No Claude attribution** in commits, PR title, or PR body. Conventional commits (`feat:`, `fix:`, `test:`, `docs:`, `refactor:`).
- **Branch:** `feat/board-tile-transcripts` (already created). Feature → PR; two reviews (code + adversarial) before the PR is opened.
- **Board scope:** every change is gated on `board.Up && board.Tiled`. The row/list view (`boardRow`/`boardDetail`) must not change.

## File Structure

| File | Responsibility |
|---|---|
| `internal/rpc/lifecycle.go` (modify) | Add `SessionStatus.Model`. |
| `internal/daemon/agent.go` (modify) | `observedModel` field; fold `ev.Session.Model` in `observe`; carry in `snapshot()`. |
| `internal/ui/fleet.go` (modify) | Fold `s.Model` in `WithStatus`. |
| `internal/ui/dm.go` (modify) | `DM.transcriptWindow(w, rows)`. |
| `internal/ui/history.go` (modify) | Generalise `askHistory`/`historyArrived` over target map + asked-set. |
| `internal/ui/boardtranscript.go` (create) | `App.boardDMs`, `App.boardHistoryAsked`, `ensureBoardDMs`, `foldBoard`, `visibleBoardAgents`. |
| `internal/ui/boardtile.go` (modify) | `tileMiddle` draws the transcript window; `tileTailCap`→`tileTailRows`. |
| `internal/ui/board.go` (modify) | `closeBoard`/`⇥`-to-rows drop `boardDMs`+`boardHistoryAsked`; guardrail-2 header rewrite. |
| `internal/ui/observe.go` (modify) | `foldTail` call → `foldBoard`. |
| `internal/ui/tail.go` (delete) | Superseded by the board DM. |
| `internal/ui/boardtail_test.go` (rework) | Retarget to `boardDMs`. |
| `cmd/wake/boardtilescreen_unix_test.go` (add tests) | Multi-line transcript + idle last-exchange on a real screen. |
| `CLAUDE.md` (modify) | Board rows + Key-locations rows describe the transcript window. |

---

### Task 1: Carry the observed model on the fleet report

**Files:**
- Modify: `internal/rpc/lifecycle.go` (SessionStatus struct)
- Modify: `internal/daemon/agent.go` (`agent` struct near `confirmedModel`; `observe` near the `ev.Session.SlashCommands` fold ~line 338; `snapshot()` ~line 702)
- Modify: `internal/ui/fleet.go` (`WithStatus` ~line 306)
- Test: `internal/ui/fleet_test.go`, `internal/daemon/agent_test.go` (or nearest existing)

**Interfaces:**
- Produces: `rpc.SessionStatus.Model string` (`json:"model,omitempty"`); folded onto `ui.Agent.Model` by `WithStatus`.

- [ ] **Step 1: Write the failing UI fold test** in `internal/ui/fleet_test.go`:

```go
func TestWithStatusFoldsTheModelFromTheReport(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", State: rpc.StateWorking, Model: "claude-opus-4-8"},
	}})
	if got, _ := f.Agent("s1"); got.Model != "claude-opus-4-8" {
		t.Errorf("Agent.Model = %q, want claude-opus-4-8", got.Model)
	}
}

func TestAnEmptyReportModelDoesNotBlankAKnownOne(t *testing.T) {
	// The event stream set the model; a later report assembled before the daemon
	// saw an init carries an empty Model and must not erase it.
	f := NewFleet()
	f, _ = f.Observe(core.Event{Kind: core.KindSystem, Session: &core.SessionFacts{Model: "claude-opus-4-8"}}, "s1")
	f = f.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{{ID: "s1", State: rpc.StateWorking}}})
	if got, _ := f.Agent("s1"); got.Model != "claude-opus-4-8" {
		t.Errorf("Agent.Model = %q, want the event-stream value kept", got.Model)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestWithStatusFoldsTheModel|TestAnEmptyReportModelDoesNotBlank' -v`
Expected: FAIL — `rpc.SessionStatus` has no `Model` field (compile error).

- [ ] **Step 3: Add the rpc field.** In `internal/rpc/lifecycle.go`, in `SessionStatus`, beside `ConfirmedModel`:

```go
	// Model is the model id this session was observed running on its init frame
	// - the resolved id, e.g. "claude-opus-4-8". Carried so a client that never
	// witnessed the init (a late attach, the board) still names the model; the
	// event-stream path (ui.Agent.Model via withFacts) is the live source and
	// this backfills it. Empty until the daemon has seen an init.
	Model string `json:"model,omitempty"`
```

- [ ] **Step 4: Fold it in `WithStatus`.** In `internal/ui/fleet.go`, in the per-session loop, add after the `a.Effort, a.Budget, a.ConfirmedModel = …` line:

```go
	// Only when the report names one, so a report assembled before the daemon saw
	// an init does not blank a model the event stream already gave. Model comes
	// off both wires now; last-writer-wins keeps them consistent. See rpc.SessionStatus.Model.
	if s.Model != "" {
		a.Model = s.Model
	}
```

- [ ] **Step 5: Run the UI tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestWithStatusFoldsTheModel|TestAnEmptyReportModelDoesNotBlank' -v`
Expected: PASS.

- [ ] **Step 6: Write the failing daemon test** in `internal/daemon/agent_test.go` (model after an existing `observe`/`snapshot` test):

```go
func TestSnapshotCarriesTheObservedInitModel(t *testing.T) {
	a := newTestAgent(t) // use the package's existing agent constructor/helper
	a.observe(core.Event{Kind: core.KindSystem, SessionID: a.claudeID,
		Session: &core.SessionFacts{Model: "claude-opus-4-8"}})
	if got := a.snapshot().Model; got != "claude-opus-4-8" {
		t.Errorf("snapshot().Model = %q, want claude-opus-4-8", got)
	}
}
```

(If `newTestAgent`/`a.claudeID` differ in this package, use the constructor the neighbouring daemon tests use; the point is: feed an init event, read `snapshot().Model`.)

- [ ] **Step 7: Run to verify failure**

Run: `go test ./internal/daemon/ -run TestSnapshotCarriesTheObservedInitModel -v`
Expected: FAIL — `snapshot().Model` is empty (field unset / not folded).

- [ ] **Step 8: Track and carry it in the daemon.** In `internal/daemon/agent.go`:

Add the field beside `confirmedModel`:
```go
	// observedModel is the model id seen on this session's init frame - the
	// resolved id, distinct from a.model (the spawn alias, empty for a default
	// spawn). Carried on the report so a client that never saw the init still
	// names the model. Display only.
	observedModel string
```

Fold it in `observe`, beside the existing `ev.Session.SlashCommands`/`Dir` folds:
```go
	if ev.Session != nil && ev.Session.Model != "" {
		a.observedModel = ev.Session.Model
	}
```

Carry it in `snapshot()`:
```go
		Model:          a.observedModel,
```

- [ ] **Step 9: Run the daemon test to verify it passes**

Run: `go test ./internal/daemon/ -run TestSnapshotCarriesTheObservedInitModel -v`
Expected: PASS.

- [ ] **Step 10: Guard the carried-fields test.** Run the whole daemon + ui suites to confirm `notCarriedOntoAnAgent` and any `SessionStatus` guards still pass (Model is *carried*, so it must not appear in a "not carried" list):

Run: `go test ./internal/daemon/ ./internal/ui/ ./internal/rpc/`
Expected: PASS. If a guard lists `Model` as not-carried, move it to the carried side with a one-line reason.

- [ ] **Step 11: Commit**

```bash
git add internal/rpc/lifecycle.go internal/daemon/agent.go internal/ui/fleet.go internal/ui/fleet_test.go internal/daemon/agent_test.go
git commit -m "fix: carry the observed model on the fleet report so late-attached surfaces name it"
```

---

### Task 2: `DM.transcriptWindow` — the cached window a tile draws

**Files:**
- Modify: `internal/ui/dm.go` (new method near `View`/`blockWidth`)
- Test: `internal/ui/dm_test.go` (or a new `internal/ui/transcriptwindow_test.go`)

**Interfaces:**
- Produces: `func (d DM) transcriptWindow(w, rows int) (DM, string)` — re-wraps only when `w` changed, sizes the transcript to `w×rows`, follows the bottom, returns the rendered window with no selection. The returned `DM` carries the re-wrap so the caller stores it back.

- [ ] **Step 1: Write the failing test:**

```go
func TestTranscriptWindowRendersTheTailAtCellSize(t *testing.T) {
	d := NewDM("s1", "luca")
	d = d.SetSize(80, 20)
	d = d.Append(core.Event{Kind: core.KindAssistantText, Text: "first line of prose"})
	d = d.Append(core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{ID: "t1", Name: "Bash", Display: "grep -n Model"}})
	_, body := d.transcriptWindow(30, 4)
	if lipgloss.Height(body) != 4 {
		t.Fatalf("window height = %d, want exactly 4 rows\n%s", lipgloss.Height(body), body)
	}
	if !strings.Contains(body, "Bash") {
		t.Errorf("window did not show the newest (tool) line:\n%s", body)
	}
}

func TestTranscriptWindowReWrapsOnlyOnWidthChange(t *testing.T) {
	renders := 0
	restore := renderTranscript
	renderTranscript = func(d DM) []block { renders++; return restore(d) }
	defer func() { renderTranscript = restore }()

	d := NewDM("s1", "luca").SetSize(80, 20)
	d = d.Append(core.Event{Kind: core.KindAssistantText, Text: "prose"})
	renders = 0
	d, _ = d.transcriptWindow(30, 4) // width changed 80→30: one re-wrap
	d, _ = d.transcriptWindow(30, 6) // width same, height changed: no re-wrap
	if renders != 1 {
		t.Errorf("renderTranscript ran %d times, want 1 (only the width change re-wraps)", renders)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run TestTranscriptWindow -v`
Expected: FAIL — `transcriptWindow` undefined.

- [ ] **Step 3: Implement the method** in `internal/ui/dm.go`:

```go
// transcriptWindow renders the tail of this conversation for a view-only tile
// of w by rows, following the newest line. It re-wraps through renderTranscript
// only when the width has moved - the DM's own cost model - and takes no
// selection, because a tile has none (guardrail 1). The tile draws this instead
// of DM.View, which would add the composer and the rest of a pane's chrome.
func (d DM) transcriptWindow(w, rows int) (DM, string) {
	w, rows = max(w, minBlockWidth), max(rows, 1)
	if w != d.width {
		d.width = w
		d.tr = d.tr.replace(renderTranscript(d))
	}
	d.tr = d.tr.sized(w, rows).toBottom()
	return d, d.tr.view(marked{})
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/ui/ -run TestTranscriptWindow -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ui/dm.go internal/ui/dm_test.go
git commit -m "feat: DM.transcriptWindow renders a cached transcript tail at cell size"
```

---

### Task 3: Generalise history seeding over target map + asked-set

**Files:**
- Modify: `internal/ui/history.go` (`askHistory`, `historyArrived`, `withHistoryAsked`, `forgetHistoryAsk`)
- Test: `internal/ui/history_test.go`

**Interfaces:**
- Produces: history bookkeeping usable by both the pane map (`a.dms`/`a.askedHistory`) and the board map (`a.boardDMs`/`a.boardHistoryAsked`). Board fields are introduced in Task 4; this task keeps the pane path byte-for-byte and adds a routing hook `historyArrived` uses.

**Design note (no parallel implementation):** keep one `FrameHistory` request path and one `DM.Before` fold. The only generalisation is *which conversation the reply lands in*. Do the minimal thing: `historyArrived` first tries `a.dms` (pane, unchanged), and if the id is not a pane conversation, folds into a board DM when one is awaiting. This adds no second wire and no second fold — Task 4 supplies the board side.

- [ ] **Step 1: Characterisation test that the pane path is unchanged** (guards the refactor) in `internal/ui/history_test.go` — if such a test already exists, run it; otherwise add:

```go
func TestHistoryArrivedStillFoldsIntoAnOpenPaneDM(t *testing.T) {
	a := App{dms: map[string]*DM{}, askedHistory: map[string]int{}}
	d := NewDM("s1", "luca")
	a.dms["s1"] = &d
	a = a.askHistory("s1")
	a, _ = a.takeHistoryAsks()
	a = a.historyArrived(rpc.Frame{SessionID: "s1", Events: []core.Event{
		{Kind: core.KindAssistantText, Text: "restored line"},
	}})
	if !strings.Contains(a.dms["s1"].tr.view(marked{}), "restored line") {
		t.Errorf("pane history fold regressed:\n%s", a.dms["s1"].dump())
	}
}
```

- [ ] **Step 2: Run to verify it passes today** (this is a characterisation test — it should already pass; it exists to catch a regression in Steps 3–4)

Run: `go test ./internal/ui/ -run TestHistoryArrivedStillFoldsIntoAnOpenPaneDM -v`
Expected: PASS.

- [ ] **Step 3: Add the routing hook to `historyArrived`.** In `internal/ui/history.go`, at the top of `historyArrived`, before the `a.dms` lookup, add:

```go
	// A reply for a conversation the pane world does not hold is the board's -
	// see boardHistoryArrived (boardtranscript.go). The two asks are mutually
	// exclusive per id (the board is drawn instead of panes, so no pane opens
	// while it is up), so this never steals a pane's reply.
	if _, isPane := a.dms[f.SessionID]; !isPane {
		return a.boardHistoryArrived(f)
	}
```

**Note:** `boardHistoryArrived` is created in Task 4. To keep this task independently green, add a stub in Task 3 that returns the app unchanged, and replace it in Task 4:

```go
// boardHistoryArrived is filled in by the board DM (Task 4). Until boardDMs
// exists there is nothing to fold into, so a stray reply is dropped.
func (a App) boardHistoryArrived(rpc.Frame) App { return a }
```

- [ ] **Step 4: Run the pane characterisation test again**

Run: `go test ./internal/ui/ -run TestHistoryArrivedStillFoldsIntoAnOpenPaneDM -v`
Expected: PASS (pane path unchanged; the new branch is only taken when `a.dms` has no entry).

- [ ] **Step 5: Commit**

```bash
git add internal/ui/history.go internal/ui/history_test.go
git commit -m "refactor: route a history reply for a non-pane conversation to the board"
```

---

### Task 4: `App.boardDMs` — build, seed, feed, drop

**Files:**
- Create: `internal/ui/boardtranscript.go`
- Modify: `internal/ui/app.go` (add `boardDMs`, `boardHistoryAsked` fields to `App`)
- Modify: `internal/ui/board.go` (`closeBoard` and the `⇥`-to-rows case drop the two maps)
- Modify: `internal/ui/observe.go` (call `foldBoard`; keep `foldTail` for now — Task 5 removes it)
- Modify: `internal/ui/history.go` (replace the Task 3 `boardHistoryArrived` stub)
- Test: `internal/ui/boardtranscript_test.go`

**Interfaces:**
- Consumes: `DM.transcriptWindow` (Task 2), the history wire (Task 3), `boardTileGrid`/`tileWindowStart`/`OnRoster` (existing).
- Produces:
  - `App.boardDMs map[string]*DM`, `App.boardHistoryAsked map[string]int`
  - `func (a App) ensureBoardDMs() App` — builds a board DM for each visible tile that lacks one, queuing a history ask.
  - `func (a App) foldBoard(sessionID string, ev core.Event) App` — appends an event to a visible tile's board DM.
  - `func (a App) visibleBoardAgents() []Agent` — the agents whose tiles are on screen.
  - `func (a App) boardHistoryArrived(f rpc.Frame) App` — folds a reply into a board DM.

- [ ] **Step 1: Write the failing lifecycle tests** in `internal/ui/boardtranscript_test.go` (modelled on `boardtail_test.go`):

```go
package ui

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func tiledBoardApp() App {
	a := App{board: Board{Up: true, Tiled: true}, fleet: NewFleet(),
		dms: map[string]*DM{}, askedHistory: map[string]int{}}
	a.fleet = a.fleet.WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "luca", State: rpc.StateWorking, Cwd: "/tmp/x"},
	}})
	a.layout = Layout{Width: 100} // enough for one tile
	return a
}

func TestEnsureBuildsABoardDMForAVisibleTile(t *testing.T) {
	a := tiledBoardApp().ensureBoardDMs()
	if _, ok := a.boardDMs["s1"]; !ok {
		t.Fatal("ensureBoardDMs built no board DM for the visible agent")
	}
	if _, asked := a.boardHistoryAsked["s1"]; !asked {
		t.Error("ensureBoardDMs did not queue a history ask for the new board DM")
	}
}

func TestFoldBoardAppendsToAVisibleTilesDM(t *testing.T) {
	a := tiledBoardApp().ensureBoardDMs()
	a = a.foldBoard("s1", core.Event{Kind: core.KindAssistantText, Text: "streamed prose"})
	_, body := a.boardDMs["s1"].transcriptWindow(40, 6)
	if !strings.Contains(body, "streamed prose") {
		t.Errorf("foldBoard did not append the block to the board DM:\n%s", body)
	}
}

func TestFoldBoardIgnoresAPagedOffAgent(t *testing.T) {
	a := tiledBoardApp() // no ensure: s1 has no board DM
	a = a.foldBoard("s1", core.Event{Kind: core.KindAssistantText, Text: "prose"})
	if _, ok := a.boardDMs["s1"]; ok {
		t.Error("foldBoard built a board DM for an agent with no visible tile")
	}
}

func TestFoldBoardIsANoOpWhenTheBoardIsDownOrInRows(t *testing.T) {
	for _, tc := range []struct{ up, tiled bool }{{false, false}, {true, false}} {
		a := tiledBoardApp()
		a.board = Board{Up: tc.up, Tiled: tc.tiled}
		a = a.ensureBoardDMs().foldBoard("s1", core.Event{Kind: core.KindAssistantText, Text: "x"})
		if len(a.boardDMs) != 0 {
			t.Errorf("up=%v tiled=%v: board DMs accumulated off the tiled board", tc.up, tc.tiled)
		}
	}
}

func TestClosingTheBoardDropsTheBoardDMs(t *testing.T) {
	a := tiledBoardApp().ensureBoardDMs()
	a = a.closeBoard()
	if len(a.boardDMs) != 0 || len(a.boardHistoryAsked) != 0 {
		t.Errorf("close left %d board DMs and %d asks, want 0/0", len(a.boardDMs), len(a.boardHistoryAsked))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run 'TestEnsureBuilds|TestFoldBoard|TestClosingTheBoardDropsTheBoardDMs' -v`
Expected: FAIL — `boardDMs`, `ensureBoardDMs`, `foldBoard` undefined.

- [ ] **Step 3: Add the App fields.** In `internal/ui/app.go`, in the `App` struct, near `tails`:

```go
	// boardDMs holds one rendered conversation per on-screen tile of the tiled
	// board, and boardHistoryAsked records which have a disk-history ask
	// outstanding (how many events they held at ask time, historyArrived's rule).
	// Both are built while board.Up && board.Tiled and dropped whole on close -
	// App.tails's lifecycle, isolated from the pane world (a.dms). See boardtranscript.go.
	boardDMs          map[string]*DM
	boardHistoryAsked map[string]int
```

- [ ] **Step 4: Create `internal/ui/boardtranscript.go`:**

```go
package ui

// The tiled board's transcripts: one rendered DM per on-screen tile, seeded
// from disk and fed live while the tiled board is up, dropped whole when it
// closes. Isolated from the pane map (a.dms) - the ⇥ ring, App.wants and hideDM
// never see these, so a tile costs nothing where a pane is counted. It reuses
// the DM's render-once-cache machinery (transcriptWindow) so no glamour runs per
// frame; the cost is a render when a tile is first seeded or paged onto, and a
// re-wrap on a width change - work per change, not per frame. Guardrail 2 of the
// 2026-08-27 tiled-board design, revised 2026-09-01: a bounded transcript
// window, still view-only and unscrollable.

import (
	"maps"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// visibleBoardAgents is the agents whose tiles are drawn this frame - the same
// window tileView draws, so the built set and the drawn set cannot disagree.
func (a App) visibleBoardAgents() []Agent {
	agents := a.fleet.OnRoster()
	if len(agents) == 0 {
		return nil
	}
	g := a.boardTileGrid(len(agents))
	start := tileWindowStart(a.boardCursor(agents), len(agents), g.cols, g.rows)
	end := min(start+g.cols*g.rows, len(agents))
	return agents[start:end]
}

// ensureBoardDMs builds a board DM for each visible tile that lacks one, and
// queues a disk-history ask for it. A no-op unless the tiled board is up. Cheap
// to call per Update: it only builds what is missing, and the visible set is a
// handful of agents (tileWindowStart pages the rest).
func (a App) ensureBoardDMs() App {
	if !a.board.Up || !a.board.Tiled {
		return a
	}
	for _, ag := range a.visibleBoardAgents() {
		if _, ok := a.boardDMs[ag.ID]; ok {
			continue
		}
		d := NewDM(ag.ID, ag.Name)
		a = a.withBoardDM(ag.ID, d)
		a = a.askBoardHistory(ag.ID)
	}
	return a
}

// withBoardDM returns an App whose boardDMs map is its own, one entry replaced -
// withDM's rule, one map over.
func (a App) withBoardDM(id string, d DM) App {
	next := make(map[string]*DM, len(a.boardDMs)+1)
	maps.Copy(next, a.boardDMs)
	next[id] = &d
	a.boardDMs = next
	return a
}

// askBoardHistory records a disk-history ask for a board DM and queues the
// FrameHistory write - askHistory's board twin, sharing the one wire (Task 3).
func (a App) askBoardHistory(id string) App {
	if _, asked := a.boardHistoryAsked[id]; id == "" || asked {
		return a
	}
	next := make(map[string]int, len(a.boardHistoryAsked)+1)
	maps.Copy(next, a.boardHistoryAsked)
	next[id] = a.boardDMs[id].events.len()
	a.boardHistoryAsked = next
	a.pendingHistory = append(append([]string(nil), a.pendingHistory...), id)
	return a
}

// boardHistoryArrived folds a disk-history reply into a board DM, with
// historyArrived's own guards: dropped if the DM is gone or if anything arrived
// since the ask (which would double-render an event that is on disk and live).
func (a App) boardHistoryArrived(f rpc.Frame) App {
	d, ok := a.boardDMs[f.SessionID]
	if !ok || len(f.Events) == 0 {
		return a
	}
	held, asked := a.boardHistoryAsked[f.SessionID]
	if !asked || d.events.len() != held {
		return a.forgetBoardHistoryAsk(f.SessionID)
	}
	return a.withBoardDM(f.SessionID, d.Before(f.Events))
}

func (a App) forgetBoardHistoryAsk(id string) App {
	next := make(map[string]int, len(a.boardHistoryAsked))
	for k, v := range a.boardHistoryAsked {
		if k != id {
			next[k] = v
		}
	}
	a.boardHistoryAsked = next
	return a
}

// foldBoard appends one agent's event to its board DM while the tiled board is
// up - foldTail's successor, feeding a whole transcript rather than a one-block
// tail. Gated on a board DM already existing, which is what bounds the render to
// visible tiles: a paged-off agent has none, so its blocks are not rendered
// until the cursor pages it back and ensureBoardDMs seeds it from disk.
func (a App) foldBoard(sessionID string, ev core.Event) App {
	if !a.board.Up || !a.board.Tiled {
		return a
	}
	d, ok := a.boardDMs[sessionID]
	if !ok {
		return a
	}
	return a.withBoardDM(sessionID, d.Append(ev))
}
```

- [ ] **Step 5: Replace the Task 3 stub.** Delete the placeholder `boardHistoryArrived` added in Task 3's history.go (the real one now lives in `boardtranscript.go`).

- [ ] **Step 6: Drop the maps on close.** In `internal/ui/board.go` `closeBoard`, beside `a.tails = nil`:

```go
	a.boardDMs = nil
	a.boardHistoryAsked = nil
```

And in `boardKey`'s `tea.KeyTab` case, in the branch that leaves tiled mode (`if !a.board.Tiled { a.tails = nil }`), add `a.boardDMs = nil; a.boardHistoryAsked = nil` so `⇥`-to-rows drops them too.

- [ ] **Step 7: Wire `ensureBoardDMs` + `foldBoard` into Update.** In `internal/ui/observe.go`, change the `a.foldTail(sessionID, ev)` call to also feed the board DM (keep `foldTail` until Task 5):

```go
	a = a.foldTail(sessionID, ev)
	a = a.foldBoard(sessionID, ev)
```

In `internal/ui/app.go`'s `apply` (the Update reducer), after the message is handled and before returning, call `ensureBoardDMs` so paging/resize/report seed newly-visible tiles. Find the return of `apply` and wrap:

```go
	return a.ensureBoardDMs(), cmd
```

(If `apply` has multiple returns, add `a = a.ensureBoardDMs()` at the single common tail, or introduce one. Keep it to work-per-change: `ensureBoardDMs` is a no-op off the tiled board.)

- [ ] **Step 8: Run the lifecycle tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestEnsureBuilds|TestFoldBoard|TestClosingTheBoardDropsTheBoardDMs' -v`
Expected: PASS.

- [ ] **Step 9: Run the whole ui package** to catch nil-map panics (a fresh App has nil `boardDMs`; `withBoardDM`/`foldBoard` must tolerate it — `maps.Copy` into a sized make handles nil source):

Run: `go test ./internal/ui/`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/ui/boardtranscript.go internal/ui/app.go internal/ui/board.go internal/ui/observe.go internal/ui/history.go internal/ui/boardtranscript_test.go
git commit -m "feat: board-scoped transcripts seeded from disk and fed live while the tiled board is up"
```

---

### Task 5: Draw the transcript window in the tile; remove the old tail

**Files:**
- Modify: `internal/ui/boardtile.go` (`tileMiddle`, `tileTailCap`→`tileTailRows`)
- Modify: `internal/ui/board.go` (guardrail-2 header rewrite; drop `a.tails` references)
- Modify: `internal/ui/observe.go` (remove the `foldTail` call)
- Delete: `internal/ui/tail.go`
- Rework: `internal/ui/boardtail_test.go` → assert the board DM behaviour (or delete if fully covered by `boardtranscript_test.go`)
- Test: `internal/ui/boardtile_test.go` (add a unit test for the middle), `cmd/wake/boardtilescreen_unix_test.go` (add screen tests)

**Interfaces:**
- Consumes: `App.boardDMs`, `DM.transcriptWindow` (Task 2/4).

- [ ] **Step 1: Write the failing unit test** for the tile middle in `internal/ui/boardtile_test.go`:

```go
func TestTileMiddleDrawsTheTranscriptWindow(t *testing.T) {
	a := tiledBoardApp().ensureBoardDMs()
	a = a.foldBoard("s1", core.Event{Kind: core.KindAssistantText, Text: "hello from luca"})
	ag, _ := a.fleet.Agent("s1")
	rows := a.tileMiddle(ag, 40, 5)
	if !strings.Contains(strings.Join(rows, "\n"), "hello from luca") {
		t.Errorf("tile middle did not draw the transcript:\n%s", strings.Join(rows, "\n"))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/ui/ -run TestTileMiddleDrawsTheTranscriptWindow -v`
Expected: FAIL — `tileMiddle` still draws the tail/`boardDetail`, not the transcript.

- [ ] **Step 3: Rewrite `tileMiddle`.** In `internal/ui/boardtile.go`, replace the tail/`boardDetail` body with the transcript window (note: `tileMiddle` must return the possibly-re-wrapped board DM to the App, so give it a pointer receiver semantics by threading through — since `tile`/`tileBody` are value methods on `App`, store the re-wrap via a small helper or accept that the draw path re-wraps a copy). Simplest that keeps the value-method draw path pure: draw off a copy and do not store the re-wrap here — the width is stable across a frame, so the first tile draw at a new width re-wraps a copy and the *next Update's* `ensureBoardDMs`-adjacent path stores it. To avoid re-wrapping every frame, store the sized board DM from the draw is not possible (View is const); instead size board DMs in `ensureBoardDMs` to the current cell width, so the draw's `transcriptWindow` finds the width already set and never re-wraps in View.

Concretely:

(a) In `ensureBoardDMs` (Task 4), after building/for each visible board DM, size it to the cell:
```go
		g := a.boardTileGrid(len(a.fleet.OnRoster()))
		inner := max(g.cellW-boxFrameWidth, 1)
		d, _ := a.boardDMs[ag.ID].transcriptWindow(inner, max(g.cellH-tileFrameRows, minTileTailRows))
		a = a.withBoardDM(ag.ID, d)
```
Do this for *every* visible board DM each call (not only new ones), so a resize re-wraps once here, off the draw path.

(b) `tileMiddle` becomes a pure draw off the stored, already-sized transcript:
```go
// tileMiddle is the rows between the state line and the bottom framing: the
// agent's live transcript tail, sized to the cell in ensureBoardDMs and drawn
// here. Empty rows while a freshly-seeded tile's disk history is still in
// flight - the framing (state, subagents, bar) draws immediately regardless.
func (a App) tileMiddle(ag Agent, inner, rows int) []string {
	if rows <= 0 {
		return nil
	}
	d, ok := a.boardDMs[ag.ID]
	if !ok {
		return padRows(nil, rows)
	}
	_, body := d.transcriptWindow(inner, rows)
	return padRows(tailLines(body, inner), rows)
}
```

(`tailLines` already splits a wrapped view into hardened physical rows and truncates each to `inner` — reuse it so a streamed control byte cannot forge the tile.)

- [ ] **Step 4: Rename `tileTailCap`→`tileTailRows`.** In `internal/ui/boardtile.go`, rename the method and update its callers (`foldTail` is being removed; the remaining caller is `ensureBoardDMs`). Keep the doc comment's "guardrail 2" note.

- [ ] **Step 5: Remove the old tail.** Delete `internal/ui/tail.go`; remove the `a.foldTail(sessionID, ev)` line from `observe.go`; remove the `a.tails` field from `App` and every reference (`board.go` `closeBoard`/`boardKey`, any test). Rework `boardtail_test.go`: delete the tail-specific tests (`TestTheTail*`, `TestFoldingATailDoesNotTouchTheFleet`, `TestClosingTheBoardDropsTheTails` — the last is now `TestClosingTheBoardDropsTheBoardDMs` in Task 4). Keep `sameFleet`/`partialEv` if still referenced, else move them.

- [ ] **Step 6: Run to verify the unit test passes and nothing references `tails`**

Run: `go build ./... && go test ./internal/ui/ -run TestTileMiddleDrawsTheTranscriptWindow -v`
Expected: PASS, and the build has no `undefined: tails` / `a.foldTail` errors.

- [ ] **Step 7: Add the screen tests** in `cmd/wake/boardtilescreen_unix_test.go` (modelled on the existing tests there; use `withScriptedAgent` with a script that emits an assistant text block and a tool call so the tile has multi-line content):

```go
// A tile draws the agent's real transcript, not a one-line summary: an agent
// that has spoken and run a tool shows both, on separate rows, inside its cell.
// This is the 2026-09-01 guardrail-2 revision - a bounded transcript window.
func TestATiledBoardTileDrawsTheTranscript(t *testing.T) {
	withScriptedAgent(t, scriptProseAndTool) // a script: assistant text then a Bash tool call
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWake(t, 100, 30)
	s.await("ready")
	s.send("/board\r")
	s.await("BOARD")
	s.send("\t") // ⇥ to tiles
	s.await("╭")
	s.settle()

	txt := s.text()
	if !strings.Contains(txt, "Bash") {
		t.Fatalf("tile did not draw the tool call from the transcript.\n%s", s.dump())
	}
	// And the prose the agent streamed, on a different row than the tool line.
	if !strings.Contains(txt, proseMarker) {
		t.Fatalf("tile did not draw the streamed prose.\n%s", s.dump())
	}
}
```

Determine `scriptProseAndTool`/`proseMarker` from the existing `withScriptedAgent` scripting used by neighbouring tests; the script must produce one assistant text block containing `proseMarker` and one tool_use for `Bash`. If a suitable scripted fixture already exists, reuse it.

- [ ] **Step 8: Run the screen tests**

Run: `go test ./cmd/wake/ -run 'TestATiledBoardTileDrawsTheTranscript|TestTheTiledBoard' -v`
Expected: PASS. (Run from a normal checkout under `$HOME` — the screen tests render the working directory.)

- [ ] **Step 9: Rewrite the guardrail-2 header** in `internal/ui/board.go` — update guardrail 2 in the file header from *"Bounded live tail … a `tail -f` snippet, never a conversation"* to the revised text in the spec §1 (bounded transcript window, still view-only and unscrollable), citing `docs/superpowers/specs/2026-09-01-board-tile-transcripts-design.md`.

- [ ] **Step 10: Run the whole ui + cmd/wake suites**

Run: `go test ./internal/ui/ ./cmd/wake/`
Expected: PASS.

- [ ] **Step 11: Commit**

```bash
git add internal/ui/boardtile.go internal/ui/board.go internal/ui/observe.go internal/ui/app.go internal/ui/boardtile_test.go internal/ui/boardtail_test.go cmd/wake/boardtilescreen_unix_test.go
git rm internal/ui/tail.go
git commit -m "feat: draw each tiled-board tile as its agent's live transcript window"
```

---

### Task 6: Cost guard, benchmark, and docs

**Files:**
- Test: `internal/ui/boardtranscript_test.go` (cost guard), `internal/ui/partial_bench_test.go` or a new `internal/ui/boardtranscript_bench_test.go`
- Modify: `CLAUDE.md` (board rows + Key-locations rows)

**Interfaces:**
- Consumes: the `renderTranscript` seam (already a `var`, swappable in tests).

- [ ] **Step 1: Write the failing cost guard** — only visible tiles ever render:

```go
func TestOnlyVisibleTilesRenderATranscript(t *testing.T) {
	rendered := map[string]int{}
	restore := renderTranscript
	renderTranscript = func(d DM) []block { rendered[d.SessionID]++; return restore(d) }
	defer func() { renderTranscript = restore }()

	// A fleet wider than one screen of tiles: only the visible window seeds.
	a := App{board: Board{Up: true, Tiled: true}, fleet: NewFleet(),
		dms: map[string]*DM{}, layout: Layout{Width: 60}} // one narrow column
	var sessions []rpc.SessionStatus
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		sessions = append(sessions, rpc.SessionStatus{ID: id, Name: id, State: rpc.StateWorking, Cwd: "/tmp"})
	}
	a.fleet = a.fleet.WithStatus(&rpc.Status{Sessions: sessions})
	a = a.ensureBoardDMs()
	// Feed a block to every session; only the ones with a (visible) board DM render.
	for _, id := range []string{"s1", "s2", "s3", "s4", "s5"} {
		a = a.foldBoard(id, core.Event{Kind: core.KindAssistantText, Text: "x"})
	}
	if len(rendered) == 0 || len(rendered) >= 5 {
		t.Fatalf("rendered %d transcripts, want only the visible window (<5)", len(rendered))
	}
}
```

(Adjust the height in `boardTileGrid` via `a.paneHeight()` if the harness needs a set `layout.Height`; the point is that fewer than all five tiles are visible, so fewer than five render.)

- [ ] **Step 2: Run to verify failure or pass** — if it already passes, it still earns its keep as a regression guard. If it fails because all five render, the visible-window gate in `foldBoard`/`ensureBoardDMs` is wrong; fix there, not in the test.

Run: `go test ./internal/ui/ -run TestOnlyVisibleTilesRenderATranscript -v`

- [ ] **Step 3: Add the benchmark** in `internal/ui/boardtranscript_bench_test.go`:

```go
func BenchmarkBoardTileSecond(b *testing.B) {
	// One second of a fleet streaming with the tiled board up: the cost is the
	// visible-tile render integral (once per completed block) plus plain-text
	// partials, never a per-token glamour pass. Pairs with
	// BenchmarkStreamingFleetSecond. Build a tiled-board App, replay a fixture
	// of partials + completed blocks across the visible agents, and measure.
	// Model the fixture and loop on BenchmarkStreamingFleetSecond in partial_bench_test.go.
	b.Skip("fill in from BenchmarkStreamingFleetSecond's harness")
}
```

Then replace the `Skip` with the real loop modelled on `BenchmarkStreamingFleetSecond` (read that benchmark first; reuse its fixture/replay). Run:

Run: `go test ./internal/ui/ -run x -bench BenchmarkBoardTileSecond -benchmem`
Expected: a number in the low milliseconds per fleet-second (same order as `BenchmarkStreamingFleetSecond`), proving no per-token glamour.

- [ ] **Step 4: Update `CLAUDE.md`.** In the "What it does today" table, the **Slash commands** `/board` row and the roster/board description: change the tiled-board wording from a view-only overview with a live tail to "a tiled live wall whose tiles draw each agent's live transcript window (view-only)". In the Key-locations table, update the board rows:
  - `internal/ui/board.go` / `boardtile.go` row: note the tile draws a transcript window.
  - Add a row: `The tiled board's per-tile transcripts | internal/ui/boardtranscript.go — App.boardDMs, ensureBoardDMs, foldBoard, seeded from disk and fed live, dropped on close`.
  - Remove/replace the `internal/ui/tail.go` row (file deleted).
  - Note the model now rides the report: update the `internal/ui/statusbar.go` / `rpc.SessionStatus` mentions to say `Model` is carried.

- [ ] **Step 5: Verify the derived-count guards.** `TestCLAUDEmdNamesTheTwoLargestNonTestFiles` reads CLAUDE.md against the tree; adding `boardtranscript.go` and deleting `tail.go` changes file sizes but not the two largest (`wire.go`/`protocol.go`). Run:

Run: `go test ./internal/ui/ -run 'TestCLAUDEmd'`
Expected: PASS. If a count moved, the test message states the correction — apply it, do not edit the guard.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/boardtranscript_test.go internal/ui/boardtranscript_bench_test.go CLAUDE.md
git commit -m "test: guard visible-only tile rendering; docs: board tile transcript window"
```

---

### Final: gate and reviews

- [ ] **Step 1: Run the full gate**

Run: `make ci`
Expected: exit 0. Read the exit line (a background-run log can mask it — read the log's exit line, per memory).

- [ ] **Step 2: Code review** — `code-reviewer` agent (or an equivalent read) over the diff for correctness, the non-negotiables, and the conventions.

- [ ] **Step 3: Adversarial review** — a separate pass that tries to break the strongest claims: that no glamour runs per frame (the cost guard), that closing the board drops everything (no leak), that a paged-off agent never renders, that the model fold does not blank a known model, that mutual-exclusivity of pane/board history asks holds.

- [ ] **Step 4: Open the PR** into `main` with both review results and the `make ci` exit code in the body. No Claude attribution.

## Self-Review (author's checklist — completed)

- **Spec coverage:** §1 revised guardrail → Task 5 Step 9 + CLAUDE.md (Task 6). §2 scope (tiles only) → every task gated on `board.Up && board.Tiled`. §3 cost model → Tasks 2/4 (cache reuse) + Task 6 cost guard/benchmark. §4.1 lifecycle → Task 4 (ensure/drop) + Task 5 (`⇥`). §4.2 seeding → Tasks 3/4 (shared history wire, board asked-set). §4.3 live feed → Task 4 `foldBoard` + Task 5 remove `foldTail`. §4.4 render → Task 5. §5 model fix → Task 1. §6 guardrail audit → Task 5 header. §7 testing → Tasks 1–6. §8 files → File Structure table. §9 risks → covered by tests (history race: Task 4 `boardHistoryArrived` guard; width thrash: Task 5 Step 3 sizes in `ensureBoardDMs`).
- **Placeholder scan:** the only deferred specifics are the exact `withScriptedAgent` script (Task 5 Step 7) and the benchmark loop (Task 6 Step 3), both pinned to a named existing template to copy — not open-ended.
- **Type consistency:** `ensureBoardDMs`/`foldBoard`/`visibleBoardAgents`/`withBoardDM`/`askBoardHistory`/`boardHistoryArrived`/`forgetBoardHistoryAsk`/`transcriptWindow`/`tileTailRows` used consistently across tasks; `boardDMs map[string]*DM`, `boardHistoryAsked map[string]int` consistent.
