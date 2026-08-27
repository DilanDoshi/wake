package ui

// The right sidebar, and the two claims it makes: it says what an agent is
// *doing*, and it says it inside exactly the column it was given.
//
// The second one is the load-bearing half and is not a tidiness assertion.
// lipgloss joins columns on their widest line, so a sidebar that draws one row
// too wide does not overflow itself - it shoves the room and the DM sideways
// for as long as that row is on screen. Every width case below measures the
// whole frame rather than inspecting the row that caused it.
//
// The state set comes from rpc's own declaration through declaredStateConstants,
// never from a list written here: a hand-written six is the failure
// decisions.md names, and it is what let a seventh state sort nowhere.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestTheSidebarShowsWhatAnAgentIsDoingAndNotWhatItSaid(t *testing.T) {
	out := Roster{}.View([]Agent{
		{ID: "s1", Name: "sydney", State: rpc.StateWorking, Tool: "Edit", ToolArg: "auth/token.go"},
	}, nil,

		rosterWidth, 10)

	if !strings.Contains(out, "sydney") || !strings.Contains(out, "token.go") {
		t.Errorf("the sidebar does not say what sydney is on:\n%s", out)
	}
}

// The widths include one below anything Layout can produce, and that is
// deliberate. Regions() hands this either 0 or rosterWidth, so a floor applied
// here fires only where no test looks - and a floor is the one thing that can
// make this function return something *wider* than it was asked for.
func TestTheSidebarMeasuresExactlyTheColumnItWasGiven(t *testing.T) {
	agents := []Agent{
		{Name: "sydney", State: rpc.StateWorking, Tool: "Bash", ToolArg: strings.Repeat("very/long/path/", 8)},
		{Name: "a-name-of-maximum-len", State: rpc.StateBlocked},
	}
	// A fleet with nothing wide in it, run through the same assertions. Without
	// it every row happens to reach the column on its own and the padding half
	// of the contract is never exercised - a sidebar of short names would go
	// out ragged, and lipgloss would then join the room against its widest row
	// rather than against its column.
	short := []Agent{{Name: "a", State: rpc.StateIdle}, {Name: "b", State: rpc.StateIdle}}
	for _, w := range []int{4, 12, rosterWidth, 40} {
		measureColumn(t, Roster{}.View(short, nil, w, 6), w, 6)
		out := Roster{}.View(agents, nil, w, 6)
		if got := lipgloss.Width(out); got != w {
			t.Errorf("width %d: measured %d. lipgloss joins columns on their widest line, so one over-wide row shoves the room out of place", w, got)
		}
		if got := lipgloss.Height(out); got != 6 {
			t.Errorf("width %d: height %d, want 6", w, got)
		}
		measureColumn(t, out, w, 6)
	}
}

// measureColumn is the whole contract: exactly this many rows, each exactly
// this wide.
//
// Per line, because lipgloss.Width over the frame is an aggregate - it is the
// *widest* line, so a row cut short and a row left long compensate for each
// other and the frame still measures w. An aggregate a compensating change
// keeps correct has passed over a rendering defect in this project twice.
func measureColumn(t *testing.T, out string, width, height int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != height {
		t.Errorf("width %d: drew %d rows, want %d", width, len(lines), height)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != width {
			t.Errorf("width %d: row %d measured %d (%q): every row is padded and cut to the column, not merely the widest of them", width, i, got, line)
		}
	}
}

// A column nobody has is drawn as nothing at all, and "nothing" has to be the
// empty string rather than an empty column: whoever composes the regions joins
// what it is handed. Both halves are here because they fail differently - a
// zero width leaves rows of blank padding, a zero height leaves one.
func TestASidebarWithNoColumnDrawsNothingRatherThanAnEmptyColumn(t *testing.T) {
	agents := []Agent{{ID: "s1", Name: "a-name-far-wider-than-nothing", State: rpc.StateWorking}}
	for _, geom := range []struct{ w, h int }{{0, 6}, {-1, 6}, {rosterWidth, 0}, {rosterWidth, -1}} {
		if out := (Roster{}).View(agents, nil, geom.w, geom.h); out != "" {
			t.Errorf("View(%d, %d) drew %q: a blank column joined where the layout said to draw none is a column of padding nobody asked for", geom.w, geom.h, out)
		}
	}
}

