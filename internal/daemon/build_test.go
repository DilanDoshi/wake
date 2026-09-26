package daemon

import (
	"testing"

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

// RunningStatus asks only a daemon that is there: a stopped fleet answers
// "not running" off one failed dial, never off the on-disk sweep.
func TestRunningStatusAsksOnlyALiveDaemon(t *testing.T) {
	if _, running, err := RunningStatus(tempSocket(t)); running || err != nil {
		t.Errorf("a socket nothing listens on: running = %v, err = %v", running, err)
	}
	d := startDaemon(t)
	st, running, err := RunningStatus(d.socket)
	if err != nil || !running || st.Build != version.Build() {
		t.Errorf("a live daemon: build %q, running %v, err %v", st.Build, running, err)
	}
}
