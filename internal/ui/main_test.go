package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// shippedLsFiles is the git the binary runs, kept for the one test that runs it.
var shippedLsFiles = lsFiles

// TestMain arms no notice linger by default. Almost every notice is a side
// effect of what a test is asserting, and a real ten-second tick would ride in
// the command it inspects - blocking a test that runs it, and reading as "acted
// on" to one that expects no command. noticelinger_test.go opts back in with
// recordNoticeTicks.
//
// Nor does any `@` run git: every other test's directory is a t.TempDir, which
// no repository holds, and notARepository is git's answer there.
//
// And the room's banner draws the main checkout's path, never the one running
// the tests: a word in a worktree's name was a word on screen ("fork").
func TestMain(m *testing.M) {
	noticeTimer = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	lsFiles = notARepository
	home, _ := os.UserHomeDir()
	wakeDir = func() string { return filepath.Join(home, "Documents", "wake-public", "internal", "ui") }
	os.Exit(m.Run())
}
