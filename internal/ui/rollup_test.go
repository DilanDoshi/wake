package ui

// The tool-run fold: a message's tool calls draw as one dimmed rollup line by
// default, and ⌃E or a click on the line opens them.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// readCall is a Read invocation, the second tool a rollup counts beside Bash.
func readCall(id, file string) core.Event {
	return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		ID: id, Name: "Read", Display: file, Receipt: "Read %d lines",
	}}
}

// mcpCall is an MCP invocation, whose category is its server rather than its
// tool - `mcp__linear-server__get_issue` counts as one linear-server.
func mcpCall(id, name string) core.Event {
	return core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{ID: id, Name: name}}
}

// prose is an ordinary assistant message, which breaks a run.
func prose(text string) core.Event {
	return core.Event{Kind: core.KindAssistantText, Text: text}
}

// --- the aggregation, pure -------------------------------------------------

func TestToolCategoryGroupsMcpByServerAndLowercasesTheRest(t *testing.T) {
	cases := map[string]string{
		"Bash":                            "bash",
		"Read":                            "read",
		"mcp__linear-server__get_issue":   "linear-server",
		"mcp__linear-server__list_issues": "linear-server",
		"TodoWrite":                       "todowrite",
	}
	for name, want := range cases {
		if got := toolCategory(name); got != want {
			t.Errorf("toolCategory(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestRollupSummaryCountsUsesByCategoryLargestFirst(t *testing.T) {
	run := []core.Event{
		bashCall("a"), result("a", "", false),
		mcpCall("b", "mcp__linear-server__get_issue"),
		bashCall("c"),
		readCall("d", "x.go"),
		mcpCall("e", "mcp__linear-server__list_issues"),
		mcpCall("f", "mcp__linear-server__list_comments"),
	}
	got := rollupSummary(tallyOf(run))
	// 6 uses: 3 linear-server, 2 bash, 1 read - largest category first.
	if want := "6 tool uses · 3 linear-server · 2 bash · 1 read"; got != want {
		t.Errorf("rollupSummary = %q, want %q", got, want)
	}
}

func TestRollupSummarySaysOneToolUseInTheSingular(t *testing.T) {
	if got := rollupSummary(tallyOf([]core.Event{bashCall("a")})); got != "1 tool use · 1 bash" {
		t.Errorf("rollupSummary = %q, want the singular form", got)
	}
}

// Results carry no use to count, so a run of only results has nothing to fold
// and summarises to nothing.
func TestRollupSummaryIsEmptyForAResultsOnlyRun(t *testing.T) {
	if got := rollupSummary(tallyOf([]core.Event{result("a", "body", false)})); got != "" {
		t.Errorf("rollupSummary = %q, want empty for a run with no uses", got)
	}
}

// The tally kept incrementally as a run grows formats the same line the one-pass
// count over the run's events does - the O(1) live path and the re-wrap path
// agreeing on the summary, not just on where the run breaks.
func TestTheIncrementalTallyMatchesAOnePassCount(t *testing.T) {
	run := []core.Event{bashCall("a"), bashCall("b"), readCall("c", "x.go"), mcpCall("d", "mcp__linear-server__get")}
	var live rollupTally
	for _, ev := range run {
		if isToolUse(ev) {
			live = countUse(live, ev.Tool.Name)
		}
	}
	if got, want := rollupSummary(live), rollupSummary(tallyOf(run)); got != want {
		t.Errorf("incremental tally formatted %q, one-pass count %q", got, want)
	}
}

// --- the fold in a conversation --------------------------------------------

// run appends a two-call run under a line of prose, the shape a folded message
// takes: prose, then its tools.
func withRun(d DM) DM {
	return d.Append(prose("working through the ticket")).
		Append(bashCall("t1")).
		Append(result("t1", "total 8", false)).
		Append(readCall("t2", "auth.go")).
		Append(result("t2", "1\talpha\n2\tbravo", false))
}

// The gap this closes: a message's tool calls were every ⏺ and ⎿ they made,
// which is the wall of detail the screenshots show. Folded, they are one line.
func TestARunOfToolCallsFoldsToOneRollupLineByDefault(t *testing.T) {
	d := withRun(NewDM("s1", "alex").SetSize(80, 30))

	assertShows(t, d, 80, 30, "2 tool uses · 1 bash · 1 read")
	assertShows(t, d, 80, 30, "working through the ticket") // the prose stays
	assertShows(t, d, 80, 30, "(⌃E to expand)")
	// None of the per-call detail is drawn.
	assertHides(t, d, 80, 30, "⏺")
	assertHides(t, d, 80, 30, "Listing files")
	assertHides(t, d, 80, 30, "$ ls -la")
}

// ⌃E opens the fold: every call draws as its own block again.
func TestExpandingShowsEveryCallInARun(t *testing.T) {
	d := withRun(NewDM("s1", "alex").SetSize(80, 30)).toggleExpanded()

	assertShows(t, d, 80, 30, "⏺ Listing files")
	assertShows(t, d, 80, 30, "auth.go")
	assertHides(t, d, 80, 30, "tool uses ·") // the rollup is gone
}

// And back, because a fold that only opens is a one-way door.
func TestExpandingARunTogglesBothWays(t *testing.T) {
	d := withRun(NewDM("s1", "alex").SetSize(80, 30)).toggleExpanded().toggleExpanded()

	assertShows(t, d, 80, 30, "2 tool uses · 1 bash · 1 read")
	assertHides(t, d, 80, 30, "⏺ Listing files")
}

// A click on one rollup opens that run alone; a second run in the same
// conversation stays folded. This is the per-turn expand the screenshots ask
// for, against the whole-conversation ⌃E.
func TestClickingARollupOpensJustThatRun(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 40).
		Append(prose("first message")).
		Append(bashCall("a1")).
		Append(result("a1", "one", false)).
		Append(prose("second message")).
		Append(bashCall("b1")).
		Append(result("b1", "two", false))

	// Two runs, each its own rollup - one per message.
	if got := strings.Count(visible(d, 80, 40), "tool use"); got != 2 {
		t.Fatalf("want two rollups, one per message:\n%s", visible(d, 80, 40))
	}

	line := runLine(t, d, "a1")
	opened, hit := d.openRun(line)
	if !hit {
		t.Fatalf("line %d holds no rollup", line)
	}

	// The first run is open (its ⏺ is back); the second is still folded.
	out := visible(opened, 80, 40)
	if !strings.Contains(out, "⏺") {
		t.Errorf("clicking the rollup did not open its run:\n%s", out)
	}
	if got := strings.Count(out, "tool use"); got != 1 {
		t.Errorf("want the other run still folded, got %d rollups:\n%s", got, out)
	}
}

// A click opens a run and leaves no rollup line behind to re-fold it, so ⌃E is
// the way back: show-everything then hide-everything returns the clicked run to
// its fold along with the rest.
func TestCollapsingEverythingFoldsAClickOpenedRun(t *testing.T) {
	d := withRun(NewDM("s1", "alex").SetSize(80, 40))
	opened, _ := d.openRun(runLine(t, d, "t1"))
	assertShows(t, opened, 80, 40, "⏺ Listing files")

	reset := opened.toggleExpanded().toggleExpanded()

	assertHides(t, reset, 80, 40, "⏺ Listing files")
	assertShows(t, reset, 80, 40, "2 tool uses")
}

// Prose between two batches of tools breaks the run, so each message's tools
// fold on their own rather than merging into one figure.
func TestProseBreaksARunSoEachMessageFoldsSeparately(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 40).
		Append(prose("one")).
		Append(bashCall("a1")).
		Append(bashCall("a2")).
		Append(prose("two")).
		Append(bashCall("b1"))

	out := visible(d, 80, 40)
	if !strings.Contains(out, "2 tool uses · 2 bash") {
		t.Errorf("the first message's two calls did not fold together:\n%s", out)
	}
	if !strings.Contains(out, "1 tool use · 1 bash") {
		t.Errorf("the second message's call did not fold on its own:\n%s", out)
	}
}

// A result whose use folded into an earlier run has no run to join, so it draws
// as itself rather than starting a rollup that would read "0 tool uses".
func TestAResultOrphanedFromItsRunDrawsAsItself(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).
		Append(prose("narration")).
		Append(result("orphan", "left behind\nsecond line", false))

	out := visible(d, 80, 30)
	if strings.Contains(out, "tool use") {
		t.Errorf("a lone result was folded into a rollup:\n%s", out)
	}
	if !strings.Contains(out, "left behind") {
		t.Errorf("a lone result was not drawn at all:\n%s", out)
	}
}

