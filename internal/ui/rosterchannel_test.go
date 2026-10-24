package ui

// The roster's channel headers: a pinned `── all ──` over every agent, and each
// team header a place the cursor can stop, picked by ↵ or a click.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func columnLines(r Roster, agents []Agent, width, height int, channel string) []string {
	return strings.Split(stripANSI(r.Column(agents, noSubsOf, width, height, channel)), "\n")
}

// isRule is whether a drawn row is a channel header naming name.
func isRule(line, name string) bool {
	return strings.Contains(line, "─ "+name+" ─")
}

// The sidebar always opens on `all`, above the manager: with no team, with
// teams, and with nobody, at exactly the column it was given.
func TestTheRosterAlwaysOpensOnAllAboveTheManager(t *testing.T) {
	manager := rpc.SessionStatus{ID: "m", Name: core.ManagerName, State: rpc.StateIdle}
	for name, sessions := range map[string][]rpc.SessionStatus{
		"no teams": {manager, {ID: "s1", Name: "iris", State: rpc.StateIdle}},
		"teams":    append([]rpc.SessionStatus{manager}, gapSessions()...),
		"nobody":   nil,
	} {
		agents := gapFleet(t, sessions...)
		const width, height = 30, 12
		out := Roster{}.Column(agents, noSubsOf, width, height, "")
		measureColumn(t, out, width, height)
		lines := strings.Split(stripANSI(out), "\n")
		if !isRule(lines[0], core.BroadcastName) {
			t.Errorf("%s: row 0 is %q, want the all header", name, lines[0])
		}
		if len(agents) > 0 && !strings.Contains(lines[1], core.ManagerName) {
			t.Errorf("%s: row 1 is %q, want the manager right under all", name, lines[1])
		}
	}
	if got := (Roster{}).Column(nil, noSubsOf, 30, 1, ""); !isRule(stripANSI(got), core.BroadcastName) || strings.Contains(got, "\n") {
		t.Errorf("a one-row column drew %q, want the all header alone", got)
	}
	if got := (Roster{}).Column(nil, noSubsOf, 30, 0, ""); got != "" {
		t.Errorf("a zero-row column drew %q", got)
	}
}

// ⇧↑↓ walk the rows in the order they are drawn, headers included, and wrap
// through all. A cursor on nothing - or on a header that has gone - still lands
// on the first agent, as it always did.
func TestTheCursorWalksOntoChannelHeaders(t *testing.T) {
	agents := gapFleet(t, gapSessions()...) // alex | backend: thea john | frontend: mara
	want := []Roster{
		{Header: core.BroadcastName}, {Selected: "s1"},
		{Header: "backend"}, {Selected: "s2"}, {Selected: "s3"},
		{Header: "frontend"}, {Selected: "s4"},
	}
	r := Roster{Header: core.BroadcastName}
	for i := range want {
		if r != want[i] {
			t.Fatalf("step %d: the cursor is on %+v, want %+v", i, r, want[i])
		}
		r = r.Move(agents, noSubsOf, 1)
	}
	if r != want[0] {
		t.Errorf("down from the last row landed on %+v, want all", r)
	}
	if got := (Roster{Header: core.BroadcastName}).Move(agents, noSubsOf, -1); got != want[len(want)-1] {
		t.Errorf("up from all landed on %+v, want the last row", got)
	}
	for _, from := range []Roster{{}, {Header: "gone"}, {Selected: "gone"}} {
		for _, delta := range []int{1, -1} {
			if got := from.Move(agents, noSubsOf, delta); got != (Roster{Selected: "s1"}) {
				t.Errorf("Move(%+v, %d) = %+v, want the first agent", from, delta, got)
			}
		}
	}
}

