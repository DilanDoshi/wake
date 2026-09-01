package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A reader following the newest message sees no banner - the ordinary case,
// and the one every existing screen test already exercises without seeing it.
func TestAReaderAtTheBottomSeesNoFollowBanner(t *testing.T) {
	d := longConversation(t, 30)
	if strings.Contains(visible(d, 60, 12), followBannerText) {
		t.Errorf("a reader at the bottom sees the follow banner:\n%s", visible(d, 60, 12))
	}
}

// A single stray scroll tick - a trackpad notch, not a deliberate scroll-back
// - detaches the reader from the newest line with nothing else on screen
// saying so. The banner is that signal.
func TestAScrolledReaderSeesTheFollowBanner(t *testing.T) {
	d := longConversation(t, 30)
	d.tr = d.tr.scrolledUp(1)

	if !strings.Contains(visible(d, 60, 12), followBannerText) {
		t.Errorf("a reader one line off the bottom sees no follow banner:\n%s", visible(d, 60, 12))
	}
}

// The banner survives an active turn: the preview and the working line update
// live regardless of scroll position, so the reader who has drifted needs the
// signal for the whole time those keep moving under a transcript that does
// not.
func TestTheFollowBannerSurvivesAStreamingPreview(t *testing.T) {
	d := longConversation(t, 30)
	d.tr = d.tr.scrolledUp(1)
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}

	for _, n := range []int{0, 1, 3, 10, 40} {
		d = tokens(d, strings.Repeat("token ", n))
		out := d.View(60, 12)
		if !strings.Contains(out, followBannerText) {
			t.Errorf("n=%d: the follow banner disappeared while the preview grew:\n%s", n, out)
		}
	}
}

// Once the reader scrolls back to the true bottom, the banner is gone -
// TestReturningToTheBottomResumesFollowing's own claim, one layer up.
func TestReturningToTheBottomClearsTheFollowBanner(t *testing.T) {
	d := longConversation(t, 30)
	d.tr = d.tr.scrolledUp(20).toBottom()

	if strings.Contains(visible(d, 60, 12), followBannerText) {
		t.Errorf("the banner survived a return to the bottom:\n%s", visible(d, 60, 12))
	}
}

// A conversation that fits entirely on screen is always "at the bottom" -
// atBottom's own trivial case - so it never draws a banner nobody can act on.
func TestAShortConversationNeverDrawsTheFollowBanner(t *testing.T) {
	d := longConversation(t, 3)
	if strings.Contains(visible(d, 60, 20), followBannerText) {
		t.Errorf("a conversation with room to spare drew the follow banner:\n%s", visible(d, 60, 20))
	}
}

// Clicking the banner is a click and not a drag - endSelection's own
// distinction - and it lands the reader back at the newest message, the same
// place TestReturningToTheBottomResumesFollowing reaches by scrolling.
//
// Found on the pane as it is actually drawn (TestADragOnAMenuPinnedOverTheComposerTakesNothing's
// own reason) rather than computed, so this cannot pass by the click and the
// banner agreeing on the same wrong row.
func TestClickingTheFollowBannerJumpsToTheLatestMessage(t *testing.T) {
	a := splitApp(t, 200, 40, 4) // room + DM "s1"="alex" open
	col := a.columnOf("s1")
	a = a.refocus("s1") // isolates the banner click from the separate "first click only focuses" rule

	d := *a.dms["s1"]
	for i := range 30 {
		d = d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: fmt.Sprintf("message-%02d", i)})
	}
	d.tr = d.tr.scrolledUp(1) // a stray tick, not a deliberate scroll-back
	a = a.withDM("s1", d)

	w, h := a.regions().Cols[col], a.paneHeight()
	rows := strings.Split(a.dmPane("s1", w, h), "\n")
	bannerRow := slices.IndexFunc(rows, func(r string) bool {
		return strings.Contains(ansi.Strip(r), "new messages below")
	})
	if bannerRow < 0 {
		t.Fatalf("no follow banner is drawn, so this proves nothing about clicking it:\n%s", strings.Join(rows, "\n"))
	}

	x := midOf(a.regions(), col)
	a, _ = a.mouse(pressAt(x, bannerRow))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: bannerRow})

	if !a.dms["s1"].tr.atBottom() {
		t.Errorf("clicking the follow banner at its drawn row %d did not return to the bottom", bannerRow)
	}
}

// The regression Codex's review caught: DM.View resizes a throwaway copy
// whenever chrome moves without a geometry change (a streaming preview
// appearing), and the stored transcript is left too tall by exactly what that
// chrome took. A banner hit decided from the stored transcript would land one
// row off from where the banner is actually drawn - this pins the fix
// (deciding the hit at press time, from the same measurement transcriptRows
// uses) against exactly that drift.
func TestClickingTheFollowBannerStillWorksWhileAPreviewHasGrownTheChromeWithNoResize(t *testing.T) {
	a := splitApp(t, 200, 40, 4)
	col := a.columnOf("s1")
	a = a.refocus("s1")

	d := *a.dms["s1"]
	for i := range 30 {
		d = d.Append(core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: fmt.Sprintf("message-%02d", i)})
	}
	d.tr = d.tr.scrolledUp(1)
	// Grows the chrome (a working line and a streaming preview) without ever
	// calling SetSize - Append never does, for a partial - so the DM's own
	// stored tr.height stays exactly what it was before this loop.
	d.Agent = Agent{ID: "s1", State: rpc.StateWorking}
	before := d.tr.height
	d = tokens(d, strings.Repeat("token ", 40))
	if d.tr.height != before {
		t.Fatalf("the stored transcript height moved to %d from %d: this test needs it to stay stale", d.tr.height, before)
	}
	a = a.withDM("s1", d)

	w, h := a.regions().Cols[col], a.paneHeight()
	rows := strings.Split(a.dmPane("s1", w, h), "\n")
	bannerRow := slices.IndexFunc(rows, func(r string) bool {
		return strings.Contains(ansi.Strip(r), "new messages below")
	})
	if bannerRow < 0 {
		t.Fatalf("no follow banner is drawn, so this proves nothing about clicking it:\n%s", strings.Join(rows, "\n"))
	}

	x := midOf(a.regions(), col)
	a, _ = a.mouse(pressAt(x, bannerRow))
	a, _ = a.mouse(tea.MouseMsg{Action: tea.MouseActionRelease, X: x, Y: bannerRow})

	if !a.dms["s1"].tr.atBottom() {
		t.Errorf("clicking the follow banner at its drawn row %d did not return to the bottom, with the chrome grown and the stored transcript left stale", bannerRow)
	}
}
