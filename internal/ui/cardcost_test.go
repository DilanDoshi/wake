package ui

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A steady frame with an ask up re-wraps nothing.
//
// This is idle_bench_test.go's own stop condition, asked of the one state it
// never covered: a pinned card changes a pane's chrome, and Room.View re-sizes
// when the chrome moves - so a card whose height was not stable would put the
// transcript back through glamour on every single draw. That is the difference
// between an expensive frame and an unbounded one.
func TestASteadyFrameWithAnAskReWrapsNothing(t *testing.T) {
	// The conversation is opened first, because a card is only ever drawn in
	// its own agent's pane now - the room puts none, so a bench App left on the
	// room would pin nothing and this would measure a frame with no card in it.
	a := wrapped(t, 200, 40, 200).(App).openDMWith("s1", "sydney")
	ask := recordedAsks(t, choiceFixture)[0]
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ask}).applyGeometry()
	if _, ok := a.cardOf(a.focus); !ok {
		t.Fatal("the bench App is putting no ask, so this test is about nothing")
	}
	// The first draw is allowed its re-size: the card arrived after the last
	// geometry pass, so the chrome moved once. What may not happen is the
	// second draw doing it again.
	_ = a.View()
	if n := countRoomRenders(t, func() { _ = a.View() }); n != 0 {
		t.Errorf("a settled frame with a card up re-wrapped the room %d times: the card's height is not stable, so every draw goes back through glamour", n)
	}
}
