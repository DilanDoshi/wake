package rpc

import "testing"

// The name becomes a path segment under the repository, so every shape that
// could leave the worktrees directory is refused before git is reached at all.
func TestAWorktreeNameThatCouldEscapeItsDirectoryIsRefused(t *testing.T) {
	for _, name := range []string{
		"", "..", ".", "a/b", `a\b`, "../escape", "/absolute",
		"-b", "--force", "has space", "quote'", "semi;colon", "dollar$",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidWorktreeName(name); err == nil {
				t.Errorf("%q is accepted as a worktree name, and it becomes a directory under the repository", name)
			}
		})
	}
}

func TestAnOrdinaryWorktreeNameIsAccepted(t *testing.T) {
	for _, name := range []string{"sydney", "fix-42", "v1.2", "a_b", "A1"} {
		if err := ValidWorktreeName(name); err != nil {
			t.Errorf("%q is refused as a worktree name: %v", name, err)
		}
	}
}
