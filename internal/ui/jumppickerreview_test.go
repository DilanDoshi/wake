package ui

// What the adversarial review found about the jump picker: a refused ⌃B on an agent
// already drawn, keys that reached the draft under the box, and the list, filter and
// ranking cases no earlier test used.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// An agent already on screen is only focused, whichever open key is pressed: ⌃B on
// one cannot "refuse" from a lower pane and then close the box and move the cursor
// without moving the keys.
func TestEveryOpenKeyOnAnAgentAlreadyOnScreenOnlyFocusesIt(t *testing.T) {
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyCtrlB, tea.KeyCtrlD} {
		a := clickAgent(t, blueFleet(t, 200, 40), "s2")
		a, _ = pressKey(jumpTyped(jumpOpened(t, a), "scr"), kmsg(tea.KeyCtrlB)) // s7 is under s2 and holds the keys
		if a.focus != "s7" {
			t.Fatalf("setup: the keys are in %q, want s7", a.focus)
		}
		before := fmt.Sprint(a.grid.Cols)
		notice.Reset()
		a, _ = pressKey(jumpTyped(jumpOpened(t, a), "web-b"), kmsg(key))
		if a.jumpPicker.Open() || a.focus != "s2" {
			t.Errorf("key %v: box open %v, focus %q, want it closed with the keys on s2", key, a.jumpPicker.Open(), a.focus)
		}
		if after := fmt.Sprint(a.grid.Cols); after != before {
			t.Errorf("key %v changed the columns from %s to %s", key, before, after)
		}
		if n, ok := notice.Latest(); ok && strings.Contains(n.String(), noRowBelow) {
			t.Errorf("key %v refused with %q for an agent that needed no placing", key, n.String())
		}
	}
}

// ⌫ and Delete under the box edit the filter. A highlight in the draft behind it
// must not make them delete the draft's text, as it does with nothing up.
func TestBackspaceUnderTheBoxNeverDeletesTheDraftsHighlight(t *testing.T) {
	for _, key := range []tea.KeyMsg{{Type: tea.KeyBackspace}, {Type: tea.KeyDelete}} {
		draft := "hello world\nsecond line here"
		a := jumpTyped(jumpOpened(t, roomDraftApp(t, draft)), "al")
		r := a.regions()
		draftTop, _, _, _, ok := a.composerRegion("", r.Room(), 0, a.paneHeight())
		if !ok {
			t.Fatal("no composer region")
		}
		left := a.layout.PaneLeft(r, 0) + composerTextLeft
		a, _ = a.mouse(pressAt(left+6, draftTop))
		for x := left + 7; x <= left+10; x++ {
			a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft, X: x, Y: draftTop})
		}
		a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: left + 10, Y: draftTop})
		if !a.sel.inComposer || a.sel.empty() {
			t.Fatalf("no composer highlight was taken: %+v", a.sel)
		}
		m, _ := a.Update(key)
		got := m.(App)
		if v := got.composer().Value(); v != draft {
			t.Errorf("%v under the box left the draft as %q, want it untouched", key.Type, v)
		}
		if !got.jumpPicker.Open() {
			t.Errorf("%v closed the box", key.Type)
		}
	}
}

// A pasted image path while the box is up closes it and lands as the chip it always
// does, rather than going into the draft behind a box that still holds the keys.
func TestAnImagePathPastedUnderTheBoxClosesItAndAttaches(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	a, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("/tmp/shot.png")})
	if cmd == nil {
		t.Error("the dropped image was not read")
	}
	if a.jumpPicker.Open() {
		t.Error("the box stayed up over a draft the image is going into")
	}
}

// Pinning the cursor to an id guards a re-rank, not a vanished agent: with its agent
// ended the cursor lands on the first row, which is the row the box then draws it on.
func TestTheCursorFallsToTheFirstRowWhenItsAgentEnds(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	a, _ = pressKey(a, kmsg(tea.KeyDown))
	a, _ = pressKey(a, kmsg(tea.KeyDown))
	a, _ = pressKey(a, kmsg(tea.KeyDown))
	gone, first := jumpCursorName(t, a), jumpNames(a)[0]
	st := rpc.Status{Running: true}
	for _, ag := range a.fleet.OnRoster() {
		state := ag.State
		if ag.Name == gone {
			state = rpc.StateEnded
		}
		st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: ag.ID, Name: ag.Name, Team: ag.Team, State: state})
	}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	if got := jumpCursorName(t, a); got != first || slices.Contains(jumpNames(a), gone) {
		t.Errorf("after %q ended the cursor is on %q (list %q), want the first row %q", gone, got, jumpNames(a), first)
	}
}

// The list is what the sidebar lists: an ended agent is gone, a parked one and the
// manager are there and can be opened.
func TestTheListHasParkedAgentsAndTheManagerButNeverAnEndedOne(t *testing.T) {
	fresh(t)
	st := rpc.Status{Running: true, ManagerClaude: "manager-harbor", Sessions: []rpc.SessionStatus{
		{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle},
		{ID: "s2", Name: "sydney", State: rpc.StateParked},
		{ID: "s3", Name: "robin", State: rpc.StateEnded},
		{ID: "s4", Name: "alex", State: rpc.StateIdle},
	}}
	a := NewRoomApp(&recorder{}, Stream{}, nil).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st}).withSize(140, 40)
	a.layout.ShowRoster = true
	a = jumpOpened(t, a.resizePanes())
	names := jumpNames(a)
	for _, want := range []string{"manager-harbor", "sydney", "alex"} {
		if !slices.Contains(names, want) {
			t.Errorf("the list %q has no %q", names, want)
		}
	}
	if slices.Contains(names, "robin") {
		t.Errorf("the list %q has an ended agent", names)
	}
	a = jumpTyped(a, "syd")
	if got, _ := pressKey(a, kmsg(tea.KeyEnter)); got.focus != "s2" {
		t.Errorf("↵ on a parked agent put the keys in %q, want s2", got.focus)
	}
}

