//go:build unix

// Sweeping a session's process group when it ends cleanly, and the one ending
// that must not be swept.
//
// Both of core's kill paths are failure paths - the scan giving up, the ctx
// being cancelled - so an agent that finishes normally after `npm run dev &`
// used to leave the dev server behind. The sweep lives in the daemon's retire
// rather than in core because only the pool can tell a park's clean exit (its
// children are the woken session's world) from an ordinary one. These are
// Unix-only because the observation is: they ask the OS whether a specific pid
// is still alive after the session ends, which is the only thing that would
// have caught the leak.

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// A clean, non-park ending sweeps the group, so what the agent spawned dies
// with it. The grandchild stays in the agent's group and holds none of Wake's
// pipes - so the agent still exits on EOF, and only retire's KillGroup reaches
// the child. Before the fix, retire's ordinary branch swept nothing and this
// child outlived the session that started it.
func TestACleanExitSweepsTheProcessGroupTheAgentLeftBehind(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("WAKE_FAKE_PIDFILE", pidPath)
	fakeClaudeOnPath(t, "leak")

	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	child := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if !processAlive(child) {
		t.Fatal("the child the agent spawned was not running before the session ended, so this test proves nothing")
	}

	// A stop, not a park: closing stdin ends the fake on EOF, and retire takes
	// the ordinary branch rather than completePark.
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: idAlpha})
	c.pollState(idAlpha, rpc.StateEnded)

	waitForProcessGone(t, child)
}

// `wake stop` leaves nothing of the fleet running: every agent, what each left
// in its process group, and the daemon (BUG-25's open question, asked of the
// stop). Two agents, so the sweep is shown per agent rather than for one lucky
// group; each group is asked of the OS whole, so a child nobody recorded counts.
func TestAStopLeavesNothingOfTheFleetRunning(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "children.pid")
	t.Setenv("WAKE_FAKE_PIDFILE", pidPath)
	fakeClaudeOnPath(t, "leak")

	d := startDaemon(t)
	c := attach(t, d.socket)
	for id, name := range map[string]string{idAlpha: "alex", idBeta: "bo"} {
		c.spawn(id, name)
		c.awaitEvent(id, "ready")
	}
	children := waitForPids(t, pidPath, 2)
	var groups []int
	for _, s := range c.status().Sessions {
		groups = append(groups, s.PID)
	}
	t.Cleanup(func() {
		// Only what is still alive, and never 0 or 1: kill(0) is this test's own group.
		for _, pid := range append(children, groups...) {
			if pid > 1 && processAlive(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	if len(groups) != 2 {
		t.Fatalf("the fleet reports process groups %v, want one per agent", groups)
	}
	for _, pid := range children {
		if !processAlive(pid) {
			t.Fatal("a child an agent spawned was not running before the stop, so this test proves nothing")
		}
	}

	c.send(rpc.Frame{Kind: rpc.FrameQuit})
	if !d.waitForExitWithin(t, testTimeout) {
		t.Fatal("the daemon was still running after the stop")
	}
	for _, pgid := range groups {
		if err := syscall.Kill(-pgid, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("process group %d still has a member after the stop (kill -0: %v)", pgid, err)
		}
	}
	for _, pid := range children {
		if processAlive(pid) {
			t.Errorf("child %d outlived the stop of the fleet that started it", pid)
		}
	}
}

// waitForPids reads n pids the agents recorded for their children, a line each.
func waitForPids(t *testing.T, path string, n int) []int {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for {
		var pids []int
		if b, err := os.ReadFile(path); err == nil {
			for _, f := range strings.Fields(string(b)) {
				if pid, err := strconv.Atoi(f); err == nil && pid > 0 {
					pids = append(pids, pid)
				}
			}
		}
		if len(pids) >= n {
			return pids
		}
		if time.Now().After(deadline) {
			t.Fatalf("the agents recorded %v of the %d children they spawned (%s)", pids, n, path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// The mirror, and the one that must not be "fixed" into symmetry: a parked
// session comes back, and the world it spawned - the dev server it will resume
// against - has to be there when it does. So completePark sweeps nothing, and a
// child left in the group is still alive after the park settles. This is the
// daemon analog of core's TestStopEndsTheAgentWithoutKillingWhatItSpawned.
func TestParkingDoesNotSweepTheProcessGroup(t *testing.T) {
	pidPath := filepath.Join(t.TempDir(), "child.pid")
	t.Setenv("WAKE_FAKE_PIDFILE", pidPath)
	fakeClaudeOnPath(t, "leak")

	d := startDaemon(t)
	c := attach(t, d.socket)
	c.spawn(idAlpha, "alex")
	c.awaitEvent(idAlpha, "ready")

	child := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(child, syscall.SIGKILL) })
	if !processAlive(child) {
		t.Fatal("the child the agent spawned was not running before the park, so this test proves nothing")
	}

	c.send(rpc.Frame{Kind: rpc.FramePark, SessionID: idAlpha})
	// A push, so completePark has fully run by the time this returns - any sweep
	// wrongly placed there would already have fired.
	c.awaitState(idAlpha, rpc.StateParked)

	if !processAlive(child) {
		t.Error("parking swept the process group: a woken session needs the world it spawned, so the sweep belongs only on the ordinary ending")
	}
}

// waitForPid reads the pid the agent recorded for its child.
func waitForPid(t *testing.T, path string) int {
	t.Helper()
	return waitForPids(t, path, 1)[0]
}
