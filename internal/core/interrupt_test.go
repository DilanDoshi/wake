// Interrupting a turn, from the frame Wake writes to the exit code the
// process leaves behind.
//
// EncodeInterrupt has its own tests in encode_test.go: those are about the
// bytes. These are about the two things only a Session can do - mint the
// correlator the receipt comes back with, and remember afterwards that the
// abort was deliberate, so an interrupted process exiting 1 with an empty
// stderr is not reported as a crash.
//
// Like session_test.go this file speaks Claude's wire, and for the same narrow
// reason: the fake process has to produce the recorded shapes to prove
// session.go never names one.

package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// interruptThenExitOne is the recorded exit-1 shape, and it is the whole
// reason half two of this work cannot be split from half one.
//
// It replays testdata/stream/interrupt-then-close.jsonl: an interrupt arrives,
// the receipt goes back carrying the request_id it was sent with, the aborted
// turn produces its marker and its error_during_execution result, and then
// stdin closes with the interrupted turn as the last one - at which point the
// real process exits **1 with zero bytes on stderr** (findings note §6,
// testimony from cmd.Wait at recording time). Without the stderr, that is
// exactly what a startup rejection looks like to exitError.
func interruptThenExitOne() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, initialLineBytes), maxLineBytes)
	for sc.Scan() {
		var f struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
		}
		if err := json.Unmarshal(sc.Bytes(), &f); err != nil {
			continue
		}
		if f.Type != "control_request" || f.Request.Subtype != "interrupt" {
			continue
		}
		// The receipt lands before the aborted result, on all ten fixtures.
		fmt.Printf(`{"type":"control_response","response":{"subtype":"success","request_id":%q,"response":{"still_queued":[]}}}`+"\n", f.RequestID)
		fmt.Println(`{"type":"user","session_id":"s1","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}`)
		fmt.Println(`{"type":"result","subtype":"error_during_execution","is_error":true,"terminal_reason":"aborted_streaming","session_id":"s1","errors":["[ede_diagnostic] result_type=user"]}`)
	}
	// Nothing on stderr. That is the recorded behaviour, and it is the half
	// that makes this indistinguishable from a crash without the flag.
	os.Exit(1)
}

// exitWithCode is a process that does nothing but end a chosen way, so the
// table below can hold real *os.ProcessState values. os does not export a way
// to construct one, and a zero value reports ExitCode -1 - which would make
// every row of that table pass for the wrong reason.
func exitWithCode() {
	code, err := strconv.Atoi(os.Getenv("WAKE_HELPER_EXIT"))
	if err != nil {
		code = 1
	}
	os.Exit(code)
}

// The frame Wake writes is EncodeInterrupt's business and is pinned there.
// What is pinned here is that Interrupt mints a correlator at all, that the
// one it returns is the one that went out, and that a second call does not
// reuse it - the receipt names no session, so a repeated id would make two
// interrupts across a fleet indistinguishable.
func TestInterruptMintsTheRequestIDItSends(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	first, err := s.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	second, err := s.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	if first == "" {
		t.Fatal("Interrupt returned no request id: the receipt would be unattributable")
	}
	if first == second {
		t.Errorf("two interrupts reused request id %q: a receipt cannot say which one it answers", first)
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
			t.Errorf("line %d = %s\nwant the request id Interrupt returned (%s)", i+1, lines[i], want)
		}
	}
}

// cancel_queued is deliberately not on the wire. Without it a message Wake
// queued still runs (interrupt-queued-survives.jsonl); with it that message is
// destroyed and the receipt names it by a uuid Wake never stamped, so nothing
// could tell the operator what went. The transcript has already drawn that
// message as sent - App.submit echoes locally - so destroying it would leave
// the conversation showing a turn that never ran.
func TestInterruptDoesNotDestroyQueuedMessages(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}

	if _, err := s.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	if got := buf.String(); strings.Contains(got, "cancel_queued") {
		t.Errorf("the interrupt carries cancel_queued: %s", got)
	}
}

