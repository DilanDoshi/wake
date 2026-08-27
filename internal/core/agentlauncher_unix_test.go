//go:build unix

package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	agentLauncherTestRecordEnv     = "WAKE_AGENT_LAUNCHER_TEST_RECORD"
	agentLauncherTestSupervisorEnv = "WAKE_AGENT_LAUNCHER_TEST_SUPERVISOR"
	agentLauncherTestStartedEnv    = "WAKE_AGENT_LAUNCHER_TEST_STARTED"
	agentLauncherTestControlIDEnv  = "WAKE_AGENT_LAUNCHER_TEST_CONTROL_ID"
	agentLauncherTestStatusIDEnv   = "WAKE_AGENT_LAUNCHER_TEST_STATUS_ID"
	agentLauncherTestLifetimeIDEnv = "WAKE_AGENT_LAUNCHER_TEST_LIFETIME_ID"
	agentLauncherTestExitGateEnv   = "WAKE_AGENT_LAUNCHER_TEST_EXIT_GATE"
	agentLauncherTestChdirGateEnv  = "WAKE_AGENT_LAUNCHER_TEST_CHDIR_GATE"
	agentLauncherTestChdirHeldEnv  = "WAKE_AGENT_LAUNCHER_TEST_CHDIR_HELD"
	agentLauncherTestWedgePidEnv   = "WAKE_AGENT_LAUNCHER_TEST_WEDGE_PID"
	agentLauncherInstantID         = "0a1b2c3d-0000-4000-8000-00000000f001"
	agentLauncherIOID              = "0a1b2c3d-0000-4000-8000-00000000f002"
	agentLauncherSignalID          = "0a1b2c3d-0000-4000-8000-00000000f004"
	agentLauncherOversizedID       = "0a1b2c3d-0000-4000-8000-00000000f009"
	agentLauncherWedgeID           = "0a1b2c3d-0000-4000-8000-00000000f00a"
	agentLauncherIOStderr          = "agent launcher target stderr"
)

type agentLauncherRecord struct {
	Cwd                   string
	Args                  []string
	PID                   int
	PGID                  int
	LauncherMarkerPresent bool
	LauncherDirPresent    bool
	ControlPipeInherited  bool
	StatusPipeInherited   bool
	LifetimePipeInherited bool
}

