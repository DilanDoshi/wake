package rpc

// What a spawn frame's *paths* may be, and the two answers are different
// because the two hazards are.
//
// Here rather than in internal/daemon for worktree.go's reason: both sides of
// the socket check them and internal/ui may not import the daemon. The fields
// are declared in this package, so their fences are too.
//
// # An added directory is fenced as a directory, because that is all it is
//
// Frame.AddDir names directories a session's tools may reach. A client that can
// dial this socket already chooses Frame.Dir, so it could have named any of
// them as the directory the session runs in - the reach onto the machine is the
// same, and the fence is therefore Dir's rather than a narrower one:
// **absolute or refused**. ValidWorktreeName's closed character set is wrong
// here and would refuse most real paths.
//
// **Absolute is the whole of it, and a separate refusal of a leading dash was
// tried and deleted**: a word that reads as a flag to claude's parser is never
// absolute, on any platform, so the guard had an empty domain and three
// comments credited it with work the absoluteness test was already doing. What
// actually stops `--add-dir -rf` reaching an argv as a flag is that `-rf` is
// not an absolute path.
//
// **`..` is deliberately allowed**, and refusing it was tried first. It reads
// like a fence and is not one - it refuses `$PWD/../lib`, which is how a shell
// names a sibling directory - while granting nothing a Clean-stable path does
// not: the two resolve to the same directory. Nothing cleans Frame.Dir either,
// for the same reason, and a path is one element of an exec argv which cannot
// introduce another.
//
// # A debug file is fenced as a *name*, because it is a file that gets written
//
// Absolute is no fence at all for a write: `/Users/someone/.zshrc` is absolute.
// Frame.DebugFile becomes a file the daemon's child creates and truncates, so
// this is daemon/manager.go's ruling arriving one field over - **a path on the
// wire lets anything that can dial this socket choose where a session writes**.
// The wire carries one segment, Wake owns the directory (see
// daemon/debuglog.go), and Wake appends the extension, so the name cannot
// choose one either.

import (
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// maxAddDirs and maxAddDirLen bound what reaches an exec argv. Nothing
	// stops an unbounded list today but the OS's own E2BIG, which is
	// maxWorktreeName's "refusal by accident".
	maxAddDirs   = 32
	maxAddDirLen = 4096

	// maxDebugFileName bounds the name that becomes a file beside the socket.
	maxDebugFileName = 100
)

// ValidAddDirs refuses a list of added directories, element by element.
//
// The count is bounded here and the elements are bounded below, because a list
// of legal directories is still an argv nobody sized.
func ValidAddDirs(dirs []string) error {
	if len(dirs) > maxAddDirs {
		return fmt.Errorf("a session may add at most %d directories, got %d", maxAddDirs, len(dirs))
	}
	for _, dir := range dirs {
		if err := ValidAddDir(dir); err != nil {
			return err
		}
	}
	return nil
}

// ValidAddDir refuses every directory that would not reach claude as the
// directory it names.
//
// Checked on both sides and neither is redundant, exactly as
// ValidWorktreeName's are: the client turns a typo into a sentence before a
// socket is dialled, and the daemon's call is what makes Frame.AddDir safe
// against a client that never ran that code.
func ValidAddDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("an added directory needs a path")
	}
	if len(dir) > maxAddDirLen {
		return fmt.Errorf("an added directory is at most %d characters, got %d", maxAddDirLen, len(dir))
	}
	// Absolute is also what stops a word that reads as a flag: `-rf` is not an
	// absolute path on any platform. See the header for the separate dash test
	// that was deleted for having no domain of its own.
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("an added directory must be absolute, got %q: a relative one would resolve against the daemon's own directory, and a flag claude would read is not one either", dir)
	}
	return nil
}

// ValidDebugFileName refuses every name that would not be one file in the
// directory Wake keeps beside its socket.
//
// The permitted set is closed on purpose, ValidWorktreeName's terms: what is
// not obviously safe is refused, and a wider one can be argued for against a
// case somebody actually has.
func ValidDebugFileName(name string) error {
	if name == "" {
		return fmt.Errorf("a debug log needs a name")
	}
	if len(name) > maxDebugFileName {
		return fmt.Errorf("a debug log name is at most %d characters, got %d", maxDebugFileName, len(name))
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "-") {
		return fmt.Errorf("%q cannot be a debug log name: it is a directory this would not create, or a flag claude would read", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("%q cannot be a debug log name: it becomes one file in the fleet's own debug directory, so letters, digits, dot, dash and underscore only", name)
		}
	}
	return nil
}
