//go:build unix

package main

import (
	"strings"
	"testing"
)

// A self-paced /loop between iterations reads "still looping", not "done": the
// line above the composer becomes `✻ Looping · iter 1 done · next …` once the
// iteration ends and the agent goes idle waiting for its next wakeup. Driven
// through a real turn - the fake agent emits a ScheduleWakeup and goes idle -
// rather than asserted off a constructed state, so it proves the line renders in
// a real pane within its bounds.
func TestADMShowsALoopWaitLineBetweenIterations(t *testing.T) {
	withScriptedAgent(t, scriptLoops)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 100, 30)
	s.await("ready")
	s.settle()

	s.send("start looping\r")
	s.await(heardPrefix + "looping") // the reply, proof the iteration ran
	s.await("Looping")               // the waiting line's head
	s.settle()

	line := ""
	for _, r := range s.lines() {
		if strings.Contains(r, "Looping") {
			line = r
			break
		}
	}
	if line == "" {
		t.Fatalf("no loop-wait line on screen after the iteration.\n%s", s.dump())
	}
	if !strings.Contains(line, "iter 1 done") {
		t.Errorf("loop-wait line %q is missing the iteration count", line)
	}
	if !strings.Contains(line, "next ") {
		t.Errorf("loop-wait line %q is missing the next-fire clause", line)
	}
	// It is the waiting line, not the working spinner: the spinner trails its word
	// with the ellipsis and carries no "Looping" head.
	if strings.Contains(line, "…") {
		t.Errorf("the line above the composer is still the working spinner: %q", line)
	}
}
