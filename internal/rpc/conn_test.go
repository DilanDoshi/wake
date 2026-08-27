// Transport contract tests: many sessions fanning out to one connection,
// and the daemon's side of a client that vanishes and comes back. The
// socket tests run over a real unix socket because EOF-versus-error is a
// property of the medium that a bytes.Buffer cannot reproduce.
//
// A socket does NOT prove serialization, and reading these tests as if it
// did is the trap. A single small Write to a unix socket is atomic, so
// TestFramesFanOutOverAUnixSocket passes with writeMu deleted - verified by
// deleting it. TestConcurrentWritersDoNotInterleave is the proof, and it
// writes through chunkWriter precisely because that atomicity would hide
// the bug. Do not consolidate the two.

package rpc

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/core"
)

func TestConcurrentWritersDoNotInterleave(t *testing.T) {
	// Every session in the fleet writes to the same connection. Two
	// unserialized Writes splice their bytes together and the stream never
	// recovers, so this writes into a deliberately unsynchronized sink
	// that yields mid-frame: without WriteFrame's lock, -race reports the
	// data race and the frames come back unparseable.
	const (
		writers         = 8
		framesPerWriter = 40
	)

	sink := &chunkWriter{}
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range framesPerWriter {
				f := Frame{
					Kind:      FrameEvent,
					SessionID: fmt.Sprintf("s%02d", w),
					Event: &core.Event{
						Kind: core.KindAssistantText,
						Text: frameText(w, i),
					},
				}
				if err := WriteFrame(sink, f); err != nil {
					t.Errorf("WriteFrame(%d,%d): %v", w, i, err)
					return
				}
			}
		}()
	}
	wg.Wait()

	got := readAll(t, bytes.NewReader(sink.buf))
	if len(got) != writers*framesPerWriter {
		t.Fatalf("got %d frames, want %d - bytes were interleaved", len(got), writers*framesPerWriter)
	}

	seen := make(map[string]int, len(got))
	for _, f := range got {
		if f.Event == nil {
			t.Fatalf("frame lost its event: %+v", f)
		}
		seen[f.Event.Text]++
	}
	for w := range writers {
		for i := range framesPerWriter {
			if n := seen[frameText(w, i)]; n != 1 {
				t.Fatalf("frame (%d,%d) appeared %d times, want 1", w, i, n)
			}
		}
	}
}

