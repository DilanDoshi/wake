// Package notice is where a failure goes when there is nowhere to report it.
//
// A TUI owns the terminal. The standard logger writes to stderr, which is the
// alt screen's canvas, so a component that logs corrupts the frame it was in
// the middle of drawing - and a draw loop that fails every frame across 15-30
// sessions makes an unconditional log a flood as well as a corruption. Both
// halves of that are why internal/render's two degradation branches were
// silent, which CLAUDE.md's log-and-skip rule forbids: the obvious fix was
// wrong twice over.
//
// Two callers need this and neither can reach the other. internal/render is a
// draw loop with no error return and no business knowing there is a UI;
// internal/ui.App has a transport failure to show and no stdout to show it on.
// ui imports render, so render cannot import ui, and a shared sink cannot live
// in either. It lives here, in a leaf package that imports nothing.
//
// # What "rate-limited" means here
//
// Once per distinct message, with a count of the repeats. The flood this is
// defending against is one failure recurring on every frame, not many
// different failures arriving at once, so collapsing by message text is the
// bound that matches the hazard: a renderer that has been broken for an hour
// costs one line and a number.
//
// The surface deliberately stops at "the newest distinct failure". A TUI has
// one row to spare, not a log pane, and a notice that scrolls is a log by
// another name - see internal/ui/app.go, which reserves that row.
package notice

import (
	"fmt"
	"sync"
)

// maxDistinct bounds how many different messages are remembered at once.
//
// A sink that grows for the life of the process is a leak, and the text can
// carry a session id or a path, so "distinct" is not a small fixed set. At the
// bound the counts are forgotten and start again, which costs a number nobody
// reads twice and keeps the map bounded whatever a caller formats into it.
const maxDistinct = 64

// repeatFormat is how a message that has happened more than once says so.
const repeatFormat = " (×%d)"

// Notice is one distinct failure and how often it has been reported.
type Notice struct {
	// Text is the message as the caller formatted it.
	Text string

	// Count is how many times this exact text has been reported, including
	// the first. It is always at least one on a Notice that exists.
	Count int
}

// String is the notice as a reader sees it: the message, and the repeat count
// only when there is one worth showing.
func (n Notice) String() string {
	if n.Count <= 1 {
		return n.Text
	}
	return n.Text + fmt.Sprintf(repeatFormat, n.Count)
}

var (
	mu      sync.Mutex
	counts  = map[string]int{}
	newest  string
	present bool
)

// Report records one failure. It is safe to call from any goroutine, from a
// draw loop, and as often as the failure happens: a message already reported
// only increments its count.
//
// It never writes anywhere by itself. Nothing is displayed until whoever owns
// the terminal reads Latest, which is the point - this package cannot be the
// thing that corrupts the screen.
func Report(format string, args ...any) {
	text := fmt.Sprintf(format, args...)

	mu.Lock()
	defer mu.Unlock()
	if _, seen := counts[text]; !seen && len(counts) >= maxDistinct {
		counts = map[string]int{}
	}
	counts[text]++
	newest, present = text, true
}

// Latest returns the most recently reported failure and whether there is one.
//
// It is a read: calling it does not clear anything, because the caller is a
// View that runs on every frame and a notice that vanished after one frame
// would be worse than no notice at all.
func Latest() (Notice, bool) {
	mu.Lock()
	defer mu.Unlock()
	if !present {
		return Notice{}, false
	}
	return Notice{Text: newest, Count: counts[newest]}, true
}

// Count is how many times one exact message has been reported. Zero for a
// message that never has been.
func Count(text string) int {
	mu.Lock()
	defer mu.Unlock()
	return counts[text]
}

// Reset forgets everything reported so far.
//
// Exported for tests, which is a real cost: this is process-global state, and
// a test asserting on it has to be able to start from empty. The alternative -
// threading a sink through render's draw path and ui's View - buys nothing at
// runtime, because there is exactly one terminal per process.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	counts = map[string]int{}
	newest, present = "", false
}
