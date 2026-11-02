package ui

// A conversation drawing a card scrolls as one page: the wheel slides the card
// and the composer off the bottom and pulls the older transcript in at the top,
// and a card taller than its pane is reached by the same wheel. Without a card
// the wheel moves the transcript alone, as it always has.

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	pageHistory = 40 // transcript lines already in the pane when the ask arrives
	// pageWheelX/Y land inside the one drawn column, over its transcript.
	pageWheelX, pageWheelY = 5, 2
)

// askingPage is one conversation with a long history and a recorded question
// up, in a window of the given height.
func askingPage(t *testing.T, height int) App {
	t.Helper()
	return askingAfter(t, height, pageHistory, 0)
}

// askingAfter is askingPage with its history length chosen, and a reader who
// scrolled that many notches back through it before the question arrived.
func askingAfter(t *testing.T, height, history, readBack int) App {
	t.Helper()
	return askingWith(t, pageSetup{height: height, history: history, readBack: readBack})
}

// pageSetup is everything a conversation can hold before a question arrives.
type pageSetup struct {
	height, history, readBack int
	sent, draft               string // a prompt already sent; words typed and not
	bar                       bool   // a status bar under the composer
}

func askingWith(t *testing.T, s pageSetup) App {
	t.Helper()
	ev := questionAsk(t)
	a := newRoomApp(t).withSize(narrowColumns, s.height).withAgents("john")
	a = pick(a, "s1").openDMWith("s1", "john").applyGeometry()
	if s.bar {
		st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "john", State: rpc.StateIdle, Cwd: "/repo/api", Model: "claude-opus-5"}}}
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	}
	if s.sent != "" {
		a, _ = pressKey(a.withDraft(s.sent), tea.KeyMsg{Type: tea.KeyEnter})
	}
	for i := range s.history {
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1",
			Event: &core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("history line %02d", i)}})
	}
	a = wheeled(a, tea.MouseButtonWheelUp, s.readBack)
	if s.draft != "" { // words already in the box when the agent blocks
		a = a.withDraft(s.draft)
	}
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: ev})
}

// oldestShown is the number of the oldest history line a frame draws, -1 for none.
func oldestShown(rows []string) int {
	for _, r := range rows {
		var n int
		if _, err := fmt.Sscanf(strings.TrimSpace(strings.SplitN(r, "│", 2)[0]), "history line %d", &n); err == nil {
			return n
		}
	}
	return -1
}

// rpcEvent is an event frame for session id.
func rpcEvent(id string, ev *core.Event) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id, Event: ev}
}

// wheeled is n notches of the wheel over the pane.
func wheeled(a App, button tea.MouseButton, n int) App {
	for range n {
		m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: button, X: pageWheelX, Y: pageWheelY})
		a = m.(App)
	}
	return a
}

// pageRows is the frame as rows of plain text, the pane's own rows only.
func pageRows(a App) []string {
	return strings.Split(stripANSI(a.View()), "\n")[:a.paneHeight()]
}

func hasRow(rows []string, sub string) bool { return paneRow(rows, sub) >= 0 }

func TestWindowPage(t *testing.T) {
	cases := []struct {
		name                 string
		height, clipped, up  int
		wantRender, wantSkip int
	}{
		{"nothing clipped, at the bottom", 20, 0, 0, 20, 0},
		{"older rows make the render taller and the window stays on its top", 20, 0, 5, 25, 0},
		{"a clipped menu at its default shows the render's top", 13, 3, 3, 16, 0},
		{"scrolled to the bottom of a clipped menu skips all of it", 13, 3, 0, 16, 3},
		{"one row up from the bottom skips one row less", 13, 3, 1, 16, 2},
		{"past the clipped rows older lines come in", 13, 3, 5, 18, 0},
	}
	for _, tc := range cases {
		got := windowPage(tc.height, tc.clipped, tc.up)
		if got.render != tc.wantRender || got.skip != tc.wantSkip {
			t.Errorf("%s: windowPage(%d, %d, %d) = render %d skip %d, want render %d skip %d",
				tc.name, tc.height, tc.clipped, tc.up, got.render, got.skip, tc.wantRender, tc.wantSkip)
		}
	}
}

