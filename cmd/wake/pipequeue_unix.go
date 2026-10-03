//go:build unix

package main

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// queued reports whether bytes are already waiting in the pipe: one poll that
// does not wait. An error says no, so the ⎋ it was asked about goes now.
func queued(f *os.File) bool {
	fds := []unix.PollFd{{Fd: int32(f.Fd()), Events: unix.POLLIN}}
	for {
		n, err := unix.Poll(fds, 0)
		if !errors.Is(err, unix.EINTR) {
			return err == nil && n > 0
		}
	}
}
