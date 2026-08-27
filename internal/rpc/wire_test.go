// Unit tests for the frame encoding: the exact bytes on the wire, the
// frame-size ceiling, and how the reader ends. Event fidelity is in
// event_test.go; the transport contract over a real socket and the
// concurrency proof are in conn_test.go. Shared test helpers live here.

package rpc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

// recvTimeout bounds every channel wait in the tests. A transport bug
// should fail a test, not hang the suite until the package timeout.
const recvTimeout = 5 * time.Second

func TestWriteThenReadFrame(t *testing.T) {
	var buf bytes.Buffer
	want := Frame{
		Kind:      FrameEvent,
		SessionID: "s1",
		Event:     &core.Event{Kind: core.KindAssistantText, Text: "hello"},
	}
	if err := WriteFrame(&buf, want); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	frames, errs := ReadFrames(&buf)
	select {
	case got := <-frames:
		if got.Kind != want.Kind || got.SessionID != want.SessionID {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		if got.Event == nil || got.Event.Text != "hello" {
			t.Fatalf("event not round-tripped: %+v", got.Event)
		}
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadFramesHandlesMultipleFramesInOneBuffer(t *testing.T) {
	var buf bytes.Buffer
	for _, txt := range []string{"one", "two", "three"} {
		if err := WriteFrame(&buf, Frame{Kind: FrameSend, Text: txt}); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
	}

	frames, _ := ReadFrames(&buf)
	var got []string
	for f := range frames {
		got = append(got, f.Text)
	}
	if len(got) != 3 || got[0] != "one" || got[2] != "three" {
		t.Fatalf("got %v, want [one two three]", got)
	}
}

func TestWriteFrameEscapesNewlines(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameSend, Text: "a\nb"}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	// One newline: the frame terminator. Otherwise a multi-line message
	// would be read back as two frames.
	if n := bytes.Count(buf.Bytes(), []byte("\n")); n != 1 {
		t.Fatalf("got %d newlines, want 1", n)
	}
}

func TestReadFramesSurfacesMalformedInput(t *testing.T) {
	r := bytes.NewBufferString("{not json\n")
	frames, errs := ReadFrames(r)
	select {
	case f, open := <-frames:
		if open {
			t.Fatalf("want no frame from malformed input, got %+v", f)
		}
	case err := <-errs:
		if err == nil {
			t.Fatal("want a non-nil error")
		}
	}
}

// --- wire contract ------------------------------------------------------

func TestFrameKindsArePinnedToTheirWireStrings(t *testing.T) {
	// A second client - the SwiftUI app the design plans as a later
	// frontend - matches on these literals without sharing this package.
	// Renaming one is a protocol break, so the strings are pinned here.
	want := map[string]string{
		"FrameEvent": "event",
		"FrameSend":  "send",
		"FrameSpawn": "spawn",
		"FrameHello": "hello",
		"FrameError": "error",
		"FrameAllow": "allow",
		"FrameDeny":  "deny",
	}
	got := map[string]string{
		"FrameEvent": FrameEvent,
		"FrameSend":  FrameSend,
		"FrameSpawn": FrameSpawn,
		"FrameHello": FrameHello,
		"FrameError": FrameError,
		"FrameAllow": FrameAllow,
		"FrameDeny":  FrameDeny,
	}
	for name, w := range want {
		if got[name] != w {
			t.Errorf("%s = %q, want %q", name, got[name], w)
		}
	}
}

// A role crosses the socket, an ordinary spawn carries none, and a role this
// build does not know is carried through rather than corrected.
//
// The third of those is the transport's own rule read once more: this package
// never interprets what it carries. The *meaning* of an unrecognised role is the
// daemon's, and the daemon's answer is "an ordinary agent" - the safe, existing
// and overwhelmingly common case - which is only a safe default because nothing
// down here quietly turned it into something else on the way.
// The key is asserted around one side rather than round-tripped, because a
// round trip proves the reader and the writer agree and says nothing about the
// bytes: renaming the tag to `the_role` on both sides is perfectly consistent
// and unreadable by every other build, including a second client that matches
// these literals without sharing this type. Verified - the round-trip half alone
// survived exactly that mutation.
func TestARoleCrossesTheSocketAndAnOrdinarySpawnCarriesNone(t *testing.T) {
	for _, role := range []string{RoleManager, "something-a-later-build-added"} {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, Frame{Kind: FrameSpawn, SessionID: "id-1", Role: role}); err != nil {
			t.Fatalf("WriteFrame: %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
			t.Fatalf("decode the bytes written: %v", err)
		}
		if raw["role"] != role {
			t.Errorf("the frame on the wire is %s, and its `role` key is %v: this is the word a second "+
				"client matches on without sharing this type", buf.String(), raw["role"])
		}
		// And the other direction, from a literal somebody typed: a reader that
		// stopped understanding the key would decode every manager spawn as an
		// ordinary agent, silently, which is the safe default failing open.
		frames, _ := ReadFrames(strings.NewReader(
			`{"kind":"spawn","session_id":"id-1","role":"` + role + `"}` + "\n"))
		if got := recv(t, frames); got.Role != role {
			t.Errorf("role = %q, want %q: a spawn's role decides what the daemon names the session, and a "+
				"transport that dropped one would decide it here", got.Role, role)
		}
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameSpawn, SessionID: "id-1"}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if strings.Contains(buf.String(), "role") {
		t.Errorf("an ordinary spawn carries %q. Empty is what every existing client sends and what every "+
			"existing frame means, so the key is absent rather than present and blank", buf.String())
	}
}

// The role's wire word is pinned for the reason every frame kind is: a second
// client matches on the literal without sharing this package.
//
// It is spelled here rather than aliased to core.ManagerName, which is the same
// string today. They are two different claims - one is what a person types in a
// composer, the other is what a spawn frame says it is asking for - and tying
// them together would make a change to either one a silent change to the other.
func TestTheManagerRoleIsPinnedToItsWireString(t *testing.T) {
	if RoleManager != "manager" {
		t.Errorf("RoleManager = %q, want %q", RoleManager, "manager")
	}
}

func TestWriteFrameOmitsEmptyFields(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameHello}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	// A hello carries nothing else. Emitting empty session_id, text and a
	// null event on every frame would cost real bytes across a fleet.
	if got := buf.String(); got != `{"kind":"hello"}`+"\n" {
		t.Errorf("frame = %q, want %q", got, `{"kind":"hello"}`+"\n")
	}
}

func TestWriteFrameTerminatesEveryFrameWithExactlyOneNewline(t *testing.T) {
	var buf bytes.Buffer
	for i := range 3 {
		f := Frame{Kind: FrameEvent, SessionID: "s1", Event: &core.Event{
			Kind: core.KindAssistantText,
			Text: "line\nwith\nbreaks",
		}}
		if err := WriteFrame(&buf, f); err != nil {
			t.Fatalf("WriteFrame %d: %v", i, err)
		}
	}
	if n := bytes.Count(buf.Bytes(), []byte("\n")); n != 3 {
		t.Fatalf("got %d newlines for 3 frames, want 3", n)
	}
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Error("last frame is not newline-terminated")
	}
}

