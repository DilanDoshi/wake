package mcp

// The acting half: the two tools that change something, and the refusals that
// stand in front of them.
//
// Every assertion about a refusal is made on the *far side* - what the fleet
// was asked to do - rather than on the error that came back. tools_test.go's
// spyFleet comment records why: the first version of the name guard asserted
// only that some error mentioning list_agents arrived, and deleting the whole
// isSessionID check left it green, because a name that fell through to the
// lookup produced an error mentioning list_agents too. A refusal that reaches
// the daemon first is not a refusal.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const idMira = "1e5c1b8a-0000-4000-8000-000000000003"

// actingFleet is a fleet that records, over the sessions given.
func actingFleet(sessions ...rpc.SessionStatus) fakeFleet {
	f := fleetOf(sessions...)
	f.acts = &actions{}
	return f
}

func onePeter() fakeFleet {
	return actingFleet(rpc.SessionStatus{
		ID: idPeter, Name: "peter", Label: "api-v2", Dir: "/repos/api", State: rpc.StateWorking,
	})
}

func TestSendToAgentReachesTheDaemonWithAnIdAndTheText(t *testing.T) {
	const text = "pause and write up where you got to"
	f := onePeter()
	call(t, f, "send_to_agent", map[string]any{agentIDArg: idPeter, messageArg: text})

	if len(f.acts.sent) != 1 {
		t.Fatalf("the daemon was asked to send %d messages, want 1: %+v", len(f.acts.sent), f.acts.sent)
	}
	if got := f.acts.sent[0]; got.id != idPeter || got.text != text {
		t.Errorf("sent %+v, want the id from list_agents and the text unchanged", got)
	}
}

// The message reaches the agent byte for byte.
//
// A tool that trimmed, prefixed or re-wrapped what the manager wrote would be
// putting words in its mouth, and the agent on the far side cannot tell the
// difference. core.Resolve is the only thing in this tree entitled to edit a
// message on its way past, and it does that for a mention it resolved.
func TestTheTextTheManagerWroteIsWhatTheAgentGets(t *testing.T) {
	const text = "  @all is not a mention here\n\nrun `make test` and report\t"
	f := onePeter()
	call(t, f, "send_to_agent", map[string]any{agentIDArg: idPeter, messageArg: text})

	if len(f.acts.sent) != 1 || f.acts.sent[0].text != text {
		t.Errorf("sent %+v, want the text exactly as written: a manager's message is the manager's words", f.acts.sent)
	}
}

func TestInterruptIsATool(t *testing.T) {
	f := onePeter()
	call(t, f, "interrupt", map[string]any{agentIDArg: idPeter})

	if len(f.acts.interrupted) != 1 || f.acts.interrupted[0] != idPeter {
		t.Fatalf("interrupt did nothing (%+v). 'tell all backend working members to pause' needs an action on a *set*, and pause is the ⎋ that shipped - send_to_agent alone cannot express it", f.acts.interrupted)
	}
}

// A message to an agent that is not there is reported, and the daemon is never
// asked.
//
// Both halves matter and only the second one can fail quietly. A manager that
// believes it delegated something is worse than one that knows it failed: it
// reports the work as assigned and nobody looks at it again.
func TestActingOnAnAgentThatIsNotThereIsAnErrorAndNotASilentNoOp(t *testing.T) {
	const ghost = "1e5c1b8a-0000-4000-8000-00000000dead"
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"send_to_agent", map[string]any{agentIDArg: ghost, messageArg: "hello"}},
		{"interrupt", map[string]any{agentIDArg: ghost}},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := actingFleet()
			_, err := callErr(t, f, c.tool, c.args)
			if err == nil {
				t.Fatalf("%s on a nonexistent agent reported success. A manager that believes it delegated something is worse than one that knows it failed - it will report the work as assigned", c.tool)
			}
			if !strings.Contains(err.Error(), "list_agents") {
				t.Errorf("the refusal does not say what to do instead: %v. The reader is a model, not somebody who can look at the screen and reconsider", err)
			}
			if len(f.acts.sent) != 0 || len(f.acts.interrupted) != 0 {
				t.Errorf("the daemon was asked to act on an agent that is not in the fleet: %+v %+v", f.acts.sent, f.acts.interrupted)
			}
		})
	}
}

