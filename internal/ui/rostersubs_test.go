package ui

// Subagents in the right sidebar: the founding message's "list of all your sub
// agents ... so you can toggle into them to manage them directly there".

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// subsFrom is a lookup over a literal, so a table test says what each agent has
// running without folding a stream to get there.
func subsFrom(m map[string][]Task) subsOf {
	return func(id string) []Task { return m[id] }
}

func running(dispatch, agentType string, tokens int) Task {
	return Task{
		ID: dispatch, Dispatch: dispatch, Kind: core.TaskAgent,
		Status: core.TaskRunning, Type: agentType, Tokens: tokens,
	}
}

func alexWith(subs ...Task) ([]Agent, subsOf) {
	return []Agent{{ID: "s1", Name: "alex", State: rpc.StateWorking}},
		subsFrom(map[string][]Task{"s1": subs})
}

// The row the whole change is for: alex dispatches a code review, and the
// sidebar says so under alex.
func TestARunningDispatchDrawsARowUnderItsAgent(t *testing.T) {
	agents, subs := alexWith(running("toolu_1", "code-reviewer", 1100))
	out := stripANSI(Roster{}.View(agents, subs, rosterWidth, 10))

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	agentAt, subAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "alex") {
			agentAt = i
		}
		if strings.Contains(l, "code-reviewer") {
			subAt = i
		}
	}
	if agentAt < 0 {
		t.Fatalf("the agent's own row is gone:\n%s", out)
	}
	if subAt < 0 {
		t.Fatalf("the subagent is not listed:\n%s", out)
	}
	if subAt <= agentAt {
		t.Errorf("the subagent is drawn above its agent, at %d against %d:\n%s", subAt, agentAt, out)
	}
	if !strings.HasPrefix(lines[subAt], strings.Repeat(" ", toolIndent)) {
		t.Errorf("the subagent row is not indented under its agent: %q", lines[subAt])
	}
}

// Running only. A session keeps every dispatch it ever made, so a sidebar
// drawing all of them grows without bound beside thirty agents - and the
// question this column answers is what is costing you *now*. The pane keeps the
// finished ones, where they can be opened and read.
//
// Driven through Fleet.RunningTasks rather than a literal, because that is
// where the rule lives and the roster draws what it is handed. A second filter
// in the sidebar would be a second opinion about one question, which is what
// View's own header refuses about ranking.
func TestAFinishedDispatchLeavesTheSidebar(t *testing.T) {
	agents := []Agent{{ID: "s1", Name: "alex", State: rpc.StateWorking}}
	f := NewFleet()
	f, _ = f.Observe(started("a1", "toolu_1", "Auditing the diff", "code-reviewer", core.TaskAgent), "s1")
	f, _ = f.Observe(started("a2", "toolu_2", "Searching", "general-purpose", core.TaskAgent), "s1")

	live := stripANSI(Roster{}.View(agents, f.RunningTasks, rosterWidth, 10))
	if !strings.Contains(live, "code-reviewer") {
		t.Fatalf("the running dispatch is not listed, so this test measures nothing:\n%s", live)
	}

	f, _ = f.Observe(ended("a1", core.TaskDone), "s1")
	after := stripANSI(Roster{}.View(agents, f.RunningTasks, rosterWidth, 10))
	if strings.Contains(after, "code-reviewer") {
		t.Errorf("a finished dispatch is still taking a row:\n%s", after)
	}
	if !strings.Contains(after, "general-purpose") {
		t.Errorf("the one still running left with it:\n%s", after)
	}
}

// The name is the subagent_type, which is what an operator asked for when they
// dispatched it. A dispatch that names no type falls back rather than drawing a
// row with nothing on it - every local_agent in the corpus is general-purpose,
// so an unrecorded type is the case this has to survive.
func TestTheSubagentRowNamesItsType(t *testing.T) {
	for _, tc := range []struct {
		name string
		task Task
		want string
	}{
		{"a type", running("toolu_1", "code-reviewer", 0), "code-reviewer"},
		{"no type, a label", Task{
			ID: "toolu_2", Dispatch: "toolu_2", Kind: core.TaskAgent,
			Status: core.TaskRunning, Label: "Auditing the diff",
		}, "Auditing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agents, subs := alexWith(tc.task)
			out := stripANSI(Roster{}.View(agents, subs, rosterWidth, 10))
			if !strings.Contains(out, tc.want) {
				t.Errorf("the row does not name the dispatch (%q):\n%s", tc.want, out)
			}
		})
	}
}

