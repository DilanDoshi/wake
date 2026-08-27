package ui

// The manager, from the room's side: it is where an unaddressed draft goes, it
// answers to its own name, and a broadcast is not to it.

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// A broadcast reaches the fleet and not the manager.
//
// It is a service, not a participant - and a manager told to report on the
// fleet by a message it also received is a manager reporting on itself, at the
// cost of one turn per broadcast that answers nothing.
func TestABroadcastDoesNotIncludeTheManager(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex", core.ManagerName)
	managerID := idOfAgentNamed(t, a, core.ManagerName)

	a, cmd := pressKey(a.withDraft("@all status"), tea.KeyMsg{Type: tea.KeyEnter})
	frames := sentFrames(t, a, cmd)
	if len(frames) != 2 {
		t.Fatalf("%d frames went out, want one per live agent and none for the manager: %+v", len(frames), frames)
	}
	for _, f := range frames {
		if f.SessionID == managerID {
			t.Errorf("a broadcast reached the manager (%s). It is a service, not a participant", managerID)
		}
	}
}

// An unaddressed draft goes to the manager, and the composer says so before ↵.
//
// The second half is the half that makes the first safe: this is the one route
// nobody typed a recipient for, so the room has to draw where it is going -
// otherwise `@src/auth.ts is the file` reaches the manager with nothing on
// screen having said it would.
func TestAnUnaddressedMessageGoesToTheManagerAndTheComposerSaysSo(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", core.ManagerName)
	managerID := idOfAgentNamed(t, a, core.ManagerName)

	a = a.withDraft("who is stuck?")
	if got := a.room.Composer().Target(); !strings.Contains(got, core.ManagerName) {
		t.Errorf("the composer reads %q for an unaddressed draft and does not name the manager: this is the "+
			"one route nobody chose a recipient for, so it is the one the room most has to draw", got)
	}

	a2, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, a2, cmd)
	if f.SessionID != managerID {
		t.Errorf("an unaddressed message went to %q, want the manager %q", f.SessionID, managerID)
	}
	if f.Text != "who is stuck?" {
		t.Errorf("the message was rewritten to %q", f.Text)
	}
}

// @manager reaches the manager by the ordinary name path.
//
// It is not in live(), so this is the one thing that proves the service is
// reachable by name at all - and it is what stops the exclusion above being a
// manager nobody can address.
func TestTheManagerAnswersToItsOwnNameFromTheRoom(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", core.ManagerName)
	managerID := idOfAgentNamed(t, a, core.ManagerName)

	a, cmd := pressKey(a.withDraft("@"+core.ManagerName+" who is stuck?"), tea.KeyMsg{Type: tea.KeyEnter})
	f := sentFrame(t, a, cmd)
	if f.SessionID != managerID {
		t.Errorf("@%s reached %q, want %q", core.ManagerName, f.SessionID, managerID)
	}
	if f.Text != "who is stuck?" {
		t.Errorf("the mention was not taken off: %q", f.Text)
	}
}

// With no manager the refusal says what is missing and how to get it.
//
// The existing refusal test asserts that nothing is sent and the draft
// survives; this one is about the sentence. A room that says only "the room
// does not guess" leaves an operator with no way to learn there is a thing
// whose whole job is being the answer.
func TestWithNoManagerAnUnaddressedMessageIsRefusedAndSaysWhatIsMissing(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney", "alex").withDraft("who is stuck?")
	a2, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Fatalf("an unaddressed message was sent somewhere: %+v", sentFrames(t, a2, cmd))
	}
	if !strings.Contains(NoAddressee, core.ManagerName) {
		t.Errorf("the refusal is %q and does not name the manager: it is the thing that is missing", NoAddressee)
	}
	if notice.Count(NoAddressee) != 1 {
		t.Error("the room swallowed the keystroke without saying how to address it")
	}
	if a2.room.Composer().Value() != "who is stuck?" {
		t.Errorf("the draft was cleared on a message that went nowhere: %q", a2.room.Composer().Value())
	}
}

