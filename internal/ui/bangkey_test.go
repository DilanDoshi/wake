package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// bang_test.go reaches everything else through App.bang directly, so the Enter
// key itself is the one thing it cannot assert - and the interception living in
// submit is exactly what "typing !ls sends it to the model" would look like
// again if someone removed it.

func TestABangIsNotSentToTheAgentOnEnter(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = typeText(m, "!echo not for the agent")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on a bang produced no command: nothing ran it")
	}
	m, _ = m.Update(cmd())

	select {
	case f := <-sent:
		if f.Kind == rpc.FrameSend {
			t.Errorf("a bang was sent to the agent as text: %q. That is what happens without an interception, and the model answers as if it had been asked about the command", f.Text)
		}
	case <-time.After(250 * time.Millisecond):
	}
	if !strings.Contains(shown(m), "not for the agent") {
		t.Errorf("the output reached neither the agent nor the conversation:\n%s", shown(m))
	}
}

func TestAnOrdinaryMessageStillReachesTheAgentOnEnter(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = typeText(m, "tell me about ! marks")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		cmd()
	}
	if f := awaitFrame(t, sent); f.Kind != rpc.FrameSend || f.Text != "tell me about ! marks" {
		t.Errorf("the daemon received %v %q, want the message", f.Kind, f.Text)
	}
}
