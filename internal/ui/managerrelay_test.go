package ui

// The manager relays the operator's `@"session" …` with SendMessage (owner,
// 2026-10-03). With no fence on that tool, the room line drawn from the call
// itself is what keeps a send visible: it does not wait on the manager's prose.

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
)

// sendCall is a SendMessage tool call from sid, as the airlock decodes one
// (manager-relay.jsonl).
func sendCall(sid, to, text string) core.Event {
	return core.Event{Kind: core.KindToolUse, SessionID: sid, Tool: &core.ToolCall{
		ID: "t1", Name: core.ToolSendMessage, Send: &core.PeerSend{To: to, Text: text},
	}}
}

// The manager's send is drawn as the peer line its receiver would show,
// `↪ manager → wf peer`, carrying the words it sent.
func TestTheManagersSendIsDrawnInTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents(core.ManagerName, "jade")
	a = a.observe("s1", sendCall("s1", "wf peer", "are you there? reply if you can"))

	out := ansi.Strip(a.View())
	if !strings.Contains(out, core.ManagerName+crossSessionArrow+"wf peer") ||
		!strings.Contains(out, "are you there? reply if you can") {
		t.Errorf("the room does not show the manager's send to wf peer:\n%s", out)
	}
}

// Only the manager's: an agent's own send stays in its conversation, as every
// tool call does, and a send to a fleet agent is drawn once, by the receiver's
// own stream.
func TestOnlyTheManagersSendOutsideTheFleetIsDrawn(t *testing.T) {
	for name, tc := range map[string]struct{ sid, to string }{
		"an agent's send":       {"s2", "wf peer"},
		"the manager to jade":   {"s1", "jade"},
		"the manager to Jade":   {"s1", "Jade"},
		"a subagent's own send": {"s1", "wf peer"},
	} {
		a := newRoomApp(t).withSize(120, 40).withAgents(core.ManagerName, "jade")
		ev := sendCall(tc.sid, tc.to, "hello there")
		if name == "a subagent's own send" {
			ev.Subagent = &core.Subagent{}
		}
		if out := ansi.Strip(a.observe(tc.sid, ev).View()); strings.Contains(out, "hello there") {
			t.Errorf("%s reached the room:\n%s", name, out)
		}
	}
}