// --- frame size ---------------------------------------------------------

func TestWriteFrameDoesNotEscapeAngleBrackets(t *testing.T) {
	// Go's encoder escapes <, > and & into six-byte \u00XX sequences so
	// JSON can be embedded in a <script> tag without injection. Wake writes
	// to a unix socket read by a Go decoder, so that threat model does not
	// exist here and the escaping is pure inflation - 6x on those bytes.
	//
	// It is not merely wasteful. The cost scales with angle-bracket
	// density, not corpus size: a tool result that is ~19% brackets, as
	// below, nearly doubles. Against maxFrameBytes that turns a frame that
	// fit into an ErrTooLong, which ends the connection for every session
	// on it rather than just dropping the one frame.
	body := strings.Repeat(`<div class="x">a & b</div>`, 100)
	frame := Frame{Kind: FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindToolResult,
		Text: body,
	}}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, frame); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	for _, esc := range []string{"\\u003c", "\\u003e", "\\u0026"} {
		if bytes.Contains(buf.Bytes(), []byte(esc)) {
			t.Errorf("frame contains %s - HTML escaping is back on", esc)
		}
	}
	if !bytes.Contains(buf.Bytes(), []byte(`<div class=\"x\">a & b</div>`)) {
		t.Error("angle brackets did not survive as single bytes")
	}

	// Compare against what the default encoder would produce, so a
	// refactor back to json.Marshal fails here rather than silently
	// reinflating the wire.
	escaped, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	// +1: Marshal omits the terminator Encode adds, so comparing raw
	// lengths would let a one-byte regression through.
	if len(escaped)+1 <= buf.Len() {
		t.Fatalf("default encoder produced %d bytes (+1 terminator) against our %d - expected it to be larger",
			len(escaped), buf.Len())
	}
	t.Logf("escaping would cost %d bytes on %d (%.2fx)", len(escaped)-buf.Len(), buf.Len(),
		float64(len(escaped))/float64(buf.Len()))

	// And it still has to survive the trip.
	got := roundTrip(t, frame)
	if got.Event == nil || got.Event.Text != body {
		t.Error("payload did not survive the round trip")
	}
}

