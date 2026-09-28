package ui

// A /rename typed while its agent is busy waits in type-ahead with its mirror:
// Wake is renamed when the passthrough reaches claude, the mirror written just
// before it, never at the keystroke (queue.go, renamesync.go).

import (
	"slices"
	"testing"

	"github.com/DilanDoshi/wake/internal/rpc"
)

// frameKinds is each frame's kind, session and text, for comparing a flush.
func frameKinds(frames []rpc.Frame) []string {
	var out []string
	for _, f := range frames {
		out = append(out, f.Kind+" "+f.SessionID+" "+f.Text)
	}
	return out
}

// busyDM is a DM on s1 whose agent is working, so a message queues.
func busyDM(t *testing.T) App {
	t.Helper()
	return idleDM(t).applyFrame(oneAgent("s1", "alex", rpc.StateWorking))
}

// A /rename bob typed at a working agent writes nothing - no FrameRename, so
// Wake's handle does not move - until the flush, which writes the mirror
// immediately before its passthrough.
func TestABusyRenameSendsItsMirrorOnlyAtTheFlush(t *testing.T) {
	m, cmd := typeAndSubmit(busyDM(t), "/rename bob")
	a := m.(App)
	if cmd != nil {
		t.Fatalf("a /rename typed at a working agent wrote %v at the keystroke", frameKinds(batchFrames(t, a, cmd)))
	}

	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateIdle))
	a, cmd = a.flushQueued()
	frames := sentFrames(t, a, cmd)
	want := []string{rpc.FrameRename + " s1 bob", rpc.FrameSend + " s1 /rename bob"}
	if got := frameKinds(frames); !slices.Equal(got, want) {
		t.Fatalf("the flush wrote %v, want the mirror just before its passthrough %v", got, want)
	}
	if !frames[0].SelfRenames {
		t.Error("the flushed mirror does not say its keystroke also renames the agent")
	}
}

// Two /renames queued at a working agent flush one per turn, each mirror just
// before its own passthrough - so the daemon never holds for two at once.
func TestTwoQueuedRenamesFlushOnePerTurnEachBehindItsMirror(t *testing.T) {
	m, _ := typeAndSubmit(busyDM(t), "/rename bob")
	m, _ = typeAndSubmit(m.(App), "/rename cat")
	a := m.(App).applyFrame(oneAgent("s1", "alex", rpc.StateIdle))

	a, cmd := a.flushQueued()
	if got, want := frameKinds(sentFrames(t, a, cmd)), []string{rpc.FrameRename + " s1 bob", rpc.FrameSend + " s1 /rename bob"}; !slices.Equal(got, want) {
		t.Fatalf("the first turn's flush wrote %v, want %v", got, want)
	}
	if _, cmd = a.flushQueued(); cmd != nil {
		t.Fatal("a second /rename flushed while the first was still in flight")
	}

	a = a.applyFrame(lifecycleFrame("s1", a.inflight["s1"], "completed"))
	a, cmd = a.flushQueued()
	if got, want := frameKinds(sentFrames(t, a, cmd)), []string{rpc.FrameRename + " s1 cat", rpc.FrameSend + " s1 /rename cat"}; !slices.Equal(got, want) {
		t.Fatalf("the second turn's flush wrote %v, want %v", got, want)
	}
}

// A queued /rename whose agent parks is dropped with its mirror: nothing is
// renamed, at the keystroke or after.
func TestADroppedQueuedRenameSendsNoMirror(t *testing.T) {
	m, cmd := typeAndSubmit(busyDM(t), "/rename bob")
	a := m.(App)
	if cmd != nil {
		t.Fatalf("a queued /rename wrote %v at the keystroke", frameKinds(batchFrames(t, a, cmd)))
	}
	a = a.applyFrame(oneAgent("s1", "alex", rpc.StateParked))
	a, cmd = a.flushQueued()
	if cmd != nil {
		t.Fatalf("a parked agent's queued /rename wrote %v", frameKinds(batchFrames(t, a, cmd)))
	}
	if _, held := a.queued["s1"]; held {
		t.Error("a parked agent's queued /rename was not dropped")
	}
}

// The room's @who /rename to a working agent waits the same way: no mirror at
// the keystroke, and at the flush the mirror just before the passthrough.
func TestARoomRenameToABusyAgentWaitsWithItsMirror(t *testing.T) {
	a := newRoomApp(t).withSize(200, 40).applyFrame(oneAgent("s2", "alex", rpc.StateWorking))
	m, cmd := typeAndSubmit(a, "@alex /rename bob")
	a = m.(App)
	if cmd != nil {
		for _, f := range batchFrames(t, a, cmd) {
			if f.Kind == rpc.FrameRename {
				t.Fatalf("a room /rename to a working agent wrote its mirror at the keystroke: %+v", f)
			}
		}
	}

	a = a.applyFrame(oneAgent("s2", "alex", rpc.StateIdle))
	a, cmd = a.flushQueued()
	got := frameKinds(sentFrames(t, a, cmd))
	if want := []string{rpc.FrameRename + " s2 bob", rpc.FrameSend + " s2 /rename bob"}; !slices.Equal(got, want) {
		t.Fatalf("the flush wrote %v, want the mirror just before its passthrough %v", got, want)
	}
}