// The count grows in place as a run streams, rather than each call taking a
// line: the whole point of folding a run of thirty tools into one row.
func TestARollupGrowsAsItsRunStreams(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).Append(prose("go")).Append(bashCall("t1"))
	assertShows(t, d, 80, 30, "1 tool use · 1 bash")

	d = d.Append(result("t1", "", false)).Append(readCall("t2", "x.go"))
	assertShows(t, d, 80, 30, "2 tool uses · 1 bash · 1 read")
	// Still one rollup line, not a line per call.
	if got := strings.Count(visible(d, 80, 30), "tool use"); got != 1 {
		t.Errorf("a growing run drew more than one rollup:\n%s", visible(d, 80, 30))
	}
}

// A click on a rollup line, arriving through the App the way a real one does,
// opens the run - clickedTool tries a run before a result, and the two are never
// on the same line. This is the wiring the DM-level tests above cannot see.
func TestClickingARollupOpensTheRunThroughTheApp(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney")
	a.roster.Selected = "s1"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if a.focus != "s1" {
		t.Fatalf("focus is %q, want the conversation under test", a.focus)
	}
	frame := func(ev core.Event) rpc.Frame {
		ev.SessionID = "s1"
		return rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev}
	}
	a = a.applyFrame(frame(prose("working")))
	a = a.applyFrame(frame(bashCall("t1")))
	a = a.applyFrame(frame(result("t1", "out", false)))

	line := a.dms["s1"].tr.runHead("t1")
	if line < 0 {
		t.Fatalf("the run was not folded to a rollup:\n%s", shown(a))
	}
	// A click is a press and release on one cell; endSelection routes an empty
	// selection to clickedTool, which opens the run under it.
	a.sel = selection{pane: "s1", anchor: point{line: line}, head: point{line: line}}
	a.selecting = true
	a, _ = a.endSelection()

	if !a.dms["s1"].runOpen["t1"] {
		t.Errorf("clicking the rollup did not open the run:\n%s", shown(a))
	}
	if !strings.Contains(shown(a), "⏺") {
		t.Errorf("the opened run shows no per-call detail:\n%s", shown(a))
	}
}

