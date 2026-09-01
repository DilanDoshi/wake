package ui

// Where the picker and the card sit in a pane: below the transcript and
// directly above the composer, the query bar they answer - not pinned at the
// pane's top, rows away from it.
//
// The completion menu already draws there (completionpane_test.go); these hold
// the picker and the card to the same place, which is what the reported "the
// /effort menu appears at the top instead of the query bar" asked for.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// paneRow is the first row of a rendered pane that contains sub, or -1.
// lastPaneRow is the bottom-most row carrying sub, which is how the composer
// is found: it is the lowest box in a pane, under the card when there is one.
func lastPaneRow(rows []string, sub string) int {
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.Contains(rows[i], sub) {
			return i
		}
	}
	return -1
}

func paneRow(rows []string, sub string) int {
	for i, r := range rows {
		if strings.Contains(r, sub) {
			return i
		}
	}
	return -1
}

// composerTopGlyph opens a rounded box. The composer draws one and so does an
// answerable card, which is framed - so a *first* match is the card whenever
// one is up, and the composer is found with lastPaneRow instead.
const composerTopGlyph = "╭"

// bannerVersion is on the first row of every conversation's banner, which is
// the top of the transcript.
const bannerVersion = "0.1.0"

// The picker opens below the transcript and directly above the composer in a
// conversation - not pinned at the pane's top.
func TestThePickerDrawsAboveTheComposerNotAtThePaneTop(t *testing.T) {
	fresh(t)
	a := dmApp(nil, Stream{}, "s1", "alex").withAgents("alex").withSize(120, 40)
	m, _ := typeAndSubmit(a, SlashPrefix+effortCommand)
	a = m.(App)
	if !a.picker.Open() {
		t.Fatal("/effort opened no picker, so there is nothing to place")
	}
	if !contains(a.picker.Options, "xhigh") {
		t.Fatalf("the effort picker offers %v, none of them the marker this test looks for", a.picker.Options)
	}

	rows := strings.Split(stripANSI(a.dmPane("s1", 100, 30)), "\n")
	banner := paneRow(rows, bannerVersion)
	menu := paneRow(rows, "xhigh")
	box := lastPaneRow(rows, composerTopGlyph)
	if banner < 0 || menu < 0 || box < 0 {
		t.Fatalf("banner=%d picker=%d composer=%d: one is not in the pane\n%s",
			banner, menu, box, strings.Join(rows, "\n"))
	}
	if banner >= menu {
		t.Errorf("the picker is at row %d and the banner at %d: it is pinned above the transcript, "+
			"not at the query bar\n%s", menu, banner, strings.Join(rows, "\n"))
	}
	if menu >= box {
		t.Errorf("the picker is at row %d and the composer at %d: it is not above the query bar\n%s",
			menu, box, strings.Join(rows, "\n"))
	}
}

// And the same in the room, where a mention opens a picker while the keys stay
// on the group chat.
func TestThePickerDrawsAboveTheRoomComposer(t *testing.T) {
	fresh(t)
	a := newRoomApp(t).withSize(120, 40).withAgents("alex")
	a, _ = pressKey(a.withDraft("@alex "+SlashPrefix+effortCommand), tea.KeyMsg{Type: tea.KeyEnter})
	if !a.picker.Open() {
		t.Fatal("@alex /effort opened no picker in the room")
	}

	rows := strings.Split(stripANSI(a.roomPane(100, 30)), "\n")
	banner := paneRow(rows, bannerVersion)
	menu := paneRow(rows, "xhigh")
	box := lastPaneRow(rows, composerTopGlyph)
	if banner < 0 || menu < 0 || box < 0 {
		t.Fatalf("banner=%d picker=%d composer=%d: one is not in the room pane\n%s",
			banner, menu, box, strings.Join(rows, "\n"))
	}
	if banner >= menu || menu >= box {
		t.Errorf("the room picker is at row %d, banner at %d, composer at %d: want banner < picker < composer\n%s",
			menu, banner, box, strings.Join(rows, "\n"))
	}
}

