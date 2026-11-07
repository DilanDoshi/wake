package ui

// The wheel over the right sidebar scrolls the sidebar (the owner's 2026-11-06
// ask: "for in case i have more agents then what can curentl be showin the right
// side bar"), wake-orch's rulings: a roster that fits takes no wheel; the wheel
// never moves the selection, and a selection it scrolls out of view takes no key
// until it is drawn again; ⇧↑↓ move the window the least that shows the cursor.
//
// Everything here goes through Update, as Bubble Tea delivers it, because the
// window lets go of the wheel's hold in Update's seam.

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// wheelTeams is the fleet's teams in the daemon's order; agent i is in
// wheelTeamOf(i), every fourth one in the header-less top block.
var wheelTeams = []string{"backend", "frontend", "infra"}

func wheelTeamOf(i int) string { return []string{"", "backend", "frontend", "infra"}[i%4] }

// wheelStatus is a report of n idle agents, agent01..agentNN, ids s1..sN, with
// the states given by id overriding idle.
func wheelStatus(n int, states map[string]string) *rpc.Status {
	st := &rpc.Status{Running: true, Teams: wheelTeams}
	for i := 1; i <= n; i++ {
		id := fmt.Sprintf("s%d", i)
		state := rpc.StateIdle
		if s, ok := states[id]; ok {
			state = s
		}
		st.Sessions = append(st.Sessions, rpc.SessionStatus{
			ID: id, Name: fmt.Sprintf("agent%02d", i), State: state, Team: wheelTeamOf(i),
		})
	}
	return st
}

// wheelFleet is a fleet taller than its sidebar: 25 agents in the top block and
// three teams, in a 100x30 room, one of them running a subagent. The roster draws
// about twenty of them.
func wheelFleet(t *testing.T) App {
	t.Helper()
	a := newRoomApp(t).withSize(100, 30)
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, nil)})
	return viaUpdate(a, taskFrame("s2", started("t1", "toolu_1", "Review the diff", "code-reviewer", core.TaskAgent)))
}

// viaUpdate folds a frame through Update, the seam included.
func viaUpdate(a App, f rpc.Frame) App {
	m, _ := a.Update(frameMsg{Frame: f})
	return m.(App)
}

// keyVia is a key through Update, and the command it returned.
func keyVia(a App, k tea.KeyType) (App, tea.Cmd) {
	m, cmd := a.Update(tea.KeyMsg{Type: k})
	return m.(App), cmd
}

// clickVia is a press and a release on one cell, through Update.
func clickVia(a App, x, y int) App {
	m, _ := a.Update(pressAt(x, y))
	m, _ = m.Update(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: y})
	return m.(App)
}

// sidebarX is a column inside the roster.
func sidebarX(a App) int { return a.layout.Width - 3 }

// wheelSidebar is n notches over the roster's rows, down (later agents) or up.
func wheelSidebar(a App, down bool, n int) App {
	button := tea.MouseButtonWheelUp
	if down {
		button = tea.MouseButtonWheelDown
	}
	return wheelAt(a, button, sidebarX(a), 6, n)
}

var drawnAgentRow = regexp.MustCompile(`^\S+ (agent\d\d)\b`)

