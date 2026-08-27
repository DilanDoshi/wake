package ui

// What the turn in flight has produced, folded onto the agent.
//
// Agent.Tokens is a session total that steps at turn end, and it is what every
// surface used to draw: during turn N it reported turns 1…N−1. Agent.TurnTokens
// is this turn's own, accumulated from the message_delta frames as the turn
// runs and cleared when it ends.

import (
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// progress is one message of a turn closing, having produced n output tokens.
func progress(id string, n int) core.Event {
	return core.Event{Kind: core.KindTurnTokens, SessionID: id, Session: &core.SessionFacts{TurnOutputTokens: n}}
}

// began is one message of a turn starting.
func began(id string) core.Event {
	return core.Event{Kind: core.KindMessageStart, SessionID: id}
}

// The figure on a delta is **cumulative for its message**, and a message emits
// one or more of them. So a turn's count replaces within a message and sums
// across them - adding the deltas up reported 250 for a message that produced
// 150, which is the failure an adversarial review caught against the streaming
// docs' own words.
func TestATurnsTokensAreCumulativeWithinAMessageAndSummedAcrossThem(t *testing.T) {
	f := NewFleet()
	// One message climbing to 150, in three cumulative reports.
	f, _ = f.Observe(began("s1"), "s1")
	for _, n := range []int{50, 100, 150} {
		f, _ = f.Observe(progress("s1", n), "s1")
	}
	if a, _ := f.Agent("s1"); a.TurnTokens != 150 {
		t.Fatalf("one message reporting 50, 100, 150 holds %d, want 150 - they are the same tokens counted again", a.TurnTokens)
	}

	// A second message of the same turn, climbing to 400.
	f, _ = f.Observe(began("s1"), "s1")
	for _, n := range []int{200, 400} {
		f, _ = f.Observe(progress("s1", n), "s1")
	}
	if a, _ := f.Agent("s1"); a.TurnTokens != 550 {
		t.Errorf("two messages of 150 and 400 hold %d, want 550", a.TurnTokens)
	}
}

// A turn is several messages and each states its own, so the turn's figure is
// their sum rather than the last of them.
func TestATurnsTokensAccumulateAcrossItsMessages(t *testing.T) {
	f := NewFleet()
	for _, n := range []int{120, 340, 90} {
		f, _ = f.Observe(began("s1"), "s1")
		f, _ = f.Observe(progress("s1", n), "s1")
	}

	a, _ := f.Agent("s1")
	if a.TurnTokens != 550 {
		t.Errorf("the turn holds %d tokens, want 120+340+90=550", a.TurnTokens)
	}
	// The session total is the result frame's business and must not have moved:
	// adding both counts every token twice.
	if a.Tokens != 0 {
		t.Errorf("the session total moved to %d on a progress frame, want it untouched", a.Tokens)
	}
}

// The count belongs to one turn, so the next turn starts from nothing. Without
// this it is a session total by another name, arriving more often.
func TestATurnsTokensAreClearedWhenItEnds(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(progress("s1", 550), "s1")
	f, _ = f.Observe(core.Event{
		Kind:      core.KindTurnEnd,
		SessionID: "s1",
		Session:   &core.SessionFacts{OutputTokens: 600},
	}, "s1")

	a, _ := f.Agent("s1")
	if a.TurnTokens != 0 {
		t.Errorf("the turn's count is %d after it ended, want it cleared", a.TurnTokens)
	}
	// And the completed turn lands on the session total, which is the figure
	// the result frame is the authority for.
	if a.Tokens != 600 {
		t.Errorf("the session total is %d, want the 600 the result reported", a.Tokens)
	}

	f, _ = f.Observe(progress("s1", 25), "s1")
	if a, _ := f.Agent("s1"); a.TurnTokens != 25 {
		t.Errorf("the next turn starts at %d, want 25", a.TurnTokens)
	}
}

// The two figures answer two questions and are never added. A turn that
// produced 550 on a session that has already spent 4000 is both numbers, not
// 4550 and not 550 replacing 4000.
func TestTheTurnsCountAndTheSessionTotalAreKeptApart(t *testing.T) {
	f := NewFleet()
	// Two completed turns, on the frame that is the authority for a total.
	for _, n := range []int{2_500, 1_500} {
		f, _ = f.Observe(core.Event{
			Kind: core.KindTurnEnd, SessionID: "s1",
			Session: &core.SessionFacts{OutputTokens: n},
		}, "s1")
	}
	// And a third turn under way.
	f, _ = f.Observe(progress("s1", 550), "s1")

	a, _ := f.Agent("s1")
	if a.Tokens != 4_000 {
		t.Errorf("the session total is %d, want 2500+1500=4000", a.Tokens)
	}
	if a.TurnTokens != 550 {
		t.Errorf("the turn holds %d, want 550 - the turn in flight and nothing before it", a.TurnTokens)
	}
}

// A turn can end without a result frame, and the count must not survive it.
//
// KindTurnEnd covers the ordinary ends and the interrupted one - every `result`
// frame decodes to it, whatever its subtype. What it does not cover is a turn
// with no result at all: ⌃C closes stdin on a working agent, and the next thing
// that session does is a *new* turn under the same Agent record. Clearing only
// at the end would carry the parked turn's figure into it, so the count starts
// high for the rest of that session's life.
func TestATurnsTokensDoNotSurviveAParkAndAWake(t *testing.T) {
	f := NewFleet()
	f = f.WithStatus(statusOf(rpc.StateWorking))
	f, _ = f.Observe(progress("s1", 550), "s1")

	// ⌃C mid-turn: no result frame, so nothing clears it on the way out.
	f = f.WithStatus(statusOf(rpc.StateParked))
	f = f.WithStatus(statusOf(rpc.StateWorking))
	f, _ = f.Observe(progress("s1", 25), "s1")

	if a, _ := f.Agent("s1"); a.TurnTokens != 25 {
		t.Errorf("the woken turn holds %d tokens, want 25 - the parked turn's 550 came back with it", a.TurnTokens)
	}
}

// A turn does not stop being one because the agent is waiting on you.
//
// The daemon's own stateLocked is the authority: `blocked` is a permission ask
// outstanding *with the turn still owed*, and `silent` is owed-and-quiet. Both
// are the same turn, so coming back to working from either is a turn resuming
// rather than starting - and a reset there leaves the row showing the suffix of
// a turn while the result frame states the whole of it.
//
// Found by an adversarial review, against the fix for the park case.
func TestATurnResumingIsNotATurnStarting(t *testing.T) {
	for _, mid := range []string{rpc.StateBlocked, rpc.StateSilent} {
		f := NewFleet()
		f = f.WithStatus(statusOf(rpc.StateWorking))
		f, _ = f.Observe(progress("s1", 550), "s1")
		started, _ := f.Agent("s1")

		f = f.WithStatus(statusOf(mid))
		f = f.WithStatus(statusOf(rpc.StateWorking))
		// A new message of the same turn: 25 is its own cumulative, not a
		// continuation of the 550 before it.
		f, _ = f.Observe(began("s1"), "s1")
		f, _ = f.Observe(progress("s1", 25), "s1")

		a, _ := f.Agent("s1")
		if a.TurnTokens != 575 {
			t.Errorf("through %s the turn holds %d tokens, want 550+25=575: the turn never ended", mid, a.TurnTokens)
		}
		// The clock is the same fact and moves with it. An age that restarted
		// every time somebody answered a permission would say a turn had just
		// begun in the middle of the one they were answering *for*.
		if !a.startedAt.Equal(started.startedAt) {
			t.Errorf("through %s the turn's clock restarted, want the turn it is measuring to keep its start", mid)
		}
	}
}

// And the states that really do delimit a turn still clear it. Without this the
// test above is satisfied by never resetting at all.
func TestEveryStateThatEndsATurnStartsTheNextOneFromNothing(t *testing.T) {
	for _, ended := range []string{rpc.StateIdle, rpc.StateParked, rpc.StateEnded} {
		f := NewFleet()
		f = f.WithStatus(statusOf(rpc.StateWorking))
		f, _ = f.Observe(progress("s1", 550), "s1")

		f = f.WithStatus(statusOf(ended))
		f = f.WithStatus(statusOf(rpc.StateWorking))
		f, _ = f.Observe(progress("s1", 25), "s1")

		if a, _ := f.Agent("s1"); a.TurnTokens != 25 {
			t.Errorf("after %s the next turn holds %d tokens, want 25", ended, a.TurnTokens)
		}
	}
}

// /clear takes the conversation the figures describe, which is the argument
// fold already makes about the session total and the context level.
func TestATurnsTokensGoWhenTheConversationDoes(t *testing.T) {
	f := NewFleet()
	f = f.WithStatus(statusOf(rpc.StateWorking))
	f, _ = f.Observe(progress("s1", 550), "s1")
	f, _ = f.Observe(core.Event{Kind: core.KindSessionReset, SessionID: "s1"}, "s1")

	if a, _ := f.Agent("s1"); a.TurnTokens != 0 {
		t.Errorf("the turn holds %d tokens after a /clear, want none: that turn is in a conversation the model no longer has", a.TurnTokens)
	}
}

// Every state the roster draws is either one a turn is in flight during or one
// that ends a turn, and which it is has to be decided here rather than defaulted.
//
// Derived from stateGlyph, which is the roster's own domain, so a seventh state
// fails this rather than quietly reading as a boundary - and a boundary is what
// throws a turn's clock and its tokens away.
func TestEveryStateTheRosterDrawsIsInFlightOrIsNot(t *testing.T) {
	inFlight := map[string]bool{
		rpc.StateWorking: true,
		rpc.StateBlocked: true,
		rpc.StateSilent:  true,
		// The boundaries. idle is the daemon's `!owed`; the other three hold no
		// process that owes a turn at all.
		rpc.StateIdle:     false,
		rpc.StateParked:   false,
		rpc.StateEnded:    false,
		rpc.StateOrphaned: false,
	}
	for state := range stateGlyph {
		want, ruled := inFlight[state]
		if !ruled {
			t.Errorf("the roster draws %q and nothing here says whether a turn is in flight during it: defaulting reads it as a boundary, which throws away the turn's clock and its tokens", state)
			continue
		}
		if got := turnInFlight(state); got != want {
			t.Errorf("turnInFlight(%q) = %v, want %v", state, got, want)
		}
	}
	for state := range inFlight {
		if _, drawn := stateGlyph[state]; !drawn {
			t.Errorf("%q is ruled on here and the roster no longer draws it: the ruling is about a state that cannot arrive", state)
		}
	}
}

// A gap in the stream takes the turn's figures with it.
//
// turnInFlight only resets on a boundary this client *saw*. Two things make a
// boundary missable: the inbox drops the oldest frame when a slow window fills
// it, and a reattach comes back onto a daemon that keeps no replay buffer. Miss
// the result and the idle report, and the next working turn's deltas land on top
// of the last one's - and because a reset needs a boundary, the count stays
// wrong for the whole of that turn.
//
// This is forgotModes' ruling applied to the same class of belief: what survived
// the gap is what nothing can confirm. Found by an adversarial review.
func TestAGapInTheStreamForgetsTheTurnItCouldNotFollow(t *testing.T) {
	for _, tc := range []struct {
		what   string
		gap    func(App) App
		frames int
	}{
		{"a reattach", func(a App) App {
			m, _ := a.reattached(reattachedMsg{})
			return m.(App)
		}, 0},
		{"a window too slow to keep up", func(a App) App {
			m, _ := a.stream(streamMsg{batch: batch{dropped: 3}})
			return m.(App)
		}, 3},
	} {
		fresh(t)
		a := newRoomApp(t).withSize(200, 40)
		a.fleet = a.fleet.WithStatus(statusOf(rpc.StateWorking))
		a.fleet, _ = a.fleet.Observe(progress("s1", 550), "s1")

		a = tc.gap(a)

		agent, _ := a.fleet.Agent("s1")
		if agent.TurnTokens != 0 {
			t.Errorf("after %s the turn still holds %d tokens: the boundary that would have cleared them may have been in the gap", tc.what, agent.TurnTokens)
		}
		if !agent.startedAt.IsZero() {
			t.Errorf("after %s the turn keeps a start time this client can no longer vouch for", tc.what)
		}
	}
}

// statusOf is one session in one state, as the daemon reports it.
func statusOf(state string) *rpc.Status {
	return &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{{ID: "s1", Name: "sydney", State: state}}}
}

// A progress frame is a count and nothing else: it must not put a line in the
// room, which is a surface where thirty agents' every message would be noise.
func TestAProgressFrameDrawsNothingInTheRoom(t *testing.T) {
	f := NewFleet()
	_, forRoom := f.Observe(progress("s1", 550), "s1")

	if len(forRoom) != 0 {
		t.Errorf("a progress frame produced %d room events: %+v", len(forRoom), forRoom)
	}
}

// And it is not something you have not read. Thirty working agents would
// otherwise drive every unread badge in the sidebar on their own.
func TestAProgressFrameIsNotUnread(t *testing.T) {
	f := NewFleet()
	f, _ = f.Observe(progress("s1", 550), "s1")

	if a, _ := f.Agent("s1"); a.Unread != 0 {
		t.Errorf("a progress frame left %d unread", a.Unread)
	}
}