func TestInterruptBeforeStartIsAnError(t *testing.T) {
	s := NewSession(Config{SessionID: "s1"})
	if _, err := s.Interrupt(); err == nil {
		t.Fatal("Interrupt on an unstarted session = nil, want an error")
	}
}

func TestInterruptAfterStopIsAnError(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if _, err := s.Interrupt(); err == nil {
		t.Fatal("Interrupt after Stop = nil, want an error")
	}
}

// The marker is resolved by its text, so the one thing keeping it from eating
// an ordinary message is which side of the transcript the frame is on.
//
// No recording has an assistant say this, which is exactly why it needs a
// hand-written frame: without one the "user frames only" half of
// interruptNotice is a branch nothing can reach, and an agent whose whole
// message is that sentence - explaining interrupts, quoting a transcript -
// would have it replaced by a label and lost.
func TestAnAssistantSayingTheMarkerIsStillAssistantSpeech(t *testing.T) {
	for _, text := range []string{
		"[Request interrupted by user]",
		"[Request interrupted by user for tool use]",
	} {
		line := fmt.Sprintf(`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":%q}]}}`, text)
		evs, err := DecodeLine([]byte(line))
		if err != nil {
			t.Fatalf("DecodeLine: %v", err)
		}
		if len(evs) != 1 {
			t.Fatalf("decoded to %d events, want 1", len(evs))
		}
		if evs[0].Kind != KindAssistantText {
			t.Errorf("Kind = %q, want %q", evs[0].Kind, KindAssistantText)
		}
		if evs[0].Notice != "" {
			t.Errorf("Notice = %q for assistant text reading %q, want none: the agent's own message would be replaced by a label", evs[0].Notice, text)
		}
		if evs[0].Text != text {
			t.Errorf("Text = %q, want it left alone", evs[0].Text)
		}
	}
}

// errWriteCloser is a stdin that refuses, which is what a session whose
// process has gone looks like from the writing end.
type errWriteCloser struct{ err error }

func (w errWriteCloser) Write([]byte) (int, error) { return 0, w.err }
func (w errWriteCloser) Close() error              { return nil }

// An interrupt that never reached the process interrupted nothing, so it must
// not license suppressing the exit status - which on this path is the status
// that says the write failed. The flag goes up after the write, not before.
func TestAnInterruptThatFailedToWriteIsNotRemembered(t *testing.T) {
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = errWriteCloser{err: errors.New("broken pipe")}

	if _, err := s.Interrupt(); err == nil {
		t.Fatal("Interrupt = nil on a stdin that refuses, want the write error")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.interrupted {
		t.Error("the session recorded an interrupt it never managed to send: its next non-zero exit would be silently swallowed")
	}
}

// The loop end to end through a real process: Wake mints an id, writes the
// frame, and the receipt comes back carrying that same id. Nothing else in
// this tree proves the id on the wire is the id the caller was handed.
func TestTheReceiptCarriesTheRequestIDInterruptMinted(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "interrupt-exit")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	requestID, err := s.Interrupt()
	if err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	receipt := awaitKind(t, s, KindControlReceipt)
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	drain(s)

	if receipt.RequestID != requestID {
		t.Errorf("receipt RequestID = %q, want the id Interrupt minted (%q)", receipt.RequestID, requestID)
	}
	if receipt.SessionID != "s1" {
		t.Errorf("receipt SessionID = %q, want it stamped from the pipe - the frame carries none", receipt.SessionID)
	}
}

// awaitKind takes events off a session until one is of the wanted kind.
func awaitKind(t *testing.T, s *Session, kind EventKind) Event {
	t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev, ok := <-s.Events():
			if !ok {
				t.Fatalf("the session ended before any %s arrived: %v", kind, s.Err())
			}
			if ev.Kind == kind {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %s arrived", kind)
		}
	}
}