// A team header under the cursor is on screen at any column that can hold its
// block - the blank, the header and its first member - under all and the count.
func TestTheWindowKeepsACursoredHeaderOnScreen(t *testing.T) {
	agents := gapFleet(t, gapSessions()...)
	for height := 5; height <= 8; height++ {
		lines := columnLines(Roster{Header: "frontend"}, agents, 30, height, "")
		var found bool
		for _, l := range lines {
			found = found || isRule(l, "frontend")
		}
		if !found {
			t.Errorf("height %d: the cursored frontend header is off screen:\n%s", height, strings.Join(lines, "\n"))
		}
	}
}

// Every row Column draws is the row Hit names: the all header, a team header, an
// agent, and nothing on a blank or the overflow count - across heights and
// cursors, the click-lands-a-row-off class of bug.
func TestEveryDrawnRowIsTheRowAClickHits(t *testing.T) {
	agents := gapFleet(t, gapSessions()...)
	cursors := []Roster{{}, {Header: core.BroadcastName}, {Selected: "s1"}, {Header: "backend"}, {Selected: "s3"}, {Header: "frontend"}, {Selected: "s4"}}
	headers := []string{core.BroadcastName, "backend", "frontend"}
	for height := 1; height <= 10; height++ {
		for _, r := range cursors {
			lines := columnLines(r, agents, 30, height, "")
			for y, line := range lines {
				spot, ok := r.Hit(agents, noSubsOf, 30, height, y)
				drawnHeader := ""
				for _, h := range headers {
					if isRule(line, h) {
						drawnHeader = h
					}
				}
				switch {
				case drawnHeader != "":
					if !ok || spot.header != drawnHeader {
						t.Errorf("height %d, cursor %+v, row %d draws the %s header and hits %+v (%v)", height, r, y, drawnHeader, spot, ok)
					}
				case ok && spot.header != "":
					t.Errorf("height %d, cursor %+v, row %d %q hits the %s header", height, r, y, line, spot.header)
				case ok && !strings.Contains(line, spot.agent.Name):
					t.Errorf("height %d, cursor %+v, row %d %q hits %s", height, r, y, line, spot.agent.Name)
				case !ok && strings.TrimSpace(line) != "" && !strings.Contains(line, "more"):
					t.Errorf("height %d, cursor %+v, row %d %q hits nothing", height, r, y, line)
				}
			}
		}
	}
}

// The selected channel's header is lit, the cursor's is bright and bold, and the
// rest are the muted rule they always were.
func TestTheSidebarMarksTheSelectedChannelAndTheCursor(t *testing.T) {
	forceColour(t)
	agents := gapFleet(t, gapSessions()...)
	const width = 30
	drawn := func(r Roster, channel, name string) string {
		for _, l := range strings.Split(r.Column(agents, noSubsOf, width, 20, channel), "\n") {
			if isRule(stripANSI(l), name) {
				return l
			}
		}
		t.Fatalf("no %s header drawn", name)
		return ""
	}
	muted := func(name string) string { return teamHeaderLine(name, width, HintStyle, HintStyle) }
	lit := func(name string) string { return teamHeaderLine(name, width, AccentStyle, AccentStyle) }
	cases := []struct {
		r             Roster
		channel, name string
		want          string
	}{
		{Roster{}, "", core.BroadcastName, lit(core.BroadcastName)},
		{Roster{}, "", "backend", muted("backend")},
		{Roster{}, "backend", core.BroadcastName, muted(core.BroadcastName)},
		{Roster{}, "backend", "backend", lit("backend")},
		{Roster{Header: "frontend"}, "backend", "frontend", teamHeaderLine("frontend", width, HintStyle, TextStyle.Bold(true))},
		{Roster{Header: "backend"}, "backend", "backend", teamHeaderLine("backend", width, AccentStyle, AccentStyle.Bold(true))},
	}
	for _, c := range cases {
		if got := drawn(c.r, c.channel, c.name); got != c.want {
			t.Errorf("cursor %+v, channel %q: the %s header is %q, want %q", c.r, c.channel, c.name, got, c.want)
		}
	}
	if lipgloss.Width(lit("backend")) != width {
		t.Errorf("a lit header is %d wide, want %d", lipgloss.Width(lit("backend")), width)
	}
}

