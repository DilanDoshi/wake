package daemon

// Restoring an agent's files: FrameRewindPreview, FrameRewindFiles and
// FrameRewindBoth. Each is a rewind_files request through Session.RewindFiles;
// a preview's answer goes to its asker alone (mcpAsker), and both holds this
// agent's input until its restore answers, then rewinds the conversation.

import (
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// restoreRefusal is why this agent's files may not be restored now, or nil.
// Read under the lock on the input goroutine every send also goes through, so
// a send queued ahead of a restore has already marked its turn owed: a restore
// aimed from a window's stale idle never rewrites files under a running turn.
func (a *agent) restoreRefusal() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case len(a.pending) > 0:
		return errors.New("this session is stopped on a permission request; answer or withdraw it before restoring its files")
	case a.owed:
		return errors.New("this session is working; let its turn end, or interrupt it, before restoring its files")
	}
	return nil
}

// rewindFiles writes the preview or restore p's frame names. The id is minted
// and its asker or waiter recorded before the write, because the answer can
// arrive before the write returns.
func (a *agent) rewindFiles(p pending) error {
	f := p.frame
	ask := core.FilesRewind{Target: f.RewindTarget, Preview: f.Kind == rpc.FrameRewindPreview, Both: f.Kind == rpc.FrameRewindBoth}
	if ask.Both && f.RewindLastSeen == "" {
		return fmt.Errorf("%w: rewinding the conversation too needs its last-seen message", core.ErrNotWritten)
	}
	id := uuid.NewString()
	var answered <-chan core.Event
	switch {
	case ask.Preview:
		a.noteMCPAsker(id, p.from)
	case ask.Both:
		answered = a.awaitRestore(id)
	}
	if err := a.sess.RewindFiles(id, ask); err != nil {
		_, _ = a.takeMCPAsker(id)
		_ = a.takeRestore(id)
		return err
	}
	if answered == nil {
		return nil
	}
	return a.rewindAfterRestore(answered, f)
}

// rewindAfterRestore is both's second half. It runs on the input goroutine,
// so nothing queued behind both reaches stdin between the restore and the
// conversation rewind, and it rewinds only once the restore has succeeded: a
// rewound conversation over files that were not put back is a known-wrong state.
func (a *agent) rewindAfterRestore(answered <-chan core.Event, f rpc.Frame) error {
	select {
	case ev := <-answered:
		if !ev.Files.Restorable || ev.Files.Error != "" {
			return nil // the receipt already told every window
		}
	case <-a.gone:
		return nil
	}
	if _, err := a.sess.Rewind(f.RewindTarget, f.RewindLastSeen); err != nil {
		return fmt.Errorf("the files were restored but the conversation was not rewound: %w", err)
	}
	return nil
}

// continueRewind hands both's restore receipt to the input goroutine waiting on
// it. Called from fanOut, after the receipt has gone to every window.
func (a *agent) continueRewind(ev core.Event) {
	if ev.Kind != core.KindFilesRewindReceipt || ev.Files == nil || !ev.Files.Both {
		return
	}
	if ch := a.takeRestore(ev.RequestID); ch != nil {
		ch <- ev
	}
}

// awaitRestore records a waiter for the restore id answers; buffered, so the
// hand-over never blocks fanOut.
func (a *agent) awaitRestore(id string) <-chan core.Event {
	ch := make(chan core.Event, 1)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.restoresAwaited == nil {
		a.restoresAwaited = map[string]chan core.Event{}
	}
	a.restoresAwaited[id] = ch
	return ch
}

func (a *agent) takeRestore(id string) chan core.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	ch := a.restoresAwaited[id]
	delete(a.restoresAwaited, id)
	return ch
}
