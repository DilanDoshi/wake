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
		_, cmd := typeAndSubmit(a, draft)
		drainBatch(cmd)
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
	_, cmd := typeAndSubmit(a, resumeVerb+" "+core.ManagerName)
	drainBatch(cmd)
	if got := rec.taken(t); len(got) != 1 || got[0].Kind != rpc.FrameWake || got[0].SessionID != "m1" {
		t.Errorf("/resume manager over the parked %s wrote %v, want it woken", harborManager, sentKinds(got))
	}

	b, _ := namedManagerRoom(t)
	_, cmd = typeAndSubmit(b, "/quit @"+core.ManagerName)
	drainBatch(cmd)
	if n := latestNotice(t); n != quitIsTheManager {
		t.Errorf("/quit @manager said %q, want the manager's own refusal %q", n, quitIsTheManager)
	}
}

// ↵ over `@manager` sends it as typed where Wake reads the word - the room's
// leading mention, an @who command's target - since Wake resolves it to the drawn
// manager-<fleet>. Prose is read by agents, who know only the long name, and the
// router's alias match is exact, so there ↵ still writes the long form; so does a
// conversation, which routes nothing.
func TestEnterSendsTheManagersShortNameWhereWakeReadsIt(t *testing.T) {
	for _, tc := range []struct {
		draft        string
		image, sends bool
	}{
		{"@manager", false, true},
		{" @manager", true, true}, // an image chip is no text on the wire
		{"/quit @manager", false, true},
		{"see @manager", false, false},
		{"@Manager", false, false},
		{"/task @jade @manager", false, false}, // the label is text
		{"/quit\n@manager", false, false},      // the router splits on a space alone
		{" /quit @manager", true, false},       // a chip ahead makes it a message
	} {
		a, _ := namedManagerRoom(t)
		if tc.image {
			a = dropImage(t, a, writePNG(t, "shot.png"))
		}
		a = a.withDraft(tc.draft)
		if !a.completionUp() || a.completion.offers[a.completion.cursor] != "@"+harborManager {
			t.Fatalf("%q drew %q, want @%s under the cursor, so this asserts nothing", tc.draft, a.completion.offers, harborManager)
		}
		if next, _, took := a.enterCompletes(); took == tc.sends {
			t.Errorf("↵ over the room's %q left %q, want sent=%v", tc.draft, next.composer().Value(), tc.sends)
		}
	}

	fresh(t)
	dm := dmApp(newRecorder(t), Stream{}, "s1", "jade").withSize(160, 40).
		applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: namedManagerReport()}).withDraft("@" + core.ManagerName)
	if !dm.completionUp() {
		t.Fatal("jade's conversation drew no menu for @manager, so this asserts nothing")
	}
	if next, _, took := dm.enterCompletes(); !took || next.composer().Value() != "@"+harborManager+" " {
		t.Errorf("↵ over jade's @manager left %q, want @%s: claude knows the manager by that name",
			next.composer().Value(), harborManager)
	}
}

// A typed `@manager` floats the row it routes to, so an agent whose name only
// starts with it (`managers` - only `manager-` is fenced) cannot take the cursor
// and ↵ cannot send to it.
func TestTheManagersRowHoldsTheCursorForATypedManager(t *testing.T) {
	report := namedManagerReport()
	report.Sessions = append([]rpc.SessionStatus{{ID: "x1", Name: "managers", State: rpc.StateIdle}}, report.Sessions...)
	fresh(t)
	a := NewRoomApp(newRecorder(t), Stream{}, nil).withSize(160, 40).
		applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: report}).withDraft("@" + core.ManagerName)
	if !slices.Contains(a.completion.offers, "@managers") {
		t.Fatalf("the menu is %q, want @managers on it too, so this asserts nothing", a.completion.offers)
	}
	if got := a.completion.offers[a.completion.cursor]; got != "@"+harborManager {
		t.Errorf("the cursor is on %q for a typed @manager, want @%s, the row it routes to", got, harborManager)
	}
}

