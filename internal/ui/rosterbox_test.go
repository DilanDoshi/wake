package ui

// Each roster section is a box: the team's header is its top edge, the members
// stand between walls, and a closing row sits under the last one. The loose agents
// under the pinned `all` are one too.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// boxedWidth is the columns the sidebar draws its rows in.
const boxedWidth = rosterWidth - dividerWidth

// boxFleet is two loose agents over three teams, so a last row is a team's and the
// loose block's alike.
func boxFleet(t *testing.T) []Agent {
	t.Helper()
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams: []string{"backend", "frontend", "qa"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "bea", State: rpc.StateIdle},
			{ID: "s2", Name: "alex", State: rpc.StateIdle},
			{ID: "s3", Name: "thea", Team: "backend", State: rpc.StateIdle},
			{ID: "s4", Name: "john", Team: "backend", State: rpc.StateIdle},
			{ID: "s5", Name: "mara", Team: "frontend", State: rpc.StateIdle},
			{ID: "s6", Name: "omar", Team: "frontend", State: rpc.StateIdle},
			{ID: "s7", Name: "lena", Team: "frontend", State: rpc.StateIdle},
			{ID: "s8", Name: "ruth", Team: "qa", State: rpc.StateIdle},
		},
	})
	return f.sectioned(f.OnRoster())
}

func isWallRow(line string) bool {
	return strings.HasPrefix(line, "│ ") && strings.HasSuffix(line, " │")
}
func isFooter(line string) bool {
	return strings.HasPrefix(line, "╰") && strings.HasSuffix(line, "╯")
}
func isTopEdge(line string) bool {
	return strings.HasPrefix(line, "╭") && strings.HasSuffix(line, "╮")
}

// A section is its top edge, its members between walls and its closing row, every
// one of them exactly as wide as the column, and a gap between sections.
func TestEverySectionIsABoxAtTheSidebarWidth(t *testing.T) {
	lines := rosterLines(boxFleet(t), "", noSubsOf, boxedWidth, 40)

	want := []struct {
		kind func(string) bool
		text string
	}{
		{isWallRow, "bea"}, {isWallRow, "alex"}, {isFooter, ""}, {nil, ""},
		{isTopEdge, "backend"}, {isWallRow, "thea"}, {isWallRow, "john"}, {isFooter, ""}, {nil, ""},
		{isTopEdge, "frontend"}, {isWallRow, "mara"}, {isWallRow, "omar"}, {isWallRow, "lena"}, {isFooter, ""}, {nil, ""},
		{isTopEdge, "qa"}, {isWallRow, "ruth"}, {isFooter, ""},
	}
	if len(lines) < len(want) {
		t.Fatalf("the roster drew %d rows, want at least %d:\n%s", len(lines), len(want), strings.Join(lines, "\n"))
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
		case ansi.StringWidth(line) != boxedWidth:
			t.Errorf("row %d is %d columns wide, want the column's %d: %q", i, ansi.StringWidth(line), boxedWidth, line)
		}
	}
}

// A team's name sits in the middle of its top edge.
func TestATeamNameIsCentredInItsTopEdge(t *testing.T) {
	lines := rosterLines(boxFleet(t), "", noSubsOf, boxedWidth, 40)
	edge := strings.TrimRight(lines[rowContaining(lines, "frontend")], " ")
	if !isTopEdge(edge) {
		t.Fatalf("frontend's header %q is not a top edge", edge)
	}

	left, right, _ := strings.Cut(strings.Trim(edge, "╭╮"), " frontend ")
	if d := strings.Count(left, "─") - strings.Count(right, "─"); d < -1 || d > 1 {
		t.Errorf("the edge %q has %d rules before the name and %d after", edge, strings.Count(left, "─"), strings.Count(right, "─"))
	}
}

