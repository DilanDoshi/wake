//go:build unix

package daemon

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// shortPeersDeadline compresses how long one run may take.
func shortPeersDeadline(t *testing.T, d time.Duration) {
	t.Helper()
	prev := peersDeadline
	peersDeadline = d
	t.Cleanup(func() { peersDeadline = prev })
}

// hungOneShot fakes a one-shot that never answers and says where its pid goes.
func hungOneShot(t *testing.T, script string) string {
	t.Helper()
	oneShotOnPath(t, script, oneShotHang)
	pidPath := filepath.Join(t.TempDir(), "oneshot.pid")
	t.Setenv(fakeOneShotPIDEnv, pidPath)
	return pidPath
}

// A run that hangs is ended at the deadline - the child killed, not left
// running - and answers empty, so it cannot wedge the asks after it.
func TestAHungOneShotIsKilledAtTheDeadlineAndAnswersEmpty(t *testing.T) {
	pidPath := hungOneShot(t, "")
	deadline := 2 * time.Second
	shortPeersDeadline(t, deadline)
	s := newServer(tempSocket(t))

	start := time.Now()
	peers := s.listPeers(t.Context())
	took := time.Since(start)

	pid := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	if peers != nil {
		t.Errorf("a hung run answered %+v", peers)
	}
	if took < deadline || took >= defaultPeersDeadline {
		t.Errorf("the run took %v, want the injected deadline %v", took, deadline)
	}
	if processAlive(pid) {
		t.Error("the hung one-shot outlived its deadline")
	}
}

// A daemon shutting down ends a run in flight rather than waiting out its
// deadline: nothing it started - goroutine or child - survives it. Quit rather
// than a cancelled context, so it is the daemon's own ending that reaches the
// run, not the caller's.
func TestShutdownMidRunLeavesNothingRunning(t *testing.T) {
	pidPath := hungOneShot(t, "advertises")
	base := settledGoroutines()
	d := startDaemon(t)
	c := attach(t, d.socket)
	advertisingAgent(t, c)

	c.send(rpc.Frame{Kind: rpc.FramePeers})
	pid := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	d.waitForExit(t)
	c.close()
	if processAlive(pid) {
		t.Error("the one-shot outlived the daemon that started it")
	}
	waitForGoroutines(t, base)
}