// runLine is the transcript row a folded run's rollup was drawn on, keyed by
// the run's first use.
func runLine(t *testing.T, d DM, key string) int {
	t.Helper()
	at := d.tr.runHead(key)
	if at < 0 {
		t.Fatalf("run %q was not drawn as a rollup:\n%s", key, visible(d, 80, 40))
	}
	return at
}

// --- the incremental fold and the re-wrap must never disagree ---------------

// The claim the whole design rests on: Append folds a run live by restyling one
// line, and renderAll re-derives the same fold on a re-wrap. If they disagreed,
// a resize would re-expand or double a folded run. A width change is the
// re-wrap; the run must come back byte-identical.
func TestAFoldedRunSurvivesAReWrapIdentically(t *testing.T) {
	d := withRun(NewDM("s1", "alex").SetSize(80, 30))
	before := visible(d, 80, 30)

	rewrapped := d.SetSize(64, 30).SetSize(80, 30)
	after := visible(rewrapped, 80, 30)

	if before != after {
		t.Errorf("a re-wrap changed a folded run:\n--- before ---\n%s\n--- after ---\n%s", before, after)
	}
	if got := strings.Count(after, "tool use"); got != 1 {
		t.Errorf("a re-wrap left %d rollups, want 1:\n%s", got, after)
	}
}

// And a run keeps growing correctly after a re-wrap: runKey survives a width
// change (it is an id, not a line), so the next call restyles the same summary
// rather than opening a second one.
func TestARunKeepsFoldingAfterAReWrap(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).
		Append(prose("go")).
		Append(bashCall("t1")).
		Append(result("t1", "", false)).
		SetSize(64, 30) // re-wrap mid-run

	d = d.Append(readCall("t2", "x.go")).Append(result("t2", "1\ty", false))

	if got := strings.Count(visible(d, 64, 30), "tool use"); got != 1 {
		t.Errorf("a call after a re-wrap opened a second rollup:\n%s", visible(d, 64, 30))
	}
	assertShows(t, d, 64, 30, "2 tool uses · 1 bash · 1 read")
}

// Before() prepends restored history, the one re-render that changes the events.
// A conversation read back off disk whose tail is a tool run must fold as one
// rollup, and a live call arriving after must join it rather than start a
// second - which is why Before recomputes runKey.
func TestARestoredRunFoldsAndKeepsGrowing(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).Before([]core.Event{
		prose("restored message"),
		bashCall("h1"),
		result("h1", "done", false),
	})
	assertShows(t, d, 80, 30, "1 tool use · 1 bash")

	d = d.Append(readCall("h2", "y.go"))
	if got := strings.Count(visible(d, 80, 30), "tool use"); got != 1 {
		t.Errorf("a live call after a restore opened a second rollup:\n%s", visible(d, 80, 30))
	}
	assertShows(t, d, 80, 30, "2 tool uses · 1 bash · 1 read")
}