// A name wider than the edge holds is cut with an ellipsis and the edge stays
// the column wide, as the divider's was.
func TestATeamNameWiderThanTheEdgeIsCutWithAnEllipsis(t *testing.T) {
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams:    []string{"a-team-name-too-long-for-a-column"},
		Sessions: []rpc.SessionStatus{{ID: "s1", Name: "thea", Team: "a-team-name-too-long-for-a-column", State: rpc.StateIdle}},
	})
	lines := rosterLines(f.sectioned(f.OnRoster()), "", noSubsOf, boxedWidth, 10)

	edge := lines[rowContaining(lines, "…")]
	if !isTopEdge(edge) || ansi.StringWidth(edge) != boxedWidth {
		t.Errorf("the cut edge is %q (%d columns), want a top edge %d wide", edge, ansi.StringWidth(edge), boxedWidth)
	}
}

// With no team at all the roster is the one box under `all`.
func TestAFleetWithNoTeamsIsOneBoxUnderAll(t *testing.T) {
	var sessions []rpc.SessionStatus
	for _, name := range []string{"alex", "thea", "john"} {
		sessions = append(sessions, rpc.SessionStatus{ID: "s-" + name, Name: name, State: rpc.StateIdle})
	}
	lines := rosterLines(gapFleet(t, sessions...), "", noSubsOf, boxedWidth, 10)

	for i, want := range []string{"alex", "thea", "john"} {
		if !isWallRow(strings.TrimRight(lines[i], " ")) || !strings.Contains(lines[i], want) {
			t.Errorf("row %d is %q, want %s between walls:\n%s", i, lines[i], want, strings.Join(lines, "\n"))
		}
	}
	if !isFooter(lines[3]) {
		t.Errorf("row 3 is %q, want the box closed under its last member:\n%s", lines[3], strings.Join(lines, "\n"))
	}
}

// What each item draws is what rowsForRow says: the footer is a row of the last
// member's block, and only of the last.
func TestRowsForRowCountsTheFooterOfALastMemberOnly(t *testing.T) {
	// alex ends the loose run, thea opens backend, john ends it, mara is frontend's
	// only member: row + footer, then gap + header + row, row + footer, and gap +
	// header + row + footer.
	want := map[string]int{"alex": 2, "thea": 3, "john": 2, "mara": 4}
	f := (Fleet{}).WithStatus(&rpc.Status{
		Teams: []string{"backend", "frontend"},
		Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", State: rpc.StateIdle},
			{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
			{ID: "s3", Name: "john", Team: "backend", State: rpc.StateIdle},
			{ID: "s4", Name: "mara", Team: "frontend", State: rpc.StateIdle},
		},
	})
	for _, item := range sectionRows(f.sectioned(f.OnRoster())) {
		if got := rowsForRow(item, noSubsOf); got != want[item.agent.Name] {
			t.Errorf("%s: rowsForRow = %d, want %d", item.agent.Name, got, want[item.agent.Name])
		}
	}
}

// A click on the closing row opens nothing, and the rows above it - a member and
// the subagent under it - still resolve to that member.
func TestAClickOnAFooterHitsNothing(t *testing.T) {
	agents := boxFleet(t)
	subs := func(id string) []Task {
		if id == "s4" { // john closes backend, with a running subagent under him
			return []Task{{ID: "t1", Dispatch: "d1", Kind: core.TaskAgent, Name: "reviewer", Status: core.TaskRunning}}
		}
		return nil
	}
	const height = 40
	lines := rosterLines(agents, "", subs, boxedWidth, height)

	footers, subRows := 0, 0
	for y, line := range lines {
		spot, ok := (Roster{}).At(agents, subs, boxedWidth, height, y)
		switch {
		case isFooter(line):
			footers++
			if ok {
				t.Errorf("the closing row %d resolves to %+v; it belongs to no one", y, spot)
			}
		case strings.Contains(line, "reviewer"):
			subRows++
			if !ok || spot.agent.Name != "john" || spot.dispatch != "d1" {
				t.Errorf("the subagent row %d resolves to (%q, %q, %v), want john's dispatch d1", y, spot.agent.Name, spot.dispatch, ok)
			}
		}
	}
	if footers != 4 || subRows != 1 {
		t.Errorf("drew %d closing rows and %d subagent rows, want 4 (the loose agents and three teams) and 1:\n%s",
			footers, subRows, strings.Join(lines, "\n"))
	}
}

