//go:build unix

package main

import (
	"syscall"
	"testing"
	"time"
)

// A daemon's socket file exists before it listens, so a `wake stop` sent the
// moment the file appears finds nobody listening and says no daemon is running
// (BUG-71). The harness calls a daemon ready only once it would take that dial.
func TestTheHarnessWaitsForTheDaemonToListenNotForItsSocketFile(t *testing.T) {
	sock := tempSocket(t)
	fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	if err := syscall.Bind(fd, &syscall.SockaddrUnix{Name: sock}); err != nil {
		t.Fatal(err)
	}
	listened := make(chan error, 1)
	time.AfterFunc(100*time.Millisecond, func() { listened <- syscall.Listen(fd, 1) })

	if exited, err := awaitListening(t, sock, make(chan error)); exited {
		t.Fatalf("no Serve ran, yet the wait reports one returned: %v", err)
	}
	select {
	case err := <-listened:
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
	default:
		t.Fatal("the harness called the daemon ready while its socket was bound but not listening")
	}
}
