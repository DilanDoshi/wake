package ui

// /wake-help: opening it, the keys it owns, filtering, scrolling and the mouse.
// What it lists, and that the list cannot fall behind the bindings, is
// helpguard_test.go's.

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const helpSampleUsage = "usage:\n  wake new [name]         open a conversation with a new agent\n\n" +
	"flags, anywhere:\n  --fleet <name>          which fleet to talk to\n" +
	"  --max-agents <n>        how many sessions the fleet runs at once;\n" +
	"                          with wake, new, attach or manager\n\n" +
	"flags, on wake status:\n  --team <name>           only the agents on that team\n"

// helpRoom is the room over alex, with a recorder behind the connection so a
// test can say nothing was written.
func helpRoom(t *testing.T, w, h int) (App, *recorder) {
	t.Helper()
	fresh(t)
	rec := newRecorder(t)
	a := NewRoomApp(rec, Stream{}, nil).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", State: rpc.StateIdle},
		rpc.SessionStatus{ID: "s2", Name: "sydney", State: rpc.StateIdle},
	).withSize(w, h)
	rec.taken(t)
	return a, rec
}

// helpOpened runs /wake-help through the router, the way a submitted draft
// reaches it.
func helpOpened(t *testing.T, a App, arg string) App {
	t.Helper()
	text := wakeHelpVerb
	if arg != "" {
		text += " " + arg
	}
	next, _, mine := a.slash(text)
	if !mine {
		t.Fatalf("%q was not taken by the router", text)
	}
	if !next.help.view.Open() {
		t.Fatalf("%q did not open the view: %+v", text, next.help.view)
	}
	return next
}

// helpFrame is the view as its pane draws it, in the size the pane is.
func helpFrame(a App) string {
	w, h, _ := a.paneSize(a.help.view.Pane)
	return stripANSI(a.helpPane(w, h))
}

// helpWords is everything the pane draws, in reading order with whitespace
// folded, so a row that wrapped still reads as the sentence it is.
func helpWords(a App) string { return strings.Join(strings.Fields(helpFrame(a)), " ") }

func typeHelp(a App, text string) App {
	for _, r := range text {
		k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
		if r == ' ' {
			k = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{r}}
		}
		a, _ = pressKey(a, k)
	}
	return a
}

func legendHelpFor(t *testing.T, glyph string) string {
	t.Helper()
	for _, e := range legendEntries {
		if e.glyph == glyph {
			return e.help
		}
	}
	t.Fatalf("no legend entry for %q", glyph)
	return ""
}

func TestSlashWakeHelpOpensInTheRoomAndWritesNothing(t *testing.T) {
	a, rec := helpRoom(t, 160, 60)
	m, cmd := typeAndSubmit(a, wakeHelpVerb)
	drainBatch(cmd)
	a = m.(App)
	if !a.help.view.Open() || a.help.view.Pane != "" {
		t.Fatalf("view = %+v, want open in the room", a.help.view)
	}
	if wrote := rec.taken(t); len(wrote) != 0 {
		t.Errorf("/wake-help wrote %v; it is drawn from what Wake holds", sentKinds(wrote))
	}
	if got := a.composer().Value(); got != "" {
		t.Errorf("the command stayed in the draft: %q", got)
	}
	out := shown(a)
	if !strings.Contains(out, roomTitle+" › wake-help") {
		t.Errorf("the room's title does not name the view:\n%s", out)
	}
	if want := legendHelpFor(t, "⌃C"); !strings.Contains(helpWords(a), strings.Join(strings.Fields(want), " ")) {
		t.Errorf("the ⌃C line %q is not drawn:\n%s", want, helpFrame(a))
	}
}

func TestSlashWakeHelpOpensInAConversationUnderItsAgentsName(t *testing.T) {
	a, _ := helpRoom(t, 160, 60)
	a = helpOpened(t, a.openDMWith("s1", "alex"), "")
	if a.help.view.Pane != "s1" {
		t.Errorf("pane = %q, want the conversation it was typed in", a.help.view.Pane)
	}
	if out := shown(a); !strings.Contains(out, "@alex › wake-help") {
		t.Errorf("the conversation's title does not name the view:\n%s", out)
	}
}

