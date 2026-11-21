package ui

// The sidebar's search bar: three rows atop the roster that filter it in place by
// the jump picker's matcher. A click focuses it, typing edits it, esc clears it and
// gives the keys back, and the filter stands after a click elsewhere.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// sideColumn is the sidebar's column as drawn, a row at a time, styling stripped.
func sideColumn(a App) []string {
	r := a.regions()
	from := a.layout.Width - (r.Roster - dividerWidth)
	rows := strings.Split(a.View(), "\n")
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, strings.TrimRight(ansi.Cut(stripANSI(row), from, a.layout.Width), " "))
	}
	return out
}

// sidebarHas is the first sidebar row holding sub, or -1.
func sidebarHas(a App, sub string) int { return rowHaving(sideColumn(a), sub) }

// barFleet is the blue fleet with its teams declared, so the sidebar draws them as boxes.
func barFleet(t testing.TB, w, h int) App {
	t.Helper()
	a := blueFleet(t, w, h)
	st := rpc.Status{Running: true, Teams: []string{"payments", "web", "infra", "qa", "docs"}}
	for _, ag := range a.fleet.OnRoster() {
		st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: ag.ID, Name: ag.Name, Team: ag.Team, State: ag.State, Tool: ag.Tool, ToolArg: ag.ToolArg})
	}
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st}).resizePanes()
}

// barApp is the blue fleet with the sidebar drawn and the search bar clicked.
func barApp(t testing.TB) App {
	t.Helper()
	a := barFleet(t, 140, 40)
	a, _ = click(a, sidebarX(a), 1)
	if !a.barFocused() {
		t.Fatal("a click on the search bar did not focus it")
	}
	return a
}

func TestTheBarIsDrawnAtopTheSidebarOverAll(t *testing.T) {
	a := barFleet(t, 140, 40)
	rows := sideColumn(a)
	if !strings.Contains(rows[0], "╭") || !strings.Contains(rows[1], "⌕ search agents") || !strings.Contains(rows[2], "╰") {
		t.Fatalf("the bar's three rows are %q, want a box holding the placeholder", rows[:3])
	}
	if !strings.Contains(rows[3], "all") {
		t.Errorf("row 3 is %q, want the pinned all under the bar", rows[3])
	}
	if a.barFocused() {
		t.Error("the bar holds the keys before anyone clicked it")
	}
}

func TestAClickOnAnyBarRowFocusesItAndAClickElsewhereLetsGo(t *testing.T) {
	for y := range searchBarRows {
		a := barFleet(t, 140, 40)
		a, _ = click(a, sidebarX(a), y)
		if !a.barFocused() {
			t.Errorf("a click on bar row %d did not focus it", y)
		}
	}
	a := barApp(t)
	a, _ = click(a, 5, 5) // the room's transcript
	if a.barFocused() {
		t.Error("a click in the room left the bar holding the keys")
	}
}

