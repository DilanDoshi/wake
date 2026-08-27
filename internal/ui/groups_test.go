package ui

// The left sidebar, and the two properties it exists for: a workspace is a
// directory, and the count beside it is real.
//
// Every guard here is written against the failure it prevents rather than
// against the implementation that prevents it:
//
//   - grouping on Label folds two repositories that are both on `main` into one
//     row, which is the ordinary case for a fleet spread across seven repos;
//   - an unread count asserted in aggregate stays right while two workspaces
//     swap theirs, so every count is asserted against its own directory and
//     again against its own drawn row;
//   - a list taken straight out of a map reshuffles on every frame, and a list
//     that keeps its input's order reshuffles whenever attention re-ranks the
//     agents it was built from - which is every state change in the fleet;
//   - and a count that exists in the struct and not on screen is the whole
//     feature missing, so the numbers are read back off the drawn column.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestWorkspacesGroupByDirectoryAndNotByBranch(t *testing.T) {
	got := Workspaces([]Agent{
		{ID: "a", Cwd: "/repos/api", Label: "main"},
		{ID: "b", Cwd: "/repos/web", Label: "main"},
		{ID: "c", Cwd: "/repos/api", Label: "auth-fix"},
	})
	if len(got) != 2 {
		t.Fatalf("grouped into %d workspaces, want 2. Two repositories both on `main` are two workspaces, and that is the ordinary case for somebody running agents across seven repos - which is exactly what grouping by Label gets wrong", len(got))
	}
	if got[0].Name != "api" || got[0].Agents != 2 {
		t.Errorf("first workspace = %q with %d agents, want api with 2", got[0].Name, got[0].Agents)
	}
}

func TestUnreadIsSummedPerWorkspace(t *testing.T) {
	agents := []Agent{
		{ID: "a", Cwd: "/repos/api", Unread: 3},
		{ID: "b", Cwd: "/repos/api", Unread: 1},
		{ID: "c", Cwd: "/repos/web", Unread: 0},
	}
	got := Workspaces(agents)
	if got[0].Unread != 4 {
		t.Errorf("api has %d unread, want 4", got[0].Unread)
	}

	// The badge is right-aligned, so "web 0" is a string the sidebar could not
	// produce even while drawing the zero - which is why this asks the drawn
	// row whether it carries a digit at all.
	row := rowFor(t, Groups{}.View(agents, groupsWidth, 6), "web")
	if strings.ContainsAny(row, "0123456789") {
		t.Errorf("a workspace with nothing unread drew a zero (%q). A count of nothing is chrome, and a sidebar of zeroes is one nobody reads", row)
	}
}

// The aggregate is the shape this project has shipped defects behind twice: a
// total that stays right while two rows swap what they carry. So the counts are
// checked against their own directories, and then again against their own drawn
// rows - because a sum that is right in the struct and rendered against the
// wrong name is the same defect one layer out.
func TestUnreadIsAttributedToItsOwnWorkspaceAndNotOnlyToTheTotal(t *testing.T) {
	agents := []Agent{
		{ID: "a", Cwd: "/repos/api", Unread: 4},
		{ID: "b", Cwd: "/repos/web", Unread: 7},
	}
	want := map[string]int{"/repos/api": 4, "/repos/web": 7}

	for _, ws := range Workspaces(agents) {
		if ws.Unread != want[ws.Dir] {
			t.Errorf("%s carries %d unread, want %d: the total across the sidebar is right either way, which is why it is not what this asserts", ws.Dir, ws.Unread, want[ws.Dir])
		}
	}

	view := Groups{}.View(agents, groupsWidth, 4)
	for dir, n := range want {
		row := rowFor(t, view, workspaceName(dir))
		if !strings.Contains(row, strconv.Itoa(n)) {
			t.Errorf("the row for %s reads %q and does not carry its %d: a count that is right in the struct and wrong on the screen is the feature missing", dir, row, n)
		}
	}
}