// An argument is where the filter starts, so `/wake-help park` is the answer to
// "what parks?" in one go.
func TestWakeHelpWithAnArgumentOpensFiltered(t *testing.T) {
	a, _ := helpRoom(t, 160, 200)
	a = helpOpened(t, a, "  detach ")
	if a.help.view.Query != "detach" {
		t.Fatalf("query = %q, want the trimmed argument", a.help.view.Query)
	}
	words := helpWords(a)
	if !strings.Contains(words, strings.Join(strings.Fields(legendHelpFor(t, "⌃O")), " ")) {
		t.Errorf("the ⌃O line is not drawn under a filter that names it:\n%s", helpFrame(a))
	}
	if strings.Contains(words, strings.Join(strings.Fields(legendHelpFor(t, "⌃W")), " ")) {
		t.Errorf("the ⌃W line is drawn under a filter that does not match it:\n%s", helpFrame(a))
	}
}

// Every key is the view's while it is up: none opens, arms, forks or reaches the
// draft - the workflow view's own rule - and esc is the way out.
func TestWakeHelpTakesEveryKeyWhileUp(t *testing.T) {
	a, rec := helpRoom(t, 160, 40)
	a = helpOpened(t, a.openDMWith("s1", "alex"), "")
	rec.taken(t)
	panes := len(a.grid.Panes())
	for _, k := range []tea.KeyType{
		tea.KeyCtrlD, tea.KeyTab, tea.KeyCtrlY, tea.KeyCtrlB, tea.KeyCtrlW, tea.KeyCtrlO, tea.KeyCtrlQ, tea.KeyEnter,
		tea.KeyCtrlE, tea.KeyShiftTab, tea.KeyCtrlX, tea.KeyCtrlT, tea.KeyCtrlA, tea.KeyCtrlR, tea.KeyCtrlF,
		tea.KeyCtrlCloseBracket, tea.KeyShiftUp, tea.KeyShiftLeft,
	} {
		a, _ = pressKey(a, wfKey(k))
		if !a.help.view.Open() || a.focus != "s1" || len(a.grid.Panes()) != panes {
			t.Fatalf("%v moved something under the view: %+v focus %q panes %d", k, a.help.view, a.focus, len(a.grid.Panes()))
		}
		if a.detachArmed || a.quitArmed {
			t.Fatalf("%v armed a lifecycle key from under the view", k)
		}
	}
	if got := a.composer().Value(); got != "" {
		t.Errorf("keys pressed in the view reached the draft: %q", got)
	}
	if wrote := rec.taken(t); len(wrote) != 0 {
		t.Errorf("keys pressed in the view wrote %v", sentKinds(wrote))
	}
	a, _ = pressKey(a, wfKey(tea.KeyEsc))
	if a.help.view.Open() {
		t.Error("esc left the view open")
	}
	if a.focus != "s1" {
		t.Errorf("esc moved the keys to %q", a.focus)
	}
}

// ⌃C closes the view and is handed on to park, as the workflow view hands it:
// the kill switch reads a ⌃C that appeared to do nothing as the start of ⌃C⌃C.
func TestCtrlCClosesTheHelpAndIsNotSwallowed(t *testing.T) {
	a, _ := helpRoom(t, 160, 40)
	a, _ = pressKey(helpOpened(t, a.openDMWith("s1", "alex"), ""), wfKey(tea.KeyCtrlC))
	if a.help.view.Open() {
		t.Error("⌃C left the view open")
	}
	if _, parking := a.parking["s1"]; !parking {
		t.Error("⌃C was swallowed by the view: alex was not parked")
	}
}

