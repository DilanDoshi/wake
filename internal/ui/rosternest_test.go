package ui

// A subagent's own subagents in the right sidebar: drawn under the subagent that
// dispatched them, not beside it (the owner's 2026-11-03 report, BUG-61).

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The recorded nest, subagent-nested.jsonl: a general-purpose subagent and the
// two Explore subagents it dispatched.
const (
	nestOuter  = "a87d4741805dbd4b7"
	nestFirst  = "aa6ffd9fbacd9f792"
	nestSecond = "aeec95ae93411f2a1"
)

// nestedEvents is the recording up to its first ending, where all three run.
func nestedEvents(t *testing.T) []core.Event {
	t.Helper()
	evs := decodeWorkflowFixture(t, "subagent-nested.jsonl")
	for i, ev := range evs {
		if ev.Task != nil && ev.Task.Phase == core.TaskEnded {
			return evs[:i]
		}
	}
	t.Fatal("the recording never ended a dispatch")
	return nil
}

func nestedFleet(t *testing.T) Fleet {
	t.Helper()
	f := NewFleet()
	for _, ev := range nestedEvents(t) {
		f, _ = f.Observe(ev, "s1")
	}
	return f
}

// under is started with the dispatching subagent's task id, as 2.1.314 sends it.
func under(parent, id, dispatch, agentType string) core.Event {
	ev := started(id, dispatch, "", agentType, core.TaskAgent)
	ev.Task.Parent = parent
	return ev
}

// subColumn is where a sidebar line's ⎿ sits, and -1 for a line without one.
func subColumn(line string) int {
	at := strings.Index(line, subGlyph)
	if at < 0 {
		return -1
	}
	return len([]rune(line[:at]))
}

// shape is each running row's id and depth, in the order the sidebar draws them.
func shape(rows []Task) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = strings.Repeat(">", r.Depth) + r.ID
	}
	return out
}

func TestASubagentsOwnSubagentsDrawUnderIt(t *testing.T) {
	f := nestedFleet(t)
	agents := []Agent{{ID: "s1", Name: "alex", State: rpc.StateWorking}}
	out := stripANSI(Roster{}.View(agents, f.RunningTasks, rosterWidth, 12))

	outer, inner := -1, []int{}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		switch {
		case strings.Contains(l, "general-purpose"):
			outer = i
		case strings.Contains(l, "Explore"):
			inner = append(inner, i)
		}
	}
	if outer < 0 || len(inner) != 2 {
		t.Fatalf("want the general-purpose row and two Explore rows:\n%s", out)
	}
	// A child's ⎿ sits where its parent's name begins.
	nameAt := subColumn(lines[outer]) + len([]rune(subGlyph+" "))
	for _, i := range inner {
		if i < outer {
			t.Errorf("an Explore row is drawn above the subagent that dispatched it:\n%s", out)
		}
		if subColumn(lines[i]) != nameAt {
			t.Errorf("an Explore row's ⎿ is at %d, want %d under the general-purpose name:\n%s", subColumn(lines[i]), nameAt, out)
		}
		if !strings.Contains(lines[i], tokenArrow) {
			t.Errorf("an Explore row lost its own figure: %q", lines[i])
		}
	}
}

// Start order interleaves two subagents' children; the sidebar keeps each under
// its own parent, in the order they started there.
func TestEachSubagentsChildrenStayUnderIt(t *testing.T) {
	f := NewFleet()
	for _, ev := range []core.Event{
		started("A", "toolu_A", "", "general-purpose", core.TaskAgent),
		started("B", "toolu_B", "", "general-purpose", core.TaskAgent),
		under("A", "a1", "toolu_a1", "Explore"),
		under("B", "b1", "toolu_b1", "Explore"),
		under("A", "a2", "toolu_a2", "Explore"),
		under("a1", "a1x", "toolu_a1x", "Explore"),
		progressed("a1", "toolu_a1", "Reading", "Read", 900, 0),
	} {
		f, _ = f.Observe(ev, "s1")
	}
	got := strings.Join(shape(f.RunningTasks("s1")), " ")
	if want := "A >a1 >>a1x >a2 B >b1"; got != want {
		t.Errorf("drawn as %q, want %q", got, want)
	}
}

