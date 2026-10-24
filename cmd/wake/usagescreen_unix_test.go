//go:build unix

package main

import (
	"strings"
	"testing"
)

// On a real screen: no bar until an agent's API response has carried the
// session window, then the sidebar's last rows draw it under the roster.
func TestTheSessionUsageBarDrawsUnderTheRosterOnceAReadingArrives(t *testing.T) {
	withScriptedAgent(t, scriptUsageReadings)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 160, 40)
	s.await("ready")
	if y := s.rowOf("session · ↻"); y >= 0 {
		t.Fatalf("the bar drew on row %d before any reading:\n%s", y, s.dump())
	}
	s.send("ship it\r")
	s.await("session · ↻")

	lines := s.lines()
	caption := s.rowOf("session · ↻")
	if want := len(lines) - 3; caption != want {
		t.Errorf("the caption is on row %d, want %d - the pane's last, over the strip and the notice row:\n%s", caption, want, s.dump())
	}
	if bar := lines[caption-1]; !strings.HasSuffix(bar, " 47%") || !strings.Contains(bar, "█") {
		t.Errorf("the row over the caption is %q, want the bar and 47%%", bar)
	}
}