func TestWakeHelpFiltersAsTypedAndWidensOnBackspace(t *testing.T) {
	a, _ := helpRoom(t, 160, 200)
	a = helpOpened(t, a, "")
	all := helpFrame(a)
	a = typeHelp(a, "detach")
	if a.help.view.Query != "detach" {
		t.Fatalf("query = %q after typing", a.help.view.Query)
	}
	narrowed := helpFrame(a)
	if !strings.Contains(narrowed, "detach") || len(strings.Fields(narrowed)) >= len(strings.Fields(all)) {
		t.Errorf("typing did not narrow the list:\n%s", narrowed)
	}
	// A section that keeps no row loses its heading too. The rows are read back
	// independently of the filter: a row matches when its text holds the word.
	for _, s := range helpSections(shellRows(helpSampleUsage)) {
		matches := 0
		for _, r := range s.rows {
			if strings.Contains(strings.ToLower(r.lead+" "+r.text), "detach") {
				matches++
			}
		}
		drawn := false
		for _, line := range strings.Split(narrowed, "\n") {
			drawn = drawn || strings.TrimSpace(line) == s.title
		}
		if drawn != (matches > 0) {
			t.Errorf("section %q has %d matching rows and its heading is drawn=%v", s.title, matches, drawn)
		}
	}
	a, _ = pressKey(a, wfKey(tea.KeyBackspace))
	if a.help.view.Query != "detac" {
		t.Errorf("⌫ left the query %q, want detac", a.help.view.Query)
	}
	for range 5 {
		a, _ = pressKey(a, wfKey(tea.KeyBackspace))
	}
	if a.help.view.Query != "" || helpFrame(a) != all {
		t.Errorf("⌫ down to nothing did not bring every row back (query %q)", a.help.view.Query)
	}
	a = typeHelp(a, "zzzzqq")
	if out := helpFrame(a); !strings.Contains(out, "nothing matches") {
		t.Errorf("a query matching nothing says nothing:\n%s", out)
	}
}

// Words narrow together, so `park resume` finds the line that holds both and
// drops the one that holds only `park`.
func TestWakeHelpTakesEveryWordOfTheQuery(t *testing.T) {
	a, _ := helpRoom(t, 160, 200)
	a = typeHelp(helpOpened(t, a, ""), "park resume")
	words := helpWords(a)
	both, onlyPark := helpFolded(legendHelpFor(t, "⌃C")), helpFolded(legendHelpFor(t, "⌃Q"))
	if !strings.Contains(strings.ToLower(both), "park") || !strings.Contains(strings.ToLower(both), "resume") || strings.Contains(strings.ToLower(onlyPark), "resume") {
		t.Fatalf("the fixture lines no longer split the words: %q and %q", both, onlyPark)
	}
	if !strings.Contains(words, both) {
		t.Errorf("the ⌃C line holds both words and is not drawn:\n%s", helpFrame(a))
	}
	if strings.Contains(words, onlyPark) {
		t.Errorf("the ⌃Q line holds only one of the words and is drawn:\n%s", helpFrame(a))
	}
}