func TestASidebarTallerThanTheFleetIsPaddedAndOneShorterIsCut(t *testing.T) {
	many := make([]Agent, 30)
	for i := range many {
		many[i] = Agent{ID: fmt.Sprintf("s%d", i), Name: fmt.Sprintf("a%d", i), State: rpc.StateIdle}
	}
	if h := lipgloss.Height(Roster{}.View(many, nil, rosterWidth, 8)); h != 8 {
		t.Errorf("30 agents in an 8-row column drew %d rows", h)
	}
	// The padded half, which the name promises and the cut half cannot show:
	// a short column is joined beside the room, so rows it does not draw are
	// rows of whatever was on screen before it.
	if h := lipgloss.Height(Roster{}.View(many[:2], nil, rosterWidth, 8)); h != 8 {
		t.Errorf("2 agents in an 8-row column drew %d rows, want 8", h)
	}
	if h := lipgloss.Height(Roster{}.View(nil, nil, rosterWidth, 8)); h != 8 {
		t.Errorf("an empty fleet in an 8-row column drew %d rows, want 8", h)
	}

	// An agent is drawn whole or not at all, and the row an odd column has left
	// over goes to the count.
	//
	// **This inverts what the column used to do, deliberately.** It drew agents
	// until it ran out of rows, so a fourth agent's name reached the screen with
	// its tool call cut - and the reason given was that a name is worth more
	// than a tool call, which was true while the alternative was nothing. It is
	// not the alternative any more: the spare row now says how many agents are
	// not on screen, which is the thing an operator cannot work out by looking.
	// A half-drawn agent is a nicety; a fleet that appears to end is a defect.
	working := make([]Agent, 6)
	for i := range working {
		working[i] = Agent{
			ID: fmt.Sprintf("s%d", i), Name: fmt.Sprintf("a%d", i),
			State: rpc.StateWorking, Tool: "Read", ToolArg: fmt.Sprintf("pkg/file%d.go", i),
		}
	}
	out := Roster{}.View(working, nil, rosterWidth, 7)
	measureColumn(t, out, rosterWidth, 7)
	flat := stripANSI(out)
	if !strings.Contains(flat, "+3 more") {
		t.Errorf("a 7-row column over 6 two-row agents does not say how many it left out:\n%s", out)
	}
	// Never a tool call whose agent is not above it. This used to be a property
	// of the *order* rows are appended in; it is now a property of the window,
	// which grows only while a whole agent fits.
	// The line *above*, not the whole output: searching the output cannot see a
	// tool row drawn without its head line, which is the thing this claims.
	lines := strings.Split(flat, "\n")
	for i, line := range lines {
		if !strings.Contains(line, "file") {
			continue
		}
		if i == 0 || !strings.Contains(lines[i-1], "a") {
			t.Errorf("row %d is a tool call and the row above it names no agent:\n%s", i, out)
		}
	}
}

// A name too long for the column is cut, never wrapped. Wrapping is the
// expensive failure: the row does not merely look wrong, it spends two or three
// of the column's rows, and the agents below it fall off the bottom.
func TestALongNameIsCutRatherThanWrappedOverTheAgentsBelowIt(t *testing.T) {
	// 35 characters: the daemon's own maxNameLen is under 36, so this is the
	// widest name that can actually reach this view.
	agents := []Agent{
		{ID: "s1", Name: strings.Repeat("n", 35), State: rpc.StateWorking},
		{ID: "s2", Name: "alex", State: rpc.StateBlocked},
		{ID: "s3", Name: "jamie", State: rpc.StateIdle},
	}
	out := stripANSI(Roster{}.View(agents, nil, rosterWidth, 3))
	for _, name := range []string{"alex", "jamie"} {
		if !strings.Contains(out, name) {
			t.Errorf("a 35-character name pushed %s out of a 3-row sidebar: the long row wrapped instead of being cut, and took the rest of the fleet with it:\n%s", name, out)
		}
	}
}

// Twenty columns cannot hold a path and the tool that opened it, so the tail is
// kept: the head of a path is what every agent in the fleet has in common.
// A command is left alone, because the informative end of one is the front.
func TestAPathIsCutToItsFileAndACommandIsNotMangled(t *testing.T) {
	for _, tc := range []struct {
		name, tool, arg, want, unwanted string
	}{
		{"path", "Edit", "internal/ui/roster.go", "roster.go", "internal"},
		{"command", "Bash", "go test ./internal/ui", "go test", ""},
		{"trailing slash", "Read", "internal/ui/", "internal/ui", ""},
		{"no separator", "Grep", "parseFrame", "parseFrame", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := stripANSI(Roster{}.View([]Agent{
				{ID: "s1", Name: "syd", State: rpc.StateWorking, Tool: tc.tool, ToolArg: tc.arg},
			}, nil,

				rosterWidth, 4))
			if !strings.Contains(out, tc.want) {
				t.Errorf("the sidebar does not say %q:\n%s", tc.want, out)
			}
			if tc.unwanted != "" && strings.Contains(out, tc.unwanted) {
				t.Errorf("the sidebar spent its twenty columns on %q, which every agent in the fleet shares:\n%s", tc.unwanted, out)
			}
		})
	}
}

