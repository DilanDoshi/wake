package main

import (
	"io"
	"strings"
	"testing"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/daemon"
)

// Every verb that can start an agent - bare `wake` included, since the room
// seats a manager - refuses up front when there is no claude to start, rather
// than failing inside the daemon at the first spawn. The verbs that only look
// at a fleet do not ask.
func TestVerbsThatStartAgentsRefuseWithoutClaude(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv(daemon.SocketEnv, tempSocket(t))
	claudeOnPath = core.ClaudeOnPath
	t.Cleanup(func() { claudeOnPath = func() error { return nil } })

	for _, args := range [][]string{nil, {"new"}, {"new", "--effort", "max"}, {"attach", "sydney"},
		{"fork", "sydney"}, {"import", "a11a0000-0000-4000-8000-00000000a11a"}, {"manager"}} {
		err := run(args, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "not on your PATH") {
			t.Errorf("run(%q) = %v, want the missing-claude refusal", args, err)
		}
	}
	for _, args := range [][]string{{"status"}, {"fleets"}, {"--version"}, {"help"}} {
		if err := run(args, io.Discard); err != nil && strings.Contains(err.Error(), "not on your PATH") {
			t.Errorf("run(%q) asked for claude: %v", args, err)
		}
	}
}