// A child that outlives its parent moves up to the nearest ancestor still
// running, and to the agent's own level when none is.
func TestAChildWhoseParentFinishedMovesUpToTheNearestRunningAncestor(t *testing.T) {
	f := NewFleet()
	for _, ev := range []core.Event{
		started("A", "toolu_A", "", "general-purpose", core.TaskAgent),
		under("A", "a1", "toolu_a1", "general-purpose"),
		under("a1", "a1x", "toolu_a1x", "Explore"),
		ended("a1", core.TaskDone),
	} {
		f, _ = f.Observe(ev, "s1")
	}
	if got, want := strings.Join(shape(f.RunningTasks("s1")), " "), "A >a1x"; got != want {
		t.Errorf("with its parent finished, drawn as %q, want %q", got, want)
	}
	f, _ = f.Observe(ended("A", core.TaskDone), "s1")
	if got, want := strings.Join(shape(f.RunningTasks("s1")), " "), "a1x"; got != want {
		t.Errorf("with no ancestor running, drawn as %q, want %q", got, want)
	}
}

// Parent is the wire's word, so a row naming itself or a loop must still be
// listed, once, rather than hang the walk or vanish.
func TestARowWhoseParentsLoopIsStillListedOnce(t *testing.T) {
	f := NewFleet()
	for _, ev := range []core.Event{
		under("X", "Y", "toolu_Y", "Explore"),
		under("Y", "X", "toolu_X", "Explore"),
		under("Z", "Z", "toolu_Z", "Explore"),
	} {
		f, _ = f.Observe(ev, "s1")
	}
	rows := f.RunningTasks("s1")
	seen := map[string]int{}
	for _, r := range rows {
		seen[r.ID]++
	}
	if len(rows) != 3 || seen["X"] != 1 || seen["Y"] != 1 || seen["Z"] != 1 {
		t.Errorf("listed %v, want X, Y and Z once each", shape(rows))
	}
}

// The indent stops at maxSubDepth, the deepest level at which general-purpose -
// the type every recorded nest dispatches - is still drawn whole in the
// sidebar's 23 drawn columns (rosterWidth less the divider). A row deeper than
// that is drawn at the cap's indent, still in tree order under its parent.
func TestTheIndentStopsWhereGeneralPurposeStillFitsWhole(t *testing.T) {
	const name = "general-purpose"
	width := rosterWidth - dividerWidth
	at := func(depth int) Task {
		return Task{ID: "x", Dispatch: "toolu_x", Kind: core.TaskAgent, Status: core.TaskRunning, Type: name, Depth: depth}
	}
	if row := subagentRow(at(maxSubDepth), width); !strings.Contains(row, name) {
		t.Errorf("at the cap the name is cut: %q", row)
	}
	if deeper := toolIndent + subStep*(maxSubDepth+1) + lipgloss.Width(subGlyph+" "+name); deeper <= width {
		t.Errorf("one level deeper still fits %s whole (%d of %d columns): the cap is too shallow", name, deeper, width)
	}
	if got, cap := subColumn(subagentRow(at(maxSubDepth+3), width)), subColumn(subagentRow(at(maxSubDepth), width)); got != cap {
		t.Errorf("a row past the cap draws its ⎿ at %d, want the cap's %d", got, cap)
	}
}

// ⇧↓ walks the tree in the order it is drawn, onto each nested row.
func TestTheCursorWalksTheTreeInTheOrderItIsDrawn(t *testing.T) {
	f := NewFleet()
	for _, ev := range []core.Event{
		started("A", "toolu_A", "", "general-purpose", core.TaskAgent),
		started("B", "toolu_B", "", "general-purpose", core.TaskAgent),
		under("A", "a1", "toolu_a1", "Explore"),
	} {
		f, _ = f.Observe(ev, "s1")
	}
	agents := []Agent{{ID: "s1", Name: "alex", State: rpc.StateWorking}}
	r := Roster{Selected: "s1"}
	var walked []string
	for range 3 {
		r = r.Move(agents, f.RunningTasks, 1)
		walked = append(walked, r.SelectedTask)
	}
	if got, want := strings.Join(walked, " "), "toolu_A toolu_a1 toolu_B"; got != want {
		t.Errorf("the cursor walked %q, want %q", got, want)
	}
}

