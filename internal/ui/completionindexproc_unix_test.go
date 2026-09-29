//go:build unix

package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// What a lister leaves holding its output is reclaimed once it returns, as
// bangRun reclaims a command's: the group kill after Run, not only at the
// deadline.
func TestAListerReclaimsWhatItLeftBehind(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "background.pid")
	script := "(sh -c 'echo $$ > " + pidFile + "; exec sleep 45' &) ; sleep 0.3; exit 0"
	if _, _, err := runCapped(time.Minute, nil, "/bin/sh", "-c", script); err == nil {
		t.Fatal("the fixture held no pipe past the shell, so this asserts nothing about the reclaim")
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read the fixture's pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		t.Fatalf("the fixture's pid is %q: %v", raw, err)
	}
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("what the lister left behind, pid %d, is still alive after it returned", pid)
		}
	}
}