// Two failures live here and only one of them is the map.
//
// Workspaces is fed Fleet.Agents(), which is in *attention* order - so its
// input reorders itself whenever any agent changes state. A sidebar that
// preserved its input's order would therefore reshuffle under the operator's
// hand exactly when the fleet is busiest, and it would do so deterministically,
// which no repeat-call check can see.
//
// Twelve directories rather than two, deliberately: two elements come out of a
// randomised map in the right order half the time, so a two-element fixture is
// a coin flip that passes on any given run. Twelve is 479,001,600 orderings.
//
// They share a basename on purpose as well. An order taken from anything but
// the whole directory - the displayed name, the agent count, the unread count -
// leaves twelve equal keys and falls back to whatever the map handed over,
// which is the same reshuffle wearing a sort.
func TestAWorkspaceListIsStableBetweenFrames(t *testing.T) {
	in := make([]Agent, 0, 12)
	for i := range 12 {
		in = append(in, Agent{ID: fmt.Sprintf("s%d", i), Cwd: fmt.Sprintf("/repos/r%02d/api", i)})
	}

	first, second := Workspaces(in), Workspaces(in)
	for i := range first {
		if first[i].Dir != second[i].Dir {
			t.Fatal("two calls ordered the workspaces differently. Go's map iteration is randomised, so a sidebar built straight out of one reshuffles on every single frame")
		}
	}

	shuffled := slices.Clone(in)
	slices.Reverse(shuffled)
	for i, ws := range Workspaces(shuffled) {
		if ws.Dir != first[i].Dir {
			t.Fatalf("row %d is %s when the agents arrive in one order and %s in another. Workspaces is fed Fleet.Agents(), which re-ranks on every state change, so an order taken from the input is one that reshuffles while somebody is aiming at it", i, ws.Dir, first[i].Dir)
		}
	}
}

func TestALargeUnreadCountStaysOneBadgeWide(t *testing.T) {
	if got := unreadBadge(1234); ansi.StringWidth(got) > 3 {
		t.Errorf("badge %q is %d columns: a 16-column sidebar cannot spend four of them on a number nobody needs exactly", got, ansi.StringWidth(got))
	}
	// The bound is the point and the boundary is what makes it honest: a count
	// that still fits is drawn exactly, and one that does not says so rather
	// than lying by truncation.
	if got, want := unreadBadge(maxBadge), strconv.Itoa(maxBadge); got != want {
		t.Errorf("unreadBadge(%d) = %q, want %q: a count that fits is drawn exactly", maxBadge, got, want)
	}
	if got := unreadBadge(maxBadge + 1); got == strconv.Itoa(maxBadge) {
		t.Errorf("unreadBadge(%d) = %q, which is a different number: a bounded badge must mark what it dropped, not silently read as the bound", maxBadge+1, got)
	}
	for _, n := range []int{1, 9, maxBadge, maxBadge + 1, 1234, 1 << 40} {
		if w := ansi.StringWidth(unreadBadge(n)); w == 0 || w > 3 {
			t.Errorf("unreadBadge(%d) = %q is %d columns: every count above zero has to be visible and none of them may be four columns wide", n, unreadBadge(n), w)
		}
	}
}

// The widths below groupsWidth are not widths Layout.Regions can produce, and
// they are here because the final clamp in row is otherwise a guard nothing can
// reach: the arithmetic above it already lands on the column exactly, at every
// width where a lead, a name and a count all fit. One, two and three are where
// they do not.
func TestTheGroupsSidebarMeasuresExactlyTheColumnItWasGiven(t *testing.T) {
	agents := []Agent{{ID: "a", Cwd: "/a/very/long/path/to/a/repository/name", Unread: 12}}
	for _, w := range []int{1, 2, 3, 8, groupsWidth, 30} {
		out := Groups{}.View(agents, w, 5)
		if got := lipgloss.Width(out); got != w {
			t.Errorf("width %d: measured %d", w, got)
		}
		if got := lipgloss.Height(out); got != 5 {
			t.Errorf("width %d: measured %d rows, want 5. Panes are joined side by side, so a sidebar that is short pulls the row below it up across the whole frame", w, got)
		}
	}
}

// An empty sidebar still holds its column, and that is not cosmetic: the panes
// are joined side by side and Layout.Hit maps a mouse column to a region using
// the widths the layout reserved. A left sidebar that measured nothing would
// draw the room sixteen columns to the left of where every click is resolved.
func TestASidebarWithNoWorkspacesInItStillHoldsItsColumn(t *testing.T) {
	out := Groups{}.View(nil, groupsWidth, 5)
	if got := lipgloss.Width(out); got != groupsWidth {
		t.Errorf("an empty sidebar measured %d columns, want %d", got, groupsWidth)
	}
	if got := lipgloss.Height(out); got != 5 {
		t.Errorf("an empty sidebar measured %d rows, want 5", got)
	}
}