// Unscrolled, a card that fits draws exactly what it drew before the page
// scrolled: the transcript's tail, the card, the composer on the bottom row.
func TestAFittingCardAtRestKeepsTheComposerOnTheBottomRow(t *testing.T) {
	a := askingPage(t, 40)
	rows := pageRows(a)

	if !hasRow(rows[len(rows)-3:], "@john") {
		t.Errorf("the composer is not the pane's last box:\n%s", strings.Join(rows, "\n"))
	}
	if !hasRow(rows, "history line 39") {
		t.Errorf("the transcript's newest line is not drawn above the card:\n%s", strings.Join(rows, "\n"))
	}
}

// The wheel moves the card with the transcript: the composer leaves the bottom,
// the card slides down, and older lines arrive at the top.
func TestTheWheelUpScrollsTheCardAndComposerWithTheTranscript(t *testing.T) {
	a := askingPage(t, 40)
	before := pageRows(a)
	cardTop := paneRow(before, "has a question")

	a = wheeled(a, tea.MouseButtonWheelUp, 1)
	after := pageRows(a)

	if got, want := paneRow(after, "has a question"), cardTop+wheelLines; got != want {
		t.Errorf("the card is on row %d after one notch up, want %d: it did not slide with the transcript", got, want)
	}
	if hasRow(after, "@john") {
		t.Errorf("the composer is still on screen after scrolling up past it:\n%s", strings.Join(after, "\n"))
	}
	if !hasRow(after, "history line 27") || hasRow(before, "history line 27") {
		t.Errorf("an older transcript line did not come in at the top:\nbefore:\n%s\nafter:\n%s",
			strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

func TestTheWheelDownBringsThePageBack(t *testing.T) {
	a := askingPage(t, 40)
	want := stripANSI(a.View())

	a = wheeled(a, tea.MouseButtonWheelUp, 4)
	a = wheeled(a, tea.MouseButtonWheelDown, 4)

	if got := stripANSI(a.View()); got != want {
		t.Errorf("four notches up and four down left a different frame:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// The page ends at its oldest line, and the wheel stops there: it does not hand
// the rest of its travel to the transcript, which would leave the card and the
// composer on the bottom row again under a transcript scrolled to its top.
func TestThePageStopsAtTheOldestLine(t *testing.T) {
	a := askingPage(t, 40)

	a = wheeled(a, tea.MouseButtonWheelUp, 1000)
	rows := pageRows(a)

	if !hasRow(rows[:1], "Wake") || !hasRow(rows, "history line 00") {
		t.Errorf("the oldest line is not at the top after a long scroll:\n%s", strings.Join(rows, "\n"))
	}
	if hasRow(rows, "has a question") || hasRow(rows, "@john") {
		t.Errorf("the card or the composer is still on screen at the top of the page:\n%s", strings.Join(rows, "\n"))
	}
	// What is stored is the page's own limit, not however far the wheel was turned:
	// a later resize reads it back as an offset.
	if place, _, _ := a.pageOf("s1"); a.dms["s1"].page.up != place.limit {
		t.Errorf("the stored offset is %d after a long scroll, want the limit %d", a.dms["s1"].page.up, place.limit)
	}
	if again := wheeled(a, tea.MouseButtonWheelUp, 1); stripANSI(again.View()) != stripANSI(a.View()) {
		t.Errorf("a notch past the top of the page moved something:\n%s", strings.Join(pageRows(again), "\n"))
	}
	// And the first notch back is felt: a page that let the wheel run past its
	// top would owe the reader dead notches on the way down.
	if back := wheeled(a, tea.MouseButtonWheelDown, 1); stripANSI(back.View()) == stripANSI(a.View()) {
		t.Error("the first notch down from the top of the page moved nothing")
	}
}

// All the way up and all the way back is where it started: the page has exactly
// the rows it says it has, none to spare at either end.
func TestTheWholePageIsWalkedAndWalkedBack(t *testing.T) {
	a := askingPage(t, 40)
	want := stripANSI(a.View())

	a = wheeled(a, tea.MouseButtonWheelUp, 1000)
	a = wheeled(a, tea.MouseButtonWheelDown, 1000)

	if got := stripANSI(a.View()); got != want {
		t.Errorf("up to the top and back down left a different frame:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// A card with the composer under it is one row too many for a short pane, and
// used to be drawn with its keys cut. The composer is what scrolls out of the
// way, so the whole card is on screen and its keys are live.
func TestACardThatFitsOnceTheComposerIsOffScreenIsAnswerable(t *testing.T) {
	a := askingPage(t, 14)
	rows := pageRows(a)

	if !hasRow(rows, "[d]eny") {
		t.Fatalf("the card's key line is not on screen:\n%s", strings.Join(rows, "\n"))
	}
	if !a.cardFullyDrawn() {
		t.Error("the whole card is drawn and its keys are dead")
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardFirstOption}})
	card, _ := a.cards.For("s1")
	if card.chosen(0) != 0 {
		t.Errorf("the first digit chose option %d, want 0", card.chosen(0))
	}
}

// A pane shorter than the card itself opens on the card's foot - the key line,
// which says the keys wait for the whole card, and the rows the keys act on -
// and the wheel brings the question in above it (the owner's 2026-11-02 ruling,
// reversing 2026-10-20's top). Its keys stay dead until the whole card fits.
func TestACardTallerThanThePaneIsReachedByTheWheel(t *testing.T) {
	a := askingPage(t, 10)
	rows := pageRows(a)
	if !hasRow(rows, cardKeysCut) || hasRow(rows, "[d]eny") {
		t.Fatalf("the foot of the card is not what a short pane opens on, or it advertises keys it cannot honour:\n%s", strings.Join(rows, "\n"))
	}
	if hasRow(rows, "has a question") {
		t.Fatalf("the card's top is on screen in a pane shorter than the card:\n%s", strings.Join(rows, "\n"))
	}
	if a.cardFullyDrawn() {
		t.Error("a card taller than its pane reads as fully drawn")
	}

	for i := 0; i < 20 && !hasRow(pageRows(a), "has a question"); i++ {
		a = wheeled(a, tea.MouseButtonWheelUp, 1)
	}

	rows = pageRows(a)
	if !hasRow(rows, "has a question") {
		t.Errorf("the wheel never reached the card's top:\n%s", strings.Join(rows, "\n"))
	}
	if hasRow(rows, "[d]eny") || a.cardFullyDrawn() {
		t.Errorf("the card advertises keys it cannot honour, or reads as whole, with its foot scrolled off:\n%s", strings.Join(rows, "\n"))
	}
}

// A key aimed at a card that is partly off screen is not an answer: it goes
// where every key goes when the card is not wholly drawn, and the composer it
// lands in is brought back into view.
func TestATypedKeyWithTheCardScrolledAwayBringsTheComposerBack(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 5)
	if a.cardFullyDrawn() {
		t.Fatal("the card is still whole fifteen rows up, so this test is not exercising a scrolled-away card")
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardFirstOption}})

	card, _ := a.cards.For("s1")
	if card.chosen(0) == 0 {
		t.Error("a key answered a card that was not wholly on screen")
	}
	if !hasRow(pageRows(a)[len(pageRows(a))-3:], "@john") {
		t.Errorf("the draft the key started is not on screen:\n%s", strings.Join(pageRows(a), "\n"))
	}
}

// A short scroll that leaves the whole card on screen leaves its keys live: the
// rule is what is drawn, not whether the operator has touched the wheel.
func TestACardStillWholeAfterAShortScrollIsStillAnswerable(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 1)

	if !hasRow(pageRows(a), "[d]eny") || !a.cardFullyDrawn() {
		t.Fatalf("one notch took the card's key line or keys away:\n%s", strings.Join(pageRows(a), "\n"))
	}
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardFirstOption}})
	if card, _ := a.cards.For("s1"); card.chosen(0) != 0 {
		t.Errorf("the digit chose option %d, want 0", card.chosen(0))
	}
}

