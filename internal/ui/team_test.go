package ui

import (
	"net"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// /team carries the target and the tag, the grammar /name, /task and /color use:
// a bare tag is the conversation you are in, an @who is that agent, and an
// @who typed in the room is the mention→target bridge. The value is validated by
// the daemon, so the client sends what was typed - "none" included, which the
// daemon reads as clear.
func TestTeamCarriesTheTargetAndTheTag(t *testing.T) {
	for _, tc := range []struct {
		name, draft, session, text string
		room                       bool
	}{
		{name: "team this conversation", draft: "/team backend", session: "s1", text: "backend"},
		{name: "team another agent", draft: "/team @sydney frontend", session: "s2", text: "frontend"},
		{name: "team from the room", draft: "/team @sydney infra", session: "s2", text: "infra", room: true},
		{name: "clear a team", draft: "/team none", session: "s1", text: "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			conn, sent := pipeClient(t)
			a := dmApp(conn, Stream{}, "s1", "alex").withAgents("alex", "sydney").withSize(200, 40)
			if tc.room {
				a = a.showRoom()
			}

			_, cmd := typeAndSubmit(a, tc.draft)
			go func() { _ = runCmdQuietly(cmd) }()
			f := awaitFrame(t, sent)

			if f.Kind != rpc.FrameTeam {
				t.Fatalf("%q wrote a %q frame, want %q", tc.draft, f.Kind, rpc.FrameTeam)
			}
			if f.SessionID != tc.session {
				t.Errorf("%q was addressed to %q, want %q", tc.draft, f.SessionID, tc.session)
			}
			if f.Text != tc.text {
				t.Errorf("%q asked for %q, want %q", tc.draft, f.Text, tc.text)
			}
		})
	}
}

// /team guesses no target, /name's own rule: the room is not one conversation,
// and a bare /team there has nobody to tag.
func TestTeamRefusesRatherThanGuess(t *testing.T) {
	for _, tc := range []struct {
		name, draft, says string
		room              bool
	}{
		{name: "no target in the room", draft: "/team backend", says: noTeamTarget, room: true},
		{name: "no tag", draft: "/team", says: teamUsage},
		{name: "a tag for nobody", draft: "/team @nobody backend", says: noSuchAgent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh(t)
			a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withAgents("alex").withSize(200, 40)
			if tc.room {
				a = a.showRoom()
			}

			m, cmd := typeAndSubmit(a, tc.draft)
			if cmd != nil {
				t.Fatalf("%q was acted on anyway: %+v", tc.draft, sentFrames(t, m.(App), cmd))
			}
			if got := shown(m.(App)); !strings.Contains(got, tc.says) {
				t.Errorf("%q was refused without saying %q:\n%s", tc.draft, tc.says, got)
			}
		})
	}
}

// A fleet report's Team folds onto the agent, the way Color does, so the roster
// and board can group by it.
func TestTheTeamOnAReportFoldsOntoTheAgent(t *testing.T) {
	f := (Fleet{}).WithStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "sydney", Team: "backend", State: rpc.StateIdle},
	}})
	got, ok := f.Agent("s1")
	if !ok {
		t.Fatal("Agent(s1) not found after WithStatus")
	}
	if got.Team != "backend" {
		t.Errorf("Agent.Team = %q, want %q: the report's team did not fold onto the agent", got.Team, "backend")
	}
}

// `@team` fans a room draft out to its live members - a scoped broadcast, not a
// single mention (so it does not narrow the room and does not bridge to a
// per-agent command) and not @all (it carries its own team marker).
func TestATeamDraftRoutesToItsMembersAndDoesNotNarrow(t *testing.T) {
	fresh(t)
	a := dmApp(newRecorder(t), Stream{}, "s1", "alex").withSize(200, 40).showRoom()
	a = a.applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
		{ID: "s1", Name: "alex", State: rpc.StateIdle},
		{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
		{ID: "s3", Name: "john", Team: "backend", State: rpc.StateIdle},
		{ID: "s4", Name: "delta", Team: "frontend", State: rpc.StateIdle},
	}})
	r := a.route("@backend ship it")
	if !slices.Equal(r.Targets, []string{"s2", "s3"}) {
		t.Errorf("targets = %v, want backend's members s2,s3", r.Targets)
	}
	if r.Team != "backend" {
		t.Errorf("route.Team = %q, want backend", r.Team)
	}
	if r.mentioned {
		t.Error("a team route is marked mentioned; retarget would narrow to one member and the bridge would fire")
	}
	if r.Text != "ship it" {
		t.Errorf("text = %q, want the @team stripped", r.Text)
	}
}

// Every Wake target-command aimed at a team is refused with the per-agent form,
// rather than reaching N claude processes as the literal text; claude's own
// command (`/compact`) fans out to the team as an ordinary N-way send.
func TestATeamTargetCommandIsRefusedButClaudesOwnFansOut(t *testing.T) {
	teamed := func(conn net.Conn) App {
		return dmApp(conn, Stream{}, "s1", "alex").withSize(200, 40).showRoom().
			applyStatus(&rpc.Status{Sessions: []rpc.SessionStatus{
				{ID: "s1", Name: "alex", State: rpc.StateIdle},
				{ID: "s2", Name: "thea", Team: "backend", State: rpc.StateIdle},
			}})
	}
	for _, draft := range []string{"@backend /color blue", "@backend /name x", "@backend /task ui", "@backend /quit"} {
		t.Run(draft, func(t *testing.T) {
			fresh(t)
			m, cmd := typeAndSubmit(teamed(newRecorder(t)), draft)
			if cmd != nil {
				t.Fatalf("%q was acted on: %+v", draft, sentFrames(t, m.(App), cmd))
			}
			if got := shown(m.(App)); !strings.Contains(got, "per-agent") {
				t.Errorf("%q was refused without naming the per-agent form:\n%s", draft, got)
			}
		})
	}
	t.Run("@backend /compact fans out", func(t *testing.T) {
		fresh(t)
		conn, sent := pipeClient(t)
		_, cmd := typeAndSubmit(teamed(conn), "@backend /compact")
		if cmd == nil {
			t.Fatal("@backend /compact was refused; claude's own command should fan out to the team")
		}
		go func() { _ = runCmdQuietly(cmd) }()
		if f := awaitFrame(t, sent); f.SessionID != "s2" {
			t.Errorf("/compact fanned to %q, want thea (s2), backend's live member", f.SessionID)
		}
	})
}

// The composer's target line names a team fan-out and its turn count, so the
// operator sees `→ @backend · 3 turns` before ↵ rather than reading it as @all.
func TestTheTargetLineShowsATeamFanout(t *testing.T) {
	r := roomRoute{Route: core.Route{Team: "backend", Resolved: "backend", Targets: []string{"a", "b", "c"}}}
	line := targetLine(r, 3)
	if !strings.Contains(line, agentPrefix+"backend") || !strings.Contains(line, "3") {
		t.Errorf("targetLine = %q, want it to name @backend and the turn count", line)
	}
}