// drawnAgents is the agents the sidebar draws, top to bottom.
func drawnAgents(a App) []string {
	var out []string
	for _, row := range sidebarRows(a) {
		if m := drawnAgentRow.FindStringSubmatch(strings.TrimSpace(ansi.Strip(row))); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

// countText is the sidebar's count row, "" when it draws none.
func countText(a App) string {
	for _, row := range sidebarRows(a) {
		if text := strings.TrimSpace(ansi.Strip(row)); strings.HasPrefix(text, "+") {
			return text
		}
	}
	return ""
}

// agentRowY is the sidebar row drawing name, or -1.
func agentRowY(a App, name string) int {
	for y, row := range sidebarRows(a) {
		if m := drawnAgentRow.FindStringSubmatch(strings.TrimSpace(ansi.Strip(row))); m != nil && m[1] == name {
			return y
		}
	}
	return -1
}

// rankedNames is every roster agent's name in the order the sidebar lays them.
func rankedNames(a App) []string {
	var out []string
	for _, ag := range a.fleet.sectioned(a.fleet.OnRoster()) {
		out = append(out, ag.Name)
	}
	return out
}

func TestTheWheelOverATallSidebarScrollsItOneAgentANotchAndMovesNothingElse(t *testing.T) {
	a := wheelFleet(t)
	a, _ = keyVia(a, tea.KeyShiftDown) // a selection, so "the wheel never moves it" can fail
	before, roster, focus, room := drawnAgents(a), a.roster, a.focus, a.room.tr.scroll
	if len(before) >= 25 {
		t.Fatalf("the fixture fits its sidebar (%d agents drawn); it must overflow", len(before))
	}

	a = wheelSidebar(a, true, 1)
	after := drawnAgents(a)
	if len(after) == 0 || after[0] != before[1] {
		t.Fatalf("one notch down drew %v, want the window one agent on from %v", after, before)
	}
	if a.roster != roster {
		t.Errorf("the wheel moved the selection from %+v to %+v", roster, a.roster)
	}
	if a.focus != focus || a.room.tr.scroll != room {
		t.Errorf("the wheel over the sidebar moved a pane: focus %q→%q, room scroll %d→%d", focus, a.focus, room, a.room.tr.scroll)
	}

	if a = wheelSidebar(a, false, 1); !slices.Equal(drawnAgents(a), before) {
		t.Errorf("one notch back up drew %v, want %v", drawnAgents(a), before)
	}
}

// A flood reaches every agent, stops at each end, and leaves nothing to unwind:
// the first notch back moves at once.
func TestAWheelFloodReachesEveryAgentAndStopsAtTheEnds(t *testing.T) {
	a := wheelFleet(t)
	top := drawnAgents(a)
	seen := map[string]bool{}
	for _, n := range top {
		seen[n] = true
	}
	for range 100 {
		a = wheelSidebar(a, true, 1)
		for _, n := range drawnAgents(a) {
			seen[n] = true
		}
	}
	names := rankedNames(a)
	if len(seen) != len(names) {
		t.Errorf("the wheel reached %d of %d agents", len(seen), len(names))
	}
	bottom := drawnAgents(a)
	if bottom[len(bottom)-1] != names[len(names)-1] {
		t.Errorf("a flood down ends on %v, want the last agent %s drawn", bottom, names[len(names)-1])
	}
	if c := countText(a); !strings.Contains(c, "above") || strings.Contains(c, "below") {
		t.Errorf("at the bottom the count reads %q, want the agents above and none below", c)
	}
	if a = wheelSidebar(a, false, 1); slices.Equal(drawnAgents(a), bottom) {
		t.Error("the first notch back up after a flood moved nothing: the window ran on past the end")
	}
	a = wheelSidebar(a, false, 100)
	if !slices.Equal(drawnAgents(a), top) {
		t.Errorf("a flood up ends on %v, want the top %v", drawnAgents(a), top)
	}
	if c := countText(a); !strings.Contains(c, "below") || strings.Contains(c, "above") {
		t.Errorf("at the top the count reads %q, want the agents below and none above", c)
	}
}

// A roster that fits takes no wheel: the room under the pointer's row is not
// what the pointer is over.
func TestTheWheelOverASidebarThatFitsDoesNothing(t *testing.T) {
	a := roomWithBacklog(t, 200)
	frame, room := a.View(), a.room.tr.scroll
	a = wheelAt(a, tea.MouseButtonWheelUp, sidebarX(a), 6, 3)
	if a.room.tr.scroll != room {
		t.Errorf("the wheel over a sidebar that fits scrolled the room: %d→%d", room, a.room.tr.scroll)
	}
	if a.View() != frame {
		t.Error("the wheel over a sidebar that fits changed the frame")
	}
}

// After the wheel, every row is what a click on it hits.
func TestAClickOnAWheeledSidebarHitsTheRowDrawnThere(t *testing.T) {
	a := wheelSidebar(wheelFleet(t), true, 4)
	for y, row := range sidebarRows(a)[:a.rosterHeight()] {
		text := strings.TrimSpace(ansi.Strip(row))
		spot, ok := a.clickedRow(y)
		if m := drawnAgentRow.FindStringSubmatch(text); m != nil {
			if !ok || spot.agent.Name != m[1] {
				t.Errorf("row %d draws %s and a click hits (%q, %v)", y, m[1], spot.agent.Name, ok)
			}
			continue
		}
		for _, team := range append([]string{core.BroadcastName}, wheelTeams...) {
			if strings.Contains(text, "─") && strings.Contains(text, " "+team+" ") && (!ok || spot.header != team) {
				t.Errorf("row %d draws the %s header and a click hits %+v (%v)", y, team, spot, ok)
			}
		}
		if strings.HasPrefix(text, "+") && ok {
			t.Errorf("the count on row %d hits %+v", y, spot)
		}
	}
}

// A report re-ranking the fleet leaves the wheel's window where it was; a click
// on a drawn row moves the selection and not the window.
func TestTheWheelsWindowSurvivesAReportAndAClick(t *testing.T) {
	a := wheelSidebar(wheelFleet(t), true, 5)
	count := countText(a)
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, map[string]string{"s25": rpc.StateWorking, "s9": rpc.StateBlocked})})
	if got := countText(a); got != count {
		t.Errorf("a report moved the wheel's window: the count read %q, now %q", count, got)
	}
	drawn := drawnAgents(a)
	name := drawn[len(drawn)/2]
	a = clickVia(a, sidebarX(a), agentRowY(a, name))
	if got := a.fleet.agents[a.roster.Selected].Name; got != name {
		t.Fatalf("the click selected %q, want %s", got, name)
	}
	if !slices.Equal(drawnAgents(a), drawn) {
		t.Errorf("a click on %s moved the window: %v, then %v", name, drawn, drawnAgents(a))
	}
}