// cursorTo presses ⇧↓ until the cursor is on want.
func cursorTo(t *testing.T, a App, want Roster) App {
	t.Helper()
	for range 20 {
		if a.roster == want {
			return a
		}
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyShiftDown})
	}
	t.Fatalf("⇧↓ never reached %+v", want)
	return a
}

// ↵ on a header picks its channel and gives the room the keys, from the room or
// from a conversation; with a draft ↵ still sends it.
func TestEnterOnAHeaderPicksItsChannel(t *testing.T) {
	a := cursorTo(t, channelRoom(t), Roster{Header: "backend"})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if a.room.channel != "backend" || a.focus != "" {
		t.Fatalf("↵ on backend: channel %q, focus %q; want backend in the room", a.room.channel, a.focus)
	}
	a = cursorTo(t, a, Roster{Header: core.BroadcastName})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if a.room.channel != "" {
		t.Errorf("↵ on all left the channel %q", a.room.channel)
	}

	a = channelRoom(t).openDMWith("s1", "sydney")
	a = cursorTo(t, a, Roster{Header: "frontend"})
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if a.room.channel != "frontend" || a.focus != "" {
		t.Errorf("↵ on frontend from sydney's conversation: channel %q, focus %q", a.room.channel, a.focus)
	}
	// The keys left sydney's conversation, so what sydney says next is unread
	// again - the more so in a channel that keeps sydney's lines out of the room.
	if f := a.fleet.Focused(); f != "" {
		t.Errorf("after the pick the fleet still reads %q as the open conversation", f)
	}
	was := a.fleet.agents["s1"].Unread
	if a = a.applyFrame(frameText("s1", "sydney late note")); a.fleet.agents["s1"].Unread != was+1 {
		t.Errorf("sydney's unread stayed %d after a line the frontend channel hides", a.fleet.agents["s1"].Unread)
	}

	// With the sidebar closed the cursor is on nothing anyone can see.
	a = cursorTo(t, channelRoom(t), Roster{Header: "backend"})
	a.layout.ShowRoster = false
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if a.room.channel != "" {
		t.Errorf("↵ on a header in a closed sidebar picked %q", a.room.channel)
	}

	a = cursorTo(t, channelRoom(t), Roster{Header: "backend"}).withDraft("hello")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if a.room.channel != "" || a.room.Composer().Value() != "" {
		t.Errorf("↵ with a draft on a header: channel %q, draft %q; want it sent", a.room.channel, a.room.Composer().Value())
	}
}

// channelHeaderRow is the screen row a channel header is drawn on.
func channelHeaderRow(t *testing.T, a App, header string) int {
	t.Helper()
	for y := range a.paneHeight() {
		if spot, ok := a.clickedRow(y); ok && spot.header == header {
			return y
		}
	}
	t.Fatalf("no roster row is the %s header", header)
	return 0
}

