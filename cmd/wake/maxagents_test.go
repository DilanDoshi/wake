package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
)

func TestTheMaxAgentsFlagComesOffAnywhere(t *testing.T) {
	for _, tc := range []struct {
		args []string
		rest []string
		n    int
	}{
		{[]string{"--max-agents", "3", "new"}, []string{"new"}, 3},
		{[]string{"new", "sydney", "--max-agents", "50"}, []string{"new", "sydney"}, 50},
		{[]string{"new", "sydney"}, []string{"new", "sydney"}, 0},
		{nil, []string{}, 0},
	} {
		rest, n, err := maxAgentsFlag(tc.args)
		if err != nil || n != tc.n || !slices.Equal(rest, tc.rest) {
			t.Errorf("maxAgentsFlag(%q) = %q, %d, %v; want %q, %d", tc.args, rest, n, err, tc.rest, tc.n)
		}
	}
}

func TestTheMaxAgentsFlagRefusesWhatIsNotACap(t *testing.T) {
	for _, args := range [][]string{
		{"--max-agents"},
		{"--max-agents", "many"},
		{"--max-agents", "0"},
		{"--max-agents", "51"},
		{"--max-agents", "-2"},
		{"--max-agents", "3", "--max-agents", "4"},
	} {
		if _, _, err := maxAgentsFlag(args); err == nil || !strings.Contains(err.Error(), maxAgentsFlagName) {
			t.Errorf("maxAgentsFlag(%q) = %v, want a refusal naming the flag", args, err)
		}
	}
}

// The flag sets a fleet's cap, so it goes with a command that opens one.
func TestTheMaxAgentsFlagGoesOnlyWithAVerbThatOpensAFleet(t *testing.T) {
	t.Setenv(daemon.SocketEnv, tempSocket(t))
	for _, args := range [][]string{
		{"status", "--max-agents", "3"},
		{"stop", "--max-agents", "3"},
		{"fleets", "--max-agents", "3"},
		{"fork", "sydney", "--max-agents", "3"},
		{"import", "--max-agents", "3"},
	} {
		err := run(args, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), maxAgentsFlagName) {
			t.Errorf("run(%q) = %v, want the flag refused", args, err)
		}
	}
}

// run takes the cap off the command line for a verb that opens the fleet - here
// `attach` to nobody, which then fails on its own account - and every run sets
// it afresh.
func TestRunTakesTheCapForAVerbThatOpensTheFleet(t *testing.T) {
	t.Setenv(daemon.SocketEnv, tempSocket(t))
	t.Cleanup(func() { fleetMaxAgents = 0 })
	_ = run([]string{"attach", "nobody", "--max-agents", "4"}, &bytes.Buffer{})
	if fleetMaxAgents != 4 {
		t.Fatalf("run left the cap at %d, want 4", fleetMaxAgents)
	}
	_ = run([]string{"attach", "nobody"}, &bytes.Buffer{})
	if fleetMaxAgents != 0 {
		t.Errorf("a run without the flag kept the last one's cap: %d", fleetMaxAgents)
	}
}

// connect hands the cap to the daemon on the verb's own connection.
func TestConnectSetsTheFleetsCap(t *testing.T) {
	d := startRealDaemon(t)
	fleetMaxAgents = 4
	t.Cleanup(func() { fleetMaxAgents = 0 })
	conn, _, err := connect(d.socket, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var st rpc.Status
	for range 50 {
		if st, err = daemon.Status(d.socket); err == nil && st.MaxAgents == 4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || st.MaxAgents != 4 {
		t.Errorf("after --max-agents 4 the fleet's cap is %d (%v)", st.MaxAgents, err)
	}
}

func TestStatusSaysHowManyOfTheCapAreRunning(t *testing.T) {
	got := formatStatus(rpc.Status{Running: true, MaxAgents: 50, Sessions: []rpc.SessionStatus{
		{ID: "a", Name: "sydney", State: rpc.StateIdle},
		{ID: "b", Name: "alex", State: rpc.StateParked},
		{ID: "c", Name: "jo", State: rpc.StateEnded},
	}}, "")
	if !strings.Contains(got, "1 of 50 sessions running") {
		t.Errorf("status does not say how many of the cap are running:\n%s", got)
	}
}

// The cap is sent once per command. A reconnect does not send it again, so it
// cannot put back a cap another window has since changed.
func TestAReconnectDoesNotResendTheCap(t *testing.T) {
	d := startRealDaemon(t)
	fleetMaxAgents = 4
	t.Cleanup(func() { fleetMaxAgents = 0 })
	first, _, err := connect(d.socket, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = first.Close() }()
	if fleetMaxAgents != 0 {
		t.Fatalf("the cap was not spent by the connect that sent it: %d", fleetMaxAgents)
	}
	if err := rpc.WriteFrameTo(first, rpc.Frame{Kind: rpc.FrameSetMaxAgents, MaxAgents: 9}); err != nil {
		t.Fatalf("another window's cap: %v", err)
	}
	again, _, err := connect(d.socket, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer func() { _ = again.Close() }()
	var st rpc.Status
	for range 50 {
		if st, err = daemon.Status(d.socket); err == nil && st.MaxAgents == 9 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("after a reconnect the cap is %d, want the other window's 9 kept", st.MaxAgents)
}

// One is a session, not "1 sessions".
func TestACapOfOneSaysSession(t *testing.T) {
	d := startRealDaemon(t)
	notice.Reset()
	t.Cleanup(notice.Reset)
	fleetMaxAgents = 1
	t.Cleanup(func() { fleetMaxAgents = 0 })
	conn, _, err := connect(d.socket, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if n, ok := notice.Latest(); !ok || !strings.Contains(n.String(), "up to 1 session at once") {
		t.Errorf("the notice for a cap of 1 reads %v", n)
	}
}