// The order is Fleet.Agents' order, which is attention order. Ranking again
// here would be a second opinion on the same question, and the roster would
// disagree with the room about which agent is first.
func TestTheSidebarDrawsAgentsInTheOrderItWasGivenThem(t *testing.T) {
	agents := []Agent{
		{ID: "s1", Name: "first", State: rpc.StateIdle},
		{ID: "s2", Name: "second", State: rpc.StateBlocked},
		{ID: "s3", Name: "third", State: rpc.StateWorking},
	}
	out := stripANSI(Roster{}.View(agents, nil, rosterWidth, 4))
	if at, next := strings.Index(out, "first"), strings.Index(out, "second"); at < 0 || next < at {
		t.Errorf("the sidebar re-ordered what it was handed - blocked floated to the top - so it and the room rank separately:\n%s", out)
	}
}

// A state this build does not know still draws a row. A row with no glyph reads
// as an agent with no state, and "" is not hypothetical: Fleet.Observe adds an
// agent the moment an event mentions it, which is before any report describes
// it.
func TestAnAgentWhoseStateHasNotArrivedYetStillDrawsAGlyph(t *testing.T) {
	for _, state := range []string{"", "a state from a later build"} {
		out := stripANSI(Roster{}.View([]Agent{{ID: "s1", Name: "sydney", State: state}}, nil, rosterWidth, 2))
		row, _, _ := strings.Cut(out, "\n")
		if !strings.HasPrefix(row, unknownGlyph+" sydney") {
			t.Errorf("state %q drew %q, want a row beginning %q: a row with no glyph reads as an agent with no state", state, row, unknownGlyph+" sydney")
		}
	}
}

// Derived from rpc's declaration, both ways, and required to be a bijection.
// Membership alone still passes when two states compensate for each other - a
// glyph nobody can reach and a state nobody drew - and two states sharing a
// glyph is the worse of the two, because the sidebar then says an ended agent
// is working and no assertion anywhere sees it.
func TestEveryStateTheDaemonCanReportHasAGlyphOfItsOwn(t *testing.T) {
	declared := declaredStateConstants(t)
	for _, state := range declared {
		if _, ok := stateGlyph[state]; !ok {
			t.Errorf("rpc reports state %q and the sidebar draws no glyph for it: the row would read as an agent with no state", state)
		}
	}

	seen := map[string]string{}
	for state, glyph := range stateGlyph {
		if !slicesContains(declared, state) {
			t.Errorf("the sidebar draws a glyph for %q and rpc declares no such state: a glyph nobody can reach is dead text, and it is what makes deleting a state a two-place edit rather than a one-line one", state)
		}
		if other, clash := seen[glyph]; clash {
			t.Errorf("%q and %q are both drawn %q: one glyph of liveness is the whole of what a row says about state, so two states sharing one makes the sidebar say the wrong thing with no way to tell", state, other, glyph)
		}
		seen[glyph] = state
	}
	if _, taken := seen[unknownGlyph]; taken {
		t.Errorf("a declared state is drawn %q, which is the glyph for a state this build does not know: the two would be indistinguishable", unknownGlyph)
	}
}

// Every glyph is one column, and this is not cosmetic: two columns for one
// glyph shifts every name in the sidebar by one and makes the column arithmetic
// above wrong for exactly the states that are rare enough not to be noticed.
func TestEveryGlyphIsOneColumnWide(t *testing.T) {
	for state, glyph := range stateGlyph {
		if w := lipgloss.Width(glyph); w != 1 {
			t.Errorf("the glyph for %q is %d columns: a wide glyph shifts every name beside it", state, w)
		}
	}
	if w := lipgloss.Width(unknownGlyph); w != 1 {
		t.Errorf("the unknown-state glyph is %d columns", w)
	}
}

