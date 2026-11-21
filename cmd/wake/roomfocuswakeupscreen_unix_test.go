//go:build unix

// A room narrowed to one agent, on a real screen, while the daemon resumes two
// agents a usage limit stopped: it must not draw the other agent's resume line.

package main

import (
	"strings"
	"testing"
)

func TestANarrowedRoomHidesAnotherAgentsResumeOnScreen(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	signInAs(t, dir, limitedAccount)
	s, names := scriptedGridScreen(t, scriptUsageLimits, 200, 40)
	first, second := names[0]+" resumed after the usage limit", names[1]+" resumed after the usage limit"

	// A turn to each agent hits the limit, so the daemon holds both stopped: the
	// pin names one and counts the other.
	s.send("@" + names[0] + " alphaword\r")
	s.await("alphaword")
	s.send("@" + names[1] + " betaword\r")
	s.await("betaword")
	s.awaitFrame("both agents pinned as limited", func(rows []string) bool {
		pin := rowIn(rows, "Wake resumes it")
		return pin >= 0 && strings.Contains(rows[pin], "+1 more")
	})
	s.settle()

	// Narrow to the second agent, then sign in elsewhere: both are resumed.
	s.send("@" + names[1] + " ")
	s.await("› @" + names[1])
	s.settle()
	signInAs(t, dir, "acct-b")
	s.await(second)
	// The pin clears only once each agent's turn has gone through, and each resume
	// is announced before it is written: so both lines have landed.
	s.awaitGone("Wake resumes it")
	s.settle()

	if strings.Contains(s.text(), first) {
		t.Fatalf("the room narrowed to %s draws %s's resume line:\n%s", names[1], names[0], s.dump())
	}
	t.Logf("SCREENSHOT room narrowed to @%s while both agents were resumed:\n%s", names[1], s.dump())

	// Widening brings the other agent's line back: it was hidden, not missing.
	s.send("\x1b") // esc clears the room draft
	s.await(first)
}
