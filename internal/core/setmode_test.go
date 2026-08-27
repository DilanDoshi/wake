// Changing a running session's permission mode.
//
// EncodeSetMode has its own tests in encode_test.go: those are about the bytes.
// These are about the two things only a Session can do - mint the correlator
// the receipt comes back with, and *not* remember the write afterwards, which
// is the whole difference between this and Interrupt.

package core

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestSetModeMintsTheRequestIDItSends is Interrupt's rule for the same reason
// and one sharper: the receipt names no session, so a repeated id would make
// two mode changes across a fleet indistinguishable - and unlike an interrupt
// receipt, this one carries the mode the session actually landed on, which is
// the only authority there is on what happened.
func TestSetModeMintsTheRequestIDItSends(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	first, err := s.SetMode(PermissionModePlan)
	if err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	second, err := s.SetMode(PermissionModeAuto)
	if err != nil {
		t.Fatalf("SetMode: %v", err)
	}

	if first == "" {
		t.Fatal("SetMode returned no request id: the receipt would be unattributable")
	}
	if first == second {
		t.Errorf("two mode changes reused request id %q: a receipt cannot say which one it answers", first)
	}
	for _, id := range []string{first, second} {
		if _, err := uuid.Parse(id); err != nil {
			t.Errorf("request id %q is not a uuid: %v", id, err)
		}
	}

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("wrote %d lines, want 2\ngot: %s", len(lines), buf.String())
	}
	for i, want := range []string{first, second} {
		if !strings.Contains(lines[i], `"request_id":"`+want+`"`) {
			t.Errorf("line %d = %s\nwant the request id SetMode returned (%s)", i+1, lines[i], want)
		}
	}
}

// TestSetModeSendsTheModeItWasGiven pins that the mode reaches the wire at all -
// the frame's shape is EncodeSetMode's business, but which of two arguments
// ends up in it is this function's.
func TestSetModeSendsTheModeItWasGiven(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	if _, err := s.SetMode(PermissionModePlan); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if !strings.Contains(buf.String(), `"mode":"`+PermissionModePlan+`"`) {
		t.Errorf("wrote %s, want the mode it was given", buf.String())
	}
}

// TestSetModeBuysNoExcuseForAnExitOne is the difference between this and
// Interrupt, and it is the one worth a test of its own.
//
// Interrupt records that Wake aborted a turn, because an interrupted process
// exits 1 with an empty stderr - byte-identical to a startup rejection - and
// without the memory a deliberate abort reads as a crash. A mode change aborts
// nothing. If it took the same licence, a session that failed to start would be
// forgiven its exit code for the rest of its life on the strength of a keystroke
// that changed a label, and Err would go quiet on exactly the failure it exists
// to report.
func TestSetModeBuysNoExcuseForAnExitOne(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "interrupt-exit")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	if _, err := s.SetMode(PermissionModePlan); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	drain(s)

	if err := s.Err(); err == nil {
		t.Error("Err = nil for a session that exited 1 after only a mode change: a mode change aborts no turn, so it owes the ending no excuse - forgiving it here hides a startup rejection")
	}
}

func TestSetModeBeforeStartIsAnError(t *testing.T) {
	s := NewSession(Config{SessionID: "s1"})
	if _, err := s.SetMode(PermissionModePlan); err == nil {
		t.Fatal("SetMode on an unstarted session = nil, want an error")
	}
}

func TestSetModeAfterStopIsAnError(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := s.SetMode(PermissionModePlan); err == nil {
		t.Fatal("SetMode after Stop = nil, want an error")
	}
}
