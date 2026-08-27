//go:build !unix

package core

import (
	"context"
	"errors"
	"os/exec"
)

func SelfAgentLauncher() (AgentLauncher, error) {
	return AgentLauncher{}, nil
}

func AgentLauncherRequested() bool { return false }

func RunAgentLauncher() error {
	return errors.New("Wake agent launcher is unavailable on this platform")
}

func agentCommand(ctx context.Context, _ AgentLauncher, dir string, args []string) (*exec.Cmd, *agentLauncherPipes, error) {
	cmd := execCommand(ctx, claudeBinary, args...)
	cmd.Dir = dir
	return cmd, nil, nil
}
