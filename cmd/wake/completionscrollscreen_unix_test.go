//go:build unix

package main

// The completion menu's window through the real binary (internal/ui's
// completionscroll_test.go decides the arithmetic): ↓ walks every offer past
// the rows a pane draws, the drawn rows follow the cursor, the key line says
// how many lie each way, and ⇥ and ↵ take the offer the cursor reached.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	arrowDown = "\x1b[B"
	arrowUp   = "\x1b[A"

	// manyFiles is how many files the `@` repository holds, and the menu's
	// prefix over them: more than any pane's rows, with every match the search's.
	manyFiles  = 60
	manyPrefix = "zq"
)

var (
	linesAbove  = regexp.MustCompile(`\+\d+ above`)
	windowCount = regexp.MustCompile(`\+\d+ (above|below)`)
)

// manyFilesRepo is a git repository of manyFiles files that share a prefix no
// other offer has.
func manyFilesRepo(t *testing.T) string {
	t.Helper()
	wakeBinary(t)
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve the repository: %v", err)
	}
	for i := range manyFiles {
		if err := os.WriteFile(filepath.Join(dir, manyFileName(i)), []byte("x\n"), 0o644); err != nil {
			t.Fatalf("write a file: %v", err)
		}
	}
	cmd := exec.Command("git", "init", "--quiet")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git init: %v\n%s", err, out)
	}
	return dir
}

func manyFileName(i int) string { return fmt.Sprintf("%s%02d.go", manyPrefix, i) }

// ↓ through a `/` menu longer than its rows reaches the last command, the rows
// follow it and the key line counts above; ↑ comes back to the first; ↵ takes
// the one reached.
func TestDownWalksACommandMenuPastItsRowsOnScreen(t *testing.T) {
	withScriptedAgent(t, scriptManyCommands)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	s := startWakeInAConversation(t, 120, 50)
	s.await("ready")
	s.settle()

	first, last := manyCommandName(0), manyCommandName(manyCommands-1)
	s.send("/")
	s.await(completionKeyHint)
	s.await("› /" + first)
	if rows := s.rowsWith("/" + last); len(rows) != 0 {
		t.Fatalf("%s is drawn before anything is walked, so the menu is not longer than its rows.\n%s", last, s.dump())
	}

	s.send(strings.Repeat(arrowDown, manyCommands-1))
	s.await("› /" + last)
	s.settle()
	if rows := s.rowsWith("/" + first); len(rows) != 0 {
		t.Fatalf("%s is still drawn with the cursor on %s: the window did not move.\n%s", first, last, s.dump())
	}
	if !linesAbove.MatchString(s.text()) {
		t.Fatalf("the key line does not count what lies above the window.\n%s", s.dump())
	}

	s.send(strings.Repeat(arrowUp, manyCommands-1))
	s.await("› /" + first)
	s.awaitGone("above")

	s.send(strings.Repeat(arrowDown, manyCommands-1))
	s.await("› /" + last)
	s.send("\r")
	s.awaitGone(completionKeyHint)
	s.await("> /" + last)
}

// The same through a conversation's `@` file search over a git repository.
func TestDownWalksTheFileMenuPastItsRowsOnScreen(t *testing.T) {
	withScriptedAgent(t, scriptAtMenu)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	t.Setenv("WAKE_PROJECTS", t.TempDir())
	s := startWakeIn(t, manyFilesRepo(t), 120, 50, cmdNew, atMenuAgent)
	s.await("ready")
	s.settle()

	first, last := "@"+manyFileName(0), "@"+manyFileName(manyFiles-1)
	s.send("@" + manyPrefix)
	s.await(completionKeyHint)
	s.await("› " + first)
	if rows := s.rowsWith(last); len(rows) != 0 {
		t.Fatalf("%s is drawn before anything is walked, so the menu is not longer than its rows.\n%s", last, s.dump())
	}

	s.send(strings.Repeat(arrowDown, manyFiles-1))
	s.await("› " + last)
	s.settle()
	if rows := s.rowsWith(first); len(rows) != 0 {
		t.Fatalf("%s is still drawn with the cursor on %s: the window did not move.\n%s", first, last, s.dump())
	}

	s.send("\t")
	s.awaitGone(completionKeyHint)
	s.await("> " + last)
}

// The same walk in a terminal of 24 rows, where the pane cannot hold the menu a
// tall one draws: the window is the rows the pane has, so the cursor and the
// count are on screen the whole way down, and ↵ takes what is seen.
func TestDownWalksACommandMenuInAShortPaneOnScreen(t *testing.T) {
	withScriptedAgent(t, scriptManyCommands)
	t.Setenv("WAKE_SOCKET", tempSocket(t))
	s := startWakeInAConversation(t, 120, 24)
	s.await("ready")
	s.settle()

	s.send("/")
	s.await(completionKeyHint)
	last := manyCommandName(manyCommands - 1)
	for cursor := 0; cursor < manyCommands-1; {
		n := min(7, manyCommands-1-cursor)
		s.send(strings.Repeat(arrowDown, n))
		cursor += n
		s.await("› /" + manyCommandName(cursor))
		if !windowCount.MatchString(s.text()) || !strings.Contains(s.text(), completionKeyHint) {
			t.Fatalf("the key line is cut, or says nothing of the rows outside the window.\n%s", s.dump())
		}
	}
	s.send("\r")
	s.awaitGone(completionKeyHint)
	s.await("> /" + last)
}
