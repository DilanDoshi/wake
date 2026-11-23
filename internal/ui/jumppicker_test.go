package ui

// The jump picker: ⌃F (or `/jump`) opens a box over the composer, a few letters of
// a name narrow the fleet, and ↵ puts the cursored agent on screen the way a click
// on its sidebar row does.

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

type jumpSpec struct{ name, team, state, tool, arg string }

// jumpApp is the room with the sidebar drawn and the named agents on it, s1..sN
// in the order given.
func jumpApp(t testing.TB, w, h int, specs ...jumpSpec) App {
	t.Helper()
	fresh(t)
	st := rpc.Status{Running: true}
	for i, s := range specs {
		st.Sessions = append(st.Sessions, rpc.SessionStatus{
			ID: fmt.Sprintf("s%d", i+1), Name: s.name, Team: s.team,
			State: cmp.Or(s.state, rpc.StateIdle), Tool: s.tool, ToolArg: s.arg,
		})
	}
	a := NewRoomApp(&recorder{}, Stream{}, nil).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st}).withSize(w, h)
	a.layout.ShowRoster = true
	return a.resizePanes()
}

// blueFleet is the fleet the mockups were drawn on, cut to eight: five names a
// `blu` finds, one of them blocked and one only by letters apart.
func blueFleet(t testing.TB, w, h int) App {
	t.Helper()
	return jumpApp(t, w, h,
		jumpSpec{name: "pay-ledger", team: "payments", tool: "Edit", arg: "ledger/post.go"},
		jumpSpec{name: "web-blur", team: "web", state: rpc.StateWorking, tool: "Edit", arg: "styles/blur.css"},
		jumpSpec{name: "ui-blueprint", team: "web", state: rpc.StateBlocked, tool: "Write", arg: "docs/blueprint.md"},
		jumpSpec{name: "infra-blue-green", team: "infra", state: rpc.StateWorking, tool: "Bash", arg: "kubectl rollout status deploy/api"},
		jumpSpec{name: "qa-visual-blur", team: "qa"},
		jumpSpec{name: "docs-bulk-upload", team: "docs"},
		jumpSpec{name: "scratch"},
		jumpSpec{name: "notes"},
	)
}

func kmsg(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

// jumpTyped types s into whatever holds the keys, a rune at a time as a terminal does.
func jumpTyped(a App, s string) App {
	for _, r := range s {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return a
}

func jumpOpened(t testing.TB, a App) App {
	t.Helper()
	a, _ = pressKey(a, kmsg(tea.KeyCtrlF))
	if !a.jumpPicker.Open() {
		t.Fatal("⌃F did not open the jump picker")
	}
	return a
}

func jumpNames(a App) []string { return namesOfAgents(a.jumpAgents()) }

func jumpCursorName(t testing.TB, a App) string {
	t.Helper()
	ag, ok := a.jumpCursorAgent()
	if !ok {
		t.Fatal("the picker has no cursored agent")
	}
	return ag.Name
}

func TestCtrlFOpensThePickerWithEveryAgentBlockedFirst(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	got := jumpNames(a)
	if len(got) != 8 || got[0] != "ui-blueprint" {
		t.Errorf("with an empty filter the picker lists %q, want all eight with the blocked one first", got)
	}
	if jumpCursorName(t, a) != "ui-blueprint" {
		t.Errorf("the cursor starts on %q, want the first row", jumpCursorName(t, a))
	}
}

func TestTypingNarrowsTheListAndPutsTheCursorOnTheBestMatch(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "blu")
	want := []string{"ui-blueprint", "web-blur", "infra-blue-green", "qa-visual-blur", "docs-bulk-upload"}
	if got := jumpNames(a); !slices.Equal(got, want) {
		t.Errorf("blu finds %q, want %q", got, want)
	}
	a, _ = pressKey(a, kmsg(tea.KeyBackspace))
	if a.jumpPicker.Query != "bl" {
		t.Errorf("⌫ left the query %q, want bl", a.jumpPicker.Query)
	}
	a = jumpTyped(a, "x")
	if got := jumpNames(a); len(got) != 0 {
		t.Errorf("blx found %q, want nothing", got)
	}
}

func TestArrowsMoveTheCursorWithoutWrapping(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "blu")
	a, _ = pressKey(a, kmsg(tea.KeyUp))
	if got := jumpCursorName(t, a); got != "ui-blueprint" {
		t.Errorf("↑ off the top moved the cursor to %q", got)
	}
	for range 9 {
		a, _ = pressKey(a, kmsg(tea.KeyDown))
	}
	if got := jumpCursorName(t, a); got != "docs-bulk-upload" {
		t.Errorf("↓ past the end left the cursor on %q, want the last row", got)
	}
}

