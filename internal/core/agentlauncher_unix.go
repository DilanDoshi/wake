//go:build unix

package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

func SelfAgentLauncher() (AgentLauncher, error) {
	executable, err := os.Executable()
	if err != nil {
		return AgentLauncher{}, fmt.Errorf("resolve Wake executable: %w", err)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return AgentLauncher{}, fmt.Errorf("make Wake executable absolute: %w", err)
	}
	return AgentLauncher{executable: executable}, nil
}

func AgentLauncherRequested() bool {
	// Both the marker and the argument, so a stray WAKE_AGENT_LAUNCHER left in
	// the ambient environment cannot turn an ordinary `wake status` into a
	// supervisor that fails its own argv check and exits. A real supervisor gets
	// both from agentCommand; nothing else sets either.
	return os.Getenv(agentLauncherMarkerEnv) == agentLauncherProtocol &&
		len(os.Args) >= 2 && os.Args[1] == agentLauncherArg
}

var agentLauncherChdir = os.Chdir

func RunAgentLauncher() error {
	if !AgentLauncherRequested() || len(os.Args) < 2 || os.Args[1] != agentLauncherArg {
		return errors.New("invalid Wake agent launcher request")
	}
	control, status, lifetime, err := openAgentLauncherProtocol()
	if err != nil {
		return err
	}
	defer func() {
		_ = control.Close()
		_ = status.Close()
		_ = lifetime.Close()
	}()
	if err := readAgentLauncherRelease(control); err != nil {
		return reportAgentLauncherFailure(status, err)
	}
	cmd, err := agentLauncherTarget()
	if err != nil {
		return reportAgentLauncherFailure(status, err)
	}
	if err := cmd.Start(); err != nil {
		return reportAgentLauncherFailure(status, fmt.Errorf("start %s: %w", cmd.Path, err))
	}
	readyErr := reportAgentLauncherReady(status)
	if readyErr != nil {
		_ = status.Close()
	}
	waitErr := cmd.Wait()
	// The parent may already be gone; target outcome remains authoritative.
	_ = reportAgentLauncherDone(lifetime)
	if waitErr != nil {
		return propagateAgentLauncherTargetExit(cmd, waitErr)
	}
	return readyErr
}

func openAgentLauncherProtocol() (*os.File, *os.File, *os.File, error) {
	control := os.NewFile(agentLauncherControlFD, "Wake agent launcher control")
	status := os.NewFile(agentLauncherStatusFD, "Wake agent launcher status")
	lifetime := os.NewFile(agentLauncherLifetimeFD, "Wake agent launcher lifetime")
	if control == nil || status == nil || lifetime == nil {
		if control != nil {
			_ = control.Close()
		}
		if status != nil {
			_ = status.Close()
		}
		if lifetime != nil {
			_ = lifetime.Close()
		}
		return nil, nil, nil, errors.New("open Wake agent launcher protocol fds 3, 4, and 5")
	}
	if _, err := control.Stat(); err != nil {
		_ = control.Close()
		_ = status.Close()
		_ = lifetime.Close()
		return nil, nil, nil, fmt.Errorf("open Wake agent launcher control fd 3: %w", err)
	}
	if _, err := status.Stat(); err != nil {
		_ = control.Close()
		_ = status.Close()
		_ = lifetime.Close()
		return nil, nil, nil, fmt.Errorf("open Wake agent launcher status fd 4: %w", err)
	}
	if _, err := lifetime.Stat(); err != nil {
		_ = control.Close()
		_ = status.Close()
		_ = lifetime.Close()
		return nil, nil, nil, fmt.Errorf("open Wake agent launcher lifetime fd 5: %w", err)
	}
	// The target inherits stdio, never the supervisor's protocol lifetime.
	syscall.CloseOnExec(int(control.Fd()))
	syscall.CloseOnExec(int(status.Fd()))
	syscall.CloseOnExec(int(lifetime.Fd()))
	return control, status, lifetime, nil
}

func readAgentLauncherRelease(control *os.File) error {
	frame, err := io.ReadAll(io.LimitReader(control, 2))
	if err != nil {
		return fmt.Errorf("read Wake agent launcher control: %w", err)
	}
	if len(frame) != 1 || frame[0] != agentLauncherRelease {
		return fmt.Errorf("invalid Wake agent launcher RELEASE frame %x", frame)
	}
	if err := control.Close(); err != nil {
		return fmt.Errorf("close Wake agent launcher control: %w", err)
	}
	return nil
}

func agentLauncherTarget() (*exec.Cmd, error) {
	target, err := exec.LookPath(claudeBinary)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", claudeBinary, err)
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("make %s path absolute: %w", claudeBinary, err)
	}
	dir := os.Getenv(agentLauncherDirEnv)
	if dir == "" {
		return nil, errors.New("wake agent launcher directory is empty")
	}
	if err := agentLauncherChdir(dir); err != nil {
		return nil, fmt.Errorf("change directory to %s: %w", dir, err)
	}
	cmd := exec.Command(target, os.Args[2:]...)
	cmd.Args[0] = claudeBinary
	cmd.Env = scrubbedEnv(os.Environ())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd, nil
}

func propagateAgentLauncherTargetExit(cmd *exec.Cmd, err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("wait for %s: %w", cmd.Path, err)
	}
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok {
		return err
	}
	if status.Signaled() {
		return signalAgentLauncher(status.Signal())
	}
	os.Exit(status.ExitStatus())
	return nil
}

func signalAgentLauncher(sig syscall.Signal) error {
	signal.Reset(sig)
	if err := syscall.Kill(os.Getpid(), sig); err != nil {
		return fmt.Errorf("propagate target signal %s: %w", sig, err)
	}
	select {}
}

func agentCommand(ctx context.Context, launcher AgentLauncher, dir string, args []string) (*exec.Cmd, *agentLauncherPipes, error) {
	if launcher.executable == "" {
		cmd := execCommand(ctx, claudeBinary, args...)
		cmd.Dir = dir
		return cmd, nil, nil
	}
	pipes, extraFiles, err := makeAgentLauncherPipes()
	if err != nil {
		return nil, nil, err
	}
	cmd := execCommand(ctx, launcher.executable, append([]string{agentLauncherArg}, args...)...)
	cmd.Env = append(scrubbedEnv(os.Environ()),
		agentLauncherMarkerEnv+"="+agentLauncherProtocol,
		agentLauncherDirEnv+"="+dir,
	)
	cmd.ExtraFiles = extraFiles
	return cmd, pipes, nil
}

func makeAgentLauncherPipes() (*agentLauncherPipes, []*os.File, error) {
	controlReader, controlWriter, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("create Wake agent launcher control pipe: %w", err)
	}
	statusReader, statusWriter, err := os.Pipe()
	if err != nil {
		_ = controlReader.Close()
		_ = controlWriter.Close()
		return nil, nil, fmt.Errorf("create Wake agent launcher status pipe: %w", err)
	}
	lifetimeReader, lifetimeWriter, err := os.Pipe()
	if err != nil {
		_ = controlReader.Close()
		_ = controlWriter.Close()
		_ = statusReader.Close()
		_ = statusWriter.Close()
		return nil, nil, fmt.Errorf("create Wake agent launcher lifetime pipe: %w", err)
	}
	pipes := &agentLauncherPipes{control: controlWriter, status: statusReader, lifetime: lifetimeReader}
	return pipes, []*os.File{controlReader, statusWriter, lifetimeWriter}, nil
}
