package main

// `wake status`'s team field and its --team filter: how an agent, which has
// `wake` on its PATH and no fleet view of its own, sees who is on its team.

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

var idGamma = testSessionID("c33c")

// teamFleet is a report with two teams, an agent on neither, the manager, and a
// fork whose parent is on the other team.
func teamFleet() rpc.Status {
	return rpc.Status{
		Running: true, PID: 7, Socket: "/tmp/s", Teams: []string{"backend", "docs"},
		Sessions: []rpc.SessionStatus{
			{ID: idAlpha, Name: "sydney", State: rpc.StateIdle, Team: "backend"},
			{ID: idBeta, Name: "alex", State: rpc.StateWorking, Team: "docs"},
			{ID: idGamma, Name: "riley", State: rpc.StateIdle, Team: "backend", ParentID: idBeta},
			{ID: testSessionID("d44d"), Name: "manager", State: rpc.StateIdle},
			{ID: testSessionID("e55e"), Name: "pat", State: rpc.StateIdle},
		},
	}
}

// The field sits straight after the id column, so it starts at one offset on
// every row that has one, and a row without a team is the row it always was.
func TestAStatusRowNamesItsTeamAfterTheIDColumn(t *testing.T) {
	withTeam := sessionLine(rpc.SessionStatus{ID: "abcdef1234", Name: "alex", State: rpc.StateIdle, Team: "backend"}, nil)
	want := fmt.Sprintf("  %-*s %-*s %-*s  team backend\n", titleColumn, "alex", stateColumn, rpc.StateIdle, idColumn, "abcdef12")
	if withTeam != want {
		t.Errorf("a row on a team = %q, want %q", withTeam, want)
	}

	without := sessionLine(rpc.SessionStatus{ID: "abcdef1234", Name: "alex", State: rpc.StateIdle}, nil)
	if strings.Contains(without, "team") {
		t.Errorf("a row with no team mentions one: %q", without)
	}
	wantBare := strings.TrimRight(fmt.Sprintf("  %-*s %-*s %-*s", titleColumn, "alex", stateColumn, rpc.StateIdle, idColumn, "abcdef12"), " ") + "\n"
	if without != wantBare {
		t.Errorf("a row with no team = %q, want it unchanged: %q", without, wantBare)
	}
}

// The team field comes before what a state owes, so `quiet` and `waiting on`
// still read as the tail of the row.
func TestTheTeamFieldPrecedesWhatAStateOwes(t *testing.T) {
	got := sessionLine(rpc.SessionStatus{ID: "abcdef1234", Name: "alex", State: rpc.StateSilent, Team: "docs", QuietMS: 90_000}, nil)
	if i, j := strings.Index(got, "team docs"), strings.Index(got, "quiet 1m30s"); i < 0 || j < 0 || i > j {
		t.Errorf("row = %q, want `team docs` before `quiet 1m30s`", got)
	}
}

func TestTheFilterKeepsOnlyTheTeamsMembers(t *testing.T) {
	got := formatStatus(teamFleet(), "backend")
	for _, want := range []string{"wake daemon running", "sydney", "riley", "team backend"} {
		if !strings.Contains(got, want) {
			t.Errorf("the backend listing is missing %q:\n%s", want, got)
		}
	}
	// Rows, not substrings: riley's row names alex as the fork's parent.
	for _, unwanted := range []string{"alex", "manager", "pat"} {
		if strings.Contains(got, "\n  "+unwanted+" ") {
			t.Errorf("the backend listing has a row for %q, who is not on it:\n%s", unwanted, got)
		}
	}
	if strings.Contains(got, "team docs") {
		t.Errorf("the backend listing holds a docs row:\n%s", got)
	}
}

// A name is resolved from the whole report, not the filtered one: the fork's
// parent is on another team and the row still says who it was forked from.
func TestAFilteredRowStillNamesAParentOnAnotherTeam(t *testing.T) {
	if got := formatStatus(teamFleet(), "backend"); !strings.Contains(got, "forked from alex") {
		t.Errorf("the filter lost the parent's name:\n%s", got)
	}
}

func TestNoFilterListsEveryone(t *testing.T) {
	got := formatStatus(teamFleet(), "")
	for _, want := range []string{"sydney", "alex", "riley", "manager", "pat"} {
		if !strings.Contains(got, want) {
			t.Errorf("the unfiltered listing is missing %q:\n%s", want, got)
		}
	}
}

func TestAFilterWithNoMembersSaysSoAndNamesTheTeamsThatExist(t *testing.T) {
	got := formatStatus(teamFleet(), "ops")
	if !strings.Contains(got, "No sessions on team ops.") {
		t.Errorf("an empty team reads as %q", got)
	}
	if !strings.Contains(got, "backend, docs") {
		t.Errorf("an empty team does not name the teams there are:\n%s", got)
	}

	noTeams := teamFleet()
	noTeams.Teams = nil
	if got := formatStatus(noTeams, "ops"); strings.Contains(got, "Teams") {
		t.Errorf("a report with no teams names some:\n%s", got)
	}
}

// The filter narrows rows. It does not change what a missing daemon says.
func TestAFilterDoesNotHideThatNoDaemonIsRunning(t *testing.T) {
	if got := formatStatus(rpc.Status{}, "backend"); got != "No daemon is running.\n" {
		t.Errorf("formatStatus = %q", got)
	}
}

