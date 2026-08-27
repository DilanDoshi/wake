// The process log sink. core/sessionlog.go names this file's job in advance:
// "a component cannot bound a sink it does not own... The fix is upstream -
// give log a writer that cannot block, or one Wake can close."
//
// The failure being prevented is process-wide, not per-session. log serializes
// every call on a package-global mutex, so one blocked Write parks every later
// log caller anywhere in the binary - every session's diagnostics goroutine,
// and the daemon's own.

package daemon

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// The property, stated directly: whatever is behind the log, logging returns.
func TestAWedgedLogWriterCannotParkALogCaller(t *testing.T) {
	blocked := newBlockingWriter()
	sink := newLogSink(blocked)
	go sink.run()

	// Cleanups run LIFO, and the order is load-bearing: close waits for the
	// writer goroutine, and that goroutine is parked inside the wedged write
	// this test installed - so the release has to happen first or the cleanup
	// deadlocks. Registered close, release, restore; they run restore,
	// release, close.
	t.Cleanup(sink.close)
	t.Cleanup(blocked.release)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	log.SetOutput(sink)

	// The first line reaches the writer and stays there.
	log.Print("this one wedges")
	select {
	case <-blocked.entered:
	case <-time.After(testTimeout):
		t.Fatal("nothing reached the writer, so this test never entered the state it is about")
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range logQueue * 2 {
			log.Printf("line %d", i)
		}
	}()
	select {
	case <-done:
	case <-time.After(testTimeout):
		t.Fatal("logging parked behind a wedged writer: every log caller in the process is stuck, including every session's diagnostics goroutine")
	}
}

// A dropped diagnostic must not be a silent one. core drops lines on purpose
// and cannot count them - "the counter would need somewhere to be reported,
// and every route to reporting it from here is another way for the pump to
// block" - and names the daemon's logger as the place that can.
func TestDroppedLogLinesAreConfessed(t *testing.T) {
	blocked := newBlockingWriter()
	sink := newLogSink(blocked)
	go sink.run()

	for i := range logQueue * 2 {
		if _, err := sink.Write([]byte("line\n")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}
	<-blocked.entered
	blocked.release()
	sink.close()

	if got := blocked.String(); !strings.Contains(got, "log lines were dropped") {
		t.Errorf("the log never admits losing anything:\n%s", firstLines(got, 3))
	}
}

// The bytes have to land somewhere a person can read afterwards, and the
// standard logger has to be put back the way it was found.
func TestOpenLogWritesBesideTheSocketAndRestoresTheLogger(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	var before bytes.Buffer
	log.SetOutput(&before)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	l, err := OpenLog(sock)
	if err != nil {
		t.Fatalf("OpenLog: %v", err)
	}
	logf("wake: a line for the file")
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(filepath.Dir(sock), logFileName))
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	if !strings.Contains(string(data), "a line for the file") {
		t.Errorf("log file = %q, want the line that was written", data)
	}

	// Closing twice is what a deferred Close plus an explicit one looks
	// like, and it must not close the file twice.
	if err := l.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	logf("wake: after the close")
	if !strings.Contains(before.String(), "after the close") {
		t.Errorf("the previous log writer was not restored: %q", before.String())
	}
}

// blockingWriter stands in for a sink that has stopped taking bytes: a full
// disk, an NFS mount that went away, a pipe nobody drains.
type blockingWriter struct {
	entered chan struct{}
	freed   chan struct{}
	once    sync.Once

	mu  sync.Mutex
	buf bytes.Buffer
}

func newBlockingWriter() *blockingWriter {
	return &blockingWriter{entered: make(chan struct{}), freed: make(chan struct{})}
}

func (w *blockingWriter) Write(p []byte) (int, error) {
	select {
	case <-w.entered:
	default:
		close(w.entered)
		<-w.freed // the first write is the one that wedges
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func (w *blockingWriter) release() { w.once.Do(func() { close(w.freed) }) }

func (w *blockingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func firstLines(s string, n int) string {
	lines := strings.SplitN(s, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// Serve must not be parkable by whatever the caller left behind the standard
// logger.
//
// The dangerous case is real and specific: a `wake daemon` whose stderr is a
// pipe nobody drains. The first logf parks holding log's package-global mutex,
// and every later one anywhere in the process queues behind it - including the
// three inside shutdown, which would leave a daemon that cannot exit. Relying
// on cmd/wake to have called OpenLog is relying on someone remembering.
func TestServeGuardsTheLoggerItWasHandedRatherThanTrustingIt(t *testing.T) {
	blocked := newBlockingWriter()
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	t.Cleanup(blocked.release)
	log.SetOutput(blocked)

	d := startDaemon(t)
	c := attach(t, d.socket)

	// Something the daemon logs about, so the first write reaches the wedged
	// writer and stays there.
	c.send(rpc.Frame{Kind: rpc.FrameStop, SessionID: "ghost"})
	c.await("an error", func(f rpc.Frame) bool { return f.Kind == rpc.FrameError })

	logged := make(chan struct{})
	go func() {
		defer close(logged)
		for range logQueue * 2 {
			logf("wake: a line nobody can write")
		}
	}()
	select {
	case <-logged:
	case <-time.After(testTimeout):
		t.Fatal("logging parked while a daemon was running: every logf in the process is stuck, and shutdown's are among them")
	}

	// And the daemon can still be shut down, which is the half that matters.
	d.cancel()
	d.waitForExit(t)
}

// The guard must not double-wrap or take the log away from a caller that did
// the deliberate thing.
func TestGuardingALoggerThatIsAlreadyGuardedChangesNothing(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	l, err := OpenLog(sock)
	if err != nil {
		t.Fatalf("OpenLog: %v", err)
	}
	installed := log.Writer()

	undo := guardLog()
	if log.Writer() != installed {
		t.Error("guardLog replaced a sink that was already there")
	}
	undo()
	if log.Writer() != installed {
		t.Error("undoing a guard that did nothing changed the logger anyway")
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
