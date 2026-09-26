package main

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/daemon"
)

// Bare `wake` makes a new fleet, whose daemon is forked from this process and
// inherits this PATH - so here, and only here, the terminal's PATH is the one
// that counts, and a missing claude is refused before anything is made. Every
// other verb talks to a daemon whose own PATH decides, and the daemon says so
// at the spawn (core.claudeMissing): a terminal without claude may still
// reattach to a fleet that has it.
func TestOnlyANewFleetChecksThisTerminalForClaude(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("HOME", filepath.Dir(tempSocket(t)))
	t.Setenv(daemon.SocketEnv, "")
	claudeOnPath = core.ClaudeOnPath
	t.Cleanup(func() { claudeOnPath = func() error { return nil } })

	if err := run(nil, io.Discard); err == nil || !strings.Contains(err.Error(), "not on your PATH") {
		t.Errorf("bare wake = %v, want the missing-claude refusal", err)
	}
	fleets, err := daemon.Fleets()
	if err != nil || len(fleets) != 0 {
		t.Errorf("a refused bare wake left fleets behind: %v, %v", fleets, err)
	}

	t.Setenv(daemon.SocketEnv, tempSocket(t))
	for _, args := range [][]string{{"status"}, {"attach", "nobody"}} {
		if err := run(args, io.Discard); err != nil && strings.Contains(err.Error(), "not on your PATH") {
			t.Errorf("run(%q) checked this terminal for claude: %v", args, err)
		}
	}
}
