//go:build !unix

package daemon

import "os/exec"

// Without process groups there is nothing to leave and no way to ask whether
// a pid is alive without holding a handle to it. The tests that need either
// are skipped on this platform, for the same reason the reaper does nothing
// here.
func leaveProcessGroup(*exec.Cmd) {}

func processAlive(int) bool { return false }
