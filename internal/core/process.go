// The child process's environment, the stdout pipe Wake hands it, and its
// stderr. Split out of session.go, which owns the lifecycle: what Wake hands a
// process and what it keeps of what the process says back are supporting
// detail, and burying them under the lifecycle is what pushed that file out of
// its size band.

package core

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
)

// ownedStdout gives the child a stdout pipe whose read end Wake keeps and
// closes on its own terms, unlike cmd.StdoutPipe's, which os/exec closes inside
// cmd.Wait. That ownership is the whole fix for a wedged agent: it lets
// awaitExit force the read end shut only after Wait has confirmed the process
// gone - never racing os/exec's own untaggable close, and never truncating
// frames the scan has not read yet.
//
// cmd.Stdout is an *os.File, so os/exec uses it as the child's fd 1 directly:
// no copier goroutine, and nothing of ours registered for its own teardown. The
// write end is the caller's to close once the child holds it, after Start.
func ownedStdout(cmd *exec.Cmd) (pr, pw *os.File, err error) {
	pr, pw, err = os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stdout = pw
	return pr, pw, nil
}

// stderrTailBytes bounds what a session keeps of its process's stderr. The
// startup rejections worth reporting are a single line; anything longer is a
// crash dump, whose tail is the useful end.
const stderrTailBytes = 4096

// agentLauncherErrorBytes bounds the private launcher's framed ERROR payload.
const agentLauncherErrorBytes = 4096

var (
	errAgentLauncherExited = errors.New("launcher exited before reporting target outcome")
	errAgentLauncherEarly  = errors.New("wake agent launcher reported status before ownership release")
)

// nestedSessionEnv are the variables a claude process exports to everything
// it spawns to describe *itself*. Wake is routinely started from inside one,
// and a child inheriting these announces its parent's identity rather than
// the one Wake assigned. Every fixture in testdata/stream was recorded with
// exactly these unset, so this is also the only environment Wake's decoder
// has evidence for.
//
// A list rather than a CLAUDE_ prefix match, on purpose: CLAUDE_CODE_OAUTH_TOKEN
// shares that prefix, and scrubbing it would unauthenticate every session on
// the machine.
var nestedSessionEnv = []string{
	"CLAUDECODE",
	"CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_EXECPATH",
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_PID",
	"CLAUDE_EFFORT",
	"CLAUDE_PLUGIN_DATA",
}

// scrubbedEnv returns a copy without nested-session or private launcher
// variables, leaving the original untouched.
func scrubbedEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if !isNestedSessionVar(kv) && !isAgentLauncherVar(kv) {
			out = append(out, kv)
		}
	}
	return out
}

func isAgentLauncherVar(kv string) bool {
	name, _, ok := strings.Cut(kv, "=")
	return ok && slices.Contains(agentLauncherEnv, name)
}