// crossStarted is two subagents and a child of the first that started after the
// second, so start order (aa, bb, ac) and tree order (aa, ac, bb) differ.
func crossStarted() []core.Event {
	return []core.Event{
		started("aa", "toolu_aa", "", "general-purpose", core.TaskAgent),
		started("bb", "toolu_bb", "", "general-purpose", core.TaskAgent),
		under("aa", "ac", "toolu_ac", "Explore"),
	}
}

// A click on a nested row lands on that row's dispatch: the second sub row is
// the child, not the second subagent started.
func TestAClickOnANestedRowLandsOnIt(t *testing.T) {
	f := NewFleet()
	for _, ev := range crossStarted() {
		f, _ = f.Observe(ev, "s1")
	}
	agents := []Agent{{ID: "s1", Name: "alex", State: rpc.StateWorking}}
	lines := strings.Split(stripANSI(Roster{}.View(agents, f.RunningTasks, rosterWidth, 12)), "\n")
	y := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "Explore") })
	if parent := slices.IndexFunc(lines, func(l string) bool { return strings.Contains(l, "general-purpose") }); y != parent+1 {
		t.Fatalf("the Explore row is at %d, want %d under its parent:\n%s", y, parent+1, strings.Join(lines, "\n"))
	}
	spot, ok := Roster{}.At(agents, f.RunningTasks, rosterWidth, 12, y)
	if !ok || spot.dispatch != "toolu_ac" {
		t.Errorf("a click on the Explore row (%d) resolved to %q, want toolu_ac", y, spot.dispatch)
	}
}

// Opening a nested row views that subagent's own transcript and asks for its own
// file, which claude keeps flat beside its parent's.
func TestOpeningANestedRowViewsThatSubagent(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(120, 40)
	for _, ev := range crossStarted() {
		a = a.applyFrame(taskFrame("s1", ev))
	}
	a = a.pickAgent(2) // alex, aa, then aa's own
	a, frames := opened(t, a)

	if got := viewedBy(t, a, "s1"); got != "toolu_ac" {
		t.Errorf("the pane is viewing %q, want the child's toolu_ac", got)
	}
	asks := subagentAsks(frames)
	if len(asks) != 1 || asks[0].Workflow == nil || asks[0].Workflow.Agent != "ac" {
		t.Errorf("asked for %+v, want the child's own file ac", asks)
	}
}

// A late client is replayed running tasks sorted by id, so a child can fold
// before its parent; it still draws under it.
func TestAChildFoldedBeforeItsParentStillDrawsUnderIt(t *testing.T) {
	f := NewFleet()
	evs := crossStarted()
	for _, ev := range []core.Event{evs[2], evs[1], evs[0]} {
		f, _ = f.Observe(ev, "s1")
	}
	if got, want := strings.Join(shape(f.RunningTasks("s1")), " "), "bb aa >ac"; got != want {
		t.Errorf("drawn as %q, want %q", got, want)
	}
}

// The board's rows view draws a nested subagent a level deeper too.
func TestTheBoardNestsASubagentsOwnSubagent(t *testing.T) {
	a := boardApp(t)
	inner := under("t1", "t2", "d2", "Explore")
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &inner})
	outer, child := -1, -1
	for _, l := range strings.Split(shown(a), "\n") {
		switch {
		case strings.Contains(l, subGlyph+" general-purpose"):
			outer = subColumn(l)
		case strings.Contains(l, subGlyph+" Explore"):
			child = subColumn(l)
		}
	}
	if outer < 0 || child != outer+subStep {
		t.Errorf("the board draws the Explore's ⎿ at %d, want %d under the general-purpose name:\n%s", child, outer+subStep, shown(a))
	}
}

// An inner subagent finishing leaves the sidebar; its parent keeps its row and
// its sibling stays under it.
func TestAFinishedInnerLeavesItsParentAndSiblingInPlace(t *testing.T) {
	f := nestedFleet(t)
	f, _ = f.Observe(ended(nestFirst, core.TaskDone), "s1")
	if got, want := strings.Join(shape(f.RunningTasks("s1")), " "), nestOuter+" >"+nestSecond; got != want {
		t.Errorf("drawn as %q, want %q", got, want)
	}
}
