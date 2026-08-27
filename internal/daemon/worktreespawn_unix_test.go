//go:build unix

// BUG-23: a worktree spawn's `git worktree add` must not hang the daemon, in
// either of the two ways it could. The command itself is bounded - a deadline
// that reaps the whole tree, and a WaitDelay for a pipe a post-checkout hook's
// backgrounded child holds open past git's exit - and the spawn that runs it is
// moved off serveClient's in-order dispatch goroutine, so a slow one does not
// hold the same client's kill, interrupt and answer frames behind it.
//
// Unix because all three lean on the shell and on process groups: a hook that
// backgrounds a process, and a `syscall.Kill` to ask whether a pid still lives.

package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// writeHook installs a post-checkout hook in a repository. `git worktree add`
// runs it in the new worktree, which is the seam these tests use to make git
// misbehave on demand.
func writeHook(t *testing.T, repo, body string) {
	t.Helper()
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.MkdirAll(filepath.Dir(hook), 0o755); err != nil {
		t.Fatalf("make hooks dir: %v", err)
	}
	if err := os.WriteFile(hook, []byte(body), 0o755); err != nil {
		t.Fatalf("write post-checkout hook: %v", err)
	}
}

// recordedPid reads the pid a hook wrote, waiting for it to appear - the hook
// records it before it blocks, so this returns while git is still hung.
func recordedPid(t *testing.T, pidFile string) int {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 1 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the post-checkout hook never recorded a pid at %s", pidFile)
	return 0
}

// killRecorded ends whatever a hook backgrounded, for cleanup. Killing it also
// closes the pipe it held, so any addWorktree still blocked on cmd.Output
// unblocks - which is how a test that timed out does not leave a goroutine
// wedged past its own end.
func killRecorded(pidFile string) {
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// waitForDeath reports whether a pid stops existing within d.
func waitForDeath(pid int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !processAlive(pid)
}

// shortGitTimeout drives the git deadline down so a test can reach the
// cancel path without waiting two minutes. gitTimeout is a var for this, the
// liveCap seam; nothing runs a daemon concurrently in this package, so writing
// it here is safe.
func shortGitTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := gitTimeout
	gitTimeout = d
	t.Cleanup(func() { gitTimeout = prev })
}

// TestAWorktreeAddThatBackgroundsAProcessDoesNotHang pins the WaitDelay.
//
// The post-checkout hook backgrounds a sleep that inherits git's stdout and
// outlives git's own exit. Without WaitDelay, os/exec's copy goroutine waits
// for that pipe to close and cmd.Output never returns - the exact hang the bug
// records. The deadline is not reached: git exits cleanly and fast, so the
// process group's Cancel never fires, which is what makes this WaitDelay's test
// alone.
func TestAWorktreeAddThatBackgroundsAProcessDoesNotHang(t *testing.T) {
	repo := tempRepo(t)
	pidFile := filepath.Join(t.TempDir(), "childpid")
	writeHook(t, repo, "#!/bin/sh\nsleep 60 &\necho $! > "+pidFile+"\n")
	t.Cleanup(func() { killRecorded(pidFile) })

	done := make(chan error, 1)
	go func() {
		_, err := addWorktree(repo, "iso")
		done <- err
	}()

	select {
	case <-done:
		// Returned - the point. git created the worktree, but a child holding
		// the pipe makes os/exec report ErrWaitDelay, so the error is not
		// asserted on; that it came back at all is.
	case <-time.After(testTimeout):
		t.Fatalf("addWorktree did not return within %v while a post-checkout hook held git's stdout open; "+
			"without WaitDelay cmd.Output waits for that pipe forever", testTimeout)
	}
}

