//go:build unix

package core

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const agentLauncherFakeProcessEnv = "WAKE_AGENT_LAUNCHER_TEST_FAKE_PROCESS"

const (
	agentLauncherLifetimeRaceID       = "0a1b2c3d-0000-4000-8000-00000000f008"
	agentLauncherTestLifetimeRaceGate = "WAKE_AGENT_LAUNCHER_TEST_LIFETIME_RACE_GATE"
)

func TestAgentLauncherFakeProcess(t *testing.T) {
	switch os.Getenv(agentLauncherFakeProcessEnv) {
	case "early-ready":
		status := os.NewFile(agentLauncherStatusFD, "fake launcher status")
		if status == nil {
			t.Fatal("open fake launcher status")
		}
		if _, err := status.Write([]byte{agentLauncherReady}); err != nil {
			t.Fatalf("write early READY: %v", err)
		}
		_ = status.Close()
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "escaped-stdout":
		spawnOrphanHoldingStdout()
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
}

// Removing awaitAgentLauncher's context select, or waiting for its reader
// goroutine after closing the read end, makes this hang while the writer stays
// open. Cancellation must return independently of readiness EOF.
func TestAwaitAgentLauncherCancellationDoesNotWaitForReadinessEOF(t *testing.T) {
	readiness, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create readiness pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = readiness.Close()
		_ = writer.Close()
	})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() { result <- awaitAgentLauncher(ctx, readiness) }()

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("awaitAgentLauncher = %v, want context canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("awaitAgentLauncher waited for EOF after cancellation while a writer remained open")
	}
}

var errOwnershipRejected = errors.New("ownership publication rejected")

// Rejecting ownership must close CONTROL and reap the supervisor before the
// target recorder can run.
func TestStartObservedRejectsOwnershipFailure(t *testing.T) {
	launcher := launcherTestFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	recordPath := filepath.Join(t.TempDir(), "target-record.json")
	t.Setenv(agentLauncherTestRecordEnv, recordPath)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000a003",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	observed := make(chan int)
	started := make(chan error, 1)
	go func() {
		started <- s.StartObserved(ctx, func(_ context.Context, pgid int) error {
			observed <- pgid
			return errOwnershipRejected
		})
	}()

	select {
	case pgid := <-observed:
		if pgid <= 0 {
			t.Fatalf("StartObserved callback PGID = %d, want a real helper process group", pgid)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartObserved did not publish the helper process before readiness")
	}
	var err error
	select {
	case err = <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("StartObserved did not return after ownership rejection")
	}
	if !errors.Is(err, errOwnershipRejected) {
		t.Fatalf("StartObserved = %v, want ownership rejection", err)
	}
	waitForObservedStartEnd(t, s)
	if _, statErr := os.Stat(recordPath); statErr == nil {
		t.Fatal("claude target ran after ownership publication was rejected")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("checking target recorder absence: %v", statErr)
	}
}

// A failed launcher start must report its real cause on Session.Err(), not the
// launcher-lifetime teardown artifact its own kill leaves on fd5.
func TestStartObservedFailedStartReportsCauseNotLifetime(t *testing.T) {
	launcher := launcherTestFixture(t)
	recordPath := filepath.Join(t.TempDir(), "target-record.json")
	t.Setenv(agentLauncherTestRecordEnv, recordPath)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000a00a",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	observed := make(chan int, 1)
	started := make(chan error, 1)
	go func() {
		started <- s.StartObserved(context.Background(), func(_ context.Context, pgid int) error {
			observed <- pgid
			return errOwnershipRejected
		})
	}()
	waitForTestPGID(t, observed, "StartObserved did not publish the supervisor")
	err := waitForTestError(t, started, "StartObserved did not return after ownership rejection")
	if !errors.Is(err, errOwnershipRejected) {
		t.Fatalf("StartObserved = %v, want ownership rejection", err)
	}
	waitForObservedStartEnd(t, s)
	sessErr := s.Err()
	if sessErr == nil || !strings.Contains(sessErr.Error(), errOwnershipRejected.Error()) {
		t.Fatalf("Session.Err() = %v, want the ownership-rejection cause", sessErr)
	}
	if strings.Contains(sessErr.Error(), "launcher lifetime") {
		t.Fatalf("Session.Err() = %v, a lifetime teardown artifact masked the cause", sessErr)
	}
}

// Removing the starting guard lets a second Start enter execCommand while the
// first has no command to publish yet. Stop must also return and latch in that
// interval, so the first process cannot escape a stop already acknowledged.
func TestStartObservedGuardsAndStopsBeforeCommandPublication(t *testing.T) {
	original := execCommand
	entered := make(chan struct{}, 2)
	execRelease := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(execRelease) }) }
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		entered <- struct{}{}
		<-execRelease
		return fakeExec(ctx, name, args...)
	}
	t.Cleanup(func() {
		release()
		execCommand = original
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewSession(Config{SessionID: "pre-publication", Dir: t.TempDir()})
	started := make(chan error, 1)
	go func() { started <- s.StartObserved(ctx, nil) }()
	waitForTestSignal(t, entered, "first StartObserved did not reach the pre-publication exec gate")

	duplicate := make(chan error, 1)
	go func() { duplicate <- s.StartObserved(ctx, nil) }()
	err := waitForTestError(t, duplicate, "duplicate StartObserved entered execCommand before the first published its command")
	if err == nil || !strings.Contains(err.Error(), "already started") {
		t.Fatalf("duplicate StartObserved = %v, want already started", err)
	}

	stopped := make(chan error, 1)
	go func() { stopped <- s.Stop() }()
	if err := waitForTestError(t, stopped, "Stop blocked while StartObserved was inside execCommand"); err != nil {
		t.Fatalf("Stop before command publication: %v", err)
	}

	release()
	err = waitForTestError(t, started, "first StartObserved did not honor the pre-publication Stop")
	if err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("first StartObserved = %v, want stopped", err)
	}
	waitForObservedStartEnd(t, s)
}