func TestJumpingGoesToTheNextAgentThatNeedsYouAndNotSimplyTheNextRow(t *testing.T) {
	agents := Rank([]Agent{
		{ID: "idle", State: rpc.StateIdle},
		{ID: "blocked-a", State: rpc.StateBlocked},
		{ID: "blocked-b", State: rpc.StateBlocked},
	})
	r := Roster{Selected: "blocked-a"}.Next(agents)
	if r.Selected != "blocked-b" {
		t.Errorf("Next selected %q, want blocked-b: ⌃⇧A is 'the next agent needing you', and it was bound to nothing at all in Phase 1 - which is why it was taken out of the legend", r.Selected)
	}
	if again := r.Next(agents).Selected; again != "blocked-a" {
		t.Errorf("Next wrapped to %q, want blocked-a: with nothing further needing you it returns to the first, rather than stopping on the last and appearing broken", again)
	}
}

func TestJumpingWithNothingBlockedSelectsNothingRatherThanPretending(t *testing.T) {
	agents := []Agent{{ID: "s1", State: rpc.StateIdle}}
	if got := (Roster{}).Next(agents).Selected; got != "" {
		t.Errorf("Next selected %q with nothing needing attention: a key that moves a cursor when there is nothing to move it to is a key that lies about the fleet", got)
	}
}

// A cursor on an agent that has since been answered starts over rather than
// counting from a row that is no longer in the list.
func TestJumpingFromAnAgentThatNoLongerNeedsYouLandsOnTheFirstThatDoes(t *testing.T) {
	agents := []Agent{
		{ID: "answered", State: rpc.StateWorking},
		{ID: "blocked-a", State: rpc.StateBlocked},
		{ID: "blocked-b", State: rpc.StateBlocked},
	}
	if got := (Roster{Selected: "answered"}).Next(agents).Selected; got != "blocked-a" {
		t.Errorf("Next selected %q from an agent that is no longer blocked, want blocked-a", got)
	}
}

