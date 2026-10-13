package ui

// The manager is manager-<fleet> to every Claude session, and Wake draws it so:
// the roster, the room's headings and the composer's target show the name agents
// use, @manager-<fleet> reaches it from the room as @manager still does, and an
// agent's conversation offers it, so the operator can tell an agent to message it.

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

const harborManager = "manager-harbor"

// namedManagerReport is a fleet whose daemon says what claude calls its manager.
func namedManagerReport() *rpc.Status {
	return &rpc.Status{Running: true, ManagerClaude: harborManager, Sessions: []rpc.SessionStatus{
		{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle},
		{ID: "s1", Name: "jade", State: rpc.StateIdle},
	}}
}

func namedManagerRoom(t *testing.T) (App, *recorder) {
	t.Helper()
	fresh(t)
	rec := newRecorder(t)
	a := NewRoomApp(rec, Stream{}, nil).withSize(160, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: namedManagerReport()})
	rec.taken(t)
	return a, rec
}

// The roster and the room's heading over the manager's words name it as
// claude does; an ordinary agent is drawn as it always was.
func TestTheManagerIsDrawnAsClaudeSessionsKnowIt(t *testing.T) {
	a, _ := namedManagerRoom(t)
	a = a.observe("m1", core.Event{Kind: core.KindAssistantText, SessionID: "m1", Text: "the fleet is idle"})
	out := ansi.Strip(a.View())
	if strings.Count(out, harborManager) < 2 {
		t.Errorf("the roster and the room's heading do not both name the manager %q:\n%s", harborManager, out)
	}
	if m, _ := a.fleet.Agent("m1"); m.Name != harborManager || !core.IsManager(m.Name) {
		t.Errorf("the manager's row is %q, want %q and still the manager", m.Name, harborManager)
	}
	if j, _ := a.fleet.Agent("s1"); j.Name != "jade" {
		t.Errorf("an ordinary agent's row is %q, want its own name", j.Name)
	}
}

// From the room, @manager-<fleet> reaches the manager - the name on screen is
// one that routes - and @manager still does.
func TestTheRoomRoutesTheManagersShownNameToIt(t *testing.T) {
	for _, draft := range []string{"@" + harborManager + " status please", "@manager status please"} {
		a, rec := namedManagerRoom(t)
		m, cmd := typeAndSubmit(a, draft)
		drainBatch(cmd)
		_ = m
		got := rec.taken(t)
		if len(got) != 1 || got[0].Kind != rpc.FrameSend || got[0].SessionID != "m1" || got[0].Text != "status please" {
			t.Errorf("%q wrote %v, want one send of the body to the manager", draft, sentKinds(got))
		}
	}
}

// The room's @ menu offers the manager under the name on screen, and so does an
// agent's conversation - claude reaches it by that name - though never the
// manager's own.
func TestTheManagersShownNameIsOfferedWhereItRoutes(t *testing.T) {
	a, _ := namedManagerRoom(t)
	if got := a.withDraft("@manager-").completion.offers; !slices.Contains(got, "@"+harborManager) {
		t.Errorf("the room's @manager- offered %q, want @%s", got, harborManager)
	}
	fresh(t)
	dm := dmApp(newRecorder(t), Stream{}, "s1", "jade").withSize(160, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: namedManagerReport()})
	if got := dm.withDraft("@manager-").completion.offers; !slices.Contains(got, "@"+harborManager) {
		t.Errorf("jade's conversation offered %q for @manager-, want @%s so jade can be told to message it", got, harborManager)
	}
	fresh(t)
	own := dmApp(newRecorder(t), Stream{}, "m1", core.ManagerName).withSize(160, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: namedManagerReport()})
	if got := own.withDraft("@manager-").completion.offers; slices.Contains(got, "@"+harborManager) {
		t.Errorf("the manager's own conversation offered itself: %q", got)
	}
}

