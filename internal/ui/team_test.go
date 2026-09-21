package ui

import (
	"strings"
	"testing"

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
