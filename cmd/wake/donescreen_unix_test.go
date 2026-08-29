//go:build unix

package main

import (
	"strings"
	"testing"
)

// After an agent finishes a turn in a DM, the line above the composer stops
// being the working spinner and becomes the done summary - `✻ Cooked for 0s ·
// done 3:04 PM` - which stands until the next turn. Driven through a real turn
// (send a prompt, the echo agent replies and goes idle) rather than asserted off
// a constructed state.
func TestADMShowsADoneLineAfterATurn(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	s.send("transform this\r")
	s.await(heardPrefix + "transform this") // the reply, proof the turn ran
	s.await("· done ")                      // the done clause, only the done line carries it
	s.settle()

	line := ""
	for _, r := range s.lines() {
		if strings.Contains(r, "· done ") {
			line = r
			break
		}
	}
	if line == "" {
		t.Fatalf("no done line on screen after the turn.\n%s", s.dump())
	}
	if !strings.Contains(line, " for ") {
		t.Errorf("done line %q has no elapsed clause", line)
	}
	// It is the done line, not the working spinner: the spinner trails its word
	// with the ellipsis and carries no "done" clause.
	if strings.Contains(line, "…") {
		t.Errorf("the line above the composer is still the working spinner: %q", line)
	}
}
