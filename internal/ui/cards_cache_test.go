package ui

// The pinned plan card is markdown through glamour, and glamour runs behind
// internal/render's one process-global mutex. Bubble Tea redraws on a cursor
// blink and on every mouse-motion message during a divider drag, so a plan card
// re-rendered per View is work per frame that is really work per change - the
// first non-negotiable. These pin it to the memo: the body goes back through the
// planBody seam when an input it depends on moves, and is served from the cache
// otherwise. The seam is the one countable entry, mirroring drawStatusBar.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// countPlanBodies replaces the plan-body seam with a counter and restores it, so
// a test asserts how many times a plan card actually went through glamour rather
// than timing it. Mirrors countBars. t.Cleanup restores the package var so the
// -race and non-race passes, which share the process, cannot cross-contaminate.
func countPlanBodies(t *testing.T) *int {
	t.Helper()
	n, prev := 0, planBody
	planBody = func(plan string, width int) string {
		n++
		return prev(plan, width)
	}
	t.Cleanup(func() { planBody = prev })
	return &n
}

// plannedApp is one agent's conversation open at this width with a recorded plan
// ask pinned in it - asking()'s shape, one shape over. The cache is cold: it
// warms only on a View or a card-key press, and neither has happened yet.
func plannedApp(t *testing.T, width int) App {
	t.Helper()
	ev := planAsk(t)
	a := newRoomApp(t).withSize(width, 40).withAgents("john")
	a = pick(a, "s1").openDMWith("s1", "john").applyGeometry()
	return a.applyFrame(rpc.Frame{Kind: rpc.FrameEvent, SessionID: "s1", Event: &ev})
}

// The gate. A pinned plan card is drawn on every frame, but its markdown moves
// only when the ask arrives - so it must go through glamour once and be served
// from the memo on every steady frame after. RED on the un-memoized tree fires
// once per frame; GREEN fires exactly once.
func TestAPinnedPlanCardRendersOnChangeNotPerFrame(t *testing.T) {
	a := plannedApp(t, narrowColumns)

	n := countPlanBodies(t)
	const frames = 8
	var first string
	for i := range frames {
		out := a.View()
		if i == 0 {
			first = stripANSI(out)
		}
	}
	if !strings.Contains(first, cardHasPlan) {
		t.Fatalf("the plan card is not on screen, so this test asserts nothing:\n%s", first)
	}
	if *n != 1 {
		t.Errorf("a pinned plan card went through glamour %d times over %d steady frames, want 1: "+
			"Bubble Tea redraws on a blink and on every mouse motion during a drag, and re-rendering a "+
			"plan on frames where nothing about it moved is the work-per-frame the first non-negotiable forbids", *n, frames)
	}
}

// The key is derived from width, so a re-wrap misses the memo and a redraw at
// the same width hits it. A plan wrapped for a width it is not in is the stale
// body the memo must never serve past.
func TestAPlanCardReRendersWhenItsWidthChanges(t *testing.T) {
	cs := Cards{}.Add("s1", planAsk(t))
	card, ok := cs.Top()
	if !ok || card.Shape() != ShapePlan {
		t.Fatalf("no plan card to draw: ok=%v shape=%d", ok, card.Shape())
	}
	by := Agent{Name: "sydney"}

	n := countPlanBodies(t)
	if cs.View(card, wideRoom, by, false); *n != 1 {
		t.Fatalf("the first draw rendered %d times, want 1", *n)
	}
	if cs.View(card, wideRoom, by, false); *n != 1 {
		t.Errorf("a redraw at the same width rendered again (%d), want the memo to serve it", *n)
	}
	if cs.View(card, narrowRoom, by, false); *n != 2 {
		t.Errorf("a width change did not re-render the plan (%d), want 2: a plan wrapped for the old width is the stale body the memo must not serve", *n)
	}
	if cs.View(card, narrowRoom, by, false); *n != 2 {
		t.Errorf("the new width was not itself cached (%d), want 2", *n)
	}
}

