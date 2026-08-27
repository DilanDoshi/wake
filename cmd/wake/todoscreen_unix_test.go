//go:build unix

// A task list on a real screen, through the real binary.
//
// The tool is never called anywhere in testdata/stream, so nothing else in the
// tree exercises this path end to end: the airlock's tests drive hand-written
// frames and the renderer's drive plain values. This is the only place a
// TodoWrite goes in one end as JSON on a pipe and comes out the other as
// characters in a terminal emulator - which is where the last defect in this
// area hid, invisible to 2,300 unit tests.

package main

import (
	"strings"
	"testing"
)

// The whole block, on screen, in the agent's own order.
func TestATaskListReachesTheScreen(t *testing.T) {
	withScriptedAgent(t, scriptPlans)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 30)
	s.await("ready")

	s.send("plan it\r")
	s.await(heardPrefix + "planned")

	// Every status draws its own glyph beside its own text.
	for _, want := range []string{
		"■ encode the receipt",
		"☐ refuse the mode verb",
		"☑ wire the daemon",
	} {
		s.await(want)
	}
}

// The list is drawn under the call that carried it rather than floating loose
// in the transcript.
func TestATaskListSitsUnderItsToolCall(t *testing.T) {
	withScriptedAgent(t, scriptPlans)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeInAConversation(t, 120, 30)
	s.await("ready")
	s.send("plan it\r")
	s.await("■ encode the receipt")

	screen := s.text()
	call, list := strings.Index(screen, "TodoWrite"), strings.Index(screen, "■ encode the receipt")
	if call < 0 {
		t.Fatalf("the tool call never reached the screen:\n%s", screen)
	}
	if call > list {
		t.Errorf("the list is drawn above its own call:\n%s", screen)
	}
}

// The pane must still measure exactly what it was given. A block that adds
// rows without them being budgeted scrolls the alt screen on every draw, which
// is how the heartbeat's row broke this pane once already.
func TestATaskListDoesNotOverflowThePane(t *testing.T) {
	withScriptedAgent(t, scriptPlans)
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	const rows = 30
	s := startWakeInAConversation(t, 120, rows)
	s.await("ready")
	s.send("plan it\r")
	s.await("■ encode the receipt")

	// The composer is the last thing on screen, so its presence after the list
	// arrived is the evidence nothing pushed it off.
	screen := s.text()
	if !strings.Contains(screen, "↵ send") {
		t.Errorf("the composer's hint line is gone, so the frame is taller than the pane:\n%s", screen)
	}
	if got := len(strings.Split(strings.TrimRight(screen, "\n"), "\n")); got > rows {
		t.Errorf("the screen holds %d rows, want at most %d:\n%s", got, rows, screen)
	}
}
