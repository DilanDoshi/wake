// The process's log sink, which the daemon owns because nothing below it can.
//
// core/sessionlog.go carries the other half of this story. A session's pump
// may never call log directly: log serializes every write on a package-global
// mutex and hands it to whatever io.Writer the process installed, so a writer
// that blocks parks the caller *and* every later log caller anywhere in the
// binary. core solved its half by moving the call off the pump. It could not
// solve the rest, and said so: "a component cannot bound a sink it does not
// own... The fix is upstream - give log a writer that cannot block, or one
// Wake can close."
//
// This is that writer. It never blocks, because it is a copy and a
// non-blocking channel send, so log's global mutex is held for a memcpy. A
// wedged file - an NFS mount, a full disk, a pipe nobody drains - costs
// diagnostics and nothing else. The 15-30 agents keep running.
//
// It also closes the specific way the daemon could have created the problem
// for itself: a `wake daemon` forked with a StderrPipe nobody reads is a
// blocking sink behind log, which is the same mistake that produced the first
// wedge in core. EnsureRunning gives the fork /dev/null and this gives it a
// file.

package daemon

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

// logFileName sits next to the socket, so a daemon on a test socket logs
// into the test's own directory.
const logFileName = "daemon.log"

// logQueue is how many lines may be in flight to the file. Diagnostics arrive
// in bursts - a wire change makes every session's decoder fail at once - and
// dropping the 1025th copy of one failure costs nothing.
const logQueue = 1024

// logPerm keeps the log to its owner: it carries session names and the tails
// of agents' stderr.
const logPerm = 0o600

// logf is how this package says anything. It goes through the standard
// logger, which is only safe because OpenLog put a writer behind it that
// cannot block - see the file header.
func logf(format string, args ...any) {
	log.Printf(format, args...)
}

// guardLog makes sure nothing behind the standard logger can park a caller,
// and returns the undo.
//
// Serve calls it, because Serve is the thing that must not be parkable and it
// cannot rely on having been set up correctly. OpenLog is the deliberate
// version - a file, chosen by the daemon binary - and this is the safety net
// under it: if a sink is already installed, it does nothing; otherwise it
// wraps whatever writer is there in one that cannot block. Wrapping rather
// than replacing, so a daemon run from a terminal still logs to that terminal
// and a test still logs to the test's output.
//
// Without it, a `wake daemon` whose stderr is a pipe nobody drains parks the
// first logf and then every later one anywhere in the process - including
// shutdown's, which would leave a daemon that cannot exit. That is the wedge
// core/sessionlog.go names, re-entered through the one door left open.
func guardLog() func() {
	prev := log.Writer()
	if _, guarded := prev.(*logSink); guarded {
		return func() {}
	}
	sink := newLogSink(prev)
	go sink.run()
	log.SetOutput(sink)
	return func() {
		log.SetOutput(prev)
		// stop, not close: close waits for the writer goroutine, and the
		// whole reason this exists is that the writer behind it might never
		// return. A goroutine holding nothing is the acceptable end of that;
		// a Serve that cannot return is not.
		sink.stop()
	}
}

// Log is the daemon's installed log sink. Close it to flush and put the
// standard logger back.
type Log struct {
	file io.WriteCloser
	sink *logSink
	prev io.Writer
	once sync.Once
}

// OpenLog points the process logger at daemon.log beside the socket.
//
// It is the daemon process's job, not Serve's: Serve is called by tests and by
// cmd/wake alike, and a library call that quietly redirects the whole
// process's logging is a surprise. `wake daemon` calls this once, before
// Serve, and defers the Close.
func OpenLog(socket string) (*Log, error) {
	path := filepath.Join(filepath.Dir(socket), logFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, logPerm)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	sink := newLogSink(f)
	go sink.run()

	l := &Log{file: f, sink: sink, prev: log.Writer()}
	log.SetOutput(sink)
	return l, nil
}

// Close flushes what it can, restores the previous log writer, and closes the
// file. Restoring first, so nothing logged during the close can arrive at a
// sink that is retiring.
func (l *Log) Close() error {
	var err error
	l.once.Do(func() {
		log.SetOutput(l.prev)
		l.sink.close()
		err = l.file.Close()
	})
	return err
}

// logSink is an io.Writer whose Write never blocks.
type logSink struct {
	lines   chan []byte
	dropped atomic.Int64

	out     io.Writer
	closing chan struct{}
	done    chan struct{}
	once    sync.Once
}

func newLogSink(out io.Writer) *logSink {
	return &logSink{
		lines:   make(chan []byte, logQueue),
		out:     out,
		closing: make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Write hands a line to the writer goroutine and returns.
//
// The copy is mandatory: log.Logger formats into a buffer it reuses on the
// next call, so keeping p would hand the sink a window onto somebody else's
// line. It is the same aliasing hazard core's logSink documents about the
// scanner's buffer, arriving from the other direction.
//
// It always reports success. A log writer's error goes back to log.Output,
// which hands it to a caller that discards it; reporting a drop that way would
// be telling nobody. The drop is counted and confessed in the file instead.
func (l *logSink) Write(p []byte) (int, error) {
	line := make([]byte, len(p))
	copy(line, p)
	select {
	case l.lines <- line:
	case <-l.closing:
	default:
		l.dropped.Add(1)
	}
	return len(p), nil
}

func (l *logSink) run() {
	defer close(l.done)
	for {
		select {
		case line := <-l.lines:
			l.emit(line)
		case <-l.closing:
			// Whatever is already queued still gets written: these are the
			// lines about why the daemon is shutting down.
			for {
				select {
				case line := <-l.lines:
					l.emit(line)
				default:
					return
				}
			}
		}
	}
}

// emit writes one line, prefixed by an admission if lines were lost since the
// last one. Nothing here can block anybody: this goroutine holds nothing.
func (l *logSink) emit(line []byte) {
	if n := l.dropped.Swap(0); n > 0 {
		_, _ = fmt.Fprintf(l.out, "wake: %d log lines were dropped\n", n)
	}
	_, _ = l.out.Write(line)
}

// close retires the writer goroutine and waits for it, so the file is not
// closed under a write in progress.
//
// The wait is unbounded on purpose, and it is the one place in this package
// that is. It waits on a goroutine that is either idle or inside a single
// file write; a file write that never returns has already taken the machine
// with it, and the alternative - closing the file under an in-flight write -
// is a real crash for a hypothetical hang.
func (l *logSink) close() {
	l.stop()
	<-l.done
}

// stop retires the writer goroutine without waiting for it, for a caller that
// must not be able to block - see guardLog.
func (l *logSink) stop() {
	l.once.Do(func() { close(l.closing) })
}
