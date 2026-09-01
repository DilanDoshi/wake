# Effort Probe + Status Bar Restructure — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make model, context, and effort visible in a status bar on both the room and conversations, with the info row above the helpers row — and confirm effort by an invisible daemon `/model` probe.

**Architecture:** Two halves. (1) A daemon-side probe: the daemon sends a bare `/model` (a local, ~11ms, $0, zero-inference CLI reply), extracts `(effort: <level>)`, stores a confirmed effort, and suppresses the reply from clients + on-disk history. (2) A UI restructure: the status bar gains an effort segment and moves above the legend; the room gets a bar following the composer's `@`-target or the manager.

**Tech Stack:** Go 1.26+, bubbletea/lipgloss/bubbles/glamour. Tests: `go test`, the pty screen harness, recorded stream-json fixtures.

**Spec:** `docs/superpowers/specs/2026-08-28-effort-probe-and-status-bar-design.md`

## Global Constraints

- **Only `internal/core`'s four airlock files know Claude's JSON** — the reply recognizer/parser go in `internal/core/vocabulary.go`. Enforced by `airlock_test.go`.
- **The UI never touches an agent's process** — the probe is entirely daemon-side.
- **Cheap to leave open** — the probe fires once per session + once per `/effort`, never on a timer.
- **Wake owns almost no state** — `confirmedEffort` is transient; not persisted to `parked.json`.
- **No literal slash command in the daemon** — `slashguard_test.go` walks the daemon package; compose `/model` as `slashPrefix + modelVerb`, the way `effort.go` composes `/effort`.
- **Files: 200–400 lines typical, 800 hard max. Functions < 50 lines. Handle every error. Immutable where practical.**
- **Run tests from this worktree (`~/Documents/wake-status-probe`)** — screen tests fail under `/tmp` or long/`effort`-containing paths.
- **`make ci` exit 0 is the only gate.** Commit type `feat:`; the branch is `feat/effort-probe-status-bar`.

---

### Task 1: core — recognize and parse a `/model` reply

**Files:**
- Modify: `internal/core/vocabulary.go`
- Test: `internal/core/vocabulary_test.go` (add), assert against `testdata/stream/bare-model.jsonl`

**Interfaces:**
- Produces: `func IsModelReply(text string) bool`, `func EffortFromModelReply(text string) (string, bool)`

- [ ] **Step 1: Write the failing test.** Table test plus a fixture assertion. The fixture's assistant text is `Current model: Opus 5 (1M context) (effort: xhigh)\nUsage: /model <name>. Available: …`.

```go
func TestIsModelReply(t *testing.T) {
	cases := map[string]bool{
		"Current model: Opus 5 (1M context) (effort: xhigh)\nUsage: /model <name>.": true,
		"Current model: Sonnet 5 (effort: medium)":                                 true,
		"Sure, the current model is opus":                                          false,
		"":                                                                         false,
	}
	for in, want := range cases {
		if got := IsModelReply(in); got != want {
			t.Errorf("IsModelReply(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestEffortFromModelReply(t *testing.T) {
	lvl, ok := EffortFromModelReply("Current model: Opus 5 (1M context) (effort: xhigh)")
	if !ok || lvl != "xhigh" {
		t.Fatalf("got (%q,%v), want (\"xhigh\",true)", lvl, ok)
	}
	if _, ok := EffortFromModelReply("Current model: Opus 5"); ok {
		t.Error("a reply with no effort clause must not parse")
	}
	if _, ok := EffortFromModelReply("Current model: X (effort: bogus)"); ok {
		t.Error("an effort not in EffortCommands must not parse")
	}
}
```

- [ ] **Step 2: Run to verify it fails.** `go test ./internal/core/ -run 'ModelReply' -count=1` → FAIL (undefined).

- [ ] **Step 3: Implement in `vocabulary.go`**, beside the other rendered-string recognizers. Use a compiled `regexp` for the effort clause; validate against `EffortCommands`.

