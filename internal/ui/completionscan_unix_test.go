//go:build unix

package ui

// A directory that never answers, and what it costs the goroutine that draws.
//
// pathScanMax bounds how many entries one read may take and nothing bounds how
// long it may take: a hard NFS mount, a cloud-sync placeholder and a stalled
// sshfs all stall in the syscall. A FIFO with no writer stalls in the same
// place and is reproducible on this machine.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// hungRead is how long a read is given before it counts as never answering.
// Generous, because this is a bound on a failure rather than a measurement.
const hungRead = 5 * time.Second

func TestADirectoryThatNeverAnswersDoesNotStopTheKeys(t *testing.T) {
	dir := workdir(t)
	hang := filepath.Join(dir, "hang")
	if err := syscall.Mkfifo(hang, 0o600); err != nil {
		t.Skipf("this platform cannot make a fifo, so it cannot stall a read: %v", err)
	}
	t.Cleanup(func() {
		// A writer releases anything still stuck in the open below, so a failure
		// here does not park an OS thread for the rest of the run.
		if w, err := os.OpenFile(hang, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = w.Close()
		}
	})

	fresh(t)
	a := newRoomApp(t).withSize(200, 40).withRoster(
		rpc.SessionStatus{ID: "s1", Name: "alex", Dir: dir, State: rpc.StateIdle},
	)

	typed := make(chan App, 1)
	go func() {
		// Not withDraft: that helper answers the read the way Bubble Tea does,
		// and the whole question here is whether Update waited for it.
		var m tea.Model = a
		for _, r := range agentPrefix + "hang" + string(os.PathSeparator) {
			m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		typed <- m.(App)
	}()

	select {
	case next := <-typed:
		if got := next.composer().Value(); got != agentPrefix+"hang"+string(os.PathSeparator) {
			t.Errorf("the draft is %q: the keys arrived but not all of them", got)
		}
	case <-time.After(hungRead):
		t.Fatalf("typing a path into a directory that never answers took longer than %s. A read on "+
			"the Update goroutine stops the frame *and* every key, including the ones that quit", hungRead)
	}
}
