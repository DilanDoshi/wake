// Diagnostics from the pump goroutine, and why they cannot be written there.

package core

import (
	"fmt"
	"log"
)

// logBuffer is how many diagnostic lines a session may run ahead of the
// logger. Decode failures arrive in bursts - a wire change makes every frame
// fail at once - so the slack is worth having, and dropping the 65th line of
// an identical failure costs nothing.
const logBuffer = 64

// logSink carries a session's diagnostics off the pump goroutine.
//
// The pump may never call log.Printf itself. log serializes every write on a
// package-level mutex and hands it to whatever io.Writer the process
// installed. If that writer blocks, the write blocks holding the mutex, and a
// pump that called it parks there.
//
// That park is a fourth route to the failure this file has found three times
// already: closeOnCancel takes away a stdout the pump is not reading, the ctx
// case in send is never reached, cmd.Cancel kills the agent's process group
// but finish never runs - so scanDone never closes, Events() never closes,
// Err() stays nil, and the session holds a live-cap slot while looking
// perfectly healthy.
//
// It needs two things at once: a line DecodeLine rejects, and a blocking sink
// behind log. The second is not exotic - Wake's own stderr being a pipe nobody
// drains is the realistic way to get one, and a daemon that spawns Wake with
// an unread StderrPipe is the same mistake that produced the first route in
// session.go.
//
// So the pump formats the line, which touches no I/O, hands it over and moves
// on. A full buffer drops it: a diagnostic is never worth a session.
//
// # What this costs, which only the daemon can pay
//
// Two limits are deliberate, and neither can be fixed from inside a session -
// a component cannot bound a sink it does not own, which is the same reason
// the bug above existed. Both belong to whoever owns Wake's log output.
//
//   - **A wedged writer seizes log for the whole process, not just for this
//     session.** log serializes every call on a package-global mutex, so the
//     first blocked Write parks every later log caller anywhere in the binary -
//     including the daemon's own logging, which does not exist yet and is
//     precisely what gets added next. run is only the first goroutine to park;
//     one more per affected session then queues behind it. The sessions
//     themselves still end and still free their live-cap slots, which is the
//     whole point of this type. But the failure is process-wide seizure rather
//     than per-session leakage, and the two want different answers: bounding or
//     counting goroutines does nothing here. The fix is upstream - give log a
//     writer that cannot block, or one Wake can close.
//   - **Dropped lines are silent.** Nothing counts them, because a counter
//     needs somewhere to be reported and every route to reporting it from here
//     is another way for the pump to block. A session that loses diagnostics
//     under burst cannot say so. If that matters, count it in the daemon's
//     logger, where blocking is somebody else's problem already.
type logSink struct {
	lines chan string
}

func newLogSink() *logSink {
	return &logSink{lines: make(chan string, logBuffer)}
}

// printf formats a line and hands it to the logger. It never blocks, and it
// is the only form of logging the pump goroutine is allowed to do.
//
// The formatting happens here, on the caller's goroutine, for two reasons, and
// the second is the load-bearing one. It touches no I/O, so it cannot park the
// pump. And **the arguments alias the scanner's buffer**: emit passes
// sc.Bytes(), which bufio reuses and overwrites on the next Scan, so those
// bytes have to become a string before this returns. Go evaluates a select's
// send expressions once, on entry, so Sprintf runs even when the drop path is
// taken - which is what makes the drop safe as well as cheap.
//
// The obvious optimization is to send the format and the args and let run do
// the work. Do not. It hands the sink a window into a buffer that has already
// moved on, and it fails the way scanner aliasing always fails: silently, with
// a line that looks entirely plausible and belongs to a different frame. That
// is the bug that made TestEventRawSurvivesTheScannersReusedBuffer worthless
// in its first draft.
func (l *logSink) printf(format string, args ...any) {
	select {
	case l.lines <- fmt.Sprintf(format, args...):
	default:
		// Dropped rather than queued. Nothing counts the drops on purpose:
		// the counter would need somewhere to be reported, and every route to
		// reporting it from here is another way for the pump to block.
	}
}

// run writes what it is handed until the sink closes. It is the one place in
// a session that is allowed to block on log, because it holds nothing anybody
// is waiting for.
func (l *logSink) run() {
	for line := range l.lines {
		log.Print(line)
	}
}

// close retires run once the last line has been handed over.
//
// Only the pump sends, so only the pump may call this - and it must be the
// last thing the pump does. **A send on a closed channel panics even inside a
// select with a default**, so a printf after this is fatal, and an unrecovered
// panic on the pump goroutine takes the daemon and every other session with
// it. That is why pump registers this defer first, so it runs last: finish is
// the natural place to log why a session ended, and it has to be able to.
func (l *logSink) close() {
	close(l.lines)
}