// A report that re-ranks the fleet while the picker is up must not slide the
// cursor onto a different agent: ↵ opens whoever the operator was looking at.
func TestTheCursorStaysOnItsAgentWhenTheFleetReRanks(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	a, _ = pressKey(a, kmsg(tea.KeyDown))
	a, _ = pressKey(a, kmsg(tea.KeyDown))
	was := jumpCursorName(t, a)
	st := rpc.Status{Running: true}
	for _, ag := range a.fleet.OnRoster() {
		state := ag.State
		if ag.Name == "notes" {
			state = rpc.StateBlocked // a second blocked agent now outranks the cursor's row
		}
		st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: ag.ID, Name: ag.Name, Team: ag.Team, State: state})
	}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	if got := jumpCursorName(t, a); got != was {
		t.Errorf("the cursor moved from %q to %q when the fleet re-ranked", was, got)
	}
}

// ↵ is a click on the agent's sidebar row: the same column, focus and selection.
// It starts in a conversation, where ⌃D's "here" would replace it, and from a
// sidebar cursor left on another agent's dispatch, which the open must clear.
func TestEnterOpensTheAgentTheWayAClickOnItsRowDoes(t *testing.T) {
	base := clickAgent(t, blueFleet(t, 200, 40), "s1")
	base.roster.Selected, base.roster.SelectedTask = "s5", "toolu_stale"
	want := clickAgent(t, base, "s2") // web-blur
	a := jumpTyped(jumpOpened(t, base), "web-b")
	got, _ := pressKey(a, kmsg(tea.KeyEnter))
	if !slices.Equal(tops(got), tops(want)) || got.focus != want.focus {
		t.Errorf("↵ left columns %q and focus %q; a click leaves %q and %q", tops(got), got.focus, tops(want), want.focus)
	}
	if want := []string{"", "s1", "s2"}; !slices.Equal(tops(got), want) {
		t.Errorf("columns %q, want %q: the open adds a column and replaces nothing", tops(got), want)
	}
	if got.roster.Selected != "s2" || got.roster.SelectedTask != "" {
		t.Errorf("the sidebar's cursor is %q/%q, want s2 and no dispatch", got.roster.Selected, got.roster.SelectedTask)
	}
	if got.jumpPicker.Open() {
		t.Error("the picker stayed up after opening the agent")
	}
}

func TestEnterOnAnAgentAlreadyOnScreenOnlyFocusesIt(t *testing.T) {
	a := clickAgent(t, blueFleet(t, 200, 40), "s2")
	a = a.refocus("")
	a.roster.Selected, a.roster.SelectedTask = "s2", "toolu_stale" // a dispatch the cursor was on
	before := tops(a)
	a = jumpTyped(jumpOpened(t, a), "web-b")
	a, _ = pressKey(a, kmsg(tea.KeyEnter))
	if !slices.Equal(tops(a), before) || a.focus != "s2" {
		t.Errorf("columns %q focus %q, want %q with the keys on s2", tops(a), a.focus, before)
	}
	if a.roster.SelectedTask != "" {
		t.Errorf("the cursor kept the dispatch %q: an opened agent must not keep one", a.roster.SelectedTask)
	}
	if a.jumpPicker.Open() {
		t.Error("the picker stayed up")
	}
}