// A space the terminal sends as KeySpace is part of the filter (not a leading one),
// and ⌫ on an empty filter does nothing rather than crash.
func TestSpaceAndBackspaceEditTheFilterAtItsEdges(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	for range 3 {
		a, _ = pressKey(a, kmsg(tea.KeyBackspace))
	}
	a, _ = pressKey(a, kmsg(tea.KeySpace))
	if a.jumpPicker.Query != "" {
		t.Fatalf("the filter is %q after ⌫ on nothing and a leading space, want it empty", a.jumpPicker.Query)
	}
	a = jumpTyped(a, "web")
	a, _ = pressKey(a, kmsg(tea.KeySpace))
	a = jumpTyped(a, "b")
	if a.jumpPicker.Query != "web b" {
		t.Errorf("the filter is %q, want %q: a space is a character of it", a.jumpPicker.Query, "web b")
	}
	if !a.jumpPicker.Open() {
		t.Error("the box closed")
	}
}

// ↵, ⌃B, ⌃D and ↑↓ with nothing matching open and move nothing, and the box says so.
func TestKeysOnAFilterNothingMatchesDoNothing(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "zzzz")
	if len(jumpNames(a)) != 0 {
		t.Fatalf("zzzz matched %q", jumpNames(a))
	}
	if !strings.Contains(strings.Join(jumpPanes(a), "\n"), fmt.Sprintf(jumpNoMatch, "zzzz")) {
		t.Errorf("the box does not say nothing matches:\n%s", strings.Join(jumpPanes(a), "\n"))
	}
	before := fmt.Sprint(a.grid.Cols)
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyCtrlB, tea.KeyCtrlD, tea.KeyUp, tea.KeyDown} {
		got, _ := pressKey(a, kmsg(key))
		if !got.jumpPicker.Open() || fmt.Sprint(got.grid.Cols) != before {
			t.Errorf("key %v on no match: box open %v, columns %v, want it up and nothing opened", key, got.jumpPicker.Open(), got.grid.Cols)
		}
	}
}

// Agents that rank alike keep the order the roster gives them, however many there
// are: a sort that only looks stable for a dozen or so would shuffle a big fleet.
func TestEqualMatchesKeepTheirOrderInABigFleet(t *testing.T) {
	var given, want []string
	for i := range 80 { // two match classes interleaved: a prefix hit, then a hit inside
		given = append(given, fmt.Sprintf("agent-%02d", i), fmt.Sprintf("z-agent-%02d", i))
	}
	for _, n := range given {
		if strings.HasPrefix(n, "agent") {
			want = append(want, n)
		}
	}
	for _, n := range given {
		if !strings.HasPrefix(n, "agent") {
			want = append(want, n)
		}
	}
	if got := namesOfAgents(matchAgents("agent", namedAgents(given...))); !slices.Equal(got, want) {
		t.Errorf("equal matches came back out of the given order:\n%q", got)
	}
}

// A team aimed at /fork is refused in words, whichever way it is aimed.
func TestSlashForkSaysWhyItRefusesATeam(t *testing.T) {
	a := forkFleet(t).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{Running: true,
		Teams: []string{"web"}, Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "alex", Team: "web"}, {ID: "s2", Name: "sydney", Team: "web"}}}})
	for draft, says := range map[string]string{
		"/fork @web": noSuchAgent,
		"@web /fork": fmt.Sprintf(teamCommandRefused, "fork", "web"),
	} {
		notice.Reset()
		typeCommand(a, draft)
		if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), says) {
			t.Errorf("%q said %q, want it to say %q", draft, n.String(), says)
		}
	}
}

// The box is told what is stacked over it and fits in what is left: the pane clips a
// menu from the bottom, so a box drawn as tall as the whole menu room would lose its
// foot - or the cursor's row - under a card.
func TestTheBoxFitsInWhatIsStackedUnderTheCard(t *testing.T) {
	checked := 0
	for h := 14; h <= 40; h++ {
		a := jumpOpened(t, blueFleet(t, 140, h))
		w, ph, ok := a.paneSize(a.focus)
		if !ok {
			t.Fatal("no pane size")
		}
		room := a.menuRoom(a.focus, w, ph)
		for over := 1; over <= 9; over++ {
			if room-over < jumpChrome+1 {
				continue // the compact box's own rule, held by the sizing sweep
			}
			checked++
			card := strings.TrimSuffix(strings.Repeat("card\n", over), "\n")
			if got := lipgloss.Height(a.jumpView(a.focus, w, ph, card)); got > room-over {
				t.Errorf("%d rows high, under a %d-row card the box is %d rows, room left is %d", h, over, got, room-over)
			}
		}
	}
	if checked == 0 {
		t.Error("no size left room for the full box under a card")
	}
}