func TestLargeFrameSurvivesRoundTrip(t *testing.T) {
	// bufio.Scanner's 64KB default truncates *silently*, and one tool
	// result - a Read of a large file - clears 64KB without trying. The
	// recorded corpus does not: its largest line is 14,778 bytes, so the
	// ceiling is sized for the traffic Wake will see rather than for the
	// traffic it has recorded.
	const payload = 1 << 20 // 1 MiB, ~71x the largest recorded line
	body := strings.Repeat("wake", payload/4)

	got := roundTrip(t, Frame{Kind: FrameEvent, SessionID: "s1", Event: &core.Event{
		Kind: core.KindToolResult,
		Text: body,
		Tool: &core.ToolCall{ID: "toolu_1", Name: "Read"},
	}})
	if got.Event == nil {
		t.Fatal("event dropped on the wire")
	}
	if len(got.Event.Text) != len(body) {
		t.Fatalf("text = %d bytes, want %d - the frame was truncated", len(got.Event.Text), len(body))
	}
	if got.Event.Text != body {
		t.Error("text round-tripped at the right length but with different bytes")
	}
}

func TestOversizedFrameIsAnErrorNotATruncation(t *testing.T) {
	// Past the ceiling the reader must say so. Truncating here would hand
	// a consumer a frame that parses but is not what was sent.
	r := io.MultiReader(
		&fillReader{b: 'x', left: maxFrameBytes + 1},
		strings.NewReader("\n"),
	)

	frames, errs := ReadFrames(r)
	for f := range frames {
		t.Fatalf("got a frame from an oversized line: %+v", f)
	}
	err := <-errs
	if err == nil {
		t.Fatal("want an error for a line past maxFrameBytes, got nil")
	}
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("err = %v, want it to wrap bufio.ErrTooLong", err)
	}
}

// --- reader lifecycle ---------------------------------------------------

func TestReadFramesClosesBothChannelsAtEOF(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameHello}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	frames, errs := ReadFrames(&buf)
	if f := recv(t, frames); f.Kind != FrameHello {
		t.Fatalf("Kind = %q, want %q", f.Kind, FrameHello)
	}
	if f, open := <-frames; open {
		t.Fatalf("want frames closed after EOF, got %+v", f)
	}
	// A client that vanishes is the ordinary case, not an error: the
	// closed connection reads as EOF and errs closes with nothing on it.
	if err, open := <-errs; open {
		t.Fatalf("want errs closed with no error, got %v", err)
	}
}

