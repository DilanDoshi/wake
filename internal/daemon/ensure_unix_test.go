//go:build unix

package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestForkReapsADaemonThatRefusesTheLock(t *testing.T) {
	sock := tempSocket(t)
	pidPath := filepath.Join(filepath.Dir(sock), "fork.pid")
	t.Setenv(SocketEnv, sock)
	t.Setenv(fakeDaemonEnv, "1")
	t.Setenv(fakeDaemonPIDEnv, pidPath)

	lock, err := takeLock(lockPath(sock))
	if err != nil {
		t.Fatalf("take the daemon lock: %v", err)
	}
	if !lock.exclusive {
		t.Fatalf("the test does not hold the daemon lock: %v", lock.why)
	}
	t.Cleanup(func() {
		if err := lock.release(); err != nil {
			t.Errorf("release the daemon lock: %v", err)
		}
	})

	if err := fork(sock); err != nil {
		t.Fatalf("fork: %v", err)
	}

	deadline := time.Now().Add(startTimeout)
	var pid int
	for {
		data, err := os.ReadFile(pidPath)
		if err == nil {
			pid, err = strconv.Atoi(string(data))
			if err != nil {
				t.Fatalf("parse forked daemon pid %q: %v", data, err)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("read forked daemon pid: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatal("the forked daemon never recorded its pid, so this test cannot prove a child existed")
		}
		time.Sleep(startPoll)
	}

	deadline = time.Now().Add(startTimeout)
	for processAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("forked daemon %d still exists after refusing the held lock; the client did not reap it", pid)
		}
		time.Sleep(startPoll)
	}
}
