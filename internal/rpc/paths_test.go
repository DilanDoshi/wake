package rpc

import (
	"strings"
	"testing"
)

// An added directory is a directory, so it gets Frame.Dir's fence and not
// ValidWorktreeName's: a closed character set would refuse most real paths.
func TestAnAddedDirectoryThatIsNotAnAbsolutePathIsRefused(t *testing.T) {
	for _, dir := range []string{"", "lib", "./lib", "../lib", "~/lib", "-rf", "--force"} {
		t.Run(dir, func(t *testing.T) {
			if err := ValidAddDir(dir); err == nil {
				t.Errorf("%q is accepted as an added directory, and it reaches a command line as written", dir)
			}
		})
	}
}

func TestAnOrdinaryAddedDirectoryIsAccepted(t *testing.T) {
	for _, dir := range []string{"/", "/repo", "/repo/lib", "/Users/someone/a b/c", "/repo/lib-1.2_x"} {
		if err := ValidAddDir(dir); err != nil {
			t.Errorf("%q is refused as an added directory: %v", dir, err)
		}
	}
}

// `..` is allowed on purpose. Refusing it reads like a fence and is not one: it
// grants the same directory a Clean-stable path would, and `$PWD/../lib` is how
// a shell names a sibling.
func TestAnAddedDirectoryMayWalkUp(t *testing.T) {
	for _, dir := range []string{"/repo/../lib", "/repo/./lib", "/repo/lib/", "/repo//lib"} {
		if err := ValidAddDir(dir); err != nil {
			t.Errorf("%q is refused, and it names the same directory its cleaned form does: %v", dir, err)
		}
	}
}

// Bounded because it reaches an exec argv, which the OS refuses by accident at
// a size nobody chose. Same argument as maxWorktreeName.
func TestAnAddedDirectoryIsBounded(t *testing.T) {
	long := "/" + strings.Repeat("a", maxAddDirLen)
	if err := ValidAddDir(long); err == nil {
		t.Errorf("a %d-character directory is accepted, against a %d bound", len(long), maxAddDirLen)
	}
}

func TestTooManyAddedDirectoriesAreRefused(t *testing.T) {
	dirs := make([]string, 0, maxAddDirs+1)
	for range maxAddDirs + 1 {
		dirs = append(dirs, "/repo")
	}
	if err := ValidAddDirs(dirs); err == nil {
		t.Errorf("%d added directories are accepted, against a %d bound", len(dirs), maxAddDirs)
	}
	if err := ValidAddDirs(dirs[:maxAddDirs]); err != nil {
		t.Errorf("%d added directories are refused: %v", maxAddDirs, err)
	}
}

// ValidAddDirs is what the daemon calls, so it has to reach every element
// rather than only the first.
func TestEveryAddedDirectoryIsChecked(t *testing.T) {
	if err := ValidAddDirs([]string{"/repo", "lib"}); err == nil {
		t.Error("a list whose second element is relative is accepted")
	}
	if err := ValidAddDirs(nil); err != nil {
		t.Errorf("no added directories is refused: %v", err)
	}
}

// A debug file is a file the daemon's child creates, so the wire carries a
// name and Wake owns the directory - --mcp-config's ruling and
// ValidWorktreeName's shape, for the same reason.
func TestADebugFileNameThatIsAPathIsRefused(t *testing.T) {
	for _, name := range []string{
		"", ".", "..", "a/b", `a\b`, "/absolute", "../escape", "~/log",
		"-rf", "--force", "has space", "semi;colon", "dollar$", "quote'",
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidDebugFileName(name); err == nil {
				t.Errorf("%q is accepted as a debug file name, and it becomes a file the daemon's child writes", name)
			}
		})
	}
}

func TestAnOrdinaryDebugFileNameIsAccepted(t *testing.T) {
	for _, name := range []string{"alex", "alex-2", "run_3", "v1.2", "A1"} {
		if err := ValidDebugFileName(name); err != nil {
			t.Errorf("%q is refused as a debug file name: %v", name, err)
		}
	}
}

func TestADebugFileNameIsBounded(t *testing.T) {
	if err := ValidDebugFileName(strings.Repeat("a", maxDebugFileName+1)); err == nil {
		t.Error("an unbounded debug file name is accepted")
	}
}