func TestWakeHelpScrollsByRowPageAndWheelAndStaysOnePanesHeight(t *testing.T) {
	a, _ := helpRoom(t, 140, 24)
	a = a.openDMWith("s1", "alex")
	a = helpOpened(t, a, "")
	w, h, _ := a.paneSize("s1")
	first := strings.Split(helpFrame(a), "\n")
	if a.help.view.Scroll != 0 {
		t.Fatalf("scroll = %d at open", a.help.view.Scroll)
	}
	a, _ = pressKey(a, wfKey(tea.KeyDown))
	if a.help.view.Scroll != 1 {
		t.Fatalf("↓ left scroll at %d, want 1", a.help.view.Scroll)
	}
	moved := strings.Split(helpFrame(a), "\n")
	if moved[helpBodyTop+1] != first[helpBodyTop+2] {
		t.Errorf("↓ did not move the body up a row:\n%q\n%q", moved[helpBodyTop+1], first[helpBodyTop+2])
	}
	a, _ = pressKey(a, wfKey(tea.KeyPgDown))
	page := a.help.view.Scroll - 1
	if page < 3 {
		t.Errorf("⇟ moved %d rows in a %d-row pane", page, h)
	}
	a, _ = pressKey(a, wfKey(tea.KeyPgUp))
	if a.help.view.Scroll != 1 {
		t.Errorf("⇞ did not undo ⇟: scroll %d", a.help.view.Scroll)
	}
	x, y, ok := viewCell(a, "wake-help")
	if !ok {
		t.Fatalf("the view is not drawn:\n%s", shown(a))
	}
	a = wheel(a, false, x, y)
	if a.help.view.Scroll != 1+wheelLines {
		t.Errorf("the wheel down left scroll at %d, want %d", a.help.view.Scroll, 1+wheelLines)
	}
	a = wheel(a, true, x, y)
	if a.help.view.Scroll != 1 {
		t.Errorf("the wheel up left scroll at %d, want 1", a.help.view.Scroll)
	}
	// The end: a held ↓ stops where the last row is drawn, and one more moves nothing.
	for range 400 {
		a, _ = pressKey(a, wfKey(tea.KeyDown))
	}
	end := a.help.view.Scroll
	a, _ = pressKey(a, wfKey(tea.KeyDown))
	if a.help.view.Scroll != end {
		t.Errorf("↓ past the end moved scroll from %d to %d", end, a.help.view.Scroll)
	}
	rows := helpLines(helpSections(nil), w)
	if last := stripANSI(rows[len(rows)-1]); !strings.Contains(helpFrame(a), strings.TrimSpace(last)) {
		t.Errorf("the last row %q is not drawn at the end of the scroll:\n%s", last, helpFrame(a))
	}
	// Every offset draws exactly the pane's height, or the alt screen scrolls.
	for _, scroll := range []int{0, 1, end, end + 50} {
		a.help.view.Scroll = scroll
		if got := lipgloss.Height(a.helpPane(w, h)); got != h {
			t.Errorf("scroll %d draws %d rows in a %d-row pane", scroll, got, h)
		}
	}
	// Typing narrows the list, and a scroll left past the shorter list goes home.
	a.help.view.Scroll = end
	a = typeHelp(a, "d")
	if a.help.view.Scroll != 0 {
		t.Errorf("a query change left scroll at %d", a.help.view.Scroll)
	}
}

func TestWakeHelpFitsANarrowPane(t *testing.T) {
	a, _ := helpRoom(t, 160, 60)
	a = helpOpened(t, a, "")
	for _, w := range []int{80, 52, 40, 24, 12} {
		out := a.helpPane(w, 30)
		if got := lipgloss.Height(out); got != 30 {
			t.Errorf("width %d draws %d rows, want 30", w, got)
		}
		for _, line := range strings.Split(out, "\n") {
			if got := ansi.StringWidth(line); got > w {
				t.Errorf("width %d: a row is %d cells wide: %q", w, got, ansi.Strip(line))
			}
		}
	}
	// Narrow, a row wraps rather than loses its end: the whole sentence is still
	// drawn, read below the title and the filter line, which echoes the query.
	narrow := a
	narrow.help.view.Query = "queued messages"
	lines := strings.Split(stripANSI(narrow.helpPane(30, 40)), "\n")
	if got, want := helpFolded(strings.Join(lines[helpBodyTop:], " ")), helpFolded(legendHelpFor(t, "⌃]")); !strings.Contains(got, want) {
		t.Errorf("the ⌃] line was cut at width 30:\n%s", strings.Join(lines, "\n"))
	}
}

func TestOpeningAWorkflowViewOrMovingTheKeysClosesTheHelpInThatPane(t *testing.T) {
	a, _ := helpRoom(t, 160, 40)
	a = helpOpened(t, a.openDMWith("s1", "alex"), "")
	if next := a.showWorkflow(WorkflowView{Up: true, Pane: "s1", Session: "s1"}); next.help.view.Open() {
		t.Error("a workflow view opened over the help in one pane, and both are up")
	}
	if next := a.refocus(""); next.help.view.Open() {
		t.Error("the help outlived the keys leaving its pane")
	}
}

// A press on the view is a screen selection, so a drag across it copies what it
// shows, as it does on every surface.
func TestADragOverTheHelpSelectsAndCopies(t *testing.T) {
	a, _ := helpRoom(t, 160, 60)
	a = helpOpened(t, a, "")
	x, y, ok := viewCell(a, "wake-help")
	if !ok {
		t.Fatalf("the view is not drawn:\n%s", shown(a))
	}
	got, cmd := drag(a, x, x+8, y)
	if !got.sel.onScreen || got.sel.empty() {
		t.Errorf("sel = %+v: a drag over the view is a screen selection", got.sel)
	}
	if cmd == nil {
		t.Error("a drag over the view produced no copy")
	}
	if !got.help.view.Open() {
		t.Error("a press closed the view")
	}
}