func TestCtrlBOpensBelowTheFocusedPane(t *testing.T) {
	a := clickAgent(t, blueFleet(t, 200, 40), "s2") // web-blur, in a column of its own
	a = jumpTyped(jumpOpened(t, a), "scr")
	a, _ = pressKey(a, kmsg(tea.KeyCtrlB))
	if a.grid.Cols[1].Top != "s2" || a.grid.Cols[1].Bottom != "s7" {
		t.Errorf("the column holds %q over %q, want s2 over s7", a.grid.Cols[1].Top, a.grid.Cols[1].Bottom)
	}
	if a.jumpPicker.Open() {
		t.Error("the picker stayed up")
	}
}

// ⌃B from a pane that is already the bottom of its column refuses, and says why.
// The picker stays up: the keystroke should cost nothing.
func TestCtrlBFromALowerPaneRefusesAndKeepsThePickerUp(t *testing.T) {
	a := clickAgent(t, blueFleet(t, 200, 40), "s2")
	a, _ = pressKey(jumpTyped(jumpOpened(t, a), "scr"), kmsg(tea.KeyCtrlB)) // s7 is now under s2, and holds the keys
	a = jumpTyped(jumpOpened(t, a), "not")
	before := fmt.Sprint(a.grid.Cols)
	got, _ := pressKey(a, kmsg(tea.KeyCtrlB))
	if after := fmt.Sprint(got.grid.Cols); after != before {
		t.Errorf("the columns changed from %s to %s on a refused ⌃B", before, after)
	}
	if !got.jumpPicker.Open() {
		t.Error("a refused ⌃B closed the picker")
	}
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), noRowBelow) {
		t.Errorf("the refusal said %q, want %q", n.String(), noRowBelow)
	}
}

func TestCtrlDOpensItInTheFocusedPaneAndNeverReplacesTheRoom(t *testing.T) {
	a := clickAgent(t, blueFleet(t, 200, 40), "s2")
	a = jumpTyped(jumpOpened(t, a), "scr")
	a, _ = pressKey(a, kmsg(tea.KeyCtrlD))
	if want := []string{"", "s7"}; !slices.Equal(tops(a), want) {
		t.Errorf("⌃D from a conversation left %q, want %q: it replaces the focused pane", tops(a), want)
	}

	fromRoom := jumpTyped(jumpOpened(t, blueFleet(t, 200, 40)), "scr")
	fromRoom, _ = pressKey(fromRoom, kmsg(tea.KeyCtrlD))
	if want := []string{"", "s7"}; !slices.Equal(tops(fromRoom), want) || fromRoom.focus != "s7" {
		t.Errorf("⌃D from the room left %q focus %q: the room is never replaced", tops(fromRoom), fromRoom.focus)
	}
}

func TestEscClosesWithoutInterruptingAnything(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "blu")
	a, cmd := pressKey(a, kmsg(tea.KeyEsc))
	if a.jumpPicker.Open() {
		t.Error("esc did not close the picker")
	}
	if cmd != nil {
		t.Errorf("esc wrote something: %v", cmd)
	}
}

func TestCtrlFTogglesIt(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	a, _ = pressKey(a, kmsg(tea.KeyCtrlF))
	if a.jumpPicker.Open() {
		t.Error("a second ⌃F left the picker up")
	}
}

// ⌃C keeps its meaning (resumePickerKey's reasoning): the first press does something
// visible, closes the box, and parks the conversation holding the keys.
func TestCtrlCClosesThePickerAndParksTheFocusedConversation(t *testing.T) {
	a := clickAgent(t, blueFleet(t, 200, 40), "s2")
	a = jumpOpened(t, a)
	got, cmd := pressKey(a, kmsg(tea.KeyCtrlC))
	if got.jumpPicker.Open() {
		t.Error("⌃C left the picker up")
	}
	if f := sentFrame(t, got, cmd); f.Kind != rpc.FramePark || f.SessionID != "s2" {
		t.Errorf("⌃C wrote %s for %q, want a park of the focused conversation s2", f.Kind, f.SessionID)
	}
}