// A column too narrow for the walls draws what fits: every line an item draws is
// no wider than the column and still exactly the rows rowsForRow counts. The
// sidebar's lipgloss width would wrap a wider line onto a row of its own, which
// the window never counted.
func TestABoxNeverOutgrowsAColumnTooNarrowForItsWalls(t *testing.T) {
	for width := 1; width <= 8; width++ {
		for _, item := range sectionRows(boxFleet(t)) {
			lines := (Roster{}).rowLines(item, noSubsOf, width)
			if len(lines) != rowsForRow(item, noSubsOf) {
				t.Errorf("width %d, %s: %d lines drawn, rowsForRow says %d", width, item.agent.Name, len(lines), rowsForRow(item, noSubsOf))
			}
			for _, line := range lines {
				if got := ansi.StringWidth(line); got > width {
					t.Errorf("width %d, %s: draws %q, %d columns wide", width, item.agent.Name, stripANSI(line), got)
				}
			}
		}
	}
}

// A window keeps a section's last member with its closing row, at every height
// and cursor: an item is whole or absent.
func TestAWindowNeverLeavesALastMemberWithoutItsFooter(t *testing.T) {
	agents := boxFleet(t)
	lastOfRun := map[string]bool{"alex": true, "john": true, "lena": true, "ruth": true}
	closing := 0 // a sweep that never meets a closing member proves nothing
	defer func() {
		if closing == 0 {
			t.Error("no section's last member was ever drawn between walls, so nothing was checked")
		}
	}()
	for height := 3; height <= 24; height++ {
		for _, a := range agents {
			lines := rosterLines(agents, a.ID, noSubsOf, boxedWidth, height)
			for y, line := range lines {
				for name := range lastOfRun {
					if !isWallRow(strings.TrimRight(line, " ")) || !strings.Contains(line, name) {
						continue
					}
					closing++
					if y+1 >= len(lines) || !isFooter(lines[y+1]) {
						t.Errorf("height %d, cursor %s: %s closes a box but the next row is not its footer:\n%s",
							height, a.Name, name, strings.Join(lines, "\n"))
					}
				}
			}
		}
	}
}

// Scrolled to the end, the roster ends on its last box's closing row, then the
// count of what is above.
func TestTheLastWindowEndsOnTheFooter(t *testing.T) {
	agents := boxFleet(t)
	r := Roster{view: rosterView{top: len(agents), held: true}} // past the end: clamped to the last window
	lines := strings.Split(stripANSI(r.View(agents, noSubsOf, boxedWidth, 6)), "\n")

	count := rowContaining(lines, "above")
	if count < 1 || !isFooter(strings.TrimRight(lines[count-1], " ")) || strings.Contains(lines[count], "below") {
		t.Errorf("the last window is not the last footer then the count of what is above:\n%s", strings.Join(lines, "\n"))
	}
}

// The pinned `all` is the loose box's top edge, one row, and a click on it picks
// the channel.
func TestThePinnedAllIsTheLooseBoxTopEdge(t *testing.T) {
	agents := boxFleet(t)
	out := strings.Split(stripANSI((Roster{}).Column(agents, noSubsOf, boxedWidth, 20, "")), "\n")

	if !isTopEdge(strings.TrimRight(out[0], " ")) || !strings.Contains(out[0], " all ") || ansi.StringWidth(strings.TrimRight(out[0], " ")) != boxedWidth {
		t.Errorf("the pinned row is %q, want `all` in a top edge %d wide", out[0], boxedWidth)
	}
	if !isWallRow(strings.TrimRight(out[1], " ")) || !strings.Contains(out[1], "bea") {
		t.Errorf("row 1 is %q, want the first loose agent between walls", out[1])
	}
	if spot, ok := (Roster{}).Hit(agents, noSubsOf, boxedWidth, 20, 0); !ok || spot.header != core.BroadcastName {
		t.Errorf("a click on the pinned row resolves to (%+v, %v), want the all channel", spot, ok)
	}
	if spot, ok := (Roster{}).Hit(agents, noSubsOf, boxedWidth, 20, 1); !ok || spot.agent.Name != "bea" {
		t.Errorf("a click on row 1 resolves to (%+v, %v), want bea", spot, ok)
	}
}