// A press on a scrolled page anchors nothing in the transcript: its rows are no
// longer where the selection code reads them, so the gesture is a frame
// selection of what is drawn.
func TestAPressOnAScrolledPageTakesAScreenSelection(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 3)

	a = a.press(pageWheelX, pageWheelY)

	if !a.sel.onScreen {
		t.Errorf("the press anchored in the transcript, whose rows are not where they were: %+v", a.sel)
	}
}

// Another card starts at rest, wherever the last one was left.
func TestANewCardStartsAtTheBottom(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 5)
	first, _ := a.cards.For("s1")
	a.cards = a.cards.Settle("s1", first.RequestID)

	next := *questionAsk(t)
	next.RequestID = first.RequestID + "-next"
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &next})

	if card, ok := a.cards.For("s1"); !ok || card.RequestID == first.RequestID {
		t.Fatalf("the second ask is not the one up: %+v", card)
	}
	rows := pageRows(a)
	if !hasRow(rows[len(rows)-3:], "@john") {
		t.Errorf("the next card inherited the last one's scroll:\n%s", strings.Join(rows, "\n"))
	}
}

// With no card the wheel is the transcript's alone, and the composer stays put.
func TestWithoutACardTheWheelMovesOnlyTheTranscript(t *testing.T) {
	a := askingPage(t, 40)
	card, _ := a.cards.For("s1")
	a.cards = a.cards.Settle("s1", card.RequestID)
	if _, up := a.cards.For("s1"); up {
		t.Fatal("the card is still up")
	}

	a = wheeled(a, tea.MouseButtonWheelUp, 2)

	if rows := pageRows(a); !hasRow(rows[len(rows)-3:], "@john") {
		t.Errorf("the composer left the bottom of a pane with no card:\n%s", strings.Join(rows, "\n"))
	}
}