func TestTypingFiltersTheSidebarInPlaceAndCountsWhatMatched(t *testing.T) {
	a := jumpTyped(barApp(t), "blu")
	text := strings.Join(sideColumn(a), "\n")
	for _, want := range []string{"ui-blueprint", "web-blur", "infra-blue-green", "qa-visual-blur", "docs-bulk-upload", "5/8"} {
		if !strings.Contains(text, want) {
			t.Errorf("the filtered sidebar has no %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"pay-ledger", "scratch", "notes"} {
		if strings.Contains(text, gone) {
			t.Errorf("the filtered sidebar still lists %q", gone)
		}
	}
	if got := a.composer().Value(); got != "" {
		t.Errorf("the draft became %q: typing belongs to the bar", got)
	}
	if web, qa := sidebarHas(a, "web-blur"), sidebarHas(a, "qa-visual-blur"); web < 0 || qa < web {
		t.Errorf("teams are out of the sidebar's own order (web-blur row %d, qa row %d)", web, qa)
	}
}

func TestAFilterNothingMatchesSaysSoUnderAll(t *testing.T) {
	a := jumpTyped(barApp(t), "zzq")
	if i := sidebarHas(a, "no agent matches"); i < 0 {
		t.Errorf("no row says nothing matches:\n%s", strings.Join(sideColumn(a), "\n"))
	}
	if !strings.Contains(strings.Join(sideColumn(a), "\n"), "0/8") {
		t.Error("the count is not 0/8")
	}
}

func TestAnAllWithNoLooseAgentUnderItIsARuleNotAnOpenBox(t *testing.T) {
	a := jumpTyped(barApp(t), "web-b") // only a team member matches, so no loose agent does
	row := sideColumn(a)[searchBarRows]
	if strings.Contains(row, "╭") || !strings.Contains(row, "all") {
		t.Errorf("the all row is %q, want a rule with no box corner over nothing", row)
	}
}

func TestTheFilterStandsAfterTheKeysLeaveAndEscClearsIt(t *testing.T) {
	a := jumpTyped(barApp(t), "blu")
	a, _ = click(a, 5, 5)
	if a.barFocused() || a.sideSearch.Query != "blu" || sidebarHas(a, "pay-ledger") >= 0 {
		t.Fatalf("after a click elsewhere: focused %v, query %q, pay-ledger row %d; want the filter standing", a.barFocused(), a.sideSearch.Query, sidebarHas(a, "pay-ledger"))
	}
	if !strings.Contains(strings.Join(sideColumn(a), "\n"), "5/8") {
		t.Error("the standing filter's count is gone")
	}
	a, _ = click(a, sidebarX(a), 1)
	a, _ = pressKey(a, kmsg(tea.KeyEsc))
	if a.barFocused() || a.sideSearch.Query != "" || sidebarHas(a, "pay-ledger") < 0 {
		t.Errorf("after esc: focused %v, query %q, pay-ledger row %d; want it cleared and every agent back", a.barFocused(), a.sideSearch.Query, sidebarHas(a, "pay-ledger"))
	}
	a = jumpTyped(a, "hello")
	if got := a.composer().Value(); got != "hello" {
		t.Errorf("the draft is %q after esc: the keys did not go back to the composer", got)
	}
}

func TestBackspaceAndSpaceEditTheFilterAtItsEdges(t *testing.T) {
	a := barApp(t)
	a, _ = pressKey(a, kmsg(tea.KeyBackspace))
	a, _ = pressKey(a, kmsg(tea.KeySpace))
	if a.sideSearch.Query != "" {
		t.Fatalf("⌫ on nothing and a leading space left %q", a.sideSearch.Query)
	}
	a = jumpTyped(a, "web")
	a, _ = pressKey(a, kmsg(tea.KeySpace))
	a = jumpTyped(a, "b")
	a, _ = pressKey(a, kmsg(tea.KeyBackspace))
	if a.sideSearch.Query != "web " {
		t.Errorf("the filter is %q, want %q", a.sideSearch.Query, "web ")
	}
}

func TestTheCursorGoesToTheFirstMatchWhenItsAgentIsFilteredOut(t *testing.T) {
	a := barApp(t)
	a.roster.Selected = "s1" // pay-ledger
	a = jumpTyped(a, "blu")
	if want := a.fleet.sectioned(a.rosterAgents())[0].ID; a.roster.Selected != want {
		t.Errorf("the cursor is on %q, want the first match %q", a.roster.Selected, want)
	}
	a.roster.Selected = "s4" // infra-blue-green, a match and not the first: typing on keeps it
	a = jumpTyped(a, "e")
	if a.roster.Selected != "s4" {
		t.Errorf("the cursor left %q, which still matches", "s4")
	}
}

func TestArrowsWalkTheMatchesAndEnterOpensTheCursorsAgentAsAClickDoes(t *testing.T) {
	for _, down := range []tea.KeyType{tea.KeyDown, tea.KeyShiftDown} {
		a := jumpTyped(barApp(t), "blu")
		first, seen := a.roster.Selected, map[string]bool{}
		for range 2 * len(a.rosterAgents()) { // the walk wraps, and never lands on what the filter hides
			a, _ = pressKey(a, kmsg(down))
			if a.roster.Selected == "" && a.roster.Header != "" && !a.cursorFilteredOut() {
				continue // a channel header is a stop of the walk too, and one the filter still draws
			}
			if !slices.ContainsFunc(a.rosterAgents(), func(ag Agent) bool { return ag.ID == a.roster.Selected }) {
				t.Fatalf("%v landed on %q (header %q), which the filter hides", down, a.roster.Selected, a.roster.Header)
			}
			seen[a.roster.Selected] = true
		}
		if len(seen) != len(a.rosterAgents()) || a.roster.Selected == "" {
			t.Fatalf("%v walked %d of the %d matches", down, len(seen), len(a.rosterAgents()))
		}
		a.roster = Roster{Selected: first}
		for a.roster.Selected == first || a.roster.Selected == "" { // on to the next agent, past any header
			a, _ = pressKey(a, kmsg(down))
		}
		want := clickAgent(t, barFleet(t, 140, 40), a.roster.Selected)
		got, _ := pressKey(a, kmsg(tea.KeyEnter))
		if !slices.Equal(tops(got), tops(want)) || got.focus != want.focus {
			t.Errorf("↵ left columns %q and focus %q; a click leaves %q and %q", tops(got), got.focus, tops(want), want.focus)
		}
		if got.barFocused() || got.sideSearch.Query != "blu" {
			t.Errorf("after ↵ the bar is focused %v with %q, want the keys moved on and the filter standing", got.barFocused(), got.sideSearch.Query)
		}
	}
}

func TestCtrlBAndCtrlDOpenWhereTheJumpBoxDoes(t *testing.T) {
	a := clickAgent(t, barFleet(t, 200, 40), "s2")
	a, _ = click(a, sidebarX(a), 1)
	a = jumpTyped(a, "scr")
	below, _ := pressKey(a, kmsg(tea.KeyCtrlB))
	if got := below.grid.Cols[len(below.grid.Cols)-1]; got.Top != "s2" || got.Bottom != "s7" {
		t.Errorf("⌃B left the last column as %+v, want scratch under web-blur", got)
	}
	here, _ := pressKey(a, kmsg(tea.KeyCtrlD))
	if here.focus != "s7" {
		t.Errorf("⌃D left the keys in %q", here.focus)
	}
}

func TestAnAgentAlreadyOnScreenIsOnlyFocusedWhateverKeyOpensIt(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyCtrlB, tea.KeyCtrlD} {
		a := clickAgent(t, barFleet(t, 200, 40), "s2").showRoom()
		a, _ = click(a, sidebarX(a), 1)
		a = jumpTyped(a, "web-b")
		before := tops(a)
		a, _ = pressKey(a, kmsg(key))
		if !slices.Equal(tops(a), before) || a.focus != "s2" {
			t.Errorf("key %v: columns %q focus %q, want %q with the keys on s2", key, tops(a), a.focus, before)
		}
	}
}

func TestAKeyTheBarDoesNotReadLetsGoAndDoesItsOwnJob(t *testing.T) {
	a := jumpTyped(barApp(t), "blu")
	a, _ = pressKey(a, kmsg(tea.KeyCtrlF))
	if a.barFocused() || !a.jumpPicker.Open() || a.sideSearch.Query != "blu" {
		t.Errorf("⌃F: bar focused %v, box open %v, filter %q; want the box up, the bar let go and its filter standing",
			a.barFocused(), a.jumpPicker.Open(), a.sideSearch.Query)
	}
	b := jumpOpened(t, barFleet(t, 140, 40))
	if b.barFocused() {
		t.Error("⌃F focused the bar")
	}
	// A key nothing binds goes on to the composer, and App.key's own App is thrown away with it.
	c, _ := pressKey(jumpTyped(barApp(t), "blu"), kmsg(tea.KeyCtrlG))
	if c.barFocused() || c.sideSearch.Query != "blu" {
		t.Errorf("after a key nothing reads: bar focused %v, filter %q; want it let go and the filter standing", c.barFocused(), c.sideSearch.Query)
	}
}

func TestASelectionTheFilterHidesTakesNoKeyAndSaysWhy(t *testing.T) {
	a := jumpTyped(barApp(t), "blu")
	a, _ = click(a, 5, 5)
	a.roster.Selected = "s1" // pay-ledger: no longer drawn
	if !a.rosterPickHidden() {
		t.Fatal("a selection the filter hides counts as drawn")
	}
	notice.Reset()
	got, _ := pressKey(a, kmsg(tea.KeyCtrlD))
	if got.focus != a.focus {
		t.Errorf("⌃D opened %q for an agent nobody can see", got.focus)
	}
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), pickFilteredOut) {
		t.Errorf("the refusal said %q, want %q", n.String(), pickFilteredOut)
	}
	a.sideSearch.Query = ""
	if a.rosterPickHidden() {
		t.Error("clearing the filter left the selection hidden")
	}
}