// Zero is how the layout spells "collapsed" - Regions().Groups is 0 below 160
// columns - so a floor here would draw a sidebar nobody reserved space for and
// push the conversation, the divider and the divider's hit test sideways.
func TestACollapsedSidebarDrawsNothingAtAll(t *testing.T) {
	agents := []Agent{{ID: "a", Cwd: "/repos/api", Unread: 3}}
	collapsed := []struct{ w, h int }{{0, 20}, {groupsWidth, 0}, {-1, 20}}
	for _, geo := range collapsed {
		out := Groups{}.View(agents, geo.w, geo.h)
		if out != "" {
			t.Errorf("View(%d, %d) drew %q: zero is what Layout.Regions returns for a collapsed sidebar, and anything drawn there shifts every pane to its right", geo.w, geo.h, out)
		}
	}
}

// The count is the reason this sidebar exists, and repository names are longer
// than sixteen columns. Spending the name's letters on the count is the trade;
// spending the count on the name loses the thing the operator came back for.
func TestALongWorkspaceNameLosesLettersAndNeverItsCount(t *testing.T) {
	agents := []Agent{{ID: "a", Cwd: "/repos/pufferfish-contracts", Unread: 12}}
	out := stripANSI(Groups{}.View(agents, groupsWidth, 1))

	if !strings.Contains(out, "12") {
		t.Errorf("a 20-character workspace name pushed its count off the row: %q. An hour inside a DM must not cost you what accumulated, and this is the row that says it did", out)
	}
	if !strings.Contains(out, "puff") {
		t.Errorf("the row reads %q and does not name the workspace at all: the count has to fit beside a name, not instead of one", out)
	}
	if got := lipgloss.Width(out); got != groupsWidth {
		t.Errorf("the row measured %d columns, want %d", got, groupsWidth)
	}
}

// A row is a directory, so the cursor is a directory. Basenames collide - two
// checkouts of the same repository, a worktree beside its origin - and a
// selection keyed on the name would light both of them up.
func TestSelectionIsByDirectoryBecauseTwoWorkspacesCanShareAName(t *testing.T) {
	agents := []Agent{{ID: "a", Cwd: "/one/api"}, {ID: "b", Cwd: "/two/api"}}
	rows := strings.Split(stripANSI(Groups{Selected: "/two/api"}.View(agents, groupsWidth, 2)), "\n")

	if strings.HasPrefix(rows[0], selectedMark) {
		t.Errorf("/one/api is marked as selected (%q) while the cursor is on /two/api: two repositories called api are two rows, and a cursor that lands on both is one that lands on neither", rows[0])
	}
	if !strings.HasPrefix(rows[1], selectedMark) {
		t.Errorf("/two/api is not marked as selected: %q", rows[1])
	}
}

// Derived from the declaration by reflection rather than restated: a field the
// type carries and the grouping never fills is a column that silently draws
// nothing, and a hand-written list of today's four would pass over the fifth.
func TestEveryFieldOfAWorkspaceIsFilledInByTheGrouping(t *testing.T) {
	got := Workspaces([]Agent{{ID: "a", Cwd: "/repos/api", Unread: 2}})
	if len(got) != 1 {
		t.Fatalf("grouped one agent into %d workspaces", len(got))
	}

	v := reflect.ValueOf(got[0])
	rt := v.Type()
	for i := range rt.NumField() {
		if v.Field(i).IsZero() {
			t.Errorf("Workspace.%s is zero after grouping an agent that has one: a field the sidebar carries and the grouping never fills is a row that draws nothing where something belongs", rt.Field(i).Name)
		}
	}
}

// notDrawnByAGroupRow is every Workspace field the row deliberately does not
// draw, with the reason. Checked both ways below - a field added with no
// decision here fails, and an excuse whose field no longer exists fails too -
// which is decisions.md's rule for a list that genuinely has to be written by
// hand.
var notDrawnByAGroupRow = map[string]string{
	"Agents": "how many sessions are in the workspace. This is the conversation list, and the number on a conversation row is what you have not read; per-agent detail is the right sidebar's column, which has the width for it",
}