```go
// A bare /model reply names the session's model and reasoning level:
//   Current model: Opus 5 (1M context) (effort: xhigh)
// It is the one place a session's effort is reported back (bare-model.jsonl),
// so the daemon reads it to confirm the level it asked for. Recognised here
// because it is Claude's rendered output shape, which is the airlock's job.
const modelReplyPrefix = "Current model:"

var effortClause = regexp.MustCompile(`\(effort:\s*([a-zA-Z]+)\)`)

func IsModelReply(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), modelReplyPrefix)
}

func EffortFromModelReply(text string) (string, bool) {
	m := effortClause.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	level := strings.ToLower(m[1])
	if !ValidEffortCommand(level) {
		return "", false
	}
	return level, true
}
```

Add `"regexp"` to the imports if absent.

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/core/ -run 'ModelReply' -count=1` → PASS. Then `go test ./internal/core/ -count=1` (airlock guard still green).

- [ ] **Step 5: Commit.** `git add internal/core/vocabulary.go internal/core/vocabulary_test.go && git commit -m "feat: recognize and parse a bare /model reply for effort"`

---

### Task 2: daemon — a confirmed-effort field, published for display

**Files:**
- Modify: `internal/daemon/agent.go` (field near line 128; `snapshot` line 669)
- Modify: `internal/daemon/effort.go` (a locked setter beside `currentEffort`)
- Test: `internal/daemon/effort_test.go` (add)

**Interfaces:**
- Produces: `agent.confirmedEffort string`, `func (a *agent) setConfirmedEffort(level string)`; `snapshot().Effort` becomes `cmp.Or(a.confirmedEffort, a.effort)`.

- [ ] **Step 1: Write the failing test.**

```go
func TestSnapshotPrefersConfirmedEffort(t *testing.T) {
	a := &agent{id: "x", effort: "high"}
	if got := a.snapshot().Effort; got != "high" {
		t.Fatalf("asked-for should show when nothing confirmed: %q", got)
	}
	a.setConfirmedEffort("xhigh")
	if got := a.snapshot().Effort; got != "xhigh" {
		t.Fatalf("confirmed should win: %q", got)
	}
}
```

- [ ] **Step 2: Run to verify it fails.** `go test ./internal/daemon/ -run TestSnapshotPrefersConfirmedEffort -count=1` → FAIL.

- [ ] **Step 3: Implement.** Add `confirmedEffort string` beside `effort` in the agent struct (agent.go ~128). In `snapshot` change `Effort: a.effort` → `Effort: cmp.Or(a.confirmedEffort, a.effort)` (add `"cmp"` import). Add the setter in `effort.go`:

```go
// setConfirmedEffort records the level a /model probe read back. Display prefers
// it over the asked-for level; park still relaunches from currentEffort (the
// level --effort accepts), so this never reaches an argv.
func (a *agent) setConfirmedEffort(level string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.confirmedEffort = level
}
```

Leave `park.go:446` (`argvEffort(a.currentEffort(), …)`) untouched — relaunch uses asked-for.

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/daemon/ -run TestSnapshotPrefersConfirmedEffort -count=1` → PASS.

- [ ] **Step 5: Commit.** `git add internal/daemon/agent.go internal/daemon/effort.go internal/daemon/effort_test.go && git commit -m "feat: publish confirmed-or-asked-for effort in the status snapshot"`

---

### Task 3: daemon — send the probe (daemon-originated, invisible)

**Files:**
- Modify: `internal/daemon/apply.go` (`pending` line 25; `apply` FrameSend arm line 72)
- Modify: `internal/daemon/effort.go` (compose `/model`; `probeEffort`; `noteEffort` returns changed)
- Test: `internal/daemon/effort_test.go`

**Interfaces:**
- Produces: `pending.probe bool`; `agent.probing bool`; `func (a *agent) probeEffort()`; `func (a *agent) noteEffort(text string) bool` (now returns whether it recorded a level).
- Consumes: `core` compose of `/model` via `slashPrefix + modelVerb`.

- [ ] **Step 1: Write the failing test** (probe enqueues a bare `/model`; a full/gone queue is a no-op; a probe send does not count as an operator turn). Use the package's existing fake-session harness pattern (see other `daemon` tests for the fake).