// Move takes an int, so it has to survive one. Go's % keeps the sign of its
// left operand: a single normalisation leaves a negative index for any delta
// below -len, and a negative index is a panic in the draw loop rather than a
// wrong row.
func TestMovingWrapsInBothDirectionsForAnyDistance(t *testing.T) {
	agents := []Agent{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	for _, tc := range []struct {
		from  string
		delta int
		want  string
	}{
		{"a", 1, "b"},
		{"c", 1, "a"},
		{"a", -1, "c"},
		{"a", -4, "c"},
		{"a", -301, "c"},
		{"b", 7, "c"},
		{"a", 0, "a"},
	} {
		if got := (Roster{Selected: tc.from}).Move(agents, nil, tc.delta).Selected; got != tc.want {
			t.Errorf("Move(%q, %d) selected %q, want %q", tc.from, tc.delta, got, tc.want)
		}
	}
}

func TestMovingWithNoCursorLandsOnTheFirstRowRatherThanCountingFromNowhere(t *testing.T) {
	agents := []Agent{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	for _, delta := range []int{1, -1, 5} {
		if got := (Roster{}).Move(agents, nil, delta).Selected; got != "a" {
			t.Errorf("Move with nothing selected and delta %d selected %q, want a: the cursor arrives on the list rather than at an offset from a row that does not exist", delta, got)
		}
	}
	if got := (Roster{Selected: "gone"}).Move(agents, nil, 1).Selected; got != "a" {
		t.Errorf("Move from an agent no longer in the list selected %q, want a", got)
	}

	// "Nobody" is not the row whose id has not arrived. Both are spelled "",
	// and reading the second as the first puts the cursor an offset away from a
	// row nobody chose.
	withUnknown := []Agent{{ID: "a"}, {ID: ""}, {ID: "c"}}
	if got := (Roster{}).Move(withUnknown, nil, 1).Selected; got != "a" {
		t.Errorf("Move with nothing selected over a fleet holding an id-less row selected %q, want a", got)
	}
}

func TestMovingAnEmptyFleetSelectsNothing(t *testing.T) {
	if got := (Roster{Selected: "a"}).Move(nil, nil, 1); got.Selected != "" {
		t.Errorf("Move over an empty fleet selected %q: there is no row to be on", got.Selected)
	}
}

// The cursor and an agent that needs you are the only two things a row says in
// colour. Both are a single style reference a refactor can drop with nothing
// else changing, and neither failure is visible in any other assertion here.
func TestTheCursorAndABlockedAgentAreDrawnDifferentlyFromEveryoneElse(t *testing.T) {
	forceColour(t)
	agents := []Agent{
		{ID: "cursor", Name: "one", State: rpc.StateIdle},
		{ID: "blocked", Name: "two", State: rpc.StateBlocked},
		{ID: "plain", Name: "three", State: rpc.StateIdle},
	}
	lines := strings.Split(Roster{Selected: "cursor"}.View(agents, nil, rosterWidth, 3), "\n")
	if len(lines) != 3 {
		t.Fatalf("wanted one line per agent, got %d", len(lines))
	}

	seen := map[string]string{}
	for i, name := range []string{"one", "two", "three"} {
		// The SGR sequence itself, not everything in front of the name. The
		// glyph sits between them and differs per state, so cutting on the name
		// compares glyphs and passes whatever the styles are - which is a test
		// that cannot fail wearing the name of one that can.
		esc := ansiPattern.FindString(lines[i])
		if esc == "" {
			t.Fatalf("row %q carries no styling at all at a forced colour profile: %q", name, lines[i])
		}
		if other, clash := seen[esc]; clash {
			t.Errorf("%q and %q are drawn identically: the cursor, an agent stopped waiting for you, and an agent doing neither are the three things this column distinguishes", name, other)
		}
		seen[esc] = name
	}
}

// An unselected roster selects nobody, which is not the same as selecting the
// agent whose id happens to be empty. Every row would otherwise be drawn as the
// cursor for as long as nothing was selected - which is the state a fleet
// starts in.
func TestARosterWithNoCursorDrawsNoCursor(t *testing.T) {
	forceColour(t)
	plain := Roster{}.View([]Agent{{Name: "one", State: rpc.StateIdle}}, nil, rosterWidth, 1)
	selected := Roster{Selected: "s1"}.View([]Agent{{ID: "s1", Name: "one", State: rpc.StateIdle}}, nil, rosterWidth, 1)
	if plain == selected {
		t.Errorf("an agent with no id is drawn as the cursor when nothing is selected:\n%q", plain)
	}
}

// The badge is why a row is worth looking at, so it is budgeted before the name
// is cut rather than after. Truncating the assembled line drops the badge
// first, every time, on exactly the rows that have one.
func TestAnUnreadCountSurvivesANameTooLongForTheColumn(t *testing.T) {
	out := stripANSI(Roster{}.View([]Agent{
		{ID: "s1", Name: strings.Repeat("n", 30), State: rpc.StateIdle, Unread: 7},
	}, nil,

		rosterWidth, 1))
	if !strings.HasSuffix(strings.TrimRight(out, " "), "7") {
		t.Errorf("a 30-character name pushed the unread count off the row:\n%q", out)
	}
	measureColumn(t, out, rosterWidth, 1)

	// And in a column too narrow for both, the count is what is left. lipgloss
	// reads a non-positive MaxWidth as "no maximum" and hands back the whole
	// line, so without a cut of its own the name would be kept here and the
	// badge dropped by the outer bound - the exact inversion this orders.
	// Two columns, not three: at three there is still one column left for the
	// name and lipgloss truncates it correctly on its own, so the guard is
	// never asked. The count and a space is exactly the column here.
	narrow := stripANSI(Roster{}.View([]Agent{
		{ID: "s1", Name: "sydney", State: rpc.StateIdle, Unread: 7},
	}, nil,

		2, 1))
	if !strings.Contains(narrow, "7") {
		t.Errorf("a 2-column sidebar drew %q and not the count: the count is the whole reason the row is worth looking at", narrow)
	}
}

// Nought is not a count. Without this every agent in the fleet carries a `0`,
// which is thirty rows saying nothing in the space reserved for the one thing
// the sidebar has to shout.
func TestAnAgentWithNothingUnreadCarriesNoBadge(t *testing.T) {
	out := stripANSI(Roster{}.View([]Agent{
		{ID: "s1", Name: "sydney", State: rpc.StateIdle},
	}, nil,

		rosterWidth, 1))
	if strings.Contains(out, "0") {
		t.Errorf("an agent with nothing unread drew %q", out)
	}
}

// Three columns, whatever the number. A sidebar this narrow cannot spend four
// of them on a count nobody needs precisely.
func TestALargeUnreadCountIsBoundedToThreeColumns(t *testing.T) {
	for n, want := range map[int]string{1: "1", 99: "99", 100: "99+", 1234: "99+"} {
		if got := unreadBadge(n); got != want {
			t.Errorf("unreadBadge(%d) = %q, want %q", n, got, want)
		}
		if w := lipgloss.Width(unreadBadge(n)); w > 3 {
			t.Errorf("unreadBadge(%d) is %d columns: at sixteen and twenty columns that is the name", n, w)
		}
	}
}

// slicesContains is slices.Contains under a local name, so this file's imports
// stay the four the assertions need.
func slicesContains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
