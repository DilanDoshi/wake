package ui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// inWindow is a run as the terminal runs it: told a w×h window's size through
// its margins, and settled, so the frame is laid out for it.
func inWindow(a App, w, h int) Margined {
	m, _ := Margined{Model: a}.Update(tea.WindowSizeMsg{Width: w, Height: h})
	next := m.(Margined)
	next.Model = next.app().applyGeometry()
	return next
}

// app is the App inside the margins.
func (m Margined) app() App { return m.Model.(App) }

// bareFrame is a told the size of the frame a w×h window holds, with no margins
// of its own: what Margined must hand it.
func bareFrame(a App, w, h int) App {
	m, _ := a.Update(tea.WindowSizeMsg{Width: w - 2*SideMargin, Height: h})
	return m.(App).applyGeometry()
}

// requireEdgesBlank fails on any row of a w-column window that draws in its first
// or its last column. The first cell must be a bare blank - a styled one is a
// background against the edge - and Bubble Tea erases a short row to its end.
func requireEdgesBlank(t *testing.T, name, view string, w int) {
	t.Helper()
	for i, row := range strings.Split(view, "\n") {
		if !strings.HasPrefix(row, " ") {
			t.Fatalf("%s: row %d draws in the window's first column: %q", name, i, ansi.Strip(row))
		}
		if got := ansi.StringWidth(row); got > w-1 {
			t.Fatalf("%s: row %d is %d cells, reaching the last column of a %d-column window: %q", name, i, got, w, ansi.Strip(row))
		}
	}
}

// The owner's ask: no text pressed against the window's left or right edge. Every
// surface lies inside the frame, so every one is held here - the room beside the
// roster with its usage bar and a friend, a grid with a stack, a conversation
// alone, the board both ways, and a window too narrow for the sidebars.
func TestNoRowOfTheWindowTouchesItsEdges(t *testing.T) {
	room := func(t *testing.T) App {
		return friendRoom(t, true, "skip", &keptFriends{}).withUsageReport(aReading, "sydney", "john")
	}
	tiled := func(t *testing.T) App {
		a, _, _ := boardApp(t).boardKey(tea.KeyMsg{Type: tea.KeyTab})
		return a
	}
	for _, tc := range []struct {
		name string
		app  func(*testing.T) App
		w, h int
	}{
		{"room, roster, usage bar and friend", room, 100, 24},
		{"three columns and a stack", threeColumns, 200, 40},
		{"a conversation alone", func(t *testing.T) App { return splitApp(t, 100, 30, 20) }, 100, 30},
		{"the board's rows", boardApp, 120, 30},
		{"the board's tiles", tiled, 120, 30},
		{"under the sidebars' width", func(t *testing.T) App { return newRoomApp(t).withAgents("sydney") }, 90, 30},
	} {
		requireEdgesBlank(t, tc.name, inWindow(tc.app(t), tc.w, tc.h).View(), tc.w)
	}
}

// A window no wider than its margins lays the frame out at nothing, the path a
// zero width always took, and draws without panicking; a cell wider gets that cell.
func TestAWindowNarrowerThanItsMarginsDrawsNoFrame(t *testing.T) {
	for w := range 2*SideMargin + 2 {
		m := inWindow(newRoomApp(t).withAgents("sydney"), w, 20)
		if got := m.app().layout.Width; got != max(w-2*SideMargin, 0) {
			t.Errorf("a %d-column window laid the frame out %d wide", w, got)
		}
		_ = m.View()
	}
}

// The frame is laid out for the window less both margins; the height is the
// window's own.
func TestTheWindowsSizeReachesTheAppLessItsMargins(t *testing.T) {
	m := inWindow(newRoomApp(t).withAgents("sydney"), 100, 30)
	if l := m.app().layout; l.Width != 100-2*SideMargin || l.Height != 30 {
		t.Errorf("a 100×30 window laid the frame out %d×%d, want %d×30", l.Width, l.Height, 100-2*SideMargin)
	}
	pad := strings.Repeat(" ", SideMargin)
	if got, want := m.View(), pad+strings.ReplaceAll(m.app().View(), "\n", "\n"+pad); got != want {
		t.Errorf("the window is not the frame one margin in:\n%s\nwant:\n%s", ansi.Strip(got), ansi.Strip(want))
	}
}

