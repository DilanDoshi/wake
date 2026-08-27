//go:build unix

package daemon

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestDiscoverySkipsAFIFOWithoutBlocking(t *testing.T) {
	projects := t.TempDir()
	real := t.TempDir()
	slug := slugOf(real)
	const (
		fifoID       = "11111111-1111-4111-8111-111111111111"
		transcriptID = "22222222-2222-4222-8222-222222222222"
		wait         = time.Second
	)
	writeTranscript(t, projects, slug, transcriptID, real)
	fifo := filepath.Join(projects, slug, fifoID+".jsonl")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skipf("this platform cannot make a fifo: %v", err)
	}
	t.Cleanup(func() {
		// Release an unfixed discovery goroutine after the timeout assertion.
		if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
	})

	type result struct {
		sessions []FoundSession
		err      error
	}
	done := make(chan result, 1)
	go func() {
		sessions, err := discover(projects)
		done <- result{sessions: sessions, err: err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("discover: %v", got.err)
		}
		if len(got.sessions) != 1 || got.sessions[0].ID != transcriptID {
			t.Errorf("discovery reported %+v, want only ordinary transcript %s", got.sessions, transcriptID)
		}
	case <-time.After(wait):
		t.Fatalf("discovery blocked longer than %s opening a UUID-named FIFO", wait)
	}
}