// The box wears its channel's colour - the accent while it is picked, muted
// otherwise - in its walls and closing row as in its edge. Every box is held to
// its own state, with each channel picked in turn, so a box that ignored the pick
// (or took another's) shows against the one beside it.
func TestTheBoxWearsItsChannelsState(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor, so the hues render apart
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	agents := boxFleet(t)
	rule := "╰" + strings.Repeat("─", boxedWidth-2) + "╯"
	boxes := []string{"", "backend", "frontend", "qa"}  // top to bottom: all's box, then each team's
	members := []string{"alex", "john", "omar", "ruth"} // one in each

	for _, picked := range boxes {
		out := (Roster{}).Column(agents, noSubsOf, boxedWidth, 40, picked)
		lines, plain := strings.Split(out, "\n"), strings.Split(stripANSI(out), "\n")
		var closing []string
		for i, l := range plain {
			if isFooter(strings.TrimRight(l, " ")) {
				closing = append(closing, lines[i])
			}
		}
		if len(closing) != len(boxes) {
			t.Fatalf("channel %q: %d closing rows, want %d:\n%s", picked, len(closing), len(boxes), strings.Join(plain, "\n"))
		}
		for i, box := range boxes {
			want := HintStyle
			if box == picked {
				want = AccentStyle
			}
			if !strings.Contains(closing[i], want.Render(rule)) {
				t.Errorf("channel %q: box %d's closing row is not drawn in %v", picked, i, want.GetForeground())
			}
			wall := lines[rowContaining(plain, members[i])]
			if !strings.Contains(wall, want.Render("│ ")) {
				t.Errorf("channel %q: %s's left wall is not drawn in %v", picked, members[i], want.GetForeground())
			}
			if !strings.Contains(wall, want.Render(" │")) {
				t.Errorf("channel %q: %s's right wall is not drawn in %v", picked, members[i], want.GetForeground())
			}
		}
	}
}

// The cursor on a header brightens and bolds its name and nothing else: the box
// around it keeps the channel's own colour, and so does its closing row.
func TestTheCursorOnAHeaderBoldsTheNameAndNotTheBox(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(0) // termenv.TrueColor, so bold and bright render apart
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	agents := boxFleet(t)
	rule := "╰" + strings.Repeat("─", boxedWidth-2) + "╯"

	out := (Roster{Header: "frontend"}).Column(agents, noSubsOf, boxedWidth, 40, "backend")
	lines, plain := strings.Split(out, "\n"), strings.Split(stripANSI(out), "\n")

	edge := lines[rowContaining(plain, " frontend ")]
	if want := centredEdge(boxBorder.TopLeft, boxBorder.TopRight, "frontend", boxedWidth, HintStyle, TextStyle.Bold(true)); edge != want {
		t.Errorf("the cursored edge is %q, want %q", edge, want)
	}
	member := lines[rowContaining(plain, "omar")]
	if !strings.Contains(member, HintStyle.Render("│ ")) || !strings.Contains(member, HintStyle.Render(" │")) {
		t.Errorf("the cursor on frontend's header changed its members' walls: %q", member)
	}
	closing := lines[rowContaining(plain, "lena")+1]
	if !strings.Contains(closing, HintStyle.Render(rule)) {
		t.Errorf("the cursor on frontend's header changed its closing row: %q", closing)
	}
}

