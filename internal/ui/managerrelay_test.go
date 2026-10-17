package ui

// The manager relays the operator's `@"session" …` with SendMessage (owner,
// 2026-10-03). With no fence on that tool, the room line drawn from the call
// itself is what keeps a send visible: it does not wait on the manager's prose.
// Every agent's send outside the fleet is drawn the same way (2026-10-16).

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

// Any agent's send to a session outside the fleet is drawn too (owner,
// 2026-10-16), under that agent's own name. One to a fleet agent is drawn once,
// by the receiver's own stream, and a subagent's own send not at all.
func TestAnyAgentsSendOutsideTheFleetIsDrawnAndOnlyThat(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents(core.ManagerName, "jade", "sydney")
	out := ansi.Strip(a.observe("s3", sendCall("s3", "wf peer", "hello from sydney")).View())
	if !strings.Contains(out, "sydney"+crossSessionArrow+"wf peer") || !strings.Contains(out, "hello from sydney") {
		t.Errorf("the room does not show sydney's send to wf peer:\n%s", out)
	}

	for name, tc := range map[string]struct{ sid, to string }{
		"the manager to jade":       {"s1", "jade"},
		"the manager to Jade":       {"s1", "Jade"},
		"an agent to a fleet agent": {"s3", "jade"},
		"a subagent's own send":     {"s3", "wf peer"},
	} {
		a := newRoomApp(t).withSize(120, 40).withAgents(core.ManagerName, "jade", "sydney")
		ev := sendCall(tc.sid, tc.to, "hello there")
		if name == "a subagent's own send" {
			ev.Subagent = &core.Subagent{}
		}
		if out := ansi.Strip(a.observe(tc.sid, ev).View()); strings.Contains(out, "hello there") {
			t.Errorf("%s reached the room:\n%s", name, out)
		}
	}
}
