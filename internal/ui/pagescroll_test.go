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
	ev := questionAsk(t)
	a := newRoomApp(t).withSize(narrowColumns, height).withAgents("john")
	a = pick(a, "s1").openDMWith("s1", "john").applyGeometry()
	for i := range history {
		a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1",
			Event: &core.Event{Kind: core.KindAssistantText, Text: fmt.Sprintf("history line %02d", i)}})
	}
	a = wheeled(a, tea.MouseButtonWheelUp, readBack)
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

// A pane shorter than the card itself shows the card's top, and the wheel
// reaches the rest. Its keys stay dead until the whole card fits, and the key
// line, where it is drawn, says so rather than advertising keys that do nothing.
func TestACardTallerThanThePaneIsReachedByTheWheel(t *testing.T) {
	a := askingPage(t, 10)
	if hasRow(pageRows(a), "[d]eny") {
		t.Fatalf("the key line is on screen at rest in a pane shorter than the card:\n%s", strings.Join(pageRows(a), "\n"))
	}
	if !hasRow(pageRows(a), "has a question") {
		t.Fatalf("the card's top is not what a short pane opens on:\n%s", strings.Join(pageRows(a), "\n"))
	}

	a = wheeled(a, tea.MouseButtonWheelDown, 3)
	rows := pageRows(a)

	if !hasRow(rows, cardKeysCut) {
		t.Errorf("scrolled to the card's foot, the key line does not say why the keys are off:\n%s", strings.Join(rows, "\n"))
	}
	if hasRow(rows, "[d]eny") {
		t.Errorf("the card advertises keys it cannot honour:\n%s", strings.Join(rows, "\n"))
	}
	if !hasRow(rows, "@john") {
		t.Errorf("the composer is not on screen at the bottom of the page:\n%s", strings.Join(rows, "\n"))
	}
	if a.cardFullyDrawn() {
		t.Error("a card taller than its pane reads as fully drawn")
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

// At rest nothing about a pane's rows has moved, so a press still anchors in the
// transcript - in a pane too short for its card as in any other.
func TestAPressAtRestStillAnchorsInTheTranscript(t *testing.T) {
	for _, height := range []int{40, 10} {
		a := askingPage(t, height)

		a = a.press(pageWheelX, 0)

		if a.sel.onScreen || a.sel.inComposer {
			t.Errorf("at %d rows a press at rest took %+v instead of anchoring in the transcript", height, a.sel)
		}
	}
}

// A window resized under a scrolled page is drawn at the new size, and the page
// stays inside the rows it has.
func TestAResizeUnderAScrolledPageStaysInsideThePane(t *testing.T) {
	a := askingPage(t, 40)
	a = wheeled(a, tea.MouseButtonWheelUp, 1000)

	for _, height := range []int{20, 60, 12} {
		a = a.withSize(narrowColumns, height)
		if got := len(strings.Split(stripANSI(a.View()), "\n")); got != height {
			t.Errorf("at %d rows the frame is %d rows tall", height, got)
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

	a = wheeled(a, tea.MouseButtonWheelUp, 1)

	rows := pageRows(a)
	if !hasRow(rows, "history line 39") || !hasRow(rows, "has a question") {
		t.Errorf("one notch up did not bring the transcript in above the card:\n%s", strings.Join(rows, "\n"))
	}
}

// However short the pane, a card too tall for it opens on its own top: the
// question is what somebody has to read first.
func TestAPaneTooShortForItsCardOpensOnTheCardsTop(t *testing.T) {
	for height := 7; height <= 11; height++ {
		a := askingAfter(t, height, 0, 0)
		if rows := pageRows(a); !hasRow(rows, "has a question") {
			t.Errorf("at %d rows the card's top is not what the pane opens on:\n%s", height, strings.Join(rows, "\n"))
		}
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

// ⌃E opens an option's whole description, which can outgrow the pane: the wheel
// is how the end of it is read.
func TestAnExpandedDescriptionTooLongForThePaneIsReachedByTheWheel(t *testing.T) {
	const lastWords = "would fail to parse it"
	a := askingPage(t, 14)
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlE})
	if hasRow(pageRows(a), lastWords) {
		t.Fatal("the whole description already fits, so there is nothing for the wheel to reach")
	}

	a = wheeled(a, tea.MouseButtonWheelDown, 1)

	if rows := pageRows(a); !hasRow(rows, lastWords) {
		t.Errorf("the end of the description is out of reach:\n%s", strings.Join(rows, "\n"))
	}
}