// A reader who had scrolled back before the card arrived keeps their place, and
// the page takes older lines in above it.
func TestAReaderWhoScrolledBackKeepsTheirPlaceAndTheWheelGoesOlder(t *testing.T) {
	a := askingAfter(t, 40, pageHistory, 4)
	at := oldestShown(pageRows(a))
	if at < 0 {
		t.Fatal("no history line is drawn")
	}

	a = wheeled(a, tea.MouseButtonWheelUp, 1)

	if got := oldestShown(pageRows(a)); got >= at {
		t.Errorf("the oldest line drawn went from %d to %d: the wheel did not bring older lines in", at, got)
	}
}

// A page at rest has nowhere to go down, so the wheel is the transcript's: a
// reader who had scrolled back is still brought forward by it.
func TestWheelDownAtRestStillBringsAScrolledReaderForward(t *testing.T) {
	a := askingAfter(t, 40, pageHistory, 4)
	at := oldestShown(pageRows(a))

	a = wheeled(a, tea.MouseButtonWheelDown, 1)

	if got := oldestShown(pageRows(a)); got <= at {
		t.Errorf("the oldest line drawn went from %d to %d: the wheel did not bring the reader forward", at, got)
	}
}

// With nothing above the card to scroll, a card taller than the pane is still
// reached: the page is the card and the composer alone.
func TestACardTallerThanThePaneIsReachedWithNoTranscriptAtAll(t *testing.T) {
	a := askingAfter(t, 10, 0, 0)
	if hasRow(pageRows(a), "@john") {
		t.Fatalf("the composer is on screen at rest, so there is nothing to scroll to:\n%s", strings.Join(pageRows(a), "\n"))
	}

	a = wheeled(a, tea.MouseButtonWheelDown, 6)

	if rows := pageRows(a); !hasRow(rows, "@john") || !hasRow(rows, cardKeysCut) {
		t.Errorf("the foot of the card and the composer are out of reach:\n%s", strings.Join(rows, "\n"))
	}
}

// The pointer over the roster has no transcript under it, and scrolls the pane
// holding the keys - which is the page when that pane has a card.
func TestTheWheelOverTheRosterScrollsTheFocusedPage(t *testing.T) {
	a := askingPage(t, 40)
	roster := a.regions().Room() + 2*a.regions().Cols[1] // inside the sidebar, whatever the width
	m, _ := a.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp, X: roster, Y: 2})
	a = m.(App)

	if hasRow(pageRows(a), "@john") {
		t.Errorf("the composer is still on screen after a wheel over the sidebar:\n%s", strings.Join(pageRows(a), "\n"))
	}
}

// Opening an answer is typing in the composer, which has to be on screen to be
// typed into: the page comes back to it.
func TestOpeningAnAnswerBringsTheComposerBack(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 1)
	if hasRow(pageRows(a), "@john") || !a.cardFullyDrawn() {
		t.Fatal("one notch should take the composer off screen and leave the card whole")
	}

	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardDenyKey}})

	rows := pageRows(a)
	if !hasRow(rows[len(rows)-3:], "deny reason") {
		t.Errorf("the reason box opened with its composer out of sight:\n%s", strings.Join(rows, "\n"))
	}
}