func TestTheBarAndItsKeysAreGoneWhereNoSidebarIsDrawn(t *testing.T) {
	a := barFleet(t, 140, 40)
	a, _ = click(a, sidebarX(a), 1)
	a.layout.ShowRoster = false
	a = a.resizePanes()
	if a.barFocused() || a.searchBarShown() {
		t.Fatal("the bar holds the keys with no sidebar drawn")
	}
	a = jumpTyped(a, "hi")
	if got := a.composer().Value(); got != "hi" {
		t.Errorf("the draft is %q: a hidden bar took the keys", got)
	}
}

// The wheel's hold was cut for the old list: typing lets go of it, so the window
// opens on the matches with the cursor drawn, not on rows that are no longer there.
func TestTypingLetsGoOfAWheelHeldWindowAndDrawsTheCursor(t *testing.T) {
	a := wheelSidebar(wheelFleet(t), true, 4)
	if !a.rosterView.held {
		t.Fatal("setup: the wheel holds no window")
	}
	a, _ = click(a, sidebarX(a), 1)
	a = jumpTyped(a, "agent2")
	if a.rosterView.held {
		t.Error("the wheel's hold survived an edit of the filter")
	}
	ag, ok := a.fleet.Agent(a.roster.Selected)
	if !ok || !slices.Contains(drawnAgents(a), ag.Name) {
		t.Errorf("the cursor %q is not among the agents drawn %q", ag.Name, drawnAgents(a))
	}
	if a.rosterPickHidden() {
		t.Error("the cursor counts as hidden after the filter homed it")
	}
}