func TestReadFramesSkipsBlankLines(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("\n")
	if err := WriteFrame(&buf, Frame{Kind: FrameSend, Text: "one"}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	buf.WriteString("\n\n")

	frames, errs := ReadFrames(&buf)
	var got []Frame
	for f := range frames {
		got = append(got, f)
	}
	if err := <-errs; err != nil {
		t.Fatalf("ReadFrames: %v", err)
	}
	if len(got) != 1 || got[0].Text != "one" {
		t.Fatalf("got %+v, want exactly one frame carrying \"one\"", got)
	}
}

func TestReadFramesDeliversFramesBeforeAMalformedOneThenStops(t *testing.T) {
	// A bad frame ends the connection rather than crashing the loop or
	// laundering corruption into plausible frames: everything before it is
	// delivered, the error is surfaced once, both channels close.
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameSend, Text: "before"}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	buf.WriteString("{not json\n")
	if err := WriteFrame(&buf, Frame{Kind: FrameSend, Text: "after"}); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	frames, errs := ReadFrames(&buf)
	var got []string
	for f := range frames {
		got = append(got, f.Text)
	}
	if len(got) != 1 || got[0] != "before" {
		t.Fatalf("got %v, want [before]", got)
	}
	err := <-errs
	if err == nil {
		t.Fatal("want an error for the malformed frame")
	}
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Errorf("err = %v, want it to wrap a *json.SyntaxError", err)
	}
	if err, open := <-errs; open {
		t.Fatalf("want errs closed after the error, got a second one: %v", err)
	}
}

func TestReadFramesSurfacesAReaderError(t *testing.T) {
	// A connection that breaks mid-stream - the socket half of a client
	// vanishing - must surface, not look like a clean EOF.
	boom := errors.New("connection reset")
	pr, pw := io.Pipe()
	go func() {
		if err := WriteFrame(pw, Frame{Kind: FrameHello}); err != nil {
			t.Errorf("WriteFrame: %v", err)
		}
		if err := pw.CloseWithError(boom); err != nil {
			t.Errorf("CloseWithError: %v", err)
		}
	}()

	frames, errs := ReadFrames(pr)
	if f := recv(t, frames); f.Kind != FrameHello {
		t.Fatalf("Kind = %q, want %q", f.Kind, FrameHello)
	}
	for range frames { //nolint:revive // drain to the close
	}
	err := <-errs
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}
}

// --- write errors -------------------------------------------------------

func TestWriteFrameSurfacesAWriterError(t *testing.T) {
	boom := errors.New("broken pipe")
	err := WriteFrame(errWriter{err: boom}, Frame{Kind: FrameHello})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}
}

func TestWriteFrameRefusesAnEventItCannotEncode(t *testing.T) {
	// ToolCall.Input is a map[string]any, so it is the one part of an
	// Event that can hold something JSON cannot express. It must break
	// loudly, with nothing written, rather than emitting a partial frame
	// that would desync the stream for every other session.
	var buf bytes.Buffer
	err := WriteFrame(&buf, Frame{Kind: FrameEvent, Event: &core.Event{
		Kind: core.KindToolUse,
		Tool: &core.ToolCall{ID: "toolu_1", Input: map[string]any{"ch": make(chan int)}},
	}})
	if err == nil {
		t.Fatal("want an error for an event that cannot be encoded")
	}
	if !strings.Contains(err.Error(), "marshal frame") {
		t.Errorf("err = %v, want it to name the marshal step", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes for a frame that failed to encode, want 0", buf.Len())
	}
}

func TestWriteFrameReleasesTheLockAfterAWriteError(t *testing.T) {
	// A client dying mid-write must not wedge every other session's
	// writes behind a lock nobody will ever unlock.
	if err := WriteFrame(errWriter{err: errors.New("broken pipe")}, Frame{Kind: FrameHello}); err == nil {
		t.Fatal("want an error from the failing writer")
	}

	done := make(chan error, 1)
	go func() {
		var buf bytes.Buffer
		done <- WriteFrame(&buf, Frame{Kind: FrameHello})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("WriteFrame after a failed write: %v", err)
		}
	case <-time.After(recvTimeout):
		t.Fatal("WriteFrame deadlocked: the failed write never released the lock")
	}
}