// A row's figures need the room the box leaves: a workflow's done/started count and
// a subagent's token figure are dropped whole at 19 columns where they fitted 23,
// and kept where the pair still fits. Drawn through the column, at its real width.
func TestARowKeepsAFigureOnlyWhereItFitsInsideTheWalls(t *testing.T) {
	for _, tc := range []struct {
		name   string
		task   Task
		figure string
		kept   bool
	}{
		{"a short workflow", workflowTask("toolu_1", "tally", 2, 3), "2/3", true},
		{"a long workflow", workflowTask("toolu_1", "dependency-audit", 2, 3), "2/", false},
		{"a short subagent type", running("toolu_1", "reviewer", 1100), "1.1k", true},
		{"a long subagent type", running("toolu_1", "code-reviewer", 1100), "1.", false},
	} {
		agents, subs := alexWith(tc.task)
		out := stripANSI(Roster{}.View(agents, subs, boxedWidth, 6))
		if got := strings.Contains(out, tc.figure); got != tc.kept {
			t.Errorf("%s: figure %q drawn %v, want %v:\n%s", tc.name, tc.figure, got, tc.kept, out)
		}
	}
}

// A name too long for the box is cut at the right wall and the unread badge after
// it survives, as it did at the column's edge.
func TestALongNameIsCutInsideTheWallsWithItsBadgeKept(t *testing.T) {
	agents := []Agent{{ID: "s1", Name: strings.Repeat("n", 30), State: rpc.StateIdle, Unread: 7}}
	lines := rosterLines(agents, "", noSubsOf, boxedWidth, 6)

	row := strings.TrimRight(lines[0], " ")
	if !isWallRow(row) || !strings.HasSuffix(row, "7 │") || ansi.StringWidth(row) != boxedWidth {
		t.Errorf("the long name's row is %q (%d columns), want it between walls ending in its badge", row, ansi.StringWidth(row))
	}
}

// The box gives a row 19 columns, so a token count that fitted the old 23 is
// dropped whole where it no longer does.
func TestATokenCountDropsWhenTheBoxLeavesNoRoomForIt(t *testing.T) {
	// "○ " + 12 + " ↓ 550" is 20 columns: it fits 23, not 19.
	agents := []Agent{{ID: "s1", Name: "abcdefghijkl", State: rpc.StateWorking, TurnTokens: 550}}

	if row := rosterLines(agents, "", noSubsOf, boxedWidth, 6)[0]; strings.Contains(row, tokenArrow) {
		t.Errorf("the token count is drawn where the row has no room for it: %q", row)
	}
	short := []Agent{{ID: "s1", Name: "sydney", State: rpc.StateWorking, TurnTokens: 550}}
	if row := rosterLines(short, "", noSubsOf, boxedWidth, 6)[0]; !strings.Contains(row, tokenArrow+" 550") {
		t.Errorf("the token count is missing from a row with room for it: %q", row)
	}
}

// inBox is a drawn row without its walls: what a section's member says.
func inBox(row string) string {
	row = strings.TrimPrefix(strings.TrimRight(row, " "), "│ ")
	return strings.TrimRight(strings.TrimSuffix(row, "│"), " ")
}

// rowStyle is the style a drawn row's own text wears, past its walls' styling.
func rowStyle(line string) string {
	_, after, _ := strings.Cut(line, "│ ") // the left wall
	for _, esc := range ansiPattern.FindAllString(after, -1) {
		if esc != "\x1b[0m" {
			return esc
		}
	}
	return ""
}

// A team tag that reached the header despite the daemon's fence must not make the
// top edge more than one row, or rowsForRow's count and every click below it shift.
func TestTheTopEdgeOfABoxIsAlwaysOneRow(t *testing.T) {
	if got := (Roster{}).headerLine("back\nend\rmore", 40); strings.ContainsAny(got, "\n\r") {
		t.Errorf("the top edge kept a line break: %q", got)
	}
}

// The board's divider is not a box: only the sidebar changed shape. Asked of the
// band the board's two views draw, not of the rule drawer under it.
func TestTheBoardKeepsItsPlainDivider(t *testing.T) {
	band := teamHeaderBand("backend", 30)
	if len(band) != teamHeaderRows {
		t.Fatalf("the board's header band is %d rows, want %d", len(band), teamHeaderRows)
	}
	got := stripANSI(band[teamHeaderRows-1])
	if strings.ContainsAny(got, "╭╮╰╯│") || !strings.HasPrefix(got, "─") || !strings.HasSuffix(got, "─") {
		t.Errorf("the board's team divider is %q, want the plain rule", got)
	}
}