func TestMain(m *testing.M) {
	if os.Getenv(agentLauncherFakeProcessEnv) != "" {
		os.Exit(m.Run())
	}
	if AgentLauncherRequested() {
		if err := recordAgentLauncherProtocolIdentity(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		switch os.Getenv(agentLauncherTestSupervisorEnv) {
		case "exit-after-target-start":
			if err := runAgentLauncherTargetWithoutReady(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		case "exit-after-ready":
			if err := runAgentLauncherTargetReadyWithoutDone(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		case "hold-chdir":
			agentLauncherChdir = holdAgentLauncherChdir
		case "stdout-before-lifetime-eof":
			if err := runAgentLauncherStdoutBeforeLifetimeEOF(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			os.Exit(0)
		}
		if err := RunAgentLauncher(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Args[0] == claudeBinary {
		recordAgentLauncherTarget()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func recordAgentLauncherTarget() {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "target cwd:", err)
		return
	}
	record := agentLauncherRecord{
		Cwd:  cwd,
		Args: os.Args,
		PID:  os.Getpid(),
		PGID: syscall.Getpgrp(),
	}
	_, record.LauncherMarkerPresent = os.LookupEnv(agentLauncherMarkerEnv)
	_, record.LauncherDirPresent = os.LookupEnv(agentLauncherDirEnv)
	record.ControlPipeInherited = inheritedAgentLauncherPipe(agentLauncherControlFD, agentLauncherTestControlIDEnv)
	record.StatusPipeInherited = inheritedAgentLauncherPipe(agentLauncherStatusFD, agentLauncherTestStatusIDEnv)
	record.LifetimePipeInherited = inheritedAgentLauncherPipe(agentLauncherLifetimeFD, agentLauncherTestLifetimeIDEnv)
	raw, err := json.Marshal(record)
	if err != nil {
		fmt.Fprintln(os.Stderr, "record target:", err)
		return
	}
	if path := os.Getenv(agentLauncherTestRecordEnv); path != "" {
		if err := os.WriteFile(path, raw, 0o600); err != nil {
			fmt.Fprintln(os.Stderr, "write target record:", err)
			return
		}
	}
	_, _ = fmt.Fprintln(os.Stdout, string(raw))
	switch agentLauncherTargetSessionID() {
	case agentLauncherInstantID:
		return
	case agentLauncherIOID:
		recordAgentLauncherIO()
		return
	case agentLauncherSignalID:
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {}
	case agentLauncherLifetimeRaceID:
		return
	case agentLauncherOversizedID:
		writeOversizedAgentLauncherLine()
		return
	case agentLauncherWedgeID:
		spawnSupervisedOrphanHoldingStdout()
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// spawnSupervisedOrphanHoldingStdout is spawnOrphanHoldingStdout one process
// deeper: the target here inherited the supervisor's fd 1, which is the write
// end of the pipe Wake reads, so a grandchild that inherits it and leaves the
// group holds Wake's read end open past both the target's and the supervisor's
// exit. That is the supervised wedge - the scan never EOFs and only awaitExit's
// force-close after reaping the supervisor can end it. os.Executable rather than
// os.Args[0], which the target carries as the bare "claude" name.
func spawnSupervisedOrphanHoldingStdout() {
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "supervised orphan self:", err)
		return
	}
	cmd := exec.Command(self, "-test.run=TestHelperProcess", "--")
	cmd.Env = append(os.Environ(), "WAKE_WANT_HELPER=1", "WAKE_HELPER_LINGER=1")
	cmd.Stdout = os.Stdout
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "supervised orphan:", err)
		return
	}
	if path := os.Getenv(agentLauncherTestWedgePidEnv); path != "" {
		_ = os.WriteFile(path, []byte(fmt.Sprintf("%d", cmd.Process.Pid)), 0o600)
	}
	// The turn before the wedge, so the test can prove the force-close after Wait
	// does not truncate what the scan already read. The target then returns and
	// exits, leaving the grandchild holding the read end.
	emitTurns()
}

func recordAgentLauncherProtocolIdentity() error {
	control, err := agentLauncherFDIdentity(agentLauncherControlFD)
	if err != nil {
		return fmt.Errorf("record launcher control identity: %w", err)
	}
	status, err := agentLauncherFDIdentity(agentLauncherStatusFD)
	if err != nil {
		return fmt.Errorf("record launcher status identity: %w", err)
	}
	if err := os.Setenv(agentLauncherTestControlIDEnv, control); err != nil {
		return err
	}
	if err := os.Setenv(agentLauncherTestStatusIDEnv, status); err != nil {
		return err
	}
	lifetime, err := agentLauncherFDIdentity(agentLauncherLifetimeFD)
	if err != nil {
		return fmt.Errorf("record launcher lifetime identity: %w", err)
	}
	return os.Setenv(agentLauncherTestLifetimeIDEnv, lifetime)
}

func agentLauncherFDIdentity(fd int) (string, error) {
	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", stat.Dev, stat.Ino), nil
}

func inheritedAgentLauncherPipe(fd int, identityEnv string) bool {
	want := os.Getenv(identityEnv)
	got, err := agentLauncherFDIdentity(fd)
	return want != "" && err == nil && got == want
}

func runAgentLauncherTargetWithoutReady() error {
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
	if path := os.Getenv(agentLauncherTestStartedEnv); path != "" {
		return os.WriteFile(path, []byte(fmt.Sprint(cmd.Process.Pid)), 0o600)
	}
	return nil
}

func runAgentLauncherTargetReadyWithoutDone() error {
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
	return waitAgentLauncherTestGate(os.Getenv(agentLauncherTestExitGateEnv))
}

func holdAgentLauncherChdir(dir string) error {
	if path := os.Getenv(agentLauncherTestChdirHeldEnv); path != "" {
		if err := os.WriteFile(path, []byte(fmt.Sprint(os.Getpid())), 0o600); err != nil {
			return err
		}
	}
	if err := waitAgentLauncherTestGate(os.Getenv(agentLauncherTestChdirGateEnv)); err != nil {
		return err
	}
	return os.Chdir(dir)
}

func waitAgentLauncherTestGate(path string) error {
	gate, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = gate.Close() }()
	var release [1]byte
	_, err = io.ReadFull(gate, release[:])
	return err
}

func agentLauncherTargetSessionID() string {
	for i := 0; i+1 < len(os.Args); i++ {
		if os.Args[i] == "--session-id" {
			return os.Args[i+1]
		}
	}
	return ""
}

// writeOversizedAgentLauncherLine floods the inherited stdout with a single
// unterminated line past maxLineBytes, so the parent scanner fails with
// ErrTooLong. The pump then kills this target's supervisor group, so the write
// loop ends only by the group signal - it never returns cleanly.
func writeOversizedAgentLauncherLine() {
	chunk := bytes.Repeat([]byte("x"), 64*1024)
	for {
		if _, err := os.Stdout.Write(chunk); err != nil {
			return
		}
	}
}

func recordAgentLauncherIO() {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		fmt.Fprintln(os.Stderr, "target stdin:", err)
		os.Exit(22)
	}
	raw, _ := json.Marshal(strings.TrimSpace(line))
	_, _ = fmt.Fprintf(os.Stdout, `{"type":"agent_launcher_io","input":%s}`+"\n", raw)
	_, _ = fmt.Fprintln(os.Stderr, agentLauncherIOStderr)
	os.Exit(23)
}

func launcherTestFixture(t *testing.T) AgentLauncher {
	t.Helper()

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	binDir := t.TempDir()
	if err := os.Symlink(executable, filepath.Join(binDir, claudeBinary)); err != nil {
		t.Fatalf("symlink claude: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	launcher, err := SelfAgentLauncher()
	if err != nil {
		t.Fatalf("SelfAgentLauncher: %v", err)
	}
	return launcher
}

// The supervisor must preserve the target's literal cwd/argv while removing
// both private launcher variables from its environment.
func TestAgentLauncherPreservesTargetAndScrubsPrivateEnvironment(t *testing.T) {
	s, wantDir, got := startRecordedAgentLauncher(t)
	assertAgentLauncherTarget(t, s, wantDir, got)
	if err := s.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("Start returned after target exited: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	drain(s)
	if err := s.Err(); err != nil {
		t.Fatalf("target exit: %v", err)
	}
}

func startRecordedAgentLauncher(t *testing.T) (*Session, string, agentLauncherRecord) {
	t.Helper()
	launcher := launcherTestFixture(t)
	wantDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve target directory: %v", err)
	}
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000feed",
		Dir:       wantDir,
		Launcher:  launcher,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ev, ok := <-s.Events()
	if !ok {
		t.Fatalf("target exited before recording itself: %v", s.Err())
	}
	var got agentLauncherRecord
	if err := json.Unmarshal(ev.Raw, &got); err != nil {
		t.Fatalf("decode target record: %v", err)
	}
	return s, wantDir, got
}

func assertAgentLauncherTarget(t *testing.T, s *Session, wantDir string, got agentLauncherRecord) {
	t.Helper()
	if got.Cwd != wantDir {
		t.Fatalf("cwd = %q, want %q", got.Cwd, wantDir)
	}
	if got.LauncherMarkerPresent {
		t.Fatal("WAKE_AGENT_LAUNCHER reached claude")
	}
	if got.LauncherDirPresent {
		t.Fatal("WAKE_AGENT_LAUNCHER_DIR reached claude")
	}
	if got.ControlPipeInherited {
		t.Fatal("agent launcher CONTROL pipe reached claude")
	}
	if got.StatusPipeInherited {
		t.Fatal("agent launcher STATUS pipe reached claude")
	}
	if got.LifetimePipeInherited {
		t.Fatal("agent launcher LIFETIME pipe reached claude")
	}
	wantArgs := []string{
		claudeBinary,
		"--print",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--permission-prompt-tool", "stdio",
		"--brief",
		"--include-hook-events",
		"--forward-subagent-text",
		"--include-partial-messages",
		"--session-id", "0a1b2c3d-0000-4000-8000-00000000feed",
		"--permission-mode", "auto",
	}
	if !slices.Equal(got.Args, wantArgs) {
		t.Fatalf("argv = %q, want %q", got.Args, wantArgs)
	}
	if got.PGID != s.Pgid() {
		t.Fatalf("target process group = %d, want supervisor group %d", got.PGID, s.Pgid())
	}
	if got.PID == s.Pgid() {
		t.Fatalf("target PID = supervisor PGID %d; supervisor did not persist", got.PID)
	}
}

// READY means cmd.Start succeeded, not that the target stayed alive long
// enough for the parent to observe it.
func TestAgentLauncherReadyAcceptsInstantTargetExit(t *testing.T) {
	launcher := launcherTestFixture(t)
	s := NewSession(Config{SessionID: agentLauncherInstantID, Dir: t.TempDir(), Launcher: launcher})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start instant target: %v", err)
	}
	drain(s)
	if err := s.Err(); err != nil {
		t.Fatalf("instant target exit: %v", err)
	}
}

// The persistent supervisor must pass all three standard streams through and
// leave the target's nonzero status visible to Session.finish.
func TestAgentLauncherPropagatesTargetIOAndExit(t *testing.T) {
	launcher := launcherTestFixture(t)
	s := NewSession(Config{SessionID: agentLauncherIOID, Dir: t.TempDir(), Launcher: launcher})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start I/O target: %v", err)
	}
	if err := s.Send("supervisor stdin", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}

	var sawInput bool
	for ev := range s.Events() {
		if strings.Contains(string(ev.Raw), "agent_launcher_io") && strings.Contains(string(ev.Raw), "supervisor stdin") {
			sawInput = true
		}
	}
	if !sawInput {
		t.Fatal("target stdout did not carry the line read from supervisor stdin")
	}
	err := s.Err()
	if err == nil || !strings.Contains(err.Error(), "exit status 23") {
		t.Fatalf("target exit = %v, want exit status 23", err)
	}
	if !strings.Contains(err.Error(), agentLauncherIOStderr) {
		t.Fatalf("target exit = %v, want stderr %q", err, agentLauncherIOStderr)
	}
}

// A helper-entry acknowledgement instead of target-exec acknowledgement makes
// this falsely pass and can start the target in the wrong directory.
func TestAgentLauncherReportsChdirFailureBeforeTargetExec(t *testing.T) {
	launcher := launcherTestFixture(t)
	wantDir := filepath.Join(t.TempDir(), "missing")
	recordPath := filepath.Join(t.TempDir(), "target-record.json")
	t.Setenv(agentLauncherTestRecordEnv, recordPath)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000beef",
		Dir:       wantDir,
		Launcher:  launcher,
	})

	err := s.Start(context.Background())
	if err == nil {
		t.Fatal("Start accepted a missing working directory")
	}
	if !strings.Contains(err.Error(), wantDir) {
		t.Fatalf("Start error %q does not name missing directory %q", err, wantDir)
	}
	if _, statErr := os.Stat(recordPath); statErr == nil {
		t.Fatal("target recorder ran after launcher chdir failure")
	} else if !os.IsNotExist(statErr) {
		t.Fatalf("checking target recorder absence: %v", statErr)
	}
}

