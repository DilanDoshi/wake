package rpc

// What may be a worktree name on the wire.
//
// Here rather than in internal/daemon because both sides of the socket check
// it and internal/ui may not import the daemon - the layering rule that keeps
// the UI from touching an agent's process, which internal/daemon's own soak
// test closes into a cycle. The field is declared in this package, so its fence
// is too.

import (
	"fmt"
	"strings"
)

// maxWorktreeName bounds the name that becomes a directory and a branch.
const maxWorktreeName = 100

// ValidWorktreeName refuses every name that would not be one path segment.
//
// The name becomes a directory under the repository and a branch beside it, so
// this runs before git is reached rather than trusting git to refuse: a leading
// dash is a flag to every git subcommand, and a separator is a session started
// somewhere the operator did not name.
//
// Checked on both sides and neither is redundant - the client turns a typo into
// a sentence before a socket is dialled, and the daemon's call is what makes
// Frame.Worktree safe against a client that never ran that code. The permitted
// set is closed on purpose: what is not obviously safe is refused, and a wider
// one can be argued for against a case somebody actually has.
func ValidWorktreeName(name string) error {
	if name == "" {
		return fmt.Errorf("a worktree needs a name")
	}
	// Bounded because it becomes a path segment and a git ref, and an
	// unbounded one reaches both through subprocess argv. Nothing stops it
	// today but the OS's own ENAMETOOLONG, which is a refusal by accident.
	if len(name) > maxWorktreeName {
		return fmt.Errorf("a worktree name is at most %d characters, got %d", maxWorktreeName, len(name))
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q cannot be a worktree name: it is a directory this would not create, or a flag git would read", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("%q cannot be a worktree name: it becomes a directory, so letters, digits, dot, dash and underscore only", name)
		}
	}
	return nil
}
