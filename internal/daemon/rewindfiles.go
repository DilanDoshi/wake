package daemon

// Restoring an agent's files: FrameRewindPreview, FrameRewindFiles and
// FrameRewindBoth. Each is a rewind_files request through Session.RewindFiles;
// a preview's answer goes to its asker alone (mcpAsker), and both's
// conversation rewind waits here on the restore's receipt.

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/DilanDoshi/wake/internal/core"
	"github.com/DilanDoshi/wake/internal/rpc"
)

// rewindFiles writes the preview or restore p's frame names. The id is minted
// and its follow-up recorded before the write, because the answer can arrive
// before the write returns.
func (a *agent) rewindFiles(p pending) error {
	f := p.frame
	ask := core.FilesRewind{Target: f.RewindTarget, Preview: f.Kind == rpc.FrameRewindPreview, Both: f.Kind == rpc.FrameRewindBoth}
	if ask.Both && f.RewindLastSeen == "" {
		return fmt.Errorf("%w: rewinding the conversation too needs its last-seen message", core.ErrNotWritten)
	}
	id := uuid.NewString()
	switch {
	case ask.Preview:
		a.noteMCPAsker(id, p.from)
	case ask.Both:
		a.noteRewindAfter(id, pending{from: p.from, frame: rpc.Frame{Kind: rpc.FrameRewind, SessionID: a.id,
			RewindTarget: f.RewindTarget, RewindLastSeen: f.RewindLastSeen}})
	}
	err := a.sess.RewindFiles(id, ask)
	if err != nil {
		_, _ = a.takeMCPAsker(id)
		_, _ = a.takeRewindAfter(id)
	}
	return err
}

// continueRewind queues both's conversation rewind once its restore has
// succeeded, behind whatever is already queued for this agent, and drops it
// otherwise: a failed restore must not leave a rewound conversation over files
// that were never put back. Called from fanOut, as a deferred probe is.
func (a *agent) continueRewind(ev core.Event) {
	if ev.Kind != core.KindFilesRewindReceipt || ev.Files == nil || !ev.Files.Both {
		return
	}
	then, ok := a.takeRewindAfter(ev.RequestID)
	if !ok || !ev.Files.Restorable || ev.Files.Error != "" {
		return
	}
	if err := a.submit(then.from, then.frame); err != nil {
		logf("wake: session %s: the conversation rewind after a restore was not queued: %v", a.id, err)
		then.from.enqueue(errorFrame(a.id, "the files were restored but the conversation was not rewound: "+err.Error()))
	}
}

func (a *agent) noteRewindAfter(id string, p pending) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.rewindsAfter == nil {
		a.rewindsAfter = map[string]pending{}
	}
	a.rewindsAfter[id] = p
}

func (a *agent) takeRewindAfter(id string) (pending, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.rewindsAfter[id]
	delete(a.rewindsAfter, id)
	return p, ok
}