// gesture is mouse messages at window cells. Each is aimed at a boundary, so a
// shift that is missing or doubled lands on the cell's neighbour and does
// something else.
type gesture struct {
	name string
	app  func(*testing.T) App
	w, h int
	msgs func(App) []tea.MouseMsg // in window cells, given the bare frame's layout
	did  func(before, after App) bool
}

// windowPress is a left press on frame cell x, as the window reports it.
func windowPress(x, y int) tea.MouseMsg { return pressAt(x+SideMargin, y) }

// windowAt is a mouse message on frame cell x, as the window reports it.
func windowAt(action tea.MouseAction, button tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: action, Button: button, X: x + SideMargin, Y: y}
}

// windowDrag is a press, motion and a release across frame cells x0..x1 of row y,
// as the window reports them.
func windowDrag(x0, x1, y int) []tea.MouseMsg {
	out := []tea.MouseMsg{windowPress(x0, y)}
	for x := x0 + 1; x <= x1; x++ {
		out = append(out, windowAt(tea.MouseActionMotion, tea.MouseButtonLeft, x, y))
	}
	return append(out, windowAt(tea.MouseActionRelease, tea.MouseButtonNone, x1, y))
}

// toFrame is a window cell as the frame reads it: one margin left, and never off
// the frame - a press in a margin is the edge cell's.
func toFrame(m tea.MouseMsg, frameWidth int) tea.MouseMsg {
	m.X = clamp(m.X-SideMargin, 0, max(frameWidth-1, 0))
	return m
}

func mouseGestures() []gesture {
	tiled := func(t *testing.T) App {
		a, _, _ := boardApp(t).boardKey(tea.KeyMsg{Type: tea.KeyTab})
		return a
	}
	drafted := func(t *testing.T) App { return splitApp(t, 200, 40, 4).withDraft("hello world") }
	return []gesture{
		{"a divider drag", threeColumns, 200, 40, func(a App) []tea.MouseMsg {
			at := a.layout.PaneLeft(a.regions(), 1) - dividerWidth
			return windowDrag(at, at+6, 10)
		}, func(b, a App) bool { return !reflect.DeepEqual(b.layout.Weights, a.layout.Weights) }},
		{"a transcript drag", threeColumns, 200, 40, func(a App) []tea.MouseMsg {
			left := a.layout.PaneLeft(a.regions(), 2)
			return windowDrag(left, left+6, 10)
		}, func(b, a App) bool { return a.sel != b.sel }},
		{"a query-box drag", drafted, 200, 40, func(a App) []tea.MouseMsg {
			r := a.regions()
			top, _, _, _, _ := a.composerRegion("", r.Room(), 0, a.paneHeight())
			left := a.layout.PaneLeft(r, 0) + composerTextLeft
			return windowDrag(left, left+4, top)
		}, func(_, a App) bool { return a.composerSelectedText() == "hello" }},
		{"the wheel over an unfocused pane's last column", threeColumns, 200, 40, func(a App) []tea.MouseMsg {
			r := a.regions()
			return []tea.MouseMsg{windowAt(tea.MouseActionPress, tea.MouseButtonWheelUp, a.layout.PaneLeft(r, 1)+r.Cols[1]-1, 10)}
		}, func(b, a App) bool { return a.transcriptIn("s1").scroll != b.transcriptIn("s1").scroll }},
		{"a click on the roster's first column", threeColumns, 200, 40, func(a App) []tea.MouseMsg {
			x := a.layout.Width - a.regions().Roster
			return []tea.MouseMsg{windowPress(x, 2), windowAt(tea.MouseActionRelease, tea.MouseButtonNone, x, 2)}
		}, func(b, a App) bool { return a.focus != b.focus || a.roster.Selected != b.roster.Selected }},
		{"a press in the left margin", threeColumns, 200, 40, func(App) []tea.MouseMsg {
			return []tea.MouseMsg{pressAt(0, 10)}
		}, func(b, a App) bool { return a.focus != b.focus }},
		{"a press in the right margin", threeColumns, 200, 40, func(App) []tea.MouseMsg {
			return []tea.MouseMsg{pressAt(199, 2), {Action: tea.MouseActionRelease, X: 199, Y: 2}}
		}, func(b, a App) bool { return a.focus != b.focus || a.roster.Selected != b.roster.Selected }},
		{"the wheel on the roster's first column", wheelFleet, 100, 30, func(a App) []tea.MouseMsg {
			return []tea.MouseMsg{windowAt(tea.MouseActionPress, tea.MouseButtonWheelDown, a.layout.Width-a.regions().Roster, 10)}
		}, func(b, a App) bool { return a.rosterView != b.rosterView }},
		{"a click on the gap's last column between two tiles", tiled, 120, 30, func(a App) []tea.MouseMsg {
			l := a.boardTileLayout(a.boardAgents())
			x, y := l.cellW+tileGap-1, boardChromeRows+1
			return []tea.MouseMsg{windowPress(x, y)}
		}, func(b, a App) bool { return b.board.Up && !a.board.Up }},
	}
}