func TestFramesFanOutOverAUnixSocket(t *testing.T) {
	// This passes without writeMu - a small socket Write is atomic, so the
	// medium hides interleaving. The serialization proof is
	// TestConcurrentWritersDoNotInterleave. What this test does prove is
	// that the frames survive a real conn and a real hangup.
	const (
		sessions         = 6
		framesPerSession = 25
	)

	sock := tempSocket(t)
	ln := listen(t, sock)
	done := readClient(t, sock)

	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}

	var wg sync.WaitGroup
	for s := range sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range framesPerSession {
				f := Frame{
					Kind:      FrameEvent,
					SessionID: fmt.Sprintf("s%02d", s),
					Event:     &core.Event{Kind: core.KindAssistantText, Text: frameText(s, i)},
				}
				if err := WriteFrame(conn, f); err != nil {
					t.Errorf("WriteFrame(%d,%d): %v", s, i, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if err := conn.Close(); err != nil {
		t.Fatalf("close server conn: %v", err)
	}

	res := waitClient(t, done)
	if res.err != nil {
		t.Fatalf("client read: %v", res.err)
	}
	if len(res.frames) != sessions*framesPerSession {
		t.Fatalf("client got %d frames, want %d", len(res.frames), sessions*framesPerSession)
	}
	seen := make(map[string]int, len(res.frames))
	for _, f := range res.frames {
		if f.Event == nil {
			t.Fatalf("frame lost its event: %+v", f)
		}
		seen[f.Event.Text]++
	}
	for s := range sessions {
		for i := range framesPerSession {
			if n := seen[frameText(s, i)]; n != 1 {
				t.Fatalf("frame (%d,%d) appeared %d times, want 1", s, i, n)
			}
		}
	}
}

func TestServerSurvivesAClientVanishingAndReconnecting(t *testing.T) {
	// The whole reason the daemon exists: closing the TUI must not take
	// anything down with it. A client that disappears reads as a clean EOF
	// on the server, and the next client is just another connection.
	sock := tempSocket(t)
	ln := listen(t, sock)

	for _, text := range []string{"first attach", "second attach"} {
		sent := make(chan error, 1)
		go func() {
			conn, err := net.Dial("unix", sock)
			if err != nil {
				sent <- fmt.Errorf("dial: %w", err)
				return
			}
			if err := WriteFrame(conn, Frame{Kind: FrameSend, Text: text}); err != nil {
				_ = conn.Close()
				sent <- err
				return
			}
			sent <- conn.Close() // the client vanishes
		}()

		conn, err := ln.Accept()
		if err != nil {
			t.Fatalf("Accept: %v", err)
		}
		frames, errs := ReadFrames(conn)
		if f := recv(t, frames); f.Text != text {
			t.Fatalf("Text = %q, want %q", f.Text, text)
		}
		if f, open := <-frames; open {
			t.Fatalf("want frames closed when the client vanished, got %+v", f)
		}
		if err, open := <-errs; open {
			t.Fatalf("want a clean EOF for a client that closed, got %v", err)
		}
		if err := <-sent; err != nil {
			t.Fatalf("client: %v", err)
		}
		if err := conn.Close(); err != nil {
			t.Fatalf("close server conn: %v", err)
		}
	}
}

func TestReadFramesEndsWhenTheConnectionIsClosedUnderIt(t *testing.T) {
	// The daemon's own shutdown path: it closes the connection and the
	// reader goroutine must end rather than sit on a blocked Read.
	sock := tempSocket(t)
	ln := listen(t, sock)

	client, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil && !isClosed(err) {
			t.Errorf("close client: %v", err)
		}
	}()

	conn, err := ln.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	frames, errs := ReadFrames(conn)
	if err := conn.Close(); err != nil {
		t.Fatalf("close server conn: %v", err)
	}

	select {
	case f, open := <-frames:
		if open {
			t.Fatalf("want no frame from a closed conn, got %+v", f)
		}
	case <-time.After(recvTimeout):
		t.Fatal("reader did not end after the connection closed")
	}
	// Closing out from under a blocked Read is a use-of-closed-conn error,
	// not an EOF. It must be reported, not swallowed.
	select {
	case <-errs:
	case <-time.After(recvTimeout):
		t.Fatal("errs never closed after the connection closed")
	}
}

// --- helpers ------------------------------------------------------------

type clientResult struct {
	frames []Frame
	err    error
}

// readClient dials the socket and drains every frame until the server
// hangs up.
func readClient(t *testing.T, sock string) <-chan clientResult {
	t.Helper()

	done := make(chan clientResult, 1)
	go func() {
		conn, err := net.Dial("unix", sock)
		if err != nil {
			done <- clientResult{err: fmt.Errorf("dial: %w", err)}
			return
		}
		defer func() { _ = conn.Close() }()

		frames, errs := ReadFrames(conn)
		var got []Frame
		for f := range frames {
			got = append(got, f)
		}
		done <- clientResult{frames: got, err: <-errs}
	}()
	return done
}

func waitClient(t *testing.T, done <-chan clientResult) clientResult {
	t.Helper()

	select {
	case res := <-done:
		return res
	case <-time.After(recvTimeout):
		t.Fatal("timed out waiting for the client to finish")
		return clientResult{}
	}
}

func listen(t *testing.T, sock string) net.Listener {
	t.Helper()

	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("listen %s: %v", sock, err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil && !isClosed(err) {
			t.Errorf("close listener: %v", err)
		}
	})
	return ln
}

// tempSocket returns a short path for a unix socket. It is short on
// purpose: sun_path is capped near 104 bytes on darwin, and a nested
// t.TempDir() name can push a socket past it.
func tempSocket(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "wakerpc")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("RemoveAll: %v", err)
		}
	})
	return filepath.Join(dir, "s")
}

func isClosed(err error) bool {
	return err != nil && strings.Contains(err.Error(), "use of closed")
}

// readAll drains a reader into a slice, failing on any transport error.
func readAll(t *testing.T, r *bytes.Reader) []Frame {
	t.Helper()

	frames, errs := ReadFrames(r)
	var got []Frame
	for f := range frames {
		got = append(got, f)
	}
	if err := <-errs; err != nil {
		t.Fatalf("ReadFrames: %v", err)
	}
	return got
}

// frameText is long enough to span several chunkWriter writes, so an
// unserialized write lands mid-frame rather than between frames.
func frameText(writer, seq int) string {
	return fmt.Sprintf("w%02d-f%02d-%s", writer, seq, strings.Repeat("x", 256))
}

// chunkWriter writes in small pieces with a scheduling point between them.
// It is deliberately unsynchronized: it is the detector, not the fix.
type chunkWriter struct {
	buf []byte
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	const chunk = 64
	for i := 0; i < len(p); i += chunk {
		w.buf = append(w.buf, p[i:min(i+chunk, len(p))]...)
		runtime.Gosched()
	}
	return len(p), nil
}