// --- helpers ------------------------------------------------------------

// roundTrip writes one frame and reads it back, failing the test on any
// transport error.
func roundTrip(t *testing.T, f Frame) Frame {
	t.Helper()

	var buf bytes.Buffer
	if err := WriteFrame(&buf, f); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	frames, errs := ReadFrames(&buf)
	var got []Frame
	for f := range frames {
		got = append(got, f)
	}
	// Drained before the error check on purpose: a transport failure is a
	// better message than "no frame arrived".
	if err := <-errs; err != nil {
		t.Fatalf("ReadFrames: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d frames, want 1", len(got))
	}
	return got[0]
}

// recv takes one frame or fails the test rather than blocking forever.
func recv(t *testing.T, frames <-chan Frame) Frame {
	t.Helper()

	select {
	case f, open := <-frames:
		if !open {
			t.Fatal("frames closed before delivering a frame")
		}
		return f
	case <-time.After(recvTimeout):
		t.Fatal("timed out waiting for a frame")
		return Frame{}
	}
}

// errWriter fails every write, standing in for a socket whose peer is gone.
type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

// fillReader yields left copies of b and then EOF, without materializing
// them - an oversized line without a multi-megabyte allocation in the test.
type fillReader struct {
	b    byte
	left int
}

func (r *fillReader) Read(p []byte) (int, error) {
	if r.left == 0 {
		return 0, io.EOF
	}
	n := min(len(p), r.left)
	for i := range p[:n] {
		p[i] = r.b
	}
	r.left -= n
	return n, nil
}

// --- the bound on a client's write ------------------------------------------

// shortWriteTimeout compresses the client write bound for one test. Five
// seconds is the right production value and an impossible one to wait out here.
func shortWriteTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	prev := writeTimeout
	writeTimeout = d
	t.Cleanup(func() { writeTimeout = prev })
}

// drained reads one end of a pipe for the life of a test, and closes both ends
// before it drains.
//
// One cleanup rather than two, and that ordering is the whole reason it exists.
// t.Cleanup is LIFO, so registering "close the pipe" and then "drain until the
// reader ends" runs the drain first - and the drain cannot end until the pipe
// is closed. Two correct-looking cleanups, one deadlock, and a package that
// times out instead of failing.
func drained(t *testing.T, ends ...net.Conn) (<-chan Frame, <-chan error) {
	t.Helper()
	frames, errs := ReadFrames(ends[len(ends)-1])
	t.Cleanup(func() {
		for _, c := range ends {
			_ = c.Close()
		}
		for range frames {
		}
		<-errs
	})
	return frames, errs
}