func TestWakeHelpDrawsTheShellUsageItWasHanded(t *testing.T) {
	a, _ := helpRoom(t, 160, 200)
	a = helpOpened(t, a.WithUsage(helpSampleUsage), "")
	words := helpWords(a)
	for _, want := range []string{"wake new [name]", "open a conversation with a new agent", "--fleet <name>", "which fleet to talk to", "flags, anywhere"} {
		if !strings.Contains(words, want) {
			t.Errorf("the shell section does not draw %q:\n%s", want, helpFrame(a))
		}
	}
	bare, _ := helpRoom(t, 160, 200)
	for _, line := range strings.Split(helpFrame(helpOpened(t, bare, "")), "\n") {
		if strings.TrimSpace(line) == shellSectionTitle {
			t.Error("a model handed no usage draws a shell section with nothing in it")
		}
	}
}

// App.key is the path a key takes after the board has let go of it, so it reads
// the view's keys too, as it reads the workflow view's.
func TestAppKeyGivesTheOpenHelpItsKeys(t *testing.T) {
	a, _ := helpRoom(t, 140, 24)
	a = helpOpened(t, a.openDMWith("s1", "alex"), "")
	next, _, handled := a.key(wfKey(tea.KeyDown))
	if !handled || next.(App).help.view.Scroll != 1 {
		t.Errorf("App.key did not scroll the view: handled=%v scroll=%d", handled, next.(App).help.view.Scroll)
	}
}

// The rows are laid out when the width or the query changes and read from the
// view between, so a frame does not pay for them - and are never read for a key
// they were not made for.
func TestWakeHelpKeepsItsLayoutAndNeverDrawsAStaleOne(t *testing.T) {
	a, _ := helpRoom(t, 140, 40)
	m, _ := typeAndSubmit(a.openDMWith("s1", "alex"), wakeHelpVerb)
	a = m.(App)
	w, h, _ := a.paneSize("s1")
	fresh := func(q string) []string { return helpLines(helpFilter(helpSections(nil), q), w) }
	if v := a.help.view; !v.laid.ok || v.laid.width != w || !slices.Equal(v.laid.rows, fresh("")) {
		t.Fatalf("opening did not lay the rows out for the pane: %+v", a.help.view.laid.width)
	}
	a = typeHelp(a, "park")
	if v := a.help.view; v.laid.query != "park" || !slices.Equal(v.laid.rows, fresh("park")) {
		t.Errorf("typing did not lay the narrowed rows out: query %q", v.laid.query)
	}
	// Read: a layout made for this width and query is what is drawn.
	poisoned := a
	poisoned.help.view.laid.rows = []string{"POISONED ROW"}
	if !strings.Contains(stripANSI(poisoned.helpPane(w, h)), "POISONED ROW") {
		t.Error("the draw laid the rows out again instead of reading the view's layout")
	}
	// Stale: one made for another width or query is never drawn.
	for _, stale := range []helpLayout{{ok: true, width: w + 1, query: "park", rows: []string{"POISONED ROW"}}, {ok: true, width: w, query: "x", rows: []string{"POISONED ROW"}}} {
		poisoned.help.view.laid = stale
		if strings.Contains(stripANSI(poisoned.helpPane(w, h)), "POISONED ROW") {
			t.Errorf("a layout for width %d query %q was drawn at width %d query %q", stale.width, stale.query, w, "park")
		}
	}
}

// A usage line that wraps carries on under its description with no gap of its
// own: it belongs to the row above, not to a row of its own that would widen the
// section's lead column and split the sentence under a filter.
func TestTheShellSectionJoinsAContinuationLineToItsRow(t *testing.T) {
	var flags []helpRow
	for _, r := range shellRows(helpSampleUsage) {
		if strings.HasPrefix(r.lead, "with wake, new") {
			t.Errorf("a continuation line became a row of its own: %+v", r)
		}
		if strings.HasPrefix(r.lead, "--max-agents") {
			flags = append(flags, r)
		}
	}
	if len(flags) != 1 || !strings.Contains(flags[0].text, "once; with wake, new, attach or manager") {
		t.Errorf("--max-agents is %+v, want one row holding the whole sentence", flags)
	}
}

