//go:build unix

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// holdLock takes the fleet's daemon lock as a daemon would and lets go after
// hold, the way a daemon that has just unlinked its socket still holds it.
func holdLock(t *testing.T, socket string, hold time.Duration) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(filepath.Dir(socket), "daemon.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(hold)
		_ = f.Close()
	}()
	t.Cleanup(func() { <-released })
}

// After a live stop the daemon lets go of its lock a moment after its socket,
// so the removal waits for it; with no daemon to wait for, a held lock is a
// daemon running, and the fleet stays.
func TestTheRemovalWaitsOnlyForADaemonOnItsWayOut(t *testing.T) {
	socket := stoppedFleet(t, "daemon.log")
	holdLock(t, socket, 200*time.Millisecond)
	var out bytes.Buffer
	if err := endFleet(socket, false, &out); err != nil {
		t.Fatal(err)
	}
	if vanished(t, filepath.Dir(socket)) || !strings.Contains(out.String(), "Fleet dev stays: a daemon is running on it again") {
		t.Errorf("with no wait and a held lock, the removal said %q", out.String())
	}

	out.Reset()
	if err := endFleet(socket, true, &out); err != nil {
		t.Fatal(err)
	}
	if !vanished(t, filepath.Dir(socket)) || !strings.Contains(out.String(), "Removed fleet dev") {
		t.Errorf("waiting out a daemon on its way out, the removal said %q", out.String())
	}
}