// The failure this exists for, reached rather than described.
//
// writeMu is process-wide and WriteFrame holds it across the socket write, so a
// peer that has stopped reading parks the writer inside the lock - and every
// other write in the process behind it. net.Pipe is unbuffered, so nobody
// reading is exactly that state.
//
// Mutation check: calling WriteFrame instead of WriteFrameTo leaves this
// failing at "the write to a peer that stopped reading never returned within
// 3s: it is parked inside the process-wide write lock".
func TestWriteFrameToGivesUpOnAPeerThatStoppedReading(t *testing.T) {
	shortWriteTimeout(t, 50*time.Millisecond)

	mine, theirs := net.Pipe()
	t.Cleanup(func() { _ = mine.Close(); _ = theirs.Close() })

	done := make(chan error, 1)
	go func() { done <- WriteFrameTo(mine, Frame{Kind: FrameSend, Text: "nobody is reading this"}) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("writing to a peer that never read anything reported success")
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("the write failed with %v, want a deadline", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the write to a peer that stopped reading never returned within 3s: it is parked inside the process-wide write lock")
	}
}

// And the lock it was holding is free afterwards, which is the half that
// matters to everyone else in the process. A bound that expired but left the
// lock held would have moved the failure rather than closed it.
func TestAStalledWriteDoesNotStallEveryOtherClient(t *testing.T) {
	shortWriteTimeout(t, 50*time.Millisecond)

	stalled, deaf := net.Pipe()
	t.Cleanup(func() { _ = stalled.Close(); _ = deaf.Close() })

	// Bounded even though it is only the setup. writeMu is process-wide, so a
	// bug that leaks it - which is the bug this file is about - would park this
	// line forever and turn a failure into a package timeout. A mutation that
	// hangs is indistinguishable from one that passes.
	stall := make(chan error, 1)
	go func() { stall <- WriteFrameTo(stalled, Frame{Kind: FrameSend, Text: "into the void"}) }()
	select {
	case err := <-stall:
		if err == nil {
			t.Fatal("the stalled write succeeded, so this test proves nothing")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stalled write never returned: something is holding writeMu with no bound on it")
	}

	// A healthy peer, written to right after. Under the bug this is the write
	// that never happens.
	healthy, reader := net.Pipe()
	frames, _ := drained(t, healthy, reader)

	done := make(chan error, 1)
	go func() { done <- WriteFrameTo(healthy, Frame{Kind: FrameSend, Text: "this one is fine"}) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("a healthy client's write failed after another client stalled: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a healthy client's write is parked behind a stalled one's: the process-wide lock was never released")
	}

	select {
	case f := <-frames:
		if f.Text != "this one is fine" {
			t.Errorf("the healthy client received %+v", f)
		}
	case <-time.After(recvTimeout):
		t.Fatal("the healthy client received nothing")
	}
}

// writerSettle is how long a test waits for a goroutine to have reached the
// step it is being set up at. Generous: these are three-goroutine handshakes on
// a loaded machine, and the alternative to slack is a channel inside
// WriteFrameTo that exists only for tests.
const writerSettle = 50 * time.Millisecond

// Two writers on one connection, which is what bubbletea produces from two
// Enter presses in quick succession: every tea.Cmd runs on its own goroutine.
//
// The setup makes the interleaving deterministic rather than hoping for it. The
// first write is parked inside writeMu with nobody reading; the second has set
// its deadline and is queued for the lock; then exactly one frame is read,
// which lets the first finish and the second start.
//
// Mutation check: restoring the SetWriteDeadline(zero) after WriteFrame leaves
// this failing at "the second write is still parked after 3s with a 200ms
// bound" - the first write's clearing removes a bound the second had already
// set, and the second then parks forever holding the process-wide lock.
func TestASecondWriteOnTheSameConnectionKeepsItsBound(t *testing.T) {
	shortWriteTimeout(t, 200*time.Millisecond)

	mine, theirs := net.Pipe()
	t.Cleanup(func() { _ = mine.Close(); _ = theirs.Close() })

	first := make(chan error, 1)
	go func() { first <- WriteFrameTo(mine, Frame{Kind: FrameHello}) }()
	time.Sleep(writerSettle) // parked in Write, holding writeMu

	second := make(chan error, 1)
	go func() { second <- WriteFrameTo(mine, Frame{Kind: FrameSend, Text: "the second keypress"}) }()
	time.Sleep(writerSettle) // deadline set, queued for writeMu

	// One frame through, and then nothing: the first write completes, the
	// second takes the lock and parks.
	go func() { _, _ = theirs.Read(make([]byte, readBufBytes)) }()

	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("the first write failed, so nothing was ever queued behind it: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first write never completed even though a frame was read")
	}

	select {
	case err := <-second:
		if err == nil {
			t.Fatal("the second write reported success with nobody reading")
		}
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Errorf("the second write failed with %v, want a deadline", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the second write is still parked after 3s with a 200ms bound: its deadline was removed by the write before it, and writeMu is held")
	}
}
