//go:build unix

package daemon

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

// passTestParentLease gives a scratch daemon the read end owned by its test
// runner. Product processes never set the source environment variable.
func passTestParentLease(cmd *exec.Cmd) (*os.File, error) {
	cmd.Env = withoutTestLeaseEnv(cmd.Env)
	raw, ok := os.LookupEnv(testParentLeaseSourceEnv)
	if !ok {
		return nil, nil
	}
	fd, err := inheritedFD(raw, testParentLeaseSourceEnv)
	if err != nil {
		return nil, err
	}
	if err := validateTestParentLeaseFD(fd, testParentLeaseSourceEnv); err != nil {
		return nil, err
	}
	dup, err := syscall.Dup(fd)
	if err != nil {
		return nil, fmt.Errorf("duplicate %s file descriptor %d: %w", testParentLeaseSourceEnv, fd, err)
	}
	file := os.NewFile(uintptr(dup), testParentLeaseSourceEnv)
	childFD := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, file)
	cmd.Env = append(cmd.Env, testParentLeaseDaemonEnv+"="+strconv.Itoa(childFD))
	return file, nil
}

// withTestParentLease turns EOF into ordinary context cancellation. Closing
// the descriptor on return also retires the watcher on every other exit path.
func withTestParentLease(parent context.Context) (context.Context, func(), error) {
	raw, ok, restore, err := takeTestLeaseEnvironment()
	if err != nil {
		return nil, restore, err
	}
	if !ok {
		return parent, restore, nil
	}
	fd, err := inheritedFD(raw, testParentLeaseDaemonEnv)
	if err != nil {
		return nil, restore, err
	}
	if err := validateTestParentLeaseFD(fd, testParentLeaseDaemonEnv); err != nil {
		return nil, restore, err
	}
	// ExtraFiles arrive blocking. os.File can make Close wake a concurrent
	// read only when the descriptor is pollable, so set this before NewFile.
	if err := syscall.SetNonblock(fd, true); err != nil {
		return nil, restore, fmt.Errorf("make test parent lease descriptor %d cancellable: %w", fd, err)
	}
	file := os.NewFile(uintptr(fd), testParentLeaseDaemonEnv)
	// ExtraFiles are inherited without close-on-exec. Set it before Serve can
	// admit an agent, or that child keeps the daemon's lease descriptor alive.
	syscall.CloseOnExec(fd)

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, file)
		cancel()
		close(done)
	}()
	return ctx, func() {
		cancel()
		_ = file.Close()
		<-done
		restore()
	}, nil
}

func inheritedFD(raw, env string) (int, error) {
	fd, err := strconv.Atoi(raw)
	if err != nil || fd < 3 {
		return 0, fmt.Errorf("%s must name an inherited file descriptor, got %q", env, raw)
	}
	return fd, nil
}

func validateTestParentLeaseFD(fd int, env string) error {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("%s must name the read end of an anonymous pipe; inspect descriptor %d: %w", env, fd, err)
	}
	if stat.Mode&syscall.S_IFMT != syscall.S_IFIFO {
		return fmt.Errorf("%s must name the read end of an anonymous pipe; descriptor %d is not a pipe", env, fd)
	}
	if err := validateAnonymousTestPipe(fd, &stat); err != nil {
		return fmt.Errorf("%s must name the read end of an anonymous pipe; descriptor %d: %w", env, fd, err)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), uintptr(syscall.F_GETFL), 0)
	if errno != 0 {
		return fmt.Errorf("%s must name the read end of an anonymous pipe; inspect descriptor %d access mode: %w", env, fd, errno)
	}
	if int(flags)&syscall.O_ACCMODE != syscall.O_RDONLY {
		return fmt.Errorf("%s must name the read end of an anonymous pipe; descriptor %d is not the read end", env, fd)
	}
	return nil
}
