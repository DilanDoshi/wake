package ui

import (
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestMain arms no notice linger by default. Almost every notice is a side
// effect of what a test is asserting, and a real ten-second tick would ride in
// the command it inspects - blocking a test that runs it, and reading as "acted
// on" to one that expects no command. noticelinger_test.go opts back in with
// recordNoticeTicks.
func TestMain(m *testing.M) {
	noticeTimer = func(time.Duration, func(time.Time) tea.Msg) tea.Cmd { return nil }
	os.Exit(m.Run())
}