```go
func TestProbeEnqueuesBareModel(t *testing.T) {
	a := newFakeAgent(t) // existing helper; fake sess records Send calls
	go a.serveInput()
	a.probeEffort()
	got := a.fake.waitSend(t)           // helper: blocks for one Send
	if got != "/model" {
		t.Fatalf("probe sent %q, want /model", got)
	}
	if a.owedNow() {                    // probe must not mark a turn owed
		t.Error("a probe counted as an operator turn")
	}
}
```

(If no `newFakeAgent`/`waitSend` helper exists, add a minimal one in the test file over the existing fake `core.Session` used by other daemon tests.)

- [ ] **Step 2: Run to verify it fails.** → FAIL (undefined `probeEffort`).

- [ ] **Step 3: Implement.**
  - `apply.go`: `type pending struct { from *client; frame rpc.Frame; probe bool }`. In `apply`, replace the `FrameSend` arm:

```go
case rpc.FrameSend:
	if p.probe {
		a.setProbing(true)
		if err := a.sess.Send(p.frame.Text, nil); err != nil {
			a.setProbing(false)
			logf("wake: session %s: effort probe not sent: %v", a.id, err)
		}
		return // no noteSent, no noteEffort, no client to report to
	}
	if err = a.sess.Send(p.frame.Text, p.frame.Images); err == nil {
		a.noteSent()
		if a.noteEffort(p.frame.Text) {
			a.probeEffort() // confirm the level just set
		}
	}
```

  - `effort.go`: add `modelVerb = "model"` beside `effortVerb`; make `noteEffort` return `bool` (`return true` after `a.effort = level`; `return false` on every early exit); add:

```go
// probeEffort asks the session for its level with a bare /model — a local CLI
// reply (num_turns:0, $0) the daemon reads and suppresses. Best-effort: skipped
// for an agent that is gone or blocked on an ask (its stdin is closed, and a
// probe there is indistinguishable from an operator answer).
func (a *agent) probeEffort() {
	if a.blockedOnAsk() {
		return
	}
	select {
	case <-a.gone:
		return
	default:
	}
	select {
	case a.in <- pending{probe: true, frame: rpc.Frame{Kind: rpc.FrameSend, SessionID: a.id, Text: slashPrefix + modelVerb}}:
	default: // queue full: skip, effort simply won't refresh this cycle
	}
}
```

  - `agent.go`: add `probing bool` to the struct and the two locked helpers (place beside `changed`):

```go
func (a *agent) setProbing(v bool) { a.mu.Lock(); a.probing = v; a.mu.Unlock() }
```

  - Add `a.owedNow()` test helper if none exists (locked read of `a.owed`).

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/daemon/ -run 'Probe|Effort' -count=1` → PASS.

- [ ] **Step 5: Commit.** `git add internal/daemon/apply.go internal/daemon/effort.go internal/daemon/agent.go internal/daemon/effort_test.go && git commit -m "feat: daemon-originated effort probe send"`

---

### Task 4: daemon — suppress the reply and record the level

**Files:**
- Modify: `internal/daemon/spawn.go` (`fanOut` loop, lines 664-688)
- Modify: `internal/daemon/agent.go` (`absorbProbe`)
- Test: `internal/daemon/spawn_test.go` or `fanout_test.go`

**Interfaces:**
- Produces: `func (a *agent) absorbProbe(ev core.Event) (suppress, publish bool)`
- Consumes: `core.IsModelReply`, `core.EffortFromModelReply`, `core.KindTurnEnd` (Task 1).

- [ ] **Step 1: Write the failing test.** A fake session that, while `probing`, emits an assistant `Current model: … (effort: max)` then a `KindTurnEnd` with the same text: assert the client received neither event frame, exactly one status push carrying `Effort:"max"`, and `confirmedEffort == "max"`.

```go
func TestProbeReplyIsSuppressedAndRecorded(t *testing.T) {
	srv, cli := newServerWithClient(t)         // existing test harness
	a := srv.spawnFake(t)
	a.setProbing(true)
	a.fake.emit(core.Event{Kind: core.KindAssistantText, Text: "Current model: Opus 5 (effort: max)"})
	a.fake.emit(core.Event{Kind: core.KindTurnEnd, Text: "Current model: Opus 5 (effort: max)\nUsage: /model"})
	frames := cli.drain(t)
	for _, f := range frames {
		if f.Kind == rpc.FrameEvent && core.IsModelReply(f.Event.Text) {
			t.Fatal("a probe reply reached a client")
		}
	}
	if a.confirmedEffortNow() != "max" {
		t.Fatalf("confirmed effort not recorded: %q", a.confirmedEffortNow())
	}
}
```

- [ ] **Step 2: Run to verify it fails.** → FAIL (reply broadcast / undefined `absorbProbe`).

- [ ] **Step 3: Implement.**
  - `agent.go`:

```go
// absorbProbe consumes a /model probe reply so it never reaches a client.
// While probing, an event carrying the reply text is the probe's own: its
// assistant line is swallowed, and its turn end records the level and closes
// the window. Keyed on the reply text (not a blanket probing gate) so a real
// turn's frames still draining on another goroutine pass through untouched.
func (a *agent) absorbProbe(ev core.Event) (suppress, publish bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.probing || !core.IsModelReply(ev.Text) {
		return false, false
	}
	if ev.Kind == core.KindTurnEnd {
		if lvl, ok := core.EffortFromModelReply(ev.Text); ok {
			a.confirmedEffort = lvl
		}
		a.probing = false
		return true, true
	}
	return true, false
}
```

  - `spawn.go` `fanOut`, at the top of the loop body (before `a.observe`):

```go
for ev := range a.sess.Events() {
	if suppress, publish := a.absorbProbe(ev); suppress {
		if publish {
			s.broadcast(s.statusPush()) // publish the new effort, not the reply
		}
		continue // no observe, no event broadcast — the agent's state is untouched
	}
	a.observe(ev)
	...
}
```

  - Add test helper `confirmedEffortNow()` (locked read).

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/daemon/ -run 'ProbeReply' -count=1` → PASS, and with `-race`.