func TestTheStatusFlagsTable(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantRest []string
		wantTeam string
		wantErr  string
	}{
		{name: "no flag", args: []string{"status"}, wantRest: []string{"status"}},
		{name: "a team", args: []string{"status", "--team", "backend"}, wantRest: []string{"status"}, wantTeam: "backend"},
		{name: "folded to lower case", args: []string{"status", "--team", "Backend"}, wantRest: []string{"status"}, wantTeam: "backend"},
		{name: "before the verb's other words", args: []string{"status", "--team", "docs", "extra"}, wantRest: []string{"status", "extra"}, wantTeam: "docs"},
		{name: "missing", args: []string{"status", "--team"}, wantErr: "needs a team name"},
		{name: "a flag where the name goes", args: []string{"status", "--team", "--fleet"}, wantErr: "dash"},
		{name: "a team cannot start with a dash", args: []string{"status", "--team", "-ops"}, wantErr: "dash"},
		{name: "twice", args: []string{"status", "--team", "a", "--team", "b"}, wantErr: "given twice"},
		{name: "none is not a team to list", args: []string{"status", "--team", "none"}, wantErr: "clears a team"},
		{name: "not a team name", args: []string{"status", "--team", "back end"}, wantErr: "cannot be a team name"},
		{name: "on a verb that lists nothing", args: []string{"new", "--team", "backend"}, wantErr: "filters `wake status`"},
		{name: "another verb without it", args: []string{"attach", "sydney"}, wantRest: []string{"attach", "sydney"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rest, team, err := statusFlags(tc.args)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("statusFlags(%v) error = %v, want one containing %q", tc.args, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("statusFlags(%v): %v", tc.args, err)
			}
			if !slices.Equal(rest, tc.wantRest) || team != tc.wantTeam {
				t.Errorf("statusFlags(%v) = %v, %q; want %v, %q", tc.args, rest, team, tc.wantRest, tc.wantTeam)
			}
		})
	}
}

// One grammar: whatever /team accepts, the filter the fleet note teaches can
// list.
func TestTheFilterTakesEveryNameTheTeamFenceAccepts(t *testing.T) {
	for _, name := range []string{"backend", "ops-", "_x", "a-b_c", "007", strings.Repeat("x", 32)} {
		want, err := rpc.NormalizeTeam(name)
		if err != nil || want == "" {
			t.Fatalf("%q is not a team name the fence accepts, so this case asserts nothing: %q, %v", name, want, err)
		}
		if _, got, err := statusFlags([]string{"status", rpc.TeamFilterFlag, name}); err != nil || got != want {
			t.Errorf("`wake status --team %s` = %q, %v; want the team %q /team accepts", name, got, err, want)
		}
	}
}

// The parsers that run before statusFlags (the fleet flag, every spawn flag) take
// a word that is theirs wherever it stands, so a team named like one could never
// be listed. The fence closes that for every spelling at once: each is a dash
// word, and none may be a team.
func TestNoFlagThisBinaryStripsIsATeamName(t *testing.T) {
	flags := []string{fleetFlagName, rpc.TeamFilterFlag}
	for _, f := range knownFlags {
		flags = append(flags, f.name)
	}
	for _, f := range flags {
		if got, err := rpc.NormalizeTeam(f); err == nil {
			t.Errorf("NormalizeTeam(%q) = %q: a team named like a flag cannot be given to `wake status --team`", f, got)
		}
	}
}

// The flag the fleet note teaches agents is the one status takes.
func TestStatusTakesTheFlagTheFleetNoteNames(t *testing.T) {
	if _, team, err := statusFlags([]string{"status", rpc.TeamFilterFlag, "backend"}); err != nil || team != "backend" {
		t.Errorf("statusFlags(status %s backend) = %q, %v", rpc.TeamFilterFlag, team, err)
	}
}

// Through run: the flag comes off before the arity check, which would
// otherwise refuse `status` for taking arguments.
func TestWakeStatusTeamReachesTheFilter(t *testing.T) {
	d := startRealDaemon(t)
	t.Setenv(daemon.SocketEnv, d.socket)

	var out bytes.Buffer
	if err := run([]string{cmdStatus, rpc.TeamFilterFlag, "backend"}, &out); err != nil {
		t.Fatalf("wake status --team backend: %v", err)
	}
	if !strings.Contains(out.String(), "No sessions on team backend.") {
		t.Errorf("wake status --team backend printed %q", out.String())
	}

	if err := run([]string{cmdNew, rpc.TeamFilterFlag, "backend"}, &out); err == nil || !strings.Contains(err.Error(), "filters `wake status`") {
		t.Errorf("wake new --team backend = %v, want a refusal naming wake status", err)
	}
}

// A team read off a hand-edited roster is not trusted into the terminal.
func TestAHandEditedTeamCannotDriveTheTerminal(t *testing.T) {
	got := sessionLine(rpc.SessionStatus{ID: idAlpha, Name: "sydney", State: rpc.StateIdle, Team: "back\x1b[2Jend\u009b2J"}, nil)
	for _, r := range strings.TrimSuffix(got, "\n") {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Errorf("the status row kept %#x: %q", r, got)
		}
	}
}