// Wake's own word still finds the drawn row wherever a command takes a name,
// not only in the room's router: the park notice says `/resume manager`, and
// /quit @manager reaches the manager's own refusal rather than "no such agent".
func TestManagerStillNamesTheDrawnRowInCommands(t *testing.T) {
	parked := namedManagerReport()
	parked.Sessions[0].State = rpc.StateParked
	fresh(t)
	rec := newRecorder(t)
	a := NewRoomApp(rec, Stream{}, nil).withSize(160, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: parked})
	rec.taken(t)
	m, cmd := typeAndSubmit(a, resumeVerb+" "+core.ManagerName)
	drainBatch(cmd)
	_ = m
	if got := rec.taken(t); len(got) != 1 || got[0].Kind != rpc.FrameWake || got[0].SessionID != "m1" {
		t.Errorf("/resume manager over the parked %s wrote %v, want it woken", harborManager, sentKinds(got))
	}

	b, _ := namedManagerRoom(t)
	m, cmd = typeAndSubmit(b, "/quit @"+core.ManagerName)
	drainBatch(cmd)
	if n := latestNotice(t); n != quitIsTheManager {
		t.Errorf("/quit @manager said %q, want the manager's own refusal %q", n, quitIsTheManager)
	}
}

// The check is exact: an agent somebody named manager-x before the daemon
// fenced the prefix, on a daemon that draws no name, is an agent like any other.
func TestAnAgentNamedLikeAManagerIsNotTheManager(t *testing.T) {
	fresh(t)
	rec := newRecorder(t)
	a := NewRoomApp(rec, Stream{}, nil).withSize(160, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: &rpc.Status{
		Running: true, Sessions: []rpc.SessionStatus{
			{ID: "x1", Name: "manager-x", State: rpc.StateIdle},
			{ID: "m1", Name: core.ManagerName, State: rpc.StateIdle},
		}}})
	rec.taken(t)
	if s := a.service(); s.ID != "m1" {
		t.Errorf("the service is %+v, want the manager m1, not manager-x", s)
	}
	m, cmd := typeAndSubmit(a, "@manager-x hi")
	drainBatch(cmd)
	_ = m
	if got := rec.taken(t); len(got) != 1 || got[0].SessionID != "x1" || got[0].Text != "hi" {
		t.Errorf("@manager-x hi wrote %v, want it sent to the agent x1", sentKinds(got))
	}
}

// The manager's conversation title keeps the drawn name across the next
// report, rather than flipping back to the daemon's word.
func TestTheManagersConversationKeepsItsDrawnName(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "m1", core.ManagerName).withSize(160, 40).applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: namedManagerReport()})
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: namedManagerReport()})
	if dm := a.dms["m1"]; dm == nil || dm.Name != harborManager {
		t.Errorf("the manager's conversation is titled %+v, want %q", dm, harborManager)
	}
}

// This fleet's manager keeps its default yellow under its drawn name; another
// fleet's, heading a cross-session line with no row of ours, does not wear it.
func TestOnlyThisFleetsManagerWearsTheManagersYellow(t *testing.T) {
	yellow, _ := identityStyle(defaultManagerColor)
	if got, _ := identityStyleFor(Agent{ID: "m1", Name: harborManager}); got.GetForeground() != yellow.GetForeground() {
		t.Errorf("this fleet's %s is not drawn in the manager's yellow", harborManager)
	}
	if got, _ := identityStyleFor(Agent{Name: "manager-atlas"}); got.GetForeground() == yellow.GetForeground() {
		t.Error("another fleet's manager is drawn in this fleet's manager yellow")
	}
}

// The fleet keeps the manager's drawn name through every copy an event makes:
// an event that moves an agent rebuilds the Fleet, and losing the name there
// left the manager's row unrecognised and the room with no one to send to.
func TestTheManagerStaysTheServiceAcrossAnEvent(t *testing.T) {
	a, rec := namedManagerRoom(t)
	a = a.observe("s1", core.Event{Kind: core.KindAssistantText, SessionID: "s1", Text: "ready"})
	if s := a.service(); s.ID != "m1" {
		t.Fatalf("after an event the service is %+v, want the manager m1", s)
	}
	m, cmd := typeAndSubmit(a, "go")
	drainBatch(cmd)
	_ = m
	if got := rec.taken(t); len(got) != 1 || got[0].SessionID != "m1" {
		t.Errorf("an unaddressed draft after an event wrote %v, want it sent to the manager", sentKinds(got))
	}
}
