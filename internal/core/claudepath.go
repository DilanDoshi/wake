package core

import (
	"errors"
	"fmt"
	"os/exec"
)

// claudeInstallURL is where Claude Code's own install instructions live.
const claudeInstallURL = "https://code.claude.com/docs/en/setup"

// ClaudeOnPath is nil when the claude every agent runs as can be found on
// this process's PATH, and otherwise says how to get it.
func ClaudeOnPath() error {
	if _, err := exec.LookPath(claudeBinary); err != nil {
		return claudeMissing(err)
	}
	return nil
}

// claudeMissing turns a spawn's not-found into how to install Claude Code, and
// leaves any other failure as it was.
func claudeMissing(err error) error {
	if !errors.Is(err, exec.ErrNotFound) {
		return err
	}
	return fmt.Errorf("every Wake agent is a Claude Code session, and `%s` is not on your PATH. "+
		"Install Claude Code (%s), sign in, then run wake again", claudeBinary, claudeInstallURL)
}