// A last-read boundary landing mid-run breaks it: the run before the boundary
// folds on its own, and the tools after it start a fresh run below the rule -
// the same break Append and renderAll must agree on.
func TestALastReadBoundaryBreaksARun(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).
		Append(prose("go")).
		Append(bashCall("a1")).
		Append(result("a1", "", false)).
		Leave(). // the reader steps away here
		Append(bashCall("a2")).
		Append(result("a2", "", false))

	out := visible(d, 80, 30)
	if got := strings.Count(out, "tool use"); got != 2 {
		t.Errorf("a boundary did not break the run into two rollups (got %d):\n%s", got, out)
	}
	assertShows(t, d, 80, 30, "you left off here")

	// And it survives a re-wrap the same way - the boundary re-derives in place.
	rewrapped := d.SetSize(64, 30).SetSize(80, 30)
	if got := strings.Count(visible(rewrapped, 80, 30), "tool use"); got != 2 {
		t.Errorf("a re-wrap merged the two runs a boundary had split:\n%s", visible(rewrapped, 80, 30))
	}
}

// Codex's finding: an event that draws nothing between two tool uses must fold
// the same way live and restored, or a resize would re-split a run. A turn end
// is dropped by both the live path and Before, so the two uses fold into one
// rollup either way.
func TestAnInvisibleEventDoesNotSplitARunLiveOrRestored(t *testing.T) {
	events := []core.Event{
		prose("message"),
		bashCall("a"), result("a", "", false),
		{Kind: core.KindTurnEnd},
		readCall("b", "x.go"), result("b", "", false),
	}

	live := NewDM("s1", "alex").SetSize(80, 30)
	for _, ev := range events {
		live = live.Append(ev)
	}
	restored := NewDM("s1", "alex").SetSize(80, 30).Before(events)

	lv, rv := visible(live, 80, 30), visible(restored, 80, 30)
	if lv != rv {
		t.Errorf("live and restored disagree on an invisible event:\n--- live ---\n%s\n--- restored ---\n%s", lv, rv)
	}
	if got := strings.Count(rv, "tool use"); got != 1 {
		t.Errorf("an invisible event split the run into %d rollups, want 1:\n%s", got, rv)
	}
	if !strings.Contains(rv, "2 tool uses · 1 bash · 1 read") {
		t.Errorf("the two uses did not fold together:\n%s", rv)
	}
	// And it survives a re-wrap, the case the whole invariant is about.
	if got := strings.Count(visible(restored.SetSize(64, 30).SetSize(80, 30), 80, 30), "tool use"); got != 1 {
		t.Error("a re-wrap re-split a run that an invisible event had not")
	}
}

// Codex second pass: a boundary breaks the run before the fold is classified.
// Leaving mid-run, then an empty result of that run, used to be folded (its
// classification computed against the old runKey), so drawFold appended an empty
// clickable line the re-wrap path then dropped - a layout that shifted on
// resize. The empty result must draw nothing, live and re-wrapped alike.
func TestAnEmptyResultAfterLeavingMidRunAddsNoLine(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(80, 30).
		Append(prose("go")).
		Append(bashCall("a")).
		Append(result("a", "ok", false)).
		Leave().                       // the reader steps away, mid-run
		Append(result("a", "", false)) // an empty result of the same run

	before := visible(d, 80, 30)
	rewrapped := visible(d.SetSize(64, 30).SetSize(80, 30), 80, 30)
	if before != rewrapped {
		t.Errorf("a re-wrap changed the layout after a mid-run leave:\n--- before ---\n%s\n--- after ---\n%s", before, rewrapped)
	}
	// runTally must not be left populated while runKey is empty.
	if d.runKey == "" && len(d.runTally) != 0 {
		t.Errorf("runTally is %v while runKey is empty - a stale tally", d.runTally)
	}
}

// A TodoWrite's result carries no todos of its own, so without inheriting its
// use's exemption it would fold into a neighbouring run and draw nothing - its
// checklist on screen, its result silently gone. The result must draw. (A
// TaskCreate/TaskUpdate is the other exempt shape; it pins its board and draws
// no block - see checklistpin_test.go.)
func TestATodoWriteResultInheritsItsUsesExemption(t *testing.T) {
	todoUse := core.Event{Kind: core.KindToolUse, Tool: &core.ToolCall{
		ID: "todo1", Name: "TodoWrite", Todos: []core.Todo{{Text: "step one", Status: core.TodoActive}},
	}}
	d := NewDM("s1", "alex").SetSize(80, 30).
		Append(prose("go")).
		Append(todoUse).        // exempt: draws its checklist, not a rollup
		Append(bashCall("b1")). // a fresh run is now live
		Append(result("todo1", "Todos have been updated", false))

	out := visible(d, 80, 30)
	if !strings.Contains(out, "Todos have been updated") {
		t.Errorf("a TodoWrite result was absorbed into a run instead of drawn:\n%s", out)
	}
	if !strings.Contains(out, "step one") {
		t.Errorf("the TodoWrite checklist did not draw whole:\n%s", out)
	}
}