// The other direction, and the one that loses a feature quietly: a count summed
// into a struct nothing renders is the whole of this task missing, and every
// unit test over Workspaces would still be green.
func TestEveryFieldOfAWorkspaceIsDrawnByARowOrSaysWhyNot(t *testing.T) {
	src := groupsSource(t)
	declared := declaredFieldsOf(t, src, "Workspace")
	drawn := fieldsReadBy(t, src, "row", "Workspace")

	for _, name := range declared {
		if drawn[name] {
			continue
		}
		if _, excused := notDrawnByAGroupRow[name]; !excused {
			t.Errorf("Workspace.%s is computed and no row reads it: draw it, or add it to notDrawnByAGroupRow with the reason", name)
		}
	}
	for name := range notDrawnByAGroupRow {
		if !slices.Contains(declared, name) {
			t.Errorf("notDrawnByAGroupRow excuses Workspace.%s, which no longer exists: a dead excuse is what makes deleting a field a three-place edit", name)
		}
	}
}

// The seam, and the reason every test above it is not enough: they hand
// Workspaces an Agent with Unread already set, so they pin the sum and say
// nothing about whether the field being summed is the one the room's fold
// actually increments. This drives real events through the Fleet the way the
// stream does, and reads the numbers back off the drawn column.
//
// The quiet marker is half of it on purpose. For 8 of 52 recorded turns it is
// the only line the room shows, so an unread rule that skipped it would leave
// an agent's whole turn accounted for nowhere.
func TestTheSidebarCountsWhatTheRoomShowedIncludingTheTurnThatSaidNothing(t *testing.T) {
	f := NewFleet().WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Dir: "/repos/api", Label: "main", State: rpc.StateWorking},
		{ID: "s2", Name: "john", Dir: "/repos/api", Label: "auth-fix", State: rpc.StateWorking},
		{ID: "s3", Name: "maya", Dir: "/repos/web", Label: "main", State: rpc.StateWorking},
	}})

	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "mapped three approaches"}, "s1")
	f, _ = f.Observe(core.Event{Kind: core.KindTurnEnd}, "s1") // spoke, so the turn end draws nothing
	f, _ = f.Observe(core.Event{Kind: core.KindTurnEnd}, "s2") // said nothing: the marker is the line
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "done"}, "s3")
	// The operator's own broadcast is a room line and is not unread: a sidebar
	// that lights up every workspace the moment you press Enter is one nobody
	// reads a number off again.
	f, _ = f.Observe(core.Event{Kind: core.KindUserText, Text: "status please"}, "s3")

	assertUnread(t, f, "the room drew three lines: two in api and one in web", map[string]int{
		"/repos/api": 2,
		"/repos/web": 1,
	})

	// An hour inside sydney's DM must not cost you what accumulated - not in
	// the other workspace, and not beside sydney's own workspace-mate.
	f = f.Focus("s1")
	assertUnread(t, f, "opening one agent's DM reads that agent and nothing else", map[string]int{
		"/repos/api": 1,
		"/repos/web": 1,
	})

	// One line each into the open DM and into its workspace-mate. Three would
	// mean the open DM counts against you anyway; one would mean an agent you
	// are not reading is silently read because its neighbour's DM is open.
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "still here"}, "s1")
	f, _ = f.Observe(core.Event{Kind: core.KindAssistantText, Text: "and me"}, "s2")
	assertUnread(t, f, "a line from the agent you are reading is read; a line from the one beside it is not", map[string]int{
		"/repos/api": 2,
		"/repos/web": 1,
	})
}

// A session the daemon could not name a directory for is still an agent whose
// lines are arriving. Dropping it would put its unread count in a row that does
// not exist.
func TestAnAgentWithNoDirectoryIsStillARowSomewhere(t *testing.T) {
	got := Workspaces([]Agent{{ID: "a", Cwd: "", Unread: 3}})
	if len(got) != 1 {
		t.Fatalf("grouped an agent with no directory into %d workspaces, want 1", len(got))
	}
	if got[0].Name == "" || got[0].Unread != 3 {
		t.Errorf("the row is %q with %d unread, want a name and 3: an empty Dir is legitimate - rpc.SessionStatus says so - and its lines have to be counted somewhere the operator can see them", got[0].Name, got[0].Unread)
	}
}