// TestAHungWorktreeAddHasItsTreeReapedOnTheDeadline pins the process group.
//
// Here git itself hangs: the hook backgrounds a child, records its pid, and
// waits on it, so the whole tree is alive when the deadline fires. The group
// kill reaps the child; the default cancel would SIGKILL the git leader alone
// and orphan it. So a WaitDelay with no group returns just as promptly and
// leaves the child running - which is what this test's second half refuses.
func TestAHungWorktreeAddHasItsTreeReapedOnTheDeadline(t *testing.T) {
	shortGitTimeout(t, 2*time.Second)
	repo := tempRepo(t)
	pidFile := filepath.Join(t.TempDir(), "childpid")
	writeHook(t, repo, "#!/bin/sh\nsleep 60 &\necho $! > "+pidFile+"\nwait\n")
	t.Cleanup(func() { killRecorded(pidFile) })

	done := make(chan error, 1)
	go func() {
		_, err := addWorktree(repo, "iso")
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatalf("addWorktree did not return within %v of a %v git deadline; a hung git worktree add was not bounded",
			testTimeout, gitTimeout)
	}

	pid := recordedPid(t, pidFile)
	if !waitForDeath(pid, testTimeout) {
		t.Fatalf("the process git's post-checkout hook started (pid %d) was still alive after the worktree add returned; "+
			"the deadline reaped the git leader alone and orphaned its tree", pid)
	}
}

// TestAWorktreeSpawnDoesNotBlockTheClientsOtherFrames pins the move off the
// dispatch goroutine.
//
// The hook blocks git until the test releases it (self-capping, so a forgotten
// release cannot wedge the daemon). A worktree spawn is sent, then a status
// question on the same connection. serveClient dispatches one connection's
// frames in order, so a spawn that ran the git here would hold the status
// behind it for as long as git blocks; run on its own goroutine, the status is
// answered at once. The bound is well under the hook's self-cap, so a reply
// that only comes after git unblocks fails.
func TestAWorktreeSpawnDoesNotBlockTheClientsOtherFrames(t *testing.T) {
	fakeClaudeOnPath(t, "")
	repo := tempRepo(t)
	release := filepath.Join(t.TempDir(), "release")
	writeHook(t, repo, "#!/bin/sh\nn=0\nwhile [ ! -e "+release+" ] && [ \"$n\" -lt 200 ]; do sleep 0.05; n=$((n+1)); done\n")

	d := startDaemon(t)
	// Released before the daemon stops (cleanups run last-in-first-out, so this
	// runs before startDaemon's own stop): the blocked git then finishes and
	// shutdown's bounded wait is not spent on it.
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o644) })

	c := attach(t, d.socket)
	c.send(rpc.Frame{Kind: rpc.FrameSpawn, SessionID: idAlpha, Text: "sydney", Dir: repo, Worktree: "iso"})
	c.send(rpc.Frame{Kind: rpc.FrameStatus})

	start := time.Now()
	if _, ok := c.awaitWithin(4*time.Second, "a status reply while the worktree git is blocked", func(f rpc.Frame) bool {
		return f.Kind == rpc.FrameStatusReply && f.Status != nil
	}); !ok {
		t.Fatalf("no status reply in 4s while a worktree spawn's git was blocked (%v elapsed); "+
			"the spawn is holding the dispatch goroutine", time.Since(start).Round(time.Millisecond))
	}
}

// awaitWithin is await with the caller's own deadline, and it returns whether
// the frame arrived rather than failing - the property under test here is how
// quickly one comes back, so a timeout is a verdict the caller reads, not an
// error. It keeps await's held-frame handling so a reply an earlier wait read
// past still counts.
func (c *testClient) awaitWithin(d time.Duration, what string, match func(rpc.Frame) bool) (rpc.Frame, bool) {
	c.t.Helper()
	if f, ok := c.takeHeld(match); ok {
		return f, true
	}
	deadline := time.After(d)
	for {
		select {
		case f, open := <-c.frames:
			if !open {
				c.t.Fatalf("the daemon hung up while waiting for %s\nsaw: %s", what, c.transcript())
			}
			c.seen = append(c.seen, f)
			if match(f) {
				return f, true
			}
			c.held = append(c.held, f)
		case err := <-c.errs:
			c.t.Fatalf("read while waiting for %s: %v\nsaw: %s", what, err, c.transcript())
		case <-deadline:
			return rpc.Frame{}, false
		}
	}
}
