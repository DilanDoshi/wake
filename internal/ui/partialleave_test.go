package ui

// A preview does not survive its pane going away.
//
// App.wants freezes accumulation while a conversation is not drawn, which is
// right, but nothing dropped what had already accumulated - so a pane closed
// mid-block and reopened before that block landed carried on appending to the
// old tail. What the operator read was the text from before, joined to the text
// from after, with everything generated in between missing: a sentence the
// agent never wrote, in the one part of the pane that is meant to be a live
// picture of what it is writing now.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func partialEvent(text string) core.Event {
	return core.Event{Kind: core.KindPartialText, Text: text}
}

// tokenFrame is one token arriving for a session, as the daemon sends it.
//
// Named for what it carries rather than for its kind, because inbox.go owns
// partialFrame - the predicate that keeps a preview from evicting the record.
func tokenFrame(id, text string) rpc.Frame {
	return rpc.Frame{Kind: rpc.FrameEvent, SessionID: id,
		Event: &core.Event{Kind: core.KindPartialText, SessionID: id, Text: text}}
}

// The reproduction, at the level the bug lives: leaving is what has to drop it,
// because leaving is the only thing that happens on every path a pane stops
// being drawn on - closed, displaced by another conversation, or pushed off
// screen by a width that cannot afford it.
func TestLeavingAConversationDropsThePreviewItWasShowing(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).Append(partialEvent("half a sent"))
	assertShows(t, d, 60, 20, "half a sent")

	d = d.Leave().Append(partialEvent("and now this arrived after reopening"))

	if out := visible(d, 60, 20); strings.Contains(out, "half a sent") {
		t.Errorf("the preview spliced the text from before the pane went away onto the text from after:\n%s", out)
	}
	assertShows(t, d, 60, 20, "and now this arrived after reopening")
}

// The early returns in Leave are about the *marker* and not about leaving, and
// a conversation whose only content is a preview trips both of them: partials
// never enter d.events, so events.len() is zero and the marker has nothing to
// anchor to. The preview still has to go.
func TestLeavingDropsThePreviewEvenWithNothingToMark(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).Append(partialEvent("only ever a preview"))
	if d.events.len() != 0 {
		t.Fatalf("a partial reached d.events (%d), so this test no longer exercises the early return it is about", d.events.len())
	}

	d = d.Leave()

	if out := visible(d, 60, 20); strings.Contains(out, "only ever a preview") {
		t.Errorf("a conversation with nothing to mark kept its preview across leaving:\n%s", out)
	}
}

// And leaving twice, which Leave already treats as one absence for the marker.
// The preview is not the marker and has no such rule - it is simply gone.
func TestLeavingTwiceKeepsThePreviewGone(t *testing.T) {
	d := NewDM("s1", "alex").SetSize(60, 20).
		Append(core.Event{Kind: core.KindAssistantText, Text: "an earlier answer"}).
		Append(partialEvent("being written"))

	d = d.Leave().Leave()

	if out := visible(d, 60, 20); strings.Contains(out, "being written") {
		t.Errorf("the preview survived a second leave:\n%s", out)
	}
}

// The whole-App path the reviewer reproduced: one conversation is streaming,
// another takes its pane, and the first is opened again before its block lands.
func TestAConversationDisplacedFromItsPaneComesBackWithNoStalePreview(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "john")
	a.roster.Selected = "s1"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	a = a.applyFrame(tokenFrame("s1", "half a sent"))
	if !strings.Contains(shown(a), "half a sent") {
		t.Fatalf("the preview is not on screen to begin with:\n%s", shown(a))
	}

	// john takes the pane sydney was in, then sydney comes back to it.
	a.roster.Selected = "s2"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})
	a.roster.Selected = "s1"
	a, _ = pressKey(a, tea.KeyMsg{Type: tea.KeyCtrlD})

	a = a.applyFrame(tokenFrame("s1", "and now this arrived after reopening"))

	if out := shown(a); strings.Contains(out, "half a sent") {
		t.Errorf("sydney came back to the preview she left, and the two fragments read as one sentence:\n%s", out)
	}
}