// A sidebar too short for the bar and a list under it draws neither the bar nor a
// filter: a standing query stays, unseen and unapplied, until the room returns.
func TestASidebarTooShortForItsBarDrawsNoBarAndNoFilter(t *testing.T) {
	a := barFleet(t, 140, 40)
	a, _ = click(a, sidebarX(a), 1)
	a = jumpTyped(a, "blu")
	short := a.withSize(140, searchBarRows+allRows+searchListFloor).resizePanes()
	if short.searchBarShown() {
		t.Fatalf("the bar is drawn in a pane %d rows tall", short.paneHeight())
	}
	if got, all := len(short.rosterAgents()), len(short.fleet.OnRoster()); got != all {
		t.Errorf("a filter nobody can see narrowed the roster to %d of %d", got, all)
	}
	if short.sideSearch.Query != "blu" {
		t.Errorf("the standing filter %q was lost", short.sideSearch.Query)
	}
	if back := short.withSize(140, 40).resizePanes(); len(back.rosterAgents()) != 5 {
		t.Errorf("the filter did not return with the room: %d agents", len(back.rosterAgents()))
	}
}

// A click lands on the row drawn under the pointer: with the bar over the roster
// every row sits three lower, and the click's row is read off the frame, not off
// the code that decides where it should be.
func TestAClickOnADrawnRowOpensThatAgentUnderTheBar(t *testing.T) {
	a := jumpTyped(barApp(t), "blu")
	y := sidebarHas(a, "qa-visual-blur")
	if y < searchBarRows {
		t.Fatalf("qa-visual-blur is drawn at row %d", y)
	}
	got, _ := click(a, sidebarX(a), y)
	if got.focus != "s5" {
		t.Errorf("a click on the row drawn for qa-visual-blur (row %d) put the keys in %q, want s5", y, got.focus)
	}
}

// With nothing selected the agent a cursor key acts on is the first one the sidebar
// draws, not the first of the whole roster the filter hides.
func TestWithNothingSelectedTheFirstAgentDrawnIsTheCursorsAgent(t *testing.T) {
	a := jumpTyped(barApp(t), "scr")
	a, _ = click(a, 5, 5)
	a.roster = Roster{}
	agent, ok := a.cursorAgent()
	if want := a.fleet.sectioned(a.rosterAgents())[0]; !ok || agent.ID != want.ID {
		t.Errorf("the cursor's agent is %q, want the first drawn, %q", agent.Name, want.Name)
	}
}

