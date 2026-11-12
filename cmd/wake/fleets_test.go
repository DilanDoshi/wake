package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// The flag comes off wherever it appears, and what is left is the verb and its
// arguments unchanged.
//
// It is legal on every verb rather than only the spawning ones, which is why it
// is parsed here and not in spawnFlags: `wake stop --fleet x` is a sentence and
// `wake stop --effort max` is not. So the thing to check is that taking it out
// leaves an argument list every other parser still recognises.
func TestTheFleetFlagComesOffAnywhereAndLeavesTheVerbAlone(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		rest  []string
		fleet string
	}{
		{"no flag at all", []string{"status"}, []string{"status"}, ""},
		{"before the verb", []string{"--fleet", "api", "status"}, []string{"status"}, "api"},
		{"after the verb", []string{"status", "--fleet", "api"}, []string{"status"}, "api"},
		{"between a verb and its argument", []string{"attach", "--fleet", "api", "alex"}, []string{"attach", "alex"}, "api"},
		{"beside a spawn flag", []string{"new", "--fleet", "api", "--effort", "max"}, []string{"new", "--effort", "max"}, "api"},
		{"nothing but the flag", []string{"--fleet", "api"}, []string{}, "api"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rest, fleet, err := fleetFlag(tc.args)
			if err != nil {
				t.Fatalf("fleetFlag(%v): %v", tc.args, err)
			}
			if fleet != tc.fleet {
				t.Errorf("fleet = %q, want %q", fleet, tc.fleet)
			}
			if strings.Join(rest, " ") != strings.Join(tc.rest, " ") {
				t.Errorf("rest = %v, want %v", rest, tc.rest)
			}
		})
	}
}

// The three ways of getting it wrong are three different sentences.
//
// A missing value is the one that matters: `wake --fleet --effort max` would
// otherwise create a fleet called `--effort`, which is a directory nobody meant
// and which then shows up in `wake fleets` forever.
func TestTheFleetFlagRefusesWhatIsNotAName(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		says string
	}{
		{"no value", []string{"--fleet"}, "needs a name"},
		{"a flag as the value", []string{"--fleet", "--effort", "max"}, "rather than a fleet name"},
		{"given twice", []string{"--fleet", "a", "--fleet", "b"}, "twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := fleetFlag(tc.args)
			if err == nil {
				t.Fatalf("fleetFlag(%v) was accepted", tc.args)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("refused with %q, which does not say %q", err, tc.says)
			}
		})
	}
}

// A machine with no named fleets says so, and says how to make one.
//
// "none" is a complete answer to the question asked and a useless one to the
// person asking it: somebody typing this verb has either just made a fleet or is
// about to.
func TestFleetsSaysHowToMakeOneWhenThereAreNone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var out bytes.Buffer
	if err := printFleets(&out); err != nil {
		t.Fatalf("printFleets: %v", err)
	}
	if !strings.Contains(out.String(), rpc.FleetFlag) {
		t.Errorf("an empty listing does not name %s, so it says nothing about how to make one:\n%s",
			rpc.FleetFlag, out.String())
	}
}

// Which invocations start a fleet nobody named, and which do not.
//
// Driven through makesNewFleet, which is the expression run uses rather than a
// copy of it: the thing that broke here was not the condition but nobody having
// asked the question, and a restatement would have been just as green.
func TestOnlyABareWakeWithNoSocketStartsANewFleet(t *testing.T) {
	for _, tc := range []struct {
		what   string
		args   []string
		fleet  string
		socket string
		want   bool
	}{
		{"a bare wake", nil, "", "", true},
		{"a bare wake under $WAKE_SOCKET", nil, "", "/tmp/x.sock", false},
		{"a bare wake with a fleet named", nil, "backend", "", false},
		{"a verb", []string{"status"}, "", "", false},
		{"a verb with a fleet named", []string{"status"}, "backend", "", false},
		{"the fleets listing", []string{"fleets"}, "", "", false},
	} {
		t.Run(tc.what, func(t *testing.T) {
			if got := makesNewFleet(tc.args, tc.fleet, tc.socket); got != tc.want {
				t.Errorf("makesNewFleet(%v, %q, %q) = %v, want %v", tc.args, tc.fleet, tc.socket, got, tc.want)
			}
		})
	}
}

// Each fleet says what it is: a running one its agents and build (and how to
// move one on another build than this wake), a stopped one what it holds and
// the command that deals with it. Commands are rendered as every other line.
func TestFleetsSayWhatEachFleetIs(t *testing.T) {
	const ours = "0.1.6+bbbbbbb"
	live := func(build string, n int) daemon.FleetState {
		st := rpc.Status{Running: true, Build: build}
		for range n {
			st.Sessions = append(st.Sessions, rpc.SessionStatus{State: rpc.StateIdle})
		}
		return daemon.FleetState{Report: &st}
	}
	stopped := func(left daemon.FleetLeft) daemon.FleetState { return daemon.FleetState{Left: left} }
	got := formatFleets([]string{"default", "atlas", "basin", "bayou", "canyon", "cove", "harbor", "reef", "torn", "wedged"},
		map[string]daemon.FleetState{
			"default": stopped(daemon.FleetLeft{Parked: []string{"alex", "manager"}}),
			"atlas":   stopped(daemon.FleetLeft{Parked: []string{"manager"}}),
			"basin":   stopped(daemon.FleetLeft{Parked: []string{"a", "b", "c"}}),
			"bayou":   stopped(daemon.FleetLeft{Kept: []string{"debug/"}}),
			"canyon":  stopped(daemon.FleetLeft{Crashed: true, Parked: []string{"a"}}),
			"cove":    stopped(daemon.FleetLeft{}),
			"harbor":  live("0.1.5+aaaaaaa", 12),
			"reef":    live(ours, 1),
			"torn":    stopped(daemon.FleetLeft{Err: errors.Join(errTest, errTest)}),
			"wedged":  {Silent: true},
		}, ours)
	want := []string{
		"default  parked · 2 agents",
		"atlas    parked · the manager",
		"basin    parked · 3 agents",
		"bayou    stopped · keeps debug/, which Wake does not remove",
		"canyon   crashed · `wake --fleet canyon` recovers its sessions",
		"cove     stopped · `wake stop --fleet cove` removes it",
		"harbor   running · 12 agents · wake 0.1.5+aaaaaaa - not this build (wake 0.1.6+bbbbbbb): ⌃Q⌃Q it, then `wake --fleet harbor`, to upgrade it",
		"reef     running · 1 agent · wake 0.1.6+bbbbbbb",
		"torn     unreadable · " + errTest.Error() + "; " + errTest.Error(),
		"wedged   running · not answering (stopping, or wedged)",
	}
	if lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n"); strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("listing:\n%s\nwant:\n%s", got, strings.Join(want, "\n"))
	}
}

// The unnamed fleet is ~/.wake itself, which no stop removes, so a stopped one
// names no command.
func TestTheUnnamedFleetOffersNoRemoval(t *testing.T) {
	got := formatFleets([]string{"default"}, map[string]daemon.FleetState{"default": {}}, "x")
	if got != "default  stopped · ~/.wake itself, which no stop removes\n" {
		t.Errorf("a stopped unnamed fleet is listed %q", got)
	}
}