// A click on a header picks it on release, resolved at the press; the blank
// above a header is no one's and picks nothing.
func TestClickingAHeaderPicksItsChannel(t *testing.T) {
	a := channelRoom(t)
	x := rosterLeft(a)
	if y := channelHeaderRow(t, a, core.BroadcastName); y != 0 {
		t.Errorf("all is on row %d, want the column's first", y)
	}
	y := channelHeaderRow(t, a, "backend")
	a, _ = click(a, x, y-1)
	if a.room.channel != "" {
		t.Fatalf("a click on the blank above backend picked %q", a.room.channel)
	}
	a, _ = click(a, x, y)
	if a.room.channel != "backend" || a.roster != (Roster{Header: "backend"}) || a.focus != "" {
		t.Fatalf("a click on backend: channel %q, cursor %+v, focus %q", a.room.channel, a.roster, a.focus)
	}

	// A report between press and release moves the rows; the press decided.
	y = channelHeaderRow(t, a, "frontend")
	a, _ = a.mouse(pressAt(x, y))
	a = a.applyStatus(channelReport(append(channelSessions(),
		rpc.SessionStatus{ID: "s6", Name: "kit", State: rpc.StateIdle})...))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: y})
	if a.room.channel != "frontend" {
		t.Errorf("the pressed frontend header picked %q on release", a.room.channel)
	}

	// A report that changes the picked channel's members between press and
	// release re-renders the room, and the click still lands.
	y = channelHeaderRow(t, a, "backend")
	a, _ = a.mouse(pressAt(x, y))
	sessions := channelSessions()
	sessions = append(sessions, rpc.SessionStatus{ID: "s7", Name: "rook", Team: "frontend", State: rpc.StateIdle})
	a = a.applyStatus(channelReport(sessions...))
	if !addressedTo(a.room.channelKey, "s7") {
		t.Fatalf("precondition: rook did not join the frontend channel's key %q", a.room.channelKey)
	}
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: y})
	if a.room.channel != "backend" {
		t.Errorf("a click pressed before the channel's members changed picked %q on release, want backend", a.room.channel)
	}

	a, _ = click(a, x, channelHeaderRow(t, a, core.BroadcastName))
	if a.room.channel != "" {
		t.Errorf("a click on all left the channel %q", a.room.channel)
	}

	// A team gone from the roster between press and release has no channel.
	y = channelHeaderRow(t, a, "frontend")
	a, _ = a.mouse(pressAt(x, y))
	sessions = channelSessions()
	sessions[2].Team = "backend"
	a = a.applyStatus(&rpc.Status{Running: true, Teams: []string{"backend"}, Sessions: sessions})
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: y})
	if a.room.channel != "" || roomTitleOf(a) != roomTitle {
		t.Errorf("a release over a team that left picked channel %q, titled %q", a.room.channel, roomTitleOf(a))
	}
}

// Every writer that puts the cursor on an agent takes it off the header, so the
// sidebar never draws two cursors; a re-keyed pane keeps the cursor it found.
func TestPuttingTheCursorOnAnAgentTakesItOffTheHeader(t *testing.T) {
	onHeader := func() App {
		a := channelRoom(t)
		a.roster = Roster{Header: "backend"}
		return a
	}
	if got := onHeader().openDMWith("s1", "sydney").roster; got != (Roster{Selected: "s1"}) {
		t.Errorf("opening sydney's conversation left the cursor %+v", got)
	}
	if got := onHeader().viewBoardDispatch("s1", "").roster; got.Header != "" {
		t.Errorf("a board dispatch left the cursor %+v", got)
	}
	a := onHeader()
	a.rosterHit = rosterHit{id: "s2", name: "thea", ok: true}
	if got := a.screenClick().roster; got != (Roster{Selected: "s2"}) {
		t.Errorf("a roster click on thea left the cursor %+v", got)
	}

	a = channelRoom(t).openDMWith("s1", "sydney")
	a.roster = Roster{Header: "backend"}
	if got := a.rekeyPane("s1", "s1-next", "sydney").roster; got != (Roster{Header: "backend"}) {
		t.Errorf("a re-keyed pane moved the cursor to %+v", got)
	}
}

// A conversation opens as it always did, and the channel stays.
func TestOpeningAConversationKeepsTheChannel(t *testing.T) {
	a := channelRoom(t).selectChannel("backend")
	a, _ = pressKey(cursorTo(t, a, Roster{Selected: "s3"}), tea.KeyMsg{Type: tea.KeyEnter})
	if a.focus != "s3" || a.room.channel != "backend" {
		t.Errorf("↵ on delta: focus %q, channel %q; want delta's conversation over the backend channel", a.focus, a.room.channel)
	}
}

// A header is no agent: ⌃C with the cursor on one parks nobody.
func TestCtrlCOnAHeaderParksNobody(t *testing.T) {
	a := cursorTo(t, channelRoom(t), Roster{Header: "backend"})
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd != nil {
		t.Errorf("⌃C on a header wrote %v", sentFrames(t, a, cmd))
	}
	if notice.Count(noParkTarget) != 1 {
		t.Errorf("⌃C on a header was not refused (count=%d)", notice.Count(noParkTarget))
	}
}
