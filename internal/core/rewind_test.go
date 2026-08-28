// Rewinding a running session's conversation.
//
// EncodeRewind has its own tests in encode_test.go: those are about the bytes.
// These are about the two things only a Session can do - mint the correlator
// the receipt comes back with, and *not* remember the write afterwards, which
// is SetMode's own reason and applies here for the same one: a rewind aborts
// no turn and is owed no forgive-the-exit licence.

package core

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestRewindMintsTheRequestIDItSends is SetMode's own test, adapted: the
// receipt names no session, so a repeated id would make two rewinds across a
// fleet indistinguishable.
func TestRewindMintsTheRequestIDItSends(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	first, err := s.Rewind("target-1", "seen-1")
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	second, err := s.Rewind("target-2", "seen-2")
	if err != nil {
		t.Fatalf("Rewind: %v", err)
	}

	if first == "" {
		t.Fatal("Rewind returned no request id: the receipt would be unattributable")
	}
	if first == second {
		t.Errorf("two rewinds reused request id %q: a receipt cannot say which one it answers", first)
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
			t.Errorf("line %d = %s\nwant the request id Rewind returned (%s)", i+1, lines[i], want)
		}
	}
}

// TestRewindSendsTheUUIDsItWasGiven pins that both uuids reach the wire, and
// under the keys the CLI's rewind_conversation subtype expects - the frame's
// exact shape is EncodeRewind's business, but which of two arguments ends up
// in which key is this function's.
func TestRewindSendsTheUUIDsItWasGiven(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	if _, err := s.Rewind("T", "S"); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	line := buf.String()
	if !strings.Contains(line, `"subtype":"rewind_conversation"`) {
		t.Errorf("wrote %s, want the rewind_conversation subtype", line)
	}
	if !strings.Contains(line, `"target_message_uuid":"T"`) {
		t.Errorf("wrote %s, want the target uuid it was given", line)
	}
	if !strings.Contains(line, `"last_seen_user_message_uuid":"S"`) {
		t.Errorf("wrote %s, want the last-seen uuid it was given", line)
	}
}

// TestRewindBuysNoExcuseForAnExitOne is TestSetModeBuysNoExcuseForAnExitOne's
// own case: a rewind aborts nothing, so it must not forgive a startup
// rejection that happens to follow one.
func TestRewindBuysNoExcuseForAnExitOne(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "interrupt-exit")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	if _, err := s.Rewind("T", "S"); err != nil {
		t.Fatalf("Rewind: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	drain(s)

	if err := s.Err(); err == nil {
		t.Error("Err = nil for a session that exited 1 after only a rewind: a rewind aborts no turn, so it owes the ending no excuse - forgiving it here hides a startup rejection")
	}
}

func TestRewindBeforeStartIsAnError(t *testing.T) {
	s := NewSession(Config{SessionID: "s1"})
	if _, err := s.Rewind("T", "S"); err == nil {
		t.Fatal("Rewind on an unstarted session = nil, want an error")
	}
}

func TestRewindAfterStopIsAnError(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := s.Rewind("T", "S"); err == nil {
		t.Fatal("Rewind after Stop = nil, want an error")
	}
}
