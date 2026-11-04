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
	"github.com/DilanDoshi/wake/internal/rpc"
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

// An agent continues a subagent with SendMessage to its agentId, which is a
// task id of its own and no session at all: a private brief that never reaches
// the room.
func TestASendToOneOfItsOwnSubagentsIsNotDrawnInTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents(core.ManagerName, "jade", "sydney")
	a = a.observe("s3", started("a73fc145a7d730b54", "a73fc145a7d730b54", "map the repo", "Explore", core.TaskAgent))
	out := ansi.Strip(a.observe("s3", sendCall("s3", "a73fc145a7d730b54", "now check the daemon too")).View())
	if strings.Contains(out, "now check the daemon too") {
		t.Errorf("a continuation of sydney's own subagent reached the room:\n%s", out)
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

// sentTo is sendCall as the daemon delivers a send by address: who it reached,
// read off Claude Code's session registry (daemon sendto.go).
func sentTo(sid, to, text, toName, toID string) core.Event {
	ev := sendCall(sid, to, text)
	ev.Tool.Send.ToName, ev.Tool.Send.ToID = toName, toID
	return ev
}

// A reply copies the from of the message it answers, a socket address. One the
// daemon names as a fleet agent is left to that agent's own stream, as a send by
// name is: the room draws the message once, sender → recipient, never the path.
func TestASendByAddressToAFleetAgentIsDrawnOnceByItsReceiver(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents(core.ManagerName, "jade", "sydney")
	a = a.observe("s3", sentTo("s3", "uds:/tmp/cc-socks/40123.sock", "ready to be quit", "jade", "s2"))
	a = a.observe("s2", core.Event{Kind: core.KindCrossSession, SessionID: "s2", FromName: "sydney", Text: "ready to be quit"})

	out := ansi.Strip(a.View())
	if n := strings.Count(out, "ready to be quit"); n != 1 {
		t.Errorf("the message is drawn %d times, want once:\n%s", n, out)
	}
	if !strings.Contains(out, "sydney"+crossSessionArrow+"jade") || strings.Contains(out, "uds:") {
		t.Errorf("the room does not draw sydney → jade alone:\n%s", out)
	}
}

// A session outside the fleet is drawn under the name the registry gives it; an
// address nothing named keeps the address; and a fleet recipient with no process,
// ended or parked, is drawn from the call, since no stream of its will draw it.
func TestASendByAddressTheReceiverWillNotDrawIsDrawnFromTheCall(t *testing.T) {
	a := newRoomApp(t).withSize(120, 40).withAgents(core.ManagerName, "jade", "sydney")
	out := ansi.Strip(a.observe("s3", sentTo("s3", "uds:/tmp/cc-socks/40123.sock", "hello there", "fable QA", "")).View())
	if !strings.Contains(out, "sydney"+crossSessionArrow+"fable QA") || strings.Contains(out, "uds:") {
		t.Errorf("a send outside the fleet is not drawn under its name:\n%s", out)
	}

	out = ansi.Strip(a.observe("s3", sendCall("s3", "uds:/tmp/cc-socks/40123.sock", "hello there")).View())
	if !strings.Contains(out, "sydney"+crossSessionArrow+"uds:/tmp/cc-socks/40123.sock") {
		t.Errorf("an address nothing named is not drawn as it was sent:\n%s", out)
	}

	for _, state := range []string{rpc.StateEnded, rpc.StateParked} {
		st := rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s2", Name: "jade", State: state},
			{ID: "s3", Name: "sydney", State: rpc.StateIdle},
		}}
		a = newRoomApp(t).withSize(120, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &st})
		out = ansi.Strip(a.observe("s3", sentTo("s3", "uds:/tmp/cc-socks/40123.sock", "hello there", "jade", "s2")).View())
		if !strings.Contains(out, "sydney"+crossSessionArrow+"jade") {
			t.Errorf("a send to a %s fleet agent is not drawn:\n%s", state, out)
		}
	}
}
