//go:build !unix

package daemon

import "os/exec"

// detach is a no-op here, and the cost is real: the forked daemon stays in
// whatever job started it, so a ^C in that terminal takes the fleet with it.
// See detach_unix.go for what is being given up.
func detach(*exec.Cmd) {}