// The memo is keyed per card identity, so the two panes that can draw two
// different plan cards in one frame do not evict each other. A single App-level
// slot would thrash - each draw a miss - and could serve one card the other's
// body, which is the stale-content failure the key's agentID+requestID guards.
func TestTwoPlanCardsDoNotEvictEachOther(t *testing.T) {
	ev1 := planAsk(t)
	ev1.RequestID = "r1"
	ev2 := core.Event{
		Kind: core.KindPermissionRequest, RequestID: "r2", Ask: core.AskApproval,
		Tool: &core.ToolCall{Name: ev1.Tool.Name, Ask: &core.AskDetail{
			Plan: "# The second plan\n\nRewrite the scheduler entirely and delete the queue.",
		}},
	}
	cs := Cards{}.Add("a1", ev1).Add("a2", ev2)
	c1, ok1 := cs.byRequest("a1", "r1")
	c2, ok2 := cs.byRequest("a2", "r2")
	if !ok1 || !ok2 || c1.Shape() != ShapePlan || c2.Shape() != ShapePlan {
		t.Fatalf("both plan cards should be open plan shapes: ok=%v/%v shape=%d/%d", ok1, ok2, c1.Shape(), c2.Shape())
	}
	by := Agent{Name: "x"}

	n := countPlanBodies(t)
	for range 3 {
		_ = cs.View(c1, wideRoom, by, false)
		_ = cs.View(c2, wideRoom, by, false)
	}
	if *n != 2 {
		t.Errorf("two plan cards drawn alternately went through glamour %d times, want 2: a single slot would evict one with the other and re-render both every frame", *n)
	}

	// Each card serves its own body, never the other's - the correctness half of
	// per-identity keying, which the count alone does not prove.
	out1 := stripANSI(cs.View(c1, wideRoom, by, false))
	out2 := stripANSI(cs.View(c2, wideRoom, by, false))
	if !containsAnyWord(out1, c1.Detail.Plan) || containsAnyWord(out1, "scheduler") {
		t.Errorf("card 1 is not showing its own plan:\n%s", out1)
	}
	if !strings.Contains(out2, "scheduler") {
		t.Errorf("card 2 is not showing its own plan:\n%s", out2)
	}
}

// Reconcile prunes the memo to the still-open set, so an ended agent's cached
// body does not live for the life of the process - while a live card's entry
// survives the report, so a fleet push does not cost it a re-render.
func TestReconcilePrunesEndedPlanMemosAndKeepsLiveOnes(t *testing.T) {
	ev1 := planAsk(t)
	ev1.RequestID = "r1"
	ev2 := planAsk(t)
	ev2.RequestID = "r2"
	cs := Cards{}.Add("a1", ev1).Add("a2", ev2)
	c1, _ := cs.byRequest("a1", "r1")
	c2, _ := cs.byRequest("a2", "r2")
	by := Agent{Name: "x"}

	n := countPlanBodies(t)
	_ = cs.View(c1, wideRoom, by, false)
	_ = cs.View(c2, wideRoom, by, false)
	if *n != 2 {
		t.Fatalf("warming two plan cards rendered %d times, want 2", *n)
	}

	// A report names only a1's ask: a2 has ended.
	cs = cs.Reconcile(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "a1", RequestIDs: []string{"r1"}},
	}})
	if _, ok := cs.plans[[2]string{"a2", "r2"}]; ok {
		t.Error("Reconcile left the ended card's memo entry behind, so the cache leaks for the life of the process")
	}
	// The survivor's body is still cached, so drawing it after the report is free.
	c1, ok := cs.byRequest("a1", "r1")
	if !ok {
		t.Fatal("the live card was dropped by Reconcile")
	}
	if _ = cs.View(c1, wideRoom, by, false); *n != 2 {
		t.Errorf("a fleet report cost the surviving plan card a re-render (%d), want 2: the prune dropped a live entry", *n)
	}
}

// Settle prunes the settled card's memo without waiting for a report. A fleet
// report almost always follows a settle - the agent unblocks and its state
// changes - but nothing guarantees one lands before the next settle, and a plan
// string plus its rendered body is not small to retain in a long-lived process.
func TestSettleDropsTheSettledPlanMemoWithoutWaitingForAReport(t *testing.T) {
	ev1 := planAsk(t)
	ev1.RequestID = "r1"
	ev2 := planAsk(t)
	ev2.RequestID = "r2"
	cs := Cards{}.Add("a1", ev1).Add("a2", ev2)
	c1, _ := cs.byRequest("a1", "r1")
	c2, _ := cs.byRequest("a2", "r2")
	by := Agent{Name: "x"}

	n := countPlanBodies(t)
	_ = cs.View(c1, wideRoom, by, false)
	_ = cs.View(c2, wideRoom, by, false)
	if *n != 2 {
		t.Fatalf("warming two plan cards rendered %d times, want 2", *n)
	}

	// Settle a2's ask, with no Reconcile following it.
	cs = cs.Settle("a2", "r2")
	if _, ok := cs.plans[[2]string{"a2", "r2"}]; ok {
		t.Error("Settle kept the settled card's memo, so a retired plan's body lingers until some later report happens to prune it")
	}
	// a1's entry survived, so drawing it is still free.
	if _ = cs.View(c1, wideRoom, by, false); *n != 2 {
		t.Errorf("Settle cost the surviving plan card a re-render (%d), want 2: the prune dropped a live entry", *n)
	}
}
