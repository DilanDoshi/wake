package ui

// The completion menu's window: ↓ walks every offer, not only the rows drawn,
// and the drawn rows follow the cursor by the least distance (decisions.md,
// 2026-11-12). Each test reads the menu as drawn, so a window that is wrong
// shows as the cursor row not being on screen.

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

var (
	downKey = tea.KeyMsg{Type: tea.KeyDown}
	upKey   = tea.KeyMsg{Type: tea.KeyUp}

	aboveCount = regexp.MustCompile(`\+(\d+) above`)
	belowCount = regexp.MustCompile(`\+(\d+) below`)
)

// longMenu is a conversation advertising n commands, with `/` typed: n offers
// of its own and Wake's verbs after them.
func longMenu(t *testing.T, n int) App {
	t.Helper()
	fresh(t)
	return dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40).
		advertising("s1", commandNames(n)...).withDraft("/")
}

// commandNames is n distinct words that sort in the order they are advertised.
func commandNames(n int) []string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("cmd-%03d", i)
	}
	return names
}

func walk(a App, k tea.KeyMsg, n int) App {
	for range n {
		a, _ = pressKey(a, k)
	}
	return a
}

// drawn is the menu's offer rows as the pane draws them, without the key line,
// and which of them the cursor is on (-1 for none). The cursor's lead is
// stripped so the rows compare with the offers.
func drawn(t *testing.T, a App) (rows []string, cursor int) {
	t.Helper()
	view := a.completionView(100, a.focus)
	if view == "" {
		t.Fatal("no menu is drawn")
	}
	lines := strings.Split(ansi.Strip(view), "\n")
	cursor = -1
	for i, line := range lines[:len(lines)-1] {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, strings.TrimSpace(cardCursor)); ok {
			cursor = i
			line = strings.TrimSpace(rest)
		}
		rows = append(rows, line)
	}
	return rows, cursor
}

