package ui

// The jump picker's edges: a card in the pane, a filter nothing matches, a
// pasted or over-long query, late menus, and a re-rank between a look and a ↵.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestAMoreExactNameBeatsABlockedAgentFoundOnlyByScatteredLetters(t *testing.T) {
	in := namedAgents("docs-bulk-upload", "db-migrate", "web-db")
	in[0].State = rpc.StateBlocked
	if got, want := namesOfAgents(matchAgents("db", in)), []string{"db-migrate", "web-db", "docs-bulk-upload"}; !slices.Equal(got, want) {
		t.Errorf("db finds %q, want %q: what was typed outranks who is blocked", got, want)
	}
}

// The cursor is on an agent from the first look, not only after ↑↓: a report that
// re-ranks the fleet between typing and ↵ must not change who ↵ opens.
func TestACursorTypedOntoStaysOnItsAgentThroughAReRank(t *testing.T) {
	a := blueFleet(t, 200, 40)
	a = jumpTyped(jumpOpened(t, a), "blu")
	st := rpc.Status{Running: true}
	for _, ag := range a.fleet.OnRoster() {
		state := ag.State
		if ag.Name == "docs-bulk-upload" || ag.Name == "qa-visual-blur" {
			state = rpc.StateBlocked // two more blocked agents now outrank the cursor's row
		}
		st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: ag.ID, Name: ag.Name, Team: ag.Team, State: state})
	}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	a, _ = pressKey(a, kmsg(tea.KeyEnter))
	if a.focus != "s3" { // ui-blueprint, the first match when it was typed
		t.Errorf("↵ opened %q after a re-rank, want ui-blueprint (s3), the agent the cursor was on", a.focus)
	}
}

// The same from the moment the box opens: ⌃K, a report that unblocks the first
// row and blocks another, ↵ opens who was first when it opened.
func TestACursorOnOpeningStaysOnItsAgentThroughAReRank(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 200, 40)) // ui-blueprint, blocked, is first
	st := rpc.Status{Running: true}
	for _, ag := range a.fleet.OnRoster() {
		state := ag.State
		switch ag.Name {
		case "ui-blueprint":
			state = rpc.StateIdle
		case "notes":
			state = rpc.StateBlocked
		}
		st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: ag.ID, Name: ag.Name, Team: ag.Team, State: state})
	}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	if first := a.jumpAgents()[0].Name; first != "notes" {
		t.Fatalf("setup: the report left %q first, want notes", first)
	}
	if got := jumpCursorName(t, a); got != "ui-blueprint" {
		t.Errorf("the cursor moved to %q when the fleet re-ranked, want it kept on ui-blueprint", got)
	}
}

func TestAnAgentThatIsStillRunningSomethingIsNotDoneInTheBoxEither(t *testing.T) {
	a := jumpApp(t, 140, 44, jumpSpec{name: "john"})
	a.fleet = finishedFleet() // john finished a turn this client watched
	box := func(a App) string {
		rows := jumpPanes(jumpOpenedOver(t, a))
		return rows[rowHaving(rows, "john")]
	}
	if row := box(a); !strings.Contains(row, turnDoneGlyph) {
		t.Fatalf("setup: a finished john is drawn %q, want the done glyph", row)
	}
	a = a.applyFrame(taskFrame("s1", started("a1", "toolu_1", "count lines", "general-purpose", core.TaskAgent)))
	if row := box(a); strings.Contains(row, turnDoneGlyph) {
		t.Errorf("a john with a subagent running is drawn done: %q (the sidebar says ○)", row)
	}
}

// jumpOpenedOver opens the box without insisting it is a fresh app's.
func jumpOpenedOver(t testing.TB, a App) App { return jumpOpened(t, a) }

// A card in the pane takes rows the box would need. The box must not open into
// rows the card pushed off the pane, nor stay up in them.
func TestTheBoxNeverTakesKeysUnderACardThatHidesIt(t *testing.T) {
	refused, drawn := 0, 0
	for _, w := range []int{70, 100, 200} {
		for h := 8; h <= 46; h++ {
			fresh(t)
			a := newRoomApp(t).withSize(w, h).withRoster(
				rpc.SessionStatus{ID: "s1", Name: "alpha"},
				rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateBlocked},
				rpc.SessionStatus{ID: "s3", Name: "scratch"},
			).openDMWith("s2", "sydney").opened(t).applyFrame(rpc.Frame{
				Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
					Kind: core.KindPermissionRequest, RequestID: "r1",
					Tool: &core.ToolCall{Name: "Bash", Display: "rm -rf build/"},
				},
			})
			a, _ = pressKey(a, kmsg(tea.KeyCtrlK))
			if !a.jumpPicker.Open() {
				refused++
				continue
			}
			a = jumpTyped(a, "s")
			rows := jumpPanes(a)
			if got := lipgloss.Height(a.View()); got > h {
				t.Fatalf("%dx%d: the frame is %d rows tall", w, h, got)
			}
			if rowHaving(rows, "⌕") < 0 && rowHaving(rows, jumpCursorName(t, a)) < 0 {
				t.Fatalf("%dx%d: the box is open under a card and none of it is drawn:\n%s", w, h, strings.Join(rows, "\n"))
			}
			drawn++
		}
	}
	if refused == 0 || drawn == 0 {
		t.Errorf("the sweep met %d refusals and %d drawn boxes: both must occur", refused, drawn)
	}
}

