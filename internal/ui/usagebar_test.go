package ui

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// usageResets is a reset at 4:09pm wherever the test runs.
var usageResets = time.Date(2026, 10, 24, 16, 9, 0, 0, time.Local)

// withUsageReport pushes a report of the named agents (s1, s2, ...) and a reading.
func (a App) withUsageReport(u *rpc.SessionUsage, names ...string) App {
	st := rpc.Status{Running: true, SessionUsage: u}
	for i, name := range names {
		st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: fmt.Sprintf("s%d", i+1), Name: name, State: rpc.StateIdle})
	}
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
}

// sidebarRows is the right sidebar's pane rows as drawn, styles and all.
func sidebarRows(a App) []string {
	r := a.regions()
	w := r.Roster - dividerWidth
	rows := strings.Split(a.View(), "\n")[:a.paneHeight()]
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = ansi.Cut(row, a.layout.Width-w, a.layout.Width)
	}
	return out
}

// usageBlock is the sidebar's last three rows, as text.
func usageBlock(a App) []string {
	rows := sidebarRows(a)
	out := rows[len(rows)-3:]
	for i, r := range out {
		out[i] = ansi.Strip(r)
	}
	return out
}

var sgrRun = regexp.MustCompile("\x1b\\[([0-9;]*)m([^\x1b]*)")

// cellsIn counts the bar cells of row drawn in a colour's dark hue.
func cellsIn(row string, c lipgloss.AdaptiveColor) int {
	hex := strings.TrimPrefix(c.Dark, "#")
	r, _ := strconv.ParseUint(hex[0:2], 16, 8)
	g, _ := strconv.ParseUint(hex[2:4], 16, 8)
	b, _ := strconv.ParseUint(hex[4:6], 16, 8)
	want := fmt.Sprintf("38;2;%d;%d;%d", r, g, b)
	n := 0
	for _, m := range sgrRun.FindAllStringSubmatch(row, -1) {
		if strings.Contains(m[1], want) {
			n += strings.Count(m[2], usageBarGlyph)
		}
	}
	return n
}

// trueColor draws in full colour on a dark ground for one test.
func trueColor(t *testing.T) {
	t.Helper()
	prev, dark := lipgloss.ColorProfile(), lipgloss.HasDarkBackground()
	lipgloss.SetColorProfile(0) // termenv.TrueColor
	lipgloss.SetHasDarkBackground(true)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev); lipgloss.SetHasDarkBackground(dark) })
}

func TestTheUsageBarIsHiddenUntilAReportNamesAReading(t *testing.T) {
	a := newRoomApp(t).withSize(100, 24).withUsageReport(nil, "sydney", "john")
	if got := a.rosterHeight(); got != a.paneHeight() {
		t.Errorf("the roster has %d rows with no reading, want all %d", got, a.paneHeight())
	}
	for _, row := range sidebarRows(a) {
		if s := ansi.Strip(row); strings.Contains(s, "session") || strings.Contains(s, sectionRule+sectionRule+sectionRule) {
			t.Errorf("the sidebar drew a usage row with no reading: %q", s)
		}
	}
}

func TestTheUsageBarDrawsTheReadingUnderTheRoster(t *testing.T) {
	trueColor(t)
	a := newRoomApp(t).withSize(100, 24).withUsageReport(&rpc.SessionUsage{Percent: 47, Resets: usageResets}, "sydney", "john")
	block := usageBlock(a)
	if block[0] != strings.Repeat(sectionRule, 23) {
		t.Errorf("the block's first row is %q, want a rule across the sidebar", block[0])
	}
	if !strings.HasSuffix(block[1], " 47%") || strings.Count(block[1], "█") != 18 {
		t.Errorf("the bar row is %q, want 18 bar cells and the figure", block[1])
	}
	if got := strings.TrimRight(block[2], " "); got != "session · ↻ 4:09pm" {
		t.Errorf("the caption is %q, want the reset", got)
	}
	bar := sidebarRows(a)[a.paneHeight()-2]
	if fill, track := cellsIn(bar, UsageFill), cellsIn(bar, UsageTrack); fill != 8 || track != 10 {
		t.Errorf("47%% of 18 cells drew %d filled and %d track, want 8 and 10", fill, track)
	}
	if got := a.rosterHeight(); got != a.paneHeight()-3 {
		t.Errorf("the roster keeps %d rows beside the block, want %d", got, a.paneHeight()-3)
	}
}