// RELEASE belongs after durable ownership. While the callback is blocked, the
// real supervisor must be unable to enter the target at all.
func TestStartObservedOwnershipBlocksTargetUntilRelease(t *testing.T) {
	launcher := launcherTestFixture(t)
	recordPath := filepath.Join(t.TempDir(), "target-record.json")
	t.Setenv(agentLauncherTestRecordEnv, recordPath)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000a001",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	callbackRelease := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(callbackRelease) }) }
	t.Cleanup(release)
	observed := make(chan int, 1)
	started := make(chan error, 1)
	go func() {
		started <- s.StartObserved(context.Background(), func(ctx context.Context, pgid int) error {
			observed <- pgid
			select {
			case <-callbackRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()

	if pgid := waitForTestPGID(t, observed, "StartObserved did not publish the supervisor"); pgid <= 0 {
		t.Fatalf("StartObserved callback PGID = %d, want positive", pgid)
	}
	assertFileAbsentFor(t, recordPath, 500*time.Millisecond)
	release()
	if err := waitForTestError(t, started, "StartObserved did not receive target READY"); err != nil {
		t.Fatalf("StartObserved after ownership release: %v", err)
	}
	waitForFile(t, recordPath)
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	drain(s)
}

// A non-nil observer promises a pre-exec barrier. The direct path has none and
// must refuse that promise before the mutable exec seam is reached.
func TestStartObservedRejectsOwnershipWithoutLauncherBeforeExec(t *testing.T) {
	original := execCommand
	reached := 0
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		reached++
		return fakeExec(ctx, name, args...)
	}
	t.Cleanup(func() { execCommand = original })
	s := NewSession(Config{SessionID: "no-launcher", Dir: t.TempDir()})

	err := s.StartObserved(context.Background(), func(context.Context, int) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "requires a Wake agent launcher") {
		t.Fatalf("StartObserved = %v, want launcher requirement", err)
	}
	if reached != 0 {
		t.Fatalf("execCommand reached %d times, want 0", reached)
	}
}

// Killing the real supervisor before it can report an outcome must produce an
// explicit launch failure; STATUS EOF is never target readiness.
func TestStartObservedSupervisorEOFIsNotReady(t *testing.T) {
	launcher := launcherTestFixture(t)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000a002",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	err := s.StartObserved(context.Background(), func(_ context.Context, pgid int) error {
		if err := KillGroup(pgid); err != nil {
			return err
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "launcher exited before reporting target outcome") {
		t.Fatalf("StartObserved = %v, want explicit launcher EOF failure", err)
	}
	waitForObservedStartEnd(t, s)
}

func TestAgentLauncherSupervisorChdirKillStartsNoTarget(t *testing.T) {
	launcher := launcherTestFixture(t)
	gatePath, release := newAgentLauncherTestGate(t)
	t.Setenv(agentLauncherTestSupervisorEnv, "hold-chdir")
	t.Setenv(agentLauncherTestChdirGateEnv, gatePath)
	heldPath := filepath.Join(t.TempDir(), "chdir-held")
	recordPath := filepath.Join(t.TempDir(), "target-record.json")
	t.Setenv(agentLauncherTestChdirHeldEnv, heldPath)
	t.Setenv(agentLauncherTestRecordEnv, recordPath)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); release() })
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000f006",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	observed := make(chan int, 1)
	started := make(chan error, 1)
	go func() {
		started <- s.StartObserved(ctx, func(_ context.Context, pgid int) error {
			observed <- pgid
			return nil
		})
	}()
	pgid := waitForTestPGID(t, observed, "ownership callback did not receive chdir supervisor")
	waitForChdirHold(t, heldPath, recordPath)
	if err := KillGroup(pgid); err != nil {
		t.Fatalf("kill held chdir supervisor: %v", err)
	}
	err := waitForTestError(t, started, "Start did not report held chdir supervisor death")
	if err == nil || !strings.Contains(err.Error(), "launcher exited") {
		t.Fatalf("Start = %v, want launcher exit", err)
	}
	waitForObservedStartEnd(t, s)
	assertFileAbsent(t, recordPath)
}

