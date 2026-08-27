//go:build unix

package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestPassTestParentLeaseStripsPrivateMarkersWithoutASourceLease(t *testing.T) {
	unsetTestEnv(t, testParentLeaseSourceEnv)
	cmd := exec.Command("unused")
	cmd.Env = []string{"SAFE=value", testParentLeaseDaemonEnv + "=99"}

	lease, err := passTestParentLease(cmd)
	if lease != nil {
		_ = lease.Close()
	}
	if err != nil {
		t.Fatalf("passTestParentLease without a source: %v", err)
	}
	assertNoLeaseMarkers(t, cmd.Env)
}

func TestPassTestParentLeaseCleansAnInheritedEnvironmentWithoutEmptyingIt(t *testing.T) {
	unsetTestEnv(t, testParentLeaseSourceEnv)
	t.Setenv(testParentLeaseDaemonEnv, "stale")
	t.Setenv("WAKE_TEST_ENV_SENTINEL", "kept")
	cmd := exec.Command("unused") // nil Env means inherit the current process

	lease, err := passTestParentLease(cmd)
	if lease != nil {
		_ = lease.Close()
	}
	if err != nil {
		t.Fatalf("passTestParentLease with inherited environment: %v", err)
	}
	assertNoLeaseMarkers(t, cmd.Env)
	kept := false
	for _, entry := range cmd.Env {
		if entry == "WAKE_TEST_ENV_SENTINEL=kept" {
			kept = true
		}
	}
	if !kept {
		t.Fatal("cleaning private lease markers replaced a nil inherited environment with an empty child environment")
	}
}

func TestPassTestParentLeaseRejectsAnythingButAReadablePipe(t *testing.T) {
	for _, tc := range invalidLeaseFiles(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(testParentLeaseSourceEnv, fileDescriptor(tc.file))
			cmd := exec.Command("unused")
			cmd.Env = []string{testParentLeaseSourceEnv + "=stale", testParentLeaseDaemonEnv + "=stale"}

			lease, err := passTestParentLease(cmd)
			if lease != nil {
				_ = lease.Close()
			}
			if err == nil || !strings.Contains(err.Error(), "read end of an anonymous pipe") {
				t.Fatalf("passTestParentLease(%s) error = %v, want an actionable readable-pipe refusal", tc.name, err)
			}
			assertNoLeaseMarkers(t, cmd.Env)
		})
	}
}

func TestServeSideLeaseDefenseRejectsAnythingButAReadablePipe(t *testing.T) {
	for _, tc := range invalidLeaseFiles(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(testParentLeaseDaemonEnv, fileDescriptor(tc.file))
			_, release, err := withTestParentLease(context.Background())
			if release != nil {
				release()
			}
			if err == nil || !strings.Contains(err.Error(), "read end of an anonymous pipe") {
				t.Fatalf("withTestParentLease(%s) error = %v, want an actionable readable-pipe refusal", tc.name, err)
			}
		})
	}
}

func TestReadableAnonymousPipeIsAcceptedAtBothLeaseBoundaries(t *testing.T) {
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = readEnd.Close()
		_ = writeEnd.Close()
	})

	t.Setenv(testParentLeaseSourceEnv, fileDescriptor(readEnd))
	cmd := exec.Command("unused")
	cmd.Env = append(os.Environ(), testParentLeaseDaemonEnv+"=stale")
	lease, err := passTestParentLease(cmd)
	if err != nil {
		t.Fatalf("pass readable pipe: %v", err)
	}
	if lease == nil || len(cmd.ExtraFiles) != 1 {
		t.Fatalf("pass readable pipe returned lease %v and %d extra files, want one inherited read end", lease, len(cmd.ExtraFiles))
	}
	_ = lease.Close()
	assertNoLeaseSourceMarker(t, cmd.Env)

	t.Setenv(testParentLeaseDaemonEnv, fileDescriptor(readEnd))
	ctx, release, err := withTestParentLease(context.Background())
	if err != nil {
		t.Fatalf("serve readable pipe: %v", err)
	}
	if err := writeEnd.Close(); err != nil {
		t.Fatalf("close lease writer: %v", err)
	}
	<-ctx.Done()
	release()
}

type namedLeaseFile struct {
	name string
	file *os.File
}

func invalidLeaseFiles(t *testing.T) []namedLeaseFile {
	t.Helper()
	regular, err := os.CreateTemp(t.TempDir(), "lease")
	if err != nil {
		t.Fatalf("regular lease file: %v", err)
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("write-end pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = regular.Close()
		_ = null.Close()
		_ = readEnd.Close()
		_ = writeEnd.Close()
	})
	fifoPath := filepath.Join(t.TempDir(), "named-pipe")
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		t.Fatalf("named pipe: %v", err)
	}
	namedPipe, err := os.OpenFile(fifoPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open named pipe: %v", err)
	}
	t.Cleanup(func() { _ = namedPipe.Close() })
	return []namedLeaseFile{
		{name: "regular-file", file: regular},
		{name: "dev-null", file: null},
		{name: "pipe-write-end", file: writeEnd},
		{name: "named-pipe", file: namedPipe},
	}
}

func assertNoLeaseMarkers(t *testing.T, env []string) {
	t.Helper()
	for _, entry := range env {
		if strings.HasPrefix(entry, testParentLeaseSourceEnv+"=") ||
			strings.HasPrefix(entry, testParentLeaseDaemonEnv+"=") {
			t.Fatalf("private lease marker leaked into child environment: %q", entry)
		}
	}
}

func assertNoLeaseSourceMarker(t *testing.T, env []string) {
	t.Helper()
	for _, entry := range env {
		if strings.HasPrefix(entry, testParentLeaseSourceEnv+"=") {
			t.Fatalf("source lease marker leaked into daemon environment: %q", entry)
		}
	}
}

func unsetTestEnv(t *testing.T, name string) {
	t.Helper()
	was, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, was)
		} else {
			_ = os.Unsetenv(name)
		}
	})
}