// menuKeyLine is the row under the offers.
func menuKeyLine(a App) string {
	lines := strings.Split(ansi.Strip(a.completionView(100, a.focus)), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// countIn is the number a count wording carries, 0 when it is not said.
func countIn(re *regexp.Regexp, line string) int {
	m := re.FindStringSubmatch(line)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// onScreen fails unless the cursored offer is drawn, highlighted, as itself.
func onScreen(t *testing.T, a App, what string) {
	t.Helper()
	rows, at := drawn(t, a)
	want := a.completion.offers[a.completion.cursor]
	if at < 0 || rows[at] != want {
		t.Fatalf("%s: the cursor is on %q (offer %d) and the menu draws %q at its cursor row:\n%s",
			what, want, a.completion.cursor, rows, a.completionView(100, a.focus))
	}
}

// The matches are all kept: the bound is a window over them, not a cut of them.
func TestTheMenuKeepsEveryMatch(t *testing.T) {
	a := longMenu(t, 60)
	if got, want := len(a.completion.offers), 60+len(wakeVerbs()); got != want {
		t.Errorf("a bare / over 60 advertised commands holds %d offers, want all %d: the rest cannot be walked to", got, want)
	}
}

// ↓ past the last drawn row reaches the next offer, and the offer is drawn.
func TestDownWalksPastTheLastDrawnRowToEveryOffer(t *testing.T) {
	a := longMenu(t, 60)
	total := 60 + len(wakeVerbs())
	if total <= completionRows {
		t.Fatalf("%d offers fit the %d rows, so this test cannot see the window", total, completionRows)
	}
	for i := 1; i < total; i++ {
		a, _ = pressKey(a, downKey)
		if a.completion.cursor != i {
			t.Fatalf("↓ number %d left the cursor on %d: the walk stopped before the last offer", i, a.completion.cursor)
		}
		onScreen(t, a, fmt.Sprintf("after ↓ number %d", i))
	}
	a, _ = pressKey(a, downKey)
	if a.completion.cursor != total-1 {
		t.Errorf("↓ at the last offer moved the cursor to %d, want it clamped on %d", a.completion.cursor, total-1)
	}
}

// ↑ comes back the same way, and clamps at the first offer.
func TestUpWalksBackToTheFirstOffer(t *testing.T) {
	a := longMenu(t, 60)
	total := 60 + len(wakeVerbs())
	a = walk(a, downKey, total-1)
	for i := total - 2; i >= 0; i-- {
		a, _ = pressKey(a, upKey)
		if a.completion.cursor != i {
			t.Fatalf("↑ left the cursor on %d, want %d", a.completion.cursor, i)
		}
		onScreen(t, a, fmt.Sprintf("after ↑ to %d", i))
	}
	a, _ = pressKey(a, upKey)
	if a.completion.cursor != 0 {
		t.Errorf("↑ at the first offer moved the cursor to %d, want it clamped on 0", a.completion.cursor)
	}
}

// The window moves the least that keeps the cursor on screen: not while the
// cursor walks inside it, one row when it leaves an edge, and back the same
// way - never centred on the cursor.
func TestTheWindowMovesTheLeastDistance(t *testing.T) {
	a := longMenu(t, 60)
	first := func(a App) string { rows, _ := drawn(t, a); return rows[0] }
	offers := a.completion.offers

	a = walk(a, downKey, completionRows-1)
	if first(a) != offers[0] {
		t.Errorf("the cursor on the last drawn row moved the window to start at %q, want it still at %q", first(a), offers[0])
	}
	a, _ = pressKey(a, downKey)
	if first(a) != offers[1] {
		t.Errorf("↓ off the last row put %q first, want the window one row on at %q", first(a), offers[1])
	}
	a = walk(a, upKey, completionRows-1) // the cursor is back on the window's first row, offer 1
	if first(a) != offers[1] {
		t.Errorf("↑ inside the window moved it to start at %q, want it still at %q", first(a), offers[1])
	}
	a, _ = pressKey(a, upKey)
	if first(a) != offers[0] {
		t.Errorf("↑ off the first row put %q first, want the window back at %q", first(a), offers[0])
	}
}

// The key line says how many offers lie each way, and they add up.
func TestTheKeyLineCountsWhatLiesAboveAndBelow(t *testing.T) {
	a := longMenu(t, 60)
	total := 60 + len(wakeVerbs())
	for _, at := range []int{0, 10, completionRows, 40, total - 1} {
		b := walk(a, downKey, at)
		line := menuKeyLine(b)
		above, below := countIn(aboveCount, line), countIn(belowCount, line)
		// The window opens on the first offer and moves only when the cursor
		// walks off its last row, so a cursor at n has n-rows+1 above it.
		wantAbove := max(at-completionRows+1, 0)
		if above != wantAbove || below != total-completionRows-wantAbove {
			t.Errorf("cursor on %d: the key line %q counts %d above and %d below, want %d and %d of %d with %d drawn",
				at, line, above, below, wantAbove, total-completionRows-wantAbove, total, completionRows)
		}
		if strings.Contains(line, "keep typing") {
			t.Errorf("cursor on %d: the key line %q asks to keep typing, but every match can be walked to", at, line)
		}
	}
}

// ⇥ and ↵ take the offer the cursor is on, drawn or scrolled to.
func TestTabAndEnterAcceptTheScrolledToOffer(t *testing.T) {
	for name, key := range map[string]tea.KeyMsg{"⇥": {Type: tea.KeyTab}, "↵": {Type: tea.KeyEnter}} {
		t.Run(name, func(t *testing.T) {
			a := walk(longMenu(t, 60), downKey, 40)
			if a.completion.cursor != 40 {
				t.Fatalf("↓ x40 left the cursor on %d, so there is no scrolled-to offer to accept", a.completion.cursor)
			}
			want := a.completion.offers[40]
			a, _ = pressKey(a, key)
			if got := a.composer().Value(); got != want+" " {
				t.Errorf("%s over the 41st offer left %q, want %q", name, got, want+" ")
			}
		})
	}
}

// Typing narrows what is offered and starts the walk over, window and all.
func TestTypingStartsTheWalkOverAtTheTop(t *testing.T) {
	a := walk(longMenu(t, 60), downKey, 40)
	a = a.withDraft("c") // `/c`: still more than a window's worth
	if a.completion.cursor != 0 {
		t.Errorf("typing left the cursor on offer %d, want the top", a.completion.cursor)
	}
	rows, at := drawn(t, a)
	if at != 0 || rows[0] != a.completion.offers[0] {
		t.Errorf("typing left the window at %q with the cursor on row %d, want offer 0 first and cursored", rows, at)
	}
}

// A menu is as tall at every cursor as it is at rest: the pane's chrome counts
// on it, and a frame one row taller scrolls the alt screen away.
func TestTheMenuIsAsTallAtEveryCursorAsAtRest(t *testing.T) {
	a := longMenu(t, 60)
	total := 60 + len(wakeVerbs())
	want := completionRows + 1
	for i := range total {
		if h := strings.Count(a.completionView(100, a.focus), "\n") + 1; h != want {
			t.Fatalf("with the cursor on offer %d the menu is %d rows, want %d", i, h, want)
		}
		a, _ = pressKey(a, downKey)
	}
}

// What the fleet reports while a walk is on the way leaves the cursor on its
// offer, and the offer on screen.
func TestAReportMidWalkKeepsTheCursorOnItsOfferAndOnScreen(t *testing.T) {
	a := walk(longMenu(t, 60), downKey, 50)
	if a.completion.cursor != 50 {
		t.Fatalf("↓ x50 left the cursor on %d, so there is no walk for a report to interrupt", a.completion.cursor)
	}
	want := a.completion.offers[50]
	a = a.withAgents("alex", "blake") // a status push: the menu is rebuilt
	if got := a.completion.offers[a.completion.cursor]; got != want {
		t.Fatalf("a report moved the cursor from %q to %q", want, got)
	}
	onScreen(t, a, "after a report")
}

// The room's `@` offers every agent, not the first screenful.
func TestTheRoomsMentionMenuWalksEveryAgent(t *testing.T) {
	names := make([]string, 30)
	for i := range names {
		names[i] = fmt.Sprintf("agent-%02d", i)
	}
	a := newRoomApp(t).withAgents(names...).withSize(200, 40).withDraft("@a")
	if want := len(names) + 1; len(a.completion.offers) != want { // and @all
		t.Fatalf("the room's `@a` holds %d offers, want %d", len(a.completion.offers), want)
	}
	a = walk(a, downKey, 25)
	want := a.completion.offers[25]
	if want != "@agent-25" {
		t.Fatalf("offer 25 is %q, want @agent-25", want)
	}
	onScreen(t, a, "in the room")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	if got := a.composer().Value(); got != want+" " {
		t.Errorf("⇥ left %q, want %q", got, want+" ")
	}
}

// A conversation's file search walks past its first screenful and accepts what
// the cursor is on.
func TestAConversationsFileSearchWalksEveryMatch(t *testing.T) {
	files := make([]string, 60)
	for i := range files {
		files[i] = fmt.Sprintf("f%02d.go", i)
	}
	withGit(t, answering(files...))
	a := convOver(t, workdir(t)).withDraft("@f")
	if len(a.completion.offers) != len(files) {
		t.Fatalf("`@f` over %d files holds %d offers", len(files), len(a.completion.offers))
	}
	a = walk(a, downKey, 50)
	onScreen(t, a, "in a file search")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	if got := a.composer().Value(); got != "@f50.go " {
		t.Errorf("⇥ over the 51st file left %q, want %q", got, "@f50.go ")
	}
}

// A search too big to keep whole is kept to the ceiling, which is walked to the
// end, and the key line says the rest is for typing.
func TestAMenuPastTheCeilingKeepsTheCeilingAndSaysSo(t *testing.T) {
	const files = completionCeiling + 100
	names := make([]string, files)
	for i := range names {
		names[i] = fmt.Sprintf("f%05d.go", i)
	}
	withGit(t, answering(names...))
	a := convOver(t, workdir(t)).withDraft("@f")
	if got := len(a.completion.offers); got != completionCeiling {
		t.Fatalf("%d matches hold %d offers, want the ceiling %d", files, got, completionCeiling)
	}
	if !strings.Contains(menuKeyLine(a), "keep typing") {
		t.Errorf("the key line %q does not say the rest is for typing", menuKeyLine(a))
	}
	if below := countIn(belowCount, menuKeyLine(a)); below != files-completionRows {
		t.Errorf("the key line counts %d below, want %d: every match the window does not draw, the ceiling's cut included",
			below, files-completionRows)
	}
	a = walk(a, downKey, files)
	if a.completion.cursor != completionCeiling-1 {
		t.Errorf("↓ ended on offer %d, want the ceiling's last %d", a.completion.cursor, completionCeiling-1)
	}
	onScreen(t, a, "at the ceiling")
	if above := countIn(aboveCount, menuKeyLine(a)); above != completionCeiling-completionRows || countIn(belowCount, menuKeyLine(a)) != files-completionCeiling {
		t.Errorf("at the end the key line is %q, want %d above and the %d past the ceiling below",
			menuKeyLine(a), completionCeiling-completionRows, files-completionCeiling)
	}
}

// The `/team` argument's teams are walked the same way.
func TestTheTeamArgumentMenuWalksEveryTeam(t *testing.T) {
	fresh(t)
	st := rpc.Status{Running: true}
	for i := range 30 {
		team := fmt.Sprintf("team-%02d", i)
		st.Teams = append(st.Teams, team)
		st.Sessions = append(st.Sessions, rpc.SessionStatus{ID: fmt.Sprintf("s%d", i+1), Name: fmt.Sprintf("n%02d", i), Team: team, State: rpc.StateIdle})
	}
	a := dmApp(nil, Stream{}, "s1", "n00").withSize(200, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st}).withDraft("/team ")
	if len(a.completion.offers) != len(st.Teams) {
		t.Fatalf("`/team ` holds %d offers over %d teams", len(a.completion.offers), len(st.Teams))
	}
	a = walk(a, downKey, 27)
	onScreen(t, a, "in the team menu")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyTab})
	if got := a.composer().Value(); got != "/team "+a.fleet.teamOrder[27]+" " {
		t.Errorf("⇥ left %q, want team %q", got, a.fleet.teamOrder[27])
	}
}

// The menu over an answer box is the same menu: ↓ walks it and ↵ completes the
// offer it reaches.
func TestAMenuOverTheAnswerBoxWalksPastItsFirstScreenful(t *testing.T) {
	pane := paneAsking(t).advertising("s1", commandNames(60)...)
	a := answeringOther(t, pane, "/")
	if got, want := len(a.completion.offers), 60+len(wakeVerbs()); got != want {
		t.Fatalf("the box's `/` holds %d offers, want %d", got, want)
	}
	a = walk(a, downKey, 40)
	if a.completion.cursor != 40 {
		t.Fatalf("↓ x40 left the cursor on %d", a.completion.cursor)
	}
	want := a.completion.offers[40]
	onScreen(t, a, "over the answer box")
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if got := strings.TrimSpace(a.composer().Value()); got != want {
		t.Errorf("↵ over the 41st offer left %q in the box, want %q", got, want)
	}
	if _, writing := a.cards.writingFor(topCard(t, a)); !writing {
		t.Error("↵ over the menu ended answer mode instead of completing the word")
	}
}
