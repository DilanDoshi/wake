//go:build unix

package daemon

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// Removing nonblocking mode from the daemon read end leaves the process stuck
// in lease cleanup until this test closes the writer it deliberately keeps.
func TestForkedDaemonDoesNotWaitForLeaseEOFOnOrdinaryShutdown(t *testing.T) {
	for _, tc := range []struct {
		name     string
		shutdown func(*testClient)
	}{
		{name: "empty-exit", shutdown: func(c *testClient) { c.close() }},
		{name: "frame-quit", shutdown: func(c *testClient) {
			c.send(rpc.Frame{Kind: rpc.FrameQuit})
			c.awaitClose()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := tempSocket(t)
			t.Setenv(SocketEnv, socket)
			t.Setenv(fakeDaemonEnv, "1")
			leaseR, leaseW, err := os.Pipe()
			if err != nil {
				t.Fatalf("parent-death pipe: %v", err)
			}
			t.Cleanup(func() { _ = leaseR.Close() })
			t.Setenv(testParentLeaseSourceEnv, fileDescriptor(leaseR))

			ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
			defer cancel()
			conn, err := EnsureRunning(ctx, socket)
			if err != nil {
				t.Fatalf("EnsureRunning: %v", err)
			}
			c := attachConn(t, conn)
			pid := c.status().PID
			if pid <= 0 {
				t.Fatalf("daemon reported pid %d", pid)
			}

			tc.shutdown(c)
			if !daemonPIDExits(pid, 2*time.Second) {
				// RED cleanup: EOF releases the blocking watcher so this mutation
				// leaves no daemon behind after the assertion.
				_ = leaseW.Close()
				if !daemonPIDExits(pid, 2*time.Second) {
					t.Errorf("daemon pid %d did not exit after lease EOF cleanup", pid)
				}
				t.Fatalf("daemon pid %d survived %s while the lease writer remained open", pid, tc.name)
			}
			if err := leaseW.Close(); err != nil {
				t.Fatalf("close lease writer: %v", err)
			}
		})
	}
}

func daemonPIDExits(pid int, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
		process, err := inspect(ctx, pid)
		cancel()
		if errors.Is(err, errNoProcess) || (err == nil && process.zombie()) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	process, err := inspect(ctx, pid)
	return errors.Is(err, errNoProcess) || (err == nil && process.zombie())
}