// Every gesture the mouse makes reaches the frame one margin left of the cell it
// was made on: a divider, a drag across text or the query box, the wheel over a
// pane or the roster, a roster click, the board. So a window's mouse does exactly
// what the bare frame does at that cell - and a press in a margin is the edge
// cell's.
func TestEveryMouseGestureReachesTheFrameOneMarginIn(t *testing.T) {
	for _, g := range mouseGestures() {
		t.Run(g.name, func(t *testing.T) {
			holdClock(t, time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)) // one working word for both builds
			bare := bareFrame(g.app(t), g.w, g.h)
			win := inWindow(g.app(t), g.w, g.h)
			before := bare
			for _, msg := range g.msgs(bare) {
				next, _ := bare.Update(toFrame(msg, bare.layout.Width))
				bare = next.(App)
				m, _ := win.Update(msg)
				win = m.(Margined)
			}
			if !g.did(before, bare) {
				t.Fatalf("the gesture did nothing to the bare frame, so it proves nothing")
			}
			got, want := ansi.Strip(win.app().View()), ansi.Strip(bare.View())
			if got != want || win.app().focus != bare.focus || win.app().sel != bare.sel {
				t.Errorf("the window's frame differs from the bare frame's after the same gesture one margin in:\nwindow:\n%s\nbare:\n%s", got, want)
			}
		})
	}
}

// Mid window-drag the frame is still laid out for the old width until the settle.
// A grown window draws it short of the new edge, so a press out there lands on the
// newest frame's edge, past the drawn one, and does nothing - never on the old
// frame's edge, the roster, far from the pointer. A shrunk one clips the old frame
// to the newest width, so its right margin holds before the settle too.
func TestAWindowDragBoundsThePressAndTheFrameByTheNewestWidth(t *testing.T) {
	press := func(m Margined, x, y int) Margined {
		for _, msg := range []tea.MouseMsg{pressAt(x, y), {Action: tea.MouseActionRelease, X: x, Y: y}} {
			next, _ := m.Update(msg)
			m = next.(Margined)
		}
		return m
	}
	grown := func(t *testing.T) Margined {
		m := inWindow(newRoomApp(t).withAgents("sydney", "john"), 100, 30)
		next, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30}) // no settle
		return next.(Margined)
	}
	m := grown(t)
	if m.app().layout.Width != 98 {
		t.Fatalf("the grown window's frame is %d wide before the settle, want the old 98", m.app().layout.Width)
	}
	if edge := press(grown(t), 98, 2).app(); edge.focus == "" && edge.roster.Selected == "" {
		t.Fatal("a press on the old frame's last column opened nothing, so the press below proves nothing")
	}
	if got := press(m, 139, 2).app(); got.focus != m.app().focus || got.roster.Selected != m.app().roster.Selected {
		t.Errorf("a press in the grown window's right margin acted on the old frame (focus %q, roster %q)", got.focus, got.roster.Selected)
	}

	shrunk, _ := inWindow(newRoomApp(t).withAgents("sydney", "john"), 140, 30).Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	requireEdgesBlank(t, "a window shrunk before the settle", shrunk.View(), 100)
}