// The roster's rows are what the bar leaves: the count row and the agents drawn fit
// the sidebar's height under it, so the wheel's window and the draw agree.
func TestTheWindowIsCutToTheRowsUnderTheBar(t *testing.T) {
	a := wheelFleet(t)
	if !a.searchBarShown() {
		t.Fatal("setup: no bar is drawn")
	}
	if a.rosterRows()+allRows != a.rosterListHeight() {
		t.Errorf("the window has %d rows under the all's %d, and the list %d", a.rosterRows(), allRows, a.rosterListHeight())
	}
	if countText(a) == "" {
		t.Error("a roster taller than its rows draws no count row")
	}
}

// The wheel only scrolls a roster taller than its rows: a filter that makes it fit
// leaves nothing for the wheel to move.
func TestAFilterThatFitsTheRosterTakesNoWheel(t *testing.T) {
	a := wheelFleet(t)
	a, _ = click(a, sidebarX(a), 1)
	a = jumpTyped(a, "agent25")
	if got := len(drawnAgents(a)) - 1; got != 1 { // the bar's own row reads as an agent's too
		t.Fatalf("the filter left %d agents drawn, want 1", got)
	}
	if a = wheelSidebar(a, true, 3); a.rosterView.held {
		t.Error("the wheel held a window over a roster that fits")
	}
}

// An edit that leaves the cursor where it was still lets go of the wheel's hold: the
// hold's "is it drawn" was worked out for the old list.
func TestAnEditLetsGoOfTheWheelEvenWhereTheCursorStays(t *testing.T) {
	a := wheelFleet(t)
	a.roster = Roster{Selected: a.fleet.sectioned(a.rosterAgents())[0].ID}
	a = wheelSidebar(a, true, 20) // the first agent scrolled out of view
	if !a.rosterPickHidden() {
		t.Fatal("setup: the wheel did not hide the selection")
	}
	a, _ = click(a, sidebarX(a), 1)
	a = jumpTyped(a, "a") // every agent still matches
	if a.rosterPickHidden() {
		t.Error("the filter's edit left the wheel's stale verdict on a selection that is drawn")
	}
}

// ⌃B on an agent already drawn, from a lower pane, only focuses it: the refusal it
// would meet is for an agent that has to be placed.
func TestCtrlBOnAnAgentDrawnAboveOnlyFocusesIt(t *testing.T) {
	a := clickAgent(t, barFleet(t, 200, 40), "s2")
	a, _ = click(a, sidebarX(a), 1)
	a, _ = pressKey(jumpTyped(a, "scr"), kmsg(tea.KeyCtrlB)) // scratch under web-blur, with the keys
	if a.focus != "s7" {
		t.Fatalf("setup: the keys are in %q", a.focus)
	}
	a, _ = click(a, sidebarX(a), 1)
	a, _ = pressKey(a, kmsg(tea.KeyEsc)) // the first filter stands until it is cleared
	a, _ = click(a, sidebarX(a), 1)
	notice.Reset()
	a, _ = pressKey(jumpTyped(a, "web-b"), kmsg(tea.KeyCtrlB))
	if a.focus != "s2" || a.barFocused() {
		t.Errorf("keys in %q, bar focused %v: want the keys on the web-blur pane, the bar let go", a.focus, a.barFocused())
	}
	if n, ok := notice.Latest(); ok && strings.Contains(n.String(), noRowBelow) {
		t.Errorf("⌃B refused with %q for an agent that needed no placing", n.String())
	}
}

// A hold the sidebar's hiding left behind is dropped on the next key, so the bar does
// not come back holding the keys when the sidebar does.
func TestAHiddenBarsHoldIsNotRevivedByTheSidebarReturning(t *testing.T) {
	a := barFleet(t, 140, 40)
	a, _ = click(a, sidebarX(a), 1)
	a.layout.ShowRoster = false
	a, _ = pressKey(a.resizePanes(), kmsg(tea.KeyCtrlF)) // a key App.key takes: its own App is kept
	a.layout.ShowRoster = true
	if a = a.resizePanes(); a.barFocused() {
		t.Error("the bar holds the keys again with the sidebar back")
	}
}

