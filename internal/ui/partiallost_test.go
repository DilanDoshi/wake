package ui

// A pane that missed a token must never format. A fence it never saw open would
// read as prose and be cut inside. Every place a token can be lost reaches the
// pane the same way: the pane stops trusting the block it is in, and previews it
// plain until the next one begins. Nothing here is about a pane coming back - a
// token lost to a pane that stays on screen is as lost.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// fenceBody is lines a pane that never saw the fence open would cut at every
// blank one: a paragraph break to it, and code to the agent.
const fenceBody = "x := inside(the, fence)\n\ny := still(inside)\n\nz := code()\n\n"

// lostTail is what an agent goes on writing after the block's start is gone, and
// what a pane that formatted it would have rendered.
func lostTail(a App, renders *int) string {
	a = a.streams("s1", fenceBody)
	return fmt.Sprintf("%d renders\n%s", *renders, shown(a))
}

// The reviewer's path: at a width where one column is drawn, opening a second
// conversation puts the first off screen with no Leave, App.wants then drops its
// tokens, and closing the second brings it back mid-block.
func TestAPaneOffScreenWithoutLeavingIsNotReadFromTheMiddleOfABlock(t *testing.T) {
	renders := chunkCounter(t)
	a := newRoomApp(t).withSize(narrowColumns, 40).withAgents("alex", "sydney").openDMWith("s1", "alex")
	a = a.applyFrame(startFrame("s1")).streams("s1", "an opening paragraph.\n\nand a second one")
	if *renders == 0 {
		t.Fatal("a pane that saw the block begin formatted nothing: the control for the rest")
	}

	a = a.openRight("s2", "sydney")
	if a.drawnConversations()("s1") {
		t.Fatalf("alex is still drawn at %d columns, so this is not the path under test", narrowColumns)
	}
	a = a.streams("s1", "continued, and then ```go\nopener := true\n\n") // dropped: nobody is looking
	a = a.hideDM(true)
	if !a.drawnConversations()("s1") {
		t.Fatal("alex did not come back when the conversation over it closed")
	}

	*renders = 0
	if got := lostTail(a, renders); *renders != 0 {
		t.Errorf("a pane that missed tokens formatted what followed:\n%s", got)
	}
	a = a.applyFrame(kindFrame("s1", core.KindAssistantText, "the block, landed"))
	a = a.applyFrame(startFrame("s1"))
	before := *renders
	a = a.streams("s1", "a fresh paragraph.\n\nand the next")
	if *renders == before {
		t.Errorf("the block after the landing was not formatted:\n%s", shown(a))
	}
}

// Every other way a pane stops being drawn is the same: the token it never gets
// unsyncs it, whichever key took the pane away.
func TestAPaneIsRawAfterAnyWayOfBeingTakenOffScreenMidBlock(t *testing.T) {
	for _, tc := range []struct {
		name string
		away func(App) App
	}{
		{"closed with ⌃W", func(a App) App { return a.closeDM() }},
		{"a column opened beside it with ⌃Y", func(a App) App { return a.openRight("s2", "sydney") }},
		{"pushed off by the room", func(a App) App { return a.showRoom() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			renders := chunkCounter(t)
			a := newRoomApp(t).withSize(narrowColumns, 40).withAgents("alex", "sydney").openDMWith("s1", "alex")
			a = a.applyFrame(startFrame("s1")).streams("s1", "one.\n\ntwo")
			a = tc.away(a)
			a = a.streams("s1", " and a fence opens ```\n\n")
			a = a.openDMWith("s1", "alex")
			*renders = 0
			if got := lostTail(a, renders); *renders != 0 {
				t.Errorf("formatted after %s:\n%s", tc.name, got)
			}
		})
	}
}

// A gap in the record is tokens gone too: whatever the ring evicted may have been
// the start of a block a pane is reading, and so may what a reattach missed.
func TestAGapInTheRecordMakesEveryPaneRawUntilItsNextBlock(t *testing.T) {
	for _, tc := range []struct {
		name string
		gap  func(App) App
	}{
		{"the window's ring evicted records", func(a App) App { return a.notedGap(3) }},
		{"the connection was replaced", func(a App) App {
			next, _ := a.reattached(reattachedMsg{})
			return next.(App)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			renders := chunkCounter(t)
			a := fmtApp(t).applyFrame(startFrame("s1")).streams("s1", "one.\n\ntwo")
			if *renders == 0 {
				t.Fatal("a pane that saw the block begin formatted nothing: the control for the rest")
			}
			a = tc.gap(a)
			*renders = 0
			if got := lostTail(a, renders); *renders != 0 {
				t.Errorf("formatted after %s:\n%s", tc.name, got)
			}
		})
	}
}

// ringApp is an App whose inbox the test fills by hand, and push applies what the
// inbox hands over the way Update does.
func ringApp(t *testing.T) App {
	t.Helper()
	a := fmtApp(t)
	a.in = newInbox()
	return a
}

