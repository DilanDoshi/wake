package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// agentProcess is one launch's handles, shared between the goroutines that
// stream it and end it. cmd is claude directly on the direct path, or the Unix
// supervisor that leads claude's process group on the launcher path; either
// way there is exactly one Wait on it, and it is awaitExit's.
type agentProcess struct {
	cmd      *exec.Cmd
	launcher *agentLauncherPipes
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	errTail  *tailWriter
	// procGone closes once awaitExit has reaped cmd and force-closed the read
	// end; finish waits on it rather than reaping a second time.
	procGone chan struct{}
	// lifetimeSettled closes once the lifetime watcher has returned, so finish
	// reads a settled lifetimeErr rather than racing the watcher that latches
	// it. Always closed, on every path, so the wait can never deadlock.
	lifetimeSettled chan struct{}
	// lifetimeMu guards both teardown causes, written by failStart / the
	// lifetime watcher and read by finish on the pump goroutine.
	lifetimeMu  sync.Mutex
	lifetimeErr error
	startErr    error
}

// Start launches the process and begins streaming events. Cancelling ctx is
// the hard kill; Stop is the graceful stdin close.
func (s *Session) Start(ctx context.Context) error {
	return s.StartObserved(ctx, nil)
}

// StartObserved publishes ownership before it releases the Unix supervisor.
// Cancellation abandons a blocked observer and tears the supervisor down.
func (s *Session) StartObserved(ctx context.Context, onProcess func(context.Context, int) error) error {
	if onProcess != nil && s.cfg.Launcher.executable == "" {
		return errors.New("observed process ownership requires a Wake agent launcher")
	}
	if err := s.beginStart(); err != nil {
		return err
	}
	published := false
	defer func() {
		if !published {
			s.clearStarting()
		}
	}()

	start, err := s.startAgent(ctx)
	if err != nil {
		return err
	}
	stopped, pgid := s.publishAgent(start)
	published = true
	s.runAgent(ctx, start)
	ownershipCtx, cancelOwnership := context.WithCancel(ctx)
	outcome := watchAgentLauncher(ctx, start.launcher)
	cause := s.observeAgentStart(ctx, ownershipCtx, start, stopped, pgid, onProcess, outcome)
	cancelOwnership()
	if cause != nil {
		return s.failStart(ctx, start, cause)
	}
	return nil
}

func (s *Session) beginStart() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.starting || s.cmd != nil:
		return fmt.Errorf("session %s already started", s.cfg.SessionID)
	case s.stopped:
		return fmt.Errorf("session %s was stopped before it started", s.cfg.SessionID)
	}
	s.starting = true
	return nil
}

func (s *Session) clearStarting() {
	s.mu.Lock()
	s.starting = false
	s.mu.Unlock()
}