// The bar, the roster and the usage bar total the pane's height at every size: a frame one
// row too tall scrolls the alt screen on every draw.
func TestTheBarTheRosterAndTheUsageBarFillThePaneAtEverySize(t *testing.T) {
	filtered, plain := 0, 0
	for _, w := range []int{100, 140, 200} {
		for h := 8; h <= 46; h++ {
			for _, query := range []string{"", "blu", "zzq"} {
				a := barFleet(t, w, h)
				a.daemon.usage = &rpc.SessionUsage{Percent: 47, Resets: usageResets} // the usage bar under the roster
				a.sideSearch.Query = query
				a = a.resizePanes()
				// The frame is cut to the terminal, so a roster too tall pushes the usage bar off the
				// pane rather than the frame past it: its last row must be the pane's.
				if len(a.usageDrawn()) > 0 {
					if last := sideColumn(a)[a.paneHeight()-1]; !strings.Contains(last, "session") {
						t.Fatalf("%dx%d filter %q: the usage bar does not end the sidebar, its last row is %q", w, h, query, last)
					}
				}
				if a.searchBarShown() && query != "" {
					filtered++
				} else if a.searchBarShown() {
					plain++
				}
			}
		}
	}
	if filtered == 0 || plain == 0 {
		t.Errorf("the sweep drew the bar %d times filtered and %d plain: both must be exercised", filtered, plain)
	}
}

// A row narrower or wider than the sidebar makes the whole column the wrong width, so the
// bar's three rows are exactly the sidebar's wide - for a long query, a wide one and the
// counts at both ends. The sidebar is rosterWidth columns whenever it is drawn, so the
// sweep starts under it, at the narrowest a "50/50" count fits, and stops over.
func TestTheBarIsExactlyAsWideAsTheSidebarAtEveryWidth(t *testing.T) {
	a := barFleet(t, 140, 40)
	for _, query := range []string{"", "blu", "web-blur-and-then-a-very-long-query-indeed", "日本語日本語日本語", "zzq"} {
		for _, focus := range []bool{false, true} {
			a.sideSearch = SideSearch{Query: query, Focus: focus}
			for w := 12; w <= 40; w++ {
				for _, n := range [][2]int{{3, 8}, {50, 50}, {0, 50}} {
					for i, row := range strings.Split(a.searchBar(w, n[0], n[1]), "\n") {
						if got := ansi.StringWidth(row); got != w {
							t.Errorf("width %d, query %q, focus %v, %d/%d: row %d is %d cells wide", w, query, focus, n[0], n[1], i, got)
						}
					}
				}
			}
		}
	}
}

// ↵ from the bar on a dispatch row opens its agent showing that dispatch, as ↵ on the row with
// the bar let go does: the bar walks onto rows, and each opens what it is.
func TestEnterOnADispatchRowFromTheBarShowsThatDispatch(t *testing.T) {
	base := viaUpdate(barFleet(t, 140, 40), taskFrame("s2", started("t1", "toolu_1", "Review the diff", "code-reviewer", core.TaskAgent)))
	a, _ := click(base, sidebarX(base), 1)
	a = jumpTyped(a, "web")
	for a.roster.SelectedTask == "" { // walk down to the dispatch under web-blur
		before := a.roster
		a, _ = pressKey(a, kmsg(tea.KeyDown))
		if a.roster == before {
			t.Fatal("the walk never reached the dispatch row")
		}
	}
	// The reference: the same cursor with the bar let go, then ⌃D.
	ref := a.letGoOfBar()
	ref, _ = pressKey(ref, kmsg(tea.KeyCtrlD))
	got, _ := pressKey(a, kmsg(tea.KeyEnter))
	if got.roster != ref.roster || got.roster.SelectedTask != "toolu_1" {
		t.Errorf("↵ from the bar left the cursor %+v, want %+v (the dispatch row)", got.roster, ref.roster)
	}
	if got.dms["s2"].viewing != ref.dms["s2"].viewing || got.dms["s2"].viewing != "toolu_1" {
		t.Errorf("↵ from the bar views %q, want the dispatch %q", got.dms["s2"].viewing, ref.dms["s2"].viewing)
	}
}