func TestAgentLauncherLostLifetimeReaderPreservesTargetExit(t *testing.T) {
	launcher := launcherTestFixture(t)
	pipes, extraFiles, err := makeAgentLauncherPipes()
	if err != nil {
		t.Fatalf("make launcher pipes: %v", err)
	}
	cmd := exec.Command(launcher.executable, agentLauncherArg, "--session-id", "0a1b2c3d-0000-4000-8000-00000000f007")
	cmd.Env = append(scrubbedEnv(os.Environ()),
		agentLauncherMarkerEnv+"="+agentLauncherProtocol,
		agentLauncherDirEnv+"="+t.TempDir(),
	)
	cmd.ExtraFiles = extraFiles
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	setProcessGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("supervisor stdin: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	t.Cleanup(func() {
		_ = killProcessGroup(cmd)
		if cmd.ProcessState == nil {
			_, _ = cmd.Process.Wait()
		}
	})
	closeAgentCommandExtraFiles(cmd)
	if err := writeAgentLauncher(pipes.control, []byte{agentLauncherRelease}); err != nil {
		t.Fatalf("write RELEASE: %v", err)
	}
	_ = pipes.control.Close()
	if err := awaitAgentLauncher(context.Background(), pipes.status); err != nil {
		t.Fatalf("await READY: %v", err)
	}
	_ = pipes.lifetime.Close()
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("supervisor exit after lost lifetime reader: %v", err)
	}
}

// stdout can EOF before the supervisor resolves its lifetime: the target exits
// and the supervisor closes the stream, then holds the lifetime pipe open. The
// session must stay open until the supervisor actually dies - awaitExit reaps
// it, the stream's EOF does not - and then report the lifetime failure even
// though the scan itself ended cleanly. The lifetimeSettled gate is what makes
// that report deterministic rather than a race against the watcher.
func TestAgentLauncherStdoutBeforeLifetimeFailureStaysOpenThenReports(t *testing.T) {
	launcher := launcherTestFixture(t)
	gatePath, release := newAgentLauncherTestGate(t)
	t.Setenv(agentLauncherTestSupervisorEnv, "stdout-before-lifetime-eof")
	t.Setenv(agentLauncherTestLifetimeRaceGate, gatePath)

	s := NewSession(Config{SessionID: agentLauncherLifetimeRaceID, Dir: t.TempDir(), Launcher: launcher})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ended := drainedAsync(s)
	// The supervisor marks its gate open, then closes stdout and blocks. Once the
	// marker is up the stream has EOF'd but the supervisor is still alive on the
	// gate, so awaitExit's Wait has not returned - the session must not have
	// ended on the stream's EOF alone.
	waitForFile(t, gatePath+".opened")
	select {
	case <-ended:
		t.Fatal("session ended on stdout EOF while its supervisor was still alive")
	case <-time.After(200 * time.Millisecond):
	}
	release()
	waitForTestSignal(t, ended, "session did not end after the supervisor died without DONE")
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), "before DONE") {
		t.Fatalf("session error = %v, want lifetime failure after stdout EOF", err)
	}
}

// A scanner failure - an oversized stdout line - is the true cause of the
// stop, and the launcher-lifetime failure that its own group kill provokes must
// not mask it. This exercises the real supervisor path end to end.
func TestAgentLauncherOversizedOutputOutranksLifetimeFailure(t *testing.T) {
	launcher := launcherTestFixture(t)
	s := NewSession(Config{SessionID: agentLauncherOversizedID, Dir: t.TempDir(), Launcher: launcher})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	drain(s)
	err := s.Err()
	if err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("session error = %v, want scanner token-too-long cause", err)
	}
	if strings.Contains(err.Error(), "before DONE") {
		t.Fatalf("session error = %v, lifetime failure masked the scanner cause", err)
	}
}