// An outside session named `manager` is not offered in the room: @manager
// reaches the fleet's manager (core.Resolve's alias, matched exactly), never the
// session. One named `Manager` is, since @Manager passes through to it.
func TestTheRoomOffersNoOutsideSessionNamedLikeTheManager(t *testing.T) {
	a, _ := namedManagerRoom(t)
	a = a.applyFrame(peersReply(core.Peer{Name: core.ManagerName, Dir: "/tmp/m"})).withDraft("@" + core.ManagerName)
	for _, offer := range a.completion.offers {
		if a.completion.tags[offer].dir != "" {
			t.Errorf("@manager offered the outside session %q (%s), which ↵ would not reach", offer, a.completion.tags[offer].dir)
		}
	}
	b, _ := namedManagerRoom(t)
	b = b.applyFrame(peersReply(core.Peer{Name: "Manager", Dir: "/tmp/m"})).withDraft("@Manager")
	if !slices.Contains(b.completion.offers, "@Manager") {
		t.Errorf("@Manager offered %q, want the outside session @Manager, which ↵ reaches through the manager", b.completion.offers)
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
	_, cmd := typeAndSubmit(a, "@manager-x hi")
	drainBatch(cmd)
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
	_, cmd := typeAndSubmit(a, "go")
	drainBatch(cmd)
	if got := rec.taken(t); len(got) != 1 || got[0].SessionID != "m1" {
		t.Errorf("an unaddressed draft after an event wrote %v, want it sent to the manager", sentKinds(got))
	}
}

// The operator renames the manager's end part with the command that renames any
// agent: `@manager /rename ops` reaches claude and mirrors onto Wake's handle
// for the manager, by either name it answers to - and unaddressed, since the
// manager is the room's default. The manager is the service rather than a
// mention, which is why this is its own test. The daemon decides what the mirror
// makes it (manager-ops), so the mirror carries what was typed.
func TestRenamingTheManagersEndPartFromTheRoom(t *testing.T) {
	for _, who := range []string{"@" + core.ManagerName + " ", "@" + harborManager + " ", ""} {
		a, _ := namedManagerRoom(t)
		_, cmd := typeAndSubmit(a, who+"/rename ops")
		var mirrored, passed bool
		for _, f := range batchFrames(t, a, cmd) {
			mirrored = mirrored || f.Kind == rpc.FrameRename && f.SessionID == "m1" && f.Text == "ops" && f.SelfRenames
			passed = passed || f.Kind == rpc.FrameSend && f.SessionID == "m1" && strings.Contains(f.Text, "rename ops")
		}
		if !mirrored || !passed {
			t.Errorf("%s/rename ops mirrored %v and reached claude %v, want both", who, mirrored, passed)
		}
	}
}

// Once the report names the manager differently every surface follows: the
// row, an open conversation's header, and what resolves - the old handle
// resolves to nothing, as for any rename, and Wake's own word still finds it.
func TestTheManagersRenameIsFollowedByEverySurface(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "m1", core.ManagerName).withSize(160, 40).
		applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: namedManagerReport()})
	renamed := namedManagerReport()
	renamed.ManagerClaude = "manager-ops"
	a = a.applyFrame(rpc.Frame{Kind: rpc.FrameStatusPush, Status: renamed})

	if m, _ := a.fleet.Agent("m1"); m.Name != "manager-ops" || !a.fleet.isManager(m.Name) {
		t.Errorf("the manager's row is %q, want manager-ops and still the manager", m.Name)
	}
	if got := a.dms["m1"].Name; got != "manager-ops" {
		t.Errorf("the manager's open conversation is headed %q, want manager-ops", got)
	}
	for who, want := range map[string]bool{"manager-ops": true, core.ManagerName: true, harborManager: false} {
		if _, ok := a.fleet.ByName(who); ok != want {
			t.Errorf("%q resolves to the manager: %v, want %v", who, ok, want)
		}
	}
}