// Claude's own verdict colours the fill; a reset that passed empties the bar.
func TestTheUsageBarWearsClaudesVerdictAndEmptiesAtTheReset(t *testing.T) {
	trueColor(t)
	for _, tc := range []struct {
		name    string
		u       rpc.SessionUsage
		colour  lipgloss.AdaptiveColor
		fill    int
		figure  string
		caption string
	}{
		{"warned", rpc.SessionUsage{Percent: 91, Resets: usageResets, Warned: true}, Warn, 16, " 91%", "session · ↻ 4:09pm"},
		{"limited", rpc.SessionUsage{Percent: 100, Resets: usageResets, Limited: true}, Error, 18, "100%", "session · ↻ 4:09pm"},
		{"lapsed", rpc.SessionUsage{Percent: 91, Resets: usageResets, Warned: true, Lapsed: true}, Warn, 0, "   —", "session · reset 4:09pm"},
	} {
		u := tc.u
		a := newRoomApp(t).withSize(100, 24).withUsageReport(&u, "sydney")
		bar := sidebarRows(a)[a.paneHeight()-2]
		if got := cellsIn(bar, tc.colour); got != tc.fill {
			t.Errorf("%s: %d cells in the verdict's colour, want %d", tc.name, got, tc.fill)
		}
		if got := cellsIn(bar, UsageTrack); got != 18-tc.fill {
			t.Errorf("%s: %d track cells, want %d", tc.name, got, 18-tc.fill)
		}
		block := usageBlock(a)
		if !strings.HasSuffix(block[1], tc.figure) || strings.TrimRight(block[2], " ") != tc.caption {
			t.Errorf("%s: drew %q / %q, want the figure %q and %q", tc.name, block[1], block[2], tc.figure, tc.caption)
		}
	}
}

// A row wider than the sidebar widens the frame past the terminal, a narrower
// one lets the pane beside it join early, and a column taller than the pane
// pushes the strip off its row.
func TestEveryUsageRowIsTheSidebarWideAndTheStripKeepsItsRow(t *testing.T) {
	trueColor(t)
	a := newRoomApp(t).withSize(100, 24).withUsageReport(&rpc.SessionUsage{Percent: 100, Resets: time.Date(2026, 10, 24, 12, 30, 0, 0, time.Local), Limited: true, Lapsed: true}, "sydney")
	if len(a.daemon.rows) != 3 {
		t.Fatalf("rendered %d usage rows, want 3", len(a.daemon.rows))
	}
	for _, row := range a.daemon.rows {
		if got := ansi.StringWidth(row); got != 23 {
			t.Errorf("a usage row is %d cells, want the sidebar's 23: %q", got, ansi.Strip(row))
		}
	}
	rows := strings.Split(a.View(), "\n")
	for y, row := range rows {
		if got := ansi.StringWidth(row); got > a.layout.Width {
			t.Errorf("frame row %d is %d cells, wider than the %d-column terminal", y, got, a.layout.Width)
		}
	}
	want := strings.TrimRight(ansi.Strip(a.strip(a.fleet.OnRoster())), " ")
	if got := strings.TrimRight(ansi.Strip(rows[a.paneHeight()]), " "); got != want {
		t.Errorf("row %d is %q, want the strip %q under the panes", a.paneHeight(), got, want)
	}
}

// The rows are rendered per report and per layout, never per frame - so a
// sidebar that comes back with no report between draws them at its width.
func TestAResizeWithNoReportRedrawsTheRowsAtTheNewSidebarWidth(t *testing.T) {
	a := newRoomApp(t).withSize(90, 24).withUsageReport(&rpc.SessionUsage{Percent: 47, Resets: usageResets}, "sydney")
	if a.regions().Roster != 0 {
		t.Fatalf("a 90-column window draws a sidebar of %d", a.regions().Roster)
	}
	a = a.withSize(140, 30).applyGeometry() // the settle, as Bubble Tea delivers it
	for _, row := range a.daemon.rows {
		if got := ansi.StringWidth(row); got != a.regions().Roster-dividerWidth {
			t.Errorf("after the resize a usage row is %d cells, want the sidebar's %d: %q", got, a.regions().Roster-dividerWidth, ansi.Strip(row))
		}
	}
	rows := sidebarRows(a)
	if got := strings.TrimRight(ansi.Strip(rows[len(rows)-1]), " "); got != "session · ↻ 4:09pm" {
		t.Errorf("after the resize the caption is %q", got)
	}
}