// A heading names the group under it, so it is drawn only while a row of that
// group is - and a query that names the heading brings the whole group.
func TestAFilterKeepsAGroupHeadingOnlyWithARowUnderIt(t *testing.T) {
	shell := func(query string) []helpRow {
		for _, s := range helpFilter(helpSections(shellRows(helpSampleUsage)), query) {
			if s.title == shellSectionTitle {
				return s.rows
			}
		}
		return nil
	}
	texts := func(rows []helpRow) string {
		var b strings.Builder
		for _, r := range rows {
			b.WriteString(r.lead + "|" + r.text + "\n")
		}
		return b.String()
	}
	got := texts(shell("team"))
	if !strings.Contains(got, "--team <name>") || !strings.Contains(got, "flags, on wake status:") {
		t.Errorf("a row of the status group lost its heading:\n%s", got)
	}
	if strings.Contains(got, "flags, anywhere:") {
		t.Errorf("a heading with no row under it was kept:\n%s", got)
	}
	got = texts(shell("anywhere"))
	for _, want := range []string{"flags, anywhere:", "--fleet <name>", "--max-agents <n>"} {
		if !strings.Contains(got, want) {
			t.Errorf("naming the heading did not bring %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "--team") {
		t.Errorf("naming one heading brought another group's row:\n%s", got)
	}
}

// esc is the only way out, so the foot says it first and a narrow pane cuts the
// rest.
func TestTheFootKeepsEscCloseAtEveryWidth(t *testing.T) {
	a, _ := helpRoom(t, 160, 60)
	a = helpOpened(t, a, "")
	for _, w := range []int{80, 40, 30, 24} {
		lines := strings.Split(stripANSI(a.helpPane(w, 30)), "\n")
		foot := ""
		for _, l := range lines[helpBodyTop:] {
			if strings.TrimSpace(l) != "" {
				foot = l // the last row with words in it
			}
		}
		if !strings.Contains(foot, "esc close") {
			t.Errorf("width %d: the foot is %q, and the way out is not in it", w, foot)
		}
	}
}

// What a paste can put in the filter is bounded, and whitespace of any kind is a
// space.
func TestAPasteIsBoundedByTheQueryMax(t *testing.T) {
	v := HelpView{}.typed(strings.Repeat("x", 3*helpQueryMax))
	if got := len([]rune(v.Query)); got != helpQueryMax {
		t.Errorf("a long paste left %d runes in the query, want %d", got, helpQueryMax)
	}
	if got := (HelpView{}).typed("a\n\tb \x07 c").Query; got != "a b c" {
		t.Errorf("whitespace and control characters left the query %q, want %q", got, "a b c")
	}
}

// Two columns while the text keeps helpMinText cells, stacked one under the
// other the cell after.
func TestARowStacksItsColumnsWhenTheTextWouldGetUnderItsMinimum(t *testing.T) {
	r := helpRow{lead: "abc", text: "alpha beta"}
	lead := 3
	enough := helpIndent + lead + helpGap + helpMargin + helpMinText
	together := stripANSI(strings.Join(helpRowLines(r, lead, enough), "\n"))
	if line := strings.Split(together, "\n")[0]; !strings.Contains(line, "abc") || !strings.Contains(line, "alpha") {
		t.Errorf("at width %d the columns are not side by side:\n%s", enough, together)
	}
	stacked := strings.Split(stripANSI(strings.Join(helpRowLines(r, lead, enough-1), "\n")), "\n")
	if strings.Contains(stacked[0], "alpha") || !strings.Contains(stacked[0], "abc") || !strings.Contains(stacked[1], "alpha") {
		t.Errorf("at width %d the columns did not stack:\n%s", enough-1, strings.Join(stacked, "\n"))
	}
}
