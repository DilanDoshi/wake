// The resolution both internal/daemon and internal/ui depend on. It lived in
// two places and had already drifted; these are the cases that separated them.

package gitref

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeHead(t *testing.T, dir, line string) {
	t.Helper()
	git := filepath.Join(dir, ".git")
	if err := os.MkdirAll(git, 0o755); err != nil {
		t.Fatalf("making .git: %v", err)
	}
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("writing HEAD: %v", err)
	}
}

func TestGitBranchReadsHead(t *testing.T) {
	dir := t.TempDir()
	writeHead(t, dir, "ref: refs/heads/feat/claude-code-fidelity")

	if got, want := Of(dir).Name(), "feat/claude-code-fidelity"; got != want {
		t.Errorf("gitBranch = %q, want %q", got, want)
	}
}

// Most sessions run below the root, so the walk up is the ordinary path.
func TestGitBranchFindsTheRootFromASubdirectory(t *testing.T) {
	root := t.TempDir()
	writeHead(t, root, "ref: refs/heads/main")
	deep := filepath.Join(root, "internal", "ui")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := Of(deep).Name(); got != "main" {
		t.Errorf("gitBranch from a subdirectory = %q, want main", got)
	}
}

// A worktree's .git is a file pointing at its own git dir - which is the case
// Wake itself is being built in, so getting it wrong reports the main
// checkout's branch for every session in a worktree.
func TestGitBranchFollowsAWorktreeFile(t *testing.T) {
	realGit := filepath.Join(t.TempDir(), "worktrees", "fidelity")
	if err := os.MkdirAll(realGit, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realGit, "HEAD"), []byte("ref: refs/heads/side-branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir: "+realGit+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := Of(tree).Name(); got != "side-branch" {
		t.Errorf("gitBranch in a worktree = %q, want side-branch", got)
	}
}

// A detached head has no branch, and saying so is more useful than saying
// nothing: it is the state where "which branch" has no answer.
func TestGitBranchNamesADetachedHeadByItsCommit(t *testing.T) {
	dir := t.TempDir()
	writeHead(t, dir, "9f2a1c4e8b7d6a5f4e3d2c1b0a9f8e7d6c5b4a39")

	if got, want := Of(dir).Name(), "9f2a1c4"; got != want {
		t.Errorf("gitBranch on a detached head = %q, want %q", got, want)
	}
}

func TestGitBranchIsEmptyOutsideAWorkTree(t *testing.T) {
	for _, dir := range []string{t.TempDir(), "", filepath.Join(t.TempDir(), "does-not-exist")} {
		if got := Of(dir); got != (Ref{}) {
			t.Errorf("directory %q reported %+v, want no ref at all", dir, got)
		}
	}
}

// The memo must not outlive the answer. Checking out another branch changes
// HEAD, and a bar still naming the old one is worse than one naming none.
func TestGitBranchNoticesACheckout(t *testing.T) {
	dir := t.TempDir()
	writeHead(t, dir, "ref: refs/heads/before")
	if got := Of(dir).Name(); got != "before" {
		t.Fatalf("first read = %q, want before", got)
	}

	writeHead(t, dir, "ref: refs/heads/after")
	if got := Of(dir).Name(); got != "after" {
		t.Errorf("after a checkout gitBranch still says %q, want after", got)
	}
}

// The case that separated the two implementations this package replaced, and
// the one Wake itself runs in: `git worktree add --relative-paths` writes a
// pointer relative to the worktree, and resolving it against the wrong base
// reports the *enclosing* repository's branch for every worktree of a repo.
func TestARelativeWorktreePointerResolvesAgainstTheWorktree(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, ".git", "worktrees", "side")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "HEAD"), []byte("ref: refs/heads/side-branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tree := filepath.Join(root, "tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	// Relative, and with no space after the colon - both shapes git writes.
	if err := os.WriteFile(filepath.Join(tree, ".git"), []byte("gitdir:../.git/worktrees/side\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := Of(tree).Name(); got != "side-branch" {
		t.Errorf("a relative worktree pointer resolved to %q, want side-branch", got)
	}
}

// Nothing read here is trusted: these are files in a directory an agent can
// write to. A HEAD that is enormous must not be read whole, and a path that
// never reaches a root must not walk forever.
func TestAnUntrustedHeadIsBounded(t *testing.T) {
	dir := t.TempDir()
	git := filepath.Join(dir, ".git")
	if err := os.MkdirAll(git, 0o755); err != nil {
		t.Fatal(err)
	}
	huge := "ref: refs/heads/" + strings.Repeat("z", 10*maxHeadBytes)
	if err := os.WriteFile(filepath.Join(git, "HEAD"), []byte(huge), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := len(Of(dir).Branch); got > maxHeadBytes {
		t.Errorf("read %d bytes of branch name, want at most %d", got, maxHeadBytes)
	}
}

func TestTheWalkIsCapped(t *testing.T) {
	// Deeper than the cap, with no .git anywhere: the answer is no ref, and
	// getting there must not cost an unbounded number of stats.
	deep := t.TempDir()
	for range maxRepoDepth + 5 {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Skipf("cannot build a path that deep here: %v", err)
	}
	if got := Of(deep); got != (Ref{}) {
		t.Errorf("a path with no repository reported %+v", got)
	}
}