// The states list_agents does not offer are the states these tools refuse, and
// the refusal happens before the daemon is asked.
//
// A parked session is the one that decides this. It keeps its name, its label
// and its directory, so it reads as addressable from every angle a model can
// see, and what it has not got is a process. Without this the manager sends
// into it, the daemon refuses on a connection this tool has already stopped
// reading, and "Sent." is what the model is told.
func TestAnAgentListAgentsDoesNotOfferIsRefusedBeforeTheDaemonIsAsked(t *testing.T) {
	for state, offered := range agentStates {
		if offered {
			continue
		}
		t.Run(state, func(t *testing.T) {
			f := actingFleet(rpc.SessionStatus{ID: idPeter, Name: "peter", State: state})
			if _, err := callErr(t, f, "send_to_agent", map[string]any{
				agentIDArg: idPeter, messageArg: "carry on",
			}); err == nil {
				t.Errorf("send_to_agent accepted a %s session", state)
			}
			if len(f.acts.sent) != 0 {
				t.Errorf("a %s session was sent to anyway: %+v", state, f.acts.sent)
			}
		})
	}
}

// An empty message is refused rather than started as an empty turn.
//
// A turn costs what a turn costs, and an agent handed nothing answers about
// nothing - so the manager pays for a turn, gets a reply that says something,
// and has no way to tell it apart from an answer.
func TestAnEmptyMessageIsRefusedRatherThanCostingATurnForNothing(t *testing.T) {
	for _, blank := range []any{"", "   ", "\n\t ", 42, nil} {
		f := onePeter()
		args := map[string]any{agentIDArg: idPeter}
		if blank != nil {
			args[messageArg] = blank
		}
		_, err := callErr(t, f, "send_to_agent", args)
		if err == nil {
			t.Errorf("send_to_agent accepted %#v as a message", blank)
		}
		if len(f.acts.sent) != 0 {
			t.Errorf("a blank message reached the daemon: %+v", f.acts.sent)
		}
		if err != nil && !strings.Contains(err.Error(), messageArg) {
			t.Errorf("the refusal does not name the argument that was wrong: %v", err)
		}
	}
}

// A daemon that refuses the write is reported, not reported as success.
//
// This is the failure requireLive cannot see: the fleet report said the agent
// was there, and the write failed anyway - the session ended in between, or its
// input queue is full. "Sent." over that is the defect this whole file is about.
func TestADaemonThatRefusesTheWriteIsNotReportedAsSent(t *testing.T) {
	want := "session " + idPeter + " is not reading its input"
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"send_to_agent", map[string]any{agentIDArg: idPeter, messageArg: "hello"}},
		{"interrupt", map[string]any{agentIDArg: idPeter}},
	} {
		t.Run(c.tool, func(t *testing.T) {
			f := onePeter()
			f.actErr = errFleet(want)
			_, err := callErr(t, f, c.tool, c.args)
			if err == nil {
				t.Fatalf("%s reported success over a daemon that refused it", c.tool)
			}
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal hid why it failed: %v", err)
			}
		})
	}
}