// Everything the picker does not own is swallowed: it is a search box, and a key
// pressed to mend a typo must not move the keys to another pane under it.
func TestEveryOtherKeyIsSwallowedAndNothingReachesTheComposer(t *testing.T) {
	a := clickAgent(t, blueFleet(t, 200, 40), "s2")
	a = jumpTyped(jumpOpened(t, a), "b")
	for _, k := range []tea.KeyMsg{kmsg(tea.KeyTab), kmsg(tea.KeyShiftTab), kmsg(tea.KeyLeft), kmsg(tea.KeyCtrlY), kmsg(tea.KeyCtrlW), kmsg(tea.KeyCtrlQ)} {
		next, _ := pressKey(a, k)
		if !next.jumpPicker.Open() || next.focus != "s2" {
			t.Errorf("%v closed the picker or moved the keys to %q", k.Type, next.focus)
		}
	}
	if v := a.composer().Value(); v != "" {
		t.Errorf("typing reached the composer: %q", v)
	}
}

func TestTheSlashCommandOpensItWithTheFilterTyped(t *testing.T) {
	for _, tc := range []struct{ draft, query string }{{"/jump", ""}, {"/jump blu", "blu"}, {"/JUMP  blu ", "blu"}} {
		a := blueFleet(t, 140, 40)
		a = a.withComposer(a.composer().WithDraft(tc.draft))
		a, _ = pressKey(a, kmsg(tea.KeyEnter))
		if !a.jumpPicker.Open() || a.jumpPicker.Query != tc.query {
			t.Errorf("%q: open %v query %q, want open with %q", tc.draft, a.jumpPicker.Open(), a.jumpPicker.Query, tc.query)
		}
		if v := a.composer().Value(); v != "" {
			t.Errorf("%q left %q in the draft", tc.draft, v)
		}
	}
}

func TestAnEmptyFleetSaysSoInsteadOfOpening(t *testing.T) {
	a, _ := pressKey(jumpApp(t, 140, 40), kmsg(tea.KeyCtrlF))
	if a.jumpPicker.Open() {
		t.Error("the picker opened over nobody")
	}
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), noJumpTarget) {
		t.Errorf("the notice was %q, want %q", n.String(), noJumpTarget)
	}
}

// A picker is drawn over the pane that holds the keys and reads them only there.
func TestAFocusChangeClosesIt(t *testing.T) {
	a := clickAgent(t, blueFleet(t, 200, 40), "s2")
	a = jumpOpened(t, a)
	if a = a.refocus(""); a.jumpPicker.Open() {
		t.Error("the picker stayed up over a pane it was not opened in")
	}
}

// plainView is the frame's rows without colour, cut to the panes: the sidebar
// beside them draws the same agents' names on the same rows.
func jumpPanes(a App) []string {
	rows := strings.Split(stripANSI(a.View()), "\n")
	for i, r := range rows {
		rows[i] = ansi.Truncate(r, max(a.layout.Width-30, 20), "")
	}
	return rows
}

func rowHaving(rows []string, sub string) int {
	return slices.IndexFunc(rows, func(r string) bool { return strings.Contains(r, sub) })
}

