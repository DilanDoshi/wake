package daemon

import (
	"net"
	"os"
	"path/filepath"
	"strings"
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

// A fleet whose daemon answers is listed by its report, and any other by what
// its files hold - and each socket is derived from the name, so a $WAKE_SOCKET
// naming one exact daemon does not stop a listing of all of them.
func TestFleetStatesAskTheRunningAndReadTheStopped(t *testing.T) {
	t.Setenv("HOME", tempHome(t))
	t.Setenv(SocketEnv, "")
	up, err := FleetSocketPath("canyon")
	if err != nil {
		t.Fatal(err)
	}
	down, err := FleetSocketPath("mesa")
	if err != nil {
		t.Fatal(err)
	}
	for _, sock := range []string{up, down} {
		if err := os.MkdirAll(filepath.Dir(sock), stateDirPerm); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(parkBookPath(down), []byte(`[{"id":"a","name":"alex"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	startDaemonOn(t, up)
	t.Setenv(SocketEnv, tempSocket(t))

	got := FleetStates([]string{"canyon", "mesa"})
	if r := got["canyon"].Report; r == nil || r.Build != version.Build() {
		t.Errorf("canyon's report = %+v, want its build %q", r, version.Build())
	}
	if m := got["mesa"]; m.Report != nil || strings.Join(m.Left.Parked, ",") != "alex" {
		t.Errorf("mesa = %+v, want no report and alex parked", m)
	}
}

// A daemon that takes the dial and never answers - mid-shutdown, or wedged -
// costs statusTimeout, so the fleets are asked together: three such fleets
// cost one timeout, not three. Each is listed as a daemon that did not answer,
// never read off its files as a crash or a stopped fleet.
func TestFleetStatesAskEveryFleetAtOnce(t *testing.T) {
	t.Setenv("HOME", tempHome(t))
	t.Setenv(SocketEnv, "")
	names := []string{"a", "b", "c"}
	for _, name := range names {
		sock, err := FleetSocketPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(sock), stateDirPerm); err != nil {
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
	for name, st := range FleetStates(names) {
		if st.Report != nil || !st.Silent {
			t.Errorf("silent daemon %s is listed %+v, want no report and silent", name, st)
		}
	}
	if took := time.Since(start); took >= 2*statusTimeout {
		t.Errorf("three silent fleets took %v; asked together they cost one %v", took, statusTimeout)
	}
}
