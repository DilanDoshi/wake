package ui

// `all` is the roster's first item and scrolls with it, and the agents that are in
// no team stand in a box of their own under it, headed `no team` (the owner's
// 2026-11-25 pick: the box that was `all`'s said "these are all the agents" when it
// meant "the agents with no team", and `all` was pinned over a list it is a row of).

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// teamsOnlyFleet is a manager and agents that are all in teams: nobody else is loose.
func teamsOnlyFleet(t *testing.T) []Agent {
	t.Helper()
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams: []string{"payments", "infra"},
		Sessions: []rpc.SessionStatus{
			{ID: "m", Name: core.ManagerName, State: rpc.StateIdle},
			{ID: "s1", Name: "pay-audit", Team: "payments", State: rpc.StateIdle},
			{ID: "s2", Name: "infra-alerts", Team: "infra", State: rpc.StateIdle},
		},
	})
	return f.sectioned(f.OnRoster())
}

func isPlainRule(line, name string) bool { return isRule(line, name) && !isTopEdge(line) }

// The sidebar opens on a plain `all` rule, then the loose agents' box headed `no
// team` with no blank row between them, then the team boxes as before.
func TestTheRosterOpensOnAllThenTheNoTeamBox(t *testing.T) {
	lines := rosterLines(boxFleet(t), "", noSubsOf, boxedWidth, 40)

	want := []struct {
		kind func(string) bool
		text string
	}{
		{func(l string) bool { return isPlainRule(l, core.BroadcastName) }, "all"},
		{isTopEdge, unteamedLabel}, {isWallRow, "bea"}, {isWallRow, "alex"}, {isFooter, ""}, {nil, ""},
		{isTopEdge, "backend"}, {isWallRow, "thea"},
	}
	for i, w := range want {
		line := strings.TrimRight(lines[i], " ")
		switch {
		case w.kind == nil:
			if line != "" {
				t.Errorf("row %d is %q, want the blank between sections:\n%s", i, line, strings.Join(lines, "\n"))
			}
		case !w.kind(line) || !strings.Contains(line, w.text):
			t.Errorf("row %d is %q, want one holding %q:\n%s", i, line, w.text, strings.Join(lines, "\n"))
		}
	}
}

// The usual fleet, every agent in a team: the manager is the only one with none, so
// the no-team box holds it alone - and a click on its row opens the manager.
func TestWithEveryAgentTeamedTheNoTeamBoxHoldsTheManagerAlone(t *testing.T) {
	agents := teamsOnlyFleet(t)
	lines := rosterLinesOf(Roster{}, agents)

	want := []func(string) bool{
		func(l string) bool { return isPlainRule(l, core.BroadcastName) },
		func(l string) bool { return isTopEdge(l) && strings.Contains(l, unteamedLabel) },
		func(l string) bool { return isWallRow(l) && strings.Contains(l, core.ManagerName) },
		isFooter,
	}
	for i, kind := range want {
		if line := strings.TrimRight(lines[i], " "); !kind(line) {
			t.Errorf("row %d is %q:\n%s", i, line, strings.Join(lines, "\n"))
		}
	}
	if spot, ok := (Roster{}).At(agents, noSubsOf, boxedWidth, 40, 2); !ok || spot.agent.Name != core.ManagerName {
		t.Errorf("a click on the manager's row resolves to (%q, %v)", spot.agent.Name, ok)
	}
}

// `all` is item 0, and its header-bearing neighbour is the first agent: one item per
// agent after it, so the window cuts between items and neither can part from its member.
func TestAllIsTheFirstItemAndTheNoTeamHeaderRidesTheFirstLooseAgent(t *testing.T) {
	agents := boxFleet(t)
	items := sectionRows(agents)
	if len(items) != len(agents)+1 || !items[0].all {
		t.Fatalf("%d items for %d agents, item 0 all=%v: want all first, then one item per agent", len(items), len(agents), items[0].all)
	}
	if n := rowsForRow(items[0], noSubsOf); n != 1 {
		t.Errorf("the all item is %d rows, want 1", n)
	}
	if items[1].header != unteamedLabel || items[1].gap {
		t.Errorf("the first loose agent's item has header %q gap %v, want %q and no gap under all", items[1].header, items[1].gap, unteamedLabel)
	}
	if items[2].hasHeader() {
		t.Errorf("the second loose agent carries a header %q", items[2].header)
	}

	// With nobody loose there is no `no team` box, and the first team opens under all as it did.
	only := sectionRows(agents[2:])
	if only[1].header != "backend" || only[1].gap {
		t.Errorf("with no loose agent the first item has header %q gap %v, want backend with no gap", only[1].header, only[1].gap)
	}
}