// headerDrawn is whether the sidebar draws a channel header's rule.
func headerDrawn(a App, header string) bool {
	for _, row := range sidebarRows(a)[:a.rosterHeight()] {
		if text := ansi.Strip(row); strings.Contains(text, "─") && strings.Contains(text, " "+header+" ") {
			return true
		}
	}
	return false
}

// selectedName is the agent the cursor is on or under, "" on a header.
func selectedName(a App) string { return a.fleet.agents[a.roster.Selected].Name }

// ⇧↑↓ move the window the least that shows the cursor: not at all while the
// cursor walks the rows already drawn.
func TestShiftArrowsMoveTheWindowTheLeastThatShowsTheCursor(t *testing.T) {
	a := wheelFleet(t)
	first := drawnAgents(a)
	last := first[len(first)-1]
	for range 3 * len(rankedNames(a)) { // bounded: headers and subagent rows are stops too
		a, _ = keyVia(a, tea.KeyShiftDown)
		if !slices.Equal(drawnAgents(a), first) {
			t.Fatalf("⇧↓ onto %+v, which was drawn, moved the window: %v, then %v", a.roster, first, drawnAgents(a))
		}
		if selectedName(a) == last {
			return
		}
	}
	t.Fatalf("⇧↓ never reached %s, the last agent drawn", last)
}

// Every stop ⇧↓ lands on, all the way round, is drawn.
func TestShiftDownKeepsEveryStopDrawn(t *testing.T) {
	a := wheelFleet(t)
	for range 3 * len(rankedNames(a)) {
		a, _ = keyVia(a, tea.KeyShiftDown)
		if h := a.roster.Header; h != "" && !headerDrawn(a, h) || h == "" && agentRowY(a, selectedName(a)) < 0 {
			t.Fatalf("⇧↓ put the cursor on %+v, which the sidebar does not draw: %v", a.roster, drawnAgents(a))
		}
	}
}

// The cursor moved past the window's last row is drawn as its last.
func TestShiftDownPastTheBottomScrollsByTheLeast(t *testing.T) {
	a := wheelFleet(t)
	last := drawnAgents(a)[len(drawnAgents(a))-1]
	for range 3 * len(rankedNames(a)) {
		if a, _ = keyVia(a, tea.KeyShiftDown); selectedName(a) == last {
			break
		}
	}
	for a, _ = keyVia(a, tea.KeyShiftDown); a.roster.Header != "" || selectedName(a) == last; {
		a, _ = keyVia(a, tea.KeyShiftDown) // past a header or a subagent row, onto the next agent
	}
	drawn, want := drawnAgents(a), selectedName(a)
	if drawn[len(drawn)-1] != want {
		t.Errorf("⇧↓ past the bottom drew %v, want %s as the last row", drawn, want)
	}
	for a, _ = keyVia(a, tea.KeyShiftUp); a.roster.Header != "" || selectedName(a) == want; {
		a, _ = keyVia(a, tea.KeyShiftUp) // past the header, onto the agent above: another item
	}
	if !slices.Equal(drawnAgents(a), drawn) {
		t.Errorf("⇧↑ to %s, drawn, moved the window: %v, then %v", selectedName(a), drawn, drawnAgents(a))
	}
}

// A selection the wheel scrolled out of view takes no key: ⌃C parks nothing, esc
// interrupts nothing, ↵ and ⌃D open nothing. ⇧↓ draws the next one, which does.
func TestASelectionScrolledOutOfViewTakesNoKey(t *testing.T) {
	a := wheelFleet(t)
	a, _ = keyVia(a, tea.KeyShiftDown)
	picked := a.roster.Selected
	name := a.fleet.agents[picked].Name
	recorderOf(t, a).taken(t)
	a = wheelSidebar(a, true, 1)
	if agentRowY(a, name) >= 0 {
		t.Fatalf("%s is still drawn after the wheel: %v", name, drawnAgents(a))
	}

	// App.key rather than Update: Update would add the notice's linger tick to
	// the command, and running it here would sleep.
	for _, k := range []tea.KeyType{tea.KeyCtrlC, tea.KeyEsc, tea.KeyEnter, tea.KeyCtrlD} {
		m, cmd, _ := a.key(tea.KeyMsg{Type: k})
		next := m.(App)
		for _, f := range writtenFrames(t, next, cmd) {
			if f.SessionID == picked {
				t.Errorf("%v on a selection out of view wrote %s for %s", k, f.Kind, name)
			}
		}
		if next.focus != "" {
			t.Errorf("%v on a selection out of view opened %q", k, next.focus)
		}
	}
	if _, ok := a.pickedAgent(); ok {
		t.Error("pickedAgent answers for a selection out of view")
	}

	a, _ = keyVia(a, tea.KeyShiftDown)
	got, ok := a.pickedAgent()
	if !ok || agentRowY(a, got.Name) < 0 {
		t.Errorf("⇧↓ from a selection out of view picked (%q, %v), drawn rows %v", got.Name, ok, drawnAgents(a))
	}
}