func runAgentLauncherStdoutBeforeLifetimeEOF() error {
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
		return err
	}
	cmd, err := agentLauncherTarget()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := reportAgentLauncherReady(status); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil {
		return err
	}
	// Open the gate reader, then mark it open before doing anything else. The
	// test releases only after seeing that marker, so the supervisor's O_RDONLY
	// fd is provably present when the release byte is written - a FIFO whose last
	// fd closes before the reader opens discards the byte, and under -race the
	// old blind grace let release() run and close the writer first.
	gatePath := os.Getenv(agentLauncherTestLifetimeRaceGate)
	gate, err := os.Open(gatePath)
	if err != nil {
		return err
	}
	defer func() { _ = gate.Close() }()
	if err := os.WriteFile(gatePath+".opened", []byte{1}, 0o600); err != nil {
		return err
	}
	// Close stdout so the parent pump crosses EOF and reaches finish, then hold
	// the lifetime pipe open until the test releases the gate.
	_ = os.Stdout.Close()
	var release [1]byte
	_, err = io.ReadFull(gate, release[:])
	return err
}

// Cancellation owns helper teardown, not callback completion. A callback that
// observes cancellation and remains blocked cannot delay Start or reap.
func TestStartObservedCallbackCancellationDoesNotWait(t *testing.T) {
	launcher := launcherTestFixture(t)
	recordPath := filepath.Join(t.TempDir(), "target-record.json")
	t.Setenv(agentLauncherTestRecordEnv, recordPath)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000a003",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	ctx, cancel := context.WithCancel(context.Background())
	callbackRelease := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(callbackRelease) }) }
	t.Cleanup(func() { cancel(); release() })
	observed := make(chan int, 1)
	callbackCanceled := make(chan struct{})
	started := make(chan error, 1)
	go func() {
		started <- s.StartObserved(ctx, func(ownershipCtx context.Context, pgid int) error {
			observed <- pgid
			<-ownershipCtx.Done()
			close(callbackCanceled)
			<-callbackRelease
			return ownershipCtx.Err()
		})
	}()

	pgid := waitForTestPGID(t, observed, "ownership callback did not receive the supervisor PGID")
	cancel()
	waitForTestSignal(t, callbackCanceled, "ownership context was not cancelled")
	err := waitForTestError(t, started, "StartObserved waited for the cancelled ownership callback")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StartObserved = %v, want context canceled", err)
	}
	waitForGroupGone(t, pgid)
	waitForObservedStartEnd(t, s)
	assertFileAbsent(t, recordPath)
	release()
}