func TestMoreWorkspacesThanRowsAreCutAndTheColumnStaysExact(t *testing.T) {
	agents := make([]Agent, 0, 10)
	for i := range 10 {
		agents = append(agents, Agent{ID: fmt.Sprintf("s%d", i), Cwd: fmt.Sprintf("/repos/r%02d", i)})
	}

	out := Groups{}.View(agents, groupsWidth, 3)
	if got := lipgloss.Height(out); got != 3 {
		t.Errorf("ten workspaces in three rows drew %d rows: a sidebar taller than its pane scrolls the alt screen on every draw", got)
	}
	if got := lipgloss.Width(out); got != groupsWidth {
		t.Errorf("measured %d columns, want %d", got, groupsWidth)
	}
	if row := rowFor(t, out, "r00"); !strings.Contains(row, "r00") {
		t.Errorf("the first workspace in order is not on the first row: %q", row)
	}
	if strings.Contains(stripANSI(out), "r03") {
		t.Error("a fourth workspace was drawn into three rows")
	}
}

// assertUnread reads the counts back off the drawn column as well as off the
// structs, because a number that never reaches a row is a number nobody has.
func assertUnread(t *testing.T, f Fleet, when string, want map[string]int) {
	t.Helper()
	agents := f.Agents()
	got := map[string]int{}
	for _, ws := range Workspaces(agents) {
		got[ws.Dir] = ws.Unread
	}
	for dir, n := range want {
		if got[dir] != n {
			t.Errorf("%s: %s has %d unread, want %d", when, dir, got[dir], n)
		}
	}

	view := Groups{}.View(agents, groupsWidth, 6)
	for dir, n := range want {
		row := rowFor(t, view, workspaceName(dir))
		if !strings.Contains(row, strconv.Itoa(n)) {
			t.Errorf("%s: the row for %s reads %q and does not carry its %d", when, dir, row, n)
		}
	}
}

// rowFor is the one drawn row naming what was asked for, and it fails on two
// matches rather than picking one: an assertion that silently reads the wrong
// row is the compensating-change shape these tests exist to avoid.
func rowFor(t *testing.T, view, name string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(stripANSI(view), "\n") {
		if strings.Contains(line, name) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d rows name %q in:\n%s", len(found), name, stripANSI(view))
	}
	return found[0]
}

// groupsSource parses groups.go for the guards that derive their sets from the
// declaration instead of restating it.
func groupsSource(t *testing.T) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "groups.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing groups.go: %v", err)
	}
	return f
}

// declaredFieldsOf is every field name of a struct type declared in file.
func declaredFieldsOf(t *testing.T, file *ast.File, typeName string) []string {
	t.Helper()
	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		ts, ok := n.(*ast.TypeSpec)
		if !ok || ts.Name.Name != typeName {
			return true
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, f := range st.Fields.List {
			for _, name := range f.Names {
				out = append(out, name.Name)
			}
		}
		return false
	})
	if len(out) == 0 {
		t.Fatalf("found no fields on %s in groups.go: the scan is broken, and a broken scan asserts nothing", typeName)
	}
	return out
}

// fieldsReadBy is every field of the named function's typeName-typed parameter
// that its body reads. Selector expressions are matched against that parameter
// by name, so an unrelated lipgloss.Width cannot stand in for a Workspace field
// somebody called Width.
func fieldsReadBy(t *testing.T, file *ast.File, funcName, typeName string) map[string]bool {
	t.Helper()
	fn := funcNamed(t, file, funcName)
	param := paramOfType(t, fn, typeName)

	out := map[string]bool{}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		if id, ok := sel.X.(*ast.Ident); ok && id.Name == param {
			out[sel.Sel.Name] = true
		}
		return true
	})
	if len(out) == 0 {
		t.Fatalf("%s reads no field of its %s at all: the scan is broken, and a broken scan asserts nothing", funcName, typeName)
	}
	return out
}

func funcNamed(t *testing.T, file *ast.File, name string) *ast.FuncDecl {
	t.Helper()
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name && fn.Body != nil {
			return fn
		}
	}
	t.Fatalf("groups.go declares no func %s", name)
	return nil
}

func paramOfType(t *testing.T, fn *ast.FuncDecl, typeName string) string {
	t.Helper()
	for _, p := range fn.Type.Params.List {
		if id, ok := p.Type.(*ast.Ident); ok && id.Name == typeName && len(p.Names) > 0 {
			return p.Names[0].Name
		}
	}
	t.Fatalf("func %s takes no %s", fn.Name.Name, typeName)
	return ""
}
