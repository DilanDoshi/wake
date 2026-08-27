package daemon

// Creating the worktree a session runs in.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// tempRepo is a git repository with one commit, which is the least a worktree
// can be added to: `git worktree add` on a repository with no commits fails on
// an unborn HEAD rather than on anything this package decides.
func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// macOS puts TempDir under /var, which is a symlink to /private/var, and
	// git reports the resolved path. Resolving here keeps a path comparison
	// from failing on the symlink rather than on the code.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve %s: %v", dir, err)
	}
	for _, args := range [][]string{
		{"init", "--quiet", "-b", "main"},
		{"config", "user.email", "wake@test"},
		{"config", "user.name", "wake"},
		{"commit", "--quiet", "--allow-empty", "-m", "root"},
	} {
		cmd := exec.Command("git", append([]string{"-C", resolved}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return resolved
}

func TestAWorktreeIsCreatedUnderTheRepositoryRoot(t *testing.T) {
	repo := tempRepo(t)

	got, err := addWorktree(repo, "sydney")
	if err != nil {
		t.Fatalf("addWorktree: %v", err)
	}

	want := filepath.Join(repo, worktreeRoot, "sydney")
	if got != want {
		t.Errorf("worktree is at %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(got, ".git")); err != nil {
		t.Errorf("%s is not a worktree: %v", got, err)
	}
}

// The path is anchored to the repository root and not to the directory the
// client happened to be in, which is any subdirectory of it. Without this a
// spawn from internal/ui puts the worktree inside internal/ui.
func TestAWorktreeIsAnchoredToTheRootAndNotToTheClientsDirectory(t *testing.T) {
	repo := tempRepo(t)
	sub := filepath.Join(repo, "internal", "ui")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := addWorktree(sub, "sydney")
	if err != nil {
		t.Fatalf("addWorktree: %v", err)
	}

	if want := filepath.Join(repo, worktreeRoot, "sydney"); got != want {
		t.Errorf("a spawn from %s put its worktree at %q, want %q", sub, got, want)
	}
}

func TestAWorktreeGetsItsOwnBranch(t *testing.T) {
	repo := tempRepo(t)

	wt, err := addWorktree(repo, "sydney")
	if err != nil {
		t.Fatalf("addWorktree: %v", err)
	}

	out, err := exec.Command("git", "-C", wt, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("read the worktree's branch: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != worktreeBranch("sydney") {
		t.Errorf("the worktree is on %q, want %q", got, worktreeBranch("sydney"))
	}
}

// Naming a branch that already exists checks it out rather than refusing:
// "put this agent on the branch I was already working on" is the case, and -B
// is never an option because it would move an existing branch's tip.
func TestAWorktreeOnAnExistingBranchChecksItOut(t *testing.T) {
	repo := tempRepo(t)
	branch := worktreeBranch("sydney")
	if out, err := exec.Command("git", "-C", repo, "branch", branch).CombinedOutput(); err != nil {
		t.Fatalf("create %s: %v\n%s", branch, err, out)
	}

	wt, err := addWorktree(repo, "sydney")
	if err != nil {
		t.Fatalf("addWorktree onto an existing branch: %v", err)
	}

	out, err := exec.Command("git", "-C", wt, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		t.Fatalf("read the worktree's branch: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != branch {
		t.Errorf("the worktree is on %q, want the branch that already existed, %q", got, branch)
	}
}

// A directory that is not a repository is a refusal carrying git's own words,
// never a spawn that quietly lands in the shared tree - which is the opposite
// of what the operator asked for by naming a worktree at all.
func TestAWorktreeOutsideARepositoryIsRefused(t *testing.T) {
	if _, err := addWorktree(t.TempDir(), "sydney"); err == nil {
		t.Fatal("addWorktree succeeded outside a git repository, so a session asking for isolation would have got none")
	}
}

func TestASecondWorktreeUnderOneNameIsRefused(t *testing.T) {
	repo := tempRepo(t)
	if _, err := addWorktree(repo, "sydney"); err != nil {
		t.Fatalf("first addWorktree: %v", err)
	}

	if _, err := addWorktree(repo, "sydney"); err == nil {
		t.Error("a second worktree under one name succeeded, which would put two agents in one tree")
	}
}

// The seam the spawn handler uses: a frame with no worktree runs where the
// client is, and one with a worktree runs in it.
func TestASpawnWithNoWorktreeRunsWhereTheClientIs(t *testing.T) {
	repo := tempRepo(t)

	got, err := sessionDir(rpc.Frame{Dir: repo})
	if err != nil {
		t.Fatalf("sessionDir: %v", err)
	}
	if got != repo {
		t.Errorf("a spawn with no worktree runs in %q, want %q", got, repo)
	}
}

func TestASpawnWithAWorktreeRunsInIt(t *testing.T) {
	repo := tempRepo(t)

	got, err := sessionDir(rpc.Frame{Dir: repo, Worktree: "sydney"})
	if err != nil {
		t.Fatalf("sessionDir: %v", err)
	}
	if want := filepath.Join(repo, worktreeRoot, "sydney"); got != want {
		t.Errorf("the session runs in %q, want %q", got, want)
	}
}

// A worktree that could not be created refuses the spawn. It must never fall
// back to the repository: an agent in the shared tree is exactly what naming a
// worktree asked to avoid, and nothing downstream would say so.
func TestAWorktreeThatCannotBeCreatedRefusesTheSpawnRatherThanFallingBack(t *testing.T) {
	plain := t.TempDir()

	got, err := sessionDir(rpc.Frame{Dir: plain, Worktree: "sydney"})
	if err == nil {
		t.Fatalf("sessionDir returned %q instead of refusing outside a repository", got)
	}
	if got != "" {
		t.Errorf("a refusal carried the directory %q, which a caller ignoring the error would spawn in", got)
	}
}
