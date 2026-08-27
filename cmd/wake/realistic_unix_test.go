//go:build unix

// The operator's own environment as a test dimension.
//
// Every other fixture in this package runs wake from the package directory on
// whatever branch the checkout happens to be on, which is a short path and a
// short branch. Real operators are not there. The report that opened
// docs/notes/bugs.md BUG-1 came with a screenshot of
// `~/Documents/Project Pufferfish/Singularity  feat-doshi` in a split pane, and
// BUG-8 is that same shape defeating BUG-1's fix: the status bar drops the
// permission mode whole when the row does not fit, and with a real path, a real
// branch and a real model name in front of it, it never fits.
//
// **The suite could not see that, and the reason is the fixture rather than the
// assertions.** The width at which a segment stops being drawn is a function of
// the operator's directory, and nothing here varied the operator's directory.
// So this file is a workspace that looks like somebody's laptop, and the guards
// that need one.
//
// It is deliberately not a new harness. startWakeIn is startWake with a working
// directory, and everything else - the pty, the emulator, the scratch socket,
// the scripted agent - is unchanged, because the point is to run the *same*
// build against a different environment rather than to build a second lab.

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The workspace, spelled out because each part of it is doing a job.
//
// A path deep enough to matter, with a space in it (the report's own
// "Project Pufferfish" had one, and a space is what breaks a naive split), and
// a branch long enough to be an ordinary feature branch rather than `main`.
const (
	realisticNesting = "Documents/Project Pufferfish/Singularity"
	realisticBranch  = "feat/status-bar-and-the-mode"
)

// realisticWorkspace is a git repository at a lifelike path, on a lifelike
// branch, returned as an absolute directory to run wake in.
//
// **It sets HOME, and that is not incidental.** shortPath replaces the home
// directory with `~`, so a workspace anywhere else renders as its full
// absolute path - under `go test` that is
// `/private/var/folders/s_/…/T/TestName123/002`, which is longer than any real
// one and shaped nothing like it. The point of this fixture is the operator's
// row, so the workspace lives under a home directory and the bar draws
// `~/Documents/Project Pufferfish/Singularity`, which is what the report
// showed.
//
// Safe because the two things that would care are already redirected: the
// socket is WAKE_SOCKET's scratch path, and the agent is the scripted fake. A
// real `git init` rather than a stub, because internal/gitref reads the ref off
// disk and the bar draws what it returns.
func realisticWorkspace(t *testing.T) string {
	t.Helper()

	// The binary is built *before* HOME moves. wakeBinary shells out to `go
	// build`, which puts its module cache in $HOME/go/pkg/mod and writes it
	// read-only - so a build that happened under the fake home left a tree
	// t.TempDir's cleanup could not unlink, and the test failed after passing.
	// buildOnce makes this free for every caller after the first.
	wakeBinary(t)

	// EvalSymlinks because on macOS t.TempDir() hands back /var/folders/... while
	// the process resolves the same directory as /private/var/folders/... .
	// shortPath compares HOME against the reported cwd with filepath.Rel and
	// neither side resolves, so the unresolved pair never matches and the bar
	// draws the whole path - the fixture would be measuring a symlink rather
	// than an operator. See bugs.md BUG-15 for the same comparison biting a
	// real directory.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the workspace root: %v", err)
	}
	t.Setenv("HOME", home)

	dir := filepath.Join(home, realisticNesting)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("make the workspace: %v", err)
	}
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"checkout", "--quiet", "-b", realisticBranch},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git %v in the workspace: %v\n%s", args, err, out)
		}
	}
	return dir
}

// A room opened from a lifelike directory still draws its own chrome.
//
// The cheapest possible use of the fixture, and it is here because a workspace
// nothing asserts against is a workspace that silently stops being built. It
// also covers the one thing a path with a space in it could plausibly break on
// the way in: the directory reaching the daemon as one argument.
func TestARoomOpensFromALifelikePath(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeIn(t, realisticWorkspace(t), 200, 50)
	s.await("group chat")
	s.await("~/Documents/Project Pufferfish/Singularity")
}

// The permission mode survives a lifelike path, or it does not reach the
// operator who reported needing it.
//
// docs/notes/bugs.md BUG-8. statusBar appends the mode only when the whole row
// including it fits, and drops it whole otherwise - which is right, because a
// right-cut leaves `permissions: …`, a label announcing a value nobody can
// read. What that rule does not survive is the row in front of it: a real path,
// a real branch and a real model name, which is every operator.
//
// **This is the guard the suite did not have.** BUG-1's own tests pass with a
// short path, so the fix was proved against a directory nobody works in, and
// the reported pane still showed nothing. What is asserted here is the property
// the report was about - press ⇧⇥, see the mode - measured at a width and a
// path an operator actually has.
//
// Marked with the bug it belongs to rather than skipped: it fails while BUG-8
// is open, and it is the thing that says BUG-8 is closed.
func TestTheModeSurvivesALifelikePath(t *testing.T) {
	withScriptedAgent(t, "")
	t.Setenv("WAKE_SOCKET", tempSocket(t))

	s := startWakeIn(t, realisticWorkspace(t), 200, 50, cmdNew)
	s.await("ready")

	// One press, and the label moves on the daemon's receipt rather than on the
	// keystroke - so this is an await, not a settle.
	s.send("\x1b[Z") // ⇧⇥
	s.await("permissions:")
}