func reportAgentLauncherFailure(status *os.File, err error) error {
	message := []byte(err.Error())
	if len(message) > agentLauncherErrorBytes {
		message = message[:agentLauncherErrorBytes]
	}
	header := [5]byte{agentLauncherError}
	binary.BigEndian.PutUint32(header[1:], uint32(len(message)))
	writeErr := writeAgentLauncher(status, header[:])
	if writeErr == nil {
		writeErr = writeAgentLauncher(status, message)
	}
	closeErr := status.Close()
	if writeErr != nil {
		return fmt.Errorf("%w (report launcher failure: %v)", err, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("%w (close launcher status: %v)", err, closeErr)
	}
	return err
}

func reportAgentLauncherReady(status *os.File) error {
	if err := writeAgentLauncher(status, []byte{agentLauncherReady}); err != nil {
		return fmt.Errorf("report Wake agent launcher READY: %w", err)
	}
	if err := status.Close(); err != nil {
		return fmt.Errorf("close Wake agent launcher status: %w", err)
	}
	return nil
}

func reportAgentLauncherDone(lifetime *os.File) error {
	writeErr := writeAgentLauncher(lifetime, []byte{agentLauncherDone})
	closeErr := lifetime.Close()
	if writeErr != nil {
		return fmt.Errorf("report Wake agent launcher DONE: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close Wake agent launcher lifetime: %w", closeErr)
	}
	return nil
}

func writeAgentLauncher(dst io.Writer, data []byte) error {
	for len(data) != 0 {
		n, err := dst.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func awaitAgentLauncher(ctx context.Context, status *os.File) error {
	if status == nil {
		return nil
	}
	result := make(chan error, 1)
	go func() { result <- readAgentLauncher(status) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		_ = status.Close()
		return ctx.Err()
	}
}

func readAgentLauncher(status *os.File) error {
	defer func() { _ = status.Close() }()
	var opcode [1]byte
	if _, err := io.ReadFull(status, opcode[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return errAgentLauncherExited
		}
		return fmt.Errorf("read Wake agent launcher status: %w", err)
	}
	switch opcode[0] {
	case agentLauncherReady:
		return readAgentLauncherStatusEOF(status)
	case agentLauncherError:
		message, err := readAgentLauncherError(status)
		if err != nil {
			return err
		}
		if err := readAgentLauncherStatusEOF(status); err != nil {
			return err
		}
		return errors.New(message)
	default:
		return fmt.Errorf("unknown Wake agent launcher status opcode 0x%02x", opcode[0])
	}
}

func readAgentLauncherError(status io.Reader) (string, error) {
	var length [4]byte
	if _, err := io.ReadFull(status, length[:]); err != nil {
		return "", fmt.Errorf("read Wake agent launcher error length: %w", err)
	}
	size := binary.BigEndian.Uint32(length[:])
	if size > agentLauncherErrorBytes {
		return "", errors.New("wake agent launcher failure exceeded 4096 bytes")
	}
	message := make([]byte, size)
	if _, err := io.ReadFull(status, message); err != nil {
		return "", fmt.Errorf("read Wake agent launcher error message: %w", err)
	}
	if len(message) == 0 {
		return "", errors.New("wake agent launcher failed without an error message")
	}
	return string(message), nil
}

func readAgentLauncherStatusEOF(status io.Reader) error {
	var trailing [1]byte
	n, err := status.Read(trailing[:])
	if n != 0 {
		return fmt.Errorf("wake agent launcher has trailing status bytes starting with 0x%02x", trailing[0])
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("finish Wake agent launcher status frame: %w", err)
	}
	return errors.New("finish Wake agent launcher status frame: empty read")
}

func readAgentLauncherLifetime(lifetime *os.File) error {
	defer func() { _ = lifetime.Close() }()
	var opcode [1]byte
	if _, err := io.ReadFull(lifetime, opcode[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return errors.New("wake agent launcher lifetime ended before DONE")
		}
		return fmt.Errorf("read Wake agent launcher lifetime: %w", err)
	}
	if opcode[0] != agentLauncherDone {
		return fmt.Errorf("unknown Wake agent launcher lifetime opcode 0x%02x", opcode[0])
	}
	var trailing [1]byte
	n, err := lifetime.Read(trailing[:])
	if n != 0 {
		return fmt.Errorf("wake agent launcher has trailing lifetime bytes starting with 0x%02x", trailing[0])
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("finish Wake agent launcher lifetime frame: %w", err)
	}
	return errors.New("finish Wake agent launcher lifetime frame: empty read")
}

func awaitAgentLauncherLifetime(ctx context.Context, lifetime *os.File) error {
	result := make(chan error, 1)
	go func() { result <- readAgentLauncherLifetime(lifetime) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		_ = lifetime.Close()
		return ctx.Err()
	}
}

func watchAgentLauncher(ctx context.Context, pipes *agentLauncherPipes) <-chan error {
	result := make(chan error, 1)
	if pipes == nil {
		result <- nil
		return result
	}
	go func() { result <- awaitAgentLauncher(ctx, pipes.status) }()
	return result
}

func closeAgentLauncherPipes(pipes *agentLauncherPipes) {
	if pipes == nil {
		return
	}
	if pipes.control != nil {
		_ = pipes.control.Close()
	}
	if pipes.status != nil {
		_ = pipes.status.Close()
	}
	if pipes.lifetime != nil {
		_ = pipes.lifetime.Close()
	}
}

func discardAgentCommand(cmd *exec.Cmd, pipes *agentLauncherPipes) {
	closeAgentLauncherPipes(pipes)
	closeAgentCommandExtraFiles(cmd)
}

func closeAgentCommandExtraFiles(cmd *exec.Cmd) {
	for _, file := range cmd.ExtraFiles {
		_ = file.Close()
	}
	cmd.ExtraFiles = nil
}

func isNestedSessionVar(kv string) bool {
	name, _, ok := strings.Cut(kv, "=")
	return ok && slices.Contains(nestedSessionEnv, name)
}

// tailWriter keeps the last max bytes written to it. os/exec runs a copying
// goroutine for a stderr that is not an *os.File, so the mutex guards that
// goroutine against the read in finish.
type tailWriter struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.max {
		w.buf = w.buf[len(w.buf)-w.max:]
	}
	return len(p), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.buf)
}