// The count is budgeted last and dropped **whole**, which is the opposite rule
// to the unread badge and deliberately so: the type is what the row is for and
// the figure is something to know. A partial `↓ 1` is a different number, and a
// wrong figure on screen is worse than no figure.
func TestTheTokenCountIsDroppedWholeRatherThanCuttingTheType(t *testing.T) {
	// Thirteen characters, which with the indent and the glyph leaves exactly
	// enough for the count at rosterWidth and not at anything narrower.
	agents, subs := alexWith(running("toolu_1", "code-reviewer", 1100))

	wide := stripANSI(Roster{}.View(agents, subs, rosterWidth, 10))
	if !strings.Contains(wide, "1.1k") {
		t.Fatalf("the count is missing at full width, so this test measures nothing:\n%s", wide)
	}

	narrow := stripANSI(Roster{}.View(agents, subs, rosterWidth-4, 10))
	if strings.Contains(narrow, "1.1k") {
		t.Errorf("the count survived into a column too narrow for it:\n%s", narrow)
	}
	if !strings.Contains(narrow, "code-reviewer") {
		t.Errorf("the type was cut to make room for a figure that then did not fit:\n%s", narrow)
	}
	// Never a fragment of one.
	for _, frag := range []string{"1.", "1k", "↓"} {
		if strings.Contains(narrow, frag) {
			t.Errorf("a piece of the count survived (%q), which reads as a different number:\n%s", frag, narrow)
		}
	}
}

// The load-bearing claim of this whole file: lipgloss joins columns on their
// widest line, so one row too wide shoves the room and every pane sideways.
func TestASubagentRowNeverOutgrowsTheColumn(t *testing.T) {
	agents, subs := alexWith(
		running("toolu_1", "code-reviewer", 1100),
		running("toolu_2", "general-purpose", 1_250_000),
		running("toolu_3", "an-absurdly-long-subagent-type-nobody-would-write", 12),
	)
	for _, w := range []int{4, 8, 12, 16, rosterWidth, 40} {
		out := stripANSI(Roster{}.View(agents, subs, w, 20))
		for _, line := range strings.Split(out, "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width %d: a row measured %d columns: %q", w, got, line)
			}
		}
	}
}

// rowsFor is the height oracle the window is chosen with. A subagent row it
// does not count is a column sized for a sidebar somebody else is drawing.
func TestTheHeightOracleCountsSubagentRows(t *testing.T) {
	agents, subs := alexWith(running("toolu_1", "code-reviewer", 0), running("toolu_2", "general-purpose", 0))

	out := stripANSI(Roster{}.View(agents, subs, rosterWidth, 20))
	drawn := len(strings.Split(strings.TrimRight(out, "\n"), "\n"))
	// View pads to the height it was given, so the promise is measured against
	// the oracle rather than against the padded block.
	if got := rowsFor(agents[0], subs(agents[0].ID)); got != 3 {
		t.Errorf("rowsFor = %d, want 3: the agent's row and its two subagents", got)
	}
	if drawn < 3 {
		t.Errorf("drew %d lines, want at least the 3 rowsFor promises:\n%s", drawn, out)
	}
}

