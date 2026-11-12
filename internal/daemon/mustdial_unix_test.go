//go:build unix

package daemon

import (
	"syscall"
	"testing"
	"time"
)

// A daemon's socket file exists before it listens, and a successor's path holds
// the crashed daemon's stale file, then nothing, until listen() binds its own. A
// dial in either window fails, so a test's first dial waits for an accepted one.
func TestTheHarnessDialWaitsForASocketThatIsNotListeningYet(t *testing.T) {
	for _, c := range []struct {
		name       string
		boundEarly bool
	}{
		{"bound, not yet listening", true},
		{"not yet bound", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			sock := tempSocket(t)
			fd, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = syscall.Close(fd) })
			bind := func() error { return syscall.Bind(fd, &syscall.SockaddrUnix{Name: sock}) }
			if c.boundEarly {
				if err := bind(); err != nil {
					t.Fatal(err)
				}
			}
			listened := make(chan error, 1)
			time.AfterFunc(100*time.Millisecond, func() {
				if !c.boundEarly {
					if err := bind(); err != nil {
						listened <- err
						return
					}
				}
				listened <- syscall.Listen(fd, 1)
			})

			_ = mustDial(t, sock).Close()
			if err := <-listened; err != nil {
				t.Fatalf("bind or listen: %v", err)
			}
		})
	}
}