// A STATUS frame before RELEASE is a protocol failure, even when it claims
// READY. The fake is a real child with the same fd layout as the supervisor.
func TestAgentLauncherProtocolRejectsStatusBeforeRelease(t *testing.T) {
	withAgentLauncherFakeProcess(t, "early-ready")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := NewSession(Config{
		SessionID: "early-status",
		Dir:       t.TempDir(),
		Launcher:  AgentLauncher{executable: "fake-launcher"},
	})
	callbackRelease := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(callbackRelease) }) }
	t.Cleanup(release)
	started := make(chan error, 1)
	go func() {
		started <- s.StartObserved(ctx, func(ctx context.Context, _ int) error {
			select {
			case <-callbackRelease:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	var err error
	select {
	case err = <-started:
	case <-time.After(5 * time.Second):
		cancel()
		release()
		t.Fatal("StartObserved did not reject STATUS before RELEASE")
	}
	if err == nil || !strings.Contains(err.Error(), "before ownership release") {
		t.Fatalf("StartObserved = %v, want early STATUS failure", err)
	}
	waitForObservedStartEnd(t, s)
}

var errEscapedStdout = errors.New("ownership rejected with escaped stdout")

// An escaped setsid descendant can keep the stdout writer after the launch
// group is dead. Failed-start cleanup must close Wake's reader before draining.
func TestFailedStartClosesStdoutHeldByEscapedDescendant(t *testing.T) {
	withAgentLauncherFakeProcess(t, "escaped-stdout")
	pidPath := filepath.Join(t.TempDir(), "orphan.pid")
	t.Setenv("WAKE_HELPER_PIDFILE", pidPath)
	s := NewSession(Config{
		SessionID: "escaped-stdout",
		Dir:       t.TempDir(),
		Launcher:  AgentLauncher{executable: "fake-launcher"},
	})
	started := make(chan error, 1)
	go func() {
		started <- s.StartObserved(context.Background(), func(context.Context, int) error {
			if err := waitForFileResult(pidPath); err != nil {
				return err
			}
			return errEscapedStdout
		})
	}()

	select {
	case err := <-started:
		if !errors.Is(err, errEscapedStdout) {
			t.Fatalf("StartObserved = %v, want escaped stdout rejection", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("failed Start waited on stdout held by escaped descendant")
	}
	waitForObservedStartEnd(t, s)
	pid := readPID(t, pidPath)
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("escaped descendant was not alive after supervisor reap: %v", err)
	}
	if err := KillGroup(pid); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("clean escaped descendant: %v", err)
	}
	waitForGroupGone(t, pid)
}

// Stop can latch while ownership is blocked. Even a later successful callback
// must not RELEASE the target after that acknowledged stop.
func TestStartObservedStopWinsBeforeOwnershipRelease(t *testing.T) {
	launcher := launcherTestFixture(t)
	recordPath := filepath.Join(t.TempDir(), "target-record.json")
	t.Setenv(agentLauncherTestRecordEnv, recordPath)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000a004",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	callbackEntered := make(chan struct{})
	callbackRelease := make(chan struct{})
	started := make(chan error, 1)
	go func() {
		started <- s.StartObserved(context.Background(), func(context.Context, int) error {
			close(callbackEntered)
			<-callbackRelease
			return nil
		})
	}()
	select {
	case <-callbackEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("ownership callback did not start")
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop before ownership release: %v", err)
	}
	close(callbackRelease)
	select {
	case err := <-started:
		if err == nil || !strings.Contains(err.Error(), "stopped") {
			t.Fatalf("StartObserved = %v, want stopped", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("StartObserved did not honor Stop")
	}
	waitForObservedStartEnd(t, s)
	assertFileAbsent(t, recordPath)
}

func waitForObservedStartEnd(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-drainedAsync(s):
	case <-time.After(5 * time.Second):
		t.Fatal("session process was not reaped after failed StartObserved")
	}
}

func withAgentLauncherFakeProcess(t *testing.T, mode string) {
	t.Helper()
	t.Setenv(agentLauncherFakeProcessEnv, mode)
	original := execCommand
	execCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestAgentLauncherFakeProcess$")
	}
	t.Cleanup(func() { execCommand = original })
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("stat %s: %v", path, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("file %s did not appear", path)
}

func waitForFileResult(path string) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("fake process did not record escaped descendant PID")
}

func assertFileAbsentFor(t *testing.T, path string, duration time.Duration) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		assertFileAbsent(t, path)
		time.Sleep(10 * time.Millisecond)
	}
}

func assertFileAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("unexpected target record %s", path)
	} else if !os.IsNotExist(err) {
		t.Fatalf("check target record %s: %v", path, err)
	}
}

func waitForGroupGone(t *testing.T, pgid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pgid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process group %d remained alive", pgid)
}

func readPID(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatalf("parse pid %q: %v", raw, err)
	}
	return pid
}

func newAgentLauncherTestGate(t *testing.T) (string, func()) {
	t.Helper()
	gatePath := filepath.Join(t.TempDir(), "launcher-gate")
	if err := syscall.Mkfifo(gatePath, 0o600); err != nil {
		t.Fatalf("create launcher gate: %v", err)
	}
	gate, err := os.OpenFile(gatePath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open launcher gate: %v", err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			_, _ = gate.Write([]byte{1})
			_ = gate.Close()
		})
	}
	t.Cleanup(release)
	return gatePath, release
}

func waitForChdirHold(t *testing.T, heldPath, recordPath string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(recordPath); err == nil {
			t.Fatal("target started before supervisor completed requested chdir")
		}
		if _, err := os.Stat(heldPath); err == nil {
			return
		} else if !os.IsNotExist(err) {
			t.Fatalf("check chdir hold: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("supervisor did not enter requested chdir")
}

func waitForTestSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal(failure)
	}
}

func waitForTestError(t *testing.T, result <-chan error, failure string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal(failure)
		return nil
	}
}

func waitForTestPGID(t *testing.T, result <-chan int, failure string) int {
	t.Helper()
	select {
	case pgid := <-result:
		return pgid
	case <-time.After(5 * time.Second):
		t.Fatal(failure)
		return 0
	}
}
