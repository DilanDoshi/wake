package mcp

// The grouping half: set_team and set_color, the two tools that let a manager
// group the fleet - assign a team, set an identity hue. Allowed on the owner's
// 2026-09-21 override of "the manager can send, interrupt and spawn, and nothing
// else"; the argument the reversal rests on is in cmd/wake/mcpguard_test.go.
//
// Like acting_test.go, every refusal is asserted on the *far side* - what the
// fleet was asked to do - because a refusal that reaches the daemon first is not
// a refusal, and the value is asserted raw because the daemon owns the fence.

import (
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestSetTeamGroupsAnAgentAndPassesTheTagThrough(t *testing.T) {
	f := onePeter()
	out := call(t, f, "set_team", map[string]any{agentIDArg: idPeter, teamArg: "backend"})

	if len(f.acts.teamed) != 1 {
		t.Fatalf("the daemon was asked to set %d teams, want 1: %+v", len(f.acts.teamed), f.acts.teamed)
	}
	if got := f.acts.teamed[0]; got.id != idPeter || got.value != "backend" {
		t.Errorf("set_team reached the daemon as %+v, want the id from list_agents and the tag unchanged", got)
	}
	if !strings.Contains(out, "backend") {
		t.Errorf("the confirmation does not name the team it set:\n%s", out)
	}
}

// The tag reaches the daemon raw, because rpc.NormalizeTeam on the far side is
// the one fence: a tool that lower-cased or trimmed here would be a second copy
// of a rule that already runs where the value is stored. "none" is the clear
// word, and it too is the daemon's to interpret.
func TestSetTeamPassesTheTagRawForTheDaemonToFence(t *testing.T) {
	for _, tag := range []string{"Backend", "  backend  ", rpc.TeamNone} {
		f := onePeter()
		call(t, f, "set_team", map[string]any{agentIDArg: idPeter, teamArg: tag})
		if len(f.acts.teamed) != 1 || f.acts.teamed[0].value != tag {
			t.Errorf("set_team edited %q to %+v; the daemon owns the fence, so the tag travels unchanged", tag, f.acts.teamed)
		}
	}
}

func TestSetColorSetsAnAgentAndPassesTheHueThrough(t *testing.T) {
	f := onePeter()
	out := call(t, f, "set_color", map[string]any{agentIDArg: idPeter, colorArg: "orange"})

	if len(f.acts.colored) != 1 {
		t.Fatalf("the daemon was asked to set %d colours, want 1: %+v", len(f.acts.colored), f.acts.colored)
	}
	if got := f.acts.colored[0]; got.id != idPeter || got.value != "orange" {
		t.Errorf("set_color reached the daemon as %+v, want the id from list_agents and the hue unchanged", got)
	}
	if !strings.Contains(out, "orange") {
		t.Errorf("the confirmation does not name the colour it set:\n%s", out)
	}
}

// The hue reaches the daemon raw, rpc.NormalizeColor's reason: it folds case and
// refuses anything outside rpc.ColorNames on the far side, so a wrong colour is
// the daemon's to refuse and a right one is the daemon's to fold.
func TestSetColorPassesTheHueRawForTheDaemonToFence(t *testing.T) {
	for _, hue := range []string{"Orange", "  blue  ", rpc.ColorNone} {
		f := onePeter()
		call(t, f, "set_color", map[string]any{agentIDArg: idPeter, colorArg: hue})
		if len(f.acts.colored) != 1 || f.acts.colored[0].value != hue {
			t.Errorf("set_color edited %q to %+v; the daemon owns the fence, so the hue travels unchanged", hue, f.acts.colored)
		}
	}
}

// A blank tag or hue is refused before the daemon is asked - not the same as
// "none", which is a value the daemon reads as clear. A missing or non-string
// argument is refused the same way, and each refusal names the argument that was
// wrong so a model does not go looking for a value it never sent.
func TestSetTeamAndSetColorRefuseABlankValueBeforeTheDaemonIsAsked(t *testing.T) {
	for _, c := range []struct {
		tool, arg string
	}{
		{"set_team", teamArg},
		{"set_color", colorArg},
	} {
		t.Run(c.tool, func(t *testing.T) {
			for _, blank := range []any{"", "   ", "\n\t ", 42, nil} {
				f := onePeter()
				args := map[string]any{agentIDArg: idPeter}
				if blank != nil {
					args[c.arg] = blank
				}
				_, err := callErr(t, f, c.tool, args)
				if err == nil {
					t.Errorf("%s accepted %#v as a value", c.tool, blank)
				}
				if err != nil && !strings.Contains(err.Error(), c.arg) {
					t.Errorf("%s refused %#v without naming %s: %v", c.tool, blank, c.arg, err)
				}
				if len(f.acts.teamed) != 0 || len(f.acts.colored) != 0 {
					t.Errorf("%s reached the daemon with a blank value: %+v %+v", c.tool, f.acts.teamed, f.acts.colored)
				}
			}
		})
	}
}

// Clearing reads as clearing, not as "set to none": the confirmation a manager
// reads back has to distinguish removing a tag from setting one, or a model
// believes it created a team called "none".
func TestClearingATeamOrColourReadsAsClearing(t *testing.T) {
	f := onePeter()
	if out := call(t, f, "set_team", map[string]any{agentIDArg: idPeter, teamArg: rpc.TeamNone}); strings.Contains(out, "under team") {
		t.Errorf("clearing a team read as setting one:\n%s", out)
	}
	f = onePeter()
	if out := call(t, f, "set_color", map[string]any{agentIDArg: idPeter, colorArg: rpc.ColorNone}); !strings.Contains(strings.ToLower(out), "clear") {
		t.Errorf("clearing a colour did not read as clearing:\n%s", out)
	}
}

// The two grouping tools are subject to every refusal the send tools are, and
// this is the registry that says so: a name never becomes an address, a session
// list_agents does not offer is refused before the daemon is asked, and the
// manager cannot group itself. Extended in the shared tables of acting_test.go;
// this one pins the property that is theirs alone - that a set reaches the wire
// only for a live agent.
func TestGroupingToolsRefuseANameAndNeverAskTheDaemon(t *testing.T) {
	for _, c := range []struct {
		tool, arg string
	}{
		{"set_team", teamArg},
		{"set_color", colorArg},
	} {
		t.Run(c.tool, func(t *testing.T) {
			lists := 0
			f := actingFleet(rpc.SessionStatus{ID: idPeter, Name: "peter", State: rpc.StateWorking})
			spy := spyFleet{lists: &lists, fleet: f}
			_, err := callErr(t, spy, c.tool, map[string]any{agentIDArg: "peter", c.arg: "x"})
			if err == nil {
				t.Fatalf("%s accepted a display name as an address", c.tool)
			}
			if lists != 0 {
				t.Errorf("%s asked the daemon %d times about a display name: refusing after the lookup is not the property", c.tool, lists)
			}
			if len(f.acts.teamed) != 0 || len(f.acts.colored) != 0 {
				t.Errorf("%s reached the daemon addressed by name: %+v %+v", c.tool, f.acts.teamed, f.acts.colored)
			}
		})
	}
}

// A daemon that refuses the fence - a reserved routing word, a colour not in the
// set, a parked session - is reported, never reported as success. This is the
// failure requireLive cannot see, so the tool leans on act's read-back the way
// send does.
func TestAGroupingToolThatIsRefusedByTheDaemonIsNotReportedAsDone(t *testing.T) {
	const want = `"all" is a reserved routing word, not a team name`
	f := onePeter()
	f.actErr = errFleet(want)
	if _, err := callErr(t, f, "set_team", map[string]any{agentIDArg: idPeter, teamArg: "all"}); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("set_team hid a daemon refusal: %v", err)
	}
	f = onePeter()
	f.actErr = errFleet("session is parked")
	if _, err := callErr(t, f, "set_color", map[string]any{agentIDArg: idPeter, colorArg: "blue"}); err == nil || !strings.Contains(err.Error(), "parked") {
		t.Errorf("set_color hid a daemon refusal: %v", err)
	}
}

// The manager may not group itself, the same reason it may not message itself:
// requireLive leaves its own row off the roster, so both grouping tools refuse
// the manager's own id.
func TestTheManagerCannotGroupItself(t *testing.T) {
	f := actingFleet(rpc.SessionStatus{ID: idMira, Name: core.ManagerName, State: rpc.StateIdle})
	for _, c := range []struct {
		tool, arg string
	}{
		{"set_team", teamArg},
		{"set_color", colorArg},
	} {
		if _, err := callErr(t, f, c.tool, map[string]any{agentIDArg: idMira, c.arg: "x"}); err == nil {
			t.Errorf("%s accepted the manager's own session", c.tool)
		}
	}
	if len(f.acts.teamed) != 0 || len(f.acts.colored) != 0 {
		t.Errorf("the manager grouped itself: %+v %+v", f.acts.teamed, f.acts.colored)
	}
}