// A click on `all` is the all channel; the `no team` header and the gap above a team
// pick nothing; the agents below sit where they are drawn.
func TestAClickOnAllPicksItAndOnTheNoTeamHeaderNothing(t *testing.T) {
	agents := boxFleet(t)
	at := func(y int) (rosterSpot, bool) { return Roster{}.At(agents, noSubsOf, boxedWidth, 40, y) }

	if spot, ok := at(0); !ok || spot.header != core.BroadcastName {
		t.Errorf("row 0 resolves to (%+v, %v), want the all channel", spot, ok)
	}
	for y, what := range map[int]string{1: "the no team header", 5: "the gap above backend", 4: "the loose box's closing row"} {
		if spot, ok := at(y); ok {
			t.Errorf("row %d (%s) resolves to %+v, want nothing", y, what, spot)
		}
	}
	if spot, ok := at(2); !ok || spot.agent.Name != "bea" {
		t.Errorf("row 2 resolves to (%+v, %v), want bea", spot, ok)
	}
	if spot, ok := at(6); !ok || spot.header != "backend" {
		t.Errorf("row 6 resolves to (%+v, %v), want backend's channel", spot, ok)
	}
}

// ⇧↑↓ stop on `all` and on each team's header, and never on `no team`.
func TestTheWalkStopsOnAllAndNeverOnTheNoTeamHeader(t *testing.T) {
	var headers []string
	for _, stop := range channelStops(boxFleet(t), noSubsOf) {
		if stop.Header != "" {
			headers = append(headers, stop.Header)
		}
	}
	want := []string{core.BroadcastName, "backend", "frontend", "qa"}
	if strings.Join(headers, ",") != strings.Join(want, ",") {
		t.Errorf("the walk's headers are %v, want %v", headers, want)
	}
}

// `all · N` when the roster says how many rows it has; a bare `all` when it does not.
func TestAllSaysHowManyRowsTheRosterHas(t *testing.T) {
	agents := boxFleet(t)
	if got := rosterLinesOf(Roster{total: 22}, agents)[0]; !strings.Contains(got, "─ all · 22 ─") {
		t.Errorf("the all row is %q, want `all · 22`", got)
	}
	if got := rosterLinesOf(Roster{}, agents)[0]; !strings.Contains(got, "─ all ─") {
		t.Errorf("the all row with no count is %q, want a bare `all`", got)
	}
}

func rosterLinesOf(r Roster, agents []Agent) []string {
	return strings.Split(stripANSI(r.View(agents, noSubsOf, boxedWidth, 40)), "\n")
}

// The `no team` box is never lit, whichever channel is picked: it is no channel, and
// only the `all` rule says all is picked.
func TestTheNoTeamBoxIsMutedWhateverChannelIsPicked(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor, so the hues render apart
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	agents := boxFleet(t)
	rule := "╰" + strings.Repeat("─", boxedWidth-2) + "╯"

	for _, picked := range []string{"", "backend", "qa"} {
		out := Roster{channel: picked}.View(agents, noSubsOf, boxedWidth, 40)
		lines, plain := strings.Split(out, "\n"), strings.Split(stripANSI(out), "\n")

		at := rowContaining(plain, " "+unteamedLabel+" ")
		if at < 0 {
			t.Fatalf("channel %q: no %s box is drawn:\n%s", picked, unteamedLabel, strings.Join(plain, "\n"))
		}
		edge := lines[at]
		if want := centredEdge(boxBorder.TopLeft, boxBorder.TopRight, unteamedLabel, boxedWidth, HintStyle, HintStyle); edge != want {
			t.Errorf("channel %q: the no team edge is %q, want it muted: %q", picked, edge, want)
		}
		wall := lines[rowContaining(plain, "bea")]
		if !strings.Contains(wall, HintStyle.Render("│ ")) || !strings.Contains(wall, HintStyle.Render(" │")) {
			t.Errorf("channel %q: bea's walls are not muted: %q", picked, wall)
		}
		if closing := lines[rowContaining(plain, "alex")+1]; !strings.Contains(closing, HintStyle.Render(rule)) {
			t.Errorf("channel %q: the no team box's closing row is not muted: %q", picked, closing)
		}
	}
}