func TestTheBoxDrawsOverTheComposerWithARuleUnderTheFilterLine(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 44)), "blu")
	rows := jumpPanes(a)
	filter := rowHaving(rows, "⌕ blu")
	if filter < 0 {
		t.Fatalf("no filter line drawn:\n%s", strings.Join(rows, "\n"))
	}
	if rule := rows[filter+1]; !strings.Contains(rule, strings.Repeat("─", 30)) {
		t.Errorf("the row under the filter line is %q, want a rule", rule)
	}
	first := rowHaving(rows, "ui-blueprint")
	if first != filter+2 {
		t.Errorf("the first result is on row %d, want the row right under the rule (%d)", first, filter+2)
	}
	if !strings.Contains(rows[first], "waiting for you") {
		t.Errorf("a blocked agent's row says %q", rows[first])
	}
	for _, want := range []string{"jump to agent · 5 of 8", "↵ new pane", "⌃B below", "⌃D here", "esc"} {
		if rowHaving(rows, want) < 0 {
			t.Errorf("the box never says %q", want)
		}
	}
	lower := rowHaving(rows, "↵ new pane")
	if lower < first || !strings.Contains(rows[lower+1], "╭") {
		t.Errorf("the row under the box's lower edge is %q, want the composer's own top edge", rows[lower+1])
	}
	blur := slices.IndexFunc(rows[first:], func(r string) bool { return strings.Contains(r, "web-blur") }) + first
	if r := rows[blur]; blur < first || !strings.Contains(r, "web") || !strings.Contains(r, "Edit(blur.css)") {
		t.Errorf("a result carries its team and what it is doing: %q", r)
	}
}

// A window of eight rows follows the cursor, and the border counts what it hides.
func TestTheWindowFollowsTheCursorAndCountsWhatItHides(t *testing.T) {
	specs := make([]jumpSpec, 14)
	for i := range specs {
		specs[i] = jumpSpec{name: fmt.Sprintf("agent-%02d", i+1)}
	}
	a := jumpOpened(t, jumpApp(t, 140, 46, specs...))
	if rows := jumpPanes(a); rowHaving(rows, "+6 below") < 0 || rowHaving(rows, "agent-08") < 0 || rowHaving(rows, "agent-09") >= 0 {
		t.Errorf("the first window is not eight rows with +6 below:\n%s", strings.Join(rows, "\n"))
	}
	for i := range 13 {
		a, _ = pressKey(a, kmsg(tea.KeyDown))
		if rowHaving(jumpPanes(a), jumpCursorName(t, a)) < 0 {
			t.Fatalf("after %d ↓ the cursor's row %q is not drawn", i+1, jumpCursorName(t, a))
		}
	}
	if rows := jumpPanes(a); rowHaving(rows, "+6 above") < 0 {
		t.Errorf("at the end the box does not say +6 above:\n%s", strings.Join(rows, "\n"))
	}
}

// The box never pushes the frame past the window, and the row ↵ opens is drawn at
// every size, the last row included - a box taller than its room is clipped from
// the bottom, which cuts exactly the rows a long walk ends on. A pane with no row
// for it refuses to open it, and one resized to nothing closes it. Below five rows
// the box is the compact view, and the sweep must reach both.
func TestTheBoxFitsEverySizeAndAlwaysDrawsTheCursorsRow(t *testing.T) {
	compact, refused := 0, 0
	for _, w := range []int{70, 100, 140, 200} {
		for h := 6; h <= 46; h++ {
			base := blueFleet(t, w, h)
			a, _ := pressKey(base, kmsg(tea.KeyCtrlF))
			if base.jumpRoom() < 1 {
				refused++
				if a.jumpPicker.Open() {
					t.Fatalf("%dx%d: the box opened in a pane with no row for it", w, h)
				}
				continue
			}
			if base.jumpRoom() < jumpChrome+1 {
				compact++
			}
			a = jumpTyped(a, "blu")
			for step := range 5 { // the first match, then each of the others down to the last
				if got := lipgloss.Height(a.View()); got > h {
					t.Fatalf("%dx%d: the frame is %d rows tall", w, h, got)
				}
				rows := jumpPanes(a)
				top := max(rowHaving(rows, "⌕"), 0) // the room's own working line names agents too
				if rowHaving(rows[top:], jumpCursorName(t, a)) < 0 {
					t.Fatalf("%dx%d at row %d: the cursor's row %q is not drawn:\n%s", w, h, step, jumpCursorName(t, a), strings.Join(rows, "\n"))
				}
				if base.jumpRoom() >= jumpChrome+1 && rowHaving(rows[top:], "↑↓") < 0 {
					t.Fatalf("%dx%d at row %d: the box's foot is clipped:\n%s", w, h, step, strings.Join(rows, "\n"))
				}
				a, _ = pressKey(a, kmsg(tea.KeyDown))
			}
		}
	}
	if compact == 0 || refused == 0 {
		t.Errorf("the sweep met %d compact and %d refused sizes: both paths must be exercised", compact, refused)
	}
}

