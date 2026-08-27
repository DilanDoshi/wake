package daemon

// The git worktree a session runs in, when the operator asked for one.
//
// **Wake creates it and does not pass `--worktree`.** claude has a managed
// worktree subsystem of its own - paths it resolves under `.claude/worktrees`,
// a lock keyed on pid, session state it persists, and a sweep that removes
// stale ones - and handing it the choice would make claude the owner of the one
// fact Wake keys everything to. Config.Dir would name the repository while the
// session ran somewhere else, its transcript would land under a different
// project slug, and the park book would record a directory a wake could not
// resume in. Creating it here keeps Dir the truth: discovery still verifies it,
// gitref still reads the linked worktree's HEAD, and groups still key on it.
//
// **Wake never removes one.** A worktree holds uncommitted work, so an
// automatic `git worktree remove` on park or on `wake stop` is the only
// irreversible thing this feature could do - and `wake stop` is meant to be the
// only irreversible verb there is. They are left for `git worktree remove`.

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

const (
	// worktreeRoot is where worktrees live, relative to the repository root.
	// Wake's own directory and not `.claude/worktrees`, which claude sweeps.
	worktreeRoot = ".wake/worktrees"

	// worktreeBranchPrefix keeps Wake's branches together in `git branch` and
	// out of the way of the operator's own naming.
	worktreeBranchPrefix = "wake/"

	// gitWaitDelay bounds the wait after a git command's own deadline, for the
	// pipes a process it started may still hold. It is internal/core's waitDelay
	// and internal/ui's bangWaitDelay for the same reason, and pays for the same
	// failure: `git worktree add` runs the repository's post-checkout hook, and a
	// hook that backgrounds anything inheriting git's stdout keeps os/exec's copy
	// goroutine alive - so cmd.Output would never return. With it, os/exec closes
	// the pipes and Wait reports exec.ErrWaitDelay, which refuses the spawn.
	gitWaitDelay = 2 * time.Second
)

// gitTimeout bounds one git call. A worktree add on a large repository copies a
// checkout, so it is generous; the point is that a git that never returns fails
// the spawn instead of holding a goroutine forever. A var rather than a const so
// a test can drive the deadline without waiting two minutes - written only from
// a test, which is liveCap's own seam.
var gitTimeout = 2 * time.Minute

// worktreeBranch is the branch a named worktree checks out.
func worktreeBranch(name string) string { return worktreeBranchPrefix + name }

// addWorktree creates the worktree a session will run in and returns its
// absolute path.
//
// dir is wherever the client was, which is any directory inside the repository;
// the worktree is anchored to the repository *root*, so a spawn from a
// subdirectory does not bury one inside it.
//
// An existing branch is checked out rather than refused - "put this agent on
// the branch I was already on" is the case - and `-B` is never used, because it
// would move that branch's tip.
func addWorktree(dir, name string) (string, error) {
	if err := rpc.ValidWorktreeName(name); err != nil {
		return "", err
	}
	root, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("a worktree needs a git repository, and %s is not inside one: %w", dir, err)
	}
	path := filepath.Join(root, filepath.FromSlash(worktreeRoot), name)
	branch := worktreeBranch(name)

	// Asked rather than inferred from a failure: git's wording for "branch
	// already exists" is not something to parse across versions or locales.
	add := []string{"worktree", "add", "--quiet", path}
	if _, err := git(root, "show-ref", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		add = append(add, branch)
	} else {
		add = append(add, "-b", branch)
	}
	if _, err := git(root, add...); err != nil {
		return "", fmt.Errorf("create the worktree %s: %w", path, err)
	}
	return path, nil
}

// sessionDir is where a spawn actually runs: the directory the client named, or
// a fresh worktree under that directory's repository when the frame asked for
// one.
//
// Here rather than beside spawnDir because the subject is the worktree, and
// spawn.go was three lines under the hard max holding it.
func sessionDir(f rpc.Frame) (string, error) {
	dir := spawnDir(f)
	if f.Worktree == "" {
		return dir, nil
	}
	return addWorktree(dir, f.Worktree)
}

// git runs one git command in dir and returns its trimmed stdout.
//
// Errors carry git's own stderr: this refuses a spawn, and "exit status 128" is
// not something an operator can act on.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	// Two bounds, and they are not redundant - internal/ui's bangRun keeps the
	// same pair. The process group makes Cancel reach everything git started (a
	// post-checkout hook and whatever it spawned), so a deadline reaps the tree
	// and not the leader alone; WaitDelay bounds the wait for a pipe such a child
	// holds open past git's own exit, the case that made cmd.Output never return.
	// Either alone leaves one of the two failures open.
	cmd.WaitDelay = gitWaitDelay
	worktreeSetGroup(cmd)
	cmd.Cancel = func() error { return worktreeKillGroup(cmd) }
	out, err := cmd.Output()
	if err != nil {
		if said := strings.TrimSpace(stderr.String()); said != "" {
			return "", fmt.Errorf("%s", said)
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