// With the manager parked, the room says so rather than saying it is missing.
//
// The general refusal used to point at `wake manager`, and that command
// **fails** against a parked manager because the name is still claimed - so the
// two sentences sent the operator to each other and neither named a verb that
// works. Both halves are asserted: the right sentence fires, and the wrong one
// does not. That the sentence names a command that exists is
// TestTheRoomsManagerAdviceNamesTheCommandThatWorks, over both sentences at
// once, rather than a third assertion here.
func TestAParkedManagerIsSaidToBeParkedRatherThanMissing(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
		Kind: rpc.FrameStatusPush,
		Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
			{ID: "s1", Name: "sydney", State: rpc.StateIdle},
			{ID: "s2", Name: core.ManagerName, State: rpc.StateParked},
		}},
	}).withDraft("who is stuck?")

	a2, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("a draft was sent to a parked manager: %+v", sentFrames(t, a2, cmd))
	}
	if notice.Count(managerParked) != 1 {
		t.Errorf("the room did not say the manager is parked. It reported %d times; the general refusal "+
			"points at `wake manager`, which fails against a parked one because the name is still claimed",
			notice.Count(managerParked))
	}
	if notice.Count(NoAddressee) != 0 {
		t.Error("the room said the manager is missing when it is parked. The two states need different " +
			"sentences: one is a spawn and the other is a wake")
	}
}

// With no manager at all the general refusal is still the right one.
//
// The two-outcome half: a fix that said "the manager is parked" whenever there
// was no default addressee would be as wrong in the other direction, and the
// test above cannot see it.
func TestWithNoManagerAtAllTheRoomStillNamesTheVerbThatStartsOne(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).withAgents("sydney").withDraft("who is stuck?")
	a2, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("an unaddressed message was sent somewhere: %+v", sentFrames(t, a2, cmd))
	}
	if notice.Count(NoAddressee) != 1 {
		t.Error("the room did not tell the operator how to get a default addressee at all")
	}
	if notice.Count(managerParked) != 0 {
		t.Error("the room said the manager is parked and there is no manager: /resume manager would be " +
			"refused, which is the same loop the other way round")
	}
}

// serviceStates is whether a manager in each state is the room's default
// addressee, with the reason per cell.
//
// **It is a table rather than two examples because a state filter with no
// totality guard fails open**, and that is not hypothetical here: it is the
// exact defect `internal/mcp`'s liveSessions shipped with and Task 14 fixed -
// three states named, everything else *included* - one package over. This one
// had the same shape. The reader is not a model, but the surface is worse in
// one way: it is the route nobody typed a recipient for, so a wrong answer here
// is a message going somewhere the operator never named.
//
// Withheld is the recoverable direction. A manager not offered means the room
// says so and names the way back; a manager offered wrongly means a frame the
// daemon refuses, with the room having drawn the message as sent.
var serviceStates = map[string]struct {
	offered bool
	why     string
}{
	rpc.StateIdle:    {offered: true, why: "the ordinary case: a manager with a process, between turns"},
	rpc.StateWorking: {offered: true, why: "a manager mid-turn takes the next message like any agent - the daemon queues it, and refusing here would make the room drop a draft because the manager was busy"},
	rpc.StateSilent:  {offered: true, why: "silent is a *guess* about a working agent that has not spoken for a while, not a state of the process. Its stdin is open"},
	rpc.StateBlocked: {offered: true, why: "blocked is an outstanding permission ask, which a message does not resolve and does not make worse. The daemon accepts the write; ⌃C is what must refuse this state, because parking closes stdin and writes a denial nobody made"},
	rpc.StateEnded:   {offered: false, why: "no process. The daemon refuses the write, and `wake manager` is the right advice - which is why this state must not be answered with managerParked"},
	rpc.StateParked:  {offered: false, why: "no process, and the name is still claimed - so `wake manager` would be refused too. This is the state the room answers with managerParked and /resume"},
}

