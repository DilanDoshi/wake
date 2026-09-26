package core

import (
	"fmt"
	"os/exec"
)

// claudeInstallURL is where Claude Code's own install instructions live.
const claudeInstallURL = "https://code.claude.com/docs/en/setup"

// ClaudeOnPath is nil when the claude every agent runs as can be found, and
// otherwise says how to get it - checked before a spawn, so a missing install
// is one sentence rather than a failure inside the daemon.
func ClaudeOnPath() error {
	if _, err := exec.LookPath(claudeBinary); err != nil {
		return fmt.Errorf("every Wake agent is a Claude Code session, and `%s` is not on your PATH. "+
			"Install Claude Code (%s), sign in, then run wake again", claudeBinary, claudeInstallURL)
	}
	return nil
}
