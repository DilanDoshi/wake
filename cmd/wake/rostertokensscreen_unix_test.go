//go:build unix

package main

// The roster's token count on a real screen.
//
// The in-process guards hold the budget and the width arithmetic. This holds
// the thing they cannot: that the figure is on the sidebar an operator looks
// at, beside the agent it belongs to, only while that agent is working - and
// that adding it did not push the sidebar into the pane next to it.
//
// It needs a session that has **finished one turn and started another**, which
// is a shape only the interruptible script has: it completes a turn on startup
// (so Agent.Tokens is non-zero) and then opens one that never closes. A session
// mid-first-turn has no count at all, because the only trustworthy figure is on
// the result frame that ends a turn.

import (
	"strings"
	"testing"
)

func TestTheSidebarShowsATokenCountWhileAnAgentIsWorking(t *testing.T) {
	withScriptedAgent(t, scriptInterruptible)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready") // the startup turn, which completes and carries usage
	name := s.agentName()
	s.settle()

	// Idle: the row is bare, which is the half of the ruling that is a promise
	// not to draw something.
	if row := rowFor(s, name); strings.Contains(row, "↓") {
		t.Fatalf("an idle row carries a token count: %q\n%s", row, s.dump())
	}

	// A turn that opens and does not close.
	s.send("take your time\r")
	s.await(workingMarker)
	s.settle()

	row := rowFor(s, name)
	if !strings.Contains(row, "↓") {
		t.Fatalf("a working row carries no token count: %q\n%s", row, s.dump())
	}
	// **The turn's figure, not the session total.** The startup turn reported
	// fakeOutputTokens (1234, which abbreviates to 1.2k) and this turn's two
	// message_delta frames sum to 550 - so the two numbers are distinguishable
	// on screen, and a surface reading the wrong field fails here rather than
	// looking plausible.
	if !strings.Contains(row, "550") {
		t.Fatalf("the row is %q, want 550 - what *this* turn has produced\n%s", row, s.dump())
	}
	if strings.Contains(row, "1.2k") {
		t.Fatalf("the row is %q, which is the session total rather than the turn's\n%s", row, s.dump())
	}

	// And the frame still fits. A roster row one column too wide does not
	// overflow itself - lipgloss joins columns on their widest line, so it
	// shoves the room sideways for as long as that row is on screen.
	for i, line := range s.lines() {
		if len([]rune(strings.TrimRight(line, " "))) > 100 {
			t.Fatalf("row %d is wider than the terminal:\n%s", i, s.dump())
		}
	}
}

// rowFor is the sidebar line naming one agent, or "" when no line does.
func rowFor(s *screen, name string) string {
	for _, line := range s.lines() {
		if strings.Contains(line, name) && strings.Contains(line, "│") {
			return line
		}
	}
	return ""
}