// A click on the bar is the keys moving to it, so what else read them lets go: a box, a picker or
// a view left open would be lit-and-dead under a bar that took the typing, and take it back
// when it closed.
func TestAClickOnTheBarClosesWhateverElseHeldTheKeys(t *testing.T) {
	for name, open := range map[string]func(App) App{
		"the jump box":  func(a App) App { return jumpOpened(t, a) },
		"/wake-help":    func(a App) App { a, _ = typeCommand(a, "/wake-help"); return a },
		"/resume's box": func(a App) App { a.resumePicker = ResumePicker{Rows: twoRows(), Selected: map[string]bool{}}; return a },
	} {
		a := open(barFleet(t, 140, 40))
		a, _ = click(a, sidebarX(a), 1)
		a = jumpTyped(a, "web")
		if a.jumpPicker.Open() || a.help.view.Open() || a.resumePicker.Open() {
			t.Errorf("%s: still open under a bar that took the typing", name)
		}
		if a.sideSearch.Query != "web" {
			t.Errorf("%s: the bar's filter is %q, want what was typed", name, a.sideSearch.Query)
		}
		a, _ = pressKey(a, kmsg(tea.KeyEsc))
		a = jumpTyped(a, "q")
		if a.barFocused() || a.composer().Value() != "q" {
			t.Errorf("%s: after esc the bar holds the keys (%v) and the draft is %q", name, a.barFocused(), a.composer().Value())
		}
	}
}

// A dropped image path goes to the composer as a chip, and the bar lets go of the keys that
// the paste says were meant for the draft.
func TestAnImagePathPastedUnderTheBarLetsGoOfIt(t *testing.T) {
	a, cmd := pressKey(barApp(t), tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("/tmp/shot.png")})
	if cmd == nil || a.barFocused() {
		t.Errorf("after an image drop: read %v, bar focused %v; want the image read and the bar let go", cmd != nil, a.barFocused())
	}
}

// A query longer than the bar shows its end, where the caret is, with the start elided.
func TestALongQueryShowsItsEndAndTheCaret(t *testing.T) {
	a := jumpTyped(barApp(t), "infra-blue-green-and-more")
	row := sideColumn(a)[1]
	if !strings.Contains(row, "…") || !strings.Contains(row, "more"+jumpCaret) || strings.Contains(row, "infra-") {
		t.Errorf("the bar row is %q, want the end of the query, its caret and an elided start", row)
	}
}

// ⌃C is the habit that cancels a search, and parking an agent is not cancelling one: with the bar
// holding the keys the first ⌃C only lets go of it, and the next parks as it always has.
func TestCtrlCOnTheBarOnlyLetsGoOfIt(t *testing.T) {
	a := jumpTyped(barApp(t), "scr") // the cursor is homed on scratch
	got, cmd := pressKey(a, kmsg(tea.KeyCtrlC))
	if got.barFocused() || got.sideSearch.Query != "scr" {
		t.Errorf("after ⌃C: bar focused %v, filter %q; want it let go with the filter standing", got.barFocused(), got.sideSearch.Query)
	}
	if parked := kindsFor(writtenFrames(t, got, cmd), rpc.FramePark); len(parked) != 0 {
		t.Errorf("the first ⌃C parked %v", parked)
	}
	_, cmd = pressKey(got, kmsg(tea.KeyCtrlC))
	if parked := kindsFor(writtenFrames(t, got, cmd), rpc.FramePark); len(parked) != 1 {
		t.Errorf("the second ⌃C parked %v, want the agent the cursor is on", parked)
	}
}

// A wide rune is whole or gone, and the end of a wide query is what shows, at the sidebar's real width.
func TestAWideQueryShowsItsEndWhole(t *testing.T) {
	a := jumpTyped(barApp(t), "日本語日本語日本語")
	row := sideColumn(a)[1]
	if !strings.Contains(row, "語"+jumpCaret) || !strings.Contains(row, "…") {
		t.Errorf("the bar row is %q, want the last rune, the caret and an elided start", row)
	}
}
