//go:build unix

package main

// The sidebar's search bar on a real screen: a click on its rows takes the keys, typing
// narrows the sidebar in place and counts what matched, a click in the room lets go and
// leaves the filter standing, and esc clears it and hands the keys back. internal/ui
// holds the logic; this is the half a person sees and the real mouse reports.

import (
	"regexp"
	"strings"
	"testing"
)

// matchedOf is the bar's count, N of the five sessions the fleet holds (the manager and w1..w3
// and the conversation's own agent).
var matchedOf = regexp.MustCompile(`\d/5`)

func TestTheSearchBarFiltersTheSidebarOnARealScreen(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, boxCols, 30)
	teamedFleet(t, s)
	s.settle()

	if !strings.Contains(strings.Join(sidebarRows(s.lines()), "\n"), "search agents") {
		t.Fatalf("the sidebar draws no search bar:\n%s", s.dump())
	}
	s.click(boxDivider+8, 1) // the bar's middle row
	s.settle()
	s.send("w3")
	s.settle()
	rows := strings.Join(sidebarRows(s.lines()), "\n")
	// The manager's random name may or may not hold a w and a 3 in order; w1 and w2 never match.
	if !strings.Contains(rows, "w3") || strings.Contains(rows, "w1") || strings.Contains(rows, "w2") || !matchedOf.MatchString(rows) {
		t.Fatalf("typing w3 left the sidebar as:\n%s", s.dump())
	}
	if !strings.Contains(strings.Join(sidebarRows(s.lines()), "\n"), "frontend") || strings.Contains(strings.Join(sidebarRows(s.lines()), "\n"), "backend") {
		t.Errorf("the team with no match is still drawn, or the one with it is not:\n%s", s.dump())
	}
	t.Logf("SCREENSHOT the search bar filtering the sidebar:\n%s", s.dump())

	s.send("\r") // opens the match, as a click on its row does
	s.await("@w3")

	s.click(boxDivider+8, 1)
	s.settle()
	s.send("\x1b") // esc: clear the filter and give the keys back
	s.settle()
	rows = strings.Join(sidebarRows(s.lines()), "\n")
	if !strings.Contains(rows, "w1") || !strings.Contains(rows, "search agents") {
		t.Errorf("esc did not clear the filter:\n%s", s.dump())
	}
}