// A report that re-ranks a held selection out of the window makes it inert, and
// one that ranks it back in makes it count again - decided on the change, not
// per frame.
func TestAReRankDecidesWhetherTheWheelsSelectionCounts(t *testing.T) {
	a := wheelFleet(t)
	a, _ = keyVia(a, tea.KeyShiftDown)
	a, _ = keyVia(a, tea.KeyShiftDown) // the top block's second agent
	picked := a.roster.Selected
	a = wheelSidebar(a, true, 1) // now the first row drawn
	if _, ok := a.pickedAgent(); !ok {
		t.Fatalf("%s is drawn after one notch (%v) and does not count", picked, drawnAgents(a))
	}
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, map[string]string{picked: rpc.StateBlocked})})
	if _, ok := a.pickedAgent(); ok {
		t.Errorf("%s ranked first, above the wheel's window (%v), and still counts", picked, drawnAgents(a))
	}
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, nil)})
	if _, ok := a.pickedAgent(); !ok {
		t.Errorf("%s ranked back into the window (%v) and does not count", picked, drawnAgents(a))
	}
}

// The usage bar under the roster is part of the sidebar for the wheel, and stays
// put; the strip under the sidebar's column is not.
func TestTheWheelOverTheUsageBarScrollsTheRosterAndTheStripDoesNot(t *testing.T) {
	a := wheelFleet(t)
	st := wheelStatus(25, nil)
	st.SessionUsage = &rpc.SessionUsage{Percent: 47, Resets: usageResets}
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: st})
	before, bar := drawnAgents(a), usageBlock(a)

	a = wheelAt(a, tea.MouseButtonWheelDown, sidebarX(a), a.paneHeight()+0, 1) // the strip
	if !slices.Equal(drawnAgents(a), before) {
		t.Errorf("the wheel over the strip scrolled the roster: %v, then %v", before, drawnAgents(a))
	}
	a = wheelAt(a, tea.MouseButtonWheelDown, sidebarX(a), a.paneHeight()-2, 1) // the bar
	if after := drawnAgents(a); len(after) == 0 || after[0] != before[1] {
		t.Errorf("the wheel over the usage bar drew %v, want the roster one agent on from %v", after, before)
	}
	if !slices.Equal(usageBlock(a), bar) {
		t.Errorf("the wheel moved the usage bar: %q, then %q", bar, usageBlock(a))
	}
}

// ⌃X jumps to an agent that needs you even when the wheel scrolled it away.
func TestNextBlockedShowsAnAgentTheWheelScrolledAway(t *testing.T) {
	a := wheelSidebar(wheelFleet(t), true, 100) // the bottom: the top block is off screen
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, map[string]string{"s4": rpc.StateBlocked})})
	if agentRowY(a, "agent04") >= 0 {
		t.Fatalf("agent04 is drawn at the bottom of the list: %v", drawnAgents(a))
	}
	a, _ = keyVia(a, tea.KeyCtrlX)
	if a.roster.Selected != "s4" || agentRowY(a, "agent04") < 0 {
		t.Errorf("⌃X selected %q; drawn %v, want agent04 selected and drawn", a.roster.Selected, drawnAgents(a))
	}
	if _, ok := a.pickedAgent(); !ok {
		t.Error("the agent ⌃X jumped to does not count")
	}
}