// The verdict is total over the states a running daemon can report.
//
// Rung 4, reusing the scan ⌃C and ⌃F already run: the domain is
// agent.stateLocked's own range, so a seventh state is a build failure here
// until somebody says whether the room may send an unaddressed draft to a
// manager in it. Both directions, so a cell over an input no daemon can produce
// fails too - a verdict about an impossible state reads as coverage.
func TestServiceHasAVerdictForEveryStateARunningDaemonCanReport(t *testing.T) {
	declared := sessionStateConstants(t)
	reachable := statesARunningDaemonReports(t)

	for name, state := range declared {
		_, decided := serviceStates[state]
		why, excused := unreachableInAForkReport[state]
		switch {
		case decided && excused:
			t.Errorf("rpc.%s = %q is both decided here and excused as unreachable (%s): one of the two is wrong", name, state, why)
		case !decided && !excused:
			t.Errorf("rpc.%s = %q is a state a session can be reported in, and nothing here says whether a "+
				"manager in it is the room's default addressee. This filter had no totality guard and "+
				"failed **open**, which is the defect internal/mcp's liveSessions shipped with one package "+
				"over - and this is the route nobody typed a recipient for", name, state)
		case decided && !reachable[state]:
			t.Errorf("rpc.%s = %q has a verdict here, but agent.stateLocked never returns it - so the cell "+
				"asserts something about an input no daemon can produce, which reads as coverage", name, state)
		}
	}
	for state := range reachable {
		if _, decided := serviceStates[state]; !decided {
			t.Errorf("agent.stateLocked can report %q and nothing here says whether the room sends an "+
				"unaddressed draft to a manager in it", state)
		}
	}
}

// And the behaviour, per member of that table.
//
// Driven through service() rather than through a keystroke, because the
// property is which rows it offers - and asserted per member rather than
// counted, so a narrowing that captures a subset is visible.
func TestServiceOffersExactlyTheStatesTheTableSays(t *testing.T) {
	checked := 0
	for state, want := range serviceStates {
		t.Run(state, func(t *testing.T) {
			a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
				Kind: rpc.FrameStatusPush,
				Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
					{ID: "s1", Name: "sydney", State: rpc.StateIdle},
					{ID: "s2", Name: core.ManagerName, State: state},
				}},
			})
			got := a.service()
			if offered := got.ID != ""; offered != want.offered {
				t.Errorf("service() offers a %q manager = %v, want %v (%s).\n"+
					"Offered wrongly, ↵ on an unaddressed draft puts a frame the daemon refuses on the "+
					"wire while the room draws the message as sent", state, offered, want.offered, want.why)
			}
			if want.offered && got.Name != core.ManagerName {
				t.Errorf("service() offered %+v, want it named %q - @manager routes on that name", got, core.ManagerName)
			}
		})
		checked++
	}
	if checked != len(serviceStates) {
		t.Fatalf("checked %d of %d states", checked, len(serviceStates))
	}
}

// Only a *parked* manager gets the parked sentence, and that is the third state
// the two examples above cannot separate.
//
// An **ended** manager also has no default addressee, and the right advice
// there is the opposite one: the name is released, so `wake manager` works and
// `/resume manager` does not. A hasParkedManager keyed on the name alone
// survives every other test in this file - verified - because a live manager
// never reaches the refusal path at all.
func TestOnlyAParkedManagerGetsTheParkedSentence(t *testing.T) {
	for state, want := range map[string]string{rpc.StateParked: managerParked, rpc.StateEnded: NoAddressee} {
		t.Run(state, func(t *testing.T) {
			fresh(t)
			a := newRoomApp(t).withSize(200, 40).applyFrame(rpc.Frame{
				Kind: rpc.FrameStatusPush,
				Status: &rpc.Status{Running: true, Sessions: []rpc.SessionStatus{
					{ID: "s1", Name: "sydney", State: rpc.StateIdle},
					{ID: "s2", Name: core.ManagerName, State: state},
				}},
			}).withDraft("who is stuck?")

			a2, cmd := pressKey(a, tea.KeyMsg{Type: tea.KeyEnter})
			if cmd != nil {
				t.Fatalf("a draft was sent to a %s manager: %+v", state, sentFrames(t, a2, cmd))
			}
			if notice.Count(want) != 1 {
				t.Errorf("a %s manager was reported with the wrong sentence. An ended manager has released "+
					"its name, so `wake manager` works and /resume does not; a parked one is the other way "+
					"round", state)
			}
		})
	}
}

// idOfAgentNamed is the session id the fleet gave one name, read back rather
// than assumed from the order withAgents happens to use.
func idOfAgentNamed(t *testing.T, a App, name string) string {
	t.Helper()
	agent, ok := a.fleet.ByName(name)
	if !ok {
		t.Fatalf("the fleet holds no agent called %q: the fixture is not the one this test names", name)
	}
	return agent.ID
}