// A window made too short while the box is up closes it on the next key, rather
// than leaving it holding keys nobody can see.
func TestAResizeThatLeavesNoRoomClosesIt(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	a = a.withSize(140, 7).resizePanes()
	a, _ = pressKey(a, kmsg(tea.KeyDown))
	if a.jumpPicker.Open() {
		t.Error("the box is still up in a pane with no row for it")
	}
}

// A half-typed `/res` has its completion menu up; the box takes the keys, so the
// menu under it would be a second menu nobody can use.
func TestOpeningItHidesTheCompletionMenuUnderIt(t *testing.T) {
	a := blueFleet(t, 140, 44).withDraft("/ju")
	if a.completionView(60, a.focus) == "" {
		t.Fatal("setup: a draft of /ju draws no completion menu, so this test asserts nothing")
	}
	a = jumpOpened(t, a)
	if a.completionView(60, a.focus) != "" {
		t.Error("the completion menu is still drawn under the jump box")
	}
	if got := a.composer().Value(); got != "/ju" {
		t.Errorf("opening the box changed the draft to %q: it is the operator's", got)
	}
}

// One menu at a time: the effort picker and /status give way, as the resume picker's do.
func TestOpeningItTakesTheOtherMenusDown(t *testing.T) {
	a := blueFleet(t, 140, 44)
	a = a.openPicker(effortCommand, []string{"s1"})
	a.statusUI = StatusView{Pane: a.focus}
	a = jumpOpened(t, a)
	if a.picker.Open() {
		t.Error("the /effort picker stayed up under the jump box")
	}
	if a.statusUI.Open() {
		t.Error("/status stayed up under the jump box")
	}
}

// Typing changes who the list is, so the cursor goes back to the first match
// rather than staying on a row the operator moved to for another filter - even
// when that row still matches.
func TestTypingHomesTheCursorOnTheFirstMatch(t *testing.T) {
	a := jumpTyped(jumpOpened(t, blueFleet(t, 140, 40)), "blu")
	a, _ = pressKey(a, kmsg(tea.KeyDown))
	a, _ = pressKey(a, kmsg(tea.KeyDown))
	if got := jumpCursorName(t, a); got != "infra-blue-green" {
		t.Fatalf("setup: ↓↓ put the cursor on %q, want infra-blue-green", got)
	}
	a = jumpTyped(a, "e") // infra-blue-green still matches blue, but is not first
	if got := jumpCursorName(t, a); got != "ui-blueprint" {
		t.Errorf("after typing, the cursor is on %q, want the first match, ui-blueprint", got)
	}
}

// ⌃F opens it over whichever pane holds the keys, the room or a conversation, and
// the box is that pane's: it is not drawn in another.
func TestCtrlFOpensOverTheRoomAndOverAConversation(t *testing.T) {
	room := jumpOpened(t, blueFleet(t, 200, 44))
	if room.jumpPicker.Pane != "" {
		t.Errorf("opened in the room, the box belongs to pane %q", room.jumpPicker.Pane)
	}
	dm := jumpOpened(t, clickAgent(t, blueFleet(t, 200, 44), "s2"))
	if dm.jumpPicker.Pane != "s2" {
		t.Errorf("opened in sydney's conversation, the box belongs to pane %q, want s2", dm.jumpPicker.Pane)
	}
	if rowHaving(jumpPanes(dm), "jump to agent") < 0 {
		t.Error("the box is not drawn over the conversation that opened it")
	}
}