// The count says which way the hidden agents are, on the one row it always had:
// at the sidebar's own width every fleet under a hundred says both ways, and a
// column too narrow for both says "+N more".
func TestTheCountSaysWhichWayTheHiddenAgentsAre(t *testing.T) {
	width := rosterWidth - dividerWidth
	for _, tc := range []struct {
		above, below int
		want         string
	}{
		{0, 5, "  +5 below"},
		{5, 0, "  +5 above"},
		{5, 2, "  +5 above · +2 below"},
	} {
		if got := countLine(tc.above, tc.below, width); got != tc.want {
			t.Errorf("%d above and %d below read %q, want %q", tc.above, tc.below, got, tc.want)
		}
	}
	for above := range 100 {
		for below := range 100 {
			if above+below == 0 {
				continue
			}
			line := countLine(above, below, width)
			if ansi.StringWidth(line) > width {
				t.Fatalf("%d above and %d below read %q, wider than the %d-cell sidebar", above, below, line, width)
			}
			if above > 0 && below > 0 && line == moreLine(above+below) {
				t.Fatalf("%d above and %d below fell back to %q at the sidebar's own width", above, below, line)
			}
		}
	}
	if got := countLine(15, 12, 16); got != moreLine(27) {
		t.Errorf("both ways in 16 cells read %q, want the fallback %q", got, moreLine(27))
	}
}

// Wherever the window stands - held or following the cursor, at any top and
// height - the column measures its rows, the count is the row under the window,
// and a click hits what is drawn.
func TestEveryWindowDrawsWhatAClickHits(t *testing.T) {
	agents := gapFleet(t,
		rpc.SessionStatus{ID: "s0", Name: "bea", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s3", Name: "john", Team: "backend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s4", Name: "mara", Team: "frontend", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s5", Name: "omar", Team: "frontend", State: rpc.StateIdle},
	)
	const width = 30
	for height := 1; height <= 14; height++ {
		for top := range len(agents) + 2 {
			for _, held := range []bool{true, false} {
				for _, sel := range []string{"", "s0", "s3", "s5"} {
					r := Roster{Selected: sel, view: rosterView{top: top, held: held}}
					out := r.View(agents, teamedSubs, width, height)
					measureColumn(t, out, width, height)
					lines := strings.Split(stripANSI(out), "\n")
					where := fmt.Sprintf("height %d, top %d, held %v, cursor %q", height, top, held, sel)
					for y, line := range lines {
						spot, ok := r.At(agents, teamedSubs, width, height, y)
						text := strings.TrimSpace(line)
						switch {
						case text == "" || isCountRow(line):
							if ok {
								t.Errorf("%s: row %d %q hits %+v", where, y, text, spot)
							}
						case strings.Contains(line, "─"):
							if !ok || spot.header == "" || !strings.Contains(line, " "+spot.header+" ") {
								t.Errorf("%s: header row %d %q hits %+v (%v)", where, y, text, spot, ok)
							}
						case !ok || !strings.Contains(line, spot.agent.Name) && !strings.HasPrefix(line, "  "):
							t.Errorf("%s: row %d %q hits %+v (%v)", where, y, text, spot, ok)
						}
					}
					// A window that reaches the fleet's end is never left with room for
					// the block before it: no blank rows under the last agent.
					rows := sectionRows(agents)
					h := itemHeights(rows, teamedSubs)
					w := r.window(rows, h, height)
					if used := h[w.from:w.to].total(); w.to == len(rows) && w.from > 0 && h.total() > height && used+h[w.from-1] <= height-1 {
						t.Errorf("%s: the window [%d,%d) ends the fleet with room for the block before it:\n%s", where, w.from, w.to, stripANSI(out))
					}
					// A window that follows the cursor draws it whenever its block fits
					// beside the count; a taller one is the count alone.
					if c := r.cursorRow(rows); !held && c >= 0 && rowsForRow(rows[c], teamedSubs) < height &&
						!strings.Contains(stripANSI(out), agents[c].Name+" ") {
						t.Errorf("%s: the window follows the cursor and does not draw %s:\n%s", where, agents[c].Name, stripANSI(out))
					}
				}
			}
		}
	}
}