// cardFullyDrawn must agree with what the draw actually clips, in both
// directions: if it reports the keys are safe to read, the card's key line has
// to be on screen - and if the key line is on screen, it must report so, or the
// answer keys are advertised but fall through to the composer while the agent
// stays blocked.
//
// The card is clipped by DM.menuRows, which runs inside DM.View after withBar
// refreshes the status bar from the live agent. cardFullyDrawn measures its
// floor through dmFor, which carries the live agent but leaves the bar cached -
// and a status push updates the fleet without re-sizing the stored DM, so that
// cache can lag by the bar's one row. A floor short by that row calls a card
// "fully drawn" whose key line the draw then clips. The other direction is the
// composerGap: the stored DM carries no menu, so its floor charges the blank row
// above the box that a pinned card drops, and a floor one row too tall calls a
// fully-drawn card clipped. Both are the accident the arm-and-confirm dance
// exists to prevent, so the sweep asserts equality and requires crossing the
// boundary.
func TestCardFullyDrawnMatchesWhatTheDrawClips(t *testing.T) {
	ev := questionAsk(t)
	a := newRoomApp(t).withSize(narrowColumns, 40).withAgents("john")
	a = pick(a, "s1").openDMWith("s1", "john").applyGeometry()

	// The Cwd arrives after the pane was sized, and the ask after it. The card
	// is what diverges: it lives on App.cards and is assembled into the pane by
	// menuBlock at draw time, so the stored DM never knows its rows.
	st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "john", State: rpc.StateIdle, Cwd: "/repo/api"},
	}}
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: ev})

	sawDrawn, sawClipped := false, false
	for h := 12; h <= 40; h++ {
		a2, _ := a.resized(narrowColumns, h)
		a2 = settle(a2)
		width, height, ok := a2.focusedPane()
		if !ok {
			continue
		}
		pane := stripANSI(a2.dmPane("s1", width, height))
		keyLineDrawn := strings.Contains(pane, cardDenyLabel)
		if fully := a2.cardFullyDrawn(); fully != keyLineDrawn {
			t.Fatalf("at terminal height %d cardFullyDrawn()=%v but the card's key line (%q) drawn=%v:\n%s",
				h, fully, cardDenyLabel, keyLineDrawn, pane)
		}
		if keyLineDrawn {
			sawDrawn = true
		} else {
			sawClipped = true
		}
	}
	if !sawDrawn || !sawClipped {
		t.Fatalf("the sweep never crossed the boundary (drawn=%v clipped=%v): the invariant was not exercised", sawDrawn, sawClipped)
	}
}

// A blocked agent's card is drawn where its answer is typed: below the
// transcript, above the composer - not pinned at the top of the pane.
func TestACardDrawsAboveTheComposerNotAtThePaneTop(t *testing.T) {
	a, _ := asking(t, narrowColumns)
	if _, ok := a.cards.For("s1"); !ok {
		t.Fatal("the conversation is pinning no ask, so there is no card to place")
	}

	rows := strings.Split(stripANSI(a.dmPane("s1", 100, 30)), "\n")
	banner := paneRow(rows, bannerVersion)
	card := paneRow(rows, cardHasQuestion)
	box := lastPaneRow(rows, composerTopGlyph)
	if banner < 0 || card < 0 || box < 0 {
		t.Fatalf("banner=%d card=%d composer=%d: one is not in the pane\n%s",
			banner, card, box, strings.Join(rows, "\n"))
	}
	if banner >= card {
		t.Errorf("the card is at row %d and the banner at %d: it is pinned above the transcript, "+
			"not at the query bar\n%s", card, banner, strings.Join(rows, "\n"))
	}
	if card >= box {
		t.Errorf("the card is at row %d and the composer at %d: it is not above the query bar\n%s",
			card, box, strings.Join(rows, "\n"))
	}
}
