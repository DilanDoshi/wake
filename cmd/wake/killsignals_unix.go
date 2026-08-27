//go:build unix

package main

import (
	"os"
	"syscall"
)

// killSignals is what watchSignals listens for.
//
// SIGINT and SIGTERM are bubbletea's own, and are here so a wedged loop that
// swallowed them still ends. SIGHUP and SIGQUIT are here because bubbletea
// handles neither: both end the process by default disposition, which runs no
// terminal restore at all and leaves a raw tty inside an alt screen.
var killSignals = []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT}