// The pinned all is always drawn, so a cursor on it counts however the wheel
// holds the window; a team header the wheel scrolled away picks no channel.
func TestHeadersUnderTheWheel(t *testing.T) {
	a := wheelFleet(t)
	a, _ = keyVia(a, tea.KeyShiftDown)
	a, _ = keyVia(a, tea.KeyShiftUp) // onto the pinned all
	if a.roster.Header != core.BroadcastName {
		t.Fatalf("⇧↑ from the first agent is on %+v, not all", a.roster)
	}
	a = wheelSidebar(a, true, 100)
	if _, ok := a.pickedHeader(); !ok {
		t.Error("the cursor on the pinned all does not count once the wheel holds the window")
	}

	a = wheelFleet(t)
	for range 3 * len(rankedNames(a)) {
		if a, _ = keyVia(a, tea.KeyShiftDown); a.roster.Header == "infra" {
			break
		}
	}
	if _, ok := a.pickedHeader(); !ok {
		t.Fatalf("⇧↓ onto the infra header (%+v) does not count", a.roster)
	}
	a = wheelSidebar(a, false, 100) // the top: the last team's header is below the window
	if _, ok := a.pickedHeader(); ok {
		t.Fatalf("the infra header the wheel scrolled away (%v) still counts", drawnAgents(a))
	}
	// Nor does ⌃D or ⌃F open or fork an agent in its place.
	for _, k := range []tea.KeyType{tea.KeyCtrlD, tea.KeyCtrlF} {
		m, cmd, _ := a.key(tea.KeyMsg{Type: k})
		if next := m.(App); next.focus != "" || len(writtenFrames(t, next, cmd)) > 0 {
			t.Errorf("%v on a header out of view acted: focus %q", k, next.focus)
		}
	}
	m, _, _ := a.key(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.(App).room.channel; got != "" {
		t.Errorf("↵ on a header scrolled out of view picked the %q channel", got)
	}
}

// A roster that comes to fit lets go of the wheel, so when it overflows again the
// window follows the cursor rather than a hold nobody can see.
func TestARosterThatFitsLetsGoOfTheWheel(t *testing.T) {
	a := wheelFleet(t)
	a, _ = keyVia(a, tea.KeyShiftDown)
	name := selectedName(a)
	a = wheelSidebar(a, true, 3)
	if _, ok := a.pickedAgent(); ok {
		t.Fatalf("%s is still drawn after three notches: %v", name, drawnAgents(a))
	}
	ended := map[string]string{}
	for i := 9; i <= 25; i++ {
		ended[fmt.Sprintf("s%d", i)] = rpc.StateEnded
	}
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, ended)})
	if c := countText(a); c != "" {
		t.Fatalf("eight agents still overflow the sidebar: %q", c)
	}
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, nil)})
	if _, ok := a.pickedAgent(); !ok || agentRowY(a, name) < 0 {
		t.Errorf("after the roster fitted and overflowed again, %s (picked %v) is not drawn: %v", name, ok, drawnAgents(a))
	}
}

// A shorter terminal decides again whether the wheel's selection is drawn.
func TestAResizeDecidesWhetherTheWheelsSelectionCounts(t *testing.T) {
	a := wheelFleet(t) // the room holds the keys: ⇧↓ opens no conversation
	drawn := drawnAgents(a)
	name := drawn[len(drawn)-1]
	for range 3 * len(rankedNames(a)) {
		if a, _ = keyVia(a, tea.KeyShiftDown); selectedName(a) == name {
			break
		}
	}
	a = wheelSidebar(a, true, 1)
	a = wheelSidebar(a, false, 1) // held at the top, the cursor on its last row
	if _, ok := a.pickedAgent(); !ok || a.focus != "" {
		t.Fatalf("%s, on the window's last row, does not count: %v", name, drawnAgents(a))
	}
	if a = a.withSize(100, 24); agentRowY(a, name) >= 0 {
		t.Fatalf("a shorter terminal still draws %s: %v", name, drawnAgents(a))
	}
	if _, ok := a.pickedAgent(); ok {
		t.Errorf("a shorter terminal cut %s off the wheel's window (%v) and it still counts", name, drawnAgents(a))
	}
}

// ⌃X onto the agent already selected is still a jump to be seen: it lets go of
// the wheel's hold though the selection did not change.
func TestNextBlockedOntoTheSelectionTheWheelHidShowsIt(t *testing.T) {
	a := wheelFleet(t)
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, map[string]string{"s4": rpc.StateBlocked})})
	a, _ = keyVia(a, tea.KeyCtrlX)
	a = wheelSidebar(a, true, 100)
	if a.roster.Selected != "s4" || agentRowY(a, "agent04") >= 0 {
		t.Fatalf("want agent04 selected and scrolled away; selected %q, drawn %v", a.roster.Selected, drawnAgents(a))
	}
	a, _ = keyVia(a, tea.KeyCtrlX)
	if agentRowY(a, "agent04") < 0 {
		t.Errorf("⌃X onto agent04, already selected, left it scrolled away: %v", drawnAgents(a))
	}
}

// A conversation holding the keys is on screen as its pane, so ⌃F forks it however
// the wheel scrolled its sidebar row away.
func TestForkInAConversationWhoseRowTheWheelHid(t *testing.T) {
	a := wheelFleet(t)
	a = clickVia(a, sidebarX(a), agentRowY(a, "agent04"))
	if a.focus != "s4" {
		t.Fatalf("a click on agent04 focused %q", a.focus)
	}
	recorderOf(t, a).taken(t)
	a = wheelSidebar(a, true, 100)
	if agentRowY(a, "agent04") >= 0 {
		t.Fatalf("agent04 is still drawn: %v", drawnAgents(a))
	}
	m, cmd, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlF})
	var forked []string
	for _, f := range writtenFrames(t, m.(App), cmd) {
		if f.Kind == rpc.FrameFork {
			forked = append(forked, f.ParentID)
		}
	}
	if !slices.Equal(forked, []string{"s4"}) {
		t.Errorf("⌃F in agent04's conversation forked %v, want s4", forked)
	}
}