- [ ] **Step 5: Commit.** `git add internal/daemon/spawn.go internal/daemon/agent.go internal/daemon/*_test.go && git commit -m "feat: suppress the /model probe reply and record confirmed effort"`

---

### Task 5: daemon — fire the probe at startup and after /effort

**Files:**
- Modify: `internal/daemon/spawn.go` (`fanOut` — probe once on the first `init`)
- Modify: `internal/daemon/agent.go` (a `probed bool` guard)
- Test: `internal/daemon/spawn_test.go`

**Interfaces:**
- Consumes: `probeEffort` (Task 3), the `init`/system event kind (`core.KindSystem` init — confirm the exact kind emitted on init).

- [ ] **Step 1: Write the failing test.** A fake session that emits its `init`: assert exactly one `/model` is sent shortly after; a second `init` (per-turn) does not re-probe. Then a `/effort high` FrameSend triggers a probe; a non-effort message does not.

- [ ] **Step 2: Run to verify it fails.** → FAIL.

- [ ] **Step 3: Implement.** In `fanOut`, after `a.observe(ev)`, detect the first init and probe once:

```go
if a.firstInit(ev) { // true once, on the init that reports the session ready
	a.probeEffort()
}
```

Add `firstInit` in `agent.go` (locked): returns true when `ev` is the init/facts event and `!a.probed`, setting `a.probed = true`. Confirm the init event kind by reading `internal/core/protocol.go` `initFacts` and the `Event.Kind` it produces (likely `KindSystem` with `Session != nil`). The `/effort` re-probe is already wired in Task 3 (`noteEffort` returning true → `probeEffort`).

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/daemon/ -run 'Probe|Init|Effort' -count=1` → PASS.

- [ ] **Step 5: Commit.** `git add internal/daemon/spawn.go internal/daemon/agent.go internal/daemon/*_test.go && git commit -m "feat: probe effort on session init and after an /effort change"`

---

### Task 6: daemon — filter the probe pair out of on-disk history

**Files:**
- Modify: `internal/daemon/history.go` (`liveHistory`, lines 113-165)
- Test: `internal/daemon/history_test.go`

**Interfaces:**
- Consumes: `core.IsModelReply` (Task 1), `core.KindUserText`, `core.KindAssistantText`.

- [ ] **Step 1: Write the failing test.** Feed `liveHistory` a reader whose lines are: a normal user turn, a `user` line `/model`, an `assistant` line `Current model: … (effort: xhigh)`, a normal assistant turn. Assert the `/model` and `Current model:` events are absent and the two normal turns are present. (Use `testdata/transcript/`-shaped lines; add a small fixture if needed.)

- [ ] **Step 2: Run to verify it fails.** → FAIL (pair present).

- [ ] **Step 3: Implement.** Wrap the `keep` closure with a one-event lookahead:

```go
armed := false
keepFiltered := func(ev core.Event) {
	if armed {
		armed = false
		if ev.Kind == core.KindAssistantText && core.IsModelReply(ev.Text) {
			return // drop the probe reply
		}
	}
	if ev.Kind == core.KindUserText && strings.TrimSpace(ev.Text) == slashPrefix+modelVerb {
		armed = true
		return // drop the probe command
	}
	keep(ev)
}
```

Call `keepFiltered(ev)` in the decode loop instead of `keep(ev)`. Add `"strings"` if absent. (`slashPrefix`/`modelVerb` are in `effort.go`, same package.)

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/daemon/ -run 'History' -count=1` → PASS.

- [ ] **Step 5: Commit.** `git add internal/daemon/history.go internal/daemon/history_test.go && git commit -m "feat: filter the /model probe pair out of restored history"`

---

### Task 7: ui — effort segment in the status bar

**Files:**
- Modify: `internal/ui/statusbar.go` (header rewrite; `statusBar` segments line 76)
- Test: `internal/ui/statusbar_test.go`

**Interfaces:**
- Consumes: `Agent.Effort` (already carries confirmed-or-asked-for from the report).

- [ ] **Step 1: Write the failing test.** effort shown as `effort:xhigh` when set; absent when `""`; present after model/ctx in order.

```go
func TestStatusBarShowsEffort(t *testing.T) {
	a := Agent{Cwd: "/tmp/repo", Model: "claude-opus-5", Effort: "xhigh"}
	got := statusBar(a, "auto", 200)
	if !strings.Contains(got, "effort:xhigh") {
		t.Fatalf("no effort segment: %q", got)
	}
	bare := statusBar(Agent{Cwd: "/tmp/repo"}, "auto", 200)
	if strings.Contains(bare, "effort:") {
		t.Fatalf("effort drawn when unknown: %q", bare)
	}
}
```

- [ ] **Step 2: Run to verify it fails.** `go test ./internal/ui/ -run TestStatusBarShowsEffort -count=1` → FAIL.

- [ ] **Step 3: Implement.** Add an `effortSegment` and include it in `segments`:

```go
segments := []string{
	shortPath(a.Cwd),
	gitref.Of(a.Cwd).Name(),
	modelName(a.Model, a.ContextWindow),
	contextLeft(a.ContextTokens, a.ContextWindow),
	effortSegment(a.Effort),
}
```

```go
const effortLabel = "effort:"

// effortSegment is the reasoning level, confirmed by the /model probe or the
// one Wake asked for. Empty when neither is known — dropped, never guessed.
func effortSegment(level string) string {
	if level == "" {
		return ""
	}
	return effortLabel + level
}
```

Rewrite the file header (lines 23-43): the "effort is deliberately missing" essay is now false — effort is confirmed by the daemon's `/model` probe and carried on the report; the segment shows the confirmed level, the asked-for one until the probe returns, or nothing.

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/ui/ -run 'StatusBar' -count=1` → PASS.

- [ ] **Step 5: Commit.** `git add internal/ui/statusbar.go internal/ui/statusbar_test.go && git commit -m "feat: show effort in the status bar"`

---

### Task 8: ui — the composer carries a bar above its legend

**Files:**
- Modify: `internal/ui/composer.go` (`View` line 327; add `bar` field + `WithBar`)
- Test: `internal/ui/composer_test.go`

**Interfaces:**
- Produces: `func (c Composer) WithBar(bar string) Composer`; `Composer.View` renders `bar` between the target line and the hint.

- [ ] **Step 1: Write the failing test.** A composer with a bar renders it above the legend line.

```go
func TestComposerDrawsBarAboveLegend(t *testing.T) {
	c := NewComposer().WithBar("~/repo  main  Opus 5  effort:xhigh")
	out := c.View(120)
	lines := strings.Split(out, "\n")
	barAt, hintAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "effort:xhigh") { barAt = i }
		if strings.Contains(l, "send") { hintAt = i } // legend contains "↵ send"
	}
	if barAt < 0 || hintAt < 0 || barAt > hintAt {
		t.Fatalf("bar (%d) must sit above legend (%d):\n%s", barAt, hintAt, out)
	}
}
```

- [ ] **Step 2: Run to verify it fails.** → FAIL (no `WithBar`).

- [ ] **Step 3: Implement.** Add `bar string` to the `Composer` struct and:

```go
// WithBar sets the status/info line drawn between the box and the legend. The
// composer only places it; the caller renders it (it reads the filesystem),
// the same contract as the target line.
func (c Composer) WithBar(bar string) Composer { c.bar = bar; return c }
```

In `View`, after the `target` append and before the hint:

```go
if c.bar != "" {
	rows = append(rows, " "+oneRow(c.bar))
}
```

(`oneRow` is in `statusbar.go`, same package.) The hint is still appended last.

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/ui/ -run 'Composer' -count=1` → PASS.

- [ ] **Step 5: Commit.** `git add internal/ui/composer.go internal/ui/composer_test.go && git commit -m "feat: composer draws an info bar above its legend"`

---

### Task 9: ui — the DM feeds its bar into the composer

**Files:**
- Modify: `internal/ui/dm.go` (`View` — stop appending `d.bar` at 577-579; feed the composer; verify `chromeHeight`/dm.go:749-762)
- Test: `internal/ui/dm_test.go` and a screen test

**Interfaces:**
- Consumes: `Composer.WithBar` (Task 8), the cached `d.bar` (`withBar`, dm.go:235, unchanged).

- [ ] **Step 1: Write the failing test.** In a DM with a known agent, the bar renders **above** the legend, and `chromeHeight` equals the composer's measured height (no lost/extra row).

- [ ] **Step 2: Run to verify it fails.** → FAIL (bar below legend).

- [ ] **Step 3: Implement.** In `View`, remove the trailing `if d.bar != "" { rows = append(rows, d.bar) }` (577-579); change the composer line (574) to `.WithBar(d.bar).WithTitle(...)`. Audit `chromeHeight`/`baseChrome` and dm.go:749-762: any explicit `+1` for the appended bar is removed because the composer's measured `View` height now includes it. Keep `withBar`/`barKey` caching intact (still recomputed on change).

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/ui/ -run 'DM|Chrome|Pane' -count=1` → PASS; run the pty screen tests touching the DM footer.

- [ ] **Step 5: Commit.** `git add internal/ui/dm.go internal/ui/*_test.go && git commit -m "feat: move the DM status bar above the legend"`

---

### Task 10: ui — the room's info bar follows the @-target or manager

**Files:**
- Modify: `internal/ui/chat.go` (`Room` gains `bar`/`barFrom`; `withBar`; `View` line 574 feeds the composer)
- Modify: `internal/ui/app.go` (resolve the info-bar agent when refreshing the room)
- Test: `internal/ui/chat_test.go` / a screen test

**Interfaces:**
- Consumes: `statusBar` (Task 7), `Fleet.manager()`, `Room.focus`, `barKey` (dm.go:217).
- Produces: `func (r Room) withBar(a Agent, mode string, width int) Room`.

- [ ] **Step 1: Write the failing test.** With composer target `@thea`, the room bar shows thea's model/effort; with no target, it shows the manager's; with neither, the room draws no bar. The bar must **not** change when only the roster cursor (`↑↓`) moves.

- [ ] **Step 2: Run to verify it fails.** → FAIL.

- [ ] **Step 3: Implement.**
  - `chat.go`: add `bar string` + `barFrom barKey` to `Room`; add `withBar` mirroring `DM.withBar` (build a `barKey` from the agent + mode + width, recompute `drawStatusBar` only on change). In `View`, `r.composer.WithTitle(...).WithBar(r.bar).View(w)`.
  - `app.go`: where the App refreshes the room each update, resolve the info-bar agent: `id := r.focus; if id == "" { id = a.fleet.manager() }`; `agent, ok := a.fleet.Agent(id)`; `mode := a.modeOf(id)`; if `ok`, `room = room.withBar(agent, mode, width)` else `room = room.withBar(Agent{}, "", width)` (→ `""`). Confirm `Fleet.manager()`'s exact name/return in `internal/ui/service.go` and `modeOf`'s signature.

- [ ] **Step 4: Run to verify it passes.** `go test ./internal/ui/ -run 'Room' -count=1` → PASS; run the room-footer screen test.

- [ ] **Step 5: Commit.** `git add internal/ui/chat.go internal/ui/app.go internal/ui/*_test.go && git commit -m "feat: room info bar follows the addressed agent or manager"`

---

### Task 11: docs, guards, screen tests, and the gate

**Files:**
- Modify: `CLAUDE.md` (status-bar table row; room-line rows; the "Effort is the one thing Wake sets and cannot confirm" load-bearing rule; the key-locations table)
- Modify: `docs/notes/deferred.md` (mark the effort-readback entry built), `docs/notes/decisions.md` (the probe ruling)
- Modify: `internal/ui/statusbar.go` header (done in Task 7 — verify)
- Modify: screen tests under `cmd/wake/*_unix_test.go`, `internal/ui/frame_test.go`

- [ ] **Step 1: Update the screen/frame tests** to the new footer order (info above legend) and the room's new bar. Run `go test ./cmd/wake/ ./internal/ui/ -count=1` and fix assertions to match the intended new layout (update, do not weaken).

- [ ] **Step 2: Rewrite the docs.** In `CLAUDE.md`: the status-bar row now lists effort and says the info sits above the legend; note the room draws an info bar for the addressed/manager agent; **rewrite** the "Effort is the one thing Wake sets and cannot confirm" rule to describe the probe; add the new seams to the key-locations table (`probeEffort`/`absorbProbe`, `confirmedEffort`, the history filter, `IsModelReply`). In `deferred.md`, mark "a session's effort is readable after all" **built** with the mechanism. In `decisions.md`, add a short ruling: the daemon may issue an invisible probe; the reply is suppressed at `fanOut` and filtered from history; the parse lives in the airlock.

- [ ] **Step 3: Run the doc-derived guards.** `go test ./internal/ui/ -run 'TestCLAUDEmd' -count=1` and fix any derived numbers with the correction (never edit the number by hand).

- [ ] **Step 4: `make ci`** from the worktree; read the exit code. Fix anything red. (Docs-only sub-changes that other worktrees need — `decisions.md` — may also be pushed to `main` per repo convention.)

- [ ] **Step 5: Commit.** `git add -A && git commit -m "docs: record the effort probe and status-bar restructure"`

---

## Self-Review

**Spec coverage:** Half 1a → Tasks 8-9; 1b → Task 10; 1c → Task 7. Half 2a → Task 3; 2b → Tasks 3+5; 2c → Tasks 1+4; 2d → Task 2; 2e → Task 6. Docs/guards → Task 11. Banner-untouched invariant → Task 10 test. No gaps.

**Placeholders:** each code step carries real code or an exact directive with `file:line`. The three "confirm the exact X" notes (init event kind; `Fleet.manager()`/`modeOf` signatures; `chromeHeight` `+1`) are reads to perform at execution time, not deferred design.

**Type consistency:** `IsModelReply`/`EffortFromModelReply` (Task 1) are used verbatim in Tasks 4 and 6. `confirmedEffort`/`setConfirmedEffort` (Task 2) used in Task 4. `probing`/`setProbing`/`probeEffort`/`absorbProbe` consistent across Tasks 3-5. `WithBar` (Task 8) used in Tasks 9-10. `noteEffort` returning `bool` (Task 3) drives the re-probe (Task 5).

## Post-implementation

Two reviews before the PR (owner's rule): a code review of the diff, and an adversarial pass against the strongest claims (the suppression is race-safe; the history filter can't drop a real turn; the probe never counts as an operator turn). Then open the PR into `main` with the `make ci` exit code in the body.