// The `all` rule wears the channel's state like any header: lit while picked, muted
// otherwise, its name bright and bold under the cursor.
func TestTheAllRuleWearsTheChannelsState(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	agents := boxFleet(t)
	first := func(r Roster) string { return strings.Split(r.View(agents, noSubsOf, boxedWidth, 40), "\n")[0] }
	rule := func(rule, text lipgloss.Style) string {
		return centredEdge("", "", core.BroadcastName, boxedWidth, rule, text)
	}

	for name, tc := range map[string]struct {
		r    Roster
		want string
	}{
		"picked":            {Roster{}, rule(AccentStyle, AccentStyle)},
		"another is picked": {Roster{channel: "backend"}, rule(HintStyle, HintStyle)},
		"cursored":          {Roster{channel: "backend", Header: core.BroadcastName}, rule(HintStyle, TextStyle.Bold(true))},
		"picked, cursored":  {Roster{Header: core.BroadcastName}, rule(AccentStyle, AccentStyle.Bold(true))},
	} {
		if got := first(tc.r); got != tc.want {
			t.Errorf("%s: the all row is %q, want %q", name, got, tc.want)
		}
	}
}

// The filter cases as mocked: `pay` leaves one team under `all`; `tri` a `no team`
// box of one; `man` the manager alone in it.
func TestAFilteredRosterKeepsAllAndDrawsTheNoTeamBoxOnlyForALooseMatch(t *testing.T) {
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams: []string{"payments"},
		Sessions: []rpc.SessionStatus{
			{ID: "m", Name: core.ManagerName, State: rpc.StateIdle},
			{ID: "s1", Name: "triage", State: rpc.StateIdle},
			{ID: "s2", Name: "pay-audit", Team: "payments", State: rpc.StateIdle},
		},
	})
	for name, tc := range map[string]struct {
		keep  string
		boxes []string
	}{
		"pay": {"pay-audit", []string{"payments"}},
		"tri": {"triage", []string{unteamedLabel}},
		"man": {core.ManagerName, []string{unteamedLabel}},
	} {
		var shown []Agent
		for _, a := range f.OnRoster() {
			if a.Name == tc.keep {
				shown = append(shown, a)
			}
		}
		lines := rosterLinesOf(Roster{total: 3}, f.sectioned(shown))
		if !strings.Contains(lines[0], "all · 3") {
			t.Errorf("%s: the first row is %q, want `all · 3`, the whole roster's count", name, lines[0])
		}
		if !isTopEdge(strings.TrimRight(lines[1], " ")) || !strings.Contains(lines[1], tc.boxes[0]) {
			t.Errorf("%s: row 1 is %q, want the %s box's edge", name, lines[1], tc.boxes[0])
		}
	}
}

// Every row of every item - `all`, the new header, a footer - is drawn in as many
// lines as rowsForRow promises, at any width, or a click lands a row off.
func TestEveryItemDrawsExactlyTheRowsItPromisesAtAnyWidth(t *testing.T) {
	agents := boxFleet(t)
	for width := 1; width <= 30; width++ {
		for i, item := range sectionRows(agents) {
			if got, want := len(Roster{total: 8}.rowLines(item, noSubsOf, width)), rowsForRow(item, noSubsOf); got != want {
				t.Errorf("item %d at width %d draws %d lines, rowsForRow says %d", i, width, got, want)
			}
		}
	}
}

