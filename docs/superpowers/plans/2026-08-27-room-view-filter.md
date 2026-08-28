# Room `@name` View Filter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Typing a lone `@name` in the room narrows the group chat to that agent's thread (a live view filter), not only where the next `↵` routes.

**Architecture:** A pure predicate `focusAdmits` decides per-line membership. `Room` gains a `focus` id (plus `managerID`/`focusName`), and the transcript renders only admitted lines — a hidden line stays in `said` at `rows == 0` with no `tr` block, so the existing span/scroll/reclaim geometry (which sums `rows`) is untouched and `said` stays 1:1 with `tr`. `App.retarget` — already the one place that resolves the composer's `@name` per keystroke — drives `Room.focus`. The operator's own room echoes carry a new `roomLine.to` so "you → @iris" can be told from "you → @john" and a broadcast.

**Tech Stack:** Go 1.26+, Bubble Tea / lipgloss / glamour (Charm). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-08-27-room-view-filter-design.md` (read it alongside this plan). One refinement to spec §5: rather than a *separate* canonical backing, `said` itself is the canonical list and carries hidden lines at `rows == 0`. Same intent (no `said`↔`tr` desync, bounded by the existing 20k cap), simpler realization — no second slice.

## Global Constraints

- **Go 1.26+.** `make ci` exit 0 is the only gate; run it before opening the PR and put the exit code in the PR body.
- **No airlock/wire/keybinding change.** `roomLine.to` is a UI-only field, never on `core.Event`. No `rpc.Frame` change. No new key, no legend entry (the legend is a bijection with `App.key` — do not touch it).
- **Immutable `Room`.** Every `Room` method takes a value receiver and returns a new `Room`. Never mutate through a retained copy. Same for `App`.
- **Files:** 200–400 lines typical, 800 hard max. `internal/ui/chat.go` is ~590 lines today; the additions here keep it under 800. If it would cross, split the new render-filter logic into `roomfocus.go`.
- **Pure functions stay pure.** `focusAdmits` is I/O-free and roster-free, in the spirit of `attention.go` — it compares ids the room already holds.
- **TDD.** Write the failing test first, watch it fail, implement minimally, watch it pass, commit. `make test` runs the suite twice (with `-race` and without) — a green race run alone is not evidence.
- **Commits:** conventional (`feat:`/`test:`/`docs:`), no Claude attribution (no `Co-Authored-By`, no generated-with footer). Branch: `feat/room-view-filter` (already created).
- **Never run `wake` from this tree without `WAKE_SOCKET` set.** Tests and `make` targets are safe; a bare `go run ./cmd/wake` is not.

---

### Task 1: The membership predicate (`focusAdmits`)

**Files:**
- Create: `internal/ui/roomfocus.go`
- Test: `internal/ui/roomfocus_test.go`

**Interfaces:**
- Consumes: `roomLine` (`internal/ui/chat.go`: fields `ev core.Event`, `by Agent`; **plus `to string`, added in Task 2** — Task 1 references `l.to`, so land Task 1 and Task 2 together if the compiler complains, or add the `to` field first). `core.KindUserText` (`internal/core/event.go`).
- Produces: `func focusAdmits(l roomLine, focus, managerID string) bool` — used by Task 3.

> Note: `roomLine.to` does not exist until Task 2. To keep Task 1 compiling on its own, add the single field `to string` to the `roomLine` struct in this task (its stamping comes in Task 2). That is the only `chat.go` change here.

- [ ] **Step 1: Add the `to` field to `roomLine`**

In `internal/ui/chat.go`, the `roomLine` struct — add one field:

```go
type roomLine struct {
	ev          core.Event
	by          Agent
	to          string // agent id this user line was addressed to; "" = broadcast / not a user line. UI-only; see roomfocus.go.
	id          uint64
	broadcastID uint64
	rows        int
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/ui/roomfocus_test.go`:

```go
package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestFocusAdmits(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	agent := func(id string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindAssistantText, SessionID: id}}
	}
	user := func(to string) roomLine {
		return roomLine{ev: core.Event{Kind: core.KindUserText}, to: to}
	}
	cases := []struct {
		name  string
		line  roomLine
		focus string
		want  bool
	}{
		{"unfocused admits agent prose", agent(iris), "", true},
		{"unfocused admits any user line", user(iris), "", true},
		{"focused admits john's prose", agent(john), john, true},
		{"focused hides iris's prose", agent(iris), john, false},
		{"focused admits the manager", agent(mgr), john, true},
		{"focused admits a broadcast", user(""), john, true},
		{"focused admits your message to john", user(john), john, true},
		{"focused hides your message to iris", user(iris), john, false},
		{"focused admits john's permission ask", roomLine{ev: core.Event{Kind: core.KindPermissionRequest, SessionID: john}}, john, true},
		{"focused hides iris's turn end", roomLine{ev: core.Event{Kind: core.KindTurnEnd, SessionID: iris}}, john, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := focusAdmits(c.line, c.focus, mgr); got != c.want {
				t.Fatalf("focusAdmits(%s) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestFocusAdmits -v`
Expected: FAIL — `undefined: focusAdmits`.

- [ ] **Step 4: Write the predicate**

Create `internal/ui/roomfocus.go`:

```go
package ui

import "github.com/DilanDoshi/wake/internal/core"

// focusAdmits reports whether a room line is drawn while the room is focused on
// one agent. It is a pure id comparison — the room holds focus, managerID and
// the line's own ids, and never resolves a name — so it stays testable without
// a fleet, the way attention.go does.
//
// focus == "" is the unfocused room and admits everything, so the filter is the
// identity until a target resolves. A user line (your own turn) is admitted when
// it is a broadcast (to == "") or addressed to the focused agent; an
// agent-produced line (prose, turn-end, permission ask) when it is the focused
// agent's or the manager's. Everything else is hidden.
func focusAdmits(l roomLine, focus, managerID string) bool {
	if focus == "" {
		return true
	}
	if l.ev.Kind == core.KindUserText {
		return l.to == "" || l.to == focus
	}
	return l.ev.SessionID == focus || l.ev.SessionID == managerID
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestFocusAdmits -v`
Expected: PASS (all subtests).

- [ ] **Step 6: Commit**

```bash
git add internal/ui/roomfocus.go internal/ui/roomfocus_test.go internal/ui/chat.go
git commit -m "feat: the room view-filter membership predicate"
```

---

### Task 2: Stamp `roomLine.to` on the operator's room echo

**Files:**
- Modify: `internal/ui/chat.go` (add `appendUser`/`appendLine`, keep `Append`)
- Modify: `internal/ui/send.go` (`sendRoom` stamps `to`)
- Test: `internal/ui/chat_test.go` (or a new `roomfocus_test.go` case)

**Interfaces:**
- Consumes: `roomRoute` fields (`internal/ui/mention.go`: `mentioned bool`, `mode MentionMode`, embedded `core.Route` with `Targets []string`), `MentionDirect` (`mention.go`).
- Produces: `func (r Room) appendUser(ev core.Event, to string) Room`; `Append` unchanged in signature. Task 3 relies on `appendLine` carrying `to` and (later) the focus filter.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/roomfocus_test.go`:

```go
func TestRoomEchoCarriesAddressee(t *testing.T) {
	r := NewRoom().SetSize(80, 24)
	r = r.appendUser(core.Event{Kind: core.KindUserText, Text: "@iris rebase"}, "iris-id")
	lines := r.said.slice(r.said.first(), r.said.len())
	if len(lines) == 0 {
		t.Fatal("expected one room line")
	}
	if got := lines[len(lines)-1].to; got != "iris-id" {
		t.Fatalf("room echo to = %q, want %q", got, "iris-id")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRoomEchoCarriesAddressee -v`
Expected: FAIL — `r.appendUser undefined`.

- [ ] **Step 3: Refactor `Append` around a private `appendLine`, add `appendUser`**

In `internal/ui/chat.go`, replace the body of `Append` with a delegation and add the two helpers. (The focus-filter branch is added in Task 3 — for now `appendLine` is exactly today's `Append` body with a `to` parameter threaded onto the stored line.)

```go
// Append draws one event, attributed to the agent that produced it. (Doc comment
// unchanged — keep the existing one above.)
func (r Room) Append(ev core.Event, by Agent) Room { return r.appendLine(ev, by, "") }

// appendUser draws the operator's own room echo, stamped with the agent it was
// addressed to (or "" for a broadcast). Only this path carries a recipient.
func (r Room) appendUser(ev core.Event, to string) Room { return r.appendLine(ev, Agent{}, to) }

func (r Room) appendLine(ev core.Event, by Agent, to string) Room {
	b := renderRoomBlock(ev, by, r.blockWidth())
	if b.text == "" {
		return r
	}
	following := r.tr.atBottom()
	r.lineMoves = nil

	r.nextLineID++
	var rows int
	r.tr, rows = r.tr.addMeasured(b)
	r.said = r.said.append(roomLine{ev: ev, by: by, to: to, id: r.nextLineID, rows: rows})
	if drop := r.said.count() - roomRetentionEvents; drop > 0 {
		r = r.reclaimOldest(drop)
	}
	if following {
		r.tr = r.tr.toBottom()
	}
	return r
}
```

- [ ] **Step 4: Stamp `to` in `sendRoom`**

In `internal/ui/send.go`, `sendRoom`, replace the room-echo line (currently `a = a.withRoom(a.room.Append(core.Event{Kind: core.KindUserText, Text: text}, Agent{}))`) with:

```go
	// The room is the record of who you said it to. A lone direct @name is
	// addressed to that agent; open mode, @all, @manager and an unaddressed
	// draft are all broadcasts (to == "") — open mode widens the message, not
	// the view. This is the one datum the view filter needs beyond who-spoke.
	to := ""
	if r.mentioned && r.mode == MentionDirect && len(r.Targets) > 0 {
		to = r.Targets[0]
	}
	a = a.withRoom(a.room.appendUser(core.Event{Kind: core.KindUserText, Text: text}, to))
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/ui/ -run 'TestRoomEchoCarriesAddressee|TestFocusAdmits' -v`
Expected: PASS. Then `go test ./internal/ui/ -run TestNothingRoutesARoomDraft -v` to confirm the send path still routes as before.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/chat.go internal/ui/send.go internal/ui/roomfocus_test.go
git commit -m "feat: stamp the addressee on the room's own echo"
```

---

### Task 3: Filter the render paths + `Room.focus`/`WithFocus` + header

**Files:**
- Modify: `internal/ui/chat.go` (`Room` fields; `WithFocus`; `renderAll` filter; `appendLine` hidden branch; `View` header)
- Test: `internal/ui/roomfocus_test.go`

**Interfaces:**
- Consumes: `focusAdmits` (Task 1); `renderRoom` seam / `renderAll` (`chat.go`); `roomBanner`, `blockLines`, `renderRoomBlock` (`chat.go`/`chat_blocks.go`).
- Produces: `func (r Room) WithFocus(focus, focusName, managerID string) Room`; `Room.focus` (read in-package by Task 4). Membership is now visible in `View`.

- [ ] **Step 1: Write the failing test**

Add to `internal/ui/roomfocus_test.go`:

```go
func TestRoomViewNarrowsToFocus(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	johnA := Agent{ID: john, Name: "john"}
	irisA := Agent{ID: iris, Name: "iris"}
	mgrA := Agent{ID: mgr, Name: core.ManagerName}

	r := NewRoom().SetSize(80, 24)
	r = r.appendUser(core.Event{Kind: core.KindUserText, Text: "everyone: status?"}, "")
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: john, Text: "john here, all green"}, johnA)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: "iris here, still building"}, irisA)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: mgr, Text: "manager coordinating"}, mgrA)
	r = r.appendUser(core.Event{Kind: core.KindUserText, Text: "@iris hurry"}, iris)

	// Unfocused: everything is present.
	full := r.View(80, 24)
	for _, want := range []string{"all green", "still building", "manager coordinating", "hurry"} {
		if !strings.Contains(full, want) {
			t.Fatalf("unfocused room missing %q", want)
		}
	}

	// Focused on john: john + manager + your broadcast stay; iris's prose and your @iris message go.
	r = r.WithFocus(john, "john", mgr)
	got := r.View(80, 24)
	for _, want := range []string{"status?", "all green", "manager coordinating"} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused room missing %q, want it kept", want)
		}
	}
	for _, gone := range []string{"still building", "hurry"} {
		if strings.Contains(got, gone) {
			t.Fatalf("focused room still shows %q, want it hidden", gone)
		}
	}
	if !strings.Contains(got, "› @john") {
		t.Fatalf("focused room header missing the affordance")
	}

	// Unfocus restores everything.
	r = r.WithFocus("", "", mgr)
	back := r.View(80, 24)
	if !strings.Contains(back, "still building") || !strings.Contains(back, "hurry") {
		t.Fatalf("unfocus did not restore hidden lines")
	}
}
```

(Ensure `roomfocus_test.go` imports `strings` and `core`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestRoomViewNarrowsToFocus -v`
Expected: FAIL — `r.WithFocus undefined`.

- [ ] **Step 3: Add the `Room` fields**

In `internal/ui/chat.go`, add to the `Room` struct (near `reclaimed`/`hist`):

```go
	// focus narrows the transcript to one agent's thread; "" is the unfocused
	// room. managerID and focusName ride with it — the predicate needs the
	// manager's id, the header needs the focused agent's name. Persistent, not
	// draw-only: every render path (a width re-wrap, a history merge) must apply
	// the same filter. See roomfocus.go and WithFocus.
	focus     string
	managerID string
	focusName string
```

- [ ] **Step 4: Add `WithFocus` (re-render on change, like a width change)**

In `internal/ui/chat.go`:

```go
// WithFocus narrows the room to one agent's thread, or widens it when focus is
// "". A change re-renders the filtered subset and jumps to bottom — a width
// change's own semantics (SetSize sets following=true), the natural read when
// you enter a focus. managerID/focusName ride along; a change in either the
// focused agent or the manager re-filters, everything else is a no-op so idle
// typing after the name resolves costs nothing. The caller clears any text
// selection on a focus change (App.retarget) — a re-render renumbers the lines a
// selection is anchored to, the same reason a width change clears it.
func (r Room) WithFocus(focus, focusName, managerID string) Room {
	r.focusName = focusName
	if r.focus == focus && r.managerID == managerID {
		return r
	}
	r.focus = focus
	r.managerID = managerID
	lines := r.said.slice(r.said.first(), r.said.len())
	blocks := renderRoom(r, lines)
	first := r.said.first()
	r.said = chunked[roomLine]{base: first, n: first}.append(lines...)
	r.tr = r.tr.replace(blocks)
	r.tr = r.tr.toBottom()
	return r
}
```

- [ ] **Step 5: Apply the filter in `renderAll`**

In `internal/ui/chat.go`, `renderAll`, inside the `for i := range lines` loop, skip a hidden line before rendering it (this also saves the glamour cost of rendering what will not show):

```go
	for i := range lines {
		lines[i].rows = 0
		if r.focus != "" && !focusAdmits(lines[i], r.focus, r.managerID) {
			continue // hidden by focus: rows stays 0, no block — geometry sums 0
		}
		b := renderRoomBlock(lines[i].ev, lines[i].by, r.blockWidth())
		if b.text != "" {
			b.laidOut = blockLines(b, false)
			lines[i].rows = len(b.laidOut)
			blocks = append(blocks, b)
		}
	}
```

- [ ] **Step 6: Apply the filter in `appendLine` (incremental path)**

In `internal/ui/chat.go`, `appendLine`, record a hidden line in `said` at `rows == 0` with no `tr` block, so unfocusing brings it back. Replace the body added in Task 2 with:

```go
func (r Room) appendLine(ev core.Event, by Agent, to string) Room {
	following := r.tr.atBottom()
	r.lineMoves = nil
	r.nextLineID++
	line := roomLine{ev: ev, by: by, to: to, id: r.nextLineID}

	if r.focus != "" && !focusAdmits(line, r.focus, r.managerID) {
		// Room-worthy but hidden by the current focus: kept in said (canonical,
		// so unfocus restores it) at rows == 0, with no rendered block.
		r.said = r.said.append(line)
	} else {
		b := renderRoomBlock(ev, by, r.blockWidth())
		if b.text == "" {
			return r // truly empty (defensive; fold pre-drops blanks)
		}
		var rows int
		r.tr, rows = r.tr.addMeasured(b)
		line.rows = rows
		r.said = r.said.append(line)
	}
	if drop := r.said.count() - roomRetentionEvents; drop > 0 {
		r = r.reclaimOldest(drop)
	}
	if following {
		r.tr = r.tr.toBottom()
	}
	return r
}
```

- [ ] **Step 7: Add the header affordance in `View`**

In `internal/ui/chat.go`, `View`, replace the final return so the pane names the focused agent:

```go
	title := roomTitle
	if r.focus != "" && r.focusName != "" {
		title = roomTitle + " › @" + r.focusName
	}
	return strings.Join(append(rows, r.composer.WithTitle(cmp.Or(r.writing, title)).View(w)), "\n")
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestRoomViewNarrowsToFocus -v`
Expected: PASS. Then `go test ./internal/ui/ -run 'TestRoom|TestFocus' -v` to confirm no room regression.

- [ ] **Step 9: Commit**

```bash
git add internal/ui/chat.go internal/ui/roomfocus_test.go
git commit -m "feat: render the room narrowed to the focused agent's thread"
```

---

### Task 4: Wire the live query through `App.retarget`

**Files:**
- Modify: `internal/ui/send.go` (`retarget`)
- Test: `internal/ui/frame_test.go` (or a new App-level test in `roomfocus_test.go`)

**Interfaces:**
- Consumes: `App.route` (`mention.go`), `App.fleet.Agent(id) (Agent, bool)`, `App.fleet.manager() (Agent, bool)` (`service.go`), `Room.WithFocus` (Task 3), `App.sel`/`App.selecting` (`app.go`), `roomRoute.mentioned`/`mode`/`Targets`.
- Produces: the end-to-end behavior — a resolved lone direct `@name` in the room composer narrows the room; a change or clear widens it.

- [ ] **Step 1: Write the failing test**

Add an App-level test (put it in `internal/ui/roomfocus_test.go`; model the App construction on an existing `frame_test.go` test — use the same fleet/report seeding helper those tests use, e.g. build an `App` with two live agents `john` and `iris` via the test's report path):

```go
func TestTypingAtNameNarrowsTheRoom(t *testing.T) {
	// Build an App with a room and two live agents, john and iris, each having
	// spoken one line into the room. (Reuse the frame_test seeding helper — see
	// the existing App/room tests in this package for the exact constructor.)
	a := roomAppWith(t, /* john says */ "john all green", /* iris says */ "iris building")

	// No mention: the room shows both.
	if v := a.roomView(); !strings.Contains(v, "green") || !strings.Contains(v, "building") {
		t.Fatalf("unfocused room should show both agents")
	}

	// Type "@john ": the room narrows to john.
	a = a.typeInRoom("@john ")
	if v := a.roomView(); !strings.Contains(v, "green") || strings.Contains(v, "building") {
		t.Fatalf("room did not narrow to @john")
	}

	// Clear the draft: the room widens again.
	a = a.clearRoomDraft()
	if v := a.roomView(); !strings.Contains(v, "building") {
		t.Fatalf("clearing the draft did not widen the room")
	}
}
```

> The helpers `roomAppWith`, `roomView`, `typeInRoom`, `clearRoomDraft` are illustrative names — bind them to the equivalents the existing `frame_test.go` uses (App construction, `App.View`/room-pane extraction, feeding key messages through `App.Update`, and clearing the composer). If none exists, add small in-package test helpers; do not add production code for tests.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ui/ -run TestTypingAtNameNarrowsTheRoom -v`
Expected: FAIL — the room does not narrow (retarget does not set focus yet).

- [ ] **Step 3: Set focus from `retarget`**

In `internal/ui/send.go`, replace `retarget` with:

```go
func (a App) retarget() App {
	draft := a.room.Composer().Value()
	prev := a.room.focus
	managerID := ""
	if m, ok := a.fleet.manager(); ok {
		managerID = m.ID
	}
	if strings.TrimSpace(draft) == "" {
		a.room = a.room.WithComposer(a.room.Composer().WithTarget(roomRoute{}, 0)).WithFocus("", "", managerID)
		if a.room.focus != prev {
			a.sel, a.selecting = selection{}, false
		}
		return a
	}
	r := a.route(a.room.Composer().WireText(draft))
	a.room = a.room.WithComposer(a.room.Composer().WithTarget(r, len(r.Targets)))
	// A lone direct @name narrows the view; open mode is a broadcast (it widens
	// the message, not the view) so it does not, and @all/@manager/no-mention
	// resolve mentioned == false.
	focusID, focusName := "", ""
	if r.mentioned && r.mode == MentionDirect && len(r.Targets) > 0 {
		if ag, ok := a.fleet.Agent(r.Targets[0]); ok {
			focusID, focusName = ag.ID, ag.Name
		}
	}
	a.room = a.room.WithFocus(focusID, focusName, managerID)
	if a.room.focus != prev {
		a.sel, a.selecting = selection{}, false
	}
	return a
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ui/ -run TestTypingAtNameNarrowsTheRoom -v`
Expected: PASS.

- [ ] **Step 5: Run the wider UI suite**

Run: `go test ./internal/ui/ -v 2>&1 | tail -30`
Expected: PASS — in particular the existing `retarget`/composer-target and selection tests, which now share this path.

- [ ] **Step 6: Commit**

```bash
git add internal/ui/send.go internal/ui/roomfocus_test.go
git commit -m "feat: narrow the room live off the composer's @name target"
```

---

### Task 5: History narrows, and a re-wrap under focus keeps hidden lines hidden

**Files:**
- Test only: `internal/ui/roomhistory_test.go` (or `roomfocus_test.go`)

**Interfaces:**
- Consumes: `Room.Before` (`chat.go`), `roomHistoryLines` (`roomhistory.go`), `Room.SetSize` (`chat.go`), `WithFocus` (Task 3).
- Produces: regression proof of spec §3 (history, no reconstruction) and the §5 risk (re-wrap must not re-admit hidden lines).

- [ ] **Step 1: Write the history-narrowing test**

The historical predicate falls out of `renderAll` (which `Before` reaches through `renderRoom`), so this is a proof, not new production code. Add:

```go
func TestFocusNarrowsRestoredHistory(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	// Build restored room lines directly (a broadcast, john's reply, iris's
	// reply, a manager line), then fold them in via Before and focus john.
	// Use the same roomLine shape roomHistoryLines emits: a KindUserText
	// broadcast is attributed to nobody (by = Agent{}, to = "").
	r := NewRoom().SetSize(80, 24)
	r = r.Before([]roomLine{
		{ev: core.Event{Kind: core.KindUserText, Text: "everyone: status?", At: at(1)}},
		{ev: core.Event{Kind: core.KindAssistantText, SessionID: john, Text: "john green", At: at(2)}, by: Agent{ID: john, Name: "john"}},
		{ev: core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: "iris building", At: at(3)}, by: Agent{ID: iris, Name: "iris"}},
		{ev: core.Event{Kind: core.KindAssistantText, SessionID: mgr, Text: "mgr coordinating", At: at(4)}, by: Agent{ID: mgr, Name: core.ManagerName}},
	})
	r = r.WithFocus(john, "john", mgr)
	got := r.View(80, 24)
	for _, want := range []string{"status?", "john green", "mgr coordinating"} {
		if !strings.Contains(got, want) {
			t.Fatalf("focused history missing %q", want)
		}
	}
	if strings.Contains(got, "iris building") {
		t.Fatalf("focused history still shows iris's restored prose")
	}
}
```

> `at(n)` is a helper for a monotonic non-zero `time.Time` — reuse the existing one in `roomhistory_test.go` if present, else add `func at(n int) time.Time { return time.Unix(int64(n), 0) }` in the test file. Non-zero `At` matters: a zero `At` is treated as a live line by the merge.

- [ ] **Step 2: Write the re-wrap-under-focus regression test**

```go
func TestReWrapUnderFocusKeepsHiddenLinesHidden(t *testing.T) {
	const john, iris, mgr = "john-id", "iris-id", "mgr-id"
	r := NewRoom().SetSize(80, 24)
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: john, Text: "john green"}, Agent{ID: john, Name: "john"})
	r = r.Append(core.Event{Kind: core.KindAssistantText, SessionID: iris, Text: "iris building"}, Agent{ID: iris, Name: "iris"})
	r = r.WithFocus(john, "john", mgr)
	// A width change re-wraps through renderAll and must re-apply the filter.
	r = r.SetSize(60, 24)
	got := r.View(60, 24)
	if !strings.Contains(got, "john green") {
		t.Fatalf("re-wrap dropped the focused agent's line")
	}
	if strings.Contains(got, "iris building") {
		t.Fatalf("re-wrap re-admitted a hidden line")
	}
}
```

- [ ] **Step 3: Run both tests**

Run: `go test ./internal/ui/ -run 'TestFocusNarrowsRestoredHistory|TestReWrapUnderFocusKeepsHiddenLinesHidden' -v`
Expected: PASS. If the history test fails, the fault is that `Before` does not thread the current focus into its `renderRoom(r, combined)` — confirm `Before` uses the receiver `r` (it does) so `r.focus` is in scope. No production change should be needed; if one is, it belongs here with its own commit.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/roomhistory_test.go
git commit -m "test: room focus narrows restored history and survives a re-wrap"
```

---

### Task 6: A pty screen test for the live round trip

**Files:**
- Test: `cmd/wake/roomfilterscreen_unix_test.go` (new)

**Interfaces:**
- Consumes: the real binary through the existing pty/`vt10x` harness — model it exactly on an existing screen test that already drives the room and types keys. Read `cmd/wake/rewindscreen_unix_test.go` and `cmd/wake/screen_unix_test.go` for the harness API (fleet seeding, feeding bytes, reading the framed screen). Do **not** invent harness calls.

- [ ] **Step 1: Write the screen test, modeled on the existing harness**

Scenario: a fleet with two live agents (`john`, `iris`) that have each spoken a room line; open on the room; type `@john ` and assert the rendered room pane contains john's line and not iris's; then clear the composer (backspace to empty) and assert iris's line is back.

Follow the existing screen test's structure precisely. The assertions are the payload:

```go
//go:build unix

package main

// ... imports and harness setup copied from the sibling screen test ...

func TestRoomNarrowsToAtNameOnScreen(t *testing.T) {
	// Seed a fleet with john and iris, each having produced one room line,
	// open on the room, and get a screen handle — exactly as the sibling
	// room screen test does.
	screen := /* harness: start binary on the room with john+iris seeded */

	// Baseline: both agents visible.
	assertContains(t, screen.Room(), "john") // john's line text
	assertContains(t, screen.Room(), "iris") // iris's line text

	// Type "@john " — the room narrows.
	screen.Type("@john ")
	assertContains(t, screen.Room(), "john")
	assertNotContains(t, screen.Room(), "iris")
	assertContains(t, screen.Room(), "› @john") // the header affordance

	// Clear the draft — the room widens.
	screen.Backspace(len("@john "))
	assertContains(t, screen.Room(), "iris")
}
```

> `screen.Room()`, `screen.Type`, `screen.Backspace`, `assertContains`/`assertNotContains` are stand-ins for whatever the harness in the sibling file actually exposes. Bind them to the real API. Keep the scenario and the four assertions.

- [ ] **Step 2: Run it**

Run: `go test ./cmd/wake/ -run TestRoomNarrowsToAtNameOnScreen -v`
Expected: PASS. (Run from a normal checkout under your home directory — the screen tests render the working directory and fail under `/tmp` or a long temp path.)

- [ ] **Step 3: Commit**

```bash
git add cmd/wake/roomfilterscreen_unix_test.go
git commit -m "test: the room narrows to @name on a real screen"
```

---

### Task 7: Docs, deferred entry, and the gate

**Files:**
- Modify: `docs/notes/deferred.md` (mark #7 addressed)
- Modify: `CLAUDE.md` (one behavior line — no legend/number change)

- [ ] **Step 1: Note the feature in `CLAUDE.md`**

In the "What it does today" behavior table (near the room's Slash/keys rows), add one concise row or sentence — e.g. under Slash commands / room behavior: "A lone `@name` in the room **narrows the group chat to that agent's thread** (their lines, the manager's, broadcasts, and your messages to them) as long as it is the composer's target; it widens again when the target changes or clears." Do **not** touch the legend paragraph, the two-largest-files sentence, or any derived number.

- [ ] **Step 2: Mark the deferred entry**

In `docs/notes/deferred.md`, under "2026-08-25 — `@john` as a room *view* filter", change the closing line to record that it shipped on `feat/room-view-filter` with the three rulings taken (trigger = live query; scope = john + manager + broadcasts + your-to-john; history = yes), pointing at the spec `docs/superpowers/specs/2026-08-27-room-view-filter-design.md`.

- [ ] **Step 3: Run the full gate**

Run: `make ci`
Expected: exit 0. Read the exit code — it is the only evidence the PR gets. If coverage on the new `internal/ui` code dips below the floor, add the missing predicate/branch cases to `roomfocus_test.go`.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md docs/notes/deferred.md
git commit -m "docs: record the room @name view filter"
```

---

## Post-implementation: the two reviews (project rule — before the PR)

Per `CLAUDE.md`'s Git section, **two reviews run before the PR is opened**, and both findings plus the `make ci` exit code go in the PR body:

1. **Code review** — correctness, the non-negotiables (airlock untouched, pure `focusAdmits`, immutable `Room`, no legend change), and conventions. Dispatch the `code-reviewer` agent or an equivalent read of the diff.
2. **Adversarial review** — try to break the claims. Prime targets: (a) the `said`↔`tr` rows=0 invariant under reclaim while focused; (b) a focus change mid-scroll (selection cleared, jump-to-bottom); (c) open-mode `@john` must **not** narrow; (d) the history test genuinely goes red if `Before` stops threading focus; (e) `make test`'s non-race pass, since this touches ordering-sensitive render paths.

Then open the PR into `main` with the branch `feat/room-view-filter`, the two reviews' findings, and the `make ci` exit code in the body. No Claude attribution.

## Self-review notes (author)

- **Spec coverage:** §1 free/not-free → Tasks 1–2; §2 predicate → Task 1; §3 history → Task 5; §4 `to` stamp → Task 2; §5 subset render → Task 3 (rows=0 refinement); §6 retarget wire → Task 4; §7 affordance → Task 3 step 7; §8 tests → Tasks 1,3,4,5,6; §9 files → all. No gap.
- **Placeholder scan:** the only illustrative names are the App-level test helpers (Task 4) and the pty harness calls (Task 6), each flagged to bind to the existing harness rather than invent — the surrounding scenario and assertions are concrete. Every production code block is complete.
- **Type consistency:** `focusAdmits(l roomLine, focus, managerID string) bool`, `WithFocus(focus, focusName, managerID string)`, `appendLine(ev, by, to)`, `appendUser(ev, to)`, `roomLine.to string` — used identically across tasks.