// After a key moved the window, a re-rank that keeps the cursor on screen moves
// nothing: the window is stored where it was drawn after the move.
func TestAReRankKeepsTheWindowAKeyMoved(t *testing.T) {
	a := wheelFleet(t)
	for _, k := range []tea.KeyType{tea.KeyShiftDown, tea.KeyShiftUp, tea.KeyShiftUp} { // wraps to the last agent
		a, _ = keyVia(a, k)
	}
	last := rankedNames(a)[len(rankedNames(a))-1]
	if selectedName(a) != last {
		t.Fatalf("⇧↓⇧↑⇧↑ is on %q, want the wrap to %s", selectedName(a), last)
	}
	drawn := drawnAgents(a)
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, map[string]string{a.roster.Selected: rpc.StateBlocked})})
	if got := drawnAgents(a); agentRowY(a, last) < 0 || got[0] != drawn[0] {
		t.Errorf("a re-rank inside the window moved it: %v, then %v", drawn, got)
	}
}

// The room's menus only offer, so the wheel hiding the selection leaves them on
// the agent the cursor names.
func TestTheWheelLeavesTheRoomsMenusOnTheSelection(t *testing.T) {
	a := wheelFleet(t)
	a, _ = keyVia(a, tea.KeyShiftDown)
	picked := a.roster.Selected
	a = wheelSidebar(a, true, 1)
	if got := a.completionAgent().ID; got != picked {
		t.Errorf("with the selection scrolled away the room's menus read %q, want %s", got, picked)
	}
}

// A key refused for a selection out of view says so, not that nothing is selected.
func TestARefusalForAHiddenSelectionSaysWhy(t *testing.T) {
	for _, k := range []tea.KeyType{tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyCtrlF} {
		a := wheelFleet(t)
		a, _ = keyVia(a, tea.KeyShiftDown)
		a = wheelSidebar(a, true, 1)
		notice.Reset()
		a.key(tea.KeyMsg{Type: k})
		if n, ok := notice.Latest(); !ok || n.Text != pickOutOfView {
			t.Errorf("%v on a selection out of view said %q, want %q", k, n.Text, pickOutOfView)
		}
	}
}

// A re-rank that pulled the window to keep the cursor drawn leaves it there for a
// click: the click starts from where the window was drawn, not where it was left.
func TestAClickAfterAReRankPulledTheWindowMovesNothing(t *testing.T) {
	a := wheelFleet(t)
	drawn := drawnAgents(a)
	last := drawn[len(drawn)-1]
	for range 3 * len(rankedNames(a)) {
		if a, _ = keyVia(a, tea.KeyShiftDown); selectedName(a) == last {
			break
		}
	}
	moved := wheelStatus(25, nil)
	for i, s := range moved.Sessions {
		if s.Name == last {
			moved.Sessions[i].Team = "infra" // the cursor's agent goes below the window
		}
	}
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: moved})
	pulled := drawnAgents(a)
	if pulled[0] == drawn[0] || agentRowY(a, last) < 0 {
		t.Fatalf("moving %s below the window did not pull it: %v, then %v", last, drawn, pulled)
	}
	a = clickVia(a, sidebarX(a), agentRowY(a, pulled[1]))
	if got := drawnAgents(a); !slices.Equal(got, pulled) {
		t.Errorf("a click on %s moved the window: %v, then %v", pulled[1], pulled, got)
	}
}

// The wheel over a roster that fits moves nothing, not even where the window will
// open when the fleet next overflows.
func TestTheWheelOverAFittingRosterKeepsWhereTheWindowStood(t *testing.T) {
	a := wheelSidebar(wheelFleet(t), true, 100)
	a = clickVia(a, sidebarX(a), agentRowY(a, drawnAgents(a)[1]))
	before := drawnAgents(a)
	ended := map[string]string{}
	for i := 9; i <= 25; i++ {
		ended[fmt.Sprintf("s%d", i)] = rpc.StateEnded
	}
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, ended)})
	a = wheelSidebar(a, true, 1)
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, nil)})
	if got := drawnAgents(a); !slices.Equal(got, before) {
		t.Errorf("a wheel over the fitting roster moved where it reopens: %v, then %v", before, got)
	}
}

// The first notch can leave the window where it was - at the top, a notch up -
// and the selection it draws still counts.
func TestAFirstNotchThatMovesNothingKeepsTheSelectionCounting(t *testing.T) {
	a := wheelFleet(t)
	a, _ = keyVia(a, tea.KeyShiftDown)
	a = wheelSidebar(a, false, 1)
	if _, ok := a.pickedAgent(); !ok {
		t.Errorf("%s is drawn at the top after a notch up there and does not count", selectedName(a))
	}
}

