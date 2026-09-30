package ui

// A DM-sent reply that lands during the 80ms resize settle is judged against
// the geometry the frame is settling *to*, not the committed one View still
// draws. The committed layout lags the terminal for the whole settle, so a
// wide→narrow resize that takes the DM off screen used to hold the reply out of
// the room for exactly that window - deferred.md's 2026-09-15 gap.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// wideColumns draws the room and one DM side by side.
const wideColumns = 200

// settlingBeside is john's DM open beside the room at a wide width, the turn
// sent from john's DM, and the terminal narrowed past the takeover with the
// settle not yet run - so the layout View draws is still the wide one.
func settlingBeside(t *testing.T, focusDM bool) App {
	t.Helper()
	a := newRoomApp(t).withSize(wideColumns, 40).withRoster(
		rpc.SessionStatus{ID: "s2", Name: "john", Dir: "/repos/api", State: rpc.StateWorking},
	)
	a = a.openDMWith("s2", "john")
	if !focusDM {
		a = a.showRoom()
	}
	if !a.drawnConversations()("s2") {
		t.Fatal("john's DM is not drawn at the wide width; the test is not starting beside the room")
	}
	a.fleet = a.fleet.sending("s2", true)
	a = a.withSize(narrowColumns, 40)
	if a.layout.Width != wideColumns {
		t.Fatalf("the narrow width was committed at once (layout %d); the test is not inside a settle", a.layout.Width)
	}
	return a
}

// johnSays is one completed block of john's DM-sent turn.
func johnSays(a App, text string) App {
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s2", Event: &core.Event{
		Kind: core.KindAssistantText, Text: text,
	}})
}

// The reported gap: the room holds the keys, so below the takeover the room is
// the one column drawn and john's DM is going off screen. A reply landing
// before the settle reaches the room.
//
// Mutation check: read drawnConversations off a.regions() again and the reply
// is held out of the room for the settle.
func TestADMReplyLandingMidSettlePromotesWhenTheResizeTakesTheDMOffScreen(t *testing.T) {
	a := johnSays(settlingBeside(t, false), "the reply that raced the resize")
	if out := roomShown(a.room, narrowColumns, 40); !strings.Contains(out, "raced the resize") {
		t.Errorf("a DM reply landing mid-settle was held out of the room although the resize is taking the DM off screen:\n%s", out)
	}
}

// The case the old-layout∩clip reading gets wrong: the DM holds the keys, so
// below the takeover the DM is the column that stays and the room goes. The DM
// is still being read, so its reply stays private.
func TestADMReplyLandingMidSettleStaysPrivateWhenTheDMKeepsTheScreen(t *testing.T) {
	a := johnSays(settlingBeside(t, true), "the private answer mid-settle")
	if out := roomShown(a.room, narrowColumns, 40); strings.Contains(out, "private answer mid-settle") {
		t.Errorf("a DM reply reached the room while the resize was leaving that DM as the only pane on screen:\n%s", out)
	}
}

// And the other direction: narrow→wide brings the DM back beside the room, so
// a reply landing mid-settle belongs to the pane about to be drawn.
func TestADMReplyLandingMidSettleStaysPrivateWhenTheResizeBringsTheDMBack(t *testing.T) {
	a := newRoomApp(t).withSize(narrowColumns, 40).withRoster(
		rpc.SessionStatus{ID: "s2", Name: "john", Dir: "/repos/api", State: rpc.StateWorking},
	)
	a = a.openDMWith("s2", "john").showRoom()
	if a.drawnConversations()("s2") {
		t.Fatal("john's DM is drawn below the takeover with the room focused; the test is not starting off screen")
	}
	a.fleet = a.fleet.sending("s2", true)
	a = a.withSize(wideColumns, 40)
	if a.layout.Width != narrowColumns {
		t.Fatalf("the wide width was committed at once (layout %d); the test is not inside a settle", a.layout.Width)
	}
	a = johnSays(a, "the answer the widening brings back")
	if out := roomShown(a.room, wideColumns, 40); strings.Contains(out, "widening brings back") {
		t.Errorf("a DM reply reached the room although the resize is putting that DM back on screen:\n%s", out)
	}
}
