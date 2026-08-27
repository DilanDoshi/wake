package daemon

import (
	"fmt"
	"os"
	"strings"
)

const (
	testParentLeaseSourceEnv = "WAKE_TEST_PARENT_LEASE_FD"
	testParentLeaseDaemonEnv = "WAKE_TEST_DAEMON_LEASE_FD"
)

// withoutTestLeaseEnv keeps the two private handoff markers off unrelated
// children. It runs even when no source lease is present, so stale daemon
// state cannot leak into an ordinary child.
func withoutTestLeaseEnv(env []string) []string {
	if env == nil {
		env = os.Environ()
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, testParentLeaseSourceEnv+"=") ||
			strings.HasPrefix(entry, testParentLeaseDaemonEnv+"=") {
			continue
		}
		out = append(out, entry)
	}
	return out
}

// takeTestLeaseEnvironment captures the daemon descriptor before removing
// both private markers. The source marker is restored only after Serve returns
// because in-process tests are both runner and daemon; a forked daemon never
// receives that marker in the first place.
func takeTestLeaseEnvironment() (string, bool, func(), error) {
	source, hadSource := os.LookupEnv(testParentLeaseSourceEnv)
	daemon, hadDaemon := os.LookupEnv(testParentLeaseDaemonEnv)
	restore := func() {
		var err error
		if hadSource {
			err = os.Setenv(testParentLeaseSourceEnv, source)
		} else {
			err = os.Unsetenv(testParentLeaseSourceEnv)
		}
		if err != nil {
			logf("wake: restore test parent lease environment: %v", err)
		}
	}
	for _, name := range []string{testParentLeaseSourceEnv, testParentLeaseDaemonEnv} {
		if err := os.Unsetenv(name); err != nil {
			restore()
			return "", false, func() {}, fmt.Errorf("clear test parent lease environment %s: %w", name, err)
		}
	}
	return daemon, hadDaemon, restore, nil
}
