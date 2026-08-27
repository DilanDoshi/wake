package daemon

// The task label: what a session is working on, taken from where it was
// started rather than from a counter.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoAt builds a git repository with no git binary involved: taskLabel reads
// the plumbing directly, so the plumbing is what a test has to build.
func repoAt(t *testing.T, dir, head string) string {
	t.Helper()

	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o700); err != nil {
		t.Fatalf("MkdirAll %s: %v", gitDir, err)
	}
	writeFile(t, filepath.Join(gitDir, "HEAD"), head)
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// The ordinary case, and the one the label exists for: thirty agents across
// half a dozen branches, each row saying which.
func TestTheLabelIsTheBranchWhenThereIsOne(t *testing.T) {
	dir := repoAt(t, t.TempDir(), "ref: refs/heads/dev-5748\n")

	if got := taskLabel(dir); got != "dev-5748" {
		t.Errorf("taskLabel = %q, want the branch dev-5748", got)
	}
}

// A branch reached from a subdirectory of the repo, which is where anyone
// actually runs `wake`.
func TestTheBranchIsFoundFromASubdirectory(t *testing.T) {
	root := repoAt(t, t.TempDir(), "ref: refs/heads/main\n")
	deep := filepath.Join(root, "internal", "daemon")
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if got := taskLabel(deep); got != "main" {
		t.Errorf("taskLabel from a subdirectory = %q, want main", got)
	}
}

// A linked worktree keeps .git as a *file* pointing at the real git directory,
// and a worktree is the exact case this feature is for: one repository, several
// branches, one agent each. Reading the file as if it were a directory gives
// every worktree the same label as its parent, which is the one answer that
// makes the label useless.
func TestALinkedWorktreeReadsItsOwnBranch(t *testing.T) {
	root := t.TempDir()

	main := repoAt(t, filepath.Join(root, "repo"), "ref: refs/heads/main\n")
	linkedGitDir := filepath.Join(main, ".git", "worktrees", "identity")
	if err := os.MkdirAll(linkedGitDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(linkedGitDir, "HEAD"), "ref: refs/heads/identity\n")

	// The directory is deliberately *not* called what the branch is called.
	// Naming both "identity" would make the fallback and the right answer the
	// same string, and this test would pass against an implementation that
	// never read the worktree's HEAD at all.
	tree := filepath.Join(root, "checkout")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(tree, ".git"), "gitdir: "+linkedGitDir+"\n")

	if got := taskLabel(tree); got != "identity" {
		t.Errorf("taskLabel in a linked worktree = %q, want identity", got)
	}
}

// A gitdir pointer may be relative to the worktree, which is what `git
// worktree add --relative-paths` writes.
func TestARelativeGitdirPointerIsResolved(t *testing.T) {
	root := t.TempDir()
	main := repoAt(t, filepath.Join(root, "repo"), "ref: refs/heads/main\n")

	linked := filepath.Join(main, ".git", "worktrees", "spike")
	if err := os.MkdirAll(linked, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(linked, "HEAD"), "ref: refs/heads/spike\n")

	// Named for neither the branch nor the pointer, for the reason above: this
	// one caught an implementation that dropped the relative-path resolution
	// only after the directory stopped sharing the branch's name.
	tree := filepath.Join(root, "checkout")
	if err := os.MkdirAll(tree, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	rel, err := filepath.Rel(tree, linked)
	if err != nil {
		t.Fatalf("Rel: %v", err)
	}
	writeFile(t, filepath.Join(tree, ".git"), "gitdir: "+rel+"\n")

	if got := taskLabel(tree); got != "spike" {
		t.Errorf("taskLabel with a relative gitdir = %q, want spike", got)
	}
}

// A detached HEAD has no branch, and the raw sha is exactly what a name exists
// to replace. The directory is the fallback, not the sha.
func TestADetachedHeadFallsBackToTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "puffer")
	repoAt(t, dir, "4f78b3d7e1c2a9b0f3e4d5c6b7a8091223344556\n")

	if got := taskLabel(dir); got != "puffer" {
		t.Errorf("taskLabel on a detached HEAD = %q, want the directory puffer", got)
	}
}

// Wake is not a git tool. A session started outside a repository still gets a
// label, because the directory is the useful half even when the branch is not
// there.
func TestOutsideARepositoryTheLabelIsTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scratch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if got := taskLabel(dir); got != "scratch" {
		t.Errorf("taskLabel outside a repository = %q, want the directory scratch", got)
	}
}

// A branch is not bounded by anything git enforces, and `wake status` lays its
// rows out in fixed columns. An unbounded label is a row that wraps, and a
// wrapped row in a thirty-agent list is the list stopping being scannable.
func TestALongBranchIsTruncatedToTheColumn(t *testing.T) {
	long := strings.Repeat("renovate/bump-everything-", 4)
	dir := repoAt(t, t.TempDir(), "ref: refs/heads/"+long+"\n")

	got := taskLabel(dir)
	if len([]rune(got)) > maxLabelLen {
		t.Errorf("taskLabel = %q, %d runes, want at most %d", got, len([]rune(got)), maxLabelLen)
	}
	if !strings.HasPrefix(got, long[:8]) {
		t.Errorf("taskLabel = %q, want it to keep the front of the branch", got)
	}
	if !strings.HasSuffix(got, truncationMark) {
		t.Errorf("taskLabel = %q, want a mark saying it was cut", got)
	}
}