// ↑↓ walk into the subagents and back out, because the founding message asked
// to "toggle into them" and a row nothing can land on is a row that is only
// decoration.
func TestTheCursorWalksIntoASubagentRowAndBackOut(t *testing.T) {
	agents := []Agent{
		{ID: "s1", Name: "alex", State: rpc.StateWorking},
		{ID: "s2", Name: "sydney", State: rpc.StateIdle},
	}
	subs := subsFrom(map[string][]Task{"s1": {running("toolu_1", "code-reviewer", 0)}})

	r := Roster{Selected: "s1"}
	r = r.Move(agents, subs, 1)
	if r.Selected != "s1" || r.SelectedTask != "toolu_1" {
		t.Fatalf("one step down landed on %+v, want alex's subagent", r)
	}
	r = r.Move(agents, subs, 1)
	if r.Selected != "s2" || r.SelectedTask != "" {
		t.Fatalf("the next step landed on %+v, want sydney with no dispatch", r)
	}
	r = r.Move(agents, subs, -1)
	if r.Selected != "s1" || r.SelectedTask != "toolu_1" {
		t.Fatalf("stepping back landed on %+v, want the subagent again", r)
	}
	r = r.Move(agents, subs, -1)
	if r.Selected != "s1" || r.SelectedTask != "" {
		t.Fatalf("stepping back again landed on %+v, want alex's own row", r)
	}
}

// A click lands on the row under the pointer, subagent rows included - the
// mouse reaches every row the keys do.
func TestAClickLandsOnTheSubagentRowUnderThePointer(t *testing.T) {
	agents, subs := alexWith(running("toolu_1", "code-reviewer", 0))

	agent, dispatch, ok := Roster{}.At(agents, subs, rosterWidth, 10, 1)
	if !ok {
		t.Fatal("the row under the pointer resolved to nothing")
	}
	if agent.ID != "s1" {
		t.Errorf("the click resolved to %q, want the parent agent", agent.ID)
	}
	if dispatch != "toolu_1" {
		t.Errorf("the click resolved to dispatch %q, want toolu_1", dispatch)
	}
}

// The agent's own row still resolves to no dispatch, or every click would open
// a subagent.
func TestAClickOnTheAgentsOwnRowNamesNoDispatch(t *testing.T) {
	agents, subs := alexWith(running("toolu_1", "code-reviewer", 0))

	agent, dispatch, ok := Roster{}.At(agents, subs, rosterWidth, 10, 0)
	if !ok || agent.ID != "s1" {
		t.Fatalf("the agent's row resolved to %+v, %v", agent, ok)
	}
	if dispatch != "" {
		t.Errorf("the agent's own row named dispatch %q", dispatch)
	}
}

// An agent with no dispatches walks exactly as it did, which is what keeps
// every existing roster behaviour true for the fleet that is not dispatching -
// and that is most of it, most of the time.
func TestAFleetWithNoDispatchesWalksAsItAlwaysDid(t *testing.T) {
	agents := []Agent{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "s2", Name: "sydney", State: rpc.StateIdle},
	}
	none := subsFrom(nil)

	r := Roster{Selected: "s1"}.Move(agents, none, 1)
	if r.Selected != "s2" || r.SelectedTask != "" {
		t.Errorf("one step down landed on %+v, want sydney", r)
	}
}

// ⌃D on a subagent row opens the conversation it belongs to and puts the pane
// inside that subagent's transcript - the founding message's "toggle into them
// to manage them directly there", reaching the surface ↵ already opens in the
// pane rather than a second one.
func TestOpeningFromTheSidebarWithTheCursorOnASubagentViewsThatDispatch(t *testing.T) {
	// The cursor already sits on alex's own row - WithOpenDM puts it there - so
	// one step down is the first dispatch under it.
	a := dispatching(t)
	a = a.pickAgent(1)

	if a.roster.SelectedTask == "" {
		t.Fatalf("the cursor did not reach a subagent row: %+v", a.roster)
	}
	want := a.roster.SelectedTask

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if got := viewedBy(t, a, "s1"); got != want {
		t.Errorf("the pane is viewing %q, want the subagent the cursor was on (%q)", got, want)
	}
}

