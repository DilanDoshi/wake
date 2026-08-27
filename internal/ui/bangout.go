package ui

// What a bang is allowed to say, and what happens to the rest of it.

// bangOutput keeps the first limit bytes a command writes and counts the rest
// away, recording that it did.
//
// # Why it does not stop the command
//
// Every Write claims the whole slice, including the bytes it drops. The
// alternative - a short write or an error - makes os/exec's copier stop
// reading, which fills the pipe, which blocks the command on its next write
// until the deadline kills it. `!yes | head -c 200000` would then take thirty
// seconds and report a timeout, for a command that finished in milliseconds
// and whose output Wake already had.
//
// # Why it does not kill the command either
//
// Because passing the cap is a statement about the output, not about the work.
// `!make build 2>&1` crossing 64KB is a build that is going fine; ending it
// there would leave a half-built tree behind and would be Wake deciding that a
// noisy command is a bad one. The command runs to its own end and the
// transcript keeps a bounded, honestly-labelled prefix of what it said.
//
// # Why the buffer is bounded rather than the appends being clever
//
// A DM's scrollback is unbounded for the life of the session, so this buffer is
// not the only copy - it is the first of two, and the second is pinned until
// the conversation ends. Bounding here bounds both.
type bangOutput struct {
	limit int
	buf   []byte

	// truncated records that at least one byte was dropped. It is a fact about
	// the transcript, not a diagnostic: a cut nobody is told about is a wrong
	// transcript rather than a short one.
	truncated bool
}

// Write records as much of p as the cap allows and reports the whole of it
// written. It is called from os/exec's single copying goroutine - one, because
// bangRun gives Stdout and Stderr the same writer - and never concurrently.
func (o *bangOutput) Write(p []byte) (int, error) {
	room := o.limit - len(o.buf)
	switch {
	case room >= len(p):
		o.buf = append(o.buf, p...)
	case room > 0:
		o.buf = append(o.buf, p[:room]...)
		o.truncated = true
	case len(p) > 0:
		o.truncated = true
	}
	return len(p), nil
}

// String is what the command said, up to the cap.
func (o *bangOutput) String() string { return string(o.buf) }