// A click onto a subagent row is a selection change, though the agent is the same.
func TestAClickOntoASubagentRowLetsGoOfTheWheel(t *testing.T) {
	a := wheelFleet(t)
	a = clickVia(a, sidebarX(a), agentRowY(a, "agent02"))
	a = clickVia(a, 2, 2)
	a = wheelSidebar(a, true, 1)
	if !a.rosterView.held || agentRowY(a, "agent02") < 0 {
		t.Fatalf("want agent02 drawn under the wheel's hold: view %+v, drawn %v", a.rosterView, drawnAgents(a))
	}
	a = clickVia(a, sidebarX(a), agentRowY(a, "agent02")+1) // its subagent's row
	if a.roster.SelectedTask == "" || a.rosterView.held {
		t.Errorf("a click on agent02's subagent row left %+v, view %+v", a.roster, a.rosterView)
	}
}

// The sidebar's last row is the sidebar's too: with no usage bar it is the count.
func TestTheWheelOverTheSidebarsLastRowScrollsIt(t *testing.T) {
	a := wheelFleet(t)
	before := drawnAgents(a)
	a = wheelAt(a, tea.MouseButtonWheelDown, sidebarX(a), a.paneHeight()-1, 1)
	if after := drawnAgents(a); len(after) == 0 || after[0] != before[1] {
		t.Errorf("the wheel over the sidebar's last row drew %v, want one agent on from %v", after, before)
	}
}

// With nothing selected there is no selection to hide: the keys fall back as they
// always did, and nothing says a selection is out of view.
func TestTheWheelWithNothingSelectedHidesNoSelection(t *testing.T) {
	a := wheelSidebar(wheelFleet(t), true, 3)
	if a.roster != (Roster{}) || !a.rosterView.held {
		t.Fatalf("want nothing selected under the wheel's hold: %+v, %+v", a.roster, a.rosterView)
	}
	notice.Reset()
	m, _, _ := a.key(tea.KeyMsg{Type: tea.KeyCtrlC})
	if n, _ := notice.Latest(); n.Text == pickOutOfView {
		t.Errorf("⌃C with nothing selected said %q", n.Text)
	}
	first, _ := m.(App).cursorAgent()
	m, _, _ = a.key(tea.KeyMsg{Type: tea.KeyCtrlD})
	if got := m.(App).focus; got != first.ID {
		t.Errorf("⌃D with nothing selected opened %q, want the first agent %s, as without the wheel", got, first.ID)
	}
}

// A subagent row out of view takes no key, even in its own agent's conversation:
// the conversation is on screen, the row the cursor names is not.
func TestASubagentRowOutOfViewTakesNoKeyInItsConversation(t *testing.T) {
	a := viaUpdate(wheelFleet(t), taskFrame("s4", started("t4", "toolu_4", "Check the docs", "general-purpose", core.TaskAgent)))
	a = clickVia(a, sidebarX(a), agentRowY(a, "agent04")+1) // agent04's subagent row
	if a.focus != "s4" || a.roster.SelectedTask == "" {
		t.Fatalf("a click on agent04's subagent row left focus %q, %+v", a.focus, a.roster)
	}
	a = wheelSidebar(a, true, 100)
	notice.Reset()
	a.key(tea.KeyMsg{Type: tea.KeyCtrlD})
	if n, _ := notice.Latest(); n.Text != pickOutOfView {
		t.Errorf("⌃D on agent04's subagent row, out of view, said %q, want %q", n.Text, pickOutOfView)
	}
}

// ⌃X onto the selection it left keeps the window where it drew it, so a re-rank
// that keeps the cursor on screen moves nothing afterwards.
func TestNextBlockedOntoTheSameSelectionKeepsItsWindow(t *testing.T) {
	a := wheelFleet(t)
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, map[string]string{"s4": rpc.StateBlocked})})
	a, _ = keyVia(a, tea.KeyCtrlX)
	a = wheelSidebar(a, true, 100)
	a, _ = keyVia(a, tea.KeyCtrlX)
	drawn := drawnAgents(a)
	if drawn[0] != "agent04" {
		t.Fatalf("⌃X onto agent04 drew %v, want it first", drawn)
	}
	// agent08 ranks above agent04 now, one row down: the window keeps its top, so
	// both are drawn.
	a = viaUpdate(a, rpc.Frame{Kind: rpc.FrameStatusPush, Status: wheelStatus(25, map[string]string{"s4": rpc.StateWorking, "s8": rpc.StateBlocked})})
	if got := drawnAgents(a); len(got) < 2 || got[0] != "agent08" || got[1] != "agent04" {
		t.Errorf("a re-rank that kept agent04 drawn moved the window: %v, then %v", drawn, got)
	}
}
