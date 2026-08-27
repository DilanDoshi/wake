//go:build !unix

package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

func passTestParentLease(cmd *exec.Cmd) (*os.File, error) {
	cmd.Env = withoutTestLeaseEnv(cmd.Env)
	if _, ok := os.LookupEnv(testParentLeaseSourceEnv); ok {
		return nil, fmt.Errorf("test parent lease file descriptors are unsupported on this platform")
	}
	return nil, nil
}

func withTestParentLease(parent context.Context) (context.Context, func(), error) {
	_, hasDaemonLease, restore, err := takeTestLeaseEnvironment()
	if err != nil {
		return nil, restore, err
	}
	if hasDaemonLease {
		return nil, restore, fmt.Errorf("test parent lease file descriptors are unsupported on this platform")
	}
	return parent, restore, nil
}
