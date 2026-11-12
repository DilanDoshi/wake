package ui

// The completion menu's window is sized to the pane that draws it, and survives
// every rebuild a walk can meet. The pane clips a menu block from the bottom, so
// a window of completionRows rows in a short pane keeps the cursor on a row the
// pane cuts: ↵ and ⇥ would then take an offer nobody sees. These read the frame
// the terminal gets, and the rebuilds that once left `top` past the offers.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

var anyCount = regexp.MustCompile(`\+\d+ (above|below)`)

// framed is the frame as plain rows.
func framed(a App) []string { return strings.Split(ansi.Strip(a.View()), "\n") }

// rowWith is the first frame row holding text, or "" when none does.
func rowWith(rows []string, text string) string {
	for _, row := range rows {
		if strings.Contains(row, text) {
			return row
		}
	}
	return ""
}

// seen fails unless the frame draws the cursored offer highlighted and the key
// line with a count - the two things ↵ and ⇥ are taken on.
func seen(t *testing.T, a App, what string) {
	t.Helper()
	rows := framed(a)
	offer := a.completion.offers[a.completion.cursor]
	if rowWith(rows, cardCursor+offer) == "" {
		t.Fatalf("%s: the cursor is on %q (offer %d) and the frame does not draw it highlighted:\n%s",
			what, offer, a.completion.cursor, strings.Join(rows, "\n"))
	}
	if key := rowWith(rows, "⇥ ↵ complete"); !anyCount.MatchString(key) {
		t.Fatalf("%s: the key line %q does not count what lies outside the window:\n%s", what, key, strings.Join(rows, "\n"))
	}
}

// menuAt is a menu over a window of the given height: a conversation's `/` over
// 60 advertised commands, or the room's `@a` over 40 agents.
func menuAt(t *testing.T, room bool, height int) App {
	t.Helper()
	fresh(t)
	if !room {
		return dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(100, height).applyGeometry().
			advertising("s1", commandNames(60)...).withDraft("/")
	}
	names := make([]string, 40)
	for i := range names {
		names[i] = fmt.Sprintf("agent-%02d", i)
	}
	return newRoomApp(t).withAgents(names...).withSize(100, height).applyGeometry().withDraft("@a")
}

// In any pane, at every cursor, the cursor's row and the count are on screen:
// the window holds the rows the pane draws, not the rows it would like to.
func TestTheWindowIsSizedToThePaneAtEveryCursor(t *testing.T) {
	for name, room := range map[string]bool{"conversation": false, "room": true} {
		t.Run(name, func(t *testing.T) {
			for height := 16; height <= 50; height++ {
				a := menuAt(t, room, height)
				if len(a.completion.offers) <= completionRows {
					t.Fatalf("%d offers fit the %d rows, so there is no window to size", len(a.completion.offers), completionRows)
				}
				for i := range a.completion.offers {
					seen(t, a, fmt.Sprintf("%d rows, cursor on offer %d", height, i))
					a, _ = pressKey(a, downKey)
				}
			}
		})
	}
}

// A resize with the menu open re-windows it: the keys that walked a tall pane's
// menu are still on screen when the pane is made short.
func TestAResizeKeepsTheCursorDrawn(t *testing.T) {
	for name, room := range map[string]bool{"conversation": false, "room": true} {
		t.Run(name, func(t *testing.T) {
			a := walk(menuAt(t, room, 50), downKey, 35)
			seen(t, a, "at 50 rows")
			for _, height := range []int{24, 17, 40} {
				a = a.withSize(100, height).applyGeometry()
				seen(t, a, fmt.Sprintf("resized to %d rows", height))
			}
		})
	}
}

