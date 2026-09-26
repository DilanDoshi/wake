package daemon

import (
	"net"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/version"
)

// A client compares its own build against the one its daemon reports, so the
// report must carry the daemon's - the binary it is actually running, which an
// upgrade on disk does not change.
func TestTheFleetReportNamesTheDaemonsBuild(t *testing.T) {
	d := startDaemon(t)
	st, err := Status(d.socket)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Build != version.Build() {
		t.Errorf("report build = %q, want %q", st.Build, version.Build())
	}
}

// runningStatus asks only a daemon that is there: a stopped fleet answers
// "not running" off one failed dial, never off the on-disk sweep.
func TestRunningStatusAsksOnlyALiveDaemon(t *testing.T) {
	if _, running, err := runningStatus(tempSocket(t)); running || err != nil {
		t.Errorf("a socket nothing listens on: running = %v, err = %v", running, err)
	}
	d := startDaemon(t)
	st, running, err := runningStatus(d.socket)
	if err != nil || !running || st.Build != version.Build() {
		t.Errorf("a live daemon: build %q, running %v, err %v", st.Build, running, err)
	}
}

// RunningBuilds names the build of each fleet with a daemon up and leaves out
// the rest - and derives each socket itself, so a $WAKE_SOCKET naming one
// exact daemon does not stop a listing of all of them.
func TestRunningBuildsNamesOnlyTheFleetsThatAreUp(t *testing.T) {
	t.Setenv("HOME", tempHome(t))
	t.Setenv(SocketEnv, "")
	up, err := FleetSocketPath("canyon")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FleetSocketPath("mesa"); err != nil {
		t.Fatal(err)
	}
	startDaemonOn(t, up)
	t.Setenv(SocketEnv, tempSocket(t))

	got := RunningBuilds([]string{"canyon", "mesa"})
	if len(got) != 1 || got["canyon"] != version.Build() {
		t.Errorf("RunningBuilds = %v, want only canyon on %q", got, version.Build())
	}
}

// A daemon that takes the dial and never answers - mid-shutdown, or wedged -
// costs statusTimeout, so the fleets are asked together: three such fleets
// cost one timeout, not three.
func TestRunningBuildsAsksEveryFleetAtOnce(t *testing.T) {
	t.Setenv("HOME", tempHome(t))
	t.Setenv(SocketEnv, "")
	names := []string{"a", "b", "c"}
	for _, name := range names {
		sock, err := FleetSocketPath(name)
		if err != nil {
			t.Fatal(err)
		}
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				t.Cleanup(func() { _ = conn.Close() })
			}
		}()
	}
	start := time.Now()
	if got := RunningBuilds(names); len(got) != 0 {
		t.Errorf("silent daemons reported builds: %v", got)
	}
	if took := time.Since(start); took >= 2*statusTimeout {
		t.Errorf("three silent fleets took %v; asked together they cost one %v", took, statusTimeout)
	}
}