func push(t *testing.T, a App, n int) App {
	t.Helper()
	m, _ := a.stream(streamMsg{batch: a.in.take(n), gen: a.gen})
	return m.(App)
}

// filler is a frame for another session that draws nothing.
func filler() rpc.Frame { return kindFrame("s2", core.KindTurnTokens, "") }

func fillRing(in *inbox, n int) {
	for range n {
		in.add(filler())
	}
}

// A token the ring has no room for is dropped where it stands. The next one for
// that session says so, at its own frame.
func TestARefusedFenceOpenerRendersNothingUntilLanding(t *testing.T) {
	a := ringApp(t)
	a.in.add(startFrame("s1"))
	a = push(t, a, takeLimit) // the pane has heard the block begin
	fillRing(a.in, inboxFrames)
	a.in.add(tokenFrame("s1", "```go\nopener := true\n\n")) // the ring is full: dropped
	if a.in.n != inboxFrames {
		t.Fatalf("%d frames held, want the ring full at %d", a.in.n, inboxFrames)
	}
	for a.in.n > inboxFrames/2 {
		a = push(t, a, takeLimit)
	}
	renders := chunkCounter(t)
	for _, tok := range pieces(fenceBody, 5) {
		a.in.add(tokenFrame("s1", tok))
	}
	a = push(t, a, takeLimit)
	for a.in.n > 0 {
		a = push(t, a, takeLimit)
	}
	if *renders != 0 {
		t.Errorf("%d chunks rendered from a block whose opener the ring refused:\n%s", *renders, shown(a))
	}
}

// And a fold the ring evicts to make room takes its tokens with it.
func TestAnEvictedFoldCarryingAFenceOpenerRendersNothingUntilLanding(t *testing.T) {
	a := ringApp(t)
	a.in.add(startFrame("s1"))
	a = push(t, a, takeLimit)
	a.in.add(tokenFrame("s1", "```go\nopener := true\n\n")) // the fold, at the head of the ring
	fillRing(a.in, inboxFrames-1)
	a.in.add(filler()) // one more: the fold is what goes
	if a.in.dropped != 0 || a.in.n != inboxFrames || len(a.in.folds) != 0 {
		t.Fatalf("dropped %d, %d held: the fixture did not evict the fold alone", a.in.dropped, a.in.n)
	}
	a = push(t, a, takeLimit)
	renders := chunkCounter(t)
	for _, tok := range pieces(fenceBody, 5) {
		a.in.add(tokenFrame("s1", tok))
	}
	for a.in.n > 0 {
		a = push(t, a, takeLimit)
	}
	if *renders != 0 {
		t.Errorf("%d chunks rendered from a block whose fold the ring evicted:\n%s", *renders, shown(a))
	}
}

// The mark is for a block: the message start after it retires it.
func TestAMessageStartRetiresTheInboxsMark(t *testing.T) {
	a := ringApp(t)
	a.in.add(startFrame("s1"))
	a = push(t, a, takeLimit)
	fillRing(a.in, inboxFrames)
	a.in.add(tokenFrame("s1", "refused")) // the ring is full
	a = push(t, a, takeLimit)
	a.in.add(kindFrame("s1", core.KindAssistantText, "the block, landed"))
	a.in.add(startFrame("s1"))
	a.in.add(tokenFrame("s1", "a clean block"))
	var last rpc.Frame
	for a.in.n > 0 {
		for _, f := range a.in.take(takeLimit).frames {
			last = f
		}
	}
	if last.Event == nil || last.Event.Kind != core.KindPartialText || last.Lost {
		t.Errorf("the last frame is %+v, want the clean block's token with no mark: the mark outlived the message start", last)
	}
}

// The daemon's mark rides the frame and the inbox keeps it through a fold.
func TestAFoldedPreviewKeepsTheDaemonsLostMark(t *testing.T) {
	in := newInbox()
	in.add(tokenFrame("s1", "before "))
	marked := tokenFrame("s1", "after ")
	marked.Lost = true
	in.add(marked)
	got := in.take(takeLimit).frames
	if len(got) != 1 || !got[0].Lost || got[0].Event.Text != "before after " {
		t.Errorf("taken %+v, want one folded frame carrying the mark", got)
	}
}

// What the daemon says, the pane believes: tokens were dropped before this one.
func TestAFrameMarkedLostMakesItsPaneRawUntilTheNextBlock(t *testing.T) {
	renders := chunkCounter(t)
	a := fmtApp(t).applyFrame(startFrame("s1")).streams("s1", "one.\n\ntwo")
	if *renders == 0 {
		t.Fatal("a pane that saw the block begin formatted nothing: the control for the rest")
	}
	f := tokenFrame("s1", "```go\nopener := true\n\n")
	f.Lost = true
	a = a.applied([]rpc.Frame{f})
	*renders = 0
	if got := lostTail(a, renders); *renders != 0 {
		t.Errorf("formatted after the daemon said tokens were lost:\n%s", got)
	}
	if out := shown(a); !strings.Contains(out, "one.") {
		t.Errorf("the finished block already drawn is gone:\n%s", out)
	}
}