// A short column still draws: the all row when it has two rows, the count alone when
// it has one, and never more rows than it was given.
func TestAShortColumnDrawsWhatItCanOfAllAndTheRest(t *testing.T) {
	agents := boxFleet(t)
	for height := 1; height <= 4; height++ {
		out := strings.Split(stripANSI(Roster{}.View(agents, noSubsOf, boxedWidth, height)), "\n")
		if len(out) != height {
			t.Errorf("a %d-row column drew %d rows", height, len(out))
		}
		if height >= 2 && !isPlainRule(strings.TrimRight(out[0], " "), core.BroadcastName) {
			t.Errorf("a %d-row column opens on %q, want the all row", height, out[0])
		}
		if height == 1 && !strings.HasPrefix(strings.TrimSpace(out[0]), "+") {
			t.Errorf("a 1-row column drew %q, want the count alone", out[0])
		}
	}
	if got := (Roster{}).View(nil, noSubsOf, boxedWidth, 3); !strings.Contains(stripANSI(got), "─ all ─") {
		t.Errorf("an empty roster drew %q, want all", got)
	}
}

// With nothing shown the row that says so sits under all, not above it.
func TestTheNothingMatchesRowSitsUnderAll(t *testing.T) {
	out := strings.Split(stripANSI(Roster{none: searchNoMatch}.View(nil, noSubsOf, boxedWidth, 5)), "\n")
	if !isPlainRule(strings.TrimRight(out[0], " "), core.BroadcastName) || !strings.Contains(out[1], searchNoMatch) {
		t.Errorf("an empty filter drew %q, want all then %q", out[:2], searchNoMatch)
	}
}

// --- all is a row of the list: it scrolls, and the pick stands ---

// allRowY is the sidebar row drawing the all rule, or -1 when it has scrolled away.
func allRowY(a App) int { return sidebarHas(a, "all · ") }

func TestAllScrollsAwayLikeAnyRow(t *testing.T) {
	a := wheelFleet(t)
	if allRowY(a) < searchBarRows {
		t.Fatalf("all is not drawn under the bar at the top of the sidebar:\n%s", strings.Join(sideColumn(a), "\n"))
	}
	if strings.Contains(countText(a), "above") {
		t.Fatalf("the count says %q before any scrolling", countText(a))
	}

	a = wheelSidebar(a, true, 1)
	if y := allRowY(a); y != -1 {
		t.Errorf("all is still drawn at row %d after a notch:\n%s", y, strings.Join(sideColumn(a), "\n"))
	}
	if got := countText(a); !strings.HasPrefix(got, "+1 above") {
		t.Errorf("the count row is %q after all scrolled off, want it to count all among the items above", got)
	}

	a = wheelSidebar(a, false, 1)
	if allRowY(a) < searchBarRows || strings.Contains(countText(a), "above") {
		t.Errorf("a notch back did not bring all home:\n%s", strings.Join(sideColumn(a), "\n"))
	}
}

// The channel a team's header picked stands while all is scrolled off, and the wheel
// picks nothing.
func TestAChannelPickStandsWhileAllIsScrolledOff(t *testing.T) {
	a := wheelFleet(t).selectChannel("backend")
	a = wheelSidebar(a, true, 3)
	if allRowY(a) != -1 {
		t.Fatalf("all is still drawn:\n%s", strings.Join(sideColumn(a), "\n"))
	}
	if a.room.channel != "backend" {
		t.Errorf("the room's channel is %q after the wheel, want backend", a.room.channel)
	}
}