// A filter nothing finds is drawn at every size and never wider than the window:
// a row past the width wraps and scrolls the alt screen.
func TestANoMatchFilterFitsEverySize(t *testing.T) {
	for _, w := range []int{70, 100, 200} {
		for h := 6; h <= 46; h++ {
			a := blueFleet(t, w, h)
			a, _ = pressKey(a, kmsg(tea.KeyCtrlK))
			if !a.jumpPicker.Open() {
				continue
			}
			a = jumpTyped(a, strings.Repeat("zq", 20)) // forty runes: the longest query
			v := a.View()
			if got := lipgloss.Height(v); got > h {
				t.Fatalf("%dx%d: the frame is %d rows tall", w, h, got)
			}
			for y, row := range strings.Split(v, "\n") {
				if got := ansi.StringWidth(row); got > a.layout.Width {
					t.Fatalf("%dx%d: row %d is %d cells wide, past the window's %d:\n%s", w, h, y, got, a.layout.Width, ansi.Strip(row))
				}
			}
		}
	}
}

func TestAPastedOrLongQueryIsNormalisedAsTheHelpsIs(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("web\n\tblur"), Paste: true})
	if a.jumpPicker.Query != "web blur" {
		t.Errorf("a pasted query is %q, want %q: whitespace is one space", a.jumpPicker.Query, "web blur")
	}
	a = jumpTyped(a, strings.Repeat("x", 3*queryMax))
	if n := len([]rune(a.jumpPicker.Query)); n != queryMax {
		t.Errorf("the query holds %d runes, want it held to %d", n, queryMax)
	}
	b := blueFleet(t, 140, 40)
	b, _ = b.jump("bl\nu")
	if b.jumpPicker.Query != "bl u" {
		t.Errorf("/jump's argument became %q: it takes the same normalising as typing", b.jumpPicker.Query)
	}
}

func TestAltEscIsOneClose(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEsc, Alt: true})
	if a.jumpPicker.Open() {
		t.Error("alt+esc, two fast escapes read as one key, left the box up")
	}
}

// ⌃C parks the agent in front of you even when a resize has left the box with no
// room: its first press must do something, or a second reads as the emergency exit.
func TestCtrlCStillParksWhenAResizeHidAllOfTheBox(t *testing.T) {
	a := clickAgent(t, blueFleet(t, 200, 40), "s2")
	a = jumpOpened(t, a)
	a = a.withSize(200, 7).resizePanes()
	got, cmd := pressKey(a, kmsg(tea.KeyCtrlC))
	if got.jumpPicker.Open() {
		t.Error("⌃C left the box up")
	}
	if f := sentFrame(t, got, cmd); f.Kind != rpc.FramePark || f.SessionID != "s2" {
		t.Errorf("⌃C wrote %s for %q, want a park of s2", f.Kind, f.SessionID)
	}
}

// One menu at a time, both ways: a /resume walk landing after ⌃K takes the box
// down, and a rewind picker's late reply does not open under it.
func TestALateMenuDoesNotLeaveTwoUp(t *testing.T) {
	a := jumpOpened(t, blueFleet(t, 140, 40))
	if a = a.openResume(twoRows(), 0, false); a.jumpPicker.Open() {
		t.Error("a resume walk that landed after ⌃K left the box up under its picker")
	}

	base := clickAgent(t, blueFleet(t, 200, 40), "s1") // pay-ledger, idle: a conversation a rewind can open on
	reply := rpc.Frame{Kind: rpc.FrameRewindTargetsReply, SessionID: "s1",
		RewindTargets: []rpc.RewindTarget{{UUID: "u1", Text: "first prompt"}}}
	if control := base.rewindTargetsArrived(reply); !control.rewind.Open() {
		t.Fatal("setup: the reply opens no rewind picker even with the box down, so this asserts nothing")
	}
	if got := jumpOpened(t, base).rewindTargetsArrived(reply); got.rewind.Open() {
		t.Error("a rewind picker opened under the jump box")
	}
}

// The columns are asserted: names are cut with an ellipsis at jumpNameCol and the
// team starts in one column on every row.
func TestResultColumnsAlignAndALongNameIsCut(t *testing.T) {
	long, longTeam := "a-very-long-agent-name-that-overruns-its-column", "tm4-a-team-name-past-its-column"
	a := jumpApp(t, 200, 44,
		jumpSpec{name: "alpha", team: "tm1"}, jumpSpec{name: long, team: "tm2"}, jumpSpec{name: "gamma-ray", team: "tm3"},
		jumpSpec{name: "delta", team: longTeam})
	rows := jumpPanes(jumpOpened(t, a))
	var cols []int
	for _, team := range []string{"tm1", "tm2", "tm3"} {
		r := rows[rowHaving(rows, team)]
		cols = append(cols, lipgloss.Width(r[:strings.Index(r, team)]))
	}
	if cols[0] != cols[1] || cols[1] != cols[2] {
		t.Errorf("the team starts in columns %v: it must be one column", cols)
	}
	if r := rows[rowHaving(rows, "tm2")]; !strings.Contains(r, "…") || strings.Contains(r, long) {
		t.Errorf("a name past %d cells is drawn %q, want it cut with an ellipsis", jumpNameCol, r)
	}
	if r := rows[rowHaving(rows, "tm4")]; !strings.Contains(r, "…") || strings.Contains(r, longTeam) {
		t.Errorf("a team past %d cells is drawn %q, want it cut with an ellipsis", jumpTeamCol, r)
	}
	if first := rows[rowHaving(rows, "tm1")]; lipgloss.Width(first[:strings.Index(first, "tm1")])-lipgloss.Width(first[:strings.Index(first, "alpha")]) < jumpNameCol-len("○ alpha") {
		t.Errorf("the name column is narrower than %d cells: %q", jumpNameCol, first)
	}
}