// And on the agent's own row it opens the conversation itself, which is what
// ⌃D has always done. A cursor that had been on a subagent and walked back must
// not leave the pane inside one.
// The pane must actually be inside a subagent before the walk back proves
// anything. The first version of this test only moved the cursor, so d.viewing
// was "" for its whole run and the closing assertion held whether or not the
// way back did anything at all - which is exactly how the way back came to be
// missing.
func TestOpeningFromTheAgentsOwnRowViewsTheConversation(t *testing.T) {
	a := dispatching(t)
	a = a.pickAgent(1)
	if a.roster.SelectedTask == "" {
		t.Fatalf("the cursor did not reach a subagent row, so the walk back proves nothing: %+v", a.roster)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if viewedBy(t, a, "s1") == "" {
		t.Fatal("the pane never entered the subagent, so there is nothing to come back from")
	}

	a = a.pickAgent(-1) // back onto alex's own row
	if a.roster.SelectedTask != "" {
		t.Fatalf("the cursor is still on a subagent: %+v", a.roster)
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	if got := viewedBy(t, a, "s1"); got != "" {
		t.Errorf("the pane is viewing %q, want the conversation itself - there is no way back", got)
	}
}

// A click on another agent's row leaves a cursor that names one agent, not two.
//
// show() sets Roster.Selected on every open and never touches SelectedTask, so
// a click that resolves to no dispatch used to leave the previous agent's
// dispatch attached to the newly clicked agent - a pair Move and walkable can
// never produce. Two things go wrong at once: the clicked row draws with no
// cursor (headStyle wants SelectedTask empty) while the old agent's subagent row
// keeps it, and a later ⌃D asks the new agent's conversation to show a dispatch
// that belongs to somebody else, which is an empty transcript.
func TestClickingAnAgentsOwnRowLeavesNoOtherAgentsDispatchAttached(t *testing.T) {
	a := dispatching(t)
	a = a.pickAgent(1)
	if a.roster.SelectedTask == "" {
		t.Fatalf("the cursor did not reach a subagent row: %+v", a.roster)
	}
	stale := a.roster.SelectedTask

	// The far right column is the roster, and row 0 is the agent's own row.
	// pickAgent has already opened the sidebar.
	a = a.press(a.layout.Width-1, 0)

	if a.roster.SelectedTask == stale {
		t.Errorf("the click kept the previous agent's dispatch: %+v", a.roster)
	}
	if a.roster.SelectedTask != "" {
		t.Errorf("a click on an agent's own row named dispatch %q", a.roster.SelectedTask)
	}
}

// Opening a *different* agent than the cursor is on drops the subagent it named.
//
// The mouse pairs Selected and SelectedTask by hand (mouse.go). Every other open
// path goes through show(), which sets Selected and never touches SelectedTask -
// so opening sydney while the cursor sits on one of alex's subagents used to
// leave {Selected: sydney, SelectedTask: a dispatch alex owns}. That is the pair
// Move and walkable can never produce, and it does two things at once: the
// roster cursor vanishes (headStyle wants SelectedTask empty), and the next ⌃D
// asks sydney's conversation to view a dispatch it does not own - an empty
// transcript. starts.go opens a fork's DM exactly this way, and ⇥ to an off-grid
// conversation is the other reachable trigger.
func TestOpeningAnotherAgentDropsTheSubagentTheCursorWasOn(t *testing.T) {
	a := dispatching(t)
	a = a.pickAgent(1)
	if a.roster.SelectedTask == "" {
		t.Fatalf("the cursor did not reach a subagent row: %+v", a.roster)
	}

	// The arrival path (starts.go) and ⇥ (panes.go) both open this way, with no
	// cursor move to re-pair the fields.
	a = a.openDMWith("s2", "sydney")

	if a.roster.SelectedTask != "" {
		t.Errorf("opening sydney kept alex's dispatch %q on the cursor", a.roster.SelectedTask)
	}
	if got := viewedBy(t, a.viewingPicked("s2"), "s2"); got != "" {
		t.Errorf("a ⌃D on sydney now views %q, a dispatch alex owns and sydney does not", got)
	}
}

// The keys that act on a *session* keep acting on the parent while the cursor
// is on one of its subagents.
//
// This is what Selected staying an agent id buys, and it is the safety half of
// the change rather than a nicety: ⌃C parks, ⎋ interrupts and ↵ opens whatever
// pickedAgent answers, and a subagent has no process, no id the daemon knows
// and no lifecycle to park. A cursor that meant "a subagent" to those three
// would be three keys asking a question with no answer - on the surface an
// operator stops a runaway agent from.
func TestTheSessionKeysStillTargetTheParentFromASubagentRow(t *testing.T) {
	a := dispatching(t)
	a = a.pickAgent(1)

	if a.roster.SelectedTask == "" {
		t.Fatalf("the cursor did not reach a subagent row: %+v", a.roster)
	}
	agent, ok := a.pickedAgent()
	if !ok {
		t.Fatal("no agent is picked while the cursor is on a subagent row")
	}
	if agent.ID != "s1" {
		t.Errorf("pickedAgent = %q, want the parent session s1", agent.ID)
	}
}

// A dispatch description is a string a model wrote, and a row is one row.
//
// lipgloss measures and clips per line, so a newline draws *two* physical rows
// out of a value rowsFor counted as one: the column runs past the height it was
// given, At maps every click below it to the wrong agent, and the rows pushed
// off the bottom go without the `+N more` that exists to say so. An ESC is the
// same defect measured the other way - an ANSI-aware width reads it as zero
// columns, so the row measures short and draws long.
func TestARowIsOneRowWhateverTheDispatchIsCalled(t *testing.T) {
	hostile := []struct{ name, what string }{
		{"a newline", "code\nreviewer"},
		{"a carriage return", "code\rreviewer"},
		{"an escape", "code\x1b[31mreviewer"},
		{"a tab", "code\treviewer"},
	}
	for _, tc := range hostile {
		t.Run(tc.name, func(t *testing.T) {
			agents, subs := alexWith(running("toolu_1", tc.what, 0))

			if got := strings.Count(subagentRow(subsFor(subs, "s1")[0], rosterWidth), "\n"); got != 0 {
				t.Errorf("the row carries %d newlines, so it draws more rows than rowsFor counted", got+1)
			}
			out := Roster{}.View(agents, subs, rosterWidth, 6)
			if got := len(strings.Split(out, "\n")); got != 6 {
				t.Errorf("the column drew %d rows, want the 6 it was given:\n%q", got, out)
			}
			if strings.ContainsRune(out, '\x1b'+0) && strings.Contains(out, "[31m") {
				t.Errorf("an escape sequence from a dispatch name reached the frame:\n%q", out)
			}
		})
	}
}

// The pane's own list has the same exposure, from the same field, and a row too
// tall there is the alt-screen scroll CLAUDE.md names: the rows are chrome, and
// taskRowCount budgets them as one line each.
func TestThePaneRowIsAlsoOneRowWhateverTheDispatchIsCalled(t *testing.T) {
	d := listing(started("c1", "toolu_3", "count\nlines", "gen\neral", core.TaskAgent))

	out := d.taskView(90)
	if got, want := len(strings.Split(out, "\n")), d.taskRowCount(); got != want {
		t.Errorf("taskView drew %d rows and taskRowCount budgeted %d - the pane is taller than it was given", got, want)
	}
}

// A background shell is not a subagent and does not get a row.
//
// Two reasons, and the second is the one that matters. It is not what this
// surface says it is listing - a shell carries no subagent_type, so
// subagentName falls through to its description and it reads exactly like a
// subagent. And it forwards no frames at all, so a row offering to open it
// opens an empty pane: Task.Openable is the one place that decides which rows
// have a conversation behind them, and taskrows.go's own header says a row
// without one must never offer one.
func TestABackgroundShellDoesNotGetASubagentRow(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(started("a1", "toolu_1", "Auditing the diff", "code-reviewer", core.TaskAgent), "s1")
	f, _ = f.Observe(started("b1", "toolu_2", "waiting for the sentinel", "", core.TaskShell), "s1")

	for _, row := range f.RunningTasks("s1") {
		if row.Kind == core.TaskShell {
			t.Errorf("a background shell is listed as a subagent: %+v", row)
		}
		if !row.Openable() {
			t.Errorf("a row with no conversation behind it is offered as one: %+v", row)
		}
	}
	if got := len(f.RunningTasks("s1")); got != 1 {
		t.Errorf("got %d rows, want the one real subagent", got)
	}

	agents := []Agent{{ID: "s1", Name: "alex", State: rpc.StateWorking}}
	if out := stripANSI(Roster{}.View(agents, f.RunningTasks, rosterWidth, 10)); strings.Contains(out, "sentinel") {
		t.Errorf("the shell is drawn in the sidebar:\n%s", out)
	}
}

// One cursor, one accented row. Selected keeps naming the agent while the
// cursor is on one of its subagents - which is what ⌃C and ⎋ need - so the
// agent's own row must stop wearing the accent, or the sidebar shows two
// selections and neither says which one a key is about.
func TestOnlyOneRowWearsTheCursorWhenItIsOnASubagent(t *testing.T) {
	// Without a colour profile every style renders the bare string, and the
	// comparisons below would hold whatever headStyle returned.
	forceColour(t)
	agent := Agent{ID: "s1", Name: "alex", State: rpc.StateWorking}
	on := Roster{Selected: "s1", SelectedTask: "toolu_1"}

	accent := AccentStyle.Render("x")
	if got := on.headStyle(agent).Render("x"); got == accent {
		t.Error("the agent's row is accented while the cursor is on its subagent: two rows look selected")
	}
	if got := on.subStyle("s1", running("toolu_1", "code-reviewer", 0)).Render("x"); got != accent {
		t.Error("the subagent row under the cursor is not accented, so the cursor is invisible")
	}

	// And on the agent's own row it is still the agent that is accented.
	own := Roster{Selected: "s1"}
	if got := own.headStyle(agent).Render("x"); got != accent {
		t.Error("the agent's own row lost the cursor")
	}
}

// A dispatch that finishes under the cursor hands the cursor back to its agent,
// not to the top of the sidebar.
//
// walkable is rebuilt from what is running, so the composite stop the cursor
// named is simply gone the moment the subagent ends - and an unfound stop used
// to reset to index 0, which at thirty agents throws somebody from the agent
// they were working with to whichever one ranks first.
func TestADispatchFinishingUnderTheCursorLeavesItOnItsAgent(t *testing.T) {
	agents := []Agent{
		{ID: "s0", Name: "first", State: rpc.StateIdle},
		{ID: "s1", Name: "alex", State: rpc.StateWorking},
	}
	// The dispatch the cursor was on has ended, so nothing answers for it.
	gone := subsFrom(map[string][]Task{"s1": nil})
	stale := Roster{Selected: "s1", SelectedTask: "toolu_1"}

	if got := stale.Move(agents, gone, 1); got.Selected != "s1" && got.Selected != "s0" {
		t.Fatalf("the cursor went to %+v, which is neither its agent nor a neighbour", got)
	}
	if got := stale.Move(agents, gone, 0); got.Selected != "s1" || got.SelectedTask != "" {
		t.Errorf("standing still landed on %+v, want alex's own row", got)
	}
	if got := stale.Move(agents, gone, 1); got.Selected != "s0" {
		t.Errorf("one step down landed on %+v, want the row after alex's - not a reset to the top", got)
	}
}

// A conversation that has dispatched something must not re-size on every draw.
//
// DM.chrome memoizes what chromeHeight returned when the transcript was last
// sized, and View re-sizes when the two disagree - which is how a heartbeat row
// appearing mid-turn is caught. chromeHeight counts the dispatch rows too, so a
// task list the *stored* DM does not carry leaves that memo permanently wrong:
// View re-sizes, and re-wraps the whole transcript, on every frame for the life
// of any conversation that has ever dispatched anything. A per-agent cost on
// the draw path is what the first non-negotiable is about.
func TestAConversationWithDispatchesDoesNotReSizeOnEveryDraw(t *testing.T) {
	a := dispatching(t)

	d := a.dmFor("s1")
	if d.taskRowCount() == 0 {
		t.Fatal("no dispatch rows, so this test is measuring nothing")
	}
	if got, want := d.chromeHeight(), d.chrome; got != want {
		t.Errorf("chromeHeight = %d but the sized chrome is %d, so View re-sizes on every frame", got, want)
	}
}
