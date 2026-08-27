//go:build unix

// C1, end to end, against a real daemon and a real flood: a draw loop that
// stalls for longer than the daemon's patience must not cost the conversation.
//
// This is the only place the whole chain is in one process. internal/ui proves
// the socket keeps draining while nothing draws, over a pipe; internal/daemon
// proves the daemon hangs up on a client that stops taking frames. Neither can
// prove the two together, because neither is allowed to import the other - the
// UI may not know the daemon exists. cmd/wake is where they meet, which makes
// it the only place the defect was ever reachable and the only place a
// regression test for it can live.

package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/daemon"
	"github.com/DilanDoshi/wake/internal/notice"
	"github.com/DilanDoshi/wake/internal/rpc"
	"github.com/DilanDoshi/wake/internal/ui"
)

// stalledRender is how long the draw loop is held up. It has to be longer than
// the daemon's clientWriteTimeout, which is five seconds and not settable from
// outside internal/daemon - so this test costs what it costs. A drag of a
// terminal window across a wide display is measured at more than this on a
// 3,000-event transcript, so it is not a contrived number.
const stalledRender = 7 * time.Second

// floodingClaude stands in for an agent that is producing output the whole time
// - which is the case that matters, because a client only gets hung up on if
// the daemon has something to write to it.
const floodingClaude = `#!/bin/sh
i=0
while [ $i -lt 5000 ]; do
  printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"flood line '"$i"' with enough prose on it to look like an ordinary turn"}]},"session_id":"fake"}'
  i=$((i+1))
done
read _ignored
`

func withFloodingClaude(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte(floodingClaude), 0o755); err != nil {
		t.Fatalf("write the flooding fake claude: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// serveInProcess runs a real daemon for the life of the test.
func serveInProcess(t *testing.T) string {
	t.Helper()

	socket := tempSocket(t)
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- daemon.Serve(ctx, socket) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-errc:
			if err != nil {
				t.Errorf("Serve: %v", err)
			}
		case <-time.After(testTimeout):
			t.Error("the daemon did not exit")
		}
	})

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if info, err := os.Lstat(socket); err == nil && info.Mode()&os.ModeSocket != 0 {
			return socket
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the daemon never started listening")
	return ""
}

// The defect, as a user reaches it: a window drag on a long transcript blocks
// the draw loop, the daemon's write to that client times out, and the daemon
// hangs up on a conversation whose agent is still working.
//
// The stall is injected with tea.WithFilter, which runs on the event loop
// before Update and is therefore exactly where a long render sits. Nothing
// about the daemon, the socket or the agent is faked.
//
// Mutation check: dropping the pump goroutine from ui.NewApp - which is
// precisely the code as it was, one read outstanding on the goroutine that
// renders - leaves this failing at "the daemon hung up on this client while it
// was drawing; the conversation is orphaned".
func TestAStalledDrawLoopDoesNotCostTheConversation(t *testing.T) {
	notice.Reset()
	t.Cleanup(notice.Reset)

	withFloodingClaude(t)
	socket := serveInProcess(t)

	conn, stream, err := connect(socket, io.Discard)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = conn.Close() }()

	sessionID := uuid.NewString()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := rpc.WriteFrameTo(conn, rpc.Frame{
		Kind: rpc.FrameSpawn, SessionID: sessionID, Text: "alex", Dir: dir,
	}); err != nil {
		t.Fatalf("spawn: %v", err)
	}

	// No dialer: reattaching would paper over the very thing under test. What
	// is being asserted is that the client is never hung up on in the first
	// place.
	app := ui.NewRoomApp(conn, stream, nil).WithOpenDM(sessionID, "alex")
	sized, _ := app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	var once sync.Once
	stalled := make(chan struct{})
	quit := make(chan struct{})
	p := tea.NewProgram(sized,
		tea.WithInput(neverReads{quit}),
		tea.WithOutput(io.Discard),
		tea.WithFilter(func(_ tea.Model, msg tea.Msg) tea.Msg {
			// One long render, on the goroutine that draws. Everything the
			// daemon writes in this window has nowhere to go but the socket.
			once.Do(func() {
				time.Sleep(stalledRender)
				close(stalled)
			})
			return msg
		}))

	done := make(chan tea.Model, 1)
	go func() {
		final, err := p.Run()
		if err != nil {
			t.Errorf("the program failed: %v", err)
		}
		done <- final
	}()

	<-stalled
	// Long enough after the stall for anything the daemon decided during it to
	// have arrived and been folded.
	time.Sleep(500 * time.Millisecond)
	p.Quit()
	close(quit)

	var final tea.Model
	select {
	case final = <-done:
	case <-time.After(testTimeout):
		t.Fatal("the program never exited")
	}

	view := stripEscapes(final.View())
	latest, reported := notice.Latest()

	// Asserted on the notice sink, never on notice.Count with a fragment.
	// notice.Count is an exact map lookup on the fully formatted message, so
	// Count("dropped") is 0 however many frames were lost - which is how "0
	// frames lost" got into a report while nearly two hundred thousand had
	// been. Latest() carries the text.
	if reported && strings.Contains(latest.Text, "hung up") {
		t.Fatalf("the daemon hung up on this client while it was drawing; the conversation is orphaned: %q\n%s", latest.Text, view)
	}
	// And it is a real flood that was survived, not an idle connection: without
	// the daemon having anything to write, there is nothing for its write
	// timeout to expire on and this test would pass against the bug.
	if !strings.Contains(view, "flood line") {
		t.Fatalf("no agent output reached the transcript, so nothing was ever written to this client:\n%s", view)
	}

	// The flood is larger than the ring on purpose, so the gap path runs. What
	// is under test is not that nothing was dropped - a client that cannot draw
	// as fast as a fleet talks *must* drop - but that it was dropped from a
	// connection that stayed up, and that the reader was told.
	if !reported {
		t.Fatal("a flood larger than the buffer was folded and nothing was reported at all")
	}
	if !strings.Contains(latest.Text, "gap") {
		t.Errorf("frames were lost and the notice does not say the conversation has a gap: %q", latest.Text)
	}
	t.Logf("MEASURED: after a %v stall the connection is up and the reader was told: %q", stalledRender, latest.Text)
}

// neverReads is a terminal with nobody at it, which ends when the test does.
type neverReads struct{ quit chan struct{} }

func (r neverReads) Read([]byte) (int, error) {
	<-r.quit
	return 0, io.EOF
}

// ansiPattern strips a frame to what a reader sees. The transcript is rendered
// through glamour, which breaks even a short sentence into several colour runs,
// so a Contains over the raw frame is testing escape codes.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripEscapes(frame string) string { return ansiPattern.ReplaceAllString(frame, "") }