func TestASeedWithAReadingDrawsTheBarOnTheFirstFrame(t *testing.T) {
	fresh(t)
	seed := &rpc.Status{Running: true, SessionUsage: &rpc.SessionUsage{Percent: 47, Resets: usageResets},
		Sessions: []rpc.SessionStatus{{ID: "s1", Name: "sydney", State: rpc.StateIdle}}}
	a := NewRoomApp(newRecorder(t), Stream{}, seed).withSize(100, 24)
	if got := strings.TrimRight(usageBlock(a)[2], " "); got != "session · ↻ 4:09pm" {
		t.Errorf("the first frame's caption is %q, want the seeded reading", got)
	}
}

// A pane too short to keep a roster row beside the block draws the roster alone.
func TestAPaneTooShortForTheBlockKeepsTheRoster(t *testing.T) {
	u := &rpc.SessionUsage{Percent: 47, Resets: usageResets}
	a := newRoomApp(t).withSize(100, 3+noticeHeight+stripHeight).withUsageReport(u, "sydney")
	if got := a.rosterHeight(); got != a.paneHeight() {
		t.Errorf("a %d-row pane gave the roster %d rows, want all of them", a.paneHeight(), got)
	}
	a = a.withSize(100, 4+noticeHeight+stripHeight).applyGeometry()
	if got := a.rosterHeight(); got != 1 {
		t.Errorf("a 4-row pane gave the roster %d rows, want 1 above the block", got)
	}
}

// rosterNames is a scrolled fleet: more agents than the pane has rows, with
// the cursor on the last so the window is not the top of the list.
func rosterNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("agent%02d", i+1)
	}
	return out
}

// everyClickLandsOnTheRowDrawnThere holds the roster's hit test to its draw: a
// click on a row naming an agent opens that agent, and a click on a usage row
// opens nothing.
func everyClickLandsOnTheRowDrawnThere(t *testing.T, a App) {
	t.Helper()
	rows := sidebarRows(a)
	block := len(a.usageDrawn())
	for y, row := range rows {
		text := strings.TrimSpace(ansi.Strip(row))
		got, _, ok := a.clickedAgent(y)
		switch {
		case y >= len(rows)-block:
			if ok {
				t.Errorf("a click on usage row %d (%q) opened %s", y, text, got.Name)
			}
		case strings.HasPrefix(text, "○ "):
			if want := strings.TrimPrefix(text, "○ "); !ok || got.Name != want {
				t.Errorf("a click on row %d, drawn as %q, opened %q", y, want, got.Name)
			}
		}
	}
}

func TestAClickOnAScrolledRosterLandsOnTheRowDrawnThereBesideTheBlock(t *testing.T) {
	names := rosterNames(30)
	a := newRoomApp(t).withSize(100, 24).withUsageReport(&rpc.SessionUsage{Percent: 47, Resets: usageResets}, names...)
	a = pick(a, "s30")
	everyClickLandsOnTheRowDrawnThere(t, a)
}

// Hidden until the first reading means the roster loses rows the moment one
// lands, and the clicks follow: the bottom row was an agent and is the bar.
func TestTheFirstReadingShortensTheRosterAndClicksFollow(t *testing.T) {
	names := rosterNames(30)
	a := pick(newRoomApp(t).withSize(100, 24).withUsageReport(nil, names...), "s30")
	bottom := -1
	for y, row := range sidebarRows(a) {
		if strings.HasPrefix(strings.TrimSpace(ansi.Strip(row)), "○ ") {
			bottom = y
		}
	}
	if bottom < a.paneHeight()-3 {
		t.Fatalf("the lowest agent row is %d of %d, above where the bar will go", bottom, a.paneHeight())
	}
	if _, _, ok := a.clickedAgent(bottom); !ok {
		t.Fatalf("before any reading row %d opened nobody", bottom)
	}
	everyClickLandsOnTheRowDrawnThere(t, a)

	a = a.withUsageReport(&rpc.SessionUsage{Percent: 47, Resets: usageResets}, names...)
	if got, _, ok := a.clickedAgent(bottom); ok {
		t.Errorf("after the first reading row %d is the bar, and opened %s", bottom, got.Name)
	}
	everyClickLandsOnTheRowDrawnThere(t, a)
}
