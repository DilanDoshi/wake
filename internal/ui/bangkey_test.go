package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// bang_test.go reaches everything else through App.bang directly, so the Enter
// key itself is the one thing it cannot assert - and the interception living in
// submit is exactly what "typing !ls sends it to the model" would look like
// again if someone removed it.

// Dilan's 2026-10-25 ruling: the bang reaches the agent, as a shell run - never
// as the text of a message, which is what Enter sent before the interception.
func TestABangReachesTheAgentAsAShellRunOnEnter(t *testing.T) {
	fresh(t)
	conn, sent := pipeClient(t)
	var m tea.Model = dmApp(conn, Stream{}, "s1", "alex")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = typeText(m, "!echo for the agent")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter on a bang produced no command: nothing ran it")
	}
	m, deliver := m.Update(cmd())
	if deliver == nil {
		t.Fatal("the bang ran and nothing was sent to the agent")
	}
	go deliver()

	f := awaitFrame(t, sent)
	if f.Kind != rpc.FrameSend || f.Text != "" || f.Shell == nil {
		t.Errorf("the agent received %v %q shell %+v, want a shell run: as text, the model answers it as a question", f.Kind, f.Text, f.Shell)
	}
	if !strings.Contains(shown(m), "for the agent") {
		t.Errorf("the output was not drawn in the conversation:\n%s", shown(m))
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