// At rest in a pane that shows the transcript nothing about its rows has moved,
// so a press still anchors there - with the composer scrolled away for the card
// as in any other. A pane too short for its card opens on the card's foot with
// the transcript scrolled off above it, so a press there takes the screen as drawn.
func TestAPressAtRestAnchorsInTheTranscriptItShows(t *testing.T) {
	for _, tc := range []struct {
		height int
		screen bool
	}{{40, false}, {14, false}, {10, true}} {
		a := askingPage(t, tc.height)

		a = a.press(pageWheelX, 0)

		if a.sel.onScreen != tc.screen || a.sel.inComposer {
			t.Errorf("at %d rows a press at rest took %+v, want onScreen=%v", tc.height, a.sel, tc.screen)
		}
	}
}

// A window resized under a scrolled page is drawn at the new size and its top is
// the transcript's first line, and the wheel answers at once: the page holds no
// rows past its own, so there are no dead notches to scroll back through.
func TestAResizeUnderAScrolledPageStaysInsideThePane(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 1000)

	for i, height := range []int{12, 20, 60} {
		a = a.withSize(narrowColumns, height)
		if got := len(strings.Split(stripANSI(a.View()), "\n")); got != height {
			t.Errorf("at %d rows the frame is %d rows tall", height, got)
		}
		if i == 0 { // the offset from the bottom outlives a resize, and a shorter window has further to go
			a = wheeled(a, tea.MouseButtonWheelUp, 100)
		}
		if !hasRow(pageRows(a)[:1], "Wake") {
			t.Errorf("at %d rows the top of the page is not the transcript's first line:\n%s", height, strings.Join(pageRows(a), "\n"))
		}
		if back := wheeled(a, tea.MouseButtonWheelDown, 1); stripANSI(back.View()) == stripANSI(a.View()) {
			t.Errorf("at %d rows the first notch down from the top moved nothing: the page has rows past its own", height)
		}
	}
}

// A pane too short for its card scrolls up as well as down: past the card's top
// the transcript comes in above it.
func TestAShortPaneScrollsUpIntoItsTranscript(t *testing.T) {
	a := askingPage(t, 10)
	if hasRow(pageRows(a), "history line 39") {
		t.Fatal("the pane already shows the transcript, so there is nothing for the wheel to bring in")
	}
	// The card's top is clipped-up rows above the window; one notch past it is the transcript.
	place, _, _ := a.pageOf("s1")
	notches := (place.clipped-place.up+wheelLines-1)/wheelLines + 1

	a = wheeled(a, tea.MouseButtonWheelUp, notches)

	rows := pageRows(a)
	if !hasRow(rows, "history line 39") || !hasRow(rows, "has a question") {
		t.Errorf("%d notches up did not bring the transcript in above the card:\n%s", notches, strings.Join(rows, "\n"))
	}
}

// A card the pane cannot hold opens on its bottom, however short the pane: the
// key line says the keys wait for the whole card, and the question is a wheel
// notch above (the owner's 2026-11-02 ruling, reversing 2026-10-20's top).
func TestAPaneTooShortForItsCardOpensOnItsBottom(t *testing.T) {
	for height := 7; height <= 11; height++ {
		a := askingAfter(t, height, 0, 0)
		if rows := pageRows(a); !hasRow(rows, cardKeysCut) || hasRow(rows, "has a question") {
			t.Errorf("at %d rows the card's foot is not what the pane opens on:\n%s", height, strings.Join(rows, "\n"))
		}
	}
}

// A card is answerable at rest whenever the pane can hold it at all: one exactly
// the pane's height opens with the transcript's row scrolled off and its keys live,
// and only a card taller than the pane waits for the window to grow.
func TestACardThePaneCanHoldIsAnswerableAtRest(t *testing.T) {
	var sawFit, sawCut bool
	for height := 7; height <= 22; height++ {
		a := askingPage(t, height)
		width, pane, _ := a.focusedPane() // the strip under the pane takes rows of the window
		fits := lipgloss.Height(a.cardBlock("s1", width, false)) <= pane
		if got := a.cardFullyDrawn(); got != fits {
			t.Errorf("in a %d-row pane the card reads answerable=%v, want %v (the pane can hold it: %v):\n%s", pane, got, fits, fits, strings.Join(pageRows(a), "\n"))
		}
		sawFit, sawCut = sawFit || fits, sawCut || !fits
	}
	if !sawFit || !sawCut {
		t.Fatalf("the sweep never crossed the card's height (fit=%v cut=%v)", sawFit, sawCut)
	}
}

