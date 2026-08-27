//go:build !unix

// The half of the process-group kill that admits it cannot. Wake is built for
// macOS and Linux; this file exists so the package still compiles elsewhere,
// and it is deliberately honest about the cost: with no group to signal, a
// kill reaches the agent and nothing it spawned, so a stdio MCP server it
// abandoned survives. See procgroup_unix.go for what is being given up.

package core

import (
	"errors"
	"os"
	"os/exec"
)

func setProcessGroup(*exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	return cmd.Process.Kill()
}

// processGroup reports the group the child leads. Nothing leads one here,
// which is exactly the cost this file exists to be honest about, and 0 is
// what every caller must read as "there is no group to signal".
func processGroup(*exec.Cmd) int { return 0 }

// KillGroup is the reaper's entry point, and there is nothing here for it to
// do: without process groups there is no id to write down at spawn and no way
// back to a tree whose daemon died. It reports that rather than pretending,
// so a reaper logs one honest line instead of silently finding nothing.
func KillGroup(int) error {
	return errors.New("process groups are not available on this platform")
}