// A name is refused by every acting tool, and the daemon is never asked about
// it.
//
// The same property agent_status has, restated over the tools that *act*,
// because this is where it bites: internal/daemon/names.go rules that nothing
// crosses the wire addressed by name, and the reaper's only proof of identity
// is a session UUID in an argv. A word a model chose is the last thing that may
// become an address.
func TestAnActingToolRefusesANameAndNeverAsksTheDaemonAboutIt(t *testing.T) {
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"send_to_agent", map[string]any{agentIDArg: "peter", messageArg: "hello"}},
		{"interrupt", map[string]any{agentIDArg: "peter"}},
	} {
		t.Run(c.tool, func(t *testing.T) {
			lists := 0
			f := actingFleet(rpc.SessionStatus{ID: idPeter, Name: "peter", State: rpc.StateWorking})
			spy := spyFleet{lists: &lists, fleet: f}

			_, err := callErr(t, spy, c.tool, c.args)
			if err == nil {
				t.Fatalf("%s accepted a display name as an address", c.tool)
			}
			if lists != 0 {
				t.Errorf("the daemon was asked %d times about a display name: refusing after the lookup is not the property", lists)
			}
			if len(f.acts.sent) != 0 || len(f.acts.interrupted) != 0 {
				t.Errorf("a display name reached the daemon as an address: %+v %+v", f.acts.sent, f.acts.interrupted)
			}
		})
	}
}

// The manager is not an agent it may act on, and it is not on its own roster.
//
// A manager that can message itself is an unbounded loop one send_to_agent
// away: the send starts a turn, the turn can send again, nobody is watching and
// every iteration costs a turn's tokens. An interrupt of itself aborts the turn
// the tool call is inside. Neither has a use, and this is the one exclusion
// whose absence a model would find rather than a person.
//
// core.ManagerName is the discriminator because the daemon reserves that name -
// internal/daemon/names.go's reservedNames reads core's own constants, and
// names_test.go requires the two sets to be equal - so no ordinary agent can
// hold it and the filter cannot catch somebody else.
func TestTheManagerIsNotOfferedToItselfAndCannotBeSentTo(t *testing.T) {
	f := actingFleet(
		rpc.SessionStatus{ID: idPeter, Name: "peter", Label: "api-v2", State: rpc.StateWorking},
		rpc.SessionStatus{ID: idMira, Name: core.ManagerName, Label: "api-v2", State: rpc.StateIdle},
	)

	if out := call(t, f, "list_agents", nil); strings.Contains(out, idMira) {
		t.Errorf("list_agents offered the manager its own row:\n%s", out)
	}
	if out := RollUp(f.status); strings.Contains(out, idMira) {
		t.Errorf("roll_up put the manager in its own digest:\n%s", out)
	}
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"send_to_agent", map[string]any{agentIDArg: idMira, messageArg: "what am I doing"}},
		{"interrupt", map[string]any{agentIDArg: idMira}},
	} {
		if _, err := callErr(t, f, c.tool, c.args); err == nil {
			t.Errorf("%s accepted the manager's own session: a manager that can message itself is a loop one call away, and nobody is watching", c.tool)
		}
	}
	if len(f.acts.sent) != 0 || len(f.acts.interrupted) != 0 {
		t.Errorf("the manager acted on itself: %+v %+v", f.acts.sent, f.acts.interrupted)
	}
	// And the exclusion is the manager's, not everybody's.
	if out := call(t, f, "list_agents", nil); !strings.Contains(out, idPeter) {
		t.Fatalf("list_agents lost the ordinary agent too, so nothing above is asserting anything about the manager:\n%s", out)
	}
}

// agent_status still answers for the manager, which is what makes the exclusion
// above cheap: the roster is what a manager chooses from, not what it may ask
// about. Same split ended and parked already have.
func TestAgentStatusStillAnswersForTheManagersOwnSession(t *testing.T) {
	f := actingFleet(rpc.SessionStatus{ID: idMira, Name: core.ManagerName, State: rpc.StateIdle})
	if out := call(t, f, "agent_status", map[string]any{agentIDArg: idMira}); !strings.Contains(out, core.ManagerName) {
		t.Errorf("agent_status refused to describe the manager's own session:\n%s", out)
	}
}

// errFleet is a daemon-side failure with a sentence in it.
func errFleet(msg string) error { return fleetError(msg) }

type fleetError string

func (e fleetError) Error() string { return string(e) }