// An answer being written is read whether or not the card is all on screen, so
// its key line stays the answer's - it is the only account of what ↵ does.
func TestAnAnswerBeingWrittenKeepsItsKeyLineWhenTheCardIsCut(t *testing.T) {
	a := askingPage(t, 14)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{cardDenyKey}})
	if a.cardFullyDrawn() {
		t.Fatal("the card is whole with its reason box open, so this test is not exercising a cut card")
	}

	rows := pageRows(a)

	if !hasRow(rows, "esc cancels") || hasRow(rows, cardKeysCut) {
		t.Errorf("the key line over the reason box is not the answer's:\n%s", strings.Join(rows, "\n"))
	}
}

// ⌃E opens an option's whole description, which can outgrow the pane: the card
// opens on its foot, so the end of the description is in view, and the wheel
// reaches the question above it.
func TestAnExpandedDescriptionTooLongForThePaneOpensOnItsEnd(t *testing.T) {
	const lastWords = "would fail to parse it"
	a := askingPage(t, 14)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})
	if a.cardFullyDrawn() {
		t.Fatal("the whole description fits, so this test is not exercising a card that outgrew its pane")
	}
	if rows := pageRows(a); !hasRow(rows, lastWords) {
		t.Errorf("the end of the description is not in view:\n%s", strings.Join(rows, "\n"))
	}

	for i := 0; i < 20 && !hasRow(pageRows(a), "has a question"); i++ {
		a = wheeled(a, tea.MouseButtonWheelUp, 1)
	}

	if rows := pageRows(a); !hasRow(rows, "has a question") {
		t.Errorf("the question above the description is out of reach:\n%s", strings.Join(rows, "\n"))
	}
}

// One notch up is the whole page moving down by exactly the notch's rows, with
// the rows above arriving at the top: for a reader on the newest line and for one
// who had scrolled back, in a pane that fits its card and in one that does not.
// Once the window's top is in the transcript, the rows a notch brings are the
// transcript's own lines, read off the transcript and not off a frame.
func TestOneNotchShiftsThePageByTheNotchesRows(t *testing.T) {
	for _, height := range []int{10, 14, 40} {
		for _, readBack := range []int{0, 4} {
			a := askingAfter(t, height, pageHistory, readBack)
			width, _, _ := a.focusedPane() // the roster beside the pane does not scroll
			inPane := func(rows []string) []string {
				out := make([]string, len(rows))
				for i, r := range rows {
					out[i] = string([]rune(r)[:width])
				}
				return out
			}
			shifted := func(before, after []string) {
				t.Helper()
				for i := range len(before) - wheelLines {
					if before[i] != after[i+wheelLines] {
						t.Fatalf("height %d, read back %d: row %d of the frame is not row %d of the frame before:\nbefore:\n%s\nafter:\n%s",
							height, readBack, i+wheelLines, i, strings.Join(before, "\n"), strings.Join(after, "\n"))
					}
				}
			}

			before := inPane(pageRows(a))
			a = wheeled(a, tea.MouseButtonWheelUp, 1)
			shifted(before, inPane(pageRows(a)))

			// Up to where a notch's rows are all the transcript's: past the card's top
			// (a card on its foot has rows of its own to bring in first) and the floor.
			for i := 0; i < 50; i++ {
				if place, _, _ := a.pageOf("s1"); place.up >= place.clipped+wheelLines {
					break
				}
				a = wheeled(a, tea.MouseButtonWheelUp, 1)
			}
			before = inPane(pageRows(a))
			a = wheeled(a, tea.MouseButtonWheelUp, 1)
			after := inPane(pageRows(a))
			shifted(before, after)
			place, _, _ := a.pageOf("s1")
			first := place.top - (place.up - place.clipped)
			for i, line := range a.dms["s1"].tr.lines.slice(first, first+wheelLines) {
				if got, want := strings.TrimRight(after[i], " "), strings.TrimRight(stripANSI(line), " "); got != want {
					t.Errorf("height %d, read back %d: row %d is %q, want the transcript's line %d, %q", height, readBack, i, got, first+i, want)
				}
			}
		}
	}
}
