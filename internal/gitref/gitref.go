// Package gitref answers one question about a directory: which commit is
// checked out there, and does it have a branch name.
//
// It is a leaf package with no Wake dependencies because two callers need the
// answer and neither may import the other - internal/daemon derives a session's
// label from it, and internal/ui draws it on the conversation's status bar.
// This was two implementations for a while and they had already drifted: one
// bounded its reads and capped its walk and the other did neither, and only one
// of them handled `git worktree add --relative-paths`, which is the shape Wake
// itself is developed in.
//
// **Nothing read here is trusted.** These are files Wake did not write, in a
// directory an agent can write to. Every read is bounded, the walk is capped,
// and every failure is the same answer - no ref - because there is nothing a
// caller could do differently for a HEAD that is missing, unreadable, or a
// directory.
package gitref

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	// maxHeadBytes bounds a read. A HEAD is one short line; anything longer is
	// not a HEAD this package will read to the end of.
	maxHeadBytes = 4096

	// maxRepoDepth caps the walk towards the root, so a pathological path
	// cannot turn one question into an unbounded number of stats.
	maxRepoDepth = 64

	// headRefPrefix opens a HEAD that names a branch; gitdirPrefix opens the
	// pointer file a linked worktree keeps where its .git directory would be.
	headRefPrefix = "ref: refs/heads/"
	gitdirPrefix  = "gitdir:"

	// shortCommitLen is how much of a detached HEAD's commit identifies it.
	shortCommitLen = 7
)

// Ref is what is checked out in a directory.
type Ref struct {
	// Branch is the branch's name, or "" when there is none - either because
	// the directory is not in a work tree or because HEAD is detached.
	Branch string

	// Commit is the short commit for a detached HEAD, and "" otherwise. It is
	// separate from Branch because the two callers want different things from
	// a detached head: a label falls back to the directory's own name, and a
	// status bar says the commit, which is the state where "which branch" has
	// no answer and a reader most wants to know that.
	Commit string
}

// Of is what is checked out in dir, or the zero Ref for a directory that is not
// in a work tree or cannot be read.
func Of(dir string) Ref {
	gitDir := findGitDir(dir)
	if gitDir == "" {
		return Ref{}
	}
	head := firstLine(readBounded(filepath.Join(gitDir, "HEAD")))
	if ref, ok := strings.CutPrefix(head, headRefPrefix); ok {
		return Ref{Branch: strings.TrimSpace(ref)}
	}
	if len(head) >= shortCommitLen && !strings.ContainsAny(head, " \t") {
		return Ref{Commit: head[:shortCommitLen]}
	}
	return Ref{}
}

// findGitDir walks towards the root looking for the repository dir belongs to.
//
// The walk stops at the first .git entry of either shape, and does not continue
// past a pointer file it could not read. Continuing would find the *enclosing*
// repository of a linked worktree, which is precisely the wrong answer: every
// worktree of a repo would report the parent's branch, and worktrees are the
// case this exists to tell apart.
func findGitDir(dir string) string {
	if dir == "" {
		return ""
	}
	cur := filepath.Clean(dir)
	for range maxRepoDepth {
		entry := filepath.Join(cur, ".git")
		if info, err := os.Stat(entry); err == nil {
			if info.IsDir() {
				return entry
			}
			return gitDirFromFile(entry, cur)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
	return ""
}

// gitDirFromFile reads the `gitdir:` pointer a linked worktree keeps where its
// .git directory would be. The path in it may be relative to the worktree,
// which is what `git worktree add --relative-paths` writes.
func gitDirFromFile(path, base string) string {
	target, pointed := strings.CutPrefix(firstLine(readBounded(path)), gitdirPrefix)
	target = strings.TrimSpace(target)
	if !pointed || target == "" {
		return ""
	}
	if !filepath.IsAbs(target) {
		return filepath.Join(base, target)
	}
	return target
}

// readBounded reads the front of a file, or nothing at all.
func readBounded(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, maxHeadBytes)
	n, err := f.Read(buf)
	if n <= 0 || (err != nil && n == 0) {
		return ""
	}
	return string(buf[:n])
}

// firstLine is everything before the first newline, trimmed.
func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// Name is the ref as one word for a reader: the branch, or the short commit
// when HEAD is detached, or "" when there is neither.
//
// A detached head is worth naming rather than blanking - it is the state where
// "which branch" has no answer, which is exactly when somebody wants to know.
func (r Ref) Name() string {
	if r.Branch != "" {
		return r.Branch
	}
	return r.Commit
}