// A git answer that lands in a short pane opens a menu whose window is sized to
// that pane at once - nothing else rebuilds it before the first key. The
// directory read that the typing started answers first, so that the git is the
// last thing to land.
func TestAnIndexLandingInAShortPaneIsWindowedToIt(t *testing.T) {
	files := make([]string, 60)
	for i := range files {
		files[i] = fmt.Sprintf("f%02d.go", i)
	}
	withGit(t, answering(files...))
	a := convOver(t, workdir(t)).withSize(100, 20).applyGeometry()
	for _, r := range "@f" {
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	for _, land := range []func(App) tea.Msg{
		func(a App) tea.Msg { return scanPaths(a.completion.paths.out)() },
		func(a App) tea.Msg { return indexPaths(a.completion.paths.indexing)() },
	} {
		m, _ := a.Update(land(a))
		a = m.(App)
	}
	if len(a.completion.offers) != len(files) {
		t.Fatalf("`@f` over %d files holds %d offers", len(files), len(a.completion.offers))
	}
	for i := range files {
		seen(t, a, fmt.Sprintf("a landed search, cursor on offer %d", i))
		a, _ = pressKey(a, downKey)
	}
}

// The answer box's menu, in a pane the card shares, keeps the cursor and the
// count on screen at every height too: the card takes no rows from it, since the
// page scrolls to the box.
func TestAMenuUnderACardIsWindowedToWhatIsLeft(t *testing.T) {
	for height := 16; height <= 50; height++ {
		pane := paneAsking(t).advertising("s1", commandNames(60)...)
		a := answeringOther(t, pane, "/").withSize(120, height).applyGeometry()
		if a.completionView(100, a.focus) == "" || a.cardBlock(a.focus, 100, false) == "" {
			t.Fatalf("at %d rows there is no menu over a card, so nothing here is sized under one", height)
		}
		for i := range a.completion.offers {
			seen(t, a, fmt.Sprintf("under a card at %d rows, cursor on offer %d", height, i))
			a, _ = pressKey(a, downKey)
		}
	}
}

// What a report does to the walk, when the offers go from under it.
func TestARebuildNeverLeavesTheWindowPastTheOffers(t *testing.T) {
	few := commandNames(10)
	for name, tc := range map[string]struct {
		walk  int
		after func(App) App
	}{
		// The offer the cursor was on is gone, with the window at the end.
		"the cursor's offer goes away": {59, func(a App) App { return a.advertising("s1", few...).withAgents("alex") }},
		// It moves one row down at the window's edge.
		"the cursor's offer moves down a row": {completionRows - 1, func(a App) App {
			return a.advertising("s1", append([]string{"aaa-new"}, commandNames(60)...)...).withAgents("alex")
		}},
		// Everything shrinks while the window is at the end.
		"the offers shrink at the end": {82, func(a App) App { return a.advertising("s1", few...).withAgents("alex") }},
	} {
		t.Run(name, func(t *testing.T) {
			a := walk(longMenu(t, 60), downKey, tc.walk)
			if a.completion.cursor != tc.walk {
				t.Fatalf("↓ x%d left the cursor on %d", tc.walk, a.completion.cursor)
			}
			a = tc.after(a)
			rows, at := drawn(t, a)
			if want := min(len(a.completion.offers), completionRows); len(rows) != want {
				t.Errorf("%d offers draw %d rows, want %d: the window was not kept inside the offers", len(a.completion.offers), len(rows), want)
			}
			if at < 0 || rows[at] != a.completion.offers[a.completion.cursor] {
				t.Errorf("the cursor on %q is not drawn at its row:\n%s", a.completion.offers[a.completion.cursor], rows)
			}
		})
	}
}

// A report that changes nothing leaves the window where the reader put it: the
// least distance is from where it was, across a rebuild too.
func TestAReportThatChangesNothingLeavesTheWindowAlone(t *testing.T) {
	a := walk(longMenu(t, 60), downKey, 40)
	a = walk(a, upKey, 5)
	before, _ := drawn(t, a)
	after, _ := drawn(t, a.withAgents("alex"))
	if before[0] != after[0] {
		t.Errorf("a report moved the window from starting at %q to %q", before[0], after[0])
	}
}

// A listing that lands under a search that is being walked shrinks the offers
// from beneath it: the window goes back inside them.
func TestAListingLandingUnderAWalkedSearchKeepsTheWindowInside(t *testing.T) {
	dir := workdir(t, ".gitignore")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("build/\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "build", "f1.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// 60 paths that spell `build/f` through an indexed directory that is not
	// `build`, so the search ranks them and the listing is still wanted.
	files := make([]string, 60)
	for i := range files {
		files[i] = fmt.Sprintf("b1u1i1l1d1/f%02d.go", i)
	}
	withGit(t, answering(files...))
	a := convOver(t, dir).withDraft("@b")
	for _, r := range "uild/f" { // no scan: the listing read stays out
		a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if got := len(a.completion.offers); got != len(files) || a.completion.paths.out == "" {
		t.Fatalf("setup: %d offers and the read for %q out, want %d offers and a read out", got, a.completion.paths.out, len(files))
	}
	a = walk(a, downKey, 50)
	a = a.scanned() // the listing lands with one entry
	if got := len(a.completion.offers); got != 1 {
		t.Fatalf("setup: the listing left %d offers, want its one", got)
	}
	if rowWith(framed(a), cardCursor+a.completion.offers[0]) == "" {
		t.Errorf("the one offer %q is not drawn after the listing landed:\n%s", a.completion.offers[0], strings.Join(framed(a), "\n"))
	}
}

// What is kept past the ceiling is counted once: names and the ranked rows both
// over it add up with `more` to every match.
func TestMoreCountsEveryMatchPastTheCeiling(t *testing.T) {
	const peers, files = 5, completionCeiling + 88
	names := make([]string, files)
	for i := range names {
		names[i] = fmt.Sprintf("f%04d.go", i)
	}
	withGit(t, answering(names...))
	dir := workdir(t)
	sessions := []rpc.SessionStatus{{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle}}
	for i := range peers {
		sessions = append(sessions, rpc.SessionStatus{ID: fmt.Sprintf("s%d", i+2), Name: fmt.Sprintf("fa%d", i), State: rpc.StateIdle})
	}
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withSize(200, 40).withRoster(sessions...).withDraft("@f")
	if got, want := len(a.completion.offers)+a.completion.more, peers+files; got != want {
		t.Errorf("%d offers and %d more are %d, want the %d matches: names and ranked rows past the ceiling are each counted once",
			len(a.completion.offers), a.completion.more, got, want)
	}
}

// The ceiling is the most one directory read takes, which is also what the read
// holds: asserted on the read itself, because the menu's own bound would hide a
// read with none.
func TestTheCeilingIsWhatOneReadTakes(t *testing.T) {
	if completionCeiling != pathScanMax {
		t.Errorf("the ceiling is %d, want pathScanMax %d: the listing and the ranked search stop at one bound", completionCeiling, pathScanMax)
	}
	dir := workdir(t)
	for i := range pathScanMax + 5 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	entries, cut := readDirBounded(dir)
	if len(entries) != pathScanMax || !cut {
		t.Errorf("a read of %d entries kept %d and said cut=%v, want %d kept and cut", pathScanMax+5, len(entries), cut, pathScanMax)
	}
	exact := workdir(t)
	for i := range pathScanMax {
		if err := os.WriteFile(filepath.Join(exact, fmt.Sprintf("f%d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if entries, cut := readDirBounded(exact); len(entries) != pathScanMax || cut {
		t.Errorf("a directory of exactly %d entries read %d and said cut=%v, want all of them and not cut", pathScanMax, len(entries), cut)
	}
}

// A directory cut at the bound says so: the entries nobody read are neither
// offered nor counted, and the key line must not read as whole. It does not ask
// for typing, which narrows what was read and never reads the rest.
func TestACutListingSaysSo(t *testing.T) {
	dir := workdir(t)
	for i := range pathScanMax + 5 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	a := roomOver(t, dir).withDraft("@f")
	if got := len(a.completion.offers); got != pathScanMax {
		t.Fatalf("a cut directory holds %d offers, want %d", got, pathScanMax)
	}
	line := menuKeyLine(a)
	if !strings.Contains(line, "cut at "+strconv.Itoa(pathScanMax)) {
		t.Errorf("the key line %q does not say the directory was cut", line)
	}
	if strings.Contains(line, "keep typing") {
		t.Errorf("the key line %q asks for typing, which does not read the rest of a cut directory", line)
	}
}

// A checklist landing in a conversation grows the pane's chrome with no rebuild
// of the menu: the window is measured again, so the walk stays on screen.
func TestAChecklistLandingKeepsTheCursorDrawn(t *testing.T) {
	a := walk(menuAt(t, false, 24), downKey, 40)
	seen(t, a, "before the checklist")
	for i := range 4 {
		ev := taskCreate(fmt.Sprintf("task %d", i), "Doing it")
		ev.SessionID = "s1"
		m, _ := a.Update(streamMsg{gen: a.gen, batch: batch{frames: []rpc.Frame{{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev}}}})
		a = m.(App)
	}
	if a.dms["s1"].checklistRows() == 0 {
		t.Fatal("the checklist drew no rows, so nothing here grew the chrome")
	}
	seen(t, a, "after the checklist landed")
	a = walk(a, downKey, 5)
	seen(t, a, "walking on after it")
}

// Past the window and past the ceiling are different things and are said apart:
// what ↓ reaches, and what only typing does.
func TestBelowIsWhatDownReachesAndMoreIsWhatTypingDoes(t *testing.T) {
	const files = completionCeiling + 100
	names := make([]string, files)
	for i := range names {
		names[i] = fmt.Sprintf("f%05d.go", i)
	}
	withGit(t, answering(names...))
	a := convOver(t, workdir(t)).withDraft("@f")
	line := menuKeyLine(a)
	if below := countIn(belowCount, line); below != completionCeiling-completionRows {
		t.Errorf("the key line %q counts %d below, want the %d the walk reaches", line, below, completionCeiling-completionRows)
	}
	if more := countIn(moreCount, line); more != files-completionCeiling {
		t.Errorf("the key line %q counts %d more, want the %d past the ceiling", line, more, files-completionCeiling)
	}
	a = walk(a, downKey, files)
	if line = menuKeyLine(a); countIn(belowCount, line) != 0 || countIn(moreCount, line) != files-completionCeiling {
		t.Errorf("at the end the key line is %q, want nothing below and the %d past the ceiling still more", line, files-completionCeiling)
	}
}

var moreCount = regexp.MustCompile(`\+(\d+) more`)