func TestAgentLauncherReportsMissingTarget(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	launcher, err := SelfAgentLauncher()
	if err != nil {
		t.Fatalf("SelfAgentLauncher: %v", err)
	}
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000fade",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})

	err = s.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "resolve claude") {
		t.Fatalf("Start = %v, want missing claude error", err)
	}
}

// A target that already started must not keep STATUS alive after its
// supervisor dies before READY.
func TestAgentLauncherTargetStartThenSupervisorDeathReportsEOF(t *testing.T) {
	launcher := launcherTestFixture(t)
	t.Setenv(agentLauncherTestSupervisorEnv, "exit-after-target-start")
	startedPath := filepath.Join(t.TempDir(), "target-started")
	t.Setenv(agentLauncherTestStartedEnv, startedPath)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000f003",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.Start(ctx) }()

	select {
	case err := <-result:
		if err == nil || !strings.Contains(err.Error(), "launcher exited before reporting target outcome") {
			t.Fatalf("Start = %v, want explicit post-target-start supervisor EOF", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		<-result
		t.Fatal("Start hung because the live target retained launcher STATUS")
	}
	waitForFile(t, startedPath)
	if pid := readPID(t, startedPath); pid <= 0 {
		t.Fatalf("recorded target PID = %d, want positive", pid)
	}
	waitForObservedStartEnd(t, s)
}

func TestAgentLauncherSupervisorDeathAfterReadyEndsSession(t *testing.T) {
	launcher := launcherTestFixture(t)
	gatePath, release := newAgentLauncherTestGate(t)
	t.Setenv(agentLauncherTestSupervisorEnv, "exit-after-ready")
	t.Setenv(agentLauncherTestExitGateEnv, gatePath)
	recordPath := filepath.Join(t.TempDir(), "target-record.json")
	t.Setenv(agentLauncherTestRecordEnv, recordPath)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000f005",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})
	if err := s.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForFile(t, recordPath)
	ended := drainedAsync(s)
	release()
	waitForTestSignal(t, ended, "session stayed open after supervisor died without DONE")
	if err := s.Err(); err == nil || !strings.Contains(err.Error(), "launcher lifetime") {
		t.Fatalf("session error = %v, want launcher lifetime failure", err)
	}
	waitForGroupGone(t, s.Pgid())
}

// The wedge through the supervisor, the launcher-path twin of
// TestAWedgedAgentWhoseGrandchildHoldsStdoutEndsItself. The target exits while a
// grandchild holds the stdout it inherited - which is the supervisor's own fd 1,
// the write end of the pipe Wake reads. The supervisor reaps the target, reports
// DONE and exits, but the grandchild keeps Wake's read end open, so the scan
// never EOFs. awaitExit reaps the *supervisor* - one process deeper than the
// direct path - and force-closes the read end after the grace, ending the
// session cleanly with no cancel, no ps-watchdog, no operator.
func TestSupervisedWedgeWhoseGrandchildHoldsStdoutEndsItself(t *testing.T) {
	base := runtime.NumGoroutine()
	launcher := launcherTestFixture(t)
	pidPath := filepath.Join(t.TempDir(), "wedge.pid")
	t.Setenv(agentLauncherTestWedgePidEnv, pidPath)

	s := NewSession(Config{SessionID: agentLauncherWedgeID, Dir: t.TempDir(), Launcher: launcher})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The turn before the wedge has to survive the force-close that ends it.
	if got := waitForKind(t, s, KindAssistantText); got != "pong 0" {
		t.Fatalf("assistant text = %q, want pong 0 - the turn before the supervised wedge was lost", got)
	}
	grandchild := waitForPid(t, pidPath)
	t.Cleanup(func() { _ = syscall.Kill(grandchild, syscall.SIGKILL) })

	// Before awaitExit reaped the supervisor this hangs forever: the scan never
	// sees EOF because the grandchild holds the read end, and finish's reap sat
	// behind that scan.
	select {
	case <-drainedAsync(s):
	case <-time.After(waitDelay + 10*time.Second):
		t.Fatal("the supervised session never ended itself: a target that exited while a grandchild held stdout wedged the pump through the supervisor")
	}

	// The clean ending: the target and its supervisor both exited on their own,
	// and Wake's force-close after Wait is not a stream that outran its process.
	if err := s.Err(); err != nil {
		t.Errorf("Err = %v, want nil - a supervised target that exited cleanly is a clean ending even when a grandchild held its stdout", err)
	}
	waitForGoroutines(t, base)
}

func TestAgentLauncherPropagatesTargetSignal(t *testing.T) {
	launcher := launcherTestFixture(t)
	s := NewSession(Config{SessionID: agentLauncherSignalID, Dir: t.TempDir(), Launcher: launcher})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start signalled target: %v", err)
	}
	drain(s)
	err := s.Err()
	if err == nil || !strings.Contains(err.Error(), "signal: terminated") {
		t.Fatalf("target signal = %v, want terminated", err)
	}
	if strings.Contains(err.Error(), "launcher lifetime") {
		t.Fatalf("normal signal propagation reported liveness failure: %v", err)
	}
}

func TestAgentLauncherLifetimeProtocol(t *testing.T) {
	for _, tt := range []struct {
		name    string
		frame   []byte
		wantErr string
	}{
		{name: "done", frame: []byte{agentLauncherDone}},
		{name: "eof", wantErr: "before DONE"},
		{name: "unknown", frame: []byte{0xff}, wantErr: "unknown Wake agent launcher lifetime"},
		{name: "trailing", frame: []byte{agentLauncherDone, 0xff}, wantErr: "trailing lifetime bytes"},
		{name: "double", frame: []byte{agentLauncherDone, agentLauncherDone}, wantErr: "trailing lifetime bytes"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := readAgentLauncherLifetimeTestFrame(t, tt.frame)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("read lifetime = %v, want nil", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("read lifetime = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func readAgentLauncherLifetimeTestFrame(t *testing.T, frame []byte) error {
	t.Helper()
	lifetime, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("lifetime pipe: %v", err)
	}
	if len(frame) != 0 {
		if _, err := writer.Write(frame); err != nil {
			t.Fatalf("write lifetime frame: %v", err)
		}
	}
	_ = writer.Close()
	return readAgentLauncherLifetime(lifetime)
}

func TestAgentLauncherReleaseClosesControl(t *testing.T) {
	control, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("control pipe: %v", err)
	}
	if _, err := writer.Write([]byte{agentLauncherRelease}); err != nil {
		t.Fatalf("write RELEASE: %v", err)
	}
	_ = writer.Close()
	if err := readAgentLauncherRelease(control); err != nil {
		t.Fatalf("accept RELEASE: %v", err)
	}
	if _, err := control.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("CONTROL remains open after RELEASE: %v", err)
	}
}

// Returning from syscall.Exec without first reporting through fd 3 turns the
// helper's exit into empty EOF, which Start would otherwise accept as success.
func TestAgentLauncherReportsTargetExecFailure(t *testing.T) {
	binDir := t.TempDir()
	target := filepath.Join(binDir, claudeBinary)
	if err := os.WriteFile(target, []byte("#!/definitely/missing/wake-agent-interpreter\n"), 0o700); err != nil {
		t.Fatalf("write invalid claude executable: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	launcher, err := SelfAgentLauncher()
	if err != nil {
		t.Fatalf("SelfAgentLauncher: %v", err)
	}
	s := NewSession(Config{
		SessionID: "0a1b2c3d-0000-4000-8000-00000000dead",
		Dir:       t.TempDir(),
		Launcher:  launcher,
	})

	err = s.Start(context.Background())
	if err == nil {
		t.Fatal("Start accepted a claude target whose interpreter does not exist")
	}
	if !strings.Contains(err.Error(), target) {
		t.Fatalf("Start error %q does not name target start failure %q", err, target)
	}
}

func TestAgentLauncherProtocolRequiresExplicitStatusFrame(t *testing.T) {
	errorBody := []byte("target start failed")
	errorFrame := make([]byte, 5+len(errorBody))
	errorFrame[0] = agentLauncherError
	binary.BigEndian.PutUint32(errorFrame[1:5], uint32(len(errorBody)))
	copy(errorFrame[5:], errorBody)
	overlong := []byte{agentLauncherError, 0, 0, 0x10, 0x01}
	trailingError := append(append([]byte{}, errorFrame...), 0xff)
	doubleError := append(append([]byte{}, errorFrame...), errorFrame...)

	tests := []struct {
		name    string
		frame   []byte
		wantErr string
	}{
		{name: "ready", frame: []byte{agentLauncherReady}},
		{name: "ready trailing byte", frame: []byte{agentLauncherReady, 0xff}, wantErr: "trailing status bytes"},
		{name: "double ready", frame: []byte{agentLauncherReady, agentLauncherReady}, wantErr: "trailing status bytes"},
		{name: "eof", wantErr: "launcher exited before reporting target outcome"},
		{name: "truncated error length", frame: []byte{agentLauncherError, 0, 0}, wantErr: "error length"},
		{name: "truncated error body", frame: []byte{agentLauncherError, 0, 0, 0, 3, 'n', 'o'}, wantErr: "error message"},
		{name: "unknown opcode", frame: []byte{0xff}, wantErr: "unknown Wake agent launcher status"},
		{name: "overlong error", frame: overlong, wantErr: "exceeded 4096 bytes"},
		{name: "error", frame: errorFrame, wantErr: string(errorBody)},
		{name: "error trailing byte", frame: trailingError, wantErr: "trailing status bytes"},
		{name: "double error", frame: doubleError, wantErr: "trailing status bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := readAgentLauncherStatus(t, tt.frame)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("readAgentLauncher = %v, want nil", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("readAgentLauncher = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func readAgentLauncherStatus(t *testing.T, frame []byte) error {
	t.Helper()
	status, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("status pipe: %v", err)
	}
	if len(frame) != 0 {
		if _, err := writer.Write(frame); err != nil {
			t.Fatalf("write status frame: %v", err)
		}
	}
	_ = writer.Close()
	return readAgentLauncher(status)
}

// Missing, unknown, and doubled RELEASE frames must all end the supervisor
// before target resolution or start.
func TestAgentLauncherProtocolRejectsInvalidRelease(t *testing.T) {
	for _, tt := range []struct {
		name    string
		control []byte
	}{
		{name: "missing"},
		{name: "unknown", control: []byte{0xff}},
		{name: "double", control: []byte{agentLauncherRelease, agentLauncherRelease}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recordPath := filepath.Join(t.TempDir(), "target-record.json")
			statusErr, waitErr := runRawAgentLauncher(t, tt.control, recordPath)
			if statusErr == nil {
				t.Fatal("invalid CONTROL frame produced no STATUS failure")
			}
			if waitErr == nil {
				t.Fatal("supervisor accepted an invalid CONTROL frame")
			}
			if _, err := os.Stat(recordPath); err == nil {
				t.Fatal("target started after invalid CONTROL frame")
			} else if !os.IsNotExist(err) {
				t.Fatalf("check target record: %v", err)
			}
		})
	}
}

func runRawAgentLauncher(t *testing.T, control []byte, recordPath string) (error, error) {
	t.Helper()
	launcher := launcherTestFixture(t)
	controlReader, controlWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("control pipe: %v", err)
	}
	statusReader, statusWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("status pipe: %v", err)
	}
	cmd := exec.Command(launcher.executable, agentLauncherArg, "--session-id", agentLauncherInstantID)
	cmd.Env = append(scrubbedEnv(os.Environ()),
		agentLauncherMarkerEnv+"="+agentLauncherProtocol,
		agentLauncherDirEnv+"="+t.TempDir(),
		agentLauncherTestRecordEnv+"="+recordPath,
	)
	cmd.ExtraFiles = []*os.File{controlReader, statusWriter}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start raw supervisor: %v", err)
	}
	_ = controlReader.Close()
	_ = statusWriter.Close()
	if len(control) != 0 {
		_, _ = controlWriter.Write(control)
	}
	_ = controlWriter.Close()
	statusErr := readAgentLauncher(statusReader)
	return statusErr, cmd.Wait()
}

// Routing the zero capability through the helper instead of the existing exec
// seam makes this observe the Wake executable rather than the fixed target.
func TestDirectStartWithoutLauncherUsesClaudeCommand(t *testing.T) {
	original := execCommand
	var gotName string
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		return fakeExec(ctx, name, args...)
	}
	t.Cleanup(func() { execCommand = original })

	s := NewSession(Config{SessionID: "s1", Dir: t.TempDir()})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	drain(s)
	if gotName != claudeBinary {
		t.Fatalf("execCommand name = %q, want %q", gotName, claudeBinary)
	}
}