func (s *Session) startAgent(ctx context.Context) (*agentProcess, error) {
	args, err := s.buildArgs()
	if err != nil {
		return nil, err
	}
	cmd, launcher, err := agentCommand(ctx, s.cfg.Launcher, s.cfg.Dir, args)
	if err != nil {
		return nil, fmt.Errorf("prepare claude for session %s in %s: %w", s.cfg.SessionID, s.cfg.Dir, err)
	}
	if cmd.Env == nil {
		cmd.Env = scrubbedEnv(os.Environ())
	}
	errTail := &tailWriter{max: stderrTailBytes}
	cmd.Stderr = errTail
	// Load-bearing beside the line above: see waitDelay. An agent is a tree -
	// MCP servers, hooks, shells - so its own group makes the whole tree
	// reachable to a single kill, on the direct path and through the supervisor
	// that shares this group on the launcher one.
	cmd.WaitDelay = waitDelay
	setProcessGroup(cmd)
	cmd.Cancel = func() error { return killProcessGroup(cmd) }
	stdin, err := cmd.StdinPipe()
	if err != nil {
		discardAgentCommand(cmd, launcher)
		return nil, fmt.Errorf("stdin pipe for session %s: %w", s.cfg.SessionID, err)
	}
	// ownedStdout, not StdoutPipe: Wake keeps the read end and closes it on its
	// own terms in awaitExit, which is the whole wedge fix. It holds whether cmd
	// is claude directly or the supervisor that hands the target this same write
	// end as its own fd 1 - a grandchild inheriting it wedges the read either
	// way, and only an owned read end can force it shut after Wait.
	stdout, stdoutW, err := ownedStdout(cmd)
	if err != nil {
		_ = stdin.Close()
		discardAgentCommand(cmd, launcher)
		return nil, fmt.Errorf("stdout pipe for session %s: %w", s.cfg.SessionID, err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stdoutW.Close()
		discardAgentCommand(cmd, launcher)
		return nil, fmt.Errorf("start claude for session %s in %s: %w", s.cfg.SessionID, s.cfg.Dir, err)
	}
	// The supervisor's protocol fds are the child's now; Wake's copies go, or a
	// read on one never EOFs. The stdout write end is the child's dup for the
	// same reason - keep Wake's copy and the pipe never EOFs and awaitExit
	// force-closes even a clean exit.
	closeAgentCommandExtraFiles(cmd)
	_ = stdoutW.Close()
	return &agentProcess{
		cmd:             cmd,
		launcher:        launcher,
		stdin:           stdin,
		stdout:          stdout,
		errTail:         errTail,
		procGone:        make(chan struct{}),
		lifetimeSettled: make(chan struct{}),
	}, nil
}

func (s *Session) publishAgent(start *agentProcess) (bool, int) {
	pgid := processGroup(start.cmd)
	s.mu.Lock()
	s.cmd, s.stdin, s.pgid = start.cmd, start.stdin, pgid
	s.starting = false
	stopped := s.stopped
	s.mu.Unlock()
	return stopped, pgid
}

// runAgent starts the streaming goroutines. awaitExit is the session's one
// cmd.Wait - on the supervisor when there is one, on claude when there is not -
// so a wedged scan can no longer keep the reaping from happening, and the
// force-close after Wait self-detects the wedge on both paths. The lifetime
// watcher is the launcher-only supplement: it hurries the scan's end and names
// the cause when the supervisor dies before reporting DONE.
func (s *Session) runAgent(ctx context.Context, start *agentProcess) {
	scanDone := make(chan struct{})
	go s.logs.run()
	go s.watchAgentLauncherLifetime(ctx, start)
	go closeOnCancel(ctx, start.stdout, scanDone)
	go s.awaitExit(start.cmd, start.stdout, scanDone, start.procGone)
	go s.pump(ctx, start, scanDone)
}

func (s *Session) watchAgentLauncherLifetime(ctx context.Context, start *agentProcess) {
	defer close(start.lifetimeSettled)
	if start.launcher == nil || start.launcher.lifetime == nil {
		return
	}
	err := awaitAgentLauncherLifetime(ctx, start.launcher.lifetime)
	if err == nil {
		// DONE: the target exited and the supervisor is on its way out. awaitExit
		// reaps it, and if a grandchild still holds the read end open, force-closes
		// that end after the grace - the wedge, ended without a word here.
		return
	}
	if ctx.Err() != nil {
		_ = killProcessGroup(start.cmd)
		return
	}
	// The supervisor is gone before it reported DONE. Latch the cause and hurry
	// the scan's end; awaitExit's own force-close after Wait is the backstop.
	start.setLifetimeError(fmt.Errorf("launcher lifetime: %w", err))
	_ = killProcessGroup(start.cmd)
	_ = start.stdout.Close()
}

func (p *agentProcess) setLifetimeError(err error) {
	p.lifetimeMu.Lock()
	p.lifetimeErr = err
	p.lifetimeMu.Unlock()
}

func (p *agentProcess) lifetimeError() error {
	p.lifetimeMu.Lock()
	defer p.lifetimeMu.Unlock()
	return p.lifetimeErr
}

func (p *agentProcess) setStartError(err error) {
	p.lifetimeMu.Lock()
	p.startErr = err
	p.lifetimeMu.Unlock()
}

func (p *agentProcess) startError() error {
	p.lifetimeMu.Lock()
	defer p.lifetimeMu.Unlock()
	return p.startErr
}

func (s *Session) observeAgentStart(
	ctx, ownershipCtx context.Context,
	start *agentProcess,
	stopped bool,
	pgid int,
	onProcess func(context.Context, int) error,
	outcome <-chan error,
) error {
	if start.launcher == nil {
		return s.awaitDirectAgentStart(ctx, stopped)
	}
	observed := observeAgentProcess(ownershipCtx, pgid, onProcess)
	select {
	case err := <-observed:
		if err != nil {
			return err
		}
	case err := <-outcome:
		return earlyAgentLauncherOutcome(err)
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-outcome:
		return earlyAgentLauncherOutcome(err)
	default:
	}
	if err := s.releaseAgentLauncher(ctx, start.launcher); err != nil {
		return err
	}
	return s.awaitAgentLauncherOutcome(ctx, outcome)
}

func observeAgentProcess(ctx context.Context, pgid int, onProcess func(context.Context, int) error) <-chan error {
	result := make(chan error, 1)
	if onProcess == nil {
		result <- nil
		return result
	}
	go func() { result <- onProcess(ctx, pgid) }()
	return result
}

func earlyAgentLauncherOutcome(err error) error {
	if err != nil {
		return err
	}
	return errAgentLauncherEarly
}

func (s *Session) releaseAgentLauncher(ctx context.Context, pipes *agentLauncherPipes) error {
	// Unlike writeLine this holds mu across the write - the exemption to the
	// file header's "mu never blocks" rule. The write is one byte into a fresh,
	// supervisor-blocked control pipe (well under PIPE_BUF), so it cannot block,
	// and the lock is what makes the stopped-check-then-RELEASE atomic against a
	// concurrent Stop (TestStartObservedStopWinsBeforeOwnershipRelease).
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.stopped {
		return s.stoppedStartError()
	}
	if pipes == nil || pipes.control == nil {
		return errors.New("wake agent launcher has no control pipe")
	}
	writeErr := writeAgentLauncher(pipes.control, []byte{agentLauncherRelease})
	closeErr := pipes.control.Close()
	pipes.control = nil
	if writeErr != nil {
		return fmt.Errorf("%w: release control: %v", errAgentLauncherExited, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Wake agent launcher control: %w", closeErr)
	}
	return nil
}

func (s *Session) awaitAgentLauncherOutcome(ctx context.Context, outcome <-chan error) error {
	select {
	case err := <-outcome:
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return err
		}
		if s.sessionStopped() {
			return s.stoppedStartError()
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Session) awaitDirectAgentStart(ctx context.Context, stopped bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if stopped {
		return s.stoppedStartError()
	}
	if s.sessionStopped() {
		return s.stoppedStartError()
	}
	return nil
}

func (s *Session) sessionStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Session) stoppedStartError() error {
	return fmt.Errorf("session %s was stopped while it started", s.cfg.SessionID)
}

// failStart tears down a launch whose handshake never completed. awaitExit is
// already running as the session's sole reaper, so this only has to reach it:
// kill the group to wake its Wait, close the read end to end the scan, then
// drain Events to wait finish out. It never reaps a second time.
func (s *Session) failStart(ctx context.Context, start *agentProcess, cause error) error {
	err := fmt.Errorf("start claude for session %s in %s: %w", s.cfg.SessionID, s.cfg.Dir, cause)
	// Latched before the kill and close below, which wake the pump's teardown,
	// so finish reports this cause rather than the os.ErrClosed and lifetime
	// artifacts that teardown leaves behind.
	start.setStartError(err)
	closeAgentLauncherPipes(start.launcher)
	s.mu.Lock()
	s.stdin = nil
	s.mu.Unlock()
	_ = start.stdin.Close()
	_ = killProcessGroup(start.cmd)
	_ = start.stdout.Close()
	if ctx.Err() == nil {
		for range s.Events() {
		}
	}
	return err
}