// Half two. The same process, the same ending, and the only difference is
// whether Wake asked for it.
//
// Both cases run the identical script and exit 1 with nothing on stderr. The
// one that was interrupted must read as an ordinary ending; the one that was
// not must still be an error, or this guard has bought silence rather than
// accuracy.
func TestOnlyAnInterruptedExitOneIsSuppressed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		interrupt bool
	}{
		{name: "interrupted", interrupt: true},
		{name: "not interrupted", interrupt: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WAKE_HELPER_SCRIPT", "interrupt-exit")
			s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

			if tc.interrupt {
				if _, err := s.Interrupt(); err != nil {
					t.Fatalf("Interrupt: %v", err)
				}
			}
			if err := s.Stop(); err != nil {
				t.Fatalf("Stop: %v", err)
			}
			drain(s)

			err := s.Err()
			if tc.interrupt && err != nil {
				t.Errorf("Err = %v after an interrupt Wake sent, want nil: exit 1 with an empty stderr is what an interrupted process does, and reporting it makes every deliberate abort look like a crash", err)
			}
			if !tc.interrupt && err == nil {
				t.Error("Err = nil for a process that exited 1 on its own: the suppression is not narrow enough to be worth having")
			}
		})
	}
}

// The licence to forgive an exit 1 lapses the moment Wake asks for another
// turn, and this is the case that makes it matter.
//
// §6 records that the exit code follows the *last turn's* is_error, and
// is_error is not exclusive to interrupts - a turn can fail on its own. So a
// session interrupted at 10:00 whose 15:00 turn fails would, with a flag that
// never cleared, report an ordinary ending five hours and several turns later.
// At 15-30 sessions "was interrupted at least once" is most sessions, so that
// is not a corner.
//
// The narrowing is the recording's own: five of the seven runs in §6 exited 0
// because their last turn completed, two of them *after* being interrupted
// earlier in their lives. Once a new turn goes in, the aborted one is no
// longer last and nothing is owed an excuse.
func TestSendingAgainEndsTheLicenceToForgiveAnExitOne(t *testing.T) {
	t.Setenv("WAKE_HELPER_SCRIPT", "interrupt-exit")
	s := startFake(t, Config{SessionID: "s1", Dir: t.TempDir()})

	if _, err := s.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}
	if err := s.Send("carry on, then", nil); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	drain(s)

	if err := s.Err(); err == nil {
		t.Error("Err = nil for a session that exited 1 after a turn Wake asked for: the interrupt was no longer the last thing that happened, and forgiving this ending hides every later failure of a session that was ever interrupted")
	}
}

// The other half of the same rule. A send that never reached the process
// started no turn, so the aborted one is still the last one and the licence
// has to survive - the mirror of an interrupt that failed to write.
func TestASendThatFailedDoesNotEndTheLicence(t *testing.T) {
	var buf bytes.Buffer
	s := NewSession(Config{SessionID: "s1"})
	s.stdin = nopWriteCloser{buf: &buf}
	if _, err := s.Interrupt(); err != nil {
		t.Fatalf("Interrupt: %v", err)
	}

	s.stdin = errWriteCloser{err: errors.New("broken pipe")}
	if err := s.Send("this goes nowhere", nil); err == nil {
		t.Fatal("Send = nil on a stdin that refuses, want the write error")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.interrupted {
		t.Error("a send that failed cleared the interrupt: no new turn started, so the aborted one is still the last")
	}
}

// Answering a permission is not asking for a turn, so it must not end the
// licence either.
//
// An allow or a deny resumes the turn that was already in flight; neither
// starts a new one, so neither displaces the aborted turn as the last thing
// that happened. Clearing on one would push this guard the *other* way -
// reporting a genuine interrupt as a crash, which is the harm the whole thing
// exists to prevent - and no test noticed until a mutation put the call there.
func TestAnsweringAPermissionDoesNotEndTheLicence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(*Session) error
	}{
		{name: "allow", answer: func(s *Session) error { return s.AllowTool("req-1", nil) }},
		{name: "deny", answer: func(s *Session) error { return s.DenyTool("req-1", "not that one") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			s := NewSession(Config{SessionID: "s1"})
			s.stdin = nopWriteCloser{buf: &buf}

			if _, err := s.Interrupt(); err != nil {
				t.Fatalf("Interrupt: %v", err)
			}
			if err := tc.answer(s); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			s.mu.Lock()
			defer s.mu.Unlock()
			if !s.interrupted {
				t.Errorf("a %s cleared the interrupt: it resumes the turn that was already running rather than starting one, so the aborted turn is still the last - and forgetting that reports a deliberate abort as a crash", tc.name)
			}
		})
	}
}