// The cursor on `all` is hidden once all scrolls off, so it takes no key and says so,
// as a hidden team header does; ⇧↑↓ bring the window back the least that shows it.
func TestACursorOnAllThatScrolledOffTakesNoKey(t *testing.T) {
	a := wheelFleet(t)
	a, _ = keyVia(a, tea.KeyShiftDown) // the first agent
	a, _ = keyVia(a, tea.KeyShiftUp)   // all
	if a.roster.Header != core.BroadcastName {
		t.Fatalf("the cursor is %+v, want all", a.roster)
	}
	if _, ok := a.pickedHeader(); !ok {
		t.Fatal("setup: the cursor on a drawn all is not a pick")
	}
	a = wheelSidebar(a, true, 1)
	if !a.rosterPickHidden() {
		t.Error("the cursor on a scrolled-off all counts as drawn")
	}
	if h, ok := a.pickedHeader(); ok {
		t.Errorf("↵ would pick %q while all is out of view", h)
	}
}

// A window that is not held anchors on the cursor: all is item 0, so a cursor on it
// pulls a scrolled window back to the top.
func TestACursorOnAllPullsAScrolledWindowBackToTheTop(t *testing.T) {
	agents := boxFleet(t)
	rows := sectionRows(agents)
	h := itemHeights(rows, noSubsOf)
	r := Roster{Header: core.BroadcastName, view: rosterView{top: 4}}
	if got := r.window(rows, h, 8); got.from != 0 {
		t.Errorf("the window starts at item %d with the cursor on all, want 0", got.from)
	}
}

// --- N ---

var stripSegment = regexp.MustCompile(`(\d+) (need you|working|idle|silent|parked|orphaned|done)`)

// `all · N` is every non-ended row the roster holds, the manager and a parked agent
// included, whatever the filter: the strip's own segments add up to the same number.
func TestAllCountsEveryRosterRowAndTheStripAddsUpToIt(t *testing.T) {
	st := wheelStatus(12, map[string]string{
		"s2": rpc.StateBlocked, "s3": rpc.StateWorking, "s4": rpc.StateParked,
		"s5": rpc.StateEnded, "s6": rpc.StateSilent, "s7": rpc.StateOrphaned,
	})
	st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: "m", Name: core.ManagerName, State: rpc.StateIdle})
	a := newRoomApp(t).withSize(100, 40)
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: st})

	n := len(a.fleet.OnRoster())
	if n != 12 { // 12 agents + the manager, less the ended one
		t.Fatalf("setup: the roster holds %d rows, want 12", n)
	}
	if y := allRowY(a); y < 0 {
		t.Errorf("no all row is drawn:\n%s", strings.Join(sideColumn(a), "\n"))
	} else if !strings.Contains(sideColumn(a)[y], "all · "+strconv.Itoa(n)) {
		t.Errorf("the all row is %q, want `all · %d`", sideColumn(a)[y], n)
	}

	sum := 0
	for _, m := range stripSegment.FindAllStringSubmatch(stripANSI(awarenessStrip(a.fleet.OnRoster(), a.fleet.RunningTasks, "", 200)), -1) {
		k, _ := strconv.Atoi(m[1])
		sum += k
	}
	if sum != n {
		t.Errorf("the strip's segments add up to %d, but the roster has %d rows", sum, n)
	}
}

// The filter narrows the rows below, not the count: the bar already says N/M.
func TestAllKeepsTheWholeCountUnderAFilter(t *testing.T) {
	a := jumpTyped(barApp(t), "blu")
	want := "all · " + strconv.Itoa(len(a.fleet.OnRoster()))
	if y := allRowY(a); y < 0 {
		t.Errorf("no all row is drawn under a filter:\n%s", strings.Join(sideColumn(a), "\n"))
	} else if !strings.Contains(sideColumn(a)[y], want) {
		t.Errorf("under a filter the all row is %q, want the whole roster's count: %q", sideColumn(a)[y], want)
	}
}

// The bar needs the same seven rows it did when all was pinned under it: three of
// its own and four of list (all and three more).
func TestTheBarStillNeedsSevenRowsOfSidebar(t *testing.T) {
	a := barFleet(t, 140, 40)
	smallest := -1
	for h := 1; h <= 30; h++ {
		if s := a.withSize(140, h).resizePanes(); s.searchBarShown() {
			smallest = s.rosterHeight()
			break
		}
	}
	if smallest != 7 {
		t.Errorf("the bar first draws with a sidebar %d rows tall, want 7", smallest)
	}
}