// There has to be an answer for a directory that names nothing, and it is "no
// label" rather than "/" or ".". Both of those render as a session called
// `sydney <> .`, which reads as information and is not any.
func TestADirectoryThatNamesNothingHasNoLabel(t *testing.T) {
	for _, dir := range []string{"", "/", ".", "/////"} {
		if got := taskLabel(dir); got != "" {
			t.Errorf("taskLabel(%q) = %q, want no label at all", dir, got)
		}
	}
}

// A HEAD that is not a HEAD must not become a label. This is a file on disk
// that Wake did not write, so it is not trusted: whatever is in it reaches
// `wake status`'s columns and, through the roster, the next daemon's report.
func TestAHeadFileThatIsNotOneIsIgnored(t *testing.T) {
	tests := []struct {
		what string
		head string
	}{
		{what: "empty", head: ""},
		{what: "a ref that names no branch", head: "ref: refs/tags/v1.0.0\n"},
		{what: "a ref with nothing after it", head: "ref: refs/heads/\n"},
		{what: "a newline in the middle of the branch", head: "ref: refs/heads/ok\nrm -rf /\n"},
		{what: "a control character inside the branch", head: "ref: refs/heads/o\x1b[2Jk\n"},
		{what: "binary", head: "\x00\x01\x02"},
	}
	for _, tc := range tests {
		t.Run(tc.what, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "fallback")
			repoAt(t, dir, tc.head)

			got := taskLabel(dir)
			// Whatever comes back, nothing that moves a cursor may be in it:
			// this is drawn inside a row somebody is scanning, and an escape
			// sequence in a branch name would be drawn as one.
			for _, r := range got {
				if r < 0x20 || r == 0x7f {
					t.Fatalf("taskLabel = %q, which carries the control character %q", got, r)
				}
			}
			switch got {
			case "fallback", "ok", "o[2Jk":
				// The directory, the first line of a HEAD with junk after it,
				// or that same line with the escape stripped out of it.
			default:
				t.Errorf("taskLabel = %q, want the directory name or the branch on the first line", got)
			}
		})
	}
}

// The walk up towards the root has to stop. A directory tree is not guaranteed
// to hold a repository and this runs on the spawn path, so a walk that did not
// terminate would be a daemon that stops spawning.
func TestTheSearchForARepositoryTerminatesAtTheRoot(t *testing.T) {
	deep := t.TempDir()
	for range 8 {
		deep = filepath.Join(deep, "d")
	}
	if err := os.MkdirAll(deep, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	if got := taskLabel(deep); got != "d" {
		t.Errorf("taskLabel = %q, want the directory name after the walk found no repository", got)
	}
}

// A .git that is a file saying nothing usable is the debris case, and it has
// to be the directory rather than a crash or a blank.
func TestAGitFilePointingNowhereFallsBackToTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "broken")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, ".git"), "this is not a gitdir pointer\n")

	if got := taskLabel(dir); got != "broken" {
		t.Errorf("taskLabel = %q, want the directory broken", got)
	}
}

// The `gitdir:` prefix is required, not decoration - and this is the case that
// proves it. A .git file holding a bare path is not git's format, and following
// it would mean this reads a HEAD out of whatever directory a file Wake did not
// write happens to name.
//
// It took a repository at the end of that path to reach: without one, a
// followed pointer and a refused one both end at the directory fallback, and
// the guard was unobservable. Mutation check: accepting a file with no prefix
// leaves this failing at "taskLabel = somewhere-else".
func TestAGitFileWithoutTheGitdirPrefixIsNotFollowed(t *testing.T) {
	root := t.TempDir()

	elsewhere := filepath.Join(root, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(elsewhere, "HEAD"), "ref: refs/heads/somewhere-else\n")

	dir := filepath.Join(root, "broken")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	writeFile(t, filepath.Join(dir, ".git"), elsewhere+"\n")

	if got := taskLabel(dir); got != "broken" {
		t.Errorf("taskLabel = %q, want the directory broken: a .git file with no `gitdir:` on it is not a pointer", got)
	}
}

// docs/notes/bugs.md BUG-9. A label is drawn inside a row somebody scans, and it
// is reachable: it is derived from the branch of a directory an agent can
// `git checkout` in, and it is settable over the socket with /task. This
// stripper was the one in the tree that stopped at C0 - and U+009B is a CSI
// with no ESC in front of it, so a terminal in 8-bit mode acted on it.
func TestALabelCannotCarryAnEscapeIntroducer(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"an 8-bit CSI", "feat\u009b2Jx"},
		{"an escape", "feat\x1b[2Jx"},
		{"a carriage return", "feat\rx"},
		{"DEL", "feat\x7fx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanLabel(tc.in)
			for _, r := range got {
				if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
					t.Errorf("the label kept %#x: %q", r, got)
				}
			}
			if !strings.Contains(got, "feat") {
				t.Errorf("cleaning ate the label: %q", got)
			}
		})
	}
}