// The suppression has to be exactly as wide as the recorded shape and no
// wider. Every row here is an ending an interrupted session could also reach,
// and every one of them still has to be reported.
func TestTheInterruptSuppressionIsNarrow(t *testing.T) {
	one := &exec.ExitError{ProcessState: exitedWith(t, 1)}

	for _, tc := range []struct {
		name    string
		waitErr error
		tail    string
		want    bool
	}{
		{name: "the recorded shape", waitErr: one, tail: "", want: true},
		{name: "whitespace is still empty", waitErr: one, tail: "  \n\t ", want: true},
		{name: "it said why", waitErr: one, tail: "Error: session id already in use", want: false},
		{name: "a clean exit", waitErr: nil, tail: "", want: false},
		{name: "some other status", waitErr: &exec.ExitError{ProcessState: exitedWith(t, 2)}, tail: "", want: false},
		{
			// A real signalled ending, not a hand-written error: os/exec
			// reports one as an *ExitError too, with ExitCode -1, so this is
			// the row that says the code check is doing the work rather than
			// errors.As happening to miss.
			name:    "a signal, not an exit",
			waitErr: &exec.ExitError{ProcessState: killedState(t)},
			tail:    "",
			want:    false,
		},
		{
			// Wake gave up on stderr rather than waiting on something the
			// agent spawned, so the empty tail is evidence of a held pipe
			// rather than of an interrupt - and exitError has a different
			// thing to say about it. os/exec cannot produce this alongside an
			// exit status, which is why interruptedExit needs no case for it;
			// what this row holds is the outcome, not the branch.
			name:    "the wait bound fired",
			waitErr: fmt.Errorf("exec: WaitDelay expired before I/O complete: %w", exec.ErrWaitDelay),
			tail:    "",
			want:    false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := interruptedExit(tc.waitErr, tc.tail); got != tc.want {
				t.Errorf("interruptedExit(%v, %q) = %v, want %v", tc.waitErr, tc.tail, got, tc.want)
			}
		})
	}
}

// exitedWith produces a real *os.ProcessState for a given status by running a
// process that ends that way.
func exitedWith(t *testing.T, code int) *os.ProcessState {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
	cmd.Env = append(os.Environ(),
		"WAKE_WANT_HELPER=1",
		"WAKE_HELPER_SCRIPT=exit-code",
		"WAKE_HELPER_EXIT="+strconv.Itoa(code),
	)
	err := cmd.Run()
	if cmd.ProcessState == nil {
		t.Fatalf("the helper did not run: %v", err)
	}
	if got := cmd.ProcessState.ExitCode(); got != code {
		t.Fatalf("the helper exited %d, want %d", got, code)
	}
	return cmd.ProcessState
}

// killedState is a real *os.ProcessState for a process that died on a signal.
// ExitCode reports -1 for one, which is the value the table above needs to be
// exercising rather than assuming.
func killedState(t *testing.T) *os.ProcessState {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestHelperProcess", "--")
	cmd.Env = append(os.Environ(), "WAKE_WANT_HELPER=1", "WAKE_HELPER_LINGER=1")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the helper: %v", err)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill the helper: %v", err)
	}
	_ = cmd.Wait()
	if cmd.ProcessState == nil {
		t.Fatal("the helper left no process state")
	}
	if got := cmd.ProcessState.ExitCode(); got != -1 {
		t.Fatalf("the killed helper reports exit code %d, want -1: this row is not testing a signal", got)
	}
	return cmd.ProcessState
}
